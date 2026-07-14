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
