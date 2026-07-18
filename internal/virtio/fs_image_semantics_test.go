package virtio

import (
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
