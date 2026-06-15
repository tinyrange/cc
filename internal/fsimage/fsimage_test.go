package fsimage

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"j5.nz/cc/internal/imagefs"
)

func TestWriteDispatchesExt4(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc", "issue"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := Write(context.Background(), &buf, imagefs.NewHostFS(root, nil), Options{Type: TypeExt4}); err != nil {
		t.Fatal(err)
	}
	if got := buf.Bytes()[1024+56 : 1024+58]; got[0] != 0x53 || got[1] != 0xef {
		t.Fatalf("ext4 magic = % x, want 53 ef", got)
	}
}

func TestParseTypeAcceptsPlannedFormats(t *testing.T) {
	for _, value := range []string{"ext4", "vfat", "iso9660"} {
		if _, err := ParseType(value); err != nil {
			t.Fatalf("ParseType(%q): %v", value, err)
		}
	}
}

func TestWriteRejectsUnimplementedWriter(t *testing.T) {
	root := t.TempDir()
	var buf bytes.Buffer
	err := Write(context.Background(), &buf, imagefs.NewHostFS(root, nil), Options{Type: TypeVFAT})
	if err == nil {
		t.Fatal("Write(vfat) succeeded before a vfat writer exists")
	}
}
