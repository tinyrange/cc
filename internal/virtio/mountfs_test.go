package virtio

import (
	"os"
	"path/filepath"
	"testing"

	"j5.nz/cc/internal/imagefs"
)

func TestMountedFSRootReadDirIncludesShareMount(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(rootDir, "base.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	shareDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(shareDir, "shared.txt"), []byte("shared"), 0o644); err != nil {
		t.Fatal(err)
	}

	be := NewMountedFS(
		NewImageFS(imagefs.NewHostFS(rootDir, nil), rootDir),
		[]ShareMount{{
			GuestPath: "/work",
			Backend:   NewImageFS(imagefs.NewHostFS(shareDir, nil), shareDir),
		}},
	)

	fh, errno := be.OpenDir(1, 0)
	if errno != 0 {
		t.Fatalf("OpenDir(/) errno = %d", errno)
	}
	defer be.ReleaseDir(1, fh)

	entries, errno := be.ReadDir(1, fh, 0, 1<<20)
	if errno != 0 {
		t.Fatalf("ReadDir(/) errno = %d", errno)
	}
	dirents := decodeDirEntries(entries)
	names := map[string]bool{}
	for _, ent := range dirents {
		names[ent.name] = true
	}
	for _, want := range []string{".", "..", "base.txt", "work"} {
		if !names[want] {
			t.Fatalf("root dir missing %q: %#v", want, dirents)
		}
	}

	workID, _, errno := be.Lookup(1, "work")
	if errno != 0 {
		t.Fatalf("Lookup(/work) errno = %d", errno)
	}
	workFH, errno := be.OpenDir(workID, 0)
	if errno != 0 {
		t.Fatalf("OpenDir(/work) errno = %d", errno)
	}
	defer be.ReleaseDir(workID, workFH)
	workEntries, errno := be.ReadDir(workID, workFH, 0, 1<<20)
	if errno != 0 {
		t.Fatalf("ReadDir(/work) errno = %d", errno)
	}
	workDirents := decodeDirEntries(workEntries)
	foundShared := false
	for _, ent := range workDirents {
		if ent.name == "shared.txt" {
			foundShared = true
			break
		}
	}
	if !foundShared {
		t.Fatalf("/work dir missing shared.txt: %#v", workDirents)
	}
}

func TestMountedFSWritableShareCreateWrite(t *testing.T) {
	t.Parallel()

	rootDir := t.TempDir()
	shareDir := t.TempDir()
	be := NewMountedFS(
		NewImageFS(imagefs.NewHostFS(rootDir, nil), rootDir),
		[]ShareMount{{
			GuestPath: "/work",
			Backend:   NewPassthroughFS(shareDir, nil),
			Writable:  true,
		}},
	)

	workID, _, errno := be.Lookup(1, "work")
	if errno != 0 {
		t.Fatalf("Lookup(/work) errno = %d", errno)
	}
	nodeID, fh, _, errno := be.(fsCreateBackend).Create(workID, "hello.txt", linuxOWRONLY|linuxOCREAT|linuxOTRUNC, 0o644)
	if errno != 0 {
		t.Fatalf("Create(/work/hello.txt) errno = %d", errno)
	}
	if wrote, errno := be.(fsWriteBackend).Write(nodeID, fh, 0, []byte("hello mounted fs"), 0); errno != 0 || wrote != 16 {
		t.Fatalf("Write(/work/hello.txt) = (%d, %d)", wrote, errno)
	}
	be.Release(nodeID, fh)

	data, err := os.ReadFile(filepath.Join(shareDir, "hello.txt"))
	if err != nil {
		t.Fatalf("ReadFile(host share) error = %v", err)
	}
	if string(data) != "hello mounted fs" {
		t.Fatalf("host share contents = %q", string(data))
	}
}
