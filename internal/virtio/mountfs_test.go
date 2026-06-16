package virtio

import (
	"os"
	"path/filepath"
	"strings"
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
	}); err == nil || !strings.Contains(err.Error(), "already in use") {
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

	if _, err := fsys.RootSnapshotAt("/missing"); err == nil || !strings.Contains(err.Error(), "not available") {
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

	if got := fsys.CachePolicy(1).Mode; got != fsCacheAggressive {
		t.Fatalf("root cache mode = %q, want %q", got, fsCacheAggressive)
	}
	if got := fsys.CachePolicy(shareID).Mode; got != fsCacheStrict {
		t.Fatalf("share cache mode = %q, want normalized strict mode", got)
	}
}

func TestMountedFSPassthroughHardlinkReportsSameInode(t *testing.T) {
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
	if aAttrAfter.NLink != 2 || bAttrAfter.NLink != 2 {
		t.Fatalf("hardlink nlink a=%d b=%d, want 2", aAttrAfter.NLink, bAttrAfter.NLink)
	}
}

func TestImageFSPreservesRootOwnershipForPermissionChecks(t *testing.T) {
	fsys := imageBackend(t, map[string]string{"/db": "root-owned"})
	fileID, _, errno := fsys.Lookup(1, "db")
	if errno != 0 {
		t.Fatalf("lookup db errno = %d", errno)
	}

	if _, errno := fsys.(fsOpenCallerBackend).OpenForCaller(fileID, linuxOWRONLY, 1000, 1000); errno != -linuxEACCES {
		t.Fatalf("non-root open root-owned file for write errno = %d, want %d", errno, -linuxEACCES)
	}
	if _, _, _, errno := fsys.(fsCreateCallerBackend).CreateForCaller(1, "non-root-new", linuxOWRONLY, 0o644, 1000, 1000); errno != -linuxEACCES {
		t.Fatalf("non-root create in root-owned dir errno = %d, want %d", errno, -linuxEACCES)
	}

	fh, errno := fsys.(fsOpenCallerBackend).OpenForCaller(fileID, linuxOWRONLY, 0, 0)
	if errno != 0 {
		t.Fatalf("root open root-owned file for write errno = %d", errno)
	}
	defer fsys.Release(fileID, fh)
	if _, errno := fsys.(fsWriteCallerBackend).WriteForCaller(fileID, fh, 0, []byte("root-write"), 0, 0, 0); errno != 0 {
		t.Fatalf("root write root-owned file errno = %d", errno)
	}
}

func TestMountedFSPreservesRootOwnershipForPermissionChecks(t *testing.T) {
	fsys := NewMountedFS(imageBackend(t, map[string]string{"/db": "root-owned"}), nil)
	fileID, _, errno := fsys.Lookup(1, "db")
	if errno != 0 {
		t.Fatalf("lookup db errno = %d", errno)
	}
	if _, errno := fsys.(fsOpenCallerBackend).OpenForCaller(fileID, linuxOWRONLY, 1000, 1000); errno != -linuxEACCES {
		t.Fatalf("non-root open mounted root-owned file for write errno = %d, want %d", errno, -linuxEACCES)
	}
	if _, _, _, errno := fsys.(fsCreateCallerBackend).CreateForCaller(1, "non-root-new", linuxOWRONLY, 0o644, 1000, 1000); errno != -linuxEACCES {
		t.Fatalf("non-root create in mounted root-owned dir errno = %d, want %d", errno, -linuxEACCES)
	}
}

func TestImageFSAllowsOwnerAfterRootChown(t *testing.T) {
	fsys := imageBackend(t, map[string]string{"/owned": "initial"})
	fileID, _, errno := fsys.Lookup(1, "owned")
	if errno != 0 {
		t.Fatalf("lookup owned errno = %d", errno)
	}
	if _, errno := fsys.(fsSetAttrCallerBackend).SetAttrForCaller(fileID, fattrUID|fattrGID, 0, 0, 0, 1000, 1000, zeroTime(), zeroTime(), 0, 0); errno != 0 {
		t.Fatalf("root chown file errno = %d", errno)
	}

	fh, errno := fsys.(fsOpenCallerBackend).OpenForCaller(fileID, linuxOWRONLY, 1000, 1000)
	if errno != 0 {
		t.Fatalf("owner open chowned file for write errno = %d", errno)
	}
	defer fsys.Release(fileID, fh)
	if _, errno := fsys.(fsWriteCallerBackend).WriteForCaller(fileID, fh, 0, []byte("owner-write"), 0, 1000, 1000); errno != 0 {
		t.Fatalf("owner write chowned file errno = %d", errno)
	}

	if _, errno := fsys.(fsSetAttrCallerBackend).SetAttrForCaller(fileID, fattrUID, 0, 0, 0, 1001, 1000, zeroTime(), zeroTime(), 1000, 1000); errno != -linuxEPERM {
		t.Fatalf("non-root chown errno = %d, want %d", errno, -linuxEPERM)
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
