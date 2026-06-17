package rootplan

import (
	"io/fs"
	"path/filepath"
	"testing"

	"j5.nz/cc/internal/imagefs"
)

func TestAddFilesAndDevices(t *testing.T) {
	base := t.TempDir()
	overlay := imagefs.NewOverlay(imagefs.NewHostFS(base, nil))
	if err := AddFiles(overlay, []File{
		{Path: "/etc/hosts", Mode: 0o644, Data: []byte("127.0.0.1 localhost\n")},
	}); err != nil {
		t.Fatalf("AddFiles: %v", err)
	}
	if err := AddDevices(overlay, []Device{
		{Path: "/dev/null", Mode: fs.ModeDevice | fs.ModeCharDevice | 0o666, RDev: 514},
	}); err != nil {
		t.Fatalf("AddDevices: %v", err)
	}
	if err := AddSymlinks(overlay, []Symlink{
		{Path: "/usr/lib/libc.so", Target: "libc.so.99.0"},
	}); err != nil {
		t.Fatalf("AddSymlinks: %v", err)
	}
	root := overlay.Root()
	hosts, err := imagefs.LookupPath(root, "/etc/hosts")
	if err != nil {
		t.Fatalf("lookup hosts: %v", err)
	}
	data, err := hosts.File.ReadAt(0, 1024)
	if err != nil {
		t.Fatalf("read hosts: %v", err)
	}
	if string(data) != "127.0.0.1 localhost\n" {
		t.Fatalf("hosts data = %q", data)
	}
	devNull, err := imagefs.LookupPath(root, filepath.ToSlash("/dev/null"))
	if err != nil {
		t.Fatalf("lookup dev null: %v", err)
	}
	_, mode := devNull.File.Stat()
	if mode != fs.ModeDevice|fs.ModeCharDevice|0o666 {
		t.Fatalf("dev null mode = %v", mode)
	}
	if got := devNull.File.RDev(); got != 514 {
		t.Fatalf("dev null rdev = %d, want 514", got)
	}
	link, err := imagefs.LookupPath(root, "/usr/lib/libc.so")
	if err != nil {
		t.Fatalf("lookup libc symlink: %v", err)
	}
	if link.Symlink == nil {
		t.Fatalf("libc entry is not a symlink")
	}
	if got := link.Symlink.Target(); got != "libc.so.99.0" {
		t.Fatalf("libc symlink target = %q", got)
	}
}
