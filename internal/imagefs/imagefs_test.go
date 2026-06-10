package imagefs

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOverlayMergesBaseAndOverrides(t *testing.T) {
	baseDir := t.TempDir()
	mustWriteFile(t, filepath.Join(baseDir, "base.txt"), "base")
	mustMkdir(t, filepath.Join(baseDir, "etc"))
	mustWriteFile(t, filepath.Join(baseDir, "etc", "config"), "base-config")

	overlay := NewOverlay(NewHostFS(baseDir, nil))
	if err := overlay.AddFile("/etc/config", 0o644, []byte("overlay-config")); err != nil {
		t.Fatalf("override file: %v", err)
	}
	if err := overlay.AddDir("/opt/bin", fs.ModeDir|0o755); err != nil {
		t.Fatalf("add dir: %v", err)
	}
	if err := overlay.AddFile("/opt/bin/tool", 0o755, []byte("#!/bin/sh\n")); err != nil {
		t.Fatalf("add file: %v", err)
	}

	root := overlay.Root()
	entries, err := root.ReadDir()
	if err != nil {
		t.Fatalf("read root: %v", err)
	}
	if got := direntNames(entries); got != "base.txt,etc,opt" {
		t.Fatalf("root entries = %q", got)
	}
	if got := readFile(t, root, "/etc/config"); got != "overlay-config" {
		t.Fatalf("overlay file = %q", got)
	}
	if got := readFile(t, root, "/base.txt"); got != "base" {
		t.Fatalf("base file = %q", got)
	}
}

func TestResolveCommandUsesPathAndExecutableBits(t *testing.T) {
	rootDir := t.TempDir()
	mustMkdir(t, filepath.Join(rootDir, "bin"))
	mustWriteExecutable(t, filepath.Join(rootDir, "bin", "run"), "run")
	mustWriteFile(t, filepath.Join(rootDir, "bin", "not-exec"), "nope")
	mustMkdir(t, filepath.Join(rootDir, "sbin"))

	root := NewHostFS(rootDir, nil)
	cmd, err := ResolveCommand(root, []string{"run", "--flag"}, []string{"PATH=/missing:/bin"})
	if err != nil {
		t.Fatalf("resolve command: %v", err)
	}
	if strings.Join(cmd, " ") != "/bin/run --flag" {
		t.Fatalf("resolved command = %#v", cmd)
	}
	if _, err := ResolveCommand(root, []string{"not-exec"}, []string{"PATH=/bin"}); err == nil || !strings.Contains(err.Error(), "resolve command") {
		t.Fatalf("non-executable command error = %v", err)
	}
	if _, err := ResolveCommand(root, []string{"/sbin"}, nil); err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("directory command error = %v", err)
	}
}

func TestResolvePathFollowsSymlinks(t *testing.T) {
	rootDir := t.TempDir()
	mustMkdir(t, filepath.Join(rootDir, "bin"))
	mustWriteExecutable(t, filepath.Join(rootDir, "bin", "target"), "target")
	if err := os.Symlink("../bin/target", filepath.Join(rootDir, "bin", "link")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	resolved, entry, err := ResolvePath(NewHostFS(rootDir, nil), "/bin/link")
	if err != nil {
		t.Fatalf("resolve symlink: %v", err)
	}
	if resolved != "/bin/target" {
		t.Fatalf("resolved path = %q, want /bin/target", resolved)
	}
	if entry.File == nil {
		t.Fatalf("resolved symlink did not yield a file")
	}
}

func TestLookupPathCleansInputAndReportsNonDirectory(t *testing.T) {
	rootDir := t.TempDir()
	mustWriteFile(t, filepath.Join(rootDir, "file"), "contents")

	entry, err := LookupPath(NewHostFS(rootDir, nil), "///file")
	if err != nil {
		t.Fatalf("lookup cleaned path: %v", err)
	}
	if entry.File == nil {
		t.Fatalf("lookup did not return file")
	}
	if _, err := LookupPath(NewHostFS(rootDir, nil), "/file/child"); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("lookup through file error = %v", err)
	}
}

func direntNames(entries []DirEnt) string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name)
	}
	return strings.Join(names, ",")
}

func readFile(t *testing.T, root Directory, guestPath string) string {
	t.Helper()
	entry, err := LookupPath(root, guestPath)
	if err != nil {
		t.Fatalf("lookup %s: %v", guestPath, err)
	}
	if entry.File == nil {
		t.Fatalf("%s is not a file", guestPath)
	}
	size, _ := entry.File.Stat()
	data, err := entry.File.ReadAt(0, uint32(size))
	if err != nil {
		t.Fatalf("read %s: %v", guestPath, err)
	}
	return string(data)
}

func mustWriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func mustWriteExecutable(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o755); err != nil {
		t.Fatalf("write executable %s: %v", path, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
