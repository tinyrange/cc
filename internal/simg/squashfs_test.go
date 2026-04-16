package simg

import (
	"os"
	"path/filepath"
	"testing"

	"j5.nz/cc/internal/imagefs"
)

func TestBuildImageFSFromFixture(t *testing.T) {
	fixture := filepath.Join("..", "..", "local", "alpine.simg")
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
