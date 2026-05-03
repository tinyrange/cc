package virtio

import (
	"os"
	"path/filepath"
	"testing"

	"j5.nz/cc/internal/imagefs"
)

func TestMountedFSAddShareExposesFiles(t *testing.T) {
	rootDir := t.TempDir()
	shareDir := filepath.Join(t.TempDir(), "share")
	if err := os.MkdirAll(shareDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(share) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(shareDir, "hello.txt"), []byte("hello from share\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(share) error = %v", err)
	}

	backend := NewMountedFS(NewPassthroughFS(rootDir, nil), nil)
	mounter, ok := backend.(ShareMounter)
	if !ok {
		t.Fatalf("backend does not support ShareMounter")
	}
	if err := mounter.AddShare(ShareMount{
		GuestPath: "/.share/demo",
		Backend:   NewPassthroughFS(shareDir, nil),
	}); err != nil {
		t.Fatalf("AddShare() error = %v", err)
	}

	nodeID, _, errno := backendLookupPath(backend, "/.share/demo/hello.txt")
	if errno != 0 {
		t.Fatalf("backendLookupPath() errno = %d", errno)
	}
	fh, errno := backend.Open(nodeID, linuxORDONLY)
	if errno != 0 {
		t.Fatalf("Open() errno = %d", errno)
	}
	defer backend.Release(nodeID, fh)

	data, errno := backend.Read(nodeID, fh, 0, 1<<20)
	if errno != 0 {
		t.Fatalf("Read() errno = %d", errno)
	}
	if string(data) != "hello from share\n" {
		t.Fatalf("Read() = %q, want %q", string(data), "hello from share\n")
	}
}

func TestMountedFSCreateUsesReturnedBackendNodeID(t *testing.T) {
	t.Parallel()

	share := &createOnlyFS{
		backendNodeID: 99,
		backendFH:     7,
	}
	backend := NewMountedFS(NewPassthroughFS(t.TempDir(), nil), []ShareMount{{
		GuestPath: "/work",
		Backend:   share,
		Writable:  true,
	}})

	workID, _, errno := backend.Lookup(1, "work")
	if errno != 0 {
		t.Fatalf("Lookup(work) errno = %d", errno)
	}
	nodeID, fh, _, errno := backend.(fsCreateBackend).Create(workID, "created.txt", linuxOWRONLY|linuxOCREAT, 0o644)
	if errno != 0 {
		t.Fatalf("Create() errno = %d", errno)
	}
	if nodeID == 0 || fh == 0 {
		t.Fatalf("Create() = node %d fh %d, want non-zero mounted IDs", nodeID, fh)
	}
	if share.lookupCalls != 0 {
		t.Fatalf("Create() performed %d backend lookups after create, want 0", share.lookupCalls)
	}
	if wrote, errno := backend.(fsWriteBackend).Write(nodeID, fh, 0, []byte("ok"), 0); errno != 0 || wrote != 2 {
		t.Fatalf("Write() = (%d, %d), want (2, 0)", wrote, errno)
	}
}

func TestMountedFSAllowsEphemeralRootImageWrites(t *testing.T) {
	t.Parallel()

	overlay := imagefs.NewOverlay(nil)
	if err := overlay.AddDir("/opt/tool", 0o755); err != nil {
		t.Fatalf("AddDir() error = %v", err)
	}
	backend := NewMountedFS(NewImageFS(overlay.Root(), ""), nil)

	optID, _, errno := backendLookupPath(backend, "/opt/tool")
	if errno != 0 {
		t.Fatalf("backendLookupPath(/opt/tool) errno = %d", errno)
	}
	nodeID, fh, _, errno := backend.(fsCreateBackend).Create(optID, ".license", linuxOWRONLY|linuxOCREAT, 0o644)
	if errno != 0 {
		t.Fatalf("Create(.license) errno = %d", errno)
	}
	if wrote, errno := backend.(fsWriteBackend).Write(nodeID, fh, 0, []byte("license\n"), 0); errno != 0 || wrote != 8 {
		t.Fatalf("Write() = (%d, %d), want (8, 0)", wrote, errno)
	}
	backend.Release(nodeID, fh)

	nodeID, _, errno = backendLookupPath(backend, "/opt/tool/.license")
	if errno != 0 {
		t.Fatalf("backendLookupPath(.license) errno = %d", errno)
	}
	fh, errno = backend.Open(nodeID, linuxORDONLY)
	if errno != 0 {
		t.Fatalf("Open(.license) errno = %d", errno)
	}
	defer backend.Release(nodeID, fh)
	data, errno := backend.Read(nodeID, fh, 0, 1<<20)
	if errno != 0 {
		t.Fatalf("Read(.license) errno = %d", errno)
	}
	if string(data) != "license\n" {
		t.Fatalf("Read(.license) = %q, want %q", data, "license\n")
	}
}

type createOnlyFS struct {
	backendNodeID uint64
	backendFH     uint64
	lookupCalls   int
}

func (f *createOnlyFS) Init() (uint32, uint32) { return 128 << 10, 0 }

func (f *createOnlyFS) GetAttr(nodeID uint64) (FuseAttr, int32) {
	return FuseAttr{Ino: nodeID, Mode: linuxSIFDIR | 0o755, NLink: 2}, 0
}

func (f *createOnlyFS) Lookup(parent uint64, name string) (uint64, FuseAttr, int32) {
	f.lookupCalls++
	return 0, FuseAttr{}, -linuxENOENT
}

func (f *createOnlyFS) Open(nodeID uint64, flags uint32) (uint64, int32) {
	return 0, -linuxENOENT
}

func (f *createOnlyFS) Release(nodeID uint64, fh uint64) {}

func (f *createOnlyFS) Read(nodeID uint64, fh uint64, off uint64, size uint32) ([]byte, int32) {
	return nil, -linuxENOENT
}

func (f *createOnlyFS) OpenDir(nodeID uint64, flags uint32) (uint64, int32) {
	return 0, -linuxENOENT
}

func (f *createOnlyFS) ReadDir(nodeID uint64, fh uint64, off uint64, maxBytes uint32) ([]byte, int32) {
	return nil, -linuxENOENT
}

func (f *createOnlyFS) ReleaseDir(nodeID uint64, fh uint64) {}

func (f *createOnlyFS) Readlink(nodeID uint64) (string, int32) {
	return "", -linuxENOENT
}

func (f *createOnlyFS) StatFS(nodeID uint64) (blocks, bfree, bavail, files, ffree, bsize, frsize, namelen uint64, errno int32) {
	return 0, 0, 0, 0, 0, 4096, 4096, 255, 0
}

func (f *createOnlyFS) Create(parent uint64, name string, flags uint32, mode uint32) (uint64, uint64, FuseAttr, int32) {
	return f.backendNodeID, f.backendFH, FuseAttr{Ino: f.backendNodeID, Mode: linuxSIFREG | mode, NLink: 1}, 0
}

func (f *createOnlyFS) Write(nodeID uint64, fh uint64, off uint64, data []byte, flags uint32) (uint32, int32) {
	if nodeID != f.backendNodeID || fh != f.backendFH {
		return 0, -linuxEBADF
	}
	return uint32(len(data)), 0
}
