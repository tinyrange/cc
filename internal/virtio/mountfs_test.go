package virtio

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"j5.nz/cc/internal/imagefs"
)

func TestMountedFSAddShareSnapshotsAndConflicts(t *testing.T) {
	rootBackend := imageBackend(t, map[string]string{"/root.txt": "root"})
	shareBackend := imageBackend(t, map[string]string{"/share.txt": "share"})
	otherBackend := imageBackend(t, map[string]string{"/share.txt": "other"})

	fsys := NewMountedFS(rootBackend, nil).(*mountedFS)
	if err := fsys.AddShare(ShareMount{
		GuestPath: "/mnt/share",
		Backend:   shareBackend,
		Writable:  true,
		CacheMode: fsCacheNormal,
	}); err != nil {
		t.Fatalf("add share: %v", err)
	}
	if err := fsys.AddShare(ShareMount{
		GuestPath: "mnt/share",
		Backend:   shareBackend,
		Writable:  true,
		CacheMode: fsCacheNormal,
	}); err != nil {
		t.Fatalf("re-add identical share: %v", err)
	}
	if err := fsys.AddShare(ShareMount{
		GuestPath: "/mnt/share",
		Backend:   otherBackend,
		Writable:  true,
		CacheMode: fsCacheNormal,
	}); err == nil {
		t.Fatalf("conflicting share error = %v", err)
	}

	rootSnap, err := fsys.RootSnapshot()
	if err != nil {
		t.Fatalf("root snapshot: %v", err)
	}
	if got := readSnapshotFile(t, rootSnap, "/root.txt"); got != "root" {
		t.Fatalf("root snapshot file = %q", got)
	}

	shareSnap, err := fsys.RootSnapshotAt("/mnt/share")
	if err != nil {
		t.Fatalf("share snapshot: %v", err)
	}
	if got := readSnapshotFile(t, shareSnap, "/share.txt"); got != "share" {
		t.Fatalf("share snapshot file = %q", got)
	}

	if _, err := fsys.RootSnapshotAt("/missing"); err == nil {
		t.Fatalf("missing mount snapshot error = %v", err)
	}
}

func TestMountedFSLookupRoutesSyntheticMountPaths(t *testing.T) {
	fsys := NewMountedFS(
		imageBackend(t, map[string]string{"/root.txt": "root"}),
		[]ShareMount{{
			GuestPath: "/mnt/share",
			Backend:   imageBackend(t, map[string]string{"/nested/file.txt": "share data"}),
			CacheMode: "unknown",
		}},
	).(*mountedFS)

	mntID, attr, errno := fsys.Lookup(1, "mnt")
	if errno != 0 {
		t.Fatalf("lookup synthetic /mnt errno = %d", errno)
	}
	if attr.Mode&uint32(dirTypeDir) == 0 && attr.Size != 0 {
		t.Fatalf("synthetic /mnt attr looks wrong: %+v", attr)
	}
	shareID, _, errno := fsys.Lookup(mntID, "share")
	if errno != 0 {
		t.Fatalf("lookup /mnt/share errno = %d", errno)
	}
	nestedID, _, errno := fsys.Lookup(shareID, "nested")
	if errno != 0 {
		t.Fatalf("lookup share dir errno = %d", errno)
	}
	fileID, _, errno := fsys.Lookup(nestedID, "file.txt")
	if errno != 0 {
		t.Fatalf("lookup share file errno = %d", errno)
	}
	fh, errno := fsys.Open(fileID, 0)
	if errno != 0 {
		t.Fatalf("open share file errno = %d", errno)
	}
	defer fsys.Release(fileID, fh)
	data, errno := fsys.Read(fileID, fh, 0, 128)
	if errno != 0 {
		t.Fatalf("read share file errno = %d", errno)
	}
	if string(data) != "share data" {
		t.Fatalf("share file data = %q", data)
	}

	if got := fsys.CachePolicy(1).AttrTTL; got != 0 {
		t.Fatalf("writable root attribute TTL = %s, want immediate mutation visibility", got)
	}
	if got := fsys.CachePolicy(shareID).Mode; got != fsCacheStrict {
		t.Fatalf("share cache mode = %q, want normalized strict mode", got)
	}
}

func TestMountedFSPreservesTrailingSpaceNames(t *testing.T) {
	root := imageBackend(t, nil)
	fsys := NewMountedFS(root, nil).(*mountedFS)
	var create fsCreateCallerBackend = fsys
	var write fsWriteCallerBackend = fsys

	created := make(map[string]uint64)
	for name, contents := range map[string]string{"collision": "A", "collision ": "B"} {
		nodeID, fh, _, errno := create.CreateForCaller(1, name, linuxOWRONLY|linuxOCREAT|linuxOEXCL, 0o644, 0, 0)
		if errno != 0 {
			t.Fatalf("create %q errno = %d", name, errno)
		}
		if _, errno := write.WriteForCaller(nodeID, fh, 0, []byte(contents), 0, 0, 0); errno != 0 {
			t.Fatalf("write %q errno = %d", name, errno)
		}
		fsys.Release(nodeID, fh)
		created[name] = nodeID
	}
	if created["collision"] == created["collision "] {
		t.Fatalf("byte-distinct names share node %d", created["collision"])
	}
	for name, want := range map[string]string{"collision": "A", "collision ": "B"} {
		nodeID, _, errno := fsys.Lookup(1, name)
		if errno != 0 {
			t.Fatalf("lookup %q errno = %d", name, errno)
		}
		fh, errno := fsys.Open(nodeID, linuxORDONLY)
		if errno != 0 {
			t.Fatalf("open %q errno = %d", name, errno)
		}
		data, errno := fsys.Read(nodeID, fh, 0, 16)
		fsys.Release(nodeID, fh)
		if errno != 0 || string(data) != want {
			t.Fatalf("read %q = %q, errno %d; want %q", name, data, errno, want)
		}
	}

	paths := fsys.SnapshotNodePaths()
	restored := NewMountedFS(root, nil).(*mountedFS)
	if err := restored.RestoreNodePaths(paths); err != nil {
		t.Fatalf("restore node paths: %v", err)
	}
	plainID, _, plainErrno := restored.Lookup(1, "collision")
	spacedID, _, spacedErrno := restored.Lookup(1, "collision ")
	if plainErrno != 0 || spacedErrno != 0 || plainID == spacedID {
		t.Fatalf("restored names = plain(%d,%d) spaced(%d,%d)", plainID, plainErrno, spacedID, spacedErrno)
	}
}

func TestMountedFSPassthroughHardlinkReportsSameInode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows passthrough inode reporting does not expose stable hardlink inode identity")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a"), []byte("data"), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	fsys := NewMountedFS(NewPassthroughFS(root, nil), nil).(*mountedFS)

	aID, aAttr, errno := fsys.Lookup(1, "a")
	if errno != 0 {
		t.Fatalf("lookup a errno = %d", errno)
	}
	bID, bAttr, errno := fsys.Link(aID, 1, "b")
	if errno != 0 {
		t.Fatalf("link errno = %d", errno)
	}
	aAttrAfter, errno := fsys.GetAttr(aID)
	if errno != 0 {
		t.Fatalf("getattr a errno = %d", errno)
	}
	bAttrAfter, errno := fsys.GetAttr(bID)
	if errno != 0 {
		t.Fatalf("getattr b errno = %d", errno)
	}
	if aAttr.Ino != bAttr.Ino || aAttrAfter.Ino != bAttrAfter.Ino {
		t.Fatalf("hardlink inodes before=(%d,%d) after=(%d,%d), want same", aAttr.Ino, bAttr.Ino, aAttrAfter.Ino, bAttrAfter.Ino)
	}
	if aAttrAfter.NLink < 2 || bAttrAfter.NLink < 2 {
		t.Fatalf("hardlink nlink a=%d b=%d, want at least 2", aAttrAfter.NLink, bAttrAfter.NLink)
	}
}

func TestImageFSHardlinkReportsSameInode(t *testing.T) {
	fsys := imageBackend(t, map[string]string{"/a": "data"})
	aID, aAttr, errno := fsys.Lookup(1, "a")
	if errno != 0 {
		t.Fatalf("lookup a errno = %d", errno)
	}
	bID, bAttr, errno := fsys.(fsLinkBackend).Link(aID, 1, "b")
	if errno != 0 {
		t.Fatalf("link errno = %d", errno)
	}
	aAttrAfter, errno := fsys.GetAttr(aID)
	if errno != 0 {
		t.Fatalf("getattr a errno = %d", errno)
	}
	bAttrAfter, errno := fsys.GetAttr(bID)
	if errno != 0 {
		t.Fatalf("getattr b errno = %d", errno)
	}
	if aAttr.Ino != bAttr.Ino || aAttrAfter.Ino != bAttrAfter.Ino {
		t.Fatalf("hardlink inodes before=(%d,%d) after=(%d,%d), want same", aAttr.Ino, bAttr.Ino, aAttrAfter.Ino, bAttrAfter.Ino)
	}
	if aID != bID {
		t.Fatalf("hardlink node IDs = %d and %d, want one FUSE inode", aID, bID)
	}
	if aAttrAfter.NLink != 2 || bAttrAfter.NLink != 2 {
		t.Fatalf("hardlink nlink a=%d b=%d, want 2", aAttrAfter.NLink, bAttrAfter.NLink)
	}
	if errno := fsys.(fsUnlinkBackend).Unlink(1, "a"); errno != 0 {
		t.Fatalf("unlink first hardlink errno = %d", errno)
	}
	remainingID, remainingAttr, errno := fsys.Lookup(1, "b")
	if errno != 0 || remainingID != bID || remainingAttr.NLink != 1 {
		t.Fatalf("remaining hardlink = node %d nlink %d errno %d, want node %d nlink 1", remainingID, remainingAttr.NLink, errno, bID)
	}
}

func TestMountedFSOpenDirectorySurvivesRemoval(t *testing.T) {
	root := imageBackend(t, nil)
	backendNodeID, _, errno := root.(fsMkdirBackend).Mkdir(1, "open-directory", 0o700, 0, 0)
	if errno != 0 {
		t.Fatalf("mkdir backend directory: errno %d", errno)
	}
	fsys := NewMountedFS(root, nil).(*mountedFS)
	nodeID, _, errno := fsys.Lookup(1, "open-directory")
	if errno != 0 {
		t.Fatalf("lookup mounted directory: errno %d", errno)
	}
	fh, errno := fsys.OpenDir(nodeID, 0)
	if errno != 0 {
		t.Fatalf("open mounted directory: errno %d", errno)
	}
	if errno := fsys.RmDir(1, "open-directory"); errno != 0 {
		t.Fatalf("remove mounted directory: errno %d", errno)
	}
	if fsys.node(nodeID) == nil {
		t.Fatal("mounted node was dropped while its directory handle was open")
	}
	if _, errno := root.GetAttr(backendNodeID); errno != 0 {
		t.Fatalf("backend node was dropped while its directory handle was open: errno %d", errno)
	}
	if _, errno := fsys.GetAttr(nodeID); errno != 0 {
		t.Fatalf("getattr open removed directory: errno %d", errno)
	}
	if _, errno := fsys.ReadDir(nodeID, fh, 0, 4096); errno != 0 {
		t.Fatalf("readdir open removed directory: errno %d", errno)
	}
	fsys.ReleaseDir(nodeID, fh)
	if _, errno := fsys.GetAttr(nodeID); errno != -linuxENOENT {
		t.Fatalf("getattr released removed directory: errno %d, want %d", errno, -linuxENOENT)
	}
}

func BenchmarkImageFSGentooLikeTinyFiles(b *testing.B) {
	const (
		fileCount = 1024
		fileSize  = 128
	)
	payload := make([]byte, fileSize)
	names := make([]string, fileCount)
	for i := range names {
		names[i] = "repo-file-" + strconv.Itoa(i)
	}

	b.ReportAllocs()
	b.SetBytes(fileCount * fileSize)
	b.ReportMetric(fileCount, "files/op")
	b.ResetTimer()

	for n := 0; n < b.N; n++ {
		fsys := NewImageFS(imagefs.NewOverlay(nil).Root(), "")
		create := fsys.(fsCreateCallerBackend)
		write := fsys.(fsWriteCallerBackend)

		for _, name := range names {
			nodeID, fh, _, errno := create.CreateForCaller(1, name, linuxOWRONLY|linuxOCREAT|linuxOEXCL, 0o644, 0, 0)
			if errno != 0 {
				b.Fatalf("create %s errno = %d", name, errno)
			}
			if _, errno := write.WriteForCaller(nodeID, fh, 0, payload, 0, 0, 0); errno != 0 {
				b.Fatalf("write %s errno = %d", name, errno)
			}
			fsys.Release(nodeID, fh)
		}
	}
}

func BenchmarkImageFSSameBytesSingleFile(b *testing.B) {
	const (
		fileCount = 1024
		fileSize  = 128
	)
	payload := make([]byte, fileCount*fileSize)

	b.ReportAllocs()
	b.SetBytes(int64(len(payload)))
	b.ReportMetric(1, "files/op")

	for n := 0; n < b.N; n++ {
		fsys := NewImageFS(imagefs.NewOverlay(nil).Root(), "")
		nodeID, fh, _, errno := fsys.(fsCreateCallerBackend).CreateForCaller(1, "repo-pack", linuxOWRONLY|linuxOCREAT|linuxOEXCL, 0o644, 0, 0)
		if errno != 0 {
			b.Fatalf("create repo-pack errno = %d", errno)
		}
		if _, errno := fsys.(fsWriteCallerBackend).WriteForCaller(nodeID, fh, 0, payload, 0, 0, 0); errno != 0 {
			b.Fatalf("write repo-pack errno = %d", errno)
		}
		fsys.Release(nodeID, fh)
	}
}

func zeroTime() time.Time {
	return time.Time{}
}

func imageBackend(t *testing.T, files map[string]string) FSBackend {
	t.Helper()
	overlay := imagefs.NewOverlay(nil)
	for guestPath, contents := range files {
		if err := overlay.AddFile(guestPath, 0o644, []byte(contents)); err != nil {
			t.Fatalf("add %s: %v", guestPath, err)
		}
	}
	return NewImageFS(overlay.Root(), "")
}

func readSnapshotFile(t *testing.T, root imagefs.Directory, guestPath string) string {
	t.Helper()
	entry, err := imagefs.LookupPath(root, guestPath)
	if err != nil {
		t.Fatalf("lookup snapshot %s: %v", guestPath, err)
	}
	if entry.File == nil {
		t.Fatalf("snapshot %s is not a file", guestPath)
	}
	size, _ := entry.File.Stat()
	data, err := entry.File.ReadAt(0, uint32(size))
	if err != nil {
		t.Fatalf("read snapshot %s: %v", guestPath, err)
	}
	return string(data)
}
