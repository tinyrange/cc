package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"j5.nz/cc/client"
	intcvmfs "j5.nz/cc/internal/cvmfs"
	"j5.nz/cc/internal/fsmeta"
	"j5.nz/cc/internal/imagefs"
)

func TestStorePullExtractsRootFSAndRuntimeConfig(t *testing.T) {
	layer1 := gzipTar(t, map[string]tarEntry{
		"bin/":           {Typeflag: tar.TypeDir, Mode: 0o755},
		"bin/busybox":    {Data: []byte("busybox"), Mode: 0o755},
		"bin/sh":         {Typeflag: tar.TypeSymlink, Linkname: "busybox", Mode: 0o777},
		"etc/":           {Typeflag: tar.TypeDir, Mode: 0o755},
		"etc/obsolete":   {Data: []byte("old"), Mode: 0o644},
		"etc/os-release": {Data: []byte("NAME=Alpine"), Mode: 0o644},
		"usr/":           {Typeflag: tar.TypeDir, Mode: 0o755},
		"usr/bin/":       {Typeflag: tar.TypeDir, Mode: 0o755},
		"usr/bin/uname":  {Data: []byte("fake"), Mode: 0o755},
	})
	layer2 := gzipTar(t, map[string]tarEntry{
		"etc/.wh.obsolete": {Data: nil, Mode: 0o000},
		"etc/hostname":     {Data: []byte("ccx3"), Mode: 0o644},
	})

	configBlob, err := json.Marshal(map[string]any{
		"architecture": nativeArch(),
		"config": map[string]any{
			"Env":        []string{"PATH=/usr/bin:/bin", "HOME=/root"},
			"Entrypoint": []string{"/bin/sh", "-c"},
			"Cmd":        []string{"echo default"},
			"WorkingDir": "/work",
			"User":       "",
			"Labels":     map[string]string{"test": "true"},
		},
	})
	if err != nil {
		t.Fatalf("Marshal(config) error = %v", err)
	}

	manifestDigest := "sha256:manifest"
	configDigest := "sha256:config"
	layer1Digest := "sha256:layer1"
	layer2Digest := "sha256:layer2"

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/library/alpine/manifests/latest":
			w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"schemaVersion": 2,
				"mediaType":     "application/vnd.oci.image.index.v1+json",
				"manifests": []map[string]any{{
					"mediaType": "application/vnd.oci.image.manifest.v1+json",
					"digest":    manifestDigest,
					"platform":  map[string]any{"os": "linux", "architecture": nativeArch()},
				}},
			})
		case "/v2/library/alpine/manifests/" + manifestDigest:
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"schemaVersion": 2,
				"mediaType":     "application/vnd.oci.image.manifest.v1+json",
				"config":        map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": configDigest},
				"layers": []map[string]any{
					{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": layer1Digest},
					{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": layer2Digest},
				},
			})
		case "/v2/library/alpine/blobs/" + configDigest:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(configBlob)
		case "/v2/library/alpine/blobs/" + layer1Digest:
			w.Header().Set("Content-Type", "application/vnd.oci.image.layer.v1.tar+gzip")
			_, _ = w.Write(layer1)
		case "/v2/library/alpine/blobs/" + layer2Digest:
			w.Header().Set("Content-Type", "application/vnd.oci.image.layer.v1.tar+gzip")
			_, _ = w.Write(layer2)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store := NewStore(t.TempDir())
	store.httpClient = server.Client()

	source := server.Listener.Addr().String() + "/library/alpine:latest"
	state, err := store.Pull(context.Background(), "alpine", source)
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if state.Status != "downloaded" {
		t.Fatalf("Pull().Status = %q, want downloaded", state.Status)
	}
	if state.SourceKind != SourceKindOCI {
		t.Fatalf("Pull().SourceKind = %q, want %q", state.SourceKind, SourceKindOCI)
	}

	img, err := store.Open("alpine")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if img.SourceKind != SourceKindOCI {
		t.Fatalf("img.SourceKind = %q, want %q", img.SourceKind, SourceKindOCI)
	}
	if img.Architecture != "arm64" && img.Architecture != "amd64" {
		t.Fatalf("img.Architecture = %q, want arm64 or amd64", img.Architecture)
	}
	if got := img.Command(nil); len(got) != 3 || got[0] != "/bin/sh" || got[2] != "echo default" {
		t.Fatalf("img.Command(nil) = %v", got)
	}
	if _, err := imagefs.LookupPath(img.RootFS, "/etc/obsolete"); err == nil {
		t.Fatal("LookupPath(/etc/obsolete) error = nil, want not found")
	}
	entry, err := imagefs.LookupPath(img.RootFS, "/etc/hostname")
	if err != nil {
		t.Fatalf("LookupPath(/etc/hostname) error = %v", err)
	}
	data, err := entry.File.ReadAt(0, 64)
	if err != nil {
		t.Fatalf("hostname.ReadAt() error = %v", err)
	}
	if string(data) != "ccx3" {
		t.Fatalf("hostname = %q, want ccx3", string(data))
	}
	entry, err = imagefs.LookupPath(img.RootFS, "/bin/sh")
	if err != nil {
		t.Fatalf("LookupPath(/bin/sh) error = %v", err)
	}
	target := entry.Symlink.Target()
	if target != "busybox" {
		t.Fatalf("bin/sh target = %q, want busybox", target)
	}
}

func TestStorePullDockerArchiveImportsIndexedLayers(t *testing.T) {
	layer1 := plainTar(t, map[string]tarEntry{
		"bin/":           {Typeflag: tar.TypeDir, Mode: 0o755},
		"bin/tool":       {Data: []byte("tool"), Mode: 0o755},
		"bin/tool-link":  {Typeflag: tar.TypeSymlink, Linkname: "tool", Mode: 0o777},
		"etc/":           {Typeflag: tar.TypeDir, Mode: 0o755},
		"etc/obsolete":   {Data: []byte("old"), Mode: 0o644},
		"etc/os-release": {Data: []byte("NAME=Archive"), Mode: 0o644},
	})
	layer2 := plainTar(t, map[string]tarEntry{
		"etc/.wh.obsolete": {Data: nil, Mode: 0o000},
		"etc/hostname":     {Data: []byte("archive-host"), Mode: 0o644},
	})
	configBlob, err := json.Marshal(map[string]any{
		"architecture": nativeArch(),
		"config": map[string]any{
			"Env":        []string{"PATH=/bin", "HOME=/root"},
			"Entrypoint": []string{"/bin/tool"},
			"Cmd":        []string{"--help"},
			"WorkingDir": "/work",
			"User":       "1000",
			"Labels":     map[string]string{"archive": "true"},
		},
	})
	if err != nil {
		t.Fatalf("Marshal(config) error = %v", err)
	}
	archivePath := dockerArchive(t, []dockerArchiveFixtureImage{{
		ConfigName: "config.json",
		Config:     configBlob,
		RepoTags:   []string{"example/tool:latest"},
		Layers: []dockerArchiveFixtureLayer{
			{Name: "111/layer.tar", Data: layer1},
			{Name: "222/layer.tar", Data: layer2},
		},
	}})

	store := NewStore(t.TempDir())
	state, err := store.Pull(context.Background(), "tool", "docker-archive:"+archivePath)
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if state.SourceKind != SourceKindDockerArchive {
		t.Fatalf("Pull().SourceKind = %q, want %q", state.SourceKind, SourceKindDockerArchive)
	}
	img, err := store.Open("tool")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if img.SourceKind != SourceKindDockerArchive {
		t.Fatalf("img.SourceKind = %q, want %q", img.SourceKind, SourceKindDockerArchive)
	}
	if got := img.Command(nil); len(got) != 2 || got[0] != "/bin/tool" || got[1] != "--help" {
		t.Fatalf("img.Command(nil) = %v, want entrypoint plus cmd", got)
	}
	if img.Config.WorkingDir != "/work" || img.Config.User != "1000" || img.Config.Labels["archive"] != "true" {
		t.Fatalf("runtime config not preserved: %#v", img.Config)
	}
	if _, err := imagefs.LookupPath(img.RootFS, "/etc/obsolete"); err == nil {
		t.Fatal("LookupPath(/etc/obsolete) error = nil, want whiteout removal")
	}
	entry, err := imagefs.LookupPath(img.RootFS, "/etc/hostname")
	if err != nil {
		t.Fatalf("LookupPath(/etc/hostname) error = %v", err)
	}
	data, err := entry.File.ReadAt(0, 64)
	if err != nil {
		t.Fatalf("hostname.ReadAt() error = %v", err)
	}
	if string(data) != "archive-host" {
		t.Fatalf("hostname = %q, want archive-host", string(data))
	}
	link, err := imagefs.LookupPath(img.RootFS, "/bin/tool-link")
	if err != nil {
		t.Fatalf("LookupPath(/bin/tool-link) error = %v", err)
	}
	if link.Symlink.Target() != "tool" {
		t.Fatalf("tool-link target = %q, want tool", link.Symlink.Target())
	}
	layerMatches, err := filepath.Glob(filepath.Join(store.imageDir("tool"), "layers", "*.tar"))
	if err != nil {
		t.Fatalf("Glob(layers) error = %v", err)
	}
	if len(layerMatches) != 2 {
		t.Fatalf("stored layers = %d, want 2 (%v)", len(layerMatches), layerMatches)
	}
	if _, err := os.Stat(filepath.Join(store.imageDir("tool"), "manifest.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("manifest.json stat error = %v, want not exist", err)
	}
}

func TestStorePullDockerArchiveSelectsRepoTag(t *testing.T) {
	firstConfig := dockerImageConfigBlob(t, "first")
	secondConfig := dockerImageConfigBlob(t, "second")
	firstLayer := plainTar(t, map[string]tarEntry{"first.txt": {Data: []byte("first"), Mode: 0o644}})
	secondLayer := plainTar(t, map[string]tarEntry{"second.txt": {Data: []byte("second"), Mode: 0o644}})
	archivePath := dockerArchive(t, []dockerArchiveFixtureImage{
		{
			ConfigName: "first.json",
			Config:     firstConfig,
			RepoTags:   []string{"example/first:latest"},
			Layers:     []dockerArchiveFixtureLayer{{Name: "first/layer.tar", Data: firstLayer}},
		},
		{
			ConfigName: "second.json",
			Config:     secondConfig,
			RepoTags:   []string{"example/second:latest"},
			Layers:     []dockerArchiveFixtureLayer{{Name: "second/layer.tar", Data: secondLayer}},
		},
	})

	store := NewStore(t.TempDir())
	if _, err := store.Pull(context.Background(), "selected", "docker-archive:"+archivePath+"#example/second:latest"); err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	img, err := store.Open("selected")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if _, err := imagefs.LookupPath(img.RootFS, "/first.txt"); err == nil {
		t.Fatal("LookupPath(/first.txt) error = nil, want selected image only")
	}
	entry, err := imagefs.LookupPath(img.RootFS, "/second.txt")
	if err != nil {
		t.Fatalf("LookupPath(/second.txt) error = %v", err)
	}
	data, err := entry.File.ReadAt(0, 64)
	if err != nil {
		t.Fatalf("second.ReadAt() error = %v", err)
	}
	if string(data) != "second" {
		t.Fatalf("second.txt = %q, want second", string(data))
	}
}

func TestStorePullDockerArchiveRequiresSelectorForMultipleImages(t *testing.T) {
	archivePath := dockerArchive(t, []dockerArchiveFixtureImage{
		{
			ConfigName: "first.json",
			Config:     dockerImageConfigBlob(t, "first"),
			RepoTags:   []string{"example/first:latest"},
			Layers:     []dockerArchiveFixtureLayer{{Name: "first/layer.tar", Data: plainTar(t, map[string]tarEntry{"first.txt": {Data: []byte("first"), Mode: 0o644}})}},
		},
		{
			ConfigName: "second.json",
			Config:     dockerImageConfigBlob(t, "second"),
			RepoTags:   []string{"example/second:latest"},
			Layers:     []dockerArchiveFixtureLayer{{Name: "second/layer.tar", Data: plainTar(t, map[string]tarEntry{"second.txt": {Data: []byte("second"), Mode: 0o644}})}},
		},
	})

	store := NewStore(t.TempDir())
	_, err := store.Pull(context.Background(), "ambiguous", "docker-archive:"+archivePath)
	if err == nil || !strings.Contains(err.Error(), "select one") {
		t.Fatalf("Pull() error = %v, want selector error", err)
	}
}

func BenchmarkStorePullOCIIndexedLayers(b *testing.B) {
	layers := make([][]byte, 0, 4)
	layerDescs := make([]map[string]any, 0, 4)
	layerBlobs := map[string][]byte{}
	for layerIndex := range 4 {
		entries := make(map[string]tarEntry, 256)
		for fileIndex := range 256 {
			name := "opt/bench/layer" + strconv.Itoa(layerIndex) + "/file" + strconv.Itoa(fileIndex) + ".txt"
			entries[name] = tarEntry{
				Data: []byte(strings.Repeat("benchmark-data-"+strconv.Itoa(layerIndex)+"-"+strconv.Itoa(fileIndex)+"\n", 16)),
				Mode: 0o644,
			}
		}
		layer := gzipTar(b, entries)
		digest := digestBytes(layer)
		layers = append(layers, layer)
		layerDescs = append(layerDescs, map[string]any{
			"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip",
			"digest":    digest,
		})
		layerBlobs[digest] = layer
	}
	configBlob, err := json.Marshal(map[string]any{
		"architecture": nativeArch(),
		"config": map[string]any{
			"Env":        []string{"PATH=/usr/bin:/bin"},
			"Cmd":        []string{"/bin/sh"},
			"WorkingDir": "/",
		},
	})
	if err != nil {
		b.Fatalf("Marshal(config) error = %v", err)
	}
	configDigest := digestBytes(configBlob)
	manifestBlob, err := json.Marshal(map[string]any{
		"schemaVersion": 2,
		"mediaType":     "application/vnd.oci.image.manifest.v1+json",
		"config":        map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": configDigest},
		"layers":        layerDescs,
	})
	if err != nil {
		b.Fatalf("Marshal(manifest) error = %v", err)
	}
	manifestDigest := digestBytes(manifestBlob)
	_ = layers

	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/library/bench/manifests/latest":
			w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"schemaVersion": 2,
				"mediaType":     "application/vnd.oci.image.index.v1+json",
				"manifests": []map[string]any{{
					"mediaType": "application/vnd.oci.image.manifest.v1+json",
					"digest":    manifestDigest,
					"platform":  map[string]any{"os": "linux", "architecture": nativeArch()},
				}},
			})
		case "/v2/library/bench/manifests/" + manifestDigest:
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			_, _ = w.Write(manifestBlob)
		case "/v2/library/bench/blobs/" + configDigest:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(configBlob)
		default:
			if blob, ok := layerBlobs[strings.TrimPrefix(r.URL.Path, "/v2/library/bench/blobs/")]; ok {
				w.Header().Set("Content-Type", "application/vnd.oci.image.layer.v1.tar+gzip")
				_, _ = w.Write(blob)
				return
			}
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := server.Listener.Addr().String() + "/library/bench:latest"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store := NewStore(b.TempDir())
		store.httpClient = server.Client()
		spec := SourceSpec{Kind: SourceKindOCI, Raw: source}
		if err := store.pullDirect(context.Background(), "bench", spec, PullOptions{}); err != nil {
			b.Fatalf("pullDirect() error = %v", err)
		}
	}
}

func BenchmarkStorePullDockerArchiveIndexedLayers(b *testing.B) {
	layers := make([]dockerArchiveFixtureLayer, 0, 4)
	for layerIndex := range 4 {
		entries := make(map[string]tarEntry, 256)
		for fileIndex := range 256 {
			name := "opt/bench/layer" + strconv.Itoa(layerIndex) + "/file" + strconv.Itoa(fileIndex) + ".txt"
			entries[name] = tarEntry{
				Data: []byte(strings.Repeat("archive-data-"+strconv.Itoa(layerIndex)+"-"+strconv.Itoa(fileIndex)+"\n", 16)),
				Mode: 0o644,
			}
		}
		layers = append(layers, dockerArchiveFixtureLayer{
			Name: strconv.Itoa(layerIndex) + "/layer.tar",
			Data: plainTar(b, entries),
		})
	}
	archivePath := dockerArchive(b, []dockerArchiveFixtureImage{{
		ConfigName: "config.json",
		Config:     dockerImageConfigBlob(b, "bench"),
		RepoTags:   []string{"example/bench:latest"},
		Layers:     layers,
	}})
	spec := SourceSpec{Kind: SourceKindDockerArchive, Raw: "docker-archive:" + archivePath}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store := NewStore(b.TempDir())
		if err := store.pullDirect(context.Background(), "bench", spec, PullOptions{}); err != nil {
			b.Fatalf("pullDirect() error = %v", err)
		}
	}
}

func TestExtractSIMGDeployMetadataUsesBuildYamlAndSingularityEnv(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "build.yaml"), []byte(`
name: bart
deploy:
  path:
    - /opt/bart-0.9.00/
  bins: [bart, bart-view]
`), 0o644); err != nil {
		t.Fatalf("WriteFile(build.yaml) error = %v", err)
	}
	envDir := filepath.Join(root, ".singularity.d", "env")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(env) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "10-docker.sh"), []byte(`
export PATH="$PATH:/opt/docker-bin"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(10-docker.sh) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "90-environment.sh"), []byte(`
export PATH="/usr/local/bin:/usr/bin:/bin"
export PATH="/opt/conda/bin:$PATH"
export TOOLBOX_PATH="/opt/bart-0.9.00/"
export EMPTY_SUFFIX="$UNSET_VAR:/opt/empty"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(90-environment.sh) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "99-micromamba-startup.sh"), []byte(`
export MAMBA_ROOT_PREFIX="/opt/conda"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(99-micromamba-startup.sh) error = %v", err)
	}

	meta := extractSIMGDeployMetadata(imagefs.NewHostFS(root, nil))
	env := strings.Join(meta.Env, "\n")
	for _, want := range []string{
		"DEPLOY_PATH=/opt/bart-0.9.00/",
		"DEPLOY_BINS=bart:bart-view",
		"TOOLBOX_PATH=/opt/bart-0.9.00/",
		"MAMBA_ROOT_PREFIX=/opt/conda",
		"PATH=/opt/bart-0.9.00/:/opt/conda/bin:/usr/local/bin:/usr/bin:/bin",
		"EMPTY_SUFFIX=:/opt/empty",
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("deploy env missing %q in:\n%s", want, env)
		}
	}
	if got := strings.Join(meta.DeployPath, ":"); got != "/opt/bart-0.9.00/" {
		t.Fatalf("DeployPath = %q, want /opt/bart-0.9.00/", got)
	}
	if got := strings.Join(meta.DeployBins, ":"); got != "bart:bart-view" {
		t.Fatalf("DeployBins = %q, want bart:bart-view", got)
	}
}

func TestExtractSIMGDeployMetadataSourcesMicromambaStartup(t *testing.T) {
	root := t.TempDir()
	envDir := filepath.Join(root, ".singularity.d", "env")
	if err := os.MkdirAll(envDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(env) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "opt", "tool", "share", "mamba", "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll(mamba/bin) error = %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "opt", "tool", "share", "mamba", "condabin"), 0o755); err != nil {
		t.Fatalf("MkdirAll(mamba/condabin) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "10-docker2singularity.sh"), []byte(`
export PATH="/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"
export XDG_DATA_HOME="/opt/tool/share"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(10-docker2singularity.sh) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(envDir, "99-micromamba-startup.sh"), []byte(`
source /etc/profile.d/micromamba.sh
`), 0o644); err != nil {
		t.Fatalf("WriteFile(99-micromamba-startup.sh) error = %v", err)
	}

	meta := extractSIMGDeployMetadata(imagefs.NewHostFS(root, nil))
	env := strings.Join(meta.Env, "\n")
	for _, want := range []string{
		"MAMBA_ROOT_PREFIX=/opt/tool/share/mamba",
		"CONDA_PREFIX=/opt/tool/share/mamba",
		"PATH=/opt/tool/share/mamba/bin:/opt/tool/share/mamba/condabin:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("deploy env missing %q in:\n%s", want, env)
		}
	}
}

func TestExtractCVMFSDeployMetadataUsesSingularityEnv(t *testing.T) {
	packedPath := filepath.Join(t.TempDir(), "rootfs.contents")
	envData := []byte("export PATH=\"$PATH:/opt/palm-alpha119\"\n")
	if err := os.WriteFile(packedPath, envData, 0o644); err != nil {
		t.Fatalf("WriteFile(rootfs.contents) error = %v", err)
	}

	nodes := []indexedNode{
		{Path: "/", Kind: indexedKindDir, Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDir | 0o755)},
		{Path: "/.singularity.d", Kind: indexedKindDir, Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDir | 0o755)},
		{Path: "/.singularity.d/env", Kind: indexedKindDir, Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDir | 0o755)},
		{
			Path:         "/.singularity.d/env/90-environment.sh",
			Kind:         indexedKindFile,
			Mode:         0o644,
			Size:         uint64(len(envData)),
			Packed:       true,
			PackedOffset: 0,
		},
	}

	meta, err := extractCVMFSDeployMetadata(nil, packedPath, nodes)
	if err != nil {
		t.Fatalf("extractCVMFSDeployMetadata() error = %v", err)
	}
	env := strings.Join(meta.Env, "\n")
	if !strings.Contains(env, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/opt/palm-alpha119") {
		t.Fatalf("deploy env missing CVMFS Singularity PATH in:\n%s", env)
	}
}

func TestExtractCVMFSDeployMetadataReadsTargetEnv(t *testing.T) {
	root := t.TempDir()
	envPath := filepath.Join(root, "10-docker.sh")
	envData := []byte("export PATH=\"/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/opt/palm-alpha119\"\nexport DEPLOY_BINS=\"palm:octave\"\n")
	if err := os.WriteFile(envPath, envData, 0o644); err != nil {
		t.Fatalf("WriteFile(10-docker.sh) error = %v", err)
	}

	nodes := []indexedNode{
		{Path: "/", Kind: indexedKindDir, Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDir | 0o755)},
		{Path: "/.singularity.d", Kind: indexedKindDir, Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDir | 0o755)},
		{Path: "/.singularity.d/env", Kind: indexedKindDir, Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDir | 0o755)},
		{
			Path:        "/.singularity.d/env/10-docker.sh",
			Kind:        indexedKindFile,
			Mode:        0o644,
			Size:        uint64(len(envData)),
			CVMFSTarget: envPath,
		},
	}

	meta, err := extractCVMFSDeployMetadata(&intcvmfs.Client{}, "", nodes)
	if err != nil {
		t.Fatalf("extractCVMFSDeployMetadata() error = %v", err)
	}
	env := strings.Join(meta.Env, "\n")
	for _, want := range []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin:/opt/palm-alpha119",
		"DEPLOY_BINS=palm:octave",
	} {
		if !strings.Contains(env, want) {
			t.Fatalf("deploy env missing %q in:\n%s", want, env)
		}
	}
}

func TestParseTopLevelDeploySkipsUnrenderedTemplatePaths(t *testing.T) {
	paths, bins := parseTopLevelDeploy(`
deploy:
  path:
    - /opt/{{ context.name }}-{{ context.version }}/bin
    - /opt/tool/bin,/opt/other/bin
  bins:
    - tool
`)
	if got := strings.Join(paths, ":"); got != "/opt/tool/bin:/opt/other/bin" {
		t.Fatalf("paths = %q, want /opt/tool/bin:/opt/other/bin", got)
	}
	if got := strings.Join(bins, ":"); got != "tool" {
		t.Fatalf("bins = %q, want tool", got)
	}
}

func TestResolvePrefetchWorkers(t *testing.T) {
	cpus := runtime.NumCPU()
	if cpus < 1 {
		cpus = 1
	}
	defaultWorkers := cpus
	if defaultWorkers < 1 {
		defaultWorkers = 1
	}
	if defaultWorkers > 8 {
		defaultWorkers = 8
	}

	if got := resolvePrefetchWorkers(0); got != defaultWorkers {
		t.Fatalf("resolvePrefetchWorkers(0) = %d, want %d", got, defaultWorkers)
	}
	if got := resolvePrefetchWorkers(-1); got != defaultWorkers {
		t.Fatalf("resolvePrefetchWorkers(-1) = %d, want %d", got, defaultWorkers)
	}
	if got := resolvePrefetchWorkers(1); got != 1 {
		t.Fatalf("resolvePrefetchWorkers(1) = %d, want 1", got)
	}
	if got := resolvePrefetchWorkers(cpus + 100); got != cpus {
		t.Fatalf("resolvePrefetchWorkers(over cpu) = %d, want %d", got, cpus)
	}
}

func TestStorePullUsesSharedCacheAcrossStores(t *testing.T) {
	sharedCache := t.TempDir()
	if err := os.Setenv(sharedCacheEnv, sharedCache); err != nil {
		t.Fatalf("Setenv(%s) error = %v", sharedCacheEnv, err)
	}
	defer os.Unsetenv(sharedCacheEnv)

	layer := gzipTar(t, map[string]tarEntry{
		"bin/":        {Typeflag: tar.TypeDir, Mode: 0o755},
		"bin/busybox": {Data: []byte("busybox"), Mode: 0o755},
		"bin/uname":   {Data: []byte("uname"), Mode: 0o755},
	})
	configBlob, err := json.Marshal(map[string]any{
		"architecture": nativeArch(),
		"config": map[string]any{
			"Env": []string{"PATH=/usr/bin:/bin"},
			"Cmd": []string{"/bin/uname"},
		},
	})
	if err != nil {
		t.Fatalf("Marshal(config) error = %v", err)
	}

	const (
		manifestDigest = "sha256:manifest"
		configDigest   = "sha256:config"
		layerDigest    = "sha256:layer1"
	)
	var hits atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/v2/library/alpine/manifests/latest":
			w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"schemaVersion": 2,
				"mediaType":     "application/vnd.oci.image.index.v1+json",
				"manifests": []map[string]any{{
					"mediaType": "application/vnd.oci.image.manifest.v1+json",
					"digest":    manifestDigest,
					"platform":  map[string]any{"os": "linux", "architecture": nativeArch()},
				}},
			})
		case "/v2/library/alpine/manifests/" + manifestDigest:
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"schemaVersion": 2,
				"mediaType":     "application/vnd.oci.image.manifest.v1+json",
				"config":        map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": configDigest},
				"layers":        []map[string]any{{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": layerDigest}},
			})
		case "/v2/library/alpine/blobs/" + configDigest:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(configBlob)
		case "/v2/library/alpine/blobs/" + layerDigest:
			w.Header().Set("Content-Type", "application/vnd.oci.image.layer.v1.tar+gzip")
			_, _ = w.Write(layer)
		default:
			http.NotFound(w, r)
		}
	}))
	source := server.Listener.Addr().String() + "/library/alpine:latest"

	storeA := NewStore(t.TempDir())
	storeA.httpClient = server.Client()
	if _, err := storeA.Pull(context.Background(), "alpine", source); err != nil {
		t.Fatalf("first Pull() error = %v", err)
	}
	if hits.Load() == 0 {
		t.Fatalf("first pull made no registry requests")
	}
	server.Close()

	storeB := NewStore(t.TempDir())
	if _, err := storeB.Pull(context.Background(), "alpine", source); err != nil {
		t.Fatalf("second Pull() using shared cache error = %v", err)
	}
	state, err := storeB.Get("alpine")
	if err != nil {
		t.Fatalf("Get() after shared-cache restore error = %v", err)
	}
	if state.SourceKind != SourceKindOCI {
		t.Fatalf("restored state.SourceKind = %q, want %q", state.SourceKind, SourceKindOCI)
	}
	img, err := storeB.Open("alpine")
	if err != nil {
		t.Fatalf("Open() after shared-cache restore error = %v", err)
	}
	if img.RootFS == nil {
		t.Fatal("restored cached RootFS = nil")
	}
	entry, err := imagefs.LookupPath(img.RootFS, "/bin/busybox")
	if err != nil {
		t.Fatalf("LookupPath(/bin/busybox) error = %v", err)
	}
	data, err := entry.File.ReadAt(0, 64)
	if err != nil {
		t.Fatalf("busybox.ReadAt() error = %v", err)
	}
	if string(data) != "busybox" {
		t.Fatalf("restored cached busybox = %q, want busybox", string(data))
	}
}

func TestStorePullFallsBackToAMD64ManifestOnArm64Hosts(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("amd64 manifest fallback only applies on arm64 hosts")
	}

	layer := gzipTar(t, map[string]tarEntry{
		"bin/":        {Typeflag: tar.TypeDir, Mode: 0o755},
		"bin/busybox": {Data: []byte("amd64 busybox"), Mode: 0o755},
	})
	configBlob, err := json.Marshal(map[string]any{
		"architecture": "amd64",
		"config": map[string]any{
			"Env": []string{"PATH=/usr/bin:/bin"},
			"Cmd": []string{"/bin/busybox"},
		},
	})
	if err != nil {
		t.Fatalf("Marshal(config) error = %v", err)
	}

	const (
		manifestDigest = "sha256:manifest-amd64"
		configDigest   = "sha256:config-amd64"
		layerDigest    = "sha256:layer-amd64"
	)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/library/alpine/manifests/latest":
			w.Header().Set("Content-Type", "application/vnd.oci.image.index.v1+json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"schemaVersion": 2,
				"mediaType":     "application/vnd.oci.image.index.v1+json",
				"manifests": []map[string]any{{
					"mediaType": "application/vnd.oci.image.manifest.v1+json",
					"digest":    manifestDigest,
					"platform":  map[string]any{"os": "linux", "architecture": "amd64"},
				}},
			})
		case "/v2/library/alpine/manifests/" + manifestDigest:
			w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"schemaVersion": 2,
				"mediaType":     "application/vnd.oci.image.manifest.v1+json",
				"config":        map[string]any{"mediaType": "application/vnd.oci.image.config.v1+json", "digest": configDigest},
				"layers":        []map[string]any{{"mediaType": "application/vnd.oci.image.layer.v1.tar+gzip", "digest": layerDigest}},
			})
		case "/v2/library/alpine/blobs/" + configDigest:
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(configBlob)
		case "/v2/library/alpine/blobs/" + layerDigest:
			w.Header().Set("Content-Type", "application/vnd.oci.image.layer.v1.tar+gzip")
			_, _ = w.Write(layer)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	store := NewStore(t.TempDir())
	store.httpClient = server.Client()
	source := server.Listener.Addr().String() + "/library/alpine:latest"

	if _, err := store.Pull(context.Background(), "alpine", source); err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	img, err := store.Open("alpine")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if img.Architecture != "amd64" {
		t.Fatalf("img.Architecture = %q, want amd64", img.Architecture)
	}
	entry, err := imagefs.LookupPath(img.RootFS, "/bin/busybox")
	if err != nil {
		t.Fatalf("LookupPath(/bin/busybox) error = %v", err)
	}
	data, err := entry.File.ReadAt(0, 64)
	if err != nil {
		t.Fatalf("ReadAt(/bin/busybox) error = %v", err)
	}
	if string(data) != "amd64 busybox" {
		t.Fatalf("busybox data = %q, want %q", string(data), "amd64 busybox")
	}
}

func TestStorePullLocalSIMG(t *testing.T) {
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("fixture missing: %v", err)
	}

	store := NewStore(t.TempDir())
	state, err := store.Pull(context.Background(), "alpine-simg", fixture)
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if state.SourceKind != SourceKindSIMG {
		t.Fatalf("Pull().SourceKind = %q, want %q", state.SourceKind, SourceKindSIMG)
	}
	img, err := store.Open("alpine-simg")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if img.SourceKind != SourceKindSIMG {
		t.Fatalf("img.SourceKind = %q, want %q", img.SourceKind, SourceKindSIMG)
	}
	if img.Architecture != "arm64" && img.Architecture != "amd64" {
		t.Fatalf("img.Architecture = %q, want arm64 or amd64", img.Architecture)
	}
	entry, err := imagefs.LookupPath(img.RootFS, "/etc/alpine-release")
	if err != nil {
		t.Fatalf("LookupPath(/etc/alpine-release) error = %v", err)
	}
	data, err := entry.File.ReadAt(0, 64)
	if err != nil {
		t.Fatalf("ReadAt(/etc/alpine-release) error = %v", err)
	}
	if len(data) == 0 {
		t.Fatal("/etc/alpine-release is empty")
	}
}

func TestParseSource(t *testing.T) {
	tests := []struct {
		source   string
		wantKind string
		wantErr  bool
	}{
		{source: "docker.io/library/alpine:latest", wantKind: SourceKindOCI},
		{source: "localhost:5000/repo/image:tag", wantKind: SourceKindOCI},
		{source: "/tmp/tool.simg", wantKind: SourceKindSIMG},
		{source: "https://example.com/image.sif", wantKind: SourceKindSIMG},
		{source: "https://cvmfs.neurodesk.org/cvmfs/neurodesk.ardc.edu.au/containers/tool/tool.simg", wantKind: SourceKindCVMFS},
		{source: "http+cvmfs://cvmfs.neurodesk.org/cvmfs/neurodesk.ardc.edu.au?path=containers/tool", wantKind: SourceKindCVMFS},
		{source: "cvmfs://neurodesk.ardc.edu.au?path=containers/tool", wantKind: SourceKindCVMFS},
		{source: "docker-archive:/tmp/tool.tar#example/tool:latest", wantKind: SourceKindDockerArchive},
		{source: "", wantErr: true},
	}
	for _, tt := range tests {
		got, err := ParseSource(tt.source)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("ParseSource(%q) error = nil, want error", tt.source)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseSource(%q) error = %v", tt.source, err)
		}
		if got.Kind != tt.wantKind {
			t.Fatalf("ParseSource(%q).Kind = %q, want %q", tt.source, got.Kind, tt.wantKind)
		}
		if got.Raw != tt.source {
			t.Fatalf("ParseSource(%q).Raw = %q, want original source", tt.source, got.Raw)
		}
	}
}

func TestPreferredManifestArchitecturesHonorsRequestedArch(t *testing.T) {
	got := preferredManifestArchitectures("x86_64")
	if len(got) != 1 || got[0] != "amd64" {
		t.Fatalf("preferredManifestArchitectures(x86_64) = %#v, want amd64 only", got)
	}
}

func TestSharedImageKeyIncludesRequestedArchitecture(t *testing.T) {
	spec := SourceSpec{Kind: SourceKindOCI, Raw: "ubuntu"}
	if sharedImageKey(spec, "amd64") == sharedImageKey(spec, "arm64") {
		t.Fatal("shared image key should differ by requested architecture")
	}
}

func TestStoreReadMetadataBackfillsSourceKind(t *testing.T) {
	store := NewStore(t.TempDir())
	imageDir := filepath.Join(store.Root(), "legacy")
	if err := os.MkdirAll(imageDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", imageDir, err)
	}
	buf, err := json.MarshalIndent(map[string]any{
		"name":       "legacy",
		"source":     "docker.io/library/alpine:latest",
		"rootfs_dir": imageDir,
	}, "", "  ")
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(imageDir, "image.json"), buf, 0o644); err != nil {
		t.Fatalf("WriteFile(image.json) error = %v", err)
	}
	state, err := store.Get("legacy")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if state.SourceKind != SourceKindOCI {
		t.Fatalf("state.SourceKind = %q, want %q", state.SourceKind, SourceKindOCI)
	}

	img, err := store.Open("legacy")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if img.SourceKind != SourceKindOCI {
		t.Fatalf("img.SourceKind = %q, want %q", img.SourceKind, SourceKindOCI)
	}
}

func TestStoreSaveRootFSPersistsImageAndExcludesRuntimePaths(t *testing.T) {
	rootDir := t.TempDir()
	for _, dir := range []string{"etc", "bin", "tmp", "proc", "host", ".ccx3"} {
		if err := os.MkdirAll(filepath.Join(rootDir, dir), 0o755); err != nil {
			t.Fatalf("MkdirAll(%s) error = %v", dir, err)
		}
	}
	if err := os.WriteFile(filepath.Join(rootDir, "etc", "motd"), []byte("hello saved image"), 0o644); err != nil {
		t.Fatalf("WriteFile(motd) error = %v", err)
	}
	if err := os.Symlink("../etc/motd", filepath.Join(rootDir, "bin", "motd")); err != nil {
		if runtime.GOOS == "windows" && (os.IsPermission(err) || strings.Contains(strings.ToLower(err.Error()), "privilege")) {
			t.Skipf("creating symlinks requires Windows developer mode or SeCreateSymbolicLinkPrivilege: %v", err)
		}
		t.Fatalf("Symlink(bin/motd) error = %v", err)
	}
	for _, file := range []string{"tmp/drop", "proc/drop", "host/drop", ".ccx3/drop"} {
		if err := os.WriteFile(filepath.Join(rootDir, filepath.FromSlash(file)), []byte("runtime"), 0o644); err != nil {
			t.Fatalf("WriteFile(%s) error = %v", file, err)
		}
	}

	store := NewStore(t.TempDir())
	state, err := store.SaveRootFS(context.Background(), "saved", imagefs.NewHostFS(rootDir, nil), SaveOptions{
		Source:       "vm:work from ubuntu",
		Architecture: "amd64",
		Config: RuntimeConfig{
			Env:        []string{"A=B"},
			Entrypoint: []string{"/bin/sh"},
			Cmd:        []string{"-l"},
			WorkingDir: "/work",
			User:       "root",
			Labels:     map[string]string{"saved": "true"},
		},
	})
	if err != nil {
		t.Fatalf("SaveRootFS() error = %v", err)
	}
	if state.Name != "saved" || state.SourceKind != SourceKindSaved {
		t.Fatalf("state = %#v, want saved image state", state)
	}

	img, err := store.Open("saved")
	if err != nil {
		t.Fatalf("Open(saved) error = %v", err)
	}
	if img.SourceKind != SourceKindSaved || img.Source != "vm:work from ubuntu" || img.Architecture != "amd64" {
		t.Fatalf("image metadata = %#v", img)
	}
	if len(img.Config.Env) != 1 || img.Config.Env[0] != "A=B" || img.Config.WorkingDir != "/work" || img.Config.User != "root" {
		t.Fatalf("runtime config = %#v, want inherited config", img.Config)
	}

	entry, err := imagefs.LookupPath(img.RootFS, "/etc/motd")
	if err != nil {
		t.Fatalf("LookupPath(/etc/motd) error = %v", err)
	}
	data, err := entry.File.ReadAt(0, 64)
	if err != nil {
		t.Fatalf("ReadAt(/etc/motd) error = %v", err)
	}
	if string(data) != "hello saved image" {
		t.Fatalf("/etc/motd = %q, want saved contents", data)
	}
	link, err := imagefs.LookupPath(img.RootFS, "/bin/motd")
	if err != nil {
		t.Fatalf("LookupPath(/bin/motd) error = %v", err)
	}
	if link.Symlink == nil || filepath.ToSlash(link.Symlink.Target()) != "../etc/motd" {
		t.Fatalf("/bin/motd = %#v, want symlink to ../etc/motd", link)
	}
	for _, excluded := range []string{"/tmp/drop", "/proc/drop", "/host/drop", "/.ccx3/drop"} {
		if _, err := imagefs.LookupPath(img.RootFS, excluded); err == nil {
			t.Fatalf("LookupPath(%s) succeeded, want excluded", excluded)
		}
	}
}

func TestStoreDeleteRemovesImage(t *testing.T) {
	store := NewStore(t.TempDir())
	root := imagefs.NewHostFS(t.TempDir(), nil)
	if _, err := store.SaveRootFS(context.Background(), "saved", root, SaveOptions{Source: "vm:work"}); err != nil {
		t.Fatalf("SaveRootFS() error = %v", err)
	}
	if _, err := store.Get("saved"); err != nil {
		t.Fatalf("Get(saved) before delete error = %v", err)
	}
	if err := store.Delete("saved"); err != nil {
		t.Fatalf("Delete(saved) error = %v", err)
	}
	if _, err := store.Get("saved"); err == nil {
		t.Fatal("Get(saved) after delete error = nil, want missing image")
	}
}

func TestStoreSaveRootFSRejectsPathTraversalName(t *testing.T) {
	store := NewStore(t.TempDir())
	root := imagefs.NewHostFS(t.TempDir(), nil)
	for _, name := range []string{"../escape", "/escape", "nested/../escape"} {
		if _, err := store.SaveRootFS(context.Background(), name, root, SaveOptions{}); err == nil {
			t.Fatalf("SaveRootFS(%q) error = nil, want rejection", name)
		}
	}
}

func TestStoreDeleteRejectsPathTraversalName(t *testing.T) {
	store := NewStore(t.TempDir())
	for _, name := range []string{"../escape", "/escape", "nested/../escape"} {
		if err := store.Delete(name); err == nil {
			t.Fatalf("Delete(%q) error = nil, want rejection", name)
		}
	}
}

func TestStorePullReportsUnsupportedSourceKinds(t *testing.T) {
	store := NewStore(t.TempDir())
	tests := []struct {
		source string
		want   []string
	}{
		{source: "/tmp/tool.simg", want: []string{"stat simg source:"}},
		{source: "/cvmfs/test.repo/containers/tool", want: []string{"read cvmfs container directory:", "/cvmfs/test.repo/containers/tool"}},
	}
	for _, tt := range tests {
		_, err := store.Pull(context.Background(), "test", tt.source)
		if err == nil {
			t.Fatalf("Pull(%q) error = nil, want error", tt.source)
		}
		for _, want := range tt.want {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("Pull(%q) error = %q, want substring %q", tt.source, err.Error(), want)
			}
		}
		if !errors.Is(err, context.Canceled) && store.lastErr["test"] == nil {
			t.Fatalf("store.lastErr[test] was not recorded")
		}
	}
}

func TestStorePullImportsDirectoryBackedCVMFSContainer(t *testing.T) {
	server := newOCICVMFSDirectoryRepoServer(t)
	store, sharedCache := newStoreWithTempSharedCache(t)
	store.httpClient = server.Client()

	source := server.URL + "/cvmfs/test.repo/containers/niimath_1.0.20250804_20251016"
	state, err := store.Pull(context.Background(), "niimath", source)
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if state.SourceKind != SourceKindCVMFS {
		t.Fatalf("Pull().SourceKind = %q, want %q", state.SourceKind, SourceKindCVMFS)
	}
	assertCVMFSCachePopulated(t, sharedCache)

	img, err := store.Open("niimath")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if img.Architecture != "amd64" {
		t.Fatalf("img.Architecture = %q, want amd64", img.Architecture)
	}
	entry, err := imagefs.LookupPath(img.RootFS, "/etc/alpine-release")
	if err != nil {
		t.Fatalf("LookupPath(/etc/alpine-release) error = %v", err)
	}
	data, err := entry.File.ReadAt(0, 64)
	if err != nil {
		t.Fatalf("ReadAt(/etc/alpine-release) error = %v", err)
	}
	if string(data) != "3.20.0\n" {
		t.Fatalf("/etc/alpine-release = %q, want %q", string(data), "3.20.0\n")
	}
}

func TestStorePullPrefetchesDirectoryBackedCVMFSIntoFileCache(t *testing.T) {
	server := newOCICVMFSDirectoryRepoServer(t)
	store, sharedCache := newStoreWithTempSharedCache(t)
	store.httpClient = server.Client()
	var events []client.ProgressEvent

	source := server.URL + "/cvmfs/test.repo/containers/niimath_1.0.20250804_20251016"
	_, err := store.Pull(context.Background(), "niimath", source, PullOptions{
		Prefetch:        true,
		PrefetchWorkers: 2,
		Report: func(event client.ProgressEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}

	meta, err := store.readMetadata("niimath")
	if err != nil {
		t.Fatalf("readMetadata() error = %v", err)
	}
	if meta.PackedContentsPath != "" {
		t.Fatalf("PackedContentsPath = %q, want empty", meta.PackedContentsPath)
	}
	if _, err := os.Stat(filepath.Join(meta.RootFSDir, "rootfs.contents")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rootfs.contents exists after cache prefetch: %v", err)
	}
	indexBuf, err := os.ReadFile(meta.IndexPath)
	if err != nil {
		t.Fatalf("ReadFile(index) error = %v", err)
	}
	nodes, err := decodeFSIndex(indexBuf)
	if err != nil {
		t.Fatalf("decodeFSIndex() error = %v", err)
	}
	for _, node := range nodes {
		if node.Packed || node.PackedOffset != 0 {
			t.Fatalf("node %q was packed after cache prefetch: %#v", node.Path, node)
		}
	}

	var cachedFiles int
	cacheRoot := filepath.Join(cvmfsCacheDir(sharedCache), "files", "test.repo")
	if err := filepath.WalkDir(cacheRoot, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			cachedFiles++
		}
		return nil
	}); err != nil {
		t.Fatalf("WalkDir(%q) error = %v", cacheRoot, err)
	}
	if cachedFiles == 0 {
		t.Fatal("prefetch did not populate CVMFS file cache")
	}

	var sawCacheProgress bool
	for _, event := range events {
		if event.Blob == "cvmfs cache" && event.FilesTotal > 0 && event.BytesTotal > 0 {
			sawCacheProgress = true
			break
		}
	}
	if !sawCacheProgress {
		t.Fatalf("prefetch progress did not describe CVMFS cache fill: %#v", events)
	}

	events = nil
	_, err = store.Pull(context.Background(), "niimath", source, PullOptions{
		Prefetch:        true,
		PrefetchWorkers: 2,
		Report: func(event client.ProgressEvent) {
			events = append(events, event)
		},
	})
	if err != nil {
		t.Fatalf("second Pull() error = %v", err)
	}
	if len(events) == 0 {
		t.Fatal("second prefetch did not report completion")
	}
	var sawNoMissing bool
	for _, event := range events {
		if event.Blob == "cvmfs cache" && event.FilesTotal == 0 && event.BytesTotal == 0 {
			sawNoMissing = true
			break
		}
	}
	if !sawNoMissing {
		t.Fatalf("second prefetch progress = %#v, want no missing files", events)
	}
}

func TestStorePullCVMFSUsesFreshTemporaryCache(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "mirror unavailable", http.StatusBadGateway)
	}))
	defer server.Close()

	store, sharedCache := newStoreWithTempSharedCache(t)
	store.httpClient = server.Client()

	source := server.URL + "/cvmfs/test.repo/containers/niimath_1.0.20250804_20251016"
	_, err := store.Pull(context.Background(), "niimath", source)
	if err == nil {
		t.Fatal("Pull() error = nil, want temporary-cache miss to surface mirror failure")
	}
	if !strings.Contains(err.Error(), "read cvmfs container directory: fetch manifest: unexpected status 502 Bad Gateway") {
		t.Fatalf("Pull() error = %q, want fresh-cache mirror failure", err.Error())
	}

	manifestPath := filepath.Join(cvmfsCacheDir(sharedCache), "state", "test.repo", "manifest")
	if _, statErr := os.Stat(manifestPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("unexpected manifest cache at %q after failed pull: %v", manifestPath, statErr)
	}
}

func TestStorePullImportsInnerSIMGDirectoryBackedCVMFSContainer(t *testing.T) {
	server := newOCICVMFSDirectoryRepoServer(t)
	store, sharedCache := newStoreWithTempSharedCache(t)
	store.httpClient = server.Client()

	source := server.URL + "/cvmfs/test.repo/containers/niimath_1.0.20250804_20251016/niimath_1.0.20250804_20251016.simg"
	state, err := store.Pull(context.Background(), "niimath", source)
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if state.SourceKind != SourceKindCVMFS {
		t.Fatalf("Pull().SourceKind = %q, want %q", state.SourceKind, SourceKindCVMFS)
	}
	assertCVMFSCachePopulated(t, sharedCache)

	img, err := store.Open("niimath")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if img.Architecture != "amd64" {
		t.Fatalf("img.Architecture = %q, want amd64", img.Architecture)
	}
	entry, err := imagefs.LookupPath(img.RootFS, "/etc/alpine-release")
	if err != nil {
		t.Fatalf("LookupPath(/etc/alpine-release) error = %v", err)
	}
	data, err := entry.File.ReadAt(0, 64)
	if err != nil {
		t.Fatalf("ReadAt(/etc/alpine-release) error = %v", err)
	}
	if string(data) != "3.20.0\n" {
		t.Fatalf("/etc/alpine-release = %q, want %q", string(data), "3.20.0\n")
	}
}

func TestCVMFSDirectoryIndexCacheRoundTrips(t *testing.T) {
	cacheDir := t.TempDir()
	nodes := []indexedNode{
		{Path: "/", Kind: indexedKindDir, Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDir | 0o755)},
		{Path: "/bin", Kind: indexedKindDir, Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDir | 0o755)},
		{Path: "/bin/sh", Kind: indexedKindFile, Mode: fsmeta.LinuxModeFromFileMode(0o755), Size: 7, CVMFSTarget: "https://example.invalid/cvmfs/test.repo/bin/sh"},
	}
	entries := map[string]fsmeta.Entry{
		"/":       {UID: 0, GID: 0, Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDir | 0o755)},
		"/bin":    {UID: 0, GID: 0, Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDir | 0o755)},
		"/bin/sh": {UID: 0, GID: 0, Mode: fsmeta.LinuxModeFromFileMode(0o755)},
	}
	if err := saveCVMFSDirectoryIndexCache(cacheDir, "abc123", "https://example.invalid/cvmfs/test.repo/container", nodes, entries, "amd64"); err != nil {
		t.Fatalf("saveCVMFSDirectoryIndexCache() error = %v", err)
	}
	gotNodes, gotEntries, gotArch, ok, err := loadCVMFSDirectoryIndexCache(cacheDir, "abc123", "https://example.invalid/cvmfs/test.repo/container")
	if err != nil {
		t.Fatalf("loadCVMFSDirectoryIndexCache() error = %v", err)
	}
	if !ok {
		t.Fatal("loadCVMFSDirectoryIndexCache() ok = false, want true")
	}
	if gotArch != "amd64" {
		t.Fatalf("loaded arch = %q, want amd64", gotArch)
	}
	if len(gotNodes) != len(nodes) {
		t.Fatalf("loaded %d nodes, want %d", len(gotNodes), len(nodes))
	}
	if gotNodes[2].CVMFSTarget != nodes[2].CVMFSTarget {
		t.Fatalf("loaded file target = %q, want %q", gotNodes[2].CVMFSTarget, nodes[2].CVMFSTarget)
	}
	if gotEntries["/bin/sh"].Mode != entries["/bin/sh"].Mode {
		t.Fatalf("loaded mode = %#o, want %#o", gotEntries["/bin/sh"].Mode, entries["/bin/sh"].Mode)
	}
}

func TestBuildCVMFSIndexedRootFSReadsPackedContents(t *testing.T) {
	contentsPath := filepath.Join(t.TempDir(), "rootfs.contents")
	payload := []byte("hello world")
	if err := os.WriteFile(contentsPath, payload, 0o644); err != nil {
		t.Fatalf("WriteFile(contents) error = %v", err)
	}
	nodes := []indexedNode{
		{Path: "/", Kind: indexedKindDir, Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDir | 0o755)},
		{Path: "/bin", Kind: indexedKindDir, Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDir | 0o755)},
		{
			Path:         "/bin/sh",
			Kind:         indexedKindFile,
			Mode:         fsmeta.LinuxModeFromFileMode(0o755),
			Size:         uint64(len(payload)),
			CVMFSTarget:  "https://example.invalid/cvmfs/test.repo/bin/sh",
			Packed:       true,
			PackedOffset: 0,
		},
	}
	rootFS, err := buildCVMFSIndexedRootFS(nil, contentsPath, nodes)
	if err != nil {
		t.Fatalf("buildCVMFSIndexedRootFS() error = %v", err)
	}
	entry, err := imagefs.LookupPath(rootFS, "/bin/sh")
	if err != nil {
		t.Fatalf("LookupPath(/bin/sh) error = %v", err)
	}
	data, err := entry.File.ReadAt(0, 64)
	if err != nil {
		t.Fatalf("ReadAt(/bin/sh) error = %v", err)
	}
	if string(data) != string(payload) {
		t.Fatalf("/bin/sh = %q, want %q", string(data), string(payload))
	}
}

func TestStorePullRefreshesDirectoryBackedCVMFSWhenManifestChanges(t *testing.T) {
	rootCatalogHashA := strings.Repeat("a", 40)
	rootCatalogHashB := strings.Repeat("f", 40)
	nestedCatalogHash := strings.Repeat("b", 40)
	commandsHash := strings.Repeat("c", 40)
	releaseHash := strings.Repeat("d", 40)
	shHash := strings.Repeat("e", 40)
	rootCatalog := createOCITestCatalogDB(t, []string{
		`CREATE TABLE catalog (md5path_1 INTEGER, md5path_2 INTEGER, parent_1 INTEGER, parent_2 INTEGER, hardlinks INTEGER, hash BLOB, size INTEGER, mode INTEGER, mtime INTEGER, mtimens INTEGER, flags INTEGER, name TEXT, symlink TEXT, uid INTEGER, gid INTEGER, xattr BLOB);`,
		`CREATE TABLE nested_catalogs (path TEXT, sha1 TEXT, size INTEGER);`,
		`INSERT INTO catalog VALUES (1, 1, 0, 0, 1, NULL, 0, 16877, 0, 0, 1, 'containers', '', 0, 0, NULL);`,
		`INSERT INTO nested_catalogs VALUES ('containers/niimath_1.0.20250804_20251016', '` + nestedCatalogHash + `', 0);`,
	})
	nestedCatalog := createOCITestCatalogDB(t, []string{
		`CREATE TABLE catalog (md5path_1 INTEGER, md5path_2 INTEGER, parent_1 INTEGER, parent_2 INTEGER, hardlinks INTEGER, hash BLOB, size INTEGER, mode INTEGER, mtime INTEGER, mtimens INTEGER, flags INTEGER, name TEXT, symlink TEXT, uid INTEGER, gid INTEGER, xattr BLOB);`,
		`INSERT INTO catalog VALUES (2, 2, 1, 1, 1, NULL, 0, 16877, 0, 0, 1, 'niimath_1.0.20250804_20251016', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (3, 3, 2, 2, 1, X'` + strings.ToUpper(commandsHash) + `', 8, 33188, 0, 0, 4, 'commands.txt', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (4, 4, 2, 2, 1, NULL, 0, 16877, 0, 0, 1, 'niimath_1.0.20250804_20251016.simg', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (5, 5, 4, 4, 1, NULL, 0, 16877, 0, 0, 1, 'etc', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (6, 6, 5, 5, 1, X'` + strings.ToUpper(releaseHash) + `', 7, 33188, 0, 0, 4, 'alpine-release', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (7, 7, 4, 4, 1, NULL, 0, 16877, 0, 0, 1, 'bin', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (8, 8, 7, 7, 1, X'` + strings.ToUpper(shHash) + `', 64, 33261, 0, 0, 4, 'sh', '', 0, 0, NULL);`,
	})

	shData := make([]byte, 64)
	copy(shData, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0})
	shData[18] = 0x3e
	shData[19] = 0x00

	var currentRootHash atomic.Value
	currentRootHash.Store(rootCatalogHashA)

	objects := map[string][]byte{
		ociObjectPath(rootCatalogHashA, "C"):  compressOCIObject(t, rootCatalog),
		ociObjectPath(rootCatalogHashB, "C"):  compressOCIObject(t, rootCatalog),
		ociObjectPath(nestedCatalogHash, "C"): compressOCIObject(t, nestedCatalog),
		ociObjectPath(commandsHash, ""):       compressOCIObject(t, []byte("niimath\n")),
		ociObjectPath(releaseHash, ""):        compressOCIObject(t, []byte("3.20.0\n")),
		ociObjectPath(shHash, ""):             compressOCIObject(t, shData),
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/cvmfs/test.repo/.cvmfspublished" {
			_, _ = w.Write([]byte("C" + currentRootHash.Load().(string) + "\nNtest.repo\n--\n"))
			return
		}
		body, ok := objects[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	defer server.Close()

	store, sharedCache := newStoreWithTempSharedCache(t)
	store.httpClient = server.Client()
	source := server.URL + "/cvmfs/test.repo/containers/niimath_1.0.20250804_20251016"

	if _, err := store.Pull(context.Background(), "niimath", source); err != nil {
		t.Fatalf("first Pull() error = %v", err)
	}
	meta, err := store.readMetadata("niimath")
	if err != nil {
		t.Fatalf("readMetadata(first) error = %v", err)
	}
	if meta.CVMFSRootHash != rootCatalogHashA {
		t.Fatalf("first CVMFSRootHash = %q, want %q", meta.CVMFSRootHash, rootCatalogHashA)
	}
	assertCVMFSCachePopulated(t, sharedCache)

	currentRootHash.Store(rootCatalogHashB)
	if _, err := store.Pull(context.Background(), "niimath", source); err != nil {
		t.Fatalf("second Pull() error = %v", err)
	}
	meta, err = store.readMetadata("niimath")
	if err != nil {
		t.Fatalf("readMetadata(second) error = %v", err)
	}
	if meta.CVMFSRootHash != rootCatalogHashB {
		t.Fatalf("second CVMFSRootHash = %q, want %q", meta.CVMFSRootHash, rootCatalogHashB)
	}
}

func newOCICVMFSDirectoryRepoServer(t *testing.T) *httptest.Server {
	t.Helper()

	rootCatalogHash := strings.Repeat("a", 40)
	nestedCatalogHash := strings.Repeat("b", 40)
	commandsHash := strings.Repeat("c", 40)
	releaseHash := strings.Repeat("d", 40)
	shHash := strings.Repeat("e", 40)

	rootCatalog := createOCITestCatalogDB(t, []string{
		`CREATE TABLE catalog (md5path_1 INTEGER, md5path_2 INTEGER, parent_1 INTEGER, parent_2 INTEGER, hardlinks INTEGER, hash BLOB, size INTEGER, mode INTEGER, mtime INTEGER, mtimens INTEGER, flags INTEGER, name TEXT, symlink TEXT, uid INTEGER, gid INTEGER, xattr BLOB);`,
		`CREATE TABLE nested_catalogs (path TEXT, sha1 TEXT, size INTEGER);`,
		`INSERT INTO catalog VALUES (1, 1, 0, 0, 1, NULL, 0, 16877, 0, 0, 1, 'containers', '', 0, 0, NULL);`,
		`INSERT INTO nested_catalogs VALUES ('containers/niimath_1.0.20250804_20251016', '` + nestedCatalogHash + `', 0);`,
	})
	nestedCatalog := createOCITestCatalogDB(t, []string{
		`CREATE TABLE catalog (md5path_1 INTEGER, md5path_2 INTEGER, parent_1 INTEGER, parent_2 INTEGER, hardlinks INTEGER, hash BLOB, size INTEGER, mode INTEGER, mtime INTEGER, mtimens INTEGER, flags INTEGER, name TEXT, symlink TEXT, uid INTEGER, gid INTEGER, xattr BLOB);`,
		`INSERT INTO catalog VALUES (2, 2, 1, 1, 1, NULL, 0, 16877, 0, 0, 1, 'niimath_1.0.20250804_20251016', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (3, 3, 2, 2, 1, X'` + strings.ToUpper(commandsHash) + `', 8, 33188, 0, 0, 4, 'commands.txt', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (4, 4, 2, 2, 1, NULL, 0, 16877, 0, 0, 1, 'niimath_1.0.20250804_20251016.simg', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (5, 5, 4, 4, 1, NULL, 0, 16877, 0, 0, 1, 'etc', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (6, 6, 5, 5, 1, X'` + strings.ToUpper(releaseHash) + `', 7, 33188, 0, 0, 4, 'alpine-release', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (7, 7, 4, 4, 1, NULL, 0, 16877, 0, 0, 1, 'bin', '', 0, 0, NULL);`,
		`INSERT INTO catalog VALUES (8, 8, 7, 7, 1, X'` + strings.ToUpper(shHash) + `', 64, 33261, 0, 0, 4, 'sh', '', 0, 0, NULL);`,
	})

	shData := make([]byte, 64)
	copy(shData, []byte{0x7f, 'E', 'L', 'F', 2, 1, 1, 0})
	shData[18] = 0x3e
	shData[19] = 0x00

	objects := map[string][]byte{
		"/cvmfs/test.repo/.cvmfspublished":    []byte("C" + rootCatalogHash + "\nNtest.repo\n--\n"),
		ociObjectPath(rootCatalogHash, "C"):   compressOCIObject(t, rootCatalog),
		ociObjectPath(nestedCatalogHash, "C"): compressOCIObject(t, nestedCatalog),
		ociObjectPath(commandsHash, ""):       compressOCIObject(t, []byte("niimath\n")),
		ociObjectPath(releaseHash, ""):        compressOCIObject(t, []byte("3.20.0\n")),
		ociObjectPath(shHash, ""):             compressOCIObject(t, shData),
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := objects[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(body)
	}))
	t.Cleanup(ts.Close)
	return ts
}

func compressOCIObject(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zlib.NewWriter(&buf)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func ociObjectPath(hash, suffix string) string {
	return "/cvmfs/test.repo/data/" + hash[:2] + "/" + hash[2:] + suffix
}

func createOCITestCatalogDB(t *testing.T, statements []string) []byte {
	t.Helper()
	if _, err := exec.LookPath("sqlite3"); err != nil {
		t.Skip("sqlite3 not available")
	}
	file, err := os.CreateTemp("", "ccx3-oci-cvmfs-test-*.sqlite")
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	t.Cleanup(func() { _ = os.Remove(file.Name()) })
	cmd := exec.Command("sqlite3", file.Name())
	cmd.Stdin = strings.NewReader(strings.Join(statements, "\n"))
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("sqlite3: %v\n%s", err, out)
	}
	data, err := os.ReadFile(file.Name())
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func newStoreWithTempSharedCache(t *testing.T) (*Store, string) {
	t.Helper()

	sharedCache := t.TempDir()
	t.Setenv(sharedCacheEnv, sharedCache)
	return NewStore(t.TempDir()), sharedCache
}

func assertCVMFSCachePopulated(t *testing.T, sharedCache string) {
	t.Helper()

	cvmfsCache := cvmfsCacheDir(sharedCache)
	manifestPath := filepath.Join(cvmfsCache, "state", "test.repo", "manifest")
	if _, err := os.Stat(manifestPath); err != nil {
		t.Fatalf("CVMFS manifest cache was not written under temp cache %q: %v", cvmfsCache, err)
	}
	objectsPath := filepath.Join(cvmfsCache, "objects")
	entries, err := os.ReadDir(objectsPath)
	if err != nil {
		t.Fatalf("CVMFS object cache was not written under temp cache %q: %v", cvmfsCache, err)
	}
	if len(entries) == 0 {
		t.Fatalf("CVMFS object cache %q is empty", objectsPath)
	}
}

func itoa(v int) string {
	return strconv.Itoa(v)
}

type tarEntry struct {
	Data     []byte
	Typeflag byte
	Linkname string
	Mode     int64
}

type fataler interface {
	Helper()
	Fatalf(string, ...any)
	TempDir() string
}

func gzipTar(t fataler, entries map[string]tarEntry) []byte {
	t.Helper()

	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gzw)
	for name, entry := range entries {
		typeflag := entry.Typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		hdr := &tar.Header{
			Name:     name,
			Typeflag: typeflag,
			Linkname: entry.Linkname,
			Mode:     entry.Mode,
			Size:     int64(len(entry.Data)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader(%s) error = %v", name, err)
		}
		if len(entry.Data) > 0 {
			if _, err := tw.Write(entry.Data); err != nil {
				t.Fatalf("Write(%s) error = %v", name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close() error = %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip Close() error = %v", err)
	}
	return buf.Bytes()
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func plainTar(t fataler, entries map[string]tarEntry) []byte {
	t.Helper()

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for name, entry := range entries {
		typeflag := entry.Typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		hdr := &tar.Header{
			Name:     name,
			Typeflag: typeflag,
			Linkname: entry.Linkname,
			Mode:     entry.Mode,
			Size:     int64(len(entry.Data)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("WriteHeader(%s) error = %v", name, err)
		}
		if len(entry.Data) > 0 {
			if _, err := tw.Write(entry.Data); err != nil {
				t.Fatalf("Write(%s) error = %v", name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar Close() error = %v", err)
	}
	return buf.Bytes()
}

type dockerArchiveFixtureImage struct {
	ConfigName string
	Config     []byte
	RepoTags   []string
	Layers     []dockerArchiveFixtureLayer
}

type dockerArchiveFixtureLayer struct {
	Name string
	Data []byte
}

func dockerArchive(t fataler, images []dockerArchiveFixtureImage) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "image.tar")
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Create(image.tar) error = %v", err)
	}
	tw := tar.NewWriter(file)
	var manifest []map[string]any
	for _, image := range images {
		layerNames := make([]string, 0, len(image.Layers))
		for _, layer := range image.Layers {
			layerNames = append(layerNames, layer.Name)
		}
		manifest = append(manifest, map[string]any{
			"Config":   image.ConfigName,
			"RepoTags": image.RepoTags,
			"Layers":   layerNames,
		})
	}
	manifestBlob, err := json.Marshal(manifest)
	if err != nil {
		t.Fatalf("Marshal(docker manifest) error = %v", err)
	}
	writeTarFile(t, tw, "manifest.json", manifestBlob)
	for _, image := range images {
		writeTarFile(t, tw, image.ConfigName, image.Config)
		for _, layer := range image.Layers {
			writeTarFile(t, tw, layer.Name, layer.Data)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("docker archive tar Close() error = %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("docker archive file Close() error = %v", err)
	}
	return path
}

func writeTarFile(t fataler, tw *tar.Writer, name string, data []byte) {
	t.Helper()

	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     int64(len(data)),
	}); err != nil {
		t.Fatalf("WriteHeader(%s) error = %v", name, err)
	}
	if len(data) > 0 {
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("Write(%s) error = %v", name, err)
		}
	}
}

func dockerImageConfigBlob(t fataler, cmd string) []byte {
	t.Helper()

	blob, err := json.Marshal(map[string]any{
		"architecture": nativeArch(),
		"config": map[string]any{
			"Env": []string{"PATH=/usr/bin:/bin"},
			"Cmd": []string{cmd},
		},
	})
	if err != nil {
		t.Fatalf("Marshal(config) error = %v", err)
	}
	return blob
}
