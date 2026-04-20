package virtio

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMountedFSAddShareExposesFiles(t *testing.T) {
	rootDir := t.TempDir()
	shareDir := filepath.Join(t.TempDir(), "share")
	if err := os.MkdirAll(shareDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(share) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(shareDir, "hello.txt"), []byte("hello from share\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(share) error = %v", err)
	}

	backend := NewMountedFS(NewPassthroughFS(rootDir, nil), nil)
	mounter, ok := backend.(ShareMounter)
	if !ok {
		t.Fatalf("backend does not support ShareMounter")
	}
	if err := mounter.AddShare(ShareMount{
		GuestPath: "/.share/demo",
		Backend:   NewPassthroughFS(shareDir, nil),
	}); err != nil {
		t.Fatalf("AddShare() error = %v", err)
	}

	nodeID, _, errno := backendLookupPath(backend, "/.share/demo/hello.txt")
	if errno != 0 {
		t.Fatalf("backendLookupPath() errno = %d", errno)
	}
	fh, errno := backend.Open(nodeID, linuxORDONLY)
	if errno != 0 {
		t.Fatalf("Open() errno = %d", errno)
	}
	defer backend.Release(nodeID, fh)

	data, errno := backend.Read(nodeID, fh, 0, 1<<20)
	if errno != 0 {
		t.Fatalf("Read() errno = %d", errno)
	}
	if string(data) != "hello from share\n" {
		t.Fatalf("Read() = %q, want %q", string(data), "hello from share\n")
	}
}
