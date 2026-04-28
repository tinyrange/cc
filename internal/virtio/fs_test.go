package virtio

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"

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

	nodeID, fh, _, errno := be.Create(1, "hello.txt", linuxOWRONLY|linuxOCREAT|linuxOTRUNC, 0o644)
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

func TestStrictFUSEFsyncUsesBackendHandle(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	backend := NewPassthroughFS(root, nil)
	be := backend.(*passthroughFS)
	nodeID, fh, _, errno := be.Create(1, "data.txt", linuxOWRONLY|linuxOCREAT|linuxOTRUNC, 0o644)
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
	for qidx := 0; qidx < fsTotalQueueCount(); qidx++ {
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
	if err := fsdev.Write(0x1000+regQueueSel, 4, uint64(fsTotalQueueCount())); err != nil {
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
