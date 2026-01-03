package virtio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"sync"

	"github.com/tinyrange/cc/internal/debug"
	"github.com/tinyrange/cc/internal/fdt"
	"github.com/tinyrange/cc/internal/hv"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
)

// -----------------------------
// Virtio-fs device constants
// -----------------------------
const (
	FsDefaultMMIOBase = 0xd0004000
	FsDefaultMMIOSize = 0x200
	FsDefaultIRQLine  = 9
	fsArmDefaultIRQ   = 41

	fsHiprioQueueIndex  = 0
	fsRequestQueueBase  = 1
	fsRequestQueueCount = 1 // single request queue for this basic device
	fsTotalQueueCount   = fsRequestQueueBase + fsRequestQueueCount
	fsQueueNumMax       = 128

	fsVendorID = 0x554d4551 // "QEMU" (arbitrary – matching block)
	fsVersion  = 2
	fsDeviceID = 26 // VIRTIO_ID_FS

	fsInterruptBit = 0x1
)

// virtio feature bits used by virtio-fs (very small subset)
var fsFeatures = []uint64{
	virtioFeatureVersion1,
}

// virtio-fs config space (subset). See linux/include/uapi/linux/virtio_fs.h
//
//	struct virtio_fs_config {
//	    char tag[36];
//	    __le32 num_request_queues;
//	};
const (
	fsCfgTagOffset  = 0
	fsCfgTagSize    = 36
	fsCfgNumQOffset = fsCfgTagOffset + fsCfgTagSize
	fsCfgTotalSize  = fsCfgNumQOffset + 4
)

type FSTemplate struct {
	Tag      string
	Backend  FsBackend
	Arch     hv.CpuArchitecture
	MMIOBase uint64
	IRQLine  uint32
}

func (t FSTemplate) mmioBaseOrDefault() uint64 {
	if t.MMIOBase != 0 {
		return t.MMIOBase
	}
	return FsDefaultMMIOBase
}

func (t FSTemplate) archOrDefault(vm hv.VirtualMachine) hv.CpuArchitecture {
	if t.Arch != "" && t.Arch != hv.ArchitectureInvalid {
		return t.Arch
	}
	if vm != nil && vm.Hypervisor() != nil {
		return vm.Hypervisor().Architecture()
	}
	return hv.ArchitectureInvalid
}

func (t FSTemplate) irqLineForArch(arch hv.CpuArchitecture) uint32 {
	if t.IRQLine != 0 {
		return t.IRQLine
	}
	if arch == hv.ArchitectureARM64 {
		return fsArmDefaultIRQ
	}
	return FsDefaultIRQLine
}

// GetLinuxCommandLineParam implements VirtioMMIODevice.
func (t FSTemplate) GetLinuxCommandLineParam() ([]string, error) {
	irqLine := t.irqLineForArch(t.Arch)
	param := fmt.Sprintf(
		"virtio_mmio.device=4k@0x%x:%d",
		t.mmioBaseOrDefault(),
		irqLine,
	)
	return []string{param}, nil
}

// DeviceTreeNodes implements VirtioMMIODevice.
func (t FSTemplate) DeviceTreeNodes() ([]fdt.Node, error) {
	irqLine := t.irqLineForArch(t.Arch)
	node := fdt.Node{
		Name: fmt.Sprintf("virtio@%x", t.mmioBaseOrDefault()),
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"virtio,mmio"}},
			"reg":        {U64: []uint64{t.mmioBaseOrDefault(), FsDefaultMMIOSize}},
			"interrupts": {U32: []uint32{0, irqLine, 4}},
			"status":     {Strings: []string{"okay"}},
		},
	}
	return []fdt.Node{node}, nil
}

// GetACPIDeviceInfo implements VirtioMMIODevice.
func (t FSTemplate) GetACPIDeviceInfo() ACPIDeviceInfo {
	irqLine := t.irqLineForArch(t.archOrDefault(nil))
	return ACPIDeviceInfo{
		BaseAddr: t.mmioBaseOrDefault(),
		Size:     FsDefaultMMIOSize,
		GSI:      irqLine,
	}
}

func (t FSTemplate) Create(vm hv.VirtualMachine) (hv.Device, error) {
	arch := t.archOrDefault(vm)
	irqLine := t.irqLineForArch(arch)
	fs := NewFS(vm, t.mmioBaseOrDefault(), FsDefaultMMIOSize, EncodeIRQLineForArch(arch, irqLine), t.Tag, t.Backend)
	if err := fs.Init(vm); err != nil {
		return nil, fmt.Errorf("virtio-fs: initialize device: %w", err)
	}
	return fs, nil
}

var (
	_ hv.DeviceTemplate = FSTemplate{}
	_ VirtioMMIODevice  = FSTemplate{}
)

// -----------------------------
// FUSE protocol (subset) types
// -----------------------------
// Wire format follows Linux FUSE. We implement a tiny subset.
//
// struct fuse_in_header {
//   __u32 len;
//   __u32 opcode;
//   __u64 unique;
//   __u64 nodeid;
//   __u32 uid;
//   __u32 gid;
//   __u32 pid;
//   __u32 padding;
// };
// struct fuse_out_header {
//   __u32 len;     // total length including this header
//   __s32 error;   // 0 or -errno
//   __u64 unique;  // echo of request
// };

const (
	fuseHdrInSize  = 40
	fuseHdrOutSize = 16
)

type fuseInHeader struct {
	Len         uint32
	Opcode      uint32
	Unique      uint64
	NodeID      uint64
	UID         uint32
	GID         uint32
	PID         uint32
	TotalExtLen uint16 // length of extensions in 8byte units (protocol 7.38+)
	Padding     uint16
}

type fuseOutHeader struct {
	Len    uint32
	Error  int32
	Unique uint64
}

// FUSE opcodes (subset)
const (
	FUSE_LOOKUP      = 1
	FUSE_FORGET      = 2
	FUSE_GETATTR     = 3
	FUSE_SETATTR     = 4
	FUSE_READLINK    = 5
	FUSE_SYMLINK     = 6
	FUSE_MKNOD       = 8
	FUSE_MKDIR       = 9
	FUSE_UNLINK      = 10
	FUSE_RMDIR       = 11
	FUSE_RENAME      = 12
	FUSE_LINK        = 13
	FUSE_OPEN        = 14
	FUSE_READ        = 15
	FUSE_WRITE       = 16
	FUSE_STATFS      = 17
	FUSE_RELEASE     = 18
	FUSE_FSYNC       = 20
	FUSE_SETXATTR    = 21
	FUSE_GETXATTR    = 22
	FUSE_LISTXATTR   = 23
	FUSE_REMOVEXATTR = 24
	FUSE_FLUSH       = 25
	FUSE_INIT        = 26
	FUSE_OPENDIR     = 27
	FUSE_READDIR     = 28
	FUSE_RELEASEDIR  = 29
	FUSE_FSYNCDIR    = 30
	FUSE_CREATE      = 35
	FUSE_RENAME2     = 45
	FUSE_LSEEK       = 46
)

// Minimal structs we need on the wire (host end)
// Note: All little-endian.

type FuseAttr struct {
	Ino       uint64
	Size      uint64
	Blocks    uint64
	ATimeSec  uint64
	MTimeSec  uint64
	CTimeSec  uint64
	ATimeNsec uint32
	MTimeNsec uint32
	CTimeNsec uint32
	Mode      uint32
	NLink     uint32
	UID       uint32
	GID       uint32
	RDev      uint32
	BlkSize   uint32
	Flags     uint32
}

const fuseEntryOutSize = 8 + 8 + 8 + 8 + 8 + 8 + 8 + 0 // we’ll compose manually

func encodeFuseAttr(dst []byte, attr FuseAttr) {
	if len(dst) < 88 {
		return
	}
	putU64 := func(off int, val uint64) {
		binary.LittleEndian.PutUint64(dst[off:off+8], val)
	}
	putU32 := func(off int, val uint32) {
		binary.LittleEndian.PutUint32(dst[off:off+4], val)
	}
	putU64(0, attr.Ino)
	putU64(8, attr.Size)
	putU64(16, attr.Blocks)
	putU64(24, attr.ATimeSec)
	putU64(32, attr.MTimeSec)
	putU64(40, attr.CTimeSec)
	putU32(48, attr.ATimeNsec)
	putU32(52, attr.MTimeNsec)
	putU32(56, attr.CTimeNsec)
	putU32(60, attr.Mode)
	putU32(64, attr.NLink)
	putU32(68, attr.UID)
	putU32(72, attr.GID)
	putU32(76, attr.RDev)
	putU32(80, attr.BlkSize)
	putU32(84, attr.Flags)
}

// fuse_init_in / fuse_init_out (subset)

type fuseInitIn struct {
	Major        uint32
	Minor        uint32
	MaxReadahead uint32
	Flags        uint32
	Flags2       uint32 // protocol 7.36+
	Unused       [11]uint32
}

type fuseInitOut struct {
	Major               uint32
	Minor               uint32
	MaxReadahead        uint32
	Flags               uint32
	MaxBackground       uint16
	CongestionThreshold uint16
	MaxWrite            uint32
	TimeGran            uint32
	MaxPages            uint16 // protocol 7.28+
	MapAlignment        uint16 // protocol 7.31+
	Flags2              uint32 // protocol 7.36+
	MaxStackDepth       uint32 // protocol 7.40+
	RequestTimeout      uint16 // protocol 7.43+
	Unused              [11]uint16
}

// -----------------------------
// Backend interface
// -----------------------------
// This hides the actual filesystem. Provide your own implementation.

// FsBackend should be safe for concurrent calls.
type FsBackend interface {
	// Init lets the backend constrain MaxWrite, flags etc.
	Init() (maxWrite uint32, flags uint32)

	// Root attributes; NodeID 1 is the root.
	GetAttr(nodeID uint64) (attr FuseAttr, errno int32)

	// Lookup child by name under a directory nodeID. Must return new Ino,
	// attributes and a generation (we pass 0).
	Lookup(parent uint64, name string) (nodeID uint64, attr FuseAttr, errno int32)

	Open(nodeID uint64, flags uint32) (fh uint64, errno int32)
	Release(nodeID uint64, fh uint64)

	Read(nodeID uint64, fh uint64, off uint64, size uint32) ([]byte, int32)

	ReadDir(nodeID uint64, off uint64, maxBytes uint32) ([]byte, int32)

	StatFS(nodeID uint64) (blocks, bfree, bavail, files, ffree, bsize, frsize, namelen uint64, errno int32)
}

// Optional directory-handle interfaces.
//
// These provide a place to implement fully correct getdents64 / d_off semantics:
// - Stable offsets (cookies) across pagination.
// - Deterministic inode/name pairs within a directory stream.
// - No "empty page == EOF" behavior when the kernel asks for a small buffer.
//
// If not implemented, the device falls back to FsBackend.ReadDir(nodeID, off, size).
type fsOpenDirBackend interface {
	OpenDir(nodeID uint64, flags uint32) (fh uint64, errno int32)
	ReleaseDir(nodeID uint64, fh uint64)
}

type fsReadDirHandleBackend interface {
	ReadDirHandle(nodeID uint64, fh uint64, off uint64, maxBytes uint32) ([]byte, int32)
}

type fsCreateBackend interface {
	Create(parent uint64, name string, mode uint32, flags uint32, umask uint32, uid uint32, gid uint32) (nodeID uint64, fh uint64, attr FuseAttr, errno int32)
}

type fsMkdirBackend interface {
	Mkdir(parent uint64, name string, mode uint32, umask uint32, uid uint32, gid uint32) (nodeID uint64, attr FuseAttr, errno int32)
}

type fsMknodBackend interface {
	Mknod(parent uint64, name string, mode uint32, rdev uint32, umask uint32, uid uint32, gid uint32) (nodeID uint64, attr FuseAttr, errno int32)
}

type fsWriteBackend interface {
	Write(nodeID uint64, fh uint64, off uint64, data []byte) (uint32, int32)
}

type fsXattrBackend interface {
	SetXattr(nodeID uint64, name string, value []byte, flags uint32) int32
	GetXattr(nodeID uint64, name string) ([]byte, int32)
	ListXattr(nodeID uint64) ([]byte, int32)
	RemoveXattr(nodeID uint64, name string) int32
}

type fsSymlinkBackend interface {
	// Symlink creates a new symlink named `name` in directory `parent` which points to `target`.
	// Returns the new nodeID and its attributes.
	Symlink(parent uint64, name string, target string, umask uint32, uid uint32, gid uint32) (nodeID uint64, attr FuseAttr, errno int32)
}

type fsReadlinkBackend interface {
	// Readlink returns the link target for a symlink node.
	Readlink(nodeID uint64) (target string, errno int32)
}

type fsRenameBackend interface {
	Rename(oldParent uint64, oldName string, newParent uint64, newName string, flags uint32) int32
}

type fsRemoveBackend interface {
	Unlink(parent uint64, name string) int32
	Rmdir(parent uint64, name string) int32
}

type fsSetattrBackend interface {
	SetAttr(nodeID uint64, size *uint64, mode *uint32, uid *uint32, gid *uint32) int32
}

type fsLseekBackend interface {
	Lseek(nodeID uint64, fh uint64, offset uint64, whence uint32) (uint64, int32)
}

type fsLinkBackend interface {
	// Link creates a hard link: a new directory entry `newName` in `newParent` pointing to `oldNodeID`.
	Link(oldNodeID uint64, newParent uint64, newName string) (nodeID uint64, attr FuseAttr, errno int32)
}

// A trivial in-memory backend placeholder that exposes an empty root.
// Replace with a real passthrough backend if desired.

type emptyBackend struct{}

func (emptyBackend) Init() (uint32, uint32) { return 128 * 1024, 0 }
func (emptyBackend) GetAttr(nodeID uint64) (FuseAttr, int32) {
	// Root dir with 0755
	if nodeID != 1 {
		return FuseAttr{}, -int32(linux.ENOENT)
	}
	return FuseAttr{
		Ino: 1, Mode: 0040000 | 0755, NLink: 2, UID: 0, GID: 0,
		BlkSize: 4096,
	}, 0
}
func (emptyBackend) Lookup(_ uint64, _ string) (uint64, FuseAttr, int32) {
	return 0, FuseAttr{}, -int32(linux.ENOENT)
}
func (emptyBackend) Open(nodeID uint64, _ uint32) (uint64, int32) {
	if nodeID == 1 {
		return 0, -int32(linux.EISDIR)
	}
	return 0, -int32(linux.ENOENT)
}
func (emptyBackend) Release(uint64, uint64) {}
func (emptyBackend) Read(uint64, uint64, uint64, uint32) ([]byte, int32) {
	return nil, -int32(linux.EISDIR)
}
func (emptyBackend) ReadDir(nodeID uint64, off uint64, _ uint32) ([]byte, int32) {
	// Return "." and ".." only when off==0. Use Linux dirent layout for FUSE.
	if nodeID != 1 {
		return nil, -int32(linux.ENOENT)
	}
	if off != 0 {
		return []byte{}, 0
	}
	// Minimal, but many guests can tolerate empty readdir.
	return []byte{}, 0
}
func (emptyBackend) StatFS(uint64) (uint64, uint64, uint64, uint64, uint64, uint64, uint64, uint64, int32) {
	return 0, 0, 0, 1, 1, 4096, 4096, 255, 0
}

// -----------------------------
// Virtio-fs device
// -----------------------------

type FS struct {
	device device

	base    uint64
	size    uint64
	irqLine uint32
	arch    hv.CpuArchitecture

	bufPool sync.Pool
	backend FsBackend

	// config
	tag       [fsCfgTagSize]byte
	numQueues uint32
}

func NewFS(vm hv.VirtualMachine, base, size uint64, irqLine uint32, tag string, backend FsBackend) *FS {
	fs := &FS{
		base:      base,
		size:      size,
		irqLine:   irqLine,
		backend:   backend,
		numQueues: fsRequestQueueCount,
		bufPool:   sync.Pool{New: func() any { return make([]byte, 0, 64*1024) }},
	}
	fs.setTag(tag)
	fs.setupDevice(vm)
	debug.Writef("virtio-fs.NewFS", "base=0x%x size=0x%x irqLine=%d tag=%q backendNil=%t", base, size, irqLine, tag, backend == nil)
	return fs
}

func (v *FS) setTag(tag string) {
	if len(tag) > fsCfgTagSize {
		tag = tag[:fsCfgTagSize]
	}
	v.tag = [fsCfgTagSize]byte{}
	copy(v.tag[:], []byte(tag))
}

func (v *FS) setupDevice(vm hv.VirtualMachine) {
	if vm != nil && vm.Hypervisor() != nil {
		v.arch = vm.Hypervisor().Architecture()
	}
	if v.backend == nil {
		v.backend = emptyBackend{}
	}
	v.device = newMMIODevice(vm, v.base, v.size, v.irqLine, fsDeviceID, fsVendorID, fsVersion, fsFeatures, v)
	if mmio, ok := v.device.(*mmioDevice); ok && vm != nil {
		mmio.vm = vm
	}
}

// Init implements hv.MemoryMappedIODevice.
func (v *FS) Init(vm hv.VirtualMachine) error {
	if v.device == nil {
		if vm == nil {
			return fmt.Errorf("virtio-fs: virtual machine is nil")
		}
		v.setupDevice(vm)
		return nil
	}
	if mmio, ok := v.device.(*mmioDevice); ok && vm != nil {
		mmio.vm = vm
	}
	return nil
}

// MMIORegions implements hv.MemoryMappedIODevice.
func (v *FS) MMIORegions() []hv.MMIORegion {
	if v.size == 0 {
		return nil
	}
	return []hv.MMIORegion{{
		Address: v.base,
		Size:    v.size,
	}}
}

// ReadMMIO implements hv.MemoryMappedIODevice.
func (v *FS) ReadMMIO(addr uint64, data []byte) error {
	dev, err := v.requireDevice()
	if err != nil {
		return err
	}
	return dev.readMMIO(addr, data)
}

// WriteMMIO implements hv.MemoryMappedIODevice.
func (v *FS) WriteMMIO(addr uint64, data []byte) error {
	dev, err := v.requireDevice()
	if err != nil {
		return err
	}
	return dev.writeMMIO(addr, data)
}

func (v *FS) requireDevice() (device, error) {
	if v.device == nil {
		return nil, fmt.Errorf("virtio-fs: device not initialized")
	}
	return v.device, nil
}

// Virtio device facade
func (v *FS) NumQueues() int          { return fsTotalQueueCount }
func (v *FS) QueueMaxSize(int) uint16 { return fsQueueNumMax }
func (v *FS) OnReset(device)          {}

func (v *FS) OnQueueNotify(dev device, qidx int) error {
	if qidx < fsHiprioQueueIndex || qidx >= fsTotalQueueCount {
		debug.Writef("virtio-fs.OnQueueNotify ignore", "qidx=%d (total=%d)", qidx, fsTotalQueueCount)
		return nil
	}
	q := dev.queue(qidx)
	if q == nil {
		debug.Writef("virtio-fs.OnQueueNotify q==nil", "qidx=%d", qidx)
	} else {
		debug.Writef("virtio-fs.OnQueueNotify q!=nil", "qidx=%d ready=%t size=%d lastAvailIdx=%d usedIdx=%d", qidx, q.ready, q.size, q.lastAvailIdx, q.usedIdx)
	}
	return v.processQueue(dev, q)
}

// Config space
func (v *FS) ReadConfig(_ device, off uint64) (uint32, bool, error) {
	// When called through deviceHandlerAdapter, off is already relative to VIRTIO_MMIO_CONFIG
	// When called directly from MMIO handler, off is absolute and we need to subtract VIRTIO_MMIO_CONFIG
	cfg := off
	if off >= VIRTIO_MMIO_CONFIG {
		cfg = off - VIRTIO_MMIO_CONFIG
	}
	switch {
	case cfg < fsCfgTagSize:
		// tag presented as little-endian 32-bit windows
		idx := int(cfg)
		var w [4]byte
		for i := 0; i < 4; i++ {
			if idx+i < len(v.tag) {
				w[i] = v.tag[idx+i]
			}
		}
		return binary.LittleEndian.Uint32(w[:]), true, nil
	case cfg >= fsCfgNumQOffset && cfg < fsCfgNumQOffset+4:
		return uint32(v.numQueues), true, nil
	default:
		return 0, false, nil
	}
}
func (v *FS) WriteConfig(device, uint64, uint32) (bool, error) { return false, nil }

// ------------- queue processing -------------

func (v *FS) processQueue(dev device, q *queue) error {
	if q == nil || !q.ready || q.size == 0 {
		if q == nil {
			debug.Writef("virtio-fs.processQueue skip", "queue=nil")
		} else {
			debug.Writef("virtio-fs.processQueue skip", "ready=%t size=%d", q.ready, q.size)
		}
		return nil
	}

	availFlags, availIdx, err := dev.readAvailState(q)
	if err != nil {
		debug.Writef("virtio-fs.processQueue readAvailState err", "error=%v", err)
		return err
	}
	debug.Writef("virtio-fs.processQueue availFlags", "0x%x availIdx=%d lastAvailIdx=%d", availFlags, availIdx, q.lastAvailIdx)
	var interruptNeeded bool

	for q.lastAvailIdx != availIdx {
		ringIndex := q.lastAvailIdx % q.size
		head, err := dev.readAvailEntry(q, ringIndex)
		if err != nil {
			debug.Writef("virtio-fs.processQueue readAvailEntry err", "ringIndex=%d error=%v", ringIndex, err)
			return err
		}
		debug.Writef("virtio-fs.processQueue handle", "head=%d ringIndex=%d", head, ringIndex)
		usedLen, err := v.handleRequest(dev, q, head)
		if err != nil {
			debug.Writef("virtio-fs.processQueue handleRequest", "head=%d error=%v", head, err)
			return err
		}
		debug.Writef("virtio-fs.processQueue recordUsed", "head=%d usedLen=%d", head, usedLen)
		if err := dev.recordUsedElement(q, head, usedLen); err != nil {
			debug.Writef("virtio-fs.processQueue recordUsedElement", "head=%d error=%v", head, err)
			return err
		}
		q.lastAvailIdx++
		interruptNeeded = true
	}
	if interruptNeeded && (availFlags&1) == 0 {
		debug.Writef("virtio-fs.processQueue raiseInterrupt", "bit=0x%x", fsInterruptBit)
		dev.raiseInterrupt(fsInterruptBit)
	}
	return nil
}

type fsDesc struct {
	addr   uint64
	length uint32
	write  bool
	next   bool
	nextID uint16
}

func (v *FS) handleRequest(dev device, q *queue, head uint16) (uint32, error) {
	// Expect a simple 2-descriptor chain: [in: request][out: reply]
	descs, err := v.readDescriptorChain(dev, q, head)
	if err != nil {
		debug.Writef("virtio-fs.handleRequest readDescriptorChain", "head=%d error=%v", head, err)
		return 0, err
	}
	if len(descs) == 0 {
		debug.Writef("virtio-fs.handleRequest", "head=%d no descriptors", head)
		return 0, errors.New("virtio-fs: no descriptors in request")
	}
	debug.Writef("virtio-fs.handleRequest", "head=%d descs=%d", head, len(descs))

	var reqDescs, respDescs []fsDesc
	for _, d := range descs {
		if d.write {
			respDescs = append(respDescs, d)
			continue
		}
		if len(respDescs) != 0 {
			return 0, errors.New("virtio-fs: read descriptor after write descriptor")
		}
		reqDescs = append(reqDescs, d)
	}
	if len(reqDescs) == 0 {
		debug.Writef("virtio-fs.handleRequest no request descriptors", "head=%d", head)
		return 0, errors.New("virtio-fs: no request descriptors")
	}

	var reqLen int
	for _, d := range reqDescs {
		reqLen += int(d.length)
	}
	if reqLen == 0 {
		debug.Writef("virtio-fs.handleRequest empty request payload", "head=%d", head)
		return 0, errors.New("virtio-fs: empty request payload")
	}
	debug.Writef("virtio-fs.handleRequest", "reqDesc=%d reqLen=%d respDesc=%d", len(reqDescs), reqLen, len(respDescs))
	reqBuf := v.getBuffer(reqLen)
	defer v.putBuffer(reqBuf)
	// Clear request buffer to avoid garbage data from buffer pool reuse
	clear(reqBuf[:reqLen])
	copyOffset := 0
	for _, d := range reqDescs {
		segLen := int(d.length)
		if segLen == 0 {
			continue
		}
		seg, err := dev.readGuest(d.addr, d.length)
		if err != nil {
			debug.Writef("virtio-fs.handleRequest readGuest", "head=%d addr=0x%x len=%d error=%v", head, d.addr, d.length, err)
			return 0, err
		}
		copy(reqBuf[copyOffset:], seg[:segLen])
		copyOffset += segLen
	}

	opcode := binary.LittleEndian.Uint32(reqBuf[4:8])
	if len(respDescs) == 0 {
		if opcode == FUSE_FORGET {
			debug.Writef("virtio-fs.handleRequest no resp (FORGET)", "head=%d opcode=%s", head, fuseOpcodeString(opcode))
			return 0, nil
		}
		debug.Writef("virtio-fs.handleRequest no response descriptors", "head=%d opcode=%s", head, fuseOpcodeString(opcode))
		return 0, errors.New("virtio-fs: no response descriptors")
	}

	var respCap int
	for _, d := range respDescs {
		respCap += int(d.length)
	}
	if respCap == 0 {
		debug.Writef("virtio-fs.handleRequest respCap=0", "head=%d opcode=%s", head, fuseOpcodeString(opcode))
		return 0, errors.New("virtio-fs: zero-length response buffer")
	}
	debug.Writef("virtio-fs.handleRequest", "respCap=%d head=%d opcode=%s", respCap, head, fuseOpcodeString(opcode))
	respBuf := v.getBuffer(respCap)
	defer v.putBuffer(respBuf)
	// Zero the response buffer to avoid garbage data
	clear(respBuf[:respCap])

	used, err := v.dispatchFUSE(reqBuf[:reqLen], respBuf[:respCap])
	if err != nil {
		debug.Writef("virtio-fs.handleRequest dispatch", "head=%d opcode=%s error=%v", head, fuseOpcodeString(opcode), err)
		return 0, err
	}
	if used == 0 {
		used = fuseHdrOutSize
	} // ensure progress
	if int(used) > respCap {
		debug.Writef("virtio-fs.handleRequest too-large", "head=%d opcode=%s used=%d respCap=%d", head, fuseOpcodeString(opcode), used, respCap)
		return 0, fmt.Errorf("virtio-fs: response too large (need %d, have %d)", used, respCap)
	}
	debug.Writef("virtio-fs.handleRequest", "used=%d head=%d opcode=%s", used, head, fuseOpcodeString(opcode))

	remaining := int(used)
	copyOffset = 0
	for _, d := range respDescs {
		chunk := int(d.length)
		if chunk == 0 {
			continue
		}
		if chunk > remaining {
			chunk = remaining
		}
		if err := dev.writeGuest(d.addr, respBuf[copyOffset:copyOffset+chunk]); err != nil {
			debug.Writef("virtio-fs.handleRequest writeGuest", "head=%d opcode=%s addr=0x%x chunk=%d error=%v", head, fuseOpcodeString(opcode), d.addr, chunk, err)
			return 0, err
		}
		copyOffset += chunk
		remaining -= chunk
		if remaining == 0 {
			break
		}
	}
	if remaining != 0 {
		debug.Writef("virtio-fs.handleRequest descriptors exhausted", "head=%d opcode=%s remaining=%d", head, fuseOpcodeString(opcode), remaining)
		return 0, errors.New("virtio-fs: response descriptors exhausted")
	}

	return used, nil
}

func (v *FS) readDescriptorChain(dev device, q *queue, head uint16) ([]fsDesc, error) {
	idx := head
	var descs []fsDesc
	for i := uint16(0); i < q.size; i++ {
		desc, err := dev.readDescriptor(q, idx)
		if err != nil {
			debug.Writef("virtio-fs.readDescriptorChain failed", "head=%d idx=%d error=%v", head, idx, err)
			return nil, err
		}
		// Log each descriptor in the chain; this is critical when debugging guest/host ring desync.
		debug.Writef("virtio-fs.readDescriptorChain", "head=%d idx=%d addr=0x%x len=%d flags=0x%x next=%d", head, idx, desc.addr, desc.length, desc.flags, desc.next)
		descs = append(descs, fsDesc{
			addr:   desc.addr,
			length: desc.length,
			write:  desc.flags&virtqDescFWrite != 0,
			next:   desc.flags&virtqDescFNext != 0,
			nextID: desc.next,
		})
		if desc.flags&virtqDescFNext == 0 {
			break
		}
		idx = desc.next
	}
	debug.Writef("virtio-fs.readDescriptorChain completed", "head=%d total=%d", head, len(descs))
	return descs, nil
}

// -----------------------------
// FUSE dispatcher (very small subset)
// -----------------------------

func (v *FS) dispatchFUSE(req []byte, resp []byte) (uint32, error) {
	if len(req) < fuseHdrInSize || len(resp) < fuseHdrOutSize {
		debug.Writef("virtio-fs.dispatchFUSE short buffers", "req=%d resp=%d", len(req), len(resp))
		return 0, fmt.Errorf("virtio-fs: short buffers req=%d resp=%d", len(req), len(resp))
	}
	var in fuseInHeader
	in.Len = binary.LittleEndian.Uint32(req[0:4])
	in.Opcode = binary.LittleEndian.Uint32(req[4:8])
	in.Unique = binary.LittleEndian.Uint64(req[8:16])
	in.NodeID = binary.LittleEndian.Uint64(req[16:24])
	in.UID = binary.LittleEndian.Uint32(req[24:28])
	in.GID = binary.LittleEndian.Uint32(req[28:32])
	in.PID = binary.LittleEndian.Uint32(req[32:36])
	if len(req) >= 40 {
		in.TotalExtLen = binary.LittleEndian.Uint16(req[36:38])
		in.Padding = binary.LittleEndian.Uint16(req[38:40])
	}
	debug.Writef("virtio-fs.dispatchFUSE in", "len=%d opcode=%s unique=%d node=%d uid=%d gid=%d pid=%d extLen=%d", in.Len, fuseOpcodeString(in.Opcode), in.Unique, in.NodeID, in.UID, in.GID, in.PID, in.TotalExtLen)

	w := func(h fuseOutHeader, extra []byte) uint32 {
		binary.LittleEndian.PutUint32(resp[0:4], h.Len)
		binary.LittleEndian.PutUint32(resp[4:8], uint32(h.Error))
		binary.LittleEndian.PutUint64(resp[8:16], h.Unique)
		if len(extra) > 0 {
			copy(resp[fuseHdrOutSize:], extra)
		}
		return h.Len
	}

	errno := int32(0)
	switch in.Opcode {
	case FUSE_INIT:
		debug.Writef("virtio-fs.dispatchFUSE op=INIT", "in=%+v", in)
		// parse init_in
		if len(req) < fuseHdrInSize+16 {
			debug.Writef("virtio-fs.dispatchFUSE op=INIT too short", "in=%+v", in)
			return 0, fmt.Errorf("FUSE_INIT too short")
		}
		maj := binary.LittleEndian.Uint32(req[40:44])
		_ = maj // we accept any >= 7
		maxWrite, flags := v.backend.Init()
		var out fuseInitOut
		out.Major = 7
		out.Minor = 31
		out.MaxReadahead = 128 * 1024
		out.Flags = flags
		out.MaxBackground = 16
		out.CongestionThreshold = 32
		if maxWrite == 0 {
			maxWrite = 128 * 1024
		}
		out.MaxWrite = maxWrite
		out.TimeGran = 1
		// Serialize
		extra := make([]byte, 40)
		binary.LittleEndian.PutUint32(extra[0:4], out.Major)
		binary.LittleEndian.PutUint32(extra[4:8], out.Minor)
		binary.LittleEndian.PutUint32(extra[8:12], out.MaxReadahead)
		binary.LittleEndian.PutUint32(extra[12:16], out.Flags)
		binary.LittleEndian.PutUint16(extra[16:18], out.MaxBackground)
		binary.LittleEndian.PutUint16(extra[18:20], out.CongestionThreshold)
		binary.LittleEndian.PutUint32(extra[20:24], out.MaxWrite)
		binary.LittleEndian.PutUint32(extra[24:28], out.TimeGran)
		// rest zero
		return w(fuseOutHeader{Len: fuseHdrOutSize + uint32(len(extra)), Error: 0, Unique: in.Unique}, extra), nil

	case FUSE_GETATTR:
		debug.Writef("virtio-fs.dispatchFUSE op=GETATTR", "node=%d", in.NodeID)
		attr, e := v.backend.GetAttr(in.NodeID)
		errno = e
		if errno == 0 {
			// fuse_attr_out = { attr_valid, attr_valid_nsec, dummy, attr }
			extra := make([]byte, 16+88)
			binary.LittleEndian.PutUint64(extra[0:8], 1)
			binary.LittleEndian.PutUint32(extra[8:12], 0)
			binary.LittleEndian.PutUint32(extra[12:16], 0)
			encodeFuseAttr(extra[16:], attr)
			return w(fuseOutHeader{Len: fuseHdrOutSize + uint32(len(extra)), Error: 0, Unique: in.Unique}, extra), nil
		}

	case FUSE_LOOKUP:
		debug.Writef("virtio-fs.dispatchFUSE op=LOOKUP", "parent=%d", in.NodeID)
		// payload: name (NUL-terminated)
		name := string(req[fuseHdrInSize:])
		if i := indexNull(name); i >= 0 {
			name = name[:i]
		}
		if name == "." {
			name = ""
		}
		debug.Writef("virtio-fs.dispatchFUSE op=LOOKUP", "name=%q", name)
		nid, attr, e := v.backend.Lookup(in.NodeID, path.Clean(name))
		errno = e
		if errno == 0 {
			// fuse_entry_out
			extra := make([]byte, 40+88)
			binary.LittleEndian.PutUint64(extra[0:8], nid)
			// generation remains zero
			binary.LittleEndian.PutUint64(extra[16:24], 1) // entry_valid
			binary.LittleEndian.PutUint64(extra[24:32], 1) // attr_valid
			// entry_valid_nsec, attr_valid_nsec already zero
			encodeFuseAttr(extra[40:], attr)
			return w(fuseOutHeader{Len: fuseHdrOutSize + uint32(len(extra)), Error: 0, Unique: in.Unique}, extra), nil
		}

	case FUSE_CREATE:
		debug.Writef("virtio-fs.dispatchFUSE op=CREATE", "parent=%d", in.NodeID)
		if len(req) < fuseHdrInSize+16 {
			debug.Writef("virtio-fs.dispatchFUSE op=CREATE too short", "in=%+v", in)
			return 0, fmt.Errorf("FUSE_CREATE too short")
		}
		flags := binary.LittleEndian.Uint32(req[40:44])
		mode := binary.LittleEndian.Uint32(req[44:48])
		umask := binary.LittleEndian.Uint32(req[48:52])
		name := readName(req[fuseHdrInSize+16:])
		debug.Writef("virtio-fs.dispatchFUSE op=CREATE", "name=%q mode=0%o flags=0x%x umask=0%o", name, mode, flags, umask)
		if be, ok := v.backend.(fsCreateBackend); ok {
			nodeID, fh, attr, e := be.Create(in.NodeID, name, mode, flags, umask, in.UID, in.GID)
			errno = e
			if errno == 0 {
				extra := make([]byte, 40+88+16)
				binary.LittleEndian.PutUint64(extra[0:8], nodeID)
				binary.LittleEndian.PutUint64(extra[16:24], 1)
				binary.LittleEndian.PutUint64(extra[24:32], 1)
				encodeFuseAttr(extra[40:], attr)
				binary.LittleEndian.PutUint64(extra[40+88:40+96], fh)
				return w(fuseOutHeader{Len: fuseHdrOutSize + uint32(len(extra)), Error: 0, Unique: in.Unique}, extra), nil
			}
		} else {
			errno = -int32(linux.ENOSYS)
		}

	case FUSE_MKNOD:
		debug.Writef("virtio-fs.dispatchFUSE op=MKNOD", "parent=%d", in.NodeID)
		if len(req) < fuseHdrInSize+16 {
			return 0, fmt.Errorf("FUSE_MKNOD too short")
		}
		mode := binary.LittleEndian.Uint32(req[40:44])
		rdev := binary.LittleEndian.Uint32(req[44:48])
		umask := binary.LittleEndian.Uint32(req[48:52])
		name := readName(req[fuseHdrInSize+16:])
		debug.Writef("virtio-fs.dispatchFUSE op=MKNOD", "name=%q mode=0%o rdev=0x%x umask=0%o", name, mode, rdev, umask)
		if be, ok := v.backend.(fsMknodBackend); ok {
			nodeID, attr, e := be.Mknod(in.NodeID, name, mode, rdev, umask, in.UID, in.GID)
			errno = e
			if errno == 0 {
				extra := make([]byte, 40+88)
				binary.LittleEndian.PutUint64(extra[0:8], nodeID)
				binary.LittleEndian.PutUint64(extra[16:24], 1)
				binary.LittleEndian.PutUint64(extra[24:32], 1)
				encodeFuseAttr(extra[40:], attr)
				return w(fuseOutHeader{Len: fuseHdrOutSize + uint32(len(extra)), Error: 0, Unique: in.Unique}, extra), nil
			}
		} else {
			errno = -int32(linux.ENOSYS)
		}

	case FUSE_MKDIR:
		debug.Writef("virtio-fs.dispatchFUSE op=MKDIR", "parent=%d", in.NodeID)
		if len(req) < fuseHdrInSize+8 {
			return 0, fmt.Errorf("FUSE_MKDIR too short")
		}
		mode := binary.LittleEndian.Uint32(req[40:44])
		umask := binary.LittleEndian.Uint32(req[44:48])
		name := readName(req[fuseHdrInSize+8:])
		debug.Writef("virtio-fs.dispatchFUSE op=MKDIR", "name=%q mode=0%o umask=0%o", name, mode, umask)
		if be, ok := v.backend.(fsMkdirBackend); ok {
			nodeID, attr, e := be.Mkdir(in.NodeID, name, mode, umask, in.UID, in.GID)
			errno = e
			if errno == 0 {
				extra := make([]byte, 40+88)
				binary.LittleEndian.PutUint64(extra[0:8], nodeID)
				binary.LittleEndian.PutUint64(extra[16:24], 1)
				binary.LittleEndian.PutUint64(extra[24:32], 1)
				encodeFuseAttr(extra[40:], attr)
				return w(fuseOutHeader{Len: fuseHdrOutSize + uint32(len(extra)), Error: 0, Unique: in.Unique}, extra), nil
			}
		} else {
			errno = -int32(linux.ENOSYS)
		}

	case FUSE_OPEN:
		debug.Writef("virtio-fs.dispatchFUSE op=OPEN", "node=%d", in.NodeID)
		if len(req) < fuseHdrInSize+8 {
			return 0, fmt.Errorf("FUSE_OPEN too short")
		}
		flags := binary.LittleEndian.Uint32(req[40:44])
		debug.Writef("virtio-fs.dispatchFUSE op=OPEN", "flags=0x%x", flags)
		fh, e := v.backend.Open(in.NodeID, flags)
		errno = e
		if errno == 0 {
			extra := make([]byte, 16)
			binary.LittleEndian.PutUint64(extra[0:8], fh)
			// open_flags=0; padding
			return w(fuseOutHeader{Len: fuseHdrOutSize + uint32(len(extra)), Error: 0, Unique: in.Unique}, extra), nil
		}

	case FUSE_OPENDIR:
		debug.Writef("virtio-fs.dispatchFUSE op=OPENDIR", "node=%d", in.NodeID)
		if len(req) < fuseHdrInSize+8 {
			return 0, fmt.Errorf("FUSE_OPENDIR too short")
		}
		flags := binary.LittleEndian.Uint32(req[40:44])
		debug.Writef("virtio-fs.dispatchFUSE op=OPENDIR", "flags=0x%x", flags)
		if be, ok := v.backend.(fsOpenDirBackend); ok {
			fh, e := be.OpenDir(in.NodeID, flags)
			errno = e
			if errno == 0 {
				// fuse_open_out (same layout as OPEN).
				extra := make([]byte, 16)
				binary.LittleEndian.PutUint64(extra[0:8], fh)
				return w(fuseOutHeader{Len: fuseHdrOutSize + uint32(len(extra)), Error: 0, Unique: in.Unique}, extra), nil
			}
		} else {
			errno = -int32(linux.ENOSYS)
		}

	case FUSE_RELEASE:
		debug.Writef("virtio-fs.dispatchFUSE op=RELEASE", "node=%d", in.NodeID)
		if len(req) < fuseHdrInSize+24 {
			return 0, fmt.Errorf("FUSE_RELEASE too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		debug.Writef("virtio-fs.dispatchFUSE op=RELEASE", "fh=%d", fh)
		v.backend.Release(in.NodeID, fh)
		return w(fuseOutHeader{Len: fuseHdrOutSize, Error: 0, Unique: in.Unique}, nil), nil

	case FUSE_RELEASEDIR:
		debug.Writef("virtio-fs.dispatchFUSE op=RELEASEDIR", "node=%d", in.NodeID)
		if len(req) < fuseHdrInSize+24 {
			return 0, fmt.Errorf("FUSE_RELEASEDIR too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		debug.Writef("virtio-fs.dispatchFUSE op=RELEASEDIR", "fh=%d", fh)
		if be, ok := v.backend.(fsOpenDirBackend); ok {
			be.ReleaseDir(in.NodeID, fh)
			return w(fuseOutHeader{Len: fuseHdrOutSize, Error: 0, Unique: in.Unique}, nil), nil
		}
		errno = -int32(linux.ENOSYS)

	case FUSE_READ:
		debug.Writef("virtio-fs.dispatchFUSE op=READ", "node=%d", in.NodeID)
		if len(req) < fuseHdrInSize+24 {
			return 0, fmt.Errorf("FUSE_READ too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		off := binary.LittleEndian.Uint64(req[48:56])
		size := binary.LittleEndian.Uint32(req[56:60])
		debug.Writef("virtio-fs.dispatchFUSE op=READ", "fh=%d off=%d size=%d", fh, off, size)
		data, e := v.backend.Read(in.NodeID, fh, off, size)
		errno = e
		if errno == 0 {
			outLen := fuseHdrOutSize + uint32(len(data))
			if int(outLen) > len(resp) {
				data = data[:len(resp)-fuseHdrOutSize]
				outLen = uint32(len(resp))
			}
			// Zero the entire response buffer area before copying to avoid garbage data
			// Clear from fuseHdrOutSize to the end of resp to ensure no garbage
			clear(resp[fuseHdrOutSize:])
			copy(resp[fuseHdrOutSize:], data)
			return w(fuseOutHeader{Len: outLen, Error: 0, Unique: in.Unique}, nil), nil
		}

	case FUSE_WRITE:
		debug.Writef("virtio-fs.dispatchFUSE op=WRITE", "node=%d", in.NodeID)
		if len(req) < fuseHdrInSize+32 {
			return 0, fmt.Errorf("FUSE_WRITE too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		off := binary.LittleEndian.Uint64(req[48:56])
		size := binary.LittleEndian.Uint32(req[56:60])
		debug.Writef("virtio-fs.dispatchFUSE op=WRITE", "fh=%d off=%d size=%d", fh, off, size)
		// Calculate where write data actually starts: header (40) + write_in structure
		// The write_in structure is: fh (8) + off (8) + size (4) + write_flags (4) + lock_owner (8) = 32 bytes
		// But the guest may send it with padding, so we need to find where the data actually starts
		// by looking at the total request length: data starts at (reqLen - size)
		writeDataStart := len(req) - int(size)
		if writeDataStart < fuseHdrInSize+32 {
			// Fallback to expected offset if calculation is wrong
			writeDataStart = fuseHdrInSize + 32
		}
		if len(req) < writeDataStart+int(size) {
			return 0, fmt.Errorf("FUSE_WRITE payload too short")
		}
		data := req[writeDataStart : writeDataStart+int(size)]
		if be, ok := v.backend.(fsWriteBackend); ok {
			written, e := be.Write(in.NodeID, fh, off, data)
			errno = e
			if errno == 0 {
				debug.Writef("virtio-fs.dispatchFUSE op=WRITE", "written=%d", written)
				extra := make([]byte, 8)
				binary.LittleEndian.PutUint32(extra[0:4], written)
				return w(fuseOutHeader{Len: fuseHdrOutSize + uint32(len(extra)), Error: 0, Unique: in.Unique}, extra), nil
			}
		} else {
			errno = -int32(linux.ENOSYS)
		}

	case FUSE_READDIR:
		debug.Writef("virtio-fs.dispatchFUSE op=READDIR", "node=%d", in.NodeID)
		if len(req) < fuseHdrInSize+24 {
			return 0, fmt.Errorf("FUSE_READDIR too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		off := binary.LittleEndian.Uint64(req[48:56])
		size := binary.LittleEndian.Uint32(req[56:60])
		debug.Writef("virtio-fs.dispatchFUSE op=READDIR", "fh=%d off=%d size=%d", fh, off, size)
		var payload []byte
		var e int32
		if be, ok := v.backend.(fsReadDirHandleBackend); ok && fh != 0 {
			payload, e = be.ReadDirHandle(in.NodeID, fh, off, size)
		} else {
			payload, e = v.backend.ReadDir(in.NodeID, off, size)
		}
		errno = e
		if errno == 0 {
			outLen := fuseHdrOutSize + uint32(len(payload))
			if int(outLen) > len(resp) {
				payload = payload[:len(resp)-fuseHdrOutSize]
				outLen = uint32(len(resp))
			}
			copy(resp[fuseHdrOutSize:], payload)
			return w(fuseOutHeader{Len: outLen, Error: 0, Unique: in.Unique}, nil), nil
		}

	case FUSE_RENAME:
		debug.Writef("virtio-fs.dispatchFUSE op=RENAME", "oldParent=%d", in.NodeID)
		if len(req) < fuseHdrInSize+8 {
			return 0, fmt.Errorf("FUSE_RENAME too short")
		}
		newParent := binary.LittleEndian.Uint64(req[40:48])
		nameStart := fuseHdrInSize + 8
		flags := uint32(0)
		if len(req) >= fuseHdrInSize+16 {
			flags = binary.LittleEndian.Uint32(req[48:52])
		}
		oldName, rest := readCString(req[nameStart:])
		if rest == nil {
			return 0, fmt.Errorf("FUSE_RENAME missing new name")
		}
		newName := readName(rest)
		debug.Writef("virtio-fs.dispatchFUSE op=RENAME", "oldName=%q newParent=%d newName=%q flags=0x%x", oldName, newParent, newName, flags)
		if be, ok := v.backend.(fsRenameBackend); ok {
			errno = be.Rename(in.NodeID, oldName, newParent, newName, flags)
			if errno == 0 {
				return w(fuseOutHeader{Len: fuseHdrOutSize, Error: 0, Unique: in.Unique}, nil), nil
			}
		} else {
			errno = -int32(linux.ENOSYS)
		}

	case FUSE_UNLINK:
		name := readName(req[fuseHdrInSize:])
		debug.Writef("virtio-fs.dispatchFUSE op=UNLINK", "parent=%d name=%q", in.NodeID, name)
		if be, ok := v.backend.(fsRemoveBackend); ok {
			errno = be.Unlink(in.NodeID, name)
			if errno == 0 {
				return w(fuseOutHeader{Len: fuseHdrOutSize, Error: 0, Unique: in.Unique}, nil), nil
			}
		} else {
			errno = -int32(linux.ENOSYS)
		}

	case FUSE_RMDIR:
		name := readName(req[fuseHdrInSize:])
		debug.Writef("virtio-fs.dispatchFUSE op=RMDIR", "parent=%d name=%q", in.NodeID, name)
		if be, ok := v.backend.(fsRemoveBackend); ok {
			errno = be.Rmdir(in.NodeID, name)
			if errno == 0 {
				return w(fuseOutHeader{Len: fuseHdrOutSize, Error: 0, Unique: in.Unique}, nil), nil
			}
		} else {
			errno = -int32(linux.ENOSYS)
		}

	case FUSE_SETXATTR:
		debug.Writef("virtio-fs.dispatchFUSE op=SETXATTR", "node=%d", in.NodeID)
		if len(req) < fuseHdrInSize+8 {
			return 0, fmt.Errorf("FUSE_SETXATTR too short")
		}
		size := binary.LittleEndian.Uint32(req[40:44])
		flags := binary.LittleEndian.Uint32(req[44:48])
		name, value := readCString(req[fuseHdrInSize+8:])
		if value == nil {
			return 0, fmt.Errorf("FUSE_SETXATTR missing value")
		}
		debug.Writef("virtio-fs.dispatchFUSE op=SETXATTR", "name=%q size=%d flags=0x%x", name, size, flags)
		if uint32(len(value)) < size {
			return 0, fmt.Errorf("FUSE_SETXATTR value short")
		}
		if be, ok := v.backend.(fsXattrBackend); ok {
			errno = be.SetXattr(in.NodeID, name, value[:size], flags)
			if errno == 0 {
				return w(fuseOutHeader{Len: fuseHdrOutSize, Error: 0, Unique: in.Unique}, nil), nil
			}
		} else {
			errno = -int32(linux.ENOSYS)
		}

	case FUSE_GETXATTR:
		debug.Writef("virtio-fs.dispatchFUSE op=GETXATTR", "node=%d", in.NodeID)
		if len(req) < fuseHdrInSize+8 {
			return 0, fmt.Errorf("FUSE_GETXATTR too short")
		}
		size := binary.LittleEndian.Uint32(req[40:44])
		name := readName(req[fuseHdrInSize+8:])
		debug.Writef("virtio-fs.dispatchFUSE op=GETXATTR", "name=%q size=%d", name, size)
		if be, ok := v.backend.(fsXattrBackend); ok {
			value, e := be.GetXattr(in.NodeID, name)
			errno = e
			if errno == 0 {
				if size == 0 {
					extra := make([]byte, 8)
					binary.LittleEndian.PutUint32(extra[0:4], uint32(len(value)))
					return w(fuseOutHeader{Len: fuseHdrOutSize + uint32(len(extra)), Error: 0, Unique: in.Unique}, extra), nil
				}
				if uint32(len(value)) > size {
					// Correct semantics: buffer too small -> ERANGE (do not truncate).
					errno = -int32(linux.ERANGE)
					break
				}
				outLen := fuseHdrOutSize + uint32(len(value))
				if int(outLen) > len(resp) {
					value = value[:len(resp)-fuseHdrOutSize]
					outLen = uint32(len(resp))
				}
				copy(resp[fuseHdrOutSize:], value)
				return w(fuseOutHeader{Len: outLen, Error: 0, Unique: in.Unique}, nil), nil
			}
		} else {
			errno = -int32(linux.ENOSYS)
		}

	case FUSE_LISTXATTR:
		debug.Writef("virtio-fs.dispatchFUSE op=LISTXATTR", "node=%d", in.NodeID)
		if len(req) < fuseHdrInSize+8 {
			return 0, fmt.Errorf("FUSE_LISTXATTR too short")
		}
		// fuse_getxattr_in: size (u32) + padding (u32)
		size := binary.LittleEndian.Uint32(req[40:44])
		debug.Writef("virtio-fs.dispatchFUSE op=LISTXATTR", "size=%d", size)
		if be, ok := v.backend.(fsXattrBackend); ok {
			list, e := be.ListXattr(in.NodeID)
			errno = e
			if errno == 0 {
				if size == 0 {
					extra := make([]byte, 8)
					binary.LittleEndian.PutUint32(extra[0:4], uint32(len(list)))
					return w(fuseOutHeader{Len: fuseHdrOutSize + uint32(len(extra)), Error: 0, Unique: in.Unique}, extra), nil
				}
				if uint32(len(list)) > size {
					// Correct semantics: buffer too small -> ERANGE (do not truncate).
					errno = -int32(linux.ERANGE)
					break
				}
				outLen := fuseHdrOutSize + uint32(len(list))
				if int(outLen) > len(resp) {
					list = list[:len(resp)-fuseHdrOutSize]
					outLen = uint32(len(resp))
				}
				copy(resp[fuseHdrOutSize:], list)
				return w(fuseOutHeader{Len: outLen, Error: 0, Unique: in.Unique}, nil), nil
			}
		} else {
			errno = -int32(linux.ENOSYS)
		}

	case FUSE_REMOVEXATTR:
		debug.Writef("virtio-fs.dispatchFUSE op=REMOVEXATTR", "node=%d", in.NodeID)
		name := readName(req[fuseHdrInSize:])
		debug.Writef("virtio-fs.dispatchFUSE op=REMOVEXATTR", "name=%q", name)
		if be, ok := v.backend.(fsXattrBackend); ok {
			errno = be.RemoveXattr(in.NodeID, name)
			if errno == 0 {
				return w(fuseOutHeader{Len: fuseHdrOutSize, Error: 0, Unique: in.Unique}, nil), nil
			}
		} else {
			errno = -int32(linux.ENOSYS)
		}

	case FUSE_READLINK:
		debug.Writef("virtio-fs.dispatchFUSE op=READLINK", "node=%d", in.NodeID)
		if be, ok := v.backend.(fsReadlinkBackend); ok {
			target, e := be.Readlink(in.NodeID)
			errno = e
			if errno == 0 {
				data := []byte(target)
				outLen := fuseHdrOutSize + uint32(len(data))
				if int(outLen) > len(resp) {
					data = data[:len(resp)-fuseHdrOutSize]
					outLen = uint32(len(resp))
				}
				clear(resp[fuseHdrOutSize:])
				copy(resp[fuseHdrOutSize:], data)
				return w(fuseOutHeader{Len: outLen, Error: 0, Unique: in.Unique}, nil), nil
			}
		} else {
			errno = -int32(linux.ENOSYS)
		}

	case FUSE_SYMLINK:
		// On-wire (Linux FUSE): `name\0target\0` (two NUL-terminated strings).
		// Note: some protocol versions may include additional fields, but Alpine's apk uses the plain layout.
		umask := uint32(0)
		name, rest := readCString(req[fuseHdrInSize:])
		target := ""
		if rest != nil {
			target = readName(rest)
		}
		debug.Writef("virtio-fs.dispatchFUSE op=SYMLINK", "parent=%d name=%q target=%q umask=0%o", in.NodeID, name, target, umask)
		if be, ok := v.backend.(fsSymlinkBackend); ok {
			nodeID, attr, e := be.Symlink(in.NodeID, name, target, umask, in.UID, in.GID)
			errno = e
			if errno == 0 {
				// fuse_entry_out
				extra := make([]byte, 40+88)
				binary.LittleEndian.PutUint64(extra[0:8], nodeID)
				binary.LittleEndian.PutUint64(extra[16:24], 1) // entry_valid
				binary.LittleEndian.PutUint64(extra[24:32], 1) // attr_valid
				encodeFuseAttr(extra[40:], attr)
				return w(fuseOutHeader{Len: fuseHdrOutSize + uint32(len(extra)), Error: 0, Unique: in.Unique}, extra), nil
			}
		} else {
			errno = -int32(linux.ENOSYS)
		}

	case FUSE_LINK:
		// fuse_link_in: oldnodeid (uint64) followed by NUL-terminated newname
		if len(req) < fuseHdrInSize+8 {
			return 0, fmt.Errorf("FUSE_LINK too short")
		}
		oldNodeID := binary.LittleEndian.Uint64(req[40:48])
		newName := readName(req[fuseHdrInSize+8:])
		debug.Writef("virtio-fs.dispatchFUSE op=LINK", "newParent=%d oldNode=%d newName=%q", in.NodeID, oldNodeID, newName)
		if be, ok := v.backend.(fsLinkBackend); ok {
			nodeID, attr, e := be.Link(oldNodeID, in.NodeID, newName)
			errno = e
			if errno == 0 {
				// fuse_entry_out
				extra := make([]byte, 40+88)
				binary.LittleEndian.PutUint64(extra[0:8], nodeID)
				binary.LittleEndian.PutUint64(extra[16:24], 1) // entry_valid
				binary.LittleEndian.PutUint64(extra[24:32], 1) // attr_valid
				encodeFuseAttr(extra[40:], attr)
				return w(fuseOutHeader{Len: fuseHdrOutSize + uint32(len(extra)), Error: 0, Unique: in.Unique}, extra), nil
			}
		} else {
			errno = -int32(linux.ENOSYS)
		}

	case FUSE_SETATTR:
		debug.Writef("virtio-fs.dispatchFUSE op=SETATTR", "node=%d", in.NodeID)
		if len(req) < fuseHdrInSize+56 {
			return 0, fmt.Errorf("FUSE_SETATTR too short")
		}
		valid := binary.LittleEndian.Uint32(req[40:44])
		// FUSE attribute valid flags
		const fattrMode = 1 << 0
		const fattrUid = 1 << 1
		const fattrGid = 1 << 2
		const fattrSize = 1 << 3
		var sizeVal *uint64
		if valid&fattrSize != 0 {
			val := binary.LittleEndian.Uint64(req[56:64])
			sizeVal = &val
		}
		var modeVal *uint32
		if valid&fattrMode != 0 {
			// According to FUSE protocol, fuse_setattr_in structure:
			// offset 40: valid (uint32)
			// offset 44: padding (uint32)
			// offset 48: fh (uint64)
			// offset 56: size (uint64)
			// offset 64: lock_owner (uint64)
			// offset 72: atime (uint64)
			// offset 80: mtime (uint64)
			// offset 88: ctime (uint64)
			// offset 96: atimensec (uint32)
			// offset 100: mtimensec (uint32)
			// offset 104: ctimensec (uint32)
			// offset 108: mode (uint32)
			// offset 112: unused4 (uint32)
			// offset 116: uid (uint32)
			// offset 120: gid (uint32)
			// Mode is at offset 108 (4 bytes), so we need at least 112 bytes total
			if len(req) >= 112 {
				val := binary.LittleEndian.Uint32(req[108:112])
				modeVal = &val
			}
		}
		var uidVal *uint32
		if valid&fattrUid != 0 {
			// uid is at offset 116
			if len(req) >= 120 {
				val := binary.LittleEndian.Uint32(req[116:120])
				uidVal = &val
			}
		}
		var gidVal *uint32
		if valid&fattrGid != 0 {
			// gid is at offset 120
			if len(req) >= 124 {
				val := binary.LittleEndian.Uint32(req[120:124])
				gidVal = &val
			}
		}
		if be, ok := v.backend.(fsSetattrBackend); ok {
			errno = be.SetAttr(in.NodeID, sizeVal, modeVal, uidVal, gidVal)
			if errno == 0 {
				debug.Writef("virtio-fs.dispatchFUSE op=SETATTR applied", "size=%v mode=%v uid=%v gid=%v", sizeVal, modeVal, uidVal, gidVal)
				attr, e := v.backend.GetAttr(in.NodeID)
				errno = e
				if errno == 0 {
					extra := make([]byte, 16+88)
					binary.LittleEndian.PutUint64(extra[0:8], 1)
					binary.LittleEndian.PutUint32(extra[8:12], 0)
					binary.LittleEndian.PutUint32(extra[12:16], 0)
					encodeFuseAttr(extra[16:], attr)
					return w(fuseOutHeader{Len: fuseHdrOutSize + uint32(len(extra)), Error: 0, Unique: in.Unique}, extra), nil
				}
			}
		} else {
			errno = -int32(linux.ENOSYS)
		}

	case FUSE_LSEEK:
		debug.Writef("virtio-fs.dispatchFUSE op=LSEEK", "node=%d", in.NodeID)
		if len(req) < fuseHdrInSize+24 {
			return 0, fmt.Errorf("FUSE_LSEEK too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		offset := binary.LittleEndian.Uint64(req[48:56])
		whence := binary.LittleEndian.Uint32(req[56:60])
		debug.Writef("virtio-fs.dispatchFUSE op=LSEEK", "fh=%d offset=%d whence=%d", fh, offset, whence)
		if be, ok := v.backend.(fsLseekBackend); ok {
			newOff, e := be.Lseek(in.NodeID, fh, offset, whence)
			errno = e
			if errno == 0 {
				debug.Writef("virtio-fs.dispatchFUSE op=LSEEK", "newOff=%d", newOff)
				extra := make([]byte, 8)
				binary.LittleEndian.PutUint64(extra[0:8], newOff)
				return w(fuseOutHeader{Len: fuseHdrOutSize + uint32(len(extra)), Error: 0, Unique: in.Unique}, extra), nil
			}
		} else {
			errno = -int32(linux.ENOSYS)
		}

	case FUSE_STATFS:
		debug.Writef("virtio-fs.dispatchFUSE op=STATFS", "node=%d", in.NodeID)
		b, bf, ba, files, ff, bsize, fr, name, e := v.backend.StatFS(in.NodeID)
		errno = e
		if errno == 0 {
			extra := make([]byte, 56)
			putU64 := func(off int, val uint64) { binary.LittleEndian.PutUint64(extra[off:off+8], val) }
			putU32 := func(off int, val uint32) { binary.LittleEndian.PutUint32(extra[off:off+4], val) }
			putU64(0, b)
			putU64(8, bf)
			putU64(16, ba)
			putU64(24, files)
			putU64(32, ff)
			// Layout matches struct fuse_kstatfs through padding; spare slots stay zeroed.
			putU32(40, uint32(bsize))
			putU32(44, uint32(name))
			putU32(48, uint32(fr))
			return w(fuseOutHeader{Len: fuseHdrOutSize + uint32(len(extra)), Error: 0, Unique: in.Unique}, extra), nil
		}
	default:
		slog.Debug("virtio-fs.dispatchFUSE unsupported", "opcode", fuseOpcodeString(in.Opcode))
		debug.Writef("virtio-fs.dispatchFUSE unsupported", "opcode=%s", fuseOpcodeString(in.Opcode))
		errno = -int32(linux.ENOSYS)
	}

	debug.Writef("virtio-fs.dispatchFUSE done", "opcode=%s unique=%d errno=%d", fuseOpcodeString(in.Opcode), in.Unique, errno)
	return w(fuseOutHeader{Len: fuseHdrOutSize, Error: errno, Unique: in.Unique}, nil), nil
}

func fuseOpcodeString(op uint32) string {
	switch op {
	case FUSE_LOOKUP:
		return "LOOKUP"
	case FUSE_FORGET:
		return "FORGET"
	case FUSE_GETATTR:
		return "GETATTR"
	case FUSE_SETATTR:
		return "SETATTR"
	case FUSE_READLINK:
		return "READLINK"
	case FUSE_SYMLINK:
		return "SYMLINK"
	case FUSE_MKNOD:
		return "MKNOD"
	case FUSE_MKDIR:
		return "MKDIR"
	case FUSE_UNLINK:
		return "UNLINK"
	case FUSE_RMDIR:
		return "RMDIR"
	case FUSE_RENAME:
		return "RENAME"
	case FUSE_LINK:
		return "LINK"
	case FUSE_OPEN:
		return "OPEN"
	case FUSE_READ:
		return "READ"
	case FUSE_WRITE:
		return "WRITE"
	case FUSE_STATFS:
		return "STATFS"
	case FUSE_RELEASE:
		return "RELEASE"
	case FUSE_FSYNC:
		return "FSYNC"
	case FUSE_FLUSH:
		return "FLUSH"
	case FUSE_INIT:
		return "INIT"
	case FUSE_OPENDIR:
		return "OPENDIR"
	case FUSE_READDIR:
		return "READDIR"
	case FUSE_RELEASEDIR:
		return "RELEASEDIR"
	case FUSE_FSYNCDIR:
		return "FSYNCDIR"
	case FUSE_CREATE:
		return "CREATE"
	case FUSE_RENAME2:
		return "RENAME2"
	case FUSE_LSEEK:
		return "LSEEK"
	default:
		return fmt.Sprintf("OP(%d)", op)
	}
}

func indexNull(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			return i
		}
	}
	return -1
}

func indexNullBytes(b []byte) int {
	for i := 0; i < len(b); i++ {
		if b[i] == 0 {
			return i
		}
	}
	return -1
}

func readName(b []byte) string {
	if idx := indexNullBytes(b); idx >= 0 {
		return string(b[:idx])
	}
	return string(b)
}

func readCString(b []byte) (string, []byte) {
	if idx := indexNullBytes(b); idx >= 0 {
		return string(b[:idx]), b[idx+1:]
	}
	return string(b), nil
}

// Buffer helpers
func (v *FS) getBuffer(n int) []byte {
	raw := v.bufPool.Get()
	if raw == nil {
		return make([]byte, n)
	}
	b := raw.([]byte)
	if cap(b) < n {
		v.bufPool.Put(b[:0])
		return make([]byte, n)
	}
	return b[:n]
}
func (v *FS) putBuffer(b []byte) {
	if b == nil {
		return
	}
	full := b[:cap(b)]
	clear(full)
	v.bufPool.Put(full[:0])
}

func (v *FS) DeviceId() string { return "virtio-fs" }

func (v *FS) CaptureSnapshot() (hv.DeviceSnapshot, error) {
	snap := &fsSnapshot{
		Arch:    v.arch,
		Base:    v.base,
		Size:    v.size,
		IRQLine: v.irqLine,
		Tag:     v.tag,
	}
	return snap, nil
}

func (v *FS) RestoreSnapshot(snap hv.DeviceSnapshot) error {
	data, ok := snap.(*fsSnapshot)
	if !ok {
		return fmt.Errorf("virtio-fs: invalid snapshot type")
	}
	v.arch = data.Arch
	v.base = data.Base
	v.size = data.Size
	v.irqLine = data.IRQLine
	v.tag = data.Tag
	if mmio, ok := v.device.(*mmioDevice); ok {
		mmio.base = v.base
		mmio.size = v.size
	}
	return nil
}

type fsSnapshot struct {
	Arch    hv.CpuArchitecture
	Base    uint64
	Size    uint64
	IRQLine uint32
	Tag     [fsCfgTagSize]byte
}

var (
	_ hv.MemoryMappedIODevice = (*FS)(nil)
	_ hv.DeviceSnapshotter    = (*FS)(nil)
	_ deviceHandler           = (*FS)(nil)
)
