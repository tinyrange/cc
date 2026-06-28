package ext4image

import (
	"bytes"
	"context"
	"testing"

	"j5.nz/cc/internal/imagefs"
)

func TestWriteBuildsExt4Image(t *testing.T) {
	overlay := imagefs.NewOverlay(nil)
	if err := overlay.AddFile("/etc/issue", 0o644, []byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	if err := overlay.AddSymlink("/etc/issue.link", "issue"); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := Write(context.Background(), &buf, overlay.Root(), Options{}); err != nil {
		t.Fatal(err)
	}
	if got := buf.Len(); got != defaultImageSize {
		t.Fatalf("image size = %d, want %d", got, defaultImageSize)
	}
	if got := buf.Bytes()[1024+56 : 1024+58]; got[0] != 0x53 || got[1] != 0xef {
		t.Fatalf("ext4 magic = % x, want 53 ef", got)
	}
}
