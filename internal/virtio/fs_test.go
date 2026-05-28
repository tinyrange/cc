package virtio

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

	"j5.nz/cc/internal/fsmeta"
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

	nodeID, fh, _, errno := be.Create(1, "hello.txt", linuxOWRONLY|linuxOCREAT|linuxOTRUNC, 0o644, 0, 0)
	if errno != 0 {
		t.Fatalf("Create() errno = %d", errno)
	}
	if wrote, errno := be.Write(nodeID, fh, 0, []byte("hello world"), 0); errno != 0 || wrote != 11 {
		t.Fatalf("Write() = (%d, %d)", wrote, errno)
	}
	if errno := be.Flush(nodeID, fh, 0); errno != 0 {
		t.Fatalf("Flush() errno = %d", errno)
	}
	if errno := be.Fsync(nodeID, fh, 0); errno != 0 {
		t.Fatalf("Fsync() errno = %d", errno)
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

func TestPassthroughFSAppendWritesAppendInsteadOfWriteAt(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	fs := NewPassthroughFS(root, nil)
	be, ok := fs.(*passthroughFS)
	if !ok {
		t.Fatalf("backend type = %T", fs)
	}

	nodeID, fh, _, errno := be.Create(1, "append.txt", linuxOWRONLY|linuxOCREAT|linuxOTRUNC, 0o644, 0, 0)
	if errno != 0 {
		t.Fatalf("Create() errno = %d", errno)
	}
	if wrote, errno := be.Write(nodeID, fh, 0, []byte("base"), 0); errno != 0 || wrote != 4 {
		t.Fatalf("initial Write() = (%d, %d)", wrote, errno)
	}
	be.Release(nodeID, fh)

	fh, errno = be.Open(nodeID, linuxOWRONLY|linuxOAPPEND)
	if errno != 0 {
		t.Fatalf("Open(O_APPEND) errno = %d", errno)
	}
	if wrote, errno := be.Write(nodeID, fh, 0, []byte("+tail"), 0); errno != 0 || wrote != 5 {
		t.Fatalf("append Write() = (%d, %d)", wrote, errno)
	}
	be.Release(nodeID, fh)

	data, err := os.ReadFile(filepath.Join(root, "append.txt"))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(data) != "base+tail" {
		t.Fatalf("ReadFile() = %q", data)
	}
}

func TestImageFSSequentialWritesGrowAmortized(t *testing.T) {
	t.Parallel()

	backend := NewImageFS(imagefs.NewHostFS(t.TempDir(), nil), "")
	be, ok := backend.(*imageFS)
	if !ok {
		t.Fatalf("backend type = %T", backend)
	}
	nodeID, fh, _, errno := be.Create(1, "large.bin", linuxOWRONLY|linuxOCREAT|linuxOTRUNC, 0o644, 0, 0)
	if errno != 0 {
		t.Fatalf("Create() errno = %d", errno)
	}
	defer be.Release(nodeID, fh)

	chunk := make([]byte, 64<<10)
	for i := range chunk {
		chunk[i] = byte(i)
	}
	const chunks = 7
	for i := 0; i < chunks; i++ {
		if wrote, errno := be.Write(nodeID, fh, uint64(i*len(chunk)), chunk, 0); errno != 0 || wrote != uint32(len(chunk)) {
			t.Fatalf("Write(%d) = (%d, %d)", i, wrote, errno)
		}
	}

	be.mu.Lock()
	node := be.nodes[nodeID]
	size, dataLen, dataCap := node.size, len(node.data), cap(node.data)
	be.mu.Unlock()
	if size != uint64(chunks*len(chunk)) || dataLen != int(size) {
		t.Fatalf("written size=%d len=%d, want %d", size, dataLen, chunks*len(chunk))
	}
	if dataCap <= dataLen {
		t.Fatalf("data cap=%d, len=%d; want spare capacity for sequential writes", dataCap, dataLen)
	}
}

func TestImageFSSetAttrPreservesDirectoryType(t *testing.T) {
	t.Parallel()

	backend := NewImageFS(imagefs.NewHostFS(t.TempDir(), nil), "")
	be := backend.(*imageFS)
	nodeID, _, errno := be.Mkdir(1, "runtime", 0o755, 0, 0)
	if errno != 0 {
		t.Fatalf("Mkdir() errno = %d", errno)
	}
	if _, errno := be.SetAttr(nodeID, fattrMode, 0, 0, 0o700, 0, 0, time.Time{}, time.Time{}); errno != 0 {
		t.Fatalf("SetAttr(chmod) errno = %d", errno)
	}
	attr, errno := be.GetAttr(nodeID)
	if errno != 0 {
		t.Fatalf("GetAttr() errno = %d", errno)
	}
	if attr.Mode&linuxSIFMT != linuxSIFDIR {
		t.Fatalf("mode after chmod = %#o, want directory", attr.Mode)
	}
	be.mu.Lock()
	nodeMode := be.nodes[nodeID].mode
	be.mu.Unlock()
	if nodeMode.Perm() != 0o700 {
		t.Fatalf("stored permissions after chmod = %#o, want 0700", nodeMode.Perm())
	}
}

func TestFUSESetAttrParsesUIDAndGID(t *testing.T) {
	t.Parallel()

	backend := NewImageFS(imagefs.NewHostFS(t.TempDir(), nil), "")
	be := backend.(*imageFS)
	nodeID, _, errno := be.Mkdir(1, "home", 0o755, 0, 0)
	if errno != 0 {
		t.Fatalf("Mkdir() errno = %d", errno)
	}
	fsdev := NewFS(0, 0, 0, "root", backend)

	req := make([]byte, fuseInHeaderSize+88)
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.LittleEndian.PutUint32(req[4:8], fuseSetAttr)
	binary.LittleEndian.PutUint64(req[8:16], 99)
	binary.LittleEndian.PutUint64(req[16:24], nodeID)
	binary.LittleEndian.PutUint32(req[40:44], fattrUID|fattrGID)
	binary.LittleEndian.PutUint32(req[116:120], 1000)
	binary.LittleEndian.PutUint32(req[120:124], 100)

	reply, err := fsdev.dispatchFUSELocked(req)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(SETATTR) error = %v", err)
	}
	if got := int32(binary.LittleEndian.Uint32(reply[4:8])); got != 0 {
		t.Fatalf("SETATTR errno = %d, want 0", got)
	}
	attr, errno := be.GetAttr(nodeID)
	if errno != 0 {
		t.Fatalf("GetAttr() errno = %d", errno)
	}
	if attr.UID != 1000 || attr.GID != 100 {
		t.Fatalf("owner = %d:%d, want 1000:100", attr.UID, attr.GID)
	}
}

func TestFUSECreateUsesCallerUIDAndGID(t *testing.T) {
	t.Parallel()

	backend := NewImageFS(imagefs.NewHostFS(t.TempDir(), nil), "")
	fsdev := NewFS(0, 0, 0, "root", backend)

	name := "created-by-user.txt"
	req := make([]byte, fuseInHeaderSize+16+len(name)+1)
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.LittleEndian.PutUint32(req[4:8], fuseCreate)
	binary.LittleEndian.PutUint64(req[8:16], 100)
	binary.LittleEndian.PutUint64(req[16:24], 1)
	binary.LittleEndian.PutUint32(req[24:28], 1000)
	binary.LittleEndian.PutUint32(req[28:32], 100)
	binary.LittleEndian.PutUint32(req[40:44], linuxOWRONLY|linuxOCREAT)
	binary.LittleEndian.PutUint32(req[44:48], 0o644)
	copy(req[fuseInHeaderSize+16:], name)

	reply, err := fsdev.dispatchFUSELocked(req)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(CREATE) error = %v", err)
	}
	if got := int32(binary.LittleEndian.Uint32(reply[4:8])); got != 0 {
		t.Fatalf("CREATE errno = %d, want 0", got)
	}
	childID := binary.LittleEndian.Uint64(reply[fuseOutHeaderSize : fuseOutHeaderSize+8])
	attr, errno := backend.GetAttr(childID)
	if errno != 0 {
		t.Fatalf("GetAttr(created) errno = %d", errno)
	}
	if attr.UID != 1000 || attr.GID != 100 {
		t.Fatalf("created owner = %d:%d, want 1000:100", attr.UID, attr.GID)
	}
}

func TestImageFSMknodCreatesSocketNode(t *testing.T) {
	t.Parallel()

	backend := NewImageFS(imagefs.NewHostFS(t.TempDir(), nil), "")
	be := backend.(*imageFS)
	nodeID, attr, errno := be.Mknod(1, "kubelet.sock", linuxSIFSOCK|0o600, 0, 0, 0)
	if errno != 0 {
		t.Fatalf("Mknod() errno = %d", errno)
	}
	if attr.Mode&linuxSIFMT != linuxSIFSOCK {
		t.Fatalf("Mknod() mode = %#o, want socket", attr.Mode)
	}
	if attr.Mode&linuxPermMask != 0o600 {
		t.Fatalf("Mknod() permissions = %#o, want 0600", attr.Mode&linuxPermMask)
	}
	if got, _, errno := be.Lookup(1, "kubelet.sock"); errno != 0 || got != nodeID {
		t.Fatalf("Lookup() = (%d, %d), want node %d", got, errno, nodeID)
	}
	fh, errno := be.OpenDir(1, 0)
	if errno != 0 {
		t.Fatalf("OpenDir() errno = %d", errno)
	}
	defer be.ReleaseDir(1, fh)
	entries, errno := be.ReadDir(1, fh, 0, 4096)
	if errno != 0 {
		t.Fatalf("ReadDir() errno = %d", errno)
	}
	if !direntHasType(entries, "kubelet.sock", dirTypeSocket) {
		t.Fatalf("ReadDir() missing socket dirent for kubelet.sock")
	}
}

func TestStrictFUSEMknodCreatesImageSocket(t *testing.T) {
	t.Parallel()

	backend := NewImageFS(imagefs.NewHostFS(t.TempDir(), nil), "")
	fsdev := NewFS(0, 0, 0, "root", backend)
	fsdev.Strict = true

	const unique = uint64(48)
	payload := make([]byte, 16+len("pod-resources.sock")+1)
	binary.LittleEndian.PutUint32(payload[0:4], linuxSIFSOCK|0o600)
	copy(payload[16:], "pod-resources.sock\x00")
	req := make([]byte, fuseInHeaderSize+len(payload))
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.LittleEndian.PutUint32(req[4:8], fuseMknod)
	binary.LittleEndian.PutUint64(req[8:16], unique)
	binary.LittleEndian.PutUint64(req[16:24], 1)
	copy(req[fuseInHeaderSize:], payload)

	reply, err := fsdev.dispatchFUSELocked(req)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(MKNOD) error = %v", err)
	}
	if got := int32(binary.LittleEndian.Uint32(reply[4:8])); got != 0 {
		t.Fatalf("MKNOD errno = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint64(reply[8:16]); got != unique {
		t.Fatalf("MKNOD unique = %d, want %d", got, unique)
	}
	attrMode := binary.LittleEndian.Uint32(reply[fuseOutHeaderSize+40+60 : fuseOutHeaderSize+40+64])
	if attrMode&linuxSIFMT != linuxSIFSOCK {
		t.Fatalf("MKNOD attr mode = %#o, want socket", attrMode)
	}
}

func direntHasType(entries []byte, name string, typ uint32) bool {
	for off := 0; off+fuseDirentBaseSize <= len(entries); {
		nameLen := int(binary.LittleEndian.Uint32(entries[off+16 : off+20]))
		entryType := binary.LittleEndian.Uint32(entries[off+20 : off+24])
		reclen := align8(fuseDirentBaseSize + nameLen)
		if off+reclen > len(entries) || off+fuseDirentBaseSize+nameLen > len(entries) {
			return false
		}
		if string(entries[off+fuseDirentBaseSize:off+fuseDirentBaseSize+nameLen]) == name && entryType == typ {
			return true
		}
		off += reclen
	}
	return false
}

func TestStrictFUSEFsyncUsesBackendHandle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	backend := NewPassthroughFS(root, nil)
	be := backend.(*passthroughFS)
	nodeID, fh, _, errno := be.Create(1, "data.txt", linuxOWRONLY|linuxOCREAT|linuxOTRUNC, 0o644, 0, 0)
	if errno != 0 {
		t.Fatalf("Create() errno = %d", errno)
	}
	defer be.Release(nodeID, fh)

	fsdev := NewFS(0, 0, 0, "root", backend)
	fsdev.Strict = true

	const unique = uint64(44)
	req := make([]byte, fuseInHeaderSize+16)
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.LittleEndian.PutUint32(req[4:8], fuseFsync)
	binary.LittleEndian.PutUint64(req[8:16], unique)
	binary.LittleEndian.PutUint64(req[16:24], nodeID)
	binary.LittleEndian.PutUint64(req[40:48], fh)

	reply, err := fsdev.dispatchFUSELocked(req)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(FSYNC) error = %v", err)
	}
	if got := int32(binary.LittleEndian.Uint32(reply[4:8])); got != 0 {
		t.Fatalf("FSYNC errno = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint64(reply[8:16]); got != unique {
		t.Fatalf("FSYNC unique = %d, want %d", got, unique)
	}
}

func TestStrictFUSESymlinkCreatesHostSymlink(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	backend := NewPassthroughFS(root, nil)
	fsdev := NewFS(0, 0, 0, "root", backend)
	fsdev.Strict = true

	const unique = uint64(45)
	payload := []byte("linked.txt\x00target.txt\x00")
	req := make([]byte, fuseInHeaderSize+len(payload))
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.LittleEndian.PutUint32(req[4:8], fuseSymlink)
	binary.LittleEndian.PutUint64(req[8:16], unique)
	binary.LittleEndian.PutUint64(req[16:24], 1)
	copy(req[fuseInHeaderSize:], payload)

	reply, err := fsdev.dispatchFUSELocked(req)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(SYMLINK) error = %v", err)
	}
	if got := int32(binary.LittleEndian.Uint32(reply[4:8])); got != 0 {
		t.Fatalf("SYMLINK errno = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint64(reply[8:16]); got != unique {
		t.Fatalf("SYMLINK unique = %d, want %d", got, unique)
	}
	target, err := os.Readlink(filepath.Join(root, "linked.txt"))
	if err != nil {
		t.Fatalf("Readlink() error = %v", err)
	}
	if target != "target.txt" {
		t.Fatalf("Readlink() = %q, want target.txt", target)
	}
}

func TestStrictFUSESetLKAndGetLK(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	backend := NewPassthroughFS(root, nil)
	be := backend.(*passthroughFS)
	nodeID, fh, _, errno := be.Create(1, "locked.sqlite", linuxORDWR|linuxOCREAT, 0o644, 0, 0)
	if errno != 0 {
		t.Fatalf("Create() errno = %d", errno)
	}
	defer be.Release(nodeID, fh)

	fsdev := NewFS(0, 0, 0, "root", backend)
	fsdev.Strict = true

	setReq := make([]byte, fuseInHeaderSize+fuseLKInSize)
	binary.LittleEndian.PutUint32(setReq[0:4], uint32(len(setReq)))
	binary.LittleEndian.PutUint32(setReq[4:8], fuseSetLK)
	binary.LittleEndian.PutUint64(setReq[8:16], 46)
	binary.LittleEndian.PutUint64(setReq[16:24], nodeID)
	binary.LittleEndian.PutUint64(setReq[40:48], fh)
	binary.LittleEndian.PutUint32(setReq[72:76], 1)
	reply, err := fsdev.dispatchFUSELocked(setReq)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(SETLK) error = %v", err)
	}
	if got := int32(binary.LittleEndian.Uint32(reply[4:8])); got != 0 {
		t.Fatalf("SETLK errno = %d, want 0", got)
	}

	getReq := make([]byte, fuseInHeaderSize+fuseLKInSize)
	binary.LittleEndian.PutUint32(getReq[0:4], uint32(len(getReq)))
	binary.LittleEndian.PutUint32(getReq[4:8], fuseGetLK)
	binary.LittleEndian.PutUint64(getReq[8:16], 47)
	binary.LittleEndian.PutUint64(getReq[16:24], nodeID)
	binary.LittleEndian.PutUint64(getReq[40:48], fh)
	reply, err = fsdev.dispatchFUSELocked(getReq)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(GETLK) error = %v", err)
	}
	if got := int32(binary.LittleEndian.Uint32(reply[4:8])); got != 0 {
		t.Fatalf("GETLK errno = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint32(reply[fuseOutHeaderSize+16 : fuseOutHeaderSize+20]); got != linuxFUnlck {
		t.Fatalf("GETLK type = %d, want F_UNLCK", got)
	}
}

func TestFSAsyncQueueCompletesLongResponseChain(t *testing.T) {
	mem := &testGuestMemory{data: make([]byte, 0x8000)}
	irq := &testIRQController{}

	fsdev := NewFS(0x1000, 0x1000, 44, "root", NewPassthroughFS(t.TempDir(), nil))
	fsdev.Strict = true
	fsdev.Async = true
	fsdev.Attach(mem, irq)

	const (
		descAddr  = 0x2000
		availAddr = 0x3000
		usedAddr  = 0x3100
		reqAddr   = 0x3200
		respAddr  = 0x4000
	)
	req := make([]byte, fuseInHeaderSize+16)
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.LittleEndian.PutUint32(req[4:8], fuseInit)
	binary.LittleEndian.PutUint64(req[8:16], 99)
	binary.LittleEndian.PutUint32(req[40:44], 7)
	binary.LittleEndian.PutUint32(req[44:48], 31)
	copy(mem.data[reqAddr:], req)

	binary.LittleEndian.PutUint64(mem.data[descAddr:descAddr+8], reqAddr)
	binary.LittleEndian.PutUint32(mem.data[descAddr+8:descAddr+12], uint32(len(req)))
	binary.LittleEndian.PutUint16(mem.data[descAddr+12:descAddr+14], descFNext)
	binary.LittleEndian.PutUint16(mem.data[descAddr+14:descAddr+16], 1)
	for i := 0; i < fuseOutHeaderSize+fuseInitOutSize; i++ {
		descOff := descAddr + 16 + i*16
		flags := uint16(descFWrite)
		if i != fuseOutHeaderSize+fuseInitOutSize-1 {
			flags |= descFNext
		}
		binary.LittleEndian.PutUint64(mem.data[descOff:descOff+8], respAddr+uint64(i))
		binary.LittleEndian.PutUint32(mem.data[descOff+8:descOff+12], 1)
		binary.LittleEndian.PutUint16(mem.data[descOff+12:descOff+14], flags)
		binary.LittleEndian.PutUint16(mem.data[descOff+14:descOff+16], uint16(i+2))
	}
	binary.LittleEndian.PutUint16(mem.data[availAddr+2:availAddr+4], 1)
	binary.LittleEndian.PutUint16(mem.data[availAddr+4:availAddr+6], 0)

	for _, write := range []struct {
		reg   uint64
		value uint64
	}{
		{regQueueSel, fsQueueRequest},
		{regQueueNum, 128},
		{regQueueDescLow, descAddr},
		{regQueueAvailLow, availAddr},
		{regQueueUsedLow, usedAddr},
		{regQueueReady, 1},
		{regQueueNotify, fsQueueRequest},
	} {
		if err := fsdev.Write(0x1000+write.reg, 4, write.value); err != nil {
			t.Fatalf("Write(reg=%#x) error = %v", write.reg, err)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	for binary.LittleEndian.Uint16(mem.data[usedAddr+2:usedAddr+4]) != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if usedIdx := binary.LittleEndian.Uint16(mem.data[usedAddr+2 : usedAddr+4]); usedIdx != 1 {
		t.Fatalf("used idx = %d, want 1", usedIdx)
	}
	if usedLen := binary.LittleEndian.Uint32(mem.data[usedAddr+8 : usedAddr+12]); usedLen != fuseOutHeaderSize+fuseInitOutSize {
		t.Fatalf("used len = %d, want %d", usedLen, fuseOutHeaderSize+fuseInitOutSize)
	}
	if irq.calls == 0 || !irq.level || irq.irq != 44 {
		t.Fatalf("irq state = irq=%d level=%v calls=%d, want irq=44 asserted", irq.irq, irq.level, irq.calls)
	}
	if got := binary.LittleEndian.Uint64(mem.data[respAddr+8 : respAddr+16]); got != 99 {
		t.Fatalf("reply unique = %d, want 99", got)
	}
}

func TestFSConfigReportsMultipleRequestQueues(t *testing.T) {
	fsdev := NewFS(0x1000, 0x1000, 44, "root", NewPassthroughFS(t.TempDir(), nil))

	cfg := fsdev.configBytesLocked()
	if got := binary.LittleEndian.Uint32(cfg[fsCfgNumQueueOff : fsCfgNumQueueOff+4]); got != fsRequestQueueCount {
		t.Fatalf("num_request_queues = %d, want %d", got, fsRequestQueueCount)
	}
	for qidx := 0; qidx < fsQueueCount; qidx++ {
		if err := fsdev.Write(0x1000+regQueueSel, 4, uint64(qidx)); err != nil {
			t.Fatalf("Write(queue-sel %d) error = %v", qidx, err)
		}
		got, err := fsdev.Read(0x1000+regQueueNumMax, 4)
		if err != nil {
			t.Fatalf("Read(queue-num-max %d) error = %v", qidx, err)
		}
		if got != 128 {
			t.Fatalf("queue %d num max = %d, want 128", qidx, got)
		}
	}
	if err := fsdev.Write(0x1000+regQueueSel, 4, uint64(fsQueueCount)); err != nil {
		t.Fatalf("Write(queue-sel out of range) error = %v", err)
	}
	got, err := fsdev.Read(0x1000+regQueueNumMax, 4)
	if err != nil {
		t.Fatalf("Read(queue-num-max out of range) error = %v", err)
	}
	if got != 0 {
		t.Fatalf("out of range queue num max = %d, want 0", got)
	}
}

func TestFSSyncSecondaryRequestQueueCompletes(t *testing.T) {
	mem := &testGuestMemory{data: make([]byte, 0x8000)}
	irq := &testIRQController{}

	fsdev := NewFS(0x1000, 0x1000, 44, "root", NewPassthroughFS(t.TempDir(), nil))
	fsdev.Strict = true
	fsdev.Attach(mem, irq)

	const (
		qidx      = fsQueueRequest + 1
		descAddr  = 0x2000
		availAddr = 0x3000
		usedAddr  = 0x3100
		reqAddr   = 0x3200
		respAddr  = 0x3300
		queueSize = 8
	)
	req := make([]byte, fuseInHeaderSize+16)
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.LittleEndian.PutUint32(req[4:8], fuseInit)
	binary.LittleEndian.PutUint64(req[8:16], 202)
	binary.LittleEndian.PutUint32(req[40:44], 7)
	binary.LittleEndian.PutUint32(req[44:48], 31)
	copy(mem.data[reqAddr:], req)

	binary.LittleEndian.PutUint64(mem.data[descAddr:descAddr+8], reqAddr)
	binary.LittleEndian.PutUint32(mem.data[descAddr+8:descAddr+12], uint32(len(req)))
	binary.LittleEndian.PutUint16(mem.data[descAddr+12:descAddr+14], descFNext)
	binary.LittleEndian.PutUint16(mem.data[descAddr+14:descAddr+16], 1)
	binary.LittleEndian.PutUint64(mem.data[descAddr+16:descAddr+24], respAddr)
	binary.LittleEndian.PutUint32(mem.data[descAddr+24:descAddr+28], fuseOutHeaderSize+fuseInitOutSize)
	binary.LittleEndian.PutUint16(mem.data[descAddr+28:descAddr+30], descFWrite)

	binary.LittleEndian.PutUint16(mem.data[availAddr+2:availAddr+4], 1)
	binary.LittleEndian.PutUint16(mem.data[availAddr+4:availAddr+6], 0)

	for _, write := range []struct {
		reg   uint64
		value uint64
	}{
		{regQueueSel, qidx},
		{regQueueNum, queueSize},
		{regQueueDescLow, descAddr},
		{regQueueAvailLow, availAddr},
		{regQueueUsedLow, usedAddr},
		{regQueueReady, 1},
		{regQueueNotify, qidx},
	} {
		if err := fsdev.Write(0x1000+write.reg, 4, write.value); err != nil {
			t.Fatalf("Write(reg=%#x) error = %v", write.reg, err)
		}
	}

	if usedIdx := binary.LittleEndian.Uint16(mem.data[usedAddr+2 : usedAddr+4]); usedIdx != 1 {
		t.Fatalf("used idx = %d, want 1", usedIdx)
	}
	if irq.calls == 0 || !irq.level || irq.irq != 44 {
		t.Fatalf("irq state = irq=%d level=%v calls=%d, want irq=44 asserted", irq.irq, irq.level, irq.calls)
	}
	if got := binary.LittleEndian.Uint64(mem.data[respAddr+8 : respAddr+16]); got != 202 {
		t.Fatalf("reply unique = %d, want 202", got)
	}
}

func TestFSSyncQueueHonorsUsedEventIdx(t *testing.T) {
	mem := &testGuestMemory{data: make([]byte, 0x8000)}
	irq := &testIRQController{}

	fsdev := NewFS(0x1000, 0x1000, 44, "root", NewPassthroughFS(t.TempDir(), nil))
	fsdev.Strict = true
	fsdev.driverFeatures = featureRingEventIdx
	fsdev.Attach(mem, irq)

	const (
		descAddr  = 0x2000
		availAddr = 0x3000
		usedAddr  = 0x3100
		reqAddr   = 0x3200
		respAddr  = 0x3300
		queueSize = 8
	)
	req := make([]byte, fuseInHeaderSize+16)
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.LittleEndian.PutUint32(req[4:8], fuseInit)
	binary.LittleEndian.PutUint64(req[8:16], 101)
	binary.LittleEndian.PutUint32(req[40:44], 7)
	binary.LittleEndian.PutUint32(req[44:48], 31)
	copy(mem.data[reqAddr:], req)

	binary.LittleEndian.PutUint64(mem.data[descAddr:descAddr+8], reqAddr)
	binary.LittleEndian.PutUint32(mem.data[descAddr+8:descAddr+12], uint32(len(req)))
	binary.LittleEndian.PutUint16(mem.data[descAddr+12:descAddr+14], descFNext)
	binary.LittleEndian.PutUint16(mem.data[descAddr+14:descAddr+16], 1)
	binary.LittleEndian.PutUint64(mem.data[descAddr+16:descAddr+24], respAddr)
	binary.LittleEndian.PutUint32(mem.data[descAddr+24:descAddr+28], fuseOutHeaderSize+fuseInitOutSize)
	binary.LittleEndian.PutUint16(mem.data[descAddr+28:descAddr+30], descFWrite)

	binary.LittleEndian.PutUint16(mem.data[availAddr+2:availAddr+4], 1)
	binary.LittleEndian.PutUint16(mem.data[availAddr+4:availAddr+6], 0)
	binary.LittleEndian.PutUint16(mem.data[availAddr+4+queueSize*2:availAddr+6+queueSize*2], 1)

	for _, write := range []struct {
		reg   uint64
		value uint64
	}{
		{regQueueSel, fsQueueRequest},
		{regQueueNum, queueSize},
		{regQueueDescLow, descAddr},
		{regQueueAvailLow, availAddr},
		{regQueueUsedLow, usedAddr},
		{regQueueReady, 1},
		{regQueueNotify, fsQueueRequest},
	} {
		if err := fsdev.Write(0x1000+write.reg, 4, write.value); err != nil {
			t.Fatalf("Write(reg=%#x) error = %v", write.reg, err)
		}
	}

	if usedIdx := binary.LittleEndian.Uint16(mem.data[usedAddr+2 : usedAddr+4]); usedIdx != 1 {
		t.Fatalf("used idx = %d, want 1", usedIdx)
	}
	if irq.calls != 0 {
		t.Fatalf("irq calls = %d, want 0", irq.calls)
	}
}

func TestVringNeedEvent(t *testing.T) {
	tests := []struct {
		name  string
		event uint16
		new   uint16
		old   uint16
		want  bool
	}{
		{name: "next completion requested", event: 0, new: 1, old: 0, want: true},
		{name: "future completion suppresses", event: 1, new: 1, old: 0, want: false},
		{name: "wrap requested", event: 0xffff, new: 0, old: 0xffff, want: true},
		{name: "wrap future suppresses", event: 0, new: 0, old: 0xffff, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := vringNeedEvent(tt.event, tt.new, tt.old); got != tt.want {
				t.Fatalf("vringNeedEvent(%d, %d, %d) = %v, want %v", tt.event, tt.new, tt.old, got, tt.want)
			}
		})
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

func TestImageFSWithOwnerMapsAttributes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "tool"), []byte("tool"), 0o600); err != nil {
		t.Fatal(err)
	}
	be := NewImageFSWithOwner(imagefs.NewHostFS(root, map[string]fsmeta.Entry{
		"/":     {UID: 1000, GID: 1000, Mode: linuxSIFDIR | 0o700},
		"/tool": {UID: 1000, GID: 1000, Mode: linuxSIFREG | 0o600},
	}), root, 1001, 1002).(*imageFS)

	rootAttr, errno := be.GetAttr(1)
	if errno != 0 {
		t.Fatalf("GetAttr(root) errno = %d", errno)
	}
	if rootAttr.UID != 1001 || rootAttr.GID != 1002 {
		t.Fatalf("root owner = %d:%d, want 1001:1002", rootAttr.UID, rootAttr.GID)
	}
	toolID, toolAttr, errno := be.Lookup(1, "tool")
	if errno != 0 {
		t.Fatalf("Lookup(tool) errno = %d", errno)
	}
	if toolAttr.UID != 1001 || toolAttr.GID != 1002 {
		t.Fatalf("tool lookup owner = %d:%d, want 1001:1002", toolAttr.UID, toolAttr.GID)
	}
	toolAttr, errno = be.GetAttr(toolID)
	if errno != 0 {
		t.Fatalf("GetAttr(tool) errno = %d", errno)
	}
	if toolAttr.UID != 1001 || toolAttr.GID != 1002 {
		t.Fatalf("tool getattr owner = %d:%d, want 1001:1002", toolAttr.UID, toolAttr.GID)
	}
}

func TestImageFSPreservesDirectoryPermissions(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "private"), 0o700); err != nil {
		t.Fatal(err)
	}
	be := NewImageFS(imagefs.NewHostFS(root, nil), root)
	_, attr, errno := be.Lookup(1, "private")
	if errno != 0 {
		t.Fatalf("Lookup(private) errno = %d", errno)
	}
	if attr.Mode&linuxPermMask != 0o700 {
		t.Fatalf("private mode = %#o, want 0700", attr.Mode&linuxPermMask)
	}
}

func TestPassthroughFSWithOwnerMapsAttributes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "private"), 0o700); err != nil {
		t.Fatal(err)
	}
	be := NewPassthroughFSWithOwner(root, nil, 1001, 1002)
	rootAttr, errno := be.GetAttr(1)
	if errno != 0 {
		t.Fatalf("GetAttr(root) errno = %d", errno)
	}
	if rootAttr.UID != 1001 || rootAttr.GID != 1002 {
		t.Fatalf("root owner = %d:%d, want 1001:1002", rootAttr.UID, rootAttr.GID)
	}
	_, attr, errno := be.Lookup(1, "private")
	if errno != 0 {
		t.Fatalf("Lookup(private) errno = %d", errno)
	}
	if attr.UID != 1001 || attr.GID != 1002 {
		t.Fatalf("private owner = %d:%d, want 1001:1002", attr.UID, attr.GID)
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

func TestStrictFUSETmpfileReturnsENOSYS(t *testing.T) {
	t.Parallel()

	fsdev := NewFS(0, 0, 0, "root", NewPassthroughFS(t.TempDir(), nil))
	fsdev.Strict = true

	const unique = uint64(44)
	req := make([]byte, fuseInHeaderSize+16)
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.LittleEndian.PutUint32(req[4:8], fuseTmpfile)
	binary.LittleEndian.PutUint64(req[8:16], unique)
	binary.LittleEndian.PutUint64(req[16:24], 1)

	reply, err := fsdev.dispatchFUSELocked(req)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(TMPFILE) error = %v", err)
	}
	if got := int32(binary.LittleEndian.Uint32(reply[4:8])); got != -linuxENOSYS {
		t.Fatalf("TMPFILE errno = %d, want %d", got, -linuxENOSYS)
	}
}

func TestStrictFUSEUnknownOpcodeReturnsENOSYS(t *testing.T) {
	t.Parallel()

	fsdev := NewFS(0, 0, 0, "root", NewPassthroughFS(t.TempDir(), nil))
	fsdev.Strict = true

	const unique = uint64(45)
	req := make([]byte, fuseInHeaderSize)
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.LittleEndian.PutUint32(req[4:8], 53)
	binary.LittleEndian.PutUint64(req[8:16], unique)
	binary.LittleEndian.PutUint64(req[16:24], 1)

	reply, err := fsdev.dispatchFUSELocked(req)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(unknown opcode) error = %v", err)
	}
	if got := int32(binary.LittleEndian.Uint32(reply[4:8])); got != -linuxENOSYS {
		t.Fatalf("unknown opcode errno = %d, want %d", got, -linuxENOSYS)
	}
	if got := binary.LittleEndian.Uint64(reply[8:16]); got != unique {
		t.Fatalf("unknown opcode unique = %d, want %d", got, unique)
	}
}

func TestFUSEInitAdvertisesImplementedCapabilities(t *testing.T) {
	t.Parallel()

	backends := []struct {
		name    string
		backend FSBackend
	}{
		{name: "passthrough", backend: NewPassthroughFS(t.TempDir(), nil)},
		{name: "image", backend: NewImageFS(imagefs.NewHostFS(t.TempDir(), nil), "")},
	}
	for _, tc := range backends {
		_, flags := tc.backend.Init()
		if flags != fuseCapPosixLocks {
			t.Fatalf("%s Init flags = %#x, want %#x", tc.name, flags, fuseCapPosixLocks)
		}
	}
}

func TestImageFSCopyUpExistingFileOnWrite(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "usr", "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "usr", "bin", "tool"), []byte("base"), 0o755); err != nil {
		t.Fatalf("WriteFile(base) error = %v", err)
	}
	be := NewImageFS(imagefs.NewHostFS(root, nil), root).(*imageFS)

	usrID, _, errno := be.Lookup(1, "usr")
	if errno != 0 {
		t.Fatalf("Lookup(usr) errno = %d", errno)
	}
	binID, _, errno := be.Lookup(usrID, "bin")
	if errno != 0 {
		t.Fatalf("Lookup(bin) errno = %d", errno)
	}
	toolID, _, errno := be.Lookup(binID, "tool")
	if errno != 0 {
		t.Fatalf("Lookup(tool) errno = %d", errno)
	}
	fh, errno := be.Open(toolID, linuxOWRONLY|linuxOTRUNC)
	if errno != 0 {
		t.Fatalf("Open(tool O_TRUNC) errno = %d", errno)
	}
	if wrote, errno := be.Write(toolID, fh, 0, []byte("overlay"), 0); errno != 0 || wrote != 7 {
		t.Fatalf("Write(tool) = (%d, %d), want (7, 0)", wrote, errno)
	}
	be.Release(toolID, fh)

	fh, errno = be.Open(toolID, linuxORDONLY)
	if errno != 0 {
		t.Fatalf("Open(tool read) errno = %d", errno)
	}
	data, errno := be.Read(toolID, fh, 0, 32)
	if errno != 0 {
		t.Fatalf("Read(tool) errno = %d", errno)
	}
	if string(data) != "overlay" {
		t.Fatalf("overlay data = %q, want overlay", data)
	}
	if base, err := os.ReadFile(filepath.Join(root, "usr", "bin", "tool")); err != nil || string(base) != "base" {
		t.Fatalf("base file = %q, %v; want base unchanged", base, err)
	}
}

func TestImageFSUnlinkWhiteoutsLowerFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "etc"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "etc", "motd"), []byte("base"), 0o644); err != nil {
		t.Fatalf("WriteFile(base) error = %v", err)
	}
	be := NewImageFS(imagefs.NewHostFS(root, nil), root).(*imageFS)
	etcID, _, errno := be.Lookup(1, "etc")
	if errno != 0 {
		t.Fatalf("Lookup(etc) errno = %d", errno)
	}
	if _, _, errno := be.Lookup(etcID, "motd"); errno != 0 {
		t.Fatalf("Lookup(motd) errno = %d", errno)
	}
	if errno := be.Unlink(etcID, "motd"); errno != 0 {
		t.Fatalf("Unlink(motd) errno = %d", errno)
	}
	if _, _, errno := be.Lookup(etcID, "motd"); errno != -linuxENOENT {
		t.Fatalf("Lookup(motd after unlink) errno = %d, want ENOENT", errno)
	}
}

func TestImageFSRenameReplacesLowerFileInOverlay(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "usr", "bin"), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "usr", "bin", "tool"), []byte("base"), 0o755); err != nil {
		t.Fatalf("WriteFile(base) error = %v", err)
	}
	be := NewImageFS(imagefs.NewHostFS(root, nil), root).(*imageFS)
	usrID, _, errno := be.Lookup(1, "usr")
	if errno != 0 {
		t.Fatalf("Lookup(usr) errno = %d", errno)
	}
	binID, _, errno := be.Lookup(usrID, "bin")
	if errno != 0 {
		t.Fatalf("Lookup(bin) errno = %d", errno)
	}
	tmpID, fh, _, errno := be.Create(binID, ".apk-new", linuxOWRONLY|linuxOCREAT, 0o755, 0, 0)
	if errno != 0 {
		t.Fatalf("Create(.apk-new) errno = %d", errno)
	}
	if wrote, errno := be.Write(tmpID, fh, 0, []byte("new"), 0); errno != 0 || wrote != 3 {
		t.Fatalf("Write(.apk-new) = (%d, %d), want (3, 0)", wrote, errno)
	}
	be.Release(tmpID, fh)
	if errno := be.Rename(binID, ".apk-new", binID, "tool", 0); errno != 0 {
		t.Fatalf("Rename(.apk-new -> tool) errno = %d", errno)
	}
	toolID, _, errno := be.Lookup(binID, "tool")
	if errno != 0 {
		t.Fatalf("Lookup(tool) errno = %d", errno)
	}
	fh, errno = be.Open(toolID, linuxORDONLY)
	if errno != 0 {
		t.Fatalf("Open(tool) errno = %d", errno)
	}
	data, errno := be.Read(toolID, fh, 0, 32)
	if errno != 0 {
		t.Fatalf("Read(tool) errno = %d", errno)
	}
	if string(data) != "new" {
		t.Fatalf("renamed data = %q, want new", data)
	}
}

func TestFUSEInitEnablesLargeWrites(t *testing.T) {
	t.Parallel()

	fsdev := NewFS(0, 0, 0, "root", NewPassthroughFS(t.TempDir(), nil))

	const unique = uint64(44)
	req := make([]byte, fuseInHeaderSize+16)
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.LittleEndian.PutUint32(req[4:8], fuseInit)
	binary.LittleEndian.PutUint64(req[8:16], unique)
	binary.LittleEndian.PutUint32(req[40:44], 7)
	binary.LittleEndian.PutUint32(req[44:48], 31)

	reply, err := fsdev.dispatchFUSELocked(req)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(INIT) error = %v", err)
	}
	if got := int32(binary.LittleEndian.Uint32(reply[4:8])); got != 0 {
		t.Fatalf("INIT errno = %d, want 0", got)
	}
	extra := reply[fuseOutHeaderSize:]
	flags := binary.LittleEndian.Uint32(extra[12:16])
	for _, flag := range []uint32{fuseCapBigWrites, fuseCapMaxPages} {
		if flags&flag == 0 {
			t.Fatalf("INIT flags = %#x, missing %#x", flags, flag)
		}
	}
	if got := binary.LittleEndian.Uint32(extra[20:24]); got != 128<<10 {
		t.Fatalf("INIT max_write = %d, want %d", got, 128<<10)
	}
	if got := binary.LittleEndian.Uint16(extra[28:30]); got != 32 {
		t.Fatalf("INIT max_pages = %d, want 32", got)
	}
}

func TestFUSEInitExplicitWritebackCache(t *testing.T) {
	t.Setenv("CCX3_VIRTIOFS_WRITEBACK", "1")

	fsdev := NewFS(0, 0, 0, "root", NewPassthroughFS(t.TempDir(), nil))

	const unique = uint64(45)
	req := make([]byte, fuseInHeaderSize+16)
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.LittleEndian.PutUint32(req[4:8], fuseInit)
	binary.LittleEndian.PutUint64(req[8:16], unique)
	binary.LittleEndian.PutUint32(req[40:44], 7)
	binary.LittleEndian.PutUint32(req[44:48], 31)

	reply, err := fsdev.dispatchFUSELocked(req)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(INIT) error = %v", err)
	}
	extra := reply[fuseOutHeaderSize:]
	flags := binary.LittleEndian.Uint32(extra[12:16])
	if flags&fuseCapWritebackCache == 0 {
		t.Fatalf("INIT flags = %#x, missing WRITEBACK_CACHE", flags)
	}
	if got := binary.LittleEndian.Uint16(extra[16:18]); got != 256 {
		t.Fatalf("INIT max_background = %d, want 256", got)
	}
	if got := binary.LittleEndian.Uint16(extra[18:20]); got != 192 {
		t.Fatalf("INIT congestion_threshold = %d, want 192", got)
	}
}

func TestFUSEGetXattrMissing(t *testing.T) {
	t.Parallel()

	fsdev := NewFS(0, 0, 0, "root", NewPassthroughFS(t.TempDir(), nil))

	const unique = uint64(46)
	req := make([]byte, fuseInHeaderSize+8+len("security.capability")+1)
	binary.LittleEndian.PutUint32(req[0:4], uint32(len(req)))
	binary.LittleEndian.PutUint32(req[4:8], fuseGetXattr)
	binary.LittleEndian.PutUint64(req[8:16], unique)
	binary.LittleEndian.PutUint64(req[16:24], 1)
	copy(req[fuseInHeaderSize+8:], "security.capability")

	reply, err := fsdev.dispatchFUSELocked(req)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(GETXATTR) error = %v", err)
	}
	if got := int32(binary.LittleEndian.Uint32(reply[4:8])); got != -linuxENODATA {
		t.Fatalf("GETXATTR errno = %d, want %d", got, -linuxENODATA)
	}
}

func TestPassthroughWritebackWriteOnlyHandleAllowsRead(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "data.bin"), []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	be := NewPassthroughFS(root, nil).(*passthroughFS)
	be.SetWritebackCache(true)
	nodeID, _, errno := be.Lookup(1, "data.bin")
	if errno != 0 {
		t.Fatalf("Lookup() errno = %d", errno)
	}
	fh, errno := be.Open(nodeID, linuxOWRONLY)
	if errno != 0 {
		t.Fatalf("Open(O_WRONLY) errno = %d", errno)
	}
	defer be.Release(nodeID, fh)
	got, errno := be.Read(nodeID, fh, 1, 3)
	if errno != 0 {
		t.Fatalf("Read() errno = %d", errno)
	}
	if string(got) != "bcd" {
		t.Fatalf("Read() = %q, want bcd", got)
	}
}

func TestFUSECachePolicyEncodesTTLAndOpenFlags(t *testing.T) {
	t.Setenv("CCX3_VIRTIOFS_CACHE", "aggressive")

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	fsdev := NewFS(0, 0, 0, "root", NewPassthroughFS(root, nil))

	lookupReq := make([]byte, fuseInHeaderSize+len("data.txt")+1)
	binary.LittleEndian.PutUint32(lookupReq[0:4], uint32(len(lookupReq)))
	binary.LittleEndian.PutUint32(lookupReq[4:8], fuseLookup)
	binary.LittleEndian.PutUint64(lookupReq[8:16], 1)
	binary.LittleEndian.PutUint64(lookupReq[16:24], 1)
	copy(lookupReq[fuseInHeaderSize:], "data.txt")
	lookupReply, err := fsdev.dispatchFUSELocked(lookupReq)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(LOOKUP) error = %v", err)
	}
	if got := int32(binary.LittleEndian.Uint32(lookupReply[4:8])); got != 0 {
		t.Fatalf("LOOKUP errno = %d, want 0", got)
	}
	extra := lookupReply[fuseOutHeaderSize:]
	if got := binary.LittleEndian.Uint64(extra[16:24]); got != 60 {
		t.Fatalf("LOOKUP entry TTL seconds = %d, want 60", got)
	}
	if got := binary.LittleEndian.Uint64(extra[24:32]); got != 60 {
		t.Fatalf("LOOKUP attr TTL seconds = %d, want 60", got)
	}
	nodeID := binary.LittleEndian.Uint64(extra[0:8])

	openReq := make([]byte, fuseInHeaderSize+8)
	binary.LittleEndian.PutUint32(openReq[0:4], uint32(len(openReq)))
	binary.LittleEndian.PutUint32(openReq[4:8], fuseOpen)
	binary.LittleEndian.PutUint64(openReq[8:16], 2)
	binary.LittleEndian.PutUint64(openReq[16:24], nodeID)
	binary.LittleEndian.PutUint32(openReq[40:44], linuxORDONLY)
	openReply, err := fsdev.dispatchFUSELocked(openReq)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(OPEN) error = %v", err)
	}
	openFlags := binary.LittleEndian.Uint32(openReply[fuseOutHeaderSize+8 : fuseOutHeaderSize+12])
	if openFlags&fuseOpenKeepCache == 0 {
		t.Fatalf("OPEN flags = %#x, want KEEP_CACHE", openFlags)
	}
	if openFlags&fuseOpenNoFlush == 0 {
		t.Fatalf("OPEN flags = %#x, want NO_FLUSH", openFlags)
	}
	fsdev.backend.Release(nodeID, binary.LittleEndian.Uint64(openReply[fuseOutHeaderSize:fuseOutHeaderSize+8]))
}

func TestFUSEStrictCachePolicyDisablesTTLAndKeepCache(t *testing.T) {
	t.Setenv("CCX3_VIRTIOFS_CACHE", "strict")

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "data.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	fsdev := NewFS(0, 0, 0, "root", NewPassthroughFS(root, nil))

	lookupReq := make([]byte, fuseInHeaderSize+len("data.txt")+1)
	binary.LittleEndian.PutUint32(lookupReq[0:4], uint32(len(lookupReq)))
	binary.LittleEndian.PutUint32(lookupReq[4:8], fuseLookup)
	binary.LittleEndian.PutUint64(lookupReq[8:16], 1)
	binary.LittleEndian.PutUint64(lookupReq[16:24], 1)
	copy(lookupReq[fuseInHeaderSize:], "data.txt")
	lookupReply, err := fsdev.dispatchFUSELocked(lookupReq)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(LOOKUP) error = %v", err)
	}
	extra := lookupReply[fuseOutHeaderSize:]
	if got := binary.LittleEndian.Uint64(extra[16:24]); got != 0 {
		t.Fatalf("LOOKUP entry TTL seconds = %d, want 0", got)
	}
	if got := binary.LittleEndian.Uint64(extra[24:32]); got != 0 {
		t.Fatalf("LOOKUP attr TTL seconds = %d, want 0", got)
	}
	nodeID := binary.LittleEndian.Uint64(extra[0:8])

	openReq := make([]byte, fuseInHeaderSize+8)
	binary.LittleEndian.PutUint32(openReq[0:4], uint32(len(openReq)))
	binary.LittleEndian.PutUint32(openReq[4:8], fuseOpen)
	binary.LittleEndian.PutUint64(openReq[8:16], 2)
	binary.LittleEndian.PutUint64(openReq[16:24], nodeID)
	binary.LittleEndian.PutUint32(openReq[40:44], linuxORDONLY)
	openReply, err := fsdev.dispatchFUSELocked(openReq)
	if err != nil {
		t.Fatalf("dispatchFUSELocked(OPEN) error = %v", err)
	}
	openFlags := binary.LittleEndian.Uint32(openReply[fuseOutHeaderSize+8 : fuseOutHeaderSize+12])
	if openFlags&fuseOpenKeepCache != 0 {
		t.Fatalf("OPEN flags = %#x, want no KEEP_CACHE", openFlags)
	}
	if openFlags&fuseOpenNoFlush == 0 {
		t.Fatalf("OPEN flags = %#x, want NO_FLUSH", openFlags)
	}
	fsdev.backend.Release(nodeID, binary.LittleEndian.Uint64(openReply[fuseOutHeaderSize:fuseOutHeaderSize+8]))
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
