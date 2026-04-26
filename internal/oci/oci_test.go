package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
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
	if err := os.WriteFile(filepath.Join(envDir, "90-environment.sh"), []byte(`
export PATH="/usr/local/bin:/usr/bin:/bin"
export TOOLBOX_PATH="/opt/bart-0.9.00/"
export SKIP_DYNAMIC="${PATH:-/ignored}"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(90-environment.sh) error = %v", err)
	}

	meta := extractSIMGDeployMetadata(imagefs.NewHostFS(root, nil))
	env := strings.Join(meta.Env, "\n")
	for _, want := range []string{
		"DEPLOY_PATH=/opt/bart-0.9.00/",
		"DEPLOY_BINS=bart:bart-view",
		"TOOLBOX_PATH=/opt/bart-0.9.00/",
		"PATH=/opt/bart-0.9.00/:/usr/local/bin:/usr/bin:/bin",
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

func TestStorePullReportsUnsupportedSourceKinds(t *testing.T) {
	store := NewStore(t.TempDir())
	tests := []struct {
		source string
		want   string
	}{
		{source: "/tmp/tool.simg", want: "stat simg source:"},
		{source: "http+cvmfs://cvmfs.neurodesk.org/cvmfs/neurodesk.ardc.edu.au?path=containers/tool", want: "read cvmfs container directory: file does not exist"},
	}
	for _, tt := range tests {
		_, err := store.Pull(context.Background(), "test", tt.source)
		if err == nil {
			t.Fatalf("Pull(%q) error = nil, want error", tt.source)
		}
		if !strings.Contains(err.Error(), tt.want) {
			t.Fatalf("Pull(%q) error = %q, want substring %q", tt.source, err.Error(), tt.want)
		}
		if !errors.Is(err, context.Canceled) && store.lastErr["test"] == nil {
			t.Fatalf("store.lastErr[test] was not recorded")
		}
	}
}

func TestStorePullImportsDirectoryBackedCVMFSContainer(t *testing.T) {
	server := newOCICVMFSDirectoryRepoServer(t)
	store := NewStore(t.TempDir())
	store.httpClient = server.Client()

	source := server.URL + "/cvmfs/test.repo/containers/niimath_1.0.20250804_20251016"
	state, err := store.Pull(context.Background(), "niimath", source)
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if state.SourceKind != SourceKindCVMFS {
		t.Fatalf("Pull().SourceKind = %q, want %q", state.SourceKind, SourceKindCVMFS)
	}

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

func TestStorePullImportsInnerSIMGDirectoryBackedCVMFSContainer(t *testing.T) {
	server := newOCICVMFSDirectoryRepoServer(t)
	store := NewStore(t.TempDir())
	store.httpClient = server.Client()

	source := server.URL + "/cvmfs/test.repo/containers/niimath_1.0.20250804_20251016/niimath_1.0.20250804_20251016.simg"
	state, err := store.Pull(context.Background(), "niimath", source)
	if err != nil {
		t.Fatalf("Pull() error = %v", err)
	}
	if state.SourceKind != SourceKindCVMFS {
		t.Fatalf("Pull().SourceKind = %q, want %q", state.SourceKind, SourceKindCVMFS)
	}

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
	sharedCache := t.TempDir()
	if err := os.Setenv(sharedCacheEnv, sharedCache); err != nil {
		t.Fatalf("Setenv(%s) error = %v", sharedCacheEnv, err)
	}
	defer os.Unsetenv(sharedCacheEnv)

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

	store := NewStore(t.TempDir())
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

func itoa(v int) string {
	return strconv.Itoa(v)
}

type tarEntry struct {
	Data     []byte
	Typeflag byte
	Linkname string
	Mode     int64
}

func gzipTar(t *testing.T, entries map[string]tarEntry) []byte {
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
