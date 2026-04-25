package virtio

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"j5.nz/cc/internal/imagefs"
)

func TestPassthroughFSCreateWriteRenameUnlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fs := NewPassthroughFS(root, nil)
	be, ok := fs.(*passthroughFS)
	if !ok {
		t.Fatalf("backend type = %T", fs)
	}

	nodeID, fh, _, errno := be.Create(1, "hello.txt", linuxOWRONLY|linuxOCREAT|linuxOTRUNC, 0o644)
	if errno != 0 {
		t.Fatalf("Create() errno = %d", errno)
	}
	if wrote, errno := be.Write(nodeID, fh, 0, []byte("hello world"), 0); errno != 0 || wrote != 11 {
		t.Fatalf("Write() = (%d, %d)", wrote, errno)
	}
	if errno := be.Flush(nodeID, fh, 0); errno != 0 {
		t.Fatalf("Flush() errno = %d", errno)
	}
	be.Release(nodeID, fh)

	data, err := os.ReadFile(filepath.Join(root, "hello.txt"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("ReadFile() = %q", string(data))
	}

	if errno := be.Rename(1, "hello.txt", 1, "renamed.txt", 0); errno != 0 {
		t.Fatalf("Rename() errno = %d", errno)
	}
	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); err != nil {
		t.Fatalf("Stat(renamed) error = %v", err)
	}
	if errno := be.Unlink(1, "renamed.txt"); errno != 0 {
		t.Fatalf("Unlink() errno = %d", errno)
	}
	if _, err := os.Stat(filepath.Join(root, "renamed.txt")); !os.IsNotExist(err) {
		t.Fatalf("Stat(after unlink) error = %v, want not exist", err)
	}
}

func TestPassthroughFSSetAttrTruncate(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	path := filepath.Join(root, "data.txt")
	if err := os.WriteFile(path, []byte("abcdefgh"), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := NewPassthroughFS(root, nil)
	be := fs.(*passthroughFS)
	nodeID := be.ensureNode("/data.txt")
	mtime := time.Unix(1710000000, 0)
	if _, errno := be.SetAttr(nodeID, fattrSize|fattrMTime, 0, 3, 0, 0, 0, time.Time{}, mtime); errno != 0 {
		t.Fatalf("SetAttr() errno = %d", errno)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "abc" {
		t.Fatalf("ReadFile() = %q", string(data))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat() error = %v", err)
	}
	if info.ModTime().Unix() != mtime.Unix() {
		t.Fatalf("mtime = %v, want %v", info.ModTime(), mtime)
	}
}

func TestStrictFUSEStatxReturnsAttr(t *testing.T) {
	t.Parallel()

	fsdev := NewFS(0, 0, 0, "root", NewPassthroughFS(t.TempDir(), nil))
	fsdev.Strict = true

	const unique = uint64(42)
	req := make([]byte, fuseInHeaderSize+24)
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.LittleEndian.PutUint32(req[4:8], fuseStatx)
	binary.LittleEndian.PutUint64(req[8:16], unique)
	binary.LittleEndian.PutUint64(req[16:24], 1)
	binary.LittleEndian.PutUint32(req[60:64], statxBasicStats)

	reply, err := fsdev.dispatchFUSELocked(req)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(STATX) error = %v", err)
	}
	if len(reply) != fuseOutHeaderSize+fuseStatxOutSize {
		t.Fatalf("STATX reply length = %d, want %d", len(reply), fuseOutHeaderSize+fuseStatxOutSize)
	}
	if got := int32(binary.LittleEndian.Uint32(reply[4:8])); got != 0 {
		t.Fatalf("STATX errno = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint64(reply[8:16]); got != unique {
		t.Fatalf("STATX unique = %d, want %d", got, unique)
	}
	statx := reply[fuseOutHeaderSize+32:]
	if got := binary.LittleEndian.Uint32(statx[0:4]); got != statxBasicStats {
		t.Fatalf("STATX mask = %#x, want %#x", got, statxBasicStats)
	}
	if got := binary.LittleEndian.Uint16(statx[28:30]); uint32(got)&linuxSIFDIR == 0 {
		t.Fatalf("STATX mode = %#o, want directory bit", got)
	}
}

func TestImageFSReadDirCompletesPartiallyMaterializedDirectory(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "lib", "os.py"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "lib", "encodings"), 0o755); err != nil {
		t.Fatal(err)
	}

	be := NewImageFS(imagefs.NewHostFS(root, nil), root).(*imageFS)
	libID, _, errno := be.Lookup(1, "lib")
	if errno != 0 {
		t.Fatalf("Lookup(lib) errno = %d", errno)
	}
	if _, _, errno := be.Lookup(libID, "os.py"); errno != 0 {
		t.Fatalf("Lookup(os.py) errno = %d", errno)
	}
	fh, errno := be.OpenDir(libID, 0)
	if errno != 0 {
		t.Fatalf("OpenDir(lib) errno = %d", errno)
	}
	defer be.ReleaseDir(libID, fh)

	entries, errno := be.ReadDir(libID, fh, 0, 4096)
	if errno != 0 {
		t.Fatalf("ReadDir(lib) errno = %d", errno)
	}
	if !containsFuseDirent(entries, "encodings") {
		t.Fatalf("ReadDir(lib) missing encodings entry in %q", string(entries))
	}
}

func TestStrictFUSEIoctlReturnsENOTTY(t *testing.T) {
	t.Parallel()

	fsdev := NewFS(0, 0, 0, "root", NewPassthroughFS(t.TempDir(), nil))
	fsdev.Strict = true

	const unique = uint64(43)
	req := make([]byte, fuseInHeaderSize)
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.LittleEndian.PutUint32(req[4:8], fuseIoctl)
	binary.LittleEndian.PutUint64(req[8:16], unique)
	binary.LittleEndian.PutUint64(req[16:24], 1)

	reply, err := fsdev.dispatchFUSELocked(req)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(IOCTL) error = %v", err)
	}
	if got := int32(binary.LittleEndian.Uint32(reply[4:8])); got != -linuxENOTTY {
		t.Fatalf("IOCTL errno = %d, want %d", got, -linuxENOTTY)
	}
}

func containsFuseDirent(data []byte, name string) bool {
	for off := 0; off+fuseDirentBaseSize <= len(data); {
		nameLen := int(binary.LittleEndian.Uint32(data[off+16 : off+20]))
		recLen := ((fuseDirentBaseSize + nameLen + 7) / 8) * 8
		if off+fuseDirentBaseSize+nameLen > len(data) || recLen <= 0 {
			return false
		}
		if string(data[off+fuseDirentBaseSize:off+fuseDirentBaseSize+nameLen]) == name {
			return true
		}
		off += recLen
	}
	return false
}
