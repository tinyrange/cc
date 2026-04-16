package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

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
		"architecture": "arm64",
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
					"platform":  map[string]any{"os": "linux", "architecture": "arm64"},
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
		"architecture": "arm64",
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
					"platform":  map[string]any{"os": "linux", "architecture": "arm64"},
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
	fixture := filepath.Join("..", "..", "local", "alpine.simg")
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
		{source: "http+cvmfs://cvmfs.neurodesk.org/cvmfs/neurodesk.ardc.edu.au?path=containers/tool", want: "cvmfs image ingestion is not implemented yet"},
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
