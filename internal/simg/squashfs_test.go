package simg

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"j5.nz/cc/internal/imagefs"
)

func TestBuildImageFSFromFixture(t *testing.T) {
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("fixture missing: %v", err)
	}

	root, _, arch, err := BuildImageFS(fixture)
	if err != nil {
		t.Fatalf("BuildImageFS() error = %v", err)
	}
	if arch != "arm64" && arch != "amd64" {
		t.Fatalf("BuildImageFS().arch = %q, want arm64 or amd64", arch)
	}

	entry, err := imagefs.LookupPath(root, "/etc/alpine-release")
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

	entry, err = imagefs.LookupPath(root, "/bin/cat")
	if err != nil {
		t.Fatalf("LookupPath(/bin/cat) error = %v", err)
	}
	if entry.Symlink == nil {
		t.Fatalf("/bin/cat entry = %#v, want symlink", entry)
	}
	if entry.Symlink.Target() != "busybox" && entry.Symlink.Target() != "/bin/busybox" {
		t.Fatalf("/bin/cat target = %q, want busybox or /bin/busybox", entry.Symlink.Target())
	}
}

func TestReadAtNonZeroOffsetMatchesFullRead(t *testing.T) {
	fixture := filepath.Join("..", "..", "fixtures", "alpine.simg")
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("fixture missing: %v", err)
	}

	root, _, _, err := BuildImageFS(fixture)
	if err != nil {
		t.Fatalf("BuildImageFS() error = %v", err)
	}
	entry, err := imagefs.LookupPath(root, "/bin/busybox")
	if err != nil {
		t.Fatalf("LookupPath(/bin/busybox) error = %v", err)
	}
	if entry.File == nil {
		t.Fatal("/bin/busybox is not a file")
	}
	size, _ := entry.File.Stat()
	if size < 8192 {
		t.Fatalf("/bin/busybox size = %d, want at least 8192 bytes", size)
	}

	full, err := entry.File.ReadAt(0, uint32(size))
	if err != nil {
		t.Fatalf("ReadAt(full) error = %v", err)
	}
	offset := uint64(4096)
	length := uint32(4096)
	chunk, err := entry.File.ReadAt(offset, length)
	if err != nil {
		t.Fatalf("ReadAt(offset) error = %v", err)
	}
	if !bytes.Equal(chunk, full[offset:offset+uint64(length)]) {
		t.Fatal("offset read does not match full read")
	}
}
