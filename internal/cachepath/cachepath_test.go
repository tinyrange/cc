package cachepath

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestEnsurePrivateRootCreatesAndRepairsDirectory(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatalf("create permissive cache: %v", err)
	}
	if err := EnsurePrivateRoot(root); err != nil {
		t.Fatalf("ensure private cache: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(root)
		if err != nil {
			t.Fatalf("stat cache: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Fatalf("cache mode = %o, want 700", got)
		}
	}
}

func TestEnsurePrivateRootRejectsSymlink(t *testing.T) {
	parent := t.TempDir()
	target := filepath.Join(parent, "target")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatalf("create target: %v", err)
	}
	link := filepath.Join(parent, "cache")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := EnsurePrivateRoot(link); err == nil {
		t.Fatal("symbolic-link cache root was accepted")
	}
}
