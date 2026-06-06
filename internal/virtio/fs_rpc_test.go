package virtio

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestFSRemoteBackendRead(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "hello.txt"), []byte("hello from coordinator"), 0o644); err != nil {
		t.Fatal(err)
	}
	remote, cleanup := newTestFSRemoteBackend(t, NewPassthroughFS(root, nil))
	defer cleanup()

	maxWrite, _ := remote.Init()
	if maxWrite == 0 {
		t.Fatal("Init().maxWrite = 0, want non-zero")
	}
	nodeID, _, errno := remote.Lookup(1, "hello.txt")
	if errno != 0 {
		t.Fatalf("Lookup() errno = %d", errno)
	}
	fh, errno := remote.Open(nodeID, linuxORDONLY)
	if errno != 0 {
		t.Fatalf("Open() errno = %d", errno)
	}
	defer remote.Release(nodeID, fh)
	data, errno := remote.Read(nodeID, fh, 0, 5)
	if errno != 0 {
		t.Fatalf("Read() errno = %d", errno)
	}
	if string(data) != "hello" {
		t.Fatalf("Read() = %q, want hello", string(data))
	}
}

func TestFSRemoteBackendCreateWriteAndReadBack(t *testing.T) {
	root := t.TempDir()
	remote, cleanup := newTestFSRemoteBackend(t, NewPassthroughFS(root, nil))
	defer cleanup()

	nodeID, fh, _, errno := remote.Create(1, "created.txt", linuxOWRONLY|linuxOCREAT, 0o644, 1000, 1000)
	if errno != 0 {
		t.Fatalf("Create() errno = %d", errno)
	}
	count, errno := remote.Write(nodeID, fh, 0, []byte("coordinator-owned"), 0)
	if errno != 0 {
		t.Fatalf("Write() errno = %d", errno)
	}
	if count != uint32(len("coordinator-owned")) {
		t.Fatalf("Write() count = %d", count)
	}
	if errno := remote.Flush(nodeID, fh, 0); errno != 0 {
		t.Fatalf("Flush() errno = %d", errno)
	}
	remote.Release(nodeID, fh)

	data, err := os.ReadFile(filepath.Join(root, "created.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "coordinator-owned" {
		t.Fatalf("created file = %q", string(data))
	}
}

func newTestFSRemoteBackend(t *testing.T, backend FSBackend) (*FSRemoteBackend, func()) {
	t.Helper()
	left, right := net.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- ServeFSBackend(right, backend)
	}()
	remote := NewFSRemoteBackend(left)
	cleanup := func() {
		_ = remote.Close()
		if err := <-done; err != nil {
			t.Fatalf("ServeFSBackend() error = %v", err)
		}
	}
	return remote, cleanup
}
