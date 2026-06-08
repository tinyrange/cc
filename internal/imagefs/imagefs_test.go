package imagefs

import (
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/internal/fsmeta"
	"j5.nz/cc/internal/linuxabi"
)

func TestResolveCommandAbsoluteAndPATH(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll(bin) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "tool"), []byte("tool"), 0o755); err != nil {
		t.Fatalf("WriteFile(tool) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "plain"), []byte("plain"), 0o644); err != nil {
		t.Fatalf("WriteFile(plain) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "busybox"), []byte("busybox"), 0o755); err != nil {
		t.Fatalf("WriteFile(busybox) error = %v", err)
	}
	if err := os.Symlink("tool", filepath.Join(root, "bin", "tool-link")); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink creation requires privileges on windows: %v", err)
		}
		t.Fatalf("Symlink(tool-link) error = %v", err)
	}
	if err := os.Symlink("busybox", filepath.Join(root, "bin", "sh")); err != nil {
		t.Fatalf("Symlink(sh) error = %v", err)
	}

	fs := NewHostFS(root, map[string]fsmeta.Entry{
		"/bin/tool":    {Mode: linuxabi.SIFREG | 0o755},
		"/bin/plain":   {Mode: linuxabi.SIFREG | 0o644},
		"/bin/busybox": {Mode: linuxabi.SIFREG | 0o755},
	})

	got, err := ResolveCommand(fs, []string{"/bin/tool", "arg"}, nil)
	if err != nil {
		t.Fatalf("ResolveCommand(abs) error = %v", err)
	}
	if got[0] != "/bin/tool" {
		t.Fatalf("ResolveCommand(abs)[0] = %q, want /bin/tool", got[0])
	}

	got, err = ResolveCommand(fs, []string{"tool", "arg"}, []string{"PATH=/usr/bin:/bin"})
	if err != nil {
		t.Fatalf("ResolveCommand(PATH) error = %v", err)
	}
	if got[0] != "/bin/tool" {
		t.Fatalf("ResolveCommand(PATH)[0] = %q, want /bin/tool", got[0])
	}

	got, err = ResolveCommand(fs, []string{"tool-link"}, []string{"PATH=/bin"})
	if err != nil {
		t.Fatalf("ResolveCommand(symlink) error = %v", err)
	}
	if got[0] != "/bin/tool" {
		t.Fatalf("ResolveCommand(symlink)[0] = %q, want /bin/tool", got[0])
	}

	got, err = ResolveCommand(fs, []string{"/bin/tool-link"}, nil)
	if err != nil {
		t.Fatalf("ResolveCommand(abs symlink) error = %v", err)
	}
	if got[0] != "/bin/tool" {
		t.Fatalf("ResolveCommand(abs symlink)[0] = %q, want /bin/tool", got[0])
	}

	got, err = ResolveCommand(fs, []string{"sh", "-lc", "true"}, []string{"PATH=/bin"})
	if err != nil {
		t.Fatalf("ResolveCommand(busybox applet) error = %v", err)
	}
	want := []string{"/bin/busybox", "sh", "-lc", "true"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("ResolveCommand(busybox applet) = %q, want %q", got, want)
	}

	if _, err := ResolveCommand(fs, []string{"plain"}, []string{"PATH=/bin"}); err == nil {
		t.Fatal("ResolveCommand(non-executable) error = nil, want error")
	}
}

func TestResolveCommandNormalizesWindowsSymlinkTarget(t *testing.T) {
	root := &testDir{entries: map[string]Entry{
		"bin": {Dir: &testDir{entries: map[string]Entry{
			"busybox": {File: testFile{mode: 0o755}},
			"sh":      {Symlink: testSymlink{target: `\bin\busybox`}},
		}}},
	}}

	got, err := ResolveCommand(root, []string{"sh", "-lc", "true"}, []string{"PATH=/bin"})
	if err != nil {
		t.Fatalf("ResolveCommand(sh) error = %v", err)
	}
	want := []string{"/bin/busybox", "sh", "-lc", "true"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("ResolveCommand(sh) = %q, want %q", got, want)
	}
}

func TestLookupPath(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "usr", "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll(usr/bin) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "usr", "bin", "hello"), []byte("hello"), 0o755); err != nil {
		t.Fatalf("WriteFile(hello) error = %v", err)
	}

	fs := NewHostFS(root, nil)
	entry, err := LookupPath(fs, "/usr/bin/hello")
	if err != nil {
		t.Fatalf("LookupPath() error = %v", err)
	}
	if entry.File == nil {
		t.Fatal("LookupPath() returned non-file entry, want file")
	}
}

type testFile struct {
	mode fs.FileMode
}

func (f testFile) Stat() (uint64, fs.FileMode)           { return 0, f.mode }
func (f testFile) ModTime() time.Time                    { return time.Unix(0, 0) }
func (f testFile) ReadAt(uint64, uint32) ([]byte, error) { return nil, nil }
func (f testFile) Owner() (uint32, uint32)               { return 0, 0 }
func (f testFile) RDev() uint32                          { return 0 }

type testDir struct {
	entries map[string]Entry
}

func (d *testDir) Stat() fs.FileMode          { return fs.ModeDir | 0o755 }
func (d *testDir) ModTime() time.Time         { return time.Unix(0, 0) }
func (d *testDir) ReadDir() ([]DirEnt, error) { return nil, nil }
func (d *testDir) Lookup(name string) (Entry, error) {
	entry, ok := d.entries[name]
	if !ok {
		return Entry{}, fs.ErrNotExist
	}
	return entry, nil
}
func (d *testDir) Owner() (uint32, uint32) { return 0, 0 }
func (d *testDir) RDev() uint32            { return 0 }

type testSymlink struct {
	target string
}

func (s testSymlink) Stat() fs.FileMode       { return fs.ModeSymlink | 0o777 }
func (s testSymlink) ModTime() time.Time      { return time.Unix(0, 0) }
func (s testSymlink) Target() string          { return s.target }
func (s testSymlink) Owner() (uint32, uint32) { return 0, 0 }
func (s testSymlink) RDev() uint32            { return 0 }

func TestOverlayAddsFilesOverBase(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll(bin) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "bin", "base"), []byte("base"), 0o755); err != nil {
		t.Fatalf("WriteFile(base) error = %v", err)
	}

	overlay := NewOverlay(NewHostFS(root, nil))
	if err := overlay.AddFile("/bin/tool", 0o755, []byte("tool")); err != nil {
		t.Fatalf("AddFile(tool) error = %v", err)
	}
	if err := overlay.AddFile("/bin/base", 0o755, []byte("override")); err != nil {
		t.Fatalf("AddFile(base) error = %v", err)
	}

	fs := overlay.Root()
	entry, err := LookupPath(fs, "/bin/tool")
	if err != nil {
		t.Fatalf("LookupPath(/bin/tool) error = %v", err)
	}
	data, err := entry.File.ReadAt(0, 16)
	if err != nil {
		t.Fatalf("tool.ReadAt() error = %v", err)
	}
	if string(data) != "tool" {
		t.Fatalf("tool data = %q, want tool", string(data))
	}

	entry, err = LookupPath(fs, "/bin/base")
	if err != nil {
		t.Fatalf("LookupPath(/bin/base) error = %v", err)
	}
	data, err = entry.File.ReadAt(0, 16)
	if err != nil {
		t.Fatalf("base.ReadAt() error = %v", err)
	}
	if string(data) != "override" {
		t.Fatalf("base data = %q, want override", string(data))
	}

	got, err := ResolveCommand(fs, []string{"tool"}, []string{"PATH=/bin"})
	if err != nil {
		t.Fatalf("ResolveCommand(tool) error = %v", err)
	}
	if got[0] != "/bin/tool" {
		t.Fatalf("ResolveCommand(tool)[0] = %q, want /bin/tool", got[0])
	}
}

func TestOverlayCreatesNewNestedFile(t *testing.T) {
	root := t.TempDir()
	base := NewHostFS(root, nil)
	overlay := NewOverlay(base)

	if err := overlay.AddFile("/usr/local/bin/hello", 0o755, []byte("hello\n")); err != nil {
		t.Fatalf("AddFile(/usr/local/bin/hello) error = %v", err)
	}

	entry, err := LookupPath(overlay.Root(), "/usr/local/bin/hello")
	if err != nil {
		t.Fatalf("LookupPath(/usr/local/bin/hello) error = %v", err)
	}
	if entry.File == nil {
		t.Fatal("LookupPath(/usr/local/bin/hello) returned non-file entry, want file")
	}
	data, err := entry.File.ReadAt(0, 32)
	if err != nil {
		t.Fatalf("hello.ReadAt() error = %v", err)
	}
	if string(data) != "hello\n" {
		t.Fatalf("hello data = %q, want %q", string(data), "hello\n")
	}

	got, err := ResolveCommand(overlay.Root(), []string{"hello"}, []string{"PATH=/usr/local/bin"})
	if err != nil {
		t.Fatalf("ResolveCommand(hello) error = %v", err)
	}
	if got[0] != "/usr/local/bin/hello" {
		t.Fatalf("ResolveCommand(hello)[0] = %q, want /usr/local/bin/hello", got[0])
	}
}

func TestOverlayModifiesExistingFile(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatalf("MkdirAll(etc) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc", "hostname"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(hostname) error = %v", err)
	}

	overlay := NewOverlay(NewHostFS(root, nil))
	if err := overlay.AddFile("/etc/hostname", 0o644, []byte("first\n")); err != nil {
		t.Fatalf("AddFile(first hostname) error = %v", err)
	}
	if err := overlay.AddFile("/etc/hostname", 0o644, []byte("second\n")); err != nil {
		t.Fatalf("AddFile(second hostname) error = %v", err)
	}

	entry, err := LookupPath(overlay.Root(), "/etc/hostname")
	if err != nil {
		t.Fatalf("LookupPath(/etc/hostname) error = %v", err)
	}
	data, err := entry.File.ReadAt(0, 32)
	if err != nil {
		t.Fatalf("hostname.ReadAt() error = %v", err)
	}
	if string(data) != "second\n" {
		t.Fatalf("hostname data = %q, want %q", string(data), "second\n")
	}

	baseEntry, err := LookupPath(NewHostFS(root, nil), "/etc/hostname")
	if err != nil {
		t.Fatalf("LookupPath(base /etc/hostname) error = %v", err)
	}
	baseData, err := baseEntry.File.ReadAt(0, 32)
	if err != nil {
		t.Fatalf("base hostname.ReadAt() error = %v", err)
	}
	if string(baseData) != "base\n" {
		t.Fatalf("base hostname data = %q, want %q", string(baseData), "base\n")
	}
}
