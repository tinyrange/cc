package virtio

import (
	"strings"
	"testing"

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
