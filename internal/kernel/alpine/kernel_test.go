package alpine

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

func TestManagerPlanModuleLoad(t *testing.T) {
	pkgPath := filepath.Join(t.TempDir(), "linux-virt-test.apk")
	if err := os.WriteFile(pkgPath, buildKernelPackage(t, map[string][]byte{
		"boot/config-6.18.22-0-virt": []byte(strings.Join([]string{
			"CONFIG_MODULES=y",
			"CONFIG_VIRTIO_MMIO=m",
			"CONFIG_FUSE_FS=m",
			"CONFIG_VIRTIO_FS=m",
			"",
		}, "\n")),
		"lib/modules/6.18.22-0-virt/modules.dep": []byte(strings.Join([]string{
			"kernel/fs/fuse/fuse.ko.gz:",
			"kernel/fs/fuse/virtiofs.ko.gz: kernel/fs/fuse/fuse.ko.gz",
			"kernel/drivers/virtio/virtio_mmio.ko.gz:",
			"",
		}, "\n")),
		"lib/modules/6.18.22-0-virt/kernel/fs/fuse/fuse.ko.gz":               gzipBytes(t, []byte("fuse module")),
		"lib/modules/6.18.22-0-virt/kernel/fs/fuse/virtiofs.ko.gz":           gzipBytes(t, []byte("virtiofs module")),
		"lib/modules/6.18.22-0-virt/kernel/drivers/virtio/virtio_mmio.ko.gz": gzipBytes(t, []byte("virtio_mmio module")),
	}), 0o644); err != nil {
		t.Fatalf("WriteFile(package) error = %v", err)
	}

	mgr := NewManager(t.TempDir())
	if err := os.MkdirAll(mgr.root, 0o755); err != nil {
		t.Fatalf("MkdirAll(root) error = %v", err)
	}
	metaBuf, err := json.Marshal(metadata{
		Version:     "6.18.22-r0",
		Source:      "test",
		PackageName: "linux-virt",
		PackageFile: pkgPath,
		Arch:        "aarch64",
	})
	if err != nil {
		t.Fatalf("Marshal(metadata) error = %v", err)
	}
	if err := os.WriteFile(mgr.metadataPath(), metaBuf, 0o644); err != nil {
		t.Fatalf("WriteFile(metadata) error = %v", err)
	}

	version, err := mgr.KernelVersion()
	if err != nil {
		t.Fatalf("KernelVersion() error = %v", err)
	}
	if version != "6.18.22-0-virt" {
		t.Fatalf("KernelVersion() = %q, want 6.18.22-0-virt", version)
	}

	modules, err := mgr.PlanModuleLoad(
		[]string{"CONFIG_VIRTIO_MMIO", "CONFIG_VIRTIO_FS"},
		map[string]string{
			"CONFIG_VIRTIO_MMIO": "kernel/drivers/virtio/virtio_mmio.ko.gz",
			"CONFIG_VIRTIO_FS":   "kernel/fs/fuse/virtiofs.ko.gz",
		},
	)
	if err != nil {
		t.Fatalf("PlanModuleLoad() error = %v", err)
	}
	got := make([]string, 0, len(modules))
	for _, mod := range modules {
		got = append(got, mod.Name)
	}
	want := []string{"virtio_mmio", "fuse", "virtiofs"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("PlanModuleLoad() names = %v, want %v", got, want)
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

func buildKernelPackage(t *testing.T, files map[string][]byte) []byte {
	t.Helper()

	var pkg bytes.Buffer
	gzw := gzip.NewWriter(&pkg)
	tw := tar.NewWriter(gzw)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(data)),
		}); err != nil {
			t.Fatalf("WriteHeader(%s) error = %v", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("Write(%s) error = %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close error = %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip close error = %v", err)
	}
	return pkg.Bytes()
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gzw := gzip.NewWriter(&buf)
	if _, err := gzw.Write(data); err != nil {
		t.Fatalf("gzip write error = %v", err)
	}
	if err := gzw.Close(); err != nil {
		t.Fatalf("gzip close error = %v", err)
	}
	return buf.Bytes()
}
