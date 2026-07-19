package virtio

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"j5.nz/cc/internal/imagefs"
)

func newEmptyImageFS(t *testing.T) *imageFS {
	t.Helper()
	backend, ok := NewImageFS(imagefs.NewOverlay(nil).Root(), "").(*imageFS)
	if !ok {
		t.Fatal("NewImageFS did not return an image filesystem")
	}
	t.Cleanup(func() { _ = backend.Close() })
	return backend
}

func createImageFile(t *testing.T, backend *imageFS, name, contents string) (uint64, uint64) {
	t.Helper()
	nodeID, fh, _, errno := backend.Create(1, name, linuxORDWR|linuxOCREAT|linuxOEXCL, 0o644, 0, 0)
	if errno != 0 {
		t.Fatalf("create %q: errno %d", name, errno)
	}
	if contents != "" {
		if _, errno := backend.Write(nodeID, fh, 0, []byte(contents), 0); errno != 0 {
			t.Fatalf("write %q: errno %d", name, errno)
		}
	}
	return nodeID, fh
}

func readImageHandle(t *testing.T, backend *imageFS, nodeID, fh uint64) string {
	t.Helper()
	data, errno := backend.Read(nodeID, fh, 0, 4096)
	if errno != 0 {
		t.Fatalf("read node %d handle %d: errno %d", nodeID, fh, errno)
	}
	return string(data)
}

func TestImageFSOpenHandleSurvivesUnlinkAndReplacement(t *testing.T) {
	backend := newEmptyImageFS(t)
	unlinkedID, unlinkedFH := createImageFile(t, backend, "unlinked", "open-unlinked")
	if errno := backend.Unlink(1, "unlinked"); errno != 0 {
		t.Fatalf("unlink: errno %d", errno)
	}
	if got := readImageHandle(t, backend, unlinkedID, unlinkedFH); got != "open-unlinked" {
		t.Fatalf("open unlinked contents = %q", got)
	}

	targetID, targetFH := createImageFile(t, backend, "target", "old-target")
	sourceID, sourceFH := createImageFile(t, backend, "source", "new-target")
	backend.Release(sourceID, sourceFH)
	if errno := backend.Rename(1, "source", 1, "target", 0); errno != 0 {
		t.Fatalf("replace target: errno %d", errno)
	}
	if got := readImageHandle(t, backend, targetID, targetFH); got != "old-target" {
		t.Fatalf("open replaced contents = %q", got)
	}
	replacementID, _, errno := backend.Lookup(1, "target")
	if errno != 0 {
		t.Fatalf("lookup replacement: errno %d", errno)
	}
	replacementFH, errno := backend.Open(replacementID, linuxORDONLY)
	if errno != 0 {
		t.Fatalf("open replacement: errno %d", errno)
	}
	if got := readImageHandle(t, backend, replacementID, replacementFH); got != "new-target" {
		t.Fatalf("replacement contents = %q", got)
	}
}

func TestImageFSRenameExchangeAndSameInodeNoOp(t *testing.T) {
	backend := newEmptyImageFS(t)
	aID, aFH := createImageFile(t, backend, "a", "AAA")
	bID, bFH := createImageFile(t, backend, "b", "BBB")
	backend.Release(aID, aFH)
	backend.Release(bID, bFH)
	if errno := backend.Rename(1, "a", 1, "b", linuxRenameExchange); errno != 0 {
		t.Fatalf("exchange: errno %d", errno)
	}
	for name, want := range map[string]string{"a": "BBB", "b": "AAA"} {
		nodeID, _, errno := backend.Lookup(1, name)
		if errno != 0 {
			t.Fatalf("lookup %q: errno %d", name, errno)
		}
		fh, errno := backend.Open(nodeID, linuxORDONLY)
		if errno != 0 {
			t.Fatalf("open %q: errno %d", name, errno)
		}
		if got := readImageHandle(t, backend, nodeID, fh); got != want {
			t.Fatalf("%s contents = %q, want %q", name, got, want)
		}
		backend.Release(nodeID, fh)
	}

	linkID, _, errno := backend.Link(aID, 1, "a-link")
	if errno != 0 {
		t.Fatalf("link: errno %d", errno)
	}
	if errno := backend.Rename(1, "b", 1, "a-link", 0); errno != 0 {
		t.Fatalf("same-inode rename: errno %d", errno)
	}
	for _, name := range []string{"b", "a-link"} {
		gotID, attr, errno := backend.Lookup(1, name)
		if errno != 0 || gotID != linkID || attr.NLink != 2 {
			t.Fatalf("lookup %q = node %d nlink %d errno %d", name, gotID, attr.NLink, errno)
		}
	}
}

func TestImageFSMetadataAndSparseStorage(t *testing.T) {
	backend := newEmptyImageFS(t)
	nodeID, fh := createImageFile(t, backend, "sparse", "")
	backend.Release(nodeID, fh)
	wantTime := time.Unix(1_234_567_890, 123)
	if _, errno := backend.SetAttr(nodeID, fattrMTime, 0, 0, 0, 0, 0, time.Time{}, wantTime); errno != 0 {
		t.Fatalf("set mtime: errno %d", errno)
	}
	if _, errno := backend.SetAttr(nodeID, fattrMode, 0, 0, 0o600, 0, 0, time.Time{}, time.Time{}); errno != 0 {
		t.Fatalf("chmod: errno %d", errno)
	}
	attr, errno := backend.GetAttr(nodeID)
	if errno != 0 || attr.MTimeSec != uint64(wantTime.Unix()) || attr.MTimeNsec != uint32(wantTime.Nanosecond()) {
		t.Fatalf("mtime after chmod = %d.%d errno %d", attr.MTimeSec, attr.MTimeNsec, errno)
	}

	const logicalSize = uint64(64 << 20)
	if _, errno := backend.SetAttr(nodeID, fattrSize, 0, logicalSize, 0, 0, 0, time.Time{}, time.Time{}); errno != 0 {
		t.Fatalf("truncate sparse file: errno %d", errno)
	}
	attr, errno = backend.GetAttr(nodeID)
	if errno != 0 || attr.Size != logicalSize || attr.Blocks != 0 {
		t.Fatalf("sparse attr = size %d blocks %d errno %d", attr.Size, attr.Blocks, errno)
	}
	fh, errno = backend.Open(nodeID, linuxORDWR)
	if errno != 0 {
		t.Fatalf("open sparse file: errno %d", errno)
	}
	if _, errno := backend.Write(nodeID, fh, logicalSize-1, []byte{'x'}, 0); errno != 0 {
		t.Fatalf("write sparse tail: errno %d", errno)
	}
	attr, _ = backend.GetAttr(nodeID)
	if attr.Blocks != imageDataPageSize/512 {
		t.Fatalf("sparse blocks = %d, want %d", attr.Blocks, imageDataPageSize/512)
	}
}

func TestImageFSWrittenDataDoesNotAccumulateInHostHeap(t *testing.T) {
	backend := newEmptyImageFS(t)
	nodeID, fh := createImageFile(t, backend, "large", "")
	page := make([]byte, imageDataPageSize)
	for i := range page {
		page[i] = byte(i)
	}
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	const written = 32 << 20
	for off := 0; off < written; off += len(page) {
		if _, errno := backend.Write(nodeID, fh, uint64(off), page, 0); errno != 0 {
			t.Fatalf("write at %d: errno %d", off, errno)
		}
	}
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	heapGrowth := int64(after.HeapAlloc) - int64(before.HeapAlloc)
	if heapGrowth > 8<<20 {
		t.Fatalf("writing %d bytes retained %d bytes of Go heap", written, heapGrowth)
	}
	if backend.dataStore.file == nil {
		t.Fatal("writable data was not placed in the backing store")
	}
	if info, err := backend.dataStore.file.Stat(); err != nil || info.Size() < written {
		t.Fatalf("backing store stat = %#v, %v", info, err)
	}
}

func TestImageFSDeletedDataReclaimsBackingStore(t *testing.T) {
	backend := newEmptyImageFS(t)
	nodeID, fh := createImageFile(t, backend, "temporary", "")
	page := make([]byte, imageDataPageSize)
	const written = 8 << 20
	for off := 0; off < written; off += len(page) {
		if _, errno := backend.Write(nodeID, fh, uint64(off), page, 0); errno != 0 {
			t.Fatalf("write at %d: errno %d", off, errno)
		}
	}
	current, highWater, err := backend.BackingUsage()
	if err != nil || current != written || highWater != written {
		t.Fatalf("usage after write = current %d high-water %d error %v", current, highWater, err)
	}
	if errno := backend.Unlink(1, "temporary"); errno != 0 {
		t.Fatalf("unlink: errno %d", errno)
	}
	// POSIX keeps the unlinked file alive while its handle is open.
	if current, _, _ := backend.BackingUsage(); current != written {
		t.Fatalf("usage with open unlinked handle = %d, want %d", current, written)
	}
	backend.Release(nodeID, fh)
	current, highWater, err = backend.BackingUsage()
	if err != nil || current != 0 || highWater != written {
		t.Fatalf("usage after final release = current %d high-water %d error %v", current, highWater, err)
	}
	info, err := backend.dataStore.file.Stat()
	if err != nil || info.Size() != 0 {
		t.Fatalf("backing store after release = %#v, %v", info, err)
	}
}

func TestImageFSDirectoryAndFileLinkCounts(t *testing.T) {
	backend := newEmptyImageFS(t)
	nodeID, fh := createImageFile(t, backend, "one", "data")
	backend.Release(nodeID, fh)
	if _, _, errno := backend.Link(nodeID, 1, "two"); errno != 0 {
		t.Fatalf("link: errno %d", errno)
	}
	for _, name := range []string{"one", "two"} {
		_, attr, errno := backend.Lookup(1, name)
		if errno != 0 || attr.NLink != 2 {
			t.Fatalf("%s nlink = %d errno %d", name, attr.NLink, errno)
		}
	}
	if errno := backend.Unlink(1, "one"); errno != 0 {
		t.Fatalf("unlink one: errno %d", errno)
	}
	_, attr, errno := backend.Lookup(1, "two")
	if errno != 0 || attr.NLink != 1 {
		t.Fatalf("remaining nlink = %d errno %d", attr.NLink, errno)
	}
	rootAttr, errno := backend.GetAttr(1)
	if errno != 0 || rootAttr.NLink != 2 {
		t.Fatalf("root with regular files nlink = %d errno %d", rootAttr.NLink, errno)
	}
	if _, _, errno := backend.Mkdir(1, "dir", 0o755, 0, 0); errno != 0 {
		t.Fatalf("mkdir: errno %d", errno)
	}
	rootAttr, _ = backend.GetAttr(1)
	if rootAttr.NLink != 3 {
		t.Fatalf("root with subdirectory nlink = %d", rootAttr.NLink)
	}
}

func TestImageFSOpenDirectorySurvivesRemoval(t *testing.T) {
	backend := newEmptyImageFS(t)
	dirID, _, errno := backend.Mkdir(1, "removed", 0o755, 0, 0)
	if errno != 0 {
		t.Fatalf("mkdir: errno %d", errno)
	}
	fh, errno := backend.OpenDir(dirID, 0)
	if errno != 0 {
		t.Fatalf("opendir: errno %d", errno)
	}
	if errno := backend.RmDir(1, "removed"); errno != 0 {
		t.Fatalf("rmdir: errno %d", errno)
	}
	if _, errno := backend.GetAttr(dirID); errno != 0 {
		t.Fatalf("getattr through open directory: errno %d", errno)
	}
	if entries, errno := backend.ReadDir(dirID, fh, 0, 4096); errno != 0 || len(entries) == 0 {
		t.Fatalf("readdir through removed directory: bytes=%d errno=%d", len(entries), errno)
	}
	backend.ReleaseDir(dirID, fh)
	if _, errno := backend.GetAttr(dirID); errno != -linuxENOENT {
		t.Fatalf("getattr after released directory: errno %d, want %d", errno, -linuxENOENT)
	}
}

func TestImageFSRootSurvivesDirectoryHandleRelease(t *testing.T) {
	backend := newEmptyImageFS(t)
	nodeID, fileHandle := createImageFile(t, backend, "still-here", "data")
	backend.Release(nodeID, fileHandle)
	fh, errno := backend.OpenDir(1, 0)
	if errno != 0 {
		t.Fatalf("open root: errno %d", errno)
	}
	backend.ReleaseDir(1, fh)
	if _, errno := backend.GetAttr(1); errno != 0 {
		t.Fatalf("getattr root after release: errno %d", errno)
	}
	if _, _, errno := backend.Lookup(1, "still-here"); errno != 0 {
		t.Fatalf("lookup after root release: errno %d", errno)
	}
}

func TestImageFSSetgidInheritanceAndPrivilegeBitClearing(t *testing.T) {
	backend := newEmptyImageFS(t)
	dirID, _, errno := backend.Mkdir(1, "shared", 0o2775, 0, 42)
	if errno != 0 {
		t.Fatalf("mkdir shared: errno %d", errno)
	}
	childDir, childAttr, errno := backend.Mkdir(dirID, "child", 0o775, 1000, 1000)
	if errno != 0 || childAttr.GID != 42 || childAttr.Mode&0o2000 == 0 {
		t.Fatalf("child directory attr = %+v errno %d", childAttr, errno)
	}
	_, _ = childDir, childAttr
	fileID, fh, fileAttr, errno := backend.Create(dirID, "tool", linuxORDWR|linuxOCREAT, 0o6755, 1000, 1000)
	if errno != 0 || fileAttr.GID != 42 {
		t.Fatalf("inherited file attr = %+v errno %d", fileAttr, errno)
	}
	if _, errno := backend.WriteForCaller(fileID, fh, 0, []byte("x"), 0, 1000, 42); errno != 0 {
		t.Fatalf("write privileged file: errno %d", errno)
	}
	fileAttr, _ = backend.GetAttr(fileID)
	if fileAttr.Mode&0o6000 != 0 {
		t.Fatalf("privilege bits after non-root write = %#o", fileAttr.Mode)
	}
	if _, errno := backend.SetAttr(fileID, fattrMode, 0, 0, 0o6755, 0, 0, time.Time{}, time.Time{}); errno != 0 {
		t.Fatalf("restore privilege bits: errno %d", errno)
	}
	if _, errno := backend.SetAttr(fileID, fattrGID, 0, 0, 0, 0, 77, time.Time{}, time.Time{}); errno != 0 {
		t.Fatalf("chgrp privileged file: errno %d", errno)
	}
	fileAttr, _ = backend.GetAttr(fileID)
	if fileAttr.GID != 77 || fileAttr.Mode&0o6000 != 0 {
		t.Fatalf("attr after chgrp = %+v", fileAttr)
	}
}

func TestImageFSXattrsAndSparseSeek(t *testing.T) {
	backend := newEmptyImageFS(t)
	nodeID, fh := createImageFile(t, backend, "data", "")
	if errno := backend.SetXattr(nodeID, "user.test", []byte{0, 1, 2, 0xff}, 0); errno != 0 {
		t.Fatalf("setxattr: errno %d", errno)
	}
	value, errno := backend.GetXattr(nodeID, "user.test")
	if errno != 0 || string(value) != string([]byte{0, 1, 2, 0xff}) {
		t.Fatalf("getxattr = %v errno %d", value, errno)
	}
	names, errno := backend.ListXattr(nodeID)
	if errno != 0 || string(names) != "user.test\x00" {
		t.Fatalf("listxattr = %q errno %d", names, errno)
	}
	if errno := backend.RemoveXattr(nodeID, "user.test"); errno != 0 {
		t.Fatalf("removexattr: errno %d", errno)
	}
	if _, errno := backend.GetXattr(nodeID, "user.test"); errno != -linuxENODATA {
		t.Fatalf("get removed xattr errno = %d, want %d", errno, -linuxENODATA)
	}
	if _, errno := backend.Write(nodeID, fh, 2*imageDataPageSize, []byte("tail"), 0); errno != 0 {
		t.Fatalf("write sparse extent: errno %d", errno)
	}
	if got, errno := backend.Lseek(nodeID, fh, 0, 3); errno != 0 || got != 2*imageDataPageSize {
		t.Fatalf("SEEK_DATA = %d errno %d", got, errno)
	}
	if got, errno := backend.Lseek(nodeID, fh, 2*imageDataPageSize, 4); errno != 0 || got != 2*imageDataPageSize+4 {
		t.Fatalf("SEEK_HOLE = %d errno %d", got, errno)
	}
}

func TestImageFSBoundsAggregateExtendedAttributeMemory(t *testing.T) {
	backend := newEmptyImageFS(t)
	nodeID, _ := createImageFile(t, backend, "xattrs", "")
	value := make([]byte, 60<<10)
	accepted := 0
	for i := 0; i < 128; i++ {
		errno := backend.SetXattr(nodeID, fmt.Sprintf("user.value-%d", i), value, 0)
		if errno == -linuxENOSPC {
			break
		}
		if errno != 0 {
			t.Fatalf("set xattr %d: errno %d", i, errno)
		}
		accepted++
	}
	if accepted == 0 || accepted >= 128 {
		t.Fatalf("aggregate xattr budget accepted %d entries", accepted)
	}
	if errno := backend.RemoveXattr(nodeID, "user.value-0"); errno != 0 {
		t.Fatalf("remove xattr: errno %d", errno)
	}
	if errno := backend.SetXattr(nodeID, "user.replacement", value, 0); errno != 0 {
		t.Fatalf("released xattr budget was not reusable: errno %d", errno)
	}
}
