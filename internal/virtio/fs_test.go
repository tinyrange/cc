package virtio

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPassthroughFSCreateWriteRenameUnlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fs := NewPassthroughFS(root, nil)
	be, ok := fs.(*passthroughFS)
	if !ok {
		t.Fatalf("backend type = %T", fs)
	}

	nodeID, fh, _, errno := be.Create(1, "hello.txt", linuxOWRONLY|linuxOCREAT|linuxOTRUNC, 0o644)
	if errno != 0 {
		t.Fatalf("Create() errno = %d", errno)
	}
	if wrote, errno := be.Write(nodeID, fh, 0, []byte("hello world"), 0); errno != 0 || wrote != 11 {
		t.Fatalf("Write() = (%d, %d)", wrote, errno)
	}
	if errno := be.Flush(nodeID, fh, 0); errno != 0 {
		t.Fatalf("Flush() errno = %d", errno)
	}
	be.Release(nodeID, fh)

	data, err := os.ReadFile(filepath.Join(root, "hello.txt"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("ReadFile() = %q", string(data))
	}

	if errno := be.Rename(1, "hello.txt", 1, "renamed.txt", 0); errno != 0 {
		t.Fatalf("Rename() errno = %d", errno)
	}
	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); err != nil {
		t.Fatalf("Stat(renamed) error = %v", err)
	}
	if errno := be.Unlink(1, "renamed.txt"); errno != 0 {
		t.Fatalf("Unlink() errno = %d", errno)
	}
	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); !os.IsNotExist(err) {
		t.Fatalf("Stat(after unlink) error = %v, want not exist", err)
	}
}

func TestPassthroughFSSetAttrTruncate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "data.txt")
	if err := os.WriteFile(path, []byte("abcdefgh"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := NewPassthroughFS(root, nil)
	be := fs.(*passthroughFS)
	nodeID := be.ensureNode("/data.txt")
	mtime := time.Unix(1710000000, 0)
	if _, errno := be.SetAttr(nodeID, fattrSize|fattrMTime, 0, 3, 0, 0, 0, time.Time{}, mtime); errno != 0 {
		t.Fatalf("SetAttr() errno = %d", errno)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "abc" {
		t.Fatalf("ReadFile() = %q", string(data))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.ModTime().Unix() != mtime.Unix() {
		t.Fatalf("mtime = %v, want %v", info.ModTime(), mtime)
	}
}
