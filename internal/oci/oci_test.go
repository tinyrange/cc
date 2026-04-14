package oci

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
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

	img, err := store.Open("alpine")
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	if img.Architecture != "arm64" {
		t.Fatalf("img.Architecture = %q, want arm64", img.Architecture)
	}
	if got := img.Command(nil); len(got) != 3 || got[0] != "/bin/sh" || got[2] != "echo default" {
		t.Fatalf("img.Command(nil) = %v", got)
	}
	if _, err := os.Stat(filepath.Join(img.RootFSDir, "etc", "obsolete")); !os.IsNotExist(err) {
		t.Fatalf("obsolete file still present, err = %v", err)
	}
	data, err := os.ReadFile(filepath.Join(img.RootFSDir, "etc", "hostname"))
	if err != nil {
		t.Fatalf("ReadFile(hostname) error = %v", err)
	}
	if string(data) != "ccx3" {
		t.Fatalf("hostname = %q, want ccx3", string(data))
	}
	target, err := os.Readlink(filepath.Join(img.RootFSDir, "bin", "sh"))
	if err != nil {
		t.Fatalf("Readlink(bin/sh) error = %v", err)
	}
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
	img, err := storeB.Open("alpine")
	if err != nil {
		t.Fatalf("Open() after shared-cache restore error = %v", err)
	}
	if _, err := os.Stat(img.RootFSDir); err != nil {
		t.Fatalf("restored cached rootfs missing root dir: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(img.RootFSDir, "bin", "busybox"))
	if err != nil {
		t.Fatalf("restored cached rootfs missing busybox: %v", err)
	}
	if string(data) != "busybox" {
		t.Fatalf("restored cached busybox = %q, want busybox", string(data))
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
