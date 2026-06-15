package ext4image

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"j5.nz/cc/internal/imagefs"
)

func TestWriteBuildsExt4Image(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc", "issue"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("issue", filepath.Join(root, "etc", "issue.link")); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := Write(context.Background(), &buf, imagefs.NewHostFS(root, nil), Options{}); err != nil {
		t.Fatal(err)
	}
	if got := buf.Len(); got != defaultImageSize {
		t.Fatalf("image size = %d, want %d", got, defaultImageSize)
	}
	if got := buf.Bytes()[1024+56 : 1024+58]; got[0] != 0x53 || got[1] != 0xef {
		t.Fatalf("ext4 magic = % x, want 53 ef", got)
	}
}
