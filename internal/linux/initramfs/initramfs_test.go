package initramfs

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBuildIncludesFileAndTrailer(t *testing.T) {
	data, err := Build([]File{{
		Path: "/init",
		Mode: 0o755,
		Data: []byte("hello"),
	}})
	if err != nil {
		t.Fatalf("Build() error = %v", err)
	}
	if !bytes.Contains(data, []byte("init\x00")) {
		t.Fatalf("archive missing init entry")
	}
	if !bytes.Contains(data, []byte("TRAILER!!!\x00")) {
		t.Fatalf("archive missing trailer entry")
	}
}

func TestBuildFromDirectoryIncludesSymlink(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatalf("Mkdir(bin) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "busybox"), []byte("busy"), 0o755); err != nil {
		t.Fatalf("WriteFile(busybox) error = %v", err)
	}
	if err := os.Symlink("busybox", filepath.Join(root, "bin", "sh")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink creation requires privileges on windows: %v", err)
		}
		t.Fatalf("Symlink(sh) error = %v", err)
	}

	data, err := BuildFromDirectory(root, nil)
	if err != nil {
		t.Fatalf("BuildFromDirectory() error = %v", err)
	}

	if !bytes.Contains(data, []byte("bin/sh\x00")) {
		t.Fatalf("archive missing symlink entry")
	}
	if !bytes.Contains(data, []byte("busybox")) {
		t.Fatalf("archive missing symlink target payload")
	}
}
