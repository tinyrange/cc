//go:build guest

package main

import (
	"os"
	"syscall"
	"testing"
)

func TestHello(t *testing.T) {
	// This is a placeholder test.
}

func TestVirtioFs(t *testing.T) {
	// mount the virtio-fs filesystem and verify it works
	tmpDir := "/mnt/virtiofs"
	err := os.MkdirAll(tmpDir, 0755)
	if err != nil {
		t.Fatalf("failed to create mount point: %v", err)
	}

	err = syscall.Mount("bringup", tmpDir, "virtiofs", 0, "")
	if err != nil {
		t.Fatalf("failed to mount virtio-fs: %v", err)
	}
	defer syscall.Unmount(tmpDir, 0)

	// Check if we can read a known file from the mounted filesystem
	data, err := os.ReadFile(tmpDir + "/testfile.txt")
	if err != nil {
		t.Fatalf("failed to read test file from virtio-fs: %v", err)
	}

	expected := "Hello from virtio-fs!"
	if string(data) != expected {
		t.Fatalf("unexpected file content: got %q, want %q", string(data), expected)
	}
}
