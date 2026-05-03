//go:build linux

package virtio

import (
	"os"
	"testing"
)

func TestPassthroughFSReportsHostOwnership(t *testing.T) {
	root := t.TempDir()
	backend := NewPassthroughFS(root, nil)

	attr, errno := backend.GetAttr(1)
	if errno != 0 {
		t.Fatalf("GetAttr(root) errno = %d", errno)
	}
	if attr.UID != uint32(os.Getuid()) {
		t.Fatalf("GetAttr(root).UID = %d, want host uid %d", attr.UID, os.Getuid())
	}
	if attr.GID != uint32(os.Getgid()) {
		t.Fatalf("GetAttr(root).GID = %d, want host gid %d", attr.GID, os.Getgid())
	}
}
