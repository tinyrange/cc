//go:build darwin && arm64

package vm

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"testing"

	"j5.nz/cc/internal/kernel/alpine"
	"j5.nz/cc/internal/oci"
)

func TestWorkerBootBundleReadsCoordinatorSocket(t *testing.T) {
	tmp, err := os.MkdirTemp("/tmp", "vmsh-boot-bundle-test.")
	if err != nil {
		t.Fatalf("MkdirTemp() error = %v", err)
	}
	defer os.RemoveAll(tmp)
	socketPath := filepath.Join(tmp, "boot.sock")
	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer ln.Close()
	want := sidecarBootBundle{
		ImageName:         "alpine",
		Architecture:      "arm64",
		Config:            oci.RuntimeConfig{Env: []string{"PATH=/bin"}, WorkingDir: "/work"},
		Kernel:            []byte("kernel"),
		Init:              []byte("init"),
		AMD64EmulatorPath: "/ccx3/qemu-x86_64",
		Modules: []alpine.Module{
			{Name: "virtio_mmio", Data: []byte("module-one")},
			{Name: "virtiofs", Data: []byte("module-two")},
		},
	}
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = writeSidecarBootBundle(conn, want)
	}()
	t.Setenv(sidecarBootSocketEnv, socketPath)

	got, err := workerBootBundle()
	if err != nil {
		t.Fatalf("workerBootBundle() error = %v", err)
	}
	if got == nil {
		t.Fatal("workerBootBundle() = nil, want bundle")
	}
	if got.ImageName != want.ImageName || got.Architecture != want.Architecture || got.Config.WorkingDir != "/work" || string(got.Kernel) != "kernel" || string(got.Init) != "init" || got.AMD64EmulatorPath != want.AMD64EmulatorPath {
		t.Fatalf("workerBootBundle() = %#v, want %#v", got, want)
	}
	if len(got.Modules) != 2 || got.Modules[0].Name != "virtio_mmio" || string(got.Modules[0].Data) != "module-one" || got.Modules[1].Name != "virtiofs" || string(got.Modules[1].Data) != "module-two" {
		t.Fatalf("modules = %#v, want ordered module payloads", got.Modules)
	}
}

func TestSidecarBootBundleTLVDoesNotBase64EncodeBinariesInMetadata(t *testing.T) {
	var buf bytes.Buffer
	bundle := sidecarBootBundle{
		ImageName: "alpine",
		Kernel:    []byte("kernel-binary"),
		Init:      []byte("init-binary"),
		Modules:   []alpine.Module{{Name: "virtio_mmio", Data: []byte("module-binary")}},
	}
	if err := writeSidecarBootBundle(&buf, bundle); err != nil {
		t.Fatalf("writeSidecarBootBundle() error = %v", err)
	}
	raw := buf.Bytes()
	if bytes.Contains(raw, []byte("a2VybmVsLWJpbmFyeQ==")) || bytes.Contains(raw, []byte("aW5pdC1iaW5hcnk=")) || bytes.Contains(raw, []byte("bW9kdWxlLWJpbmFyeQ==")) {
		t.Fatalf("boot bundle wire payload contains base64-encoded binary data: %q", raw)
	}
	got, err := readSidecarBootBundle(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("readSidecarBootBundle() error = %v", err)
	}
	if string(got.Kernel) != "kernel-binary" || string(got.Init) != "init-binary" || len(got.Modules) != 1 || string(got.Modules[0].Data) != "module-binary" {
		t.Fatalf("read bundle = %#v, want raw binary payloads", got)
	}
}

func TestWorkerBootBundleAbsent(t *testing.T) {
	t.Setenv(sidecarBootSocketEnv, "")
	got, err := workerBootBundle()
	if err != nil {
		t.Fatalf("workerBootBundle() error = %v", err)
	}
	if got != nil {
		t.Fatalf("workerBootBundle() = %#v, want nil", got)
	}
}

func TestCleanupStaleSidecarSocketsMissingDir(t *testing.T) {
	cleanupStaleSidecarSockets(filepath.Join(os.TempDir(), "vmsh-missing-sidecar-dir"))
}
