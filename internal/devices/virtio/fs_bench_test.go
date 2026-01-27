package virtio

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/tinyrange/cc/internal/hv"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
	"github.com/tinyrange/cc/internal/timeslice"
)

// -----------------------------------------------------------------------------
// Test infrastructure (similar to console_test.go)
// -----------------------------------------------------------------------------

// fsTestVM implements a minimal hv.VirtualMachine for fs testing
type fsTestVM struct {
	memory []byte
	irqs   map[uint32]bool
	mu     sync.Mutex
}

func newFsTestVM(memorySize int) *fsTestVM {
	return &fsTestVM{
		memory: make([]byte, memorySize),
		irqs:   make(map[uint32]bool),
	}
}

func (vm *fsTestVM) ReadAt(p []byte, off int64) (int, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	if off < 0 || int(off)+len(p) > len(vm.memory) {
		return 0, fmt.Errorf("read out of bounds: offset=%d len=%d memsize=%d", off, len(p), len(vm.memory))
	}
	copy(p, vm.memory[off:off+int64(len(p))])
	return len(p), nil
}

func (vm *fsTestVM) WriteAt(p []byte, off int64) (int, error) {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	if off < 0 || int(off)+len(p) > len(vm.memory) {
		return 0, fmt.Errorf("write out of bounds: offset=%d len=%d memsize=%d", off, len(p), len(vm.memory))
	}
	copy(vm.memory[off:], p)
	return len(p), nil
}

func (vm *fsTestVM) SetIRQ(line uint32, level bool) error {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	vm.irqs[line] = level
	return nil
}

func (vm *fsTestVM) GetIRQ(line uint32) bool {
	vm.mu.Lock()
	defer vm.mu.Unlock()
	return vm.irqs[line]
}

// Implement other required hv.VirtualMachine methods as no-ops
func (vm *fsTestVM) Close() error                                                  { return nil }
func (vm *fsTestVM) Hypervisor() hv.Hypervisor                                     { return nil }
func (vm *fsTestVM) MemorySize() uint64                                            { return uint64(len(vm.memory)) }
func (vm *fsTestVM) MemoryBase() uint64                                            { return 0 }
func (vm *fsTestVM) Run(ctx context.Context, cfg hv.RunConfig) error               { return nil }
func (vm *fsTestVM) VirtualCPUCall(id int, f func(vcpu hv.VirtualCPU) error) error { return nil }
func (vm *fsTestVM) AddDevice(dev hv.Device) error                                 { return nil }
func (vm *fsTestVM) AddDeviceFromTemplate(template hv.DeviceTemplate) (hv.Device, error) {
	return nil, nil
}
func (vm *fsTestVM) AllocateMemory(physAddr, size uint64) (hv.MemoryRegion, error) {
	return nil, fmt.Errorf("not implemented")
}
func (vm *fsTestVM) CaptureSnapshot() (hv.Snapshot, error)  { return nil, nil }
func (vm *fsTestVM) RestoreSnapshot(snap hv.Snapshot) error { return nil }
func (vm *fsTestVM) AllocateMMIO(req hv.MMIOAllocationRequest) (hv.MMIOAllocation, error) {
	return hv.MMIOAllocation{}, nil
}
func (vm *fsTestVM) RegisterFixedMMIO(name string, base, size uint64) error { return nil }
func (vm *fsTestVM) GetAllocatedMMIORegions() []hv.MMIOAllocation           { return nil }

var _ hv.VirtualMachine = (*fsTestVM)(nil)

// fsTestExitContext implements hv.ExitContext for testing
type fsTestExitContext struct{}

func (m fsTestExitContext) SetExitTimeslice(kind timeslice.TimesliceID) {}

var _ hv.ExitContext = fsTestExitContext{}

// Memory layout constants for tests
const (
	fsTestMemorySize = 16 * 1024 * 1024 // 16 MB

	// High-priority queue (queue 0)
	fsTestHiDescTableAddr = 0x100000
	fsTestHiAvailRingAddr = 0x101000
	fsTestHiUsedRingAddr  = 0x102000

	// Request queue (queue 1)
	fsTestReqDescTableAddr = 0x110000
	fsTestReqAvailRingAddr = 0x111000
	fsTestReqUsedRingAddr  = 0x112000

	// Data buffers
	fsTestBufferAddr   = 0x200000 // 2 MB
	fsTestFileDataAddr = 0x800000 // 8 MB - for file data storage
)

// fsMMIOHelper provides convenience methods for MMIO operations
type fsMMIOHelper struct {
	fs  *FS
	ctx hv.ExitContext
}

func newFsMMIOHelper(fs *FS) *fsMMIOHelper {
	return &fsMMIOHelper{
		fs:  fs,
		ctx: fsTestExitContext{},
	}
}

func (h *fsMMIOHelper) readReg(offset uint64) uint32 {
	data := make([]byte, 4)
	if err := h.fs.ReadMMIO(h.ctx, FsDefaultMMIOBase+offset, data); err != nil {
		panic(fmt.Sprintf("readReg failed: %v", err))
	}
	return binary.LittleEndian.Uint32(data)
}

func (h *fsMMIOHelper) writeReg(offset uint64, value uint32) {
	data := make([]byte, 4)
	binary.LittleEndian.PutUint32(data, value)
	if err := h.fs.WriteMMIO(h.ctx, FsDefaultMMIOBase+offset, data); err != nil {
		panic(fmt.Sprintf("writeReg failed: %v", err))
	}
}

// fsVirtqueueSetup sets up a virtqueue in guest memory
type fsVirtqueueSetup struct {
	vm            *fsTestVM
	descTableAddr uint64
	availRingAddr uint64
	usedRingAddr  uint64
	queueSize     uint16
	nextDescIdx   uint16
	availIdx      uint16
}

func newFsVirtqueueSetup(vm *fsTestVM, descTable, availRing, usedRing uint64, size uint16) *fsVirtqueueSetup {
	return &fsVirtqueueSetup{
		vm:            vm,
		descTableAddr: descTable,
		availRingAddr: availRing,
		usedRingAddr:  usedRing,
		queueSize:     size,
	}
}

func (vq *fsVirtqueueSetup) writeUint16(addr uint64, val uint16) {
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], val)
	vq.vm.WriteAt(buf[:], int64(addr))
}

func (vq *fsVirtqueueSetup) writeUint32(addr uint64, val uint32) {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], val)
	vq.vm.WriteAt(buf[:], int64(addr))
}

func (vq *fsVirtqueueSetup) writeUint64(addr uint64, val uint64) {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], val)
	vq.vm.WriteAt(buf[:], int64(addr))
}

func (vq *fsVirtqueueSetup) readUint16(addr uint64) uint16 {
	var buf [2]byte
	vq.vm.ReadAt(buf[:], int64(addr))
	return binary.LittleEndian.Uint16(buf[:])
}

func (vq *fsVirtqueueSetup) readUint32(addr uint64) uint32 {
	var buf [4]byte
	vq.vm.ReadAt(buf[:], int64(addr))
	return binary.LittleEndian.Uint32(buf[:])
}

// initRings initializes the available and used rings
func (vq *fsVirtqueueSetup) initRings() {
	// Initialize available ring header: flags=0, idx=0
	vq.writeUint16(vq.availRingAddr+0, 0)
	vq.writeUint16(vq.availRingAddr+2, 0)

	// Initialize used ring header: flags=0, idx=0
	vq.writeUint16(vq.usedRingAddr+0, 0)
	vq.writeUint16(vq.usedRingAddr+2, 0)
}

// writeDescriptor writes a descriptor to the descriptor table
func (vq *fsVirtqueueSetup) writeDescriptor(idx uint16, addr uint64, length uint32, flags uint16, next uint16) {
	base := vq.descTableAddr + uint64(idx)*16
	vq.writeUint64(base+0, addr)
	vq.writeUint32(base+8, length)
	vq.writeUint16(base+12, flags)
	vq.writeUint16(base+14, next)
}

// addAvailableBuffer adds a buffer to the available ring
func (vq *fsVirtqueueSetup) addAvailableBuffer(descIdx uint16) {
	ringIdx := vq.availIdx % vq.queueSize
	vq.writeUint16(vq.availRingAddr+4+uint64(ringIdx)*2, descIdx)
	vq.availIdx++
	vq.writeUint16(vq.availRingAddr+2, vq.availIdx)
}

// getUsedEntry reads a used ring entry
func (vq *fsVirtqueueSetup) getUsedEntry(idx uint16) (head uint32, length uint32) {
	base := vq.usedRingAddr + 4 + uint64(idx)*8
	return vq.readUint32(base), vq.readUint32(base + 4)
}

// getUsedIdx reads the used ring index
func (vq *fsVirtqueueSetup) getUsedIdx() uint16 {
	return vq.readUint16(vq.usedRingAddr + 2)
}

// allocDescriptor allocates a descriptor and returns its index
func (vq *fsVirtqueueSetup) allocDescriptor(addr uint64, length uint32, flags uint16) uint16 {
	idx := vq.nextDescIdx
	vq.writeDescriptor(idx, addr, length, flags, 0)
	vq.nextDescIdx++
	return idx
}

// writeBuffer writes data to a buffer in guest memory
func (vq *fsVirtqueueSetup) writeBuffer(addr uint64, data []byte) {
	vq.vm.WriteAt(data, int64(addr))
}

// readBuffer reads data from a buffer in guest memory
func (vq *fsVirtqueueSetup) readBuffer(addr uint64, length uint32) []byte {
	buf := make([]byte, length)
	vq.vm.ReadAt(buf, int64(addr))
	return buf
}

// Descriptor flags
const (
	fsVirtqDescFNext  uint16 = 1
	fsVirtqDescFWrite uint16 = 2
)

// -----------------------------------------------------------------------------
// Test Backend Implementation
// -----------------------------------------------------------------------------

// testFsNode represents a filesystem node (file, directory, or symlink)
type testFsNode struct {
	id       uint64
	parent   uint64
	name     string
	mode     uint32
	uid      uint32
	gid      uint32
	size     uint64
	data     []byte            // For files
	children map[string]uint64 // For directories
	target   string            // For symlinks
	xattrs   map[string][]byte
	atime    time.Time
	mtime    time.Time
	ctime    time.Time
	nlink    uint32
	rdev     uint32 // For device nodes
}

// testFsHandle represents an open file handle
type testFsHandle struct {
	nodeID uint64
	flags  uint32
	isDir  bool
	offset uint64 // For directories, tracks readdir position
}

// testFsBackend implements FsBackend and all optional interfaces for testing
type testFsBackend struct {
	mu         sync.Mutex
	nodes      map[uint64]*testFsNode
	handles    map[uint64]*testFsHandle
	dirHandles map[uint64]*testFsHandle
	nextID     uint64
	nextFH     uint64
	locks      map[uint64][]testLockRange // Per-inode locks
}

type testLockRange struct {
	start uint64
	end   uint64
	typ   uint32
	pid   uint32
	owner uint64
}

func newTestFsBackend() *testFsBackend {
	be := &testFsBackend{
		nodes:      make(map[uint64]*testFsNode),
		handles:    make(map[uint64]*testFsHandle),
		dirHandles: make(map[uint64]*testFsHandle),
		locks:      make(map[uint64][]testLockRange),
		nextID:     2, // 1 is reserved for root
		nextFH:     1,
	}
	// Create root directory
	now := time.Now()
	be.nodes[1] = &testFsNode{
		id:       1,
		parent:   1,
		name:     "",
		mode:     0040755, // directory with rwxr-xr-x
		uid:      0,
		gid:      0,
		children: make(map[string]uint64),
		xattrs:   make(map[string][]byte),
		atime:    now,
		mtime:    now,
		ctime:    now,
		nlink:    2,
	}
	return be
}

// FsBackend interface implementation
func (be *testFsBackend) Init() (uint32, uint32) {
	return 128 * 1024, FuseCapPosixLocks | FuseCapFlockLocks
}

func (be *testFsBackend) GetAttr(nodeID uint64) (FuseAttr, int32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	node, ok := be.nodes[nodeID]
	if !ok {
		return FuseAttr{}, -int32(linux.ENOENT)
	}

	return be.nodeToAttr(node), 0
}

func (be *testFsBackend) nodeToAttr(node *testFsNode) FuseAttr {
	blocks := (node.size + 511) / 512
	return FuseAttr{
		Ino:       node.id,
		Size:      node.size,
		Blocks:    blocks,
		ATimeSec:  uint64(node.atime.Unix()),
		MTimeSec:  uint64(node.mtime.Unix()),
		CTimeSec:  uint64(node.ctime.Unix()),
		ATimeNsec: uint32(node.atime.Nanosecond()),
		MTimeNsec: uint32(node.mtime.Nanosecond()),
		CTimeNsec: uint32(node.ctime.Nanosecond()),
		Mode:      node.mode,
		NLink:     node.nlink,
		UID:       node.uid,
		GID:       node.gid,
		RDev:      node.rdev,
		BlkSize:   4096,
	}
}

func (be *testFsBackend) Lookup(parent uint64, name string) (uint64, FuseAttr, int32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	parentNode, ok := be.nodes[parent]
	if !ok {
		return 0, FuseAttr{}, -int32(linux.ENOENT)
	}

	childID, ok := parentNode.children[name]
	if !ok {
		return 0, FuseAttr{}, -int32(linux.ENOENT)
	}

	child, ok := be.nodes[childID]
	if !ok {
		return 0, FuseAttr{}, -int32(linux.ENOENT)
	}

	return childID, be.nodeToAttr(child), 0
}

func (be *testFsBackend) Open(nodeID uint64, flags uint32) (uint64, int32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	node, ok := be.nodes[nodeID]
	if !ok {
		return 0, -int32(linux.ENOENT)
	}

	if node.mode&0040000 != 0 {
		return 0, -int32(linux.EISDIR)
	}

	fh := be.nextFH
	be.nextFH++
	be.handles[fh] = &testFsHandle{
		nodeID: nodeID,
		flags:  flags,
		isDir:  false,
	}
	return fh, 0
}

func (be *testFsBackend) Release(nodeID uint64, fh uint64) {
	be.mu.Lock()
	defer be.mu.Unlock()
	delete(be.handles, fh)
}

func (be *testFsBackend) Read(nodeID uint64, fh uint64, off uint64, size uint32) ([]byte, int32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	node, ok := be.nodes[nodeID]
	if !ok {
		return nil, -int32(linux.ENOENT)
	}

	if off >= node.size {
		return []byte{}, 0
	}

	end := off + uint64(size)
	if end > node.size {
		end = node.size
	}

	return node.data[off:end], 0
}

func (be *testFsBackend) ReadDir(nodeID uint64, off uint64, maxBytes uint32) ([]byte, int32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	node, ok := be.nodes[nodeID]
	if !ok {
		return nil, -int32(linux.ENOENT)
	}

	if node.mode&0040000 == 0 {
		return nil, -int32(linux.ENOTDIR)
	}

	// Build directory entries
	var entries []struct {
		name   string
		nodeID uint64
	}
	for name, id := range node.children {
		entries = append(entries, struct {
			name   string
			nodeID uint64
		}{name, id})
	}

	// Skip entries based on offset
	if int(off) >= len(entries) {
		return []byte{}, 0
	}
	entries = entries[off:]

	// Build FUSE dirent format
	var buf bytes.Buffer
	for i, entry := range entries {
		child, ok := be.nodes[entry.nodeID]
		if !ok {
			continue
		}

		// fuse_dirent: ino (8), off (8), namelen (4), type (4), name (padded)
		nameBytes := []byte(entry.name)
		entryLen := 24 + len(nameBytes)
		padLen := (8 - (entryLen % 8)) % 8
		entryLen += padLen

		if uint32(buf.Len()+entryLen) > maxBytes {
			break
		}

		var dirent [24]byte
		binary.LittleEndian.PutUint64(dirent[0:8], entry.nodeID)
		binary.LittleEndian.PutUint64(dirent[8:16], off+uint64(i)+1) // next offset
		binary.LittleEndian.PutUint32(dirent[16:20], uint32(len(nameBytes)))
		// Type: extract from mode
		dtype := uint32(0)
		switch child.mode & 0170000 {
		case 0100000: // regular file
			dtype = 8
		case 0040000: // directory
			dtype = 4
		case 0120000: // symlink
			dtype = 10
		}
		binary.LittleEndian.PutUint32(dirent[20:24], dtype)

		buf.Write(dirent[:])
		buf.Write(nameBytes)
		buf.Write(make([]byte, padLen))
	}

	return buf.Bytes(), 0
}

func (be *testFsBackend) StatFS(nodeID uint64) (blocks, bfree, bavail, files, ffree, bsize, frsize, namelen uint64, errno int32) {
	be.mu.Lock()
	defer be.mu.Unlock()
	return 1000000, 500000, 500000, 100000, 90000, 4096, 4096, 255, 0
}

// fsCreateBackend implementation
func (be *testFsBackend) Create(parent uint64, name string, mode uint32, flags uint32, umask uint32, uid uint32, gid uint32) (uint64, uint64, FuseAttr, int32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	parentNode, ok := be.nodes[parent]
	if !ok {
		return 0, 0, FuseAttr{}, -int32(linux.ENOENT)
	}

	if _, exists := parentNode.children[name]; exists {
		return 0, 0, FuseAttr{}, -int32(linux.EEXIST)
	}

	now := time.Now()
	nodeID := be.nextID
	be.nextID++

	node := &testFsNode{
		id:     nodeID,
		parent: parent,
		name:   name,
		mode:   (mode &^ umask) | 0100000, // Regular file
		uid:    uid,
		gid:    gid,
		data:   []byte{},
		xattrs: make(map[string][]byte),
		atime:  now,
		mtime:  now,
		ctime:  now,
		nlink:  1,
	}
	be.nodes[nodeID] = node
	parentNode.children[name] = nodeID

	fh := be.nextFH
	be.nextFH++
	be.handles[fh] = &testFsHandle{
		nodeID: nodeID,
		flags:  flags,
		isDir:  false,
	}

	return nodeID, fh, be.nodeToAttr(node), 0
}

// fsMkdirBackend implementation
func (be *testFsBackend) Mkdir(parent uint64, name string, mode uint32, umask uint32, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	parentNode, ok := be.nodes[parent]
	if !ok {
		return 0, FuseAttr{}, -int32(linux.ENOENT)
	}

	if _, exists := parentNode.children[name]; exists {
		return 0, FuseAttr{}, -int32(linux.EEXIST)
	}

	now := time.Now()
	nodeID := be.nextID
	be.nextID++

	node := &testFsNode{
		id:       nodeID,
		parent:   parent,
		name:     name,
		mode:     (mode &^ umask) | 0040000, // Directory
		uid:      uid,
		gid:      gid,
		children: make(map[string]uint64),
		xattrs:   make(map[string][]byte),
		atime:    now,
		mtime:    now,
		ctime:    now,
		nlink:    2,
	}
	be.nodes[nodeID] = node
	parentNode.children[name] = nodeID
	parentNode.nlink++

	return nodeID, be.nodeToAttr(node), 0
}

// fsMknodBackend implementation
func (be *testFsBackend) Mknod(parent uint64, name string, mode uint32, rdev uint32, umask uint32, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	parentNode, ok := be.nodes[parent]
	if !ok {
		return 0, FuseAttr{}, -int32(linux.ENOENT)
	}

	if _, exists := parentNode.children[name]; exists {
		return 0, FuseAttr{}, -int32(linux.EEXIST)
	}

	now := time.Now()
	nodeID := be.nextID
	be.nextID++

	node := &testFsNode{
		id:     nodeID,
		parent: parent,
		name:   name,
		mode:   mode &^ umask,
		uid:    uid,
		gid:    gid,
		rdev:   rdev,
		xattrs: make(map[string][]byte),
		atime:  now,
		mtime:  now,
		ctime:  now,
		nlink:  1,
	}
	be.nodes[nodeID] = node
	parentNode.children[name] = nodeID

	return nodeID, be.nodeToAttr(node), 0
}

// fsWriteBackend implementation
func (be *testFsBackend) Write(nodeID uint64, fh uint64, off uint64, data []byte) (uint32, int32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	node, ok := be.nodes[nodeID]
	if !ok {
		return 0, -int32(linux.ENOENT)
	}

	// Extend file if necessary
	end := off + uint64(len(data))
	if end > node.size {
		newData := make([]byte, end)
		copy(newData, node.data)
		node.data = newData
		node.size = end
	}

	copy(node.data[off:], data)
	node.mtime = time.Now()

	return uint32(len(data)), 0
}

// fsOpenDirBackend implementation
func (be *testFsBackend) OpenDir(nodeID uint64, flags uint32) (uint64, int32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	node, ok := be.nodes[nodeID]
	if !ok {
		return 0, -int32(linux.ENOENT)
	}

	if node.mode&0040000 == 0 {
		return 0, -int32(linux.ENOTDIR)
	}

	fh := be.nextFH
	be.nextFH++
	be.dirHandles[fh] = &testFsHandle{
		nodeID: nodeID,
		flags:  flags,
		isDir:  true,
		offset: 0,
	}
	return fh, 0
}

func (be *testFsBackend) ReleaseDir(nodeID uint64, fh uint64) {
	be.mu.Lock()
	defer be.mu.Unlock()
	delete(be.dirHandles, fh)
}

// fsReadDirHandleBackend implementation
func (be *testFsBackend) ReadDirHandle(nodeID uint64, fh uint64, off uint64, maxBytes uint32) ([]byte, int32) {
	return be.ReadDir(nodeID, off, maxBytes)
}

// fsSymlinkBackend implementation
func (be *testFsBackend) Symlink(parent uint64, name string, target string, umask uint32, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	parentNode, ok := be.nodes[parent]
	if !ok {
		return 0, FuseAttr{}, -int32(linux.ENOENT)
	}

	if _, exists := parentNode.children[name]; exists {
		return 0, FuseAttr{}, -int32(linux.EEXIST)
	}

	now := time.Now()
	nodeID := be.nextID
	be.nextID++

	node := &testFsNode{
		id:     nodeID,
		parent: parent,
		name:   name,
		mode:   0120777, // Symlink
		uid:    uid,
		gid:    gid,
		target: target,
		size:   uint64(len(target)),
		xattrs: make(map[string][]byte),
		atime:  now,
		mtime:  now,
		ctime:  now,
		nlink:  1,
	}
	be.nodes[nodeID] = node
	parentNode.children[name] = nodeID

	return nodeID, be.nodeToAttr(node), 0
}

// fsReadlinkBackend implementation
func (be *testFsBackend) Readlink(nodeID uint64) (string, int32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	node, ok := be.nodes[nodeID]
	if !ok {
		return "", -int32(linux.ENOENT)
	}

	if node.mode&0170000 != 0120000 {
		return "", -int32(linux.EINVAL)
	}

	return node.target, 0
}

// fsLinkBackend implementation
func (be *testFsBackend) Link(oldNodeID uint64, newParent uint64, newName string) (uint64, FuseAttr, int32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	oldNode, ok := be.nodes[oldNodeID]
	if !ok {
		return 0, FuseAttr{}, -int32(linux.ENOENT)
	}

	parentNode, ok := be.nodes[newParent]
	if !ok {
		return 0, FuseAttr{}, -int32(linux.ENOENT)
	}

	if _, exists := parentNode.children[newName]; exists {
		return 0, FuseAttr{}, -int32(linux.EEXIST)
	}

	parentNode.children[newName] = oldNodeID
	oldNode.nlink++
	oldNode.ctime = time.Now()

	return oldNodeID, be.nodeToAttr(oldNode), 0
}

// fsRenameBackend implementation
func (be *testFsBackend) Rename(oldParent uint64, oldName string, newParent uint64, newName string, flags uint32) int32 {
	be.mu.Lock()
	defer be.mu.Unlock()

	oldParentNode, ok := be.nodes[oldParent]
	if !ok {
		return -int32(linux.ENOENT)
	}

	nodeID, ok := oldParentNode.children[oldName]
	if !ok {
		return -int32(linux.ENOENT)
	}

	newParentNode, ok := be.nodes[newParent]
	if !ok {
		return -int32(linux.ENOENT)
	}

	// Remove old entry
	delete(oldParentNode.children, oldName)

	// Add new entry (overwrites if exists)
	newParentNode.children[newName] = nodeID

	// Update node's parent and name
	node := be.nodes[nodeID]
	node.parent = newParent
	node.name = newName
	node.ctime = time.Now()

	return 0
}

// fsRemoveBackend implementation
func (be *testFsBackend) Unlink(parent uint64, name string) int32 {
	be.mu.Lock()
	defer be.mu.Unlock()

	parentNode, ok := be.nodes[parent]
	if !ok {
		return -int32(linux.ENOENT)
	}

	nodeID, ok := parentNode.children[name]
	if !ok {
		return -int32(linux.ENOENT)
	}

	node := be.nodes[nodeID]
	if node.mode&0040000 != 0 {
		return -int32(linux.EISDIR)
	}

	delete(parentNode.children, name)
	node.nlink--
	if node.nlink == 0 {
		delete(be.nodes, nodeID)
	}

	return 0
}

func (be *testFsBackend) Rmdir(parent uint64, name string) int32 {
	be.mu.Lock()
	defer be.mu.Unlock()

	parentNode, ok := be.nodes[parent]
	if !ok {
		return -int32(linux.ENOENT)
	}

	nodeID, ok := parentNode.children[name]
	if !ok {
		return -int32(linux.ENOENT)
	}

	node := be.nodes[nodeID]
	if node.mode&0040000 == 0 {
		return -int32(linux.ENOTDIR)
	}

	if len(node.children) > 0 {
		return -39 // ENOTEMPTY
	}

	delete(parentNode.children, name)
	parentNode.nlink--
	delete(be.nodes, nodeID)

	return 0
}

// fsSetattrBackend implementation
func (be *testFsBackend) SetAttr(nodeID uint64, size *uint64, mode *uint32, uid *uint32, gid *uint32, atime *time.Time, mtime *time.Time, reqUID uint32, reqGID uint32) int32 {
	be.mu.Lock()
	defer be.mu.Unlock()

	node, ok := be.nodes[nodeID]
	if !ok {
		return -int32(linux.ENOENT)
	}

	if size != nil {
		if *size < node.size {
			node.data = node.data[:*size]
		} else if *size > node.size {
			newData := make([]byte, *size)
			copy(newData, node.data)
			node.data = newData
		}
		node.size = *size
	}

	if mode != nil {
		node.mode = (node.mode & 0170000) | (*mode & 07777)
	}

	if uid != nil {
		node.uid = *uid
	}

	if gid != nil {
		node.gid = *gid
	}

	if atime != nil {
		node.atime = *atime
	}

	if mtime != nil {
		node.mtime = *mtime
	}

	node.ctime = time.Now()

	return 0
}

// fsXattrBackend implementation
func (be *testFsBackend) SetXattr(nodeID uint64, name string, value []byte, flags uint32, uid uint32, gid uint32) int32 {
	be.mu.Lock()
	defer be.mu.Unlock()

	node, ok := be.nodes[nodeID]
	if !ok {
		return -int32(linux.ENOENT)
	}

	node.xattrs[name] = append([]byte{}, value...)
	return 0
}

func (be *testFsBackend) GetXattr(nodeID uint64, name string) ([]byte, int32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	node, ok := be.nodes[nodeID]
	if !ok {
		return nil, -int32(linux.ENOENT)
	}

	value, ok := node.xattrs[name]
	if !ok {
		return nil, -61 // ENODATA
	}

	return value, 0
}

func (be *testFsBackend) ListXattr(nodeID uint64) ([]byte, int32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	node, ok := be.nodes[nodeID]
	if !ok {
		return nil, -int32(linux.ENOENT)
	}

	var buf bytes.Buffer
	for name := range node.xattrs {
		buf.WriteString(name)
		buf.WriteByte(0)
	}

	return buf.Bytes(), 0
}

func (be *testFsBackend) RemoveXattr(nodeID uint64, name string) int32 {
	be.mu.Lock()
	defer be.mu.Unlock()

	node, ok := be.nodes[nodeID]
	if !ok {
		return -int32(linux.ENOENT)
	}

	if _, ok := node.xattrs[name]; !ok {
		return -61 // ENODATA
	}

	delete(node.xattrs, name)
	return 0
}

// fsLseekBackend implementation
func (be *testFsBackend) Lseek(nodeID uint64, fh uint64, offset uint64, whence uint32) (uint64, int32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	node, ok := be.nodes[nodeID]
	if !ok {
		return 0, -int32(linux.ENOENT)
	}

	// SEEK_DATA = 3, SEEK_HOLE = 4
	switch whence {
	case 3: // SEEK_DATA
		if offset >= node.size {
			return 0, -int32(linux.ENXIO)
		}
		return offset, 0
	case 4: // SEEK_HOLE
		if offset >= node.size {
			return 0, -int32(linux.ENXIO)
		}
		return node.size, 0
	default:
		return 0, -int32(linux.EINVAL)
	}
}

// fsFallocateBackend implementation
func (be *testFsBackend) Fallocate(nodeID uint64, fh uint64, offset uint64, length uint64, mode uint32) int32 {
	be.mu.Lock()
	defer be.mu.Unlock()

	node, ok := be.nodes[nodeID]
	if !ok {
		return -int32(linux.ENOENT)
	}

	end := offset + length
	if end > node.size {
		newData := make([]byte, end)
		copy(newData, node.data)
		node.data = newData
		node.size = end
	}

	return 0
}

// fsLockBackend implementation
func (be *testFsBackend) GetLk(nodeID uint64, fh uint64, owner uint64, lk FuseLock, flags uint32) (FuseLock, int32) {
	be.mu.Lock()
	defer be.mu.Unlock()

	// Check for conflicting locks
	locks := be.locks[nodeID]
	for _, existing := range locks {
		if existing.owner == owner {
			continue // Own locks don't conflict
		}
		if existing.end < lk.Start || lk.End < existing.start {
			continue // Non-overlapping
		}
		if lk.Type == 0 && existing.typ == 0 {
			continue // Both read locks
		}
		// Conflict found
		return FuseLock{
			Start: existing.start,
			End:   existing.end,
			Type:  existing.typ,
			PID:   existing.pid,
		}, 0
	}

	// No conflict - return unlocked
	return FuseLock{Type: 2}, 0 // F_UNLCK = 2
}

func (be *testFsBackend) SetLk(nodeID uint64, fh uint64, owner uint64, lk FuseLock, flags uint32) int32 {
	be.mu.Lock()
	defer be.mu.Unlock()

	if lk.Type == 2 { // F_UNLCK
		// Remove matching locks
		var remaining []testLockRange
		for _, existing := range be.locks[nodeID] {
			if existing.owner == owner && existing.start == lk.Start && existing.end == lk.End {
				continue
			}
			remaining = append(remaining, existing)
		}
		be.locks[nodeID] = remaining
		return 0
	}

	// Check for conflicts
	for _, existing := range be.locks[nodeID] {
		if existing.owner == owner {
			continue
		}
		if existing.end < lk.Start || lk.End < existing.start {
			continue
		}
		if lk.Type == 0 && existing.typ == 0 {
			continue
		}
		return -int32(linux.EAGAIN)
	}

	// Add lock
	be.locks[nodeID] = append(be.locks[nodeID], testLockRange{
		start: lk.Start,
		end:   lk.End,
		typ:   lk.Type,
		pid:   lk.PID,
		owner: owner,
	})

	return 0
}

func (be *testFsBackend) SetLkW(nodeID uint64, fh uint64, owner uint64, lk FuseLock, flags uint32) int32 {
	return be.SetLk(nodeID, fh, owner, lk, flags)
}

// fsFlushBackend implementation
func (be *testFsBackend) Flush(nodeID uint64, fh uint64, lockOwner uint64) int32 {
	return 0
}

// Verify interface implementations
var (
	_ FsBackend              = (*testFsBackend)(nil)
	_ fsCreateBackend        = (*testFsBackend)(nil)
	_ fsMkdirBackend         = (*testFsBackend)(nil)
	_ fsMknodBackend         = (*testFsBackend)(nil)
	_ fsWriteBackend         = (*testFsBackend)(nil)
	_ fsOpenDirBackend       = (*testFsBackend)(nil)
	_ fsReadDirHandleBackend = (*testFsBackend)(nil)
	_ fsSymlinkBackend       = (*testFsBackend)(nil)
	_ fsReadlinkBackend      = (*testFsBackend)(nil)
	_ fsLinkBackend          = (*testFsBackend)(nil)
	_ fsRenameBackend        = (*testFsBackend)(nil)
	_ fsRemoveBackend        = (*testFsBackend)(nil)
	_ fsSetattrBackend       = (*testFsBackend)(nil)
	_ fsXattrBackend         = (*testFsBackend)(nil)
	_ fsLseekBackend         = (*testFsBackend)(nil)
	_ fsFallocateBackend     = (*testFsBackend)(nil)
	_ fsLockBackend          = (*testFsBackend)(nil)
	_ fsFlushBackend         = (*testFsBackend)(nil)
)

// -----------------------------------------------------------------------------
// FUSE Client Implementation
// -----------------------------------------------------------------------------

// testFuseClient provides a client interface for sending FUSE requests
type testFuseClient struct {
	fs       *FS
	mmio     *fsMMIOHelper
	reqQueue *fsVirtqueueSetup
	vm       *fsTestVM
	unique   uint64
	bufAddr  uint64 // Address for request/response buffers
}

func newTestFuseClient(fs *FS, mmio *fsMMIOHelper, reqQueue *fsVirtqueueSetup, vm *fsTestVM) *testFuseClient {
	return &testFuseClient{
		fs:       fs,
		mmio:     mmio,
		reqQueue: reqQueue,
		vm:       vm,
		unique:   1,
		bufAddr:  fsTestBufferAddr,
	}
}

// resetForBench is a no-op placeholder for benchmarks.
// The sendRequest function now handles wrapping around descriptor indices
// automatically, so no explicit reset is needed.
func (c *testFuseClient) resetForBench() {
	// No-op - descriptors wrap around automatically
}

// sendRequest sends a FUSE request and returns the response
func (c *testFuseClient) sendRequest(opcode uint32, nodeID uint64, payload []byte, respSize uint32) ([]byte, int32, error) {
	// Build request
	reqLen := fuseHdrInSize + len(payload)
	req := make([]byte, reqLen)
	binary.LittleEndian.PutUint32(req[0:4], uint32(reqLen))
	binary.LittleEndian.PutUint32(req[4:8], opcode)
	binary.LittleEndian.PutUint64(req[8:16], c.unique)
	c.unique++
	binary.LittleEndian.PutUint64(req[16:24], nodeID)
	binary.LittleEndian.PutUint32(req[24:28], 0) // UID
	binary.LittleEndian.PutUint32(req[28:32], 0) // GID
	binary.LittleEndian.PutUint32(req[32:36], 0) // PID
	copy(req[fuseHdrInSize:], payload)

	// Use unique buffer addresses for each request to avoid conflicts
	// Use modulo to cycle through buffer slots
	slot := c.unique % 64
	reqAddr := c.bufAddr + slot*0x20000
	c.vm.WriteAt(req, int64(reqAddr))

	// Allocate response buffer
	respAddr := reqAddr + 0x10000
	respBuf := make([]byte, respSize)
	c.vm.WriteAt(respBuf, int64(respAddr))

	// Track the expected used index before this request
	expectedUsedIdx := c.reqQueue.availIdx

	// Allocate descriptors for this request with wrapping
	// Each request uses 2 descriptors, so we need nextDescIdx to stay within queue bounds
	descBase := c.reqQueue.nextDescIdx % c.reqQueue.queueSize
	nextDesc := (descBase + 1) % c.reqQueue.queueSize
	c.reqQueue.writeDescriptor(descBase, reqAddr, uint32(reqLen), fsVirtqDescFNext, nextDesc)
	c.reqQueue.writeDescriptor(nextDesc, respAddr, respSize, fsVirtqDescFWrite, 0)
	c.reqQueue.nextDescIdx += 2

	// Add to available ring
	c.reqQueue.addAvailableBuffer(descBase)

	// Notify device
	c.mmio.writeReg(VIRTIO_MMIO_QUEUE_NOTIFY, 1) // Request queue is queue 1

	// Read response - find the used entry for our request
	usedIdx := c.reqQueue.getUsedIdx()
	// Use signed arithmetic to handle wrap-around correctly
	// When indices wrap from 65535 to 0, we need (usedIdx - expectedUsedIdx) > 0
	if int16(usedIdx-expectedUsedIdx) <= 0 {
		return nil, 0, fmt.Errorf("no response received (usedIdx=%d, expected>%d)", usedIdx, expectedUsedIdx)
	}

	_, usedLen := c.reqQueue.getUsedEntry(expectedUsedIdx % c.reqQueue.queueSize)
	resp := c.reqQueue.readBuffer(respAddr, usedLen)

	if len(resp) < fuseHdrOutSize {
		return nil, 0, fmt.Errorf("response too short: %d", len(resp))
	}

	// Parse response header
	errno := int32(binary.LittleEndian.Uint32(resp[4:8]))
	return resp[fuseHdrOutSize:], errno, nil
}

// FUSE operations

func (c *testFuseClient) Init() error {
	payload := make([]byte, 64)
	binary.LittleEndian.PutUint32(payload[0:4], 7)         // Major
	binary.LittleEndian.PutUint32(payload[4:8], 31)        // Minor
	binary.LittleEndian.PutUint32(payload[8:12], 128*1024) // Max readahead
	binary.LittleEndian.PutUint32(payload[12:16], 0)       // Flags

	_, errno, err := c.sendRequest(FUSE_INIT, 0, payload, 256)
	if err != nil {
		return err
	}
	if errno != 0 {
		return fmt.Errorf("FUSE_INIT failed: errno=%d", errno)
	}
	return nil
}

func (c *testFuseClient) Lookup(parent uint64, name string) (uint64, FuseAttr, error) {
	payload := append([]byte(name), 0)

	resp, errno, err := c.sendRequest(FUSE_LOOKUP, parent, payload, 256)
	if err != nil {
		return 0, FuseAttr{}, err
	}
	if errno != 0 {
		return 0, FuseAttr{}, fmt.Errorf("FUSE_LOOKUP failed: errno=%d", errno)
	}

	if len(resp) < 40+88 {
		return 0, FuseAttr{}, fmt.Errorf("response too short")
	}

	nodeID := binary.LittleEndian.Uint64(resp[0:8])
	attr := decodeFuseAttr(resp[40:])

	return nodeID, attr, nil
}

func (c *testFuseClient) GetAttr(nodeID uint64) (FuseAttr, error) {
	resp, errno, err := c.sendRequest(FUSE_GETATTR, nodeID, nil, 256)
	if err != nil {
		return FuseAttr{}, err
	}
	if errno != 0 {
		return FuseAttr{}, fmt.Errorf("FUSE_GETATTR failed: errno=%d", errno)
	}

	if len(resp) < 16+88 {
		return FuseAttr{}, fmt.Errorf("response too short")
	}

	return decodeFuseAttr(resp[16:]), nil
}

func (c *testFuseClient) Create(parent uint64, name string, mode uint32, flags uint32) (uint64, uint64, FuseAttr, error) {
	payload := make([]byte, 16+len(name)+1)
	binary.LittleEndian.PutUint32(payload[0:4], flags)
	binary.LittleEndian.PutUint32(payload[4:8], mode)
	binary.LittleEndian.PutUint32(payload[8:12], 0) // umask
	copy(payload[16:], name)
	payload[16+len(name)] = 0

	resp, errno, err := c.sendRequest(FUSE_CREATE, parent, payload, 256)
	if err != nil {
		return 0, 0, FuseAttr{}, err
	}
	if errno != 0 {
		return 0, 0, FuseAttr{}, fmt.Errorf("FUSE_CREATE failed: errno=%d", errno)
	}

	if len(resp) < 40+88+16 {
		return 0, 0, FuseAttr{}, fmt.Errorf("response too short")
	}

	nodeID := binary.LittleEndian.Uint64(resp[0:8])
	attr := decodeFuseAttr(resp[40:])
	fh := binary.LittleEndian.Uint64(resp[40+88 : 40+96])

	return nodeID, fh, attr, nil
}

func (c *testFuseClient) Open(nodeID uint64, flags uint32) (uint64, error) {
	payload := make([]byte, 8)
	binary.LittleEndian.PutUint32(payload[0:4], flags)

	resp, errno, err := c.sendRequest(FUSE_OPEN, nodeID, payload, 64)
	if err != nil {
		return 0, err
	}
	if errno != 0 {
		return 0, fmt.Errorf("FUSE_OPEN failed: errno=%d", errno)
	}

	if len(resp) < 8 {
		return 0, fmt.Errorf("response too short")
	}

	return binary.LittleEndian.Uint64(resp[0:8]), nil
}

func (c *testFuseClient) Read(nodeID uint64, fh uint64, offset uint64, size uint32) ([]byte, error) {
	payload := make([]byte, 24)
	binary.LittleEndian.PutUint64(payload[0:8], fh)
	binary.LittleEndian.PutUint64(payload[8:16], offset)
	binary.LittleEndian.PutUint32(payload[16:20], size)

	resp, errno, err := c.sendRequest(FUSE_READ, nodeID, payload, fuseHdrOutSize+size)
	if err != nil {
		return nil, err
	}
	if errno != 0 {
		return nil, fmt.Errorf("FUSE_READ failed: errno=%d", errno)
	}

	return resp, nil
}

func (c *testFuseClient) Write(nodeID uint64, fh uint64, offset uint64, data []byte) (uint32, error) {
	payload := make([]byte, 32+len(data))
	binary.LittleEndian.PutUint64(payload[0:8], fh)
	binary.LittleEndian.PutUint64(payload[8:16], offset)
	binary.LittleEndian.PutUint32(payload[16:20], uint32(len(data)))
	binary.LittleEndian.PutUint32(payload[20:24], 0) // write_flags
	binary.LittleEndian.PutUint64(payload[24:32], 0) // lock_owner
	copy(payload[32:], data)

	resp, errno, err := c.sendRequest(FUSE_WRITE, nodeID, payload, 64)
	if err != nil {
		return 0, err
	}
	if errno != 0 {
		return 0, fmt.Errorf("FUSE_WRITE failed: errno=%d", errno)
	}

	if len(resp) < 4 {
		return 0, fmt.Errorf("response too short")
	}

	return binary.LittleEndian.Uint32(resp[0:4]), nil
}

func (c *testFuseClient) Release(nodeID uint64, fh uint64) error {
	payload := make([]byte, 24)
	binary.LittleEndian.PutUint64(payload[0:8], fh)

	_, errno, err := c.sendRequest(FUSE_RELEASE, nodeID, payload, 32)
	if err != nil {
		return err
	}
	if errno != 0 {
		return fmt.Errorf("FUSE_RELEASE failed: errno=%d", errno)
	}
	return nil
}

func (c *testFuseClient) Mkdir(parent uint64, name string, mode uint32) (uint64, FuseAttr, error) {
	payload := make([]byte, 8+len(name)+1)
	binary.LittleEndian.PutUint32(payload[0:4], mode)
	binary.LittleEndian.PutUint32(payload[4:8], 0) // umask
	copy(payload[8:], name)
	payload[8+len(name)] = 0

	resp, errno, err := c.sendRequest(FUSE_MKDIR, parent, payload, 256)
	if err != nil {
		return 0, FuseAttr{}, err
	}
	if errno != 0 {
		return 0, FuseAttr{}, fmt.Errorf("FUSE_MKDIR failed: errno=%d", errno)
	}

	if len(resp) < 40+88 {
		return 0, FuseAttr{}, fmt.Errorf("response too short")
	}

	nodeID := binary.LittleEndian.Uint64(resp[0:8])
	attr := decodeFuseAttr(resp[40:])

	return nodeID, attr, nil
}

func (c *testFuseClient) Rmdir(parent uint64, name string) error {
	payload := append([]byte(name), 0)

	_, errno, err := c.sendRequest(FUSE_RMDIR, parent, payload, 32)
	if err != nil {
		return err
	}
	if errno != 0 {
		return fmt.Errorf("FUSE_RMDIR failed: errno=%d", errno)
	}
	return nil
}

func (c *testFuseClient) Unlink(parent uint64, name string) error {
	payload := append([]byte(name), 0)

	_, errno, err := c.sendRequest(FUSE_UNLINK, parent, payload, 32)
	if err != nil {
		return err
	}
	if errno != 0 {
		return fmt.Errorf("FUSE_UNLINK failed: errno=%d", errno)
	}
	return nil
}

func (c *testFuseClient) StatFS(nodeID uint64) (blocks, bfree, bavail, files, ffree uint64, err error) {
	resp, errno, ferr := c.sendRequest(FUSE_STATFS, nodeID, nil, 128)
	if ferr != nil {
		return 0, 0, 0, 0, 0, ferr
	}
	if errno != 0 {
		return 0, 0, 0, 0, 0, fmt.Errorf("FUSE_STATFS failed: errno=%d", errno)
	}

	if len(resp) < 40 {
		return 0, 0, 0, 0, 0, fmt.Errorf("response too short")
	}

	return binary.LittleEndian.Uint64(resp[0:8]),
		binary.LittleEndian.Uint64(resp[8:16]),
		binary.LittleEndian.Uint64(resp[16:24]),
		binary.LittleEndian.Uint64(resp[24:32]),
		binary.LittleEndian.Uint64(resp[32:40]),
		nil
}

// Helper to decode FuseAttr from bytes
func decodeFuseAttr(b []byte) FuseAttr {
	if len(b) < 88 {
		return FuseAttr{}
	}
	return FuseAttr{
		Ino:       binary.LittleEndian.Uint64(b[0:8]),
		Size:      binary.LittleEndian.Uint64(b[8:16]),
		Blocks:    binary.LittleEndian.Uint64(b[16:24]),
		ATimeSec:  binary.LittleEndian.Uint64(b[24:32]),
		MTimeSec:  binary.LittleEndian.Uint64(b[32:40]),
		CTimeSec:  binary.LittleEndian.Uint64(b[40:48]),
		ATimeNsec: binary.LittleEndian.Uint32(b[48:52]),
		MTimeNsec: binary.LittleEndian.Uint32(b[52:56]),
		CTimeNsec: binary.LittleEndian.Uint32(b[56:60]),
		Mode:      binary.LittleEndian.Uint32(b[60:64]),
		NLink:     binary.LittleEndian.Uint32(b[64:68]),
		UID:       binary.LittleEndian.Uint32(b[68:72]),
		GID:       binary.LittleEndian.Uint32(b[72:76]),
		RDev:      binary.LittleEndian.Uint32(b[76:80]),
		BlkSize:   binary.LittleEndian.Uint32(b[80:84]),
		Flags:     binary.LittleEndian.Uint32(b[84:88]),
	}
}

// -----------------------------------------------------------------------------
// Device Initialization
// -----------------------------------------------------------------------------

func initializeFsDevice(t testing.TB, mmio *fsMMIOHelper, hiQueue, reqQueue *fsVirtqueueSetup) {
	// Check magic value
	magic := mmio.readReg(VIRTIO_MMIO_MAGIC_VALUE)
	if magic != 0x74726976 {
		t.Fatalf("invalid magic value: got 0x%x, want 0x74726976", magic)
	}

	// Reset device
	mmio.writeReg(VIRTIO_MMIO_STATUS, 0)

	// Set status bits
	mmio.writeReg(VIRTIO_MMIO_STATUS, 1)   // ACKNOWLEDGE
	mmio.writeReg(VIRTIO_MMIO_STATUS, 1|2) // DRIVER

	// Feature negotiation
	mmio.writeReg(VIRTIO_MMIO_DEVICE_FEATURES_SEL, 0)
	featuresLow := mmio.readReg(VIRTIO_MMIO_DEVICE_FEATURES)
	mmio.writeReg(VIRTIO_MMIO_DEVICE_FEATURES_SEL, 1)
	featuresHigh := mmio.readReg(VIRTIO_MMIO_DEVICE_FEATURES)

	mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES_SEL, 0)
	mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES, featuresLow)
	mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES_SEL, 1)
	mmio.writeReg(VIRTIO_MMIO_DRIVER_FEATURES, featuresHigh)

	mmio.writeReg(VIRTIO_MMIO_STATUS, 1|2|8) // FEATURES_OK

	// Configure high-priority queue (queue 0)
	mmio.writeReg(VIRTIO_MMIO_QUEUE_SEL, 0)
	mmio.writeReg(VIRTIO_MMIO_QUEUE_NUM, uint32(hiQueue.queueSize))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_LOW, uint32(hiQueue.descTableAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_HIGH, uint32(hiQueue.descTableAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_LOW, uint32(hiQueue.availRingAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_HIGH, uint32(hiQueue.availRingAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_LOW, uint32(hiQueue.usedRingAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_HIGH, uint32(hiQueue.usedRingAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_READY, 1)

	// Configure request queue (queue 1)
	mmio.writeReg(VIRTIO_MMIO_QUEUE_SEL, 1)
	mmio.writeReg(VIRTIO_MMIO_QUEUE_NUM, uint32(reqQueue.queueSize))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_LOW, uint32(reqQueue.descTableAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_DESC_HIGH, uint32(reqQueue.descTableAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_LOW, uint32(reqQueue.availRingAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_AVAIL_HIGH, uint32(reqQueue.availRingAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_LOW, uint32(reqQueue.usedRingAddr))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_USED_HIGH, uint32(reqQueue.usedRingAddr>>32))
	mmio.writeReg(VIRTIO_MMIO_QUEUE_READY, 1)

	// Driver OK
	mmio.writeReg(VIRTIO_MMIO_STATUS, 1|2|4|8)
}

// createTestSetup creates a complete test setup
func createTestSetup(t testing.TB) (*FS, *testFsBackend, *testFuseClient, *fsTestVM) {
	vm := newFsTestVM(fsTestMemorySize)
	backend := newTestFsBackend()
	fsDevice := NewFS(vm, FsDefaultMMIOBase, FsDefaultMMIOSize, FsDefaultIRQLine, "testfs", backend)
	fsDevice.Init(vm)

	mmio := newFsMMIOHelper(fsDevice)

	hiQueue := newFsVirtqueueSetup(vm, fsTestHiDescTableAddr, fsTestHiAvailRingAddr, fsTestHiUsedRingAddr, 128)
	reqQueue := newFsVirtqueueSetup(vm, fsTestReqDescTableAddr, fsTestReqAvailRingAddr, fsTestReqUsedRingAddr, 128)

	hiQueue.initRings()
	reqQueue.initRings()

	initializeFsDevice(t, mmio, hiQueue, reqQueue)

	client := newTestFuseClient(fsDevice, mmio, reqQueue, vm)

	// Initialize FUSE protocol
	if err := client.Init(); err != nil {
		t.Fatalf("FUSE init failed: %v", err)
	}

	return fsDevice, backend, client, vm
}

// -----------------------------------------------------------------------------
// Unit Tests
// -----------------------------------------------------------------------------

func TestFsInit(t *testing.T) {
	vm := newFsTestVM(fsTestMemorySize)
	backend := newTestFsBackend()
	fsDevice := NewFS(vm, FsDefaultMMIOBase, FsDefaultMMIOSize, FsDefaultIRQLine, "testfs", backend)
	fsDevice.Init(vm)

	mmio := newFsMMIOHelper(fsDevice)

	hiQueue := newFsVirtqueueSetup(vm, fsTestHiDescTableAddr, fsTestHiAvailRingAddr, fsTestHiUsedRingAddr, 128)
	reqQueue := newFsVirtqueueSetup(vm, fsTestReqDescTableAddr, fsTestReqAvailRingAddr, fsTestReqUsedRingAddr, 128)

	hiQueue.initRings()
	reqQueue.initRings()

	initializeFsDevice(t, mmio, hiQueue, reqQueue)

	client := newTestFuseClient(fsDevice, mmio, reqQueue, vm)

	if err := client.Init(); err != nil {
		t.Fatalf("FUSE init failed: %v", err)
	}
}

func TestFsGetAttr(t *testing.T) {
	_, _, client, _ := createTestSetup(t)

	attr, err := client.GetAttr(1) // Root node
	if err != nil {
		t.Fatalf("GetAttr failed: %v", err)
	}

	if attr.Ino != 1 {
		t.Errorf("expected ino=1, got %d", attr.Ino)
	}
	if attr.Mode&0040000 == 0 { // S_IFDIR
		t.Errorf("expected directory mode, got 0%o", attr.Mode)
	}
}

func TestFsCreate(t *testing.T) {
	_, _, client, _ := createTestSetup(t)

	nodeID, fh, attr, err := client.Create(1, "testfile.txt", 0644, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if nodeID == 0 {
		t.Error("expected non-zero node ID")
	}
	if fh == 0 {
		t.Error("expected non-zero file handle")
	}
	if attr.Mode&0777 != 0644 {
		t.Errorf("expected mode 0644, got 0%o", attr.Mode&0777)
	}

	// Cleanup
	if err := client.Release(nodeID, fh); err != nil {
		t.Errorf("Release failed: %v", err)
	}
}

func TestFsWriteRead(t *testing.T) {
	_, _, client, _ := createTestSetup(t)

	// Create file
	nodeID, fh, _, err := client.Create(1, "testfile.txt", 0644, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Write data
	testData := []byte("Hello, World!")
	written, err := client.Write(nodeID, fh, 0, testData)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if written != uint32(len(testData)) {
		t.Errorf("expected %d bytes written, got %d", len(testData), written)
	}

	// Read data back
	data, err := client.Read(nodeID, fh, 0, uint32(len(testData)))
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if !bytes.Equal(data, testData) {
		t.Errorf("expected %q, got %q", testData, data)
	}

	// Cleanup
	if err := client.Release(nodeID, fh); err != nil {
		t.Errorf("Release failed: %v", err)
	}
}

func TestFsMkdirRmdir(t *testing.T) {
	_, _, client, _ := createTestSetup(t)

	// Create directory
	nodeID, attr, err := client.Mkdir(1, "testdir", 0755)
	if err != nil {
		t.Fatalf("Mkdir failed: %v", err)
	}

	if nodeID == 0 {
		t.Error("expected non-zero node ID")
	}
	if attr.Mode&0040000 == 0 {
		t.Errorf("expected directory mode, got 0%o", attr.Mode)
	}

	// Remove directory
	if err := client.Rmdir(1, "testdir"); err != nil {
		t.Fatalf("Rmdir failed: %v", err)
	}

	// Verify it's gone
	_, _, err = client.Lookup(1, "testdir")
	if err == nil {
		t.Error("expected lookup to fail after rmdir")
	}
}

func TestFsLookup(t *testing.T) {
	_, _, client, _ := createTestSetup(t)

	// Create file
	expectedID, _, _, err := client.Create(1, "lookuptest.txt", 0644, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Lookup file
	nodeID, attr, err := client.Lookup(1, "lookuptest.txt")
	if err != nil {
		t.Fatalf("Lookup failed: %v", err)
	}

	if nodeID != expectedID {
		t.Errorf("expected node ID %d, got %d", expectedID, nodeID)
	}
	if attr.Mode&0100000 == 0 {
		t.Errorf("expected regular file mode, got 0%o", attr.Mode)
	}
}

func TestFsUnlink(t *testing.T) {
	_, _, client, _ := createTestSetup(t)

	// Create file
	nodeID, fh, _, err := client.Create(1, "todelete.txt", 0644, 0)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Release handle
	if err := client.Release(nodeID, fh); err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	// Delete file
	if err := client.Unlink(1, "todelete.txt"); err != nil {
		t.Fatalf("Unlink failed: %v", err)
	}

	// Verify it's gone
	_, _, err = client.Lookup(1, "todelete.txt")
	if err == nil {
		t.Error("expected lookup to fail after unlink")
	}
}

func TestFsStatFS(t *testing.T) {
	_, _, client, _ := createTestSetup(t)

	blocks, bfree, bavail, files, ffree, err := client.StatFS(1)
	if err != nil {
		t.Fatalf("StatFS failed: %v", err)
	}

	if blocks == 0 {
		t.Error("expected non-zero blocks")
	}
	if bfree == 0 {
		t.Error("expected non-zero free blocks")
	}
	if bavail == 0 {
		t.Error("expected non-zero available blocks")
	}
	if files == 0 {
		t.Error("expected non-zero files")
	}
	if ffree == 0 {
		t.Error("expected non-zero free files")
	}
}

// -----------------------------------------------------------------------------
// Benchmarks
// -----------------------------------------------------------------------------

func BenchmarkFsReadThroughput4K(b *testing.B) {
	_, backend, client, _ := createTestSetup(b)

	// Create a file with test data
	nodeID, fh, _, err := client.Create(1, "bench4k.dat", 0644, 0)
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}

	// Pre-fill file with data
	testData := make([]byte, 4096)
	for i := range testData {
		testData[i] = byte(i % 256)
	}
	backend.mu.Lock()
	node := backend.nodes[nodeID]
	node.data = testData
	node.size = uint64(len(testData))
	backend.mu.Unlock()

	b.SetBytes(4096)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		client.resetForBench()
		_, err := client.Read(nodeID, fh, 0, 4096)
		if err != nil {
			b.Fatalf("Read failed: %v", err)
		}
	}

	b.StopTimer()
	client.Release(nodeID, fh)
}

func BenchmarkFsReadThroughput64K(b *testing.B) {
	_, backend, client, _ := createTestSetup(b)

	nodeID, fh, _, err := client.Create(1, "bench64k.dat", 0644, 0)
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}

	testData := make([]byte, 64*1024)
	for i := range testData {
		testData[i] = byte(i % 256)
	}
	backend.mu.Lock()
	node := backend.nodes[nodeID]
	node.data = testData
	node.size = uint64(len(testData))
	backend.mu.Unlock()

	b.SetBytes(64 * 1024)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		client.resetForBench()
		_, err := client.Read(nodeID, fh, 0, 64*1024)
		if err != nil {
			b.Fatalf("Read failed: %v", err)
		}
	}

	b.StopTimer()
	client.Release(nodeID, fh)
}

func BenchmarkFsWriteThroughput4K(b *testing.B) {
	_, _, client, _ := createTestSetup(b)

	nodeID, fh, _, err := client.Create(1, "benchwrite4k.dat", 0644, 0)
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}

	testData := make([]byte, 4096)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	b.SetBytes(4096)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		client.resetForBench()
		_, err := client.Write(nodeID, fh, 0, testData)
		if err != nil {
			b.Fatalf("Write failed: %v", err)
		}
	}

	b.StopTimer()
	client.Release(nodeID, fh)
}

func BenchmarkFsWriteThroughput64K(b *testing.B) {
	_, _, client, _ := createTestSetup(b)

	nodeID, fh, _, err := client.Create(1, "benchwrite64k.dat", 0644, 0)
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}

	testData := make([]byte, 64*1024)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	b.SetBytes(64 * 1024)
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		client.resetForBench()
		_, err := client.Write(nodeID, fh, 0, testData)
		if err != nil {
			b.Fatalf("Write failed: %v", err)
		}
	}

	b.StopTimer()
	client.Release(nodeID, fh)
}

func BenchmarkFsRandomReadIOPS(b *testing.B) {
	_, backend, client, _ := createTestSetup(b)

	nodeID, fh, _, err := client.Create(1, "randread.dat", 0644, 0)
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}

	// Create 1MB file
	fileSize := 1024 * 1024
	testData := make([]byte, fileSize)
	for i := range testData {
		testData[i] = byte(i % 256)
	}
	backend.mu.Lock()
	node := backend.nodes[nodeID]
	node.data = testData
	node.size = uint64(len(testData))
	backend.mu.Unlock()

	blockSize := uint32(4096)
	maxBlocks := fileSize / int(blockSize)

	b.SetBytes(int64(blockSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		client.resetForBench()
		offset := uint64(rand.Intn(maxBlocks)) * uint64(blockSize)
		_, err := client.Read(nodeID, fh, offset, blockSize)
		if err != nil {
			b.Fatalf("Read failed: %v", err)
		}
	}

	b.StopTimer()
	client.Release(nodeID, fh)
}

func BenchmarkFsRandomWriteIOPS(b *testing.B) {
	_, backend, client, _ := createTestSetup(b)

	nodeID, fh, _, err := client.Create(1, "randwrite.dat", 0644, 0)
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}

	// Pre-allocate 1MB file
	fileSize := 1024 * 1024
	testData := make([]byte, fileSize)
	backend.mu.Lock()
	node := backend.nodes[nodeID]
	node.data = testData
	node.size = uint64(len(testData))
	backend.mu.Unlock()

	blockSize := 4096
	writeData := make([]byte, blockSize)
	for i := range writeData {
		writeData[i] = byte(i % 256)
	}
	maxBlocks := fileSize / blockSize

	b.SetBytes(int64(blockSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		client.resetForBench()
		offset := uint64(rand.Intn(maxBlocks)) * uint64(blockSize)
		_, err := client.Write(nodeID, fh, offset, writeData)
		if err != nil {
			b.Fatalf("Write failed: %v", err)
		}
	}

	b.StopTimer()
	client.Release(nodeID, fh)
}

func BenchmarkFsCreateFile(b *testing.B) {
	_, _, client, _ := createTestSetup(b)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		client.resetForBench()
		name := fmt.Sprintf("file%d.txt", i)
		nodeID, fh, _, err := client.Create(1, name, 0644, 0)
		if err != nil {
			b.Fatalf("Create failed: %v", err)
		}
		client.resetForBench()
		client.Release(nodeID, fh)
	}

	b.StopTimer()
}

func BenchmarkFsStatFile(b *testing.B) {
	_, _, client, _ := createTestSetup(b)

	// Create file to stat
	nodeID, fh, _, err := client.Create(1, "stattest.txt", 0644, 0)
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}
	client.Release(nodeID, fh)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		client.resetForBench()
		_, err := client.GetAttr(nodeID)
		if err != nil {
			b.Fatalf("GetAttr failed: %v", err)
		}
	}

	b.StopTimer()
}

func BenchmarkFsLookup(b *testing.B) {
	_, _, client, _ := createTestSetup(b)

	// Create file to lookup
	nodeID, fh, _, err := client.Create(1, "lookupbench.txt", 0644, 0)
	if err != nil {
		b.Fatalf("Create failed: %v", err)
	}
	client.Release(nodeID, fh)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		client.resetForBench()
		_, _, err := client.Lookup(1, "lookupbench.txt")
		if err != nil {
			b.Fatalf("Lookup failed: %v", err)
		}
	}

	b.StopTimer()
}

func BenchmarkFsCreateStatDelete(b *testing.B) {
	_, _, client, _ := createTestSetup(b)

	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		name := fmt.Sprintf("lifecycle%d.txt", i)

		// Create
		client.resetForBench()
		nodeID, fh, _, err := client.Create(1, name, 0644, 0)
		if err != nil {
			b.Fatalf("Create failed: %v", err)
		}

		// Stat
		client.resetForBench()
		_, err = client.GetAttr(nodeID)
		if err != nil {
			b.Fatalf("GetAttr failed: %v", err)
		}

		// Release
		client.resetForBench()
		client.Release(nodeID, fh)

		// Delete
		client.resetForBench()
		err = client.Unlink(1, name)
		if err != nil {
			b.Fatalf("Unlink failed: %v", err)
		}
	}

	b.StopTimer()
}

func BenchmarkFsFileLifecycle(b *testing.B) {
	_, _, client, _ := createTestSetup(b)

	testData := make([]byte, 4096)
	for i := range testData {
		testData[i] = byte(i % 256)
	}

	b.SetBytes(int64(len(testData)) * 2) // Write + Read
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		name := fmt.Sprintf("fullcycle%d.txt", i)

		// Create
		client.resetForBench()
		nodeID, fh, _, err := client.Create(1, name, 0644, 0)
		if err != nil {
			b.Fatalf("Create failed: %v", err)
		}

		// Write
		client.resetForBench()
		_, err = client.Write(nodeID, fh, 0, testData)
		if err != nil {
			b.Fatalf("Write failed: %v", err)
		}

		// Read
		client.resetForBench()
		_, err = client.Read(nodeID, fh, 0, uint32(len(testData)))
		if err != nil {
			b.Fatalf("Read failed: %v", err)
		}

		// Release
		client.resetForBench()
		client.Release(nodeID, fh)

		// Delete
		client.resetForBench()
		err = client.Unlink(1, name)
		if err != nil {
			b.Fatalf("Unlink failed: %v", err)
		}
	}

	b.StopTimer()
}
