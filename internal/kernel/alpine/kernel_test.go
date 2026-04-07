package alpine

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManagerEnsureDownloadsKernelPackage(t *testing.T) {
	indexBytes := buildAPKIndexArchive(t, "linux-virt", "6.12.1-r0", "aarch64")
	apkBytes := []byte("fake kernel apk")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest-stable/main/aarch64/APKINDEX.tar.gz":
			w.Write(indexBytes)
		case "/latest-stable/main/aarch64/linux-virt-6.12.1-r0.apk":
			w.Write(apkBytes)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	root := t.TempDir()
	mgr := NewManager(root)
	mgr.mirror = srv.URL
	mgr.arch = "aarch64"
	mgr.httpClient = srv.Client()

	if err := mgr.Ensure(context.Background()); err != nil {
		t.Fatalf("Ensure() error = %v", err)
	}

	state := mgr.Status()
	if state.Status != "downloaded" {
		t.Fatalf("Status().Status = %q, want downloaded", state.Status)
	}
	if state.Version != "6.12.1-r0" {
		t.Fatalf("Status().Version = %q", state.Version)
	}
	if _, err := os.Stat(filepath.Join(root, "packages", "linux-virt-6.12.1-r0.apk")); err != nil {
		t.Fatalf("downloaded package missing: %v", err)
	}
}

func TestManagerStatusErrorAfterFailedEnsure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	mgr := NewManager(t.TempDir())
	mgr.mirror = srv.URL
	mgr.arch = "aarch64"
	mgr.httpClient = srv.Client()

	if err := mgr.Ensure(context.Background()); err == nil {
		t.Fatal("Ensure() error = nil, want error")
	}

	state := mgr.Status()
	if state.Status != "error" {
		t.Fatalf("Status().Status = %q, want error", state.Status)
	}
	if !strings.Contains(state.Error, "status 404") {
		t.Fatalf("Status().Error = %q", state.Error)
	}
}

func buildAPKIndexArchive(t *testing.T, pkgName, version, arch string) []byte {
	t.Helper()

	var index bytes.Buffer
	gzw := gzip.NewWriter(&index)
	tw := tar.NewWriter(gzw)

	contents := "P:" + pkgName + "\nV:" + version + "\nA:" + arch + "\n\n"
	hdr := &tar.Header{
		Name: "APKINDEX",
		Mode: 0o644,
		Size: int64(len(contents)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatalf("WriteHeader() error = %v", err)
	}
	if _, err := tw.Write([]byte(contents)); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close error = %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip close error = %v", err)
	}

	return index.Bytes()
}
