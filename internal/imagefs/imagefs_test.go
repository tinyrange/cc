package imagefs

import (
	"archive/tar"
	"bytes"
	"context"
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
	overlay := NewOverlay(nil)
	if err := overlay.AddDir("/bin", fs.ModeDir|0o755); err != nil {
		t.Fatalf("add bin: %v", err)
	}
	if err := overlay.AddFile("/bin/run", 0o755, []byte("run")); err != nil {
		t.Fatalf("add executable: %v", err)
	}
	if err := overlay.AddFile("/bin/not-exec", 0o644, []byte("nope")); err != nil {
		t.Fatalf("add non-executable: %v", err)
	}
	if err := overlay.AddDir("/sbin", fs.ModeDir|0o755); err != nil {
		t.Fatalf("add sbin: %v", err)
	}

	root := overlay.Root()
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
	overlay := NewOverlay(nil)
	if err := overlay.AddDir("/bin", fs.ModeDir|0o755); err != nil {
		t.Fatalf("add bin: %v", err)
	}
	if err := overlay.AddFile("/bin/target", 0o755, []byte("target")); err != nil {
		t.Fatalf("add target: %v", err)
	}
	if err := overlay.AddSymlink("/bin/link", "../bin/target"); err != nil {
		t.Fatalf("add symlink: %v", err)
	}

	resolved, entry, err := ResolvePath(overlay.Root(), "/bin/link")
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

func TestTarFSKeepsImplicitDirectoryChildrenWhenDirectoryHeaderAppearsLater(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	mustWriteTarFile(t, tw, "usr/lib/libearly.so.1.0", "early")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "usr/lib",
		Typeflag: tar.TypeDir,
		Mode:     0o755,
	}); err != nil {
		t.Fatalf("write usr/lib header: %v", err)
	}
	mustWriteTarFile(t, tw, "usr/lib/late.o", "late")
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}

	tfs, err := NewTarFS(context.Background(), bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("read tarfs: %v", err)
	}
	defer tfs.Close()
	root := tfs.Root()
	if got := readFile(t, root, "/usr/lib/libearly.so.1.0"); got != "early" {
		t.Fatalf("early file = %q", got)
	}
	if got := readFile(t, root, "/usr/lib/late.o"); got != "late" {
		t.Fatalf("late file = %q", got)
	}
	entry, err := LookupPath(root, "/usr/lib")
	if err != nil {
		t.Fatalf("lookup /usr/lib: %v", err)
	}
	entries, err := entry.Dir.ReadDir()
	if err != nil {
		t.Fatalf("read /usr/lib: %v", err)
	}
	if got := direntNames(entries); got != "late.o,libearly.so.1.0" {
		t.Fatalf("/usr/lib entries = %q", got)
	}
}

func TestSeekableTarFSReadsPayloadsFromTarFile(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	mustWriteTarFile(t, tw, "bin/tool", "tool payload")
	if err := tw.WriteHeader(&tar.Header{
		Name:     "bin/tool-hardlink",
		Typeflag: tar.TypeLink,
		Mode:     0o644,
		Linkname: "bin/tool",
	}); err != nil {
		t.Fatalf("write hardlink header: %v", err)
	}
	mustWriteTarFile(t, tw, "etc/config", "config payload")
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	tarPath := filepath.Join(t.TempDir(), "root.tar")
	if err := os.WriteFile(tarPath, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write tar: %v", err)
	}

	tfs, err := NewSeekableTarFS(context.Background(), tarPath)
	if err != nil {
		t.Fatalf("read seekable tarfs: %v", err)
	}
	defer tfs.Close()
	root := tfs.Root()
	if got := readFile(t, root, "/bin/tool"); got != "tool payload" {
		t.Fatalf("tool = %q", got)
	}
	if got := readFile(t, root, "/bin/tool-hardlink"); got != "tool payload" {
		t.Fatalf("hardlink = %q", got)
	}
	if got := readFile(t, root, "/etc/config"); got != "config payload" {
		t.Fatalf("config = %q", got)
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

func mustWriteTarFile(t *testing.T, tw *tar.Writer, name, contents string) {
	t.Helper()
	if err := tw.WriteHeader(&tar.Header{
		Name:     name,
		Typeflag: tar.TypeReg,
		Mode:     0o644,
		Size:     int64(len(contents)),
	}); err != nil {
		t.Fatalf("write %s header: %v", name, err)
	}
	if _, err := tw.Write([]byte(contents)); err != nil {
		t.Fatalf("write %s payload: %v", name, err)
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
}
