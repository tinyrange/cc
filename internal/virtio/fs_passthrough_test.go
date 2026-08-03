package virtio

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestPassthroughFSParentLookupAndReadDirUseParentNode(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	fsys := NewPassthroughFS(root, nil)
	aID, _, errno := fsys.Lookup(1, "a")
	if errno != 0 {
		t.Fatalf("lookup a errno = %d", errno)
	}
	bID, _, errno := fsys.Lookup(aID, "b")
	if errno != 0 {
		t.Fatalf("lookup b errno = %d", errno)
	}
	parentID, _, errno := fsys.Lookup(bID, "..")
	if errno != 0 {
		t.Fatalf("lookup .. errno = %d", errno)
	}
	if parentID != aID {
		t.Fatalf("lookup .. node = %d, want %d", parentID, aID)
	}

	fh, errno := fsys.OpenDir(bID, 0)
	if errno != 0 {
		t.Fatalf("opendir b errno = %d", errno)
	}
	defer fsys.ReleaseDir(bID, fh)
	data, errno := fsys.ReadDir(bID, fh, 0, 4096)
	if errno != 0 {
		t.Fatalf("readdir b errno = %d", errno)
	}
	entries := parseTestFuseDirents(data)
	if got := entries["."]; got != bID {
		t.Fatalf("readdir . ino = %d, want %d", got, bID)
	}
	if got := entries[".."]; got != aID {
		t.Fatalf("readdir .. ino = %d, want %d", got, aID)
	}
}

func TestPassthroughFSReadDirRefreshesWhenEnumerationRestarts(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "item.txt"), []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "folder"), 0o755); err != nil {
		t.Fatal(err)
	}
	fsys := NewPassthroughFS(root, nil)
	renameFS, ok := fsys.(fsRenameBackend)
	if !ok {
		t.Fatal("passthrough filesystem does not support rename")
	}
	rootHandle, errno := fsys.OpenDir(1, 0)
	if errno != 0 {
		t.Fatalf("opendir root errno = %d", errno)
	}
	defer fsys.ReleaseDir(1, rootHandle)

	entries := readTestPassthroughDir(t, fsys, 1, rootHandle)
	if _, ok := entries["item.txt"]; !ok {
		t.Fatalf("initial directory entries = %v", entries)
	}

	folderID, _, errno := fsys.Lookup(1, "folder")
	if errno != 0 {
		t.Fatalf("lookup folder errno = %d", errno)
	}
	if errno := renameFS.Rename(1, "item.txt", folderID, "item.txt", 0); errno != 0 {
		t.Fatalf("move into folder errno = %d", errno)
	}
	entries = readTestPassthroughDir(t, fsys, 1, rootHandle)
	if _, ok := entries["item.txt"]; ok {
		t.Fatalf("entries after move still contain item.txt: %v", entries)
	}

	if errno := renameFS.Rename(folderID, "item.txt", 1, "item.txt", 0); errno != 0 {
		t.Fatalf("move back to root errno = %d", errno)
	}
	entries = readTestPassthroughDir(t, fsys, 1, rootHandle)
	if _, ok := entries["item.txt"]; !ok {
		t.Fatalf("entries after move back = %v", entries)
	}
}

func readTestPassthroughDir(t *testing.T, fsys FSBackend, nodeID, handle uint64) map[string]uint64 {
	t.Helper()
	data, errno := fsys.ReadDir(nodeID, handle, 0, 4096)
	if errno != 0 {
		t.Fatalf("readdir errno = %d", errno)
	}
	return parseTestFuseDirents(data)
}

func parseTestFuseDirents(data []byte) map[string]uint64 {
	out := map[string]uint64{}
	for off := 0; off+fuseDirentBaseSize <= len(data); {
		ino := binary.LittleEndian.Uint64(data[off:])
		nameLen := int(binary.LittleEndian.Uint32(data[off+16:]))
		recLen := align8(fuseDirentBaseSize + nameLen)
		if off+recLen > len(data) || off+fuseDirentBaseSize+nameLen > len(data) {
			break
		}
		name := string(data[off+fuseDirentBaseSize : off+fuseDirentBaseSize+nameLen])
		out[name] = ino
		off += recLen
	}
	return out
}
