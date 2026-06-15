package ffs

import (
	"context"
	"encoding/binary"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"j5.nz/cc/internal/fsmeta"
	"j5.nz/cc/internal/imagefs"
)

func TestBuildFFSWritesOpenBSDRoot(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, "sbin"))
	mustMkdir(t, filepath.Join(root, "dev"))
	mustMkdir(t, filepath.Join(root, "etc"))
	mustWriteMode(t, filepath.Join(root, "sbin", "init"), "hello openbsd\n", 0o755)
	mustWriteMode(t, filepath.Join(root, "dev", "console"), "", 0o600)
	if err := os.Symlink("/sbin/init", filepath.Join(root, "etc", "init")); err != nil {
		t.Fatal(err)
	}
	meta := map[string]fsmeta.Entry{
		"/dev/console": {Mode: fsmeta.LinuxModeFromFileMode(fs.ModeDevice | fs.ModeCharDevice | 0o600), RDev: 0},
	}
	img, err := Build(context.Background(), imagefs.NewHostFS(root, meta), Options{
		SizeBytes:         16 << 20,
		DeterministicTime: time.Unix(1234, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, img.Size())
	if _, err := img.ReadAt(data, 0); err != nil {
		t.Fatal(err)
	}
	labelOff := ffsPartLBA*ffsSectorSize + ffsSectorSize
	if got := binary.LittleEndian.Uint32(data[labelOff : labelOff+4]); got != dlMagic {
		t.Fatalf("disklabel magic = %#x, want %#x", got, dlMagic)
	}
	var labelXOR uint16
	for off := 0; off < 148+16*16; off += 2 {
		labelXOR ^= binary.LittleEndian.Uint16(data[labelOff+off : labelOff+off+2])
	}
	if labelXOR != 0 {
		t.Fatalf("disklabel checksum xor = %#x, want 0", labelXOR)
	}
	fsOff := int64(binary.LittleEndian.Uint32(data[labelOff+148+4 : labelOff+148+8]))
	fsOff *= ffsSectorSize
	if got := binary.LittleEndian.Uint32(data[fsOff+ffsSBOFF+1372 : fsOff+ffsSBOFF+1376]); got != ffsMagic {
		t.Fatalf("superblock magic = %#x, want %#x", got, ffsMagic)
	}
	reader := newFFSTestReader(t, data)
	initIno := reader.lookupPath("/sbin/init")
	if got := reader.inodeMode(initIno); got != ifreg|0o755 {
		t.Fatalf("/sbin/init mode = %#o, want %#o", got, ifreg|0o755)
	}
	if got := string(reader.fileData(initIno)); got != "hello openbsd\n" {
		t.Fatalf("/sbin/init contents = %q", got)
	}
	consoleIno := reader.lookupPath("/dev/console")
	if got := reader.inodeMode(consoleIno); got != ifchr|0o600 {
		t.Fatalf("/dev/console mode = %#o, want %#o", got, ifchr|0o600)
	}
	linkIno := reader.lookupPath("/etc/init")
	if got := reader.symlinkTarget(linkIno); got != "/sbin/init" {
		t.Fatalf("/etc/init target = %q", got)
	}
}

func TestBuildFFSLazilyMapsRegularFiles(t *testing.T) {
	file := &lazyTestFile{data: []byte("lazy payload\n")}
	root := lazyTestDir{entries: map[string]imagefs.Entry{
		"payload": {File: file},
	}}
	img, err := Build(context.Background(), root, Options{
		SizeBytes:         16 << 20,
		DeterministicTime: time.Unix(1234, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	if file.reads != 0 {
		t.Fatalf("Build read file payload %d times; want lazy mapping", file.reads)
	}
	data := make([]byte, img.Size())
	if _, err := img.ReadAt(data, 0); err != nil {
		t.Fatal(err)
	}
	if file.reads == 0 {
		t.Fatal("image read did not read mapped file payload")
	}
	reader := newFFSTestReader(t, data)
	payloadIno := reader.lookupPath("/payload")
	if got := string(reader.fileData(payloadIno)); got != "lazy payload\n" {
		t.Fatalf("/payload contents = %q", got)
	}
}

type ffsTestReader struct {
	t     *testing.T
	data  []byte
	base  int
	fsize int
}

func newFFSTestReader(t *testing.T, data []byte) *ffsTestReader {
	t.Helper()
	labelOff := ffsPartLBA*ffsSectorSize + ffsSectorSize
	base := int(binary.LittleEndian.Uint32(data[labelOff+148+4:labelOff+148+8])) * ffsSectorSize
	return &ffsTestReader{t: t, data: data, base: base, fsize: int(binary.LittleEndian.Uint32(data[base+ffsSBOFF+52 : base+ffsSBOFF+56]))}
}

func (r *ffsTestReader) lookupPath(name string) uint32 {
	r.t.Helper()
	ino := uint32(ffsRootIno)
	if name == "/" {
		return ino
	}
	for _, part := range splitTestPath(name) {
		entries := r.dirEntries(ino)
		next, ok := entries[part]
		if !ok {
			r.t.Fatalf("%s not found in inode %d", part, ino)
		}
		ino = next
	}
	return ino
}

func (r *ffsTestReader) inodeMode(ino uint32) uint16 {
	off := r.inodeOff(ino)
	return binary.LittleEndian.Uint16(r.data[off : off+2])
}

func (r *ffsTestReader) inodeSize(ino uint32) uint64 {
	off := r.inodeOff(ino)
	return binary.LittleEndian.Uint64(r.data[off+8 : off+16])
}

func (r *ffsTestReader) symlinkTarget(ino uint32) string {
	off := r.inodeOff(ino)
	size := int(r.inodeSize(ino))
	return string(r.data[off+40 : off+40+size])
}

func (r *ffsTestReader) fileData(ino uint32) []byte {
	off := r.inodeOff(ino)
	size := int(r.inodeSize(ino))
	block := binary.LittleEndian.Uint32(r.data[off+40 : off+44])
	start := int(block) * r.fsize
	start += r.base
	return r.data[start : start+size]
}

func (r *ffsTestReader) dirEntries(ino uint32) map[string]uint32 {
	data := r.fileData(ino)
	out := map[string]uint32{}
	for off := 0; off+8 <= len(data); {
		entIno := binary.LittleEndian.Uint32(data[off : off+4])
		reclen := int(binary.LittleEndian.Uint16(data[off+4 : off+6]))
		namlen := int(data[off+7])
		if reclen <= 0 {
			break
		}
		if entIno != 0 && namlen > 0 {
			out[string(data[off+8:off+8+namlen])] = entIno
		}
		off += reclen
	}
	return out
}

func (r *ffsTestReader) inodeOff(ino uint32) int {
	return r.base + ffsIBlkNo*ffsFSize + int(ino)*ffsInodeSize
}

func splitTestPath(name string) []string {
	var out []string
	for _, part := range strings.Split(name, "/") {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWriteMode(t *testing.T, path, contents string, mode fs.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), mode); err != nil {
		t.Fatal(err)
	}
}

type lazyTestDir struct {
	entries map[string]imagefs.Entry
}

func (d lazyTestDir) Stat() fs.FileMode       { return fs.ModeDir | 0o755 }
func (d lazyTestDir) ModTime() time.Time      { return time.Unix(1234, 0) }
func (d lazyTestDir) Owner() (uint32, uint32) { return 0, 0 }
func (d lazyTestDir) RDev() uint32            { return 0 }
func (d lazyTestDir) ReadDir() ([]imagefs.DirEnt, error) {
	out := make([]imagefs.DirEnt, 0, len(d.entries))
	for name, entry := range d.entries {
		mode := fs.FileMode(0)
		if entry.Dir != nil {
			mode = fs.ModeDir | entry.Dir.Stat()
		} else if entry.Symlink != nil {
			mode = fs.ModeSymlink | entry.Symlink.Stat()
		} else if entry.File != nil {
			_, mode = entry.File.Stat()
		}
		out = append(out, imagefs.DirEnt{Name: name, Mode: mode})
	}
	return out, nil
}
func (d lazyTestDir) Lookup(name string) (imagefs.Entry, error) {
	entry, ok := d.entries[name]
	if !ok {
		return imagefs.Entry{}, os.ErrNotExist
	}
	return entry, nil
}

type lazyTestFile struct {
	data  []byte
	reads int
}

func (f *lazyTestFile) Stat() (uint64, fs.FileMode) { return uint64(len(f.data)), 0o644 }
func (f *lazyTestFile) ModTime() time.Time          { return time.Unix(1234, 0) }
func (f *lazyTestFile) Owner() (uint32, uint32)     { return 0, 0 }
func (f *lazyTestFile) RDev() uint32                { return 0 }
func (f *lazyTestFile) ReadAt(off uint64, size uint32) ([]byte, error) {
	f.reads++
	if off >= uint64(len(f.data)) {
		return nil, nil
	}
	end := off + uint64(size)
	if end > uint64(len(f.data)) {
		end = uint64(len(f.data))
	}
	return append([]byte(nil), f.data[off:end]...), nil
}
