package virtio

import (
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"j5.nz/cc/internal/fdt"
	"j5.nz/cc/internal/fsmeta"
)

const (
	mmioDeviceIDFS = 26

	fsQueueHiprio  = 0
	fsQueueRequest = 1

	fsCfgTagSize       = 36
	fsCfgNumQueueOff   = fsCfgTagSize
	fsCfgTotalSize     = fsCfgTagSize + 4
	fsInterruptVring   = 0x1
	fuseInHeaderSize   = 40
	fuseOutHeaderSize  = 16
	fuseAttrSize       = 88
	fuseEntryOutSize   = 40 + fuseAttrSize
	fuseAttrOutSize    = 16 + fuseAttrSize
	fuseOpenOutSize    = 16
	fuseInitOutSize    = 40
	fuseStatfsOutSize  = 80
	fuseDirentBaseSize = 24
)

const (
	fuseLookup     = 1
	fuseForget     = 2
	fuseGetAttr    = 3
	fuseReadlink   = 5
	fuseMkdir      = 9
	fuseRmDir      = 11
	fuseOpen       = 14
	fuseRead       = 15
	fuseStatfs     = 17
	fuseRelease    = 18
	fuseGetXattr   = 22
	fuseListXattr  = 23
	fuseFlush      = 25
	fuseInit       = 26
	fuseOpenDir    = 27
	fuseReadDir    = 28
	fuseReleaseDir = 29
	fuseAccess     = 34
	fuseDestroy    = 38
	fusePoll       = 40
	fuseLseek      = 46
	fuseSyncFS     = 50
)

const (
	fuseCapPosixLocks = 1 << 1
	fuseCapPosixACL   = 1 << 20
)

const (
	fuseOpenKeepCache = 1 << 1
	fuseOpenCacheDir  = 1 << 3
)

const (
	dirTypeUnknown = 0
	dirTypeFIFO    = 1
	dirTypeChar    = 2
	dirTypeDir     = 4
	dirTypeBlock   = 6
	dirTypeFile    = 8
	dirTypeLink    = 10
	dirTypeSocket  = 12
)

type FSBackend interface {
	Init() (maxWrite uint32, flags uint32)
	GetAttr(nodeID uint64) (FuseAttr, int32)
	Lookup(parent uint64, name string) (nodeID uint64, attr FuseAttr, errno int32)
	Open(nodeID uint64, flags uint32) (fh uint64, errno int32)
	Release(nodeID uint64, fh uint64)
	Read(nodeID uint64, fh uint64, off uint64, size uint32) ([]byte, int32)
	OpenDir(nodeID uint64, flags uint32) (fh uint64, errno int32)
	ReadDir(nodeID uint64, fh uint64, off uint64, maxBytes uint32) ([]byte, int32)
	ReleaseDir(nodeID uint64, fh uint64)
	Readlink(nodeID uint64) (string, int32)
	StatFS(nodeID uint64) (blocks, bfree, bavail, files, ffree, bsize, frsize, namelen uint64, errno int32)
}

type fsXattrBackend interface {
	GetXattr(nodeID uint64, name string) ([]byte, int32)
	ListXattr(nodeID uint64) ([]byte, int32)
}

type fsFlushBackend interface {
	Flush(nodeID uint64, fh uint64, lockOwner uint64) int32
}

type fsLseekBackend interface {
	Lseek(nodeID uint64, fh uint64, offset uint64, whence uint32) (uint64, int32)
}

type fsMkdirBackend interface {
	Mkdir(parent uint64, name string, mode uint32) (nodeID uint64, attr FuseAttr, errno int32)
}

type fsRmDirBackend interface {
	RmDir(parent uint64, name string) int32
}

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

type FS struct {
	Base   uint64
	Size   uint64
	IRQ    uint32
	Log    io.Writer
	Strict bool

	mu               sync.Mutex
	mem              GuestMemory
	irq              IRQController
	backend          FSBackend
	tag              [fsCfgTagSize]byte
	deviceFeatureSel uint32
	driverFeatureSel uint32
	driverFeatures   uint64
	queueSel         uint32
	status           uint32
	interruptStatus  uint32
	irqHigh          bool
	configGeneration uint32
	queues           [2]queue
}

type fsDesc struct {
	addr   uint64
	length uint32
	flags  uint16
	next   uint16
	write  bool
}

func NewFS(base, size uint64, irq uint32, tag string, backend FSBackend) *FS {
	fs := &FS{
		Base:    base,
		Size:    size,
		IRQ:     irq,
		backend: backend,
	}
	if fs.backend == nil {
		fs.backend = NewPassthroughFS("", nil)
	}
	copy(fs.tag[:], []byte(tag))
	fs.resetLocked()
	return fs
}

func (f *FS) Attach(mem GuestMemory, irq IRQController) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mem = mem
	f.irq = irq
}

func (f *FS) Contains(addr uint64, size int) bool {
	return addr >= f.Base && addr+uint64(size) <= f.Base+f.Size
}

func (f *FS) DeviceTreeNode() fdt.Node {
	return fdt.Node{
		Name: fmt.Sprintf("virtio@%x", f.Base),
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"virtio,mmio"}},
			"reg":        {U64: []uint64{f.Base, f.Size}},
			"interrupts": {U32: []uint32{0, f.IRQ, 4}},
			"status":     {Strings: []string{"okay"}},
		},
	}
}

func (f *FS) Read(addr uint64, size int) (uint64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	offset := addr - f.Base
	switch offset {
	case regMagicValue:
		return truncateValue(mmioMagicValue, size), nil
	case regVersion:
		return truncateValue(mmioVersion, size), nil
	case regDeviceID:
		return truncateValue(mmioDeviceIDFS, size), nil
	case regVendorID:
		return truncateValue(mmioVendorID, size), nil
	case regDeviceFeatures:
		if f.deviceFeatureSel == 0 {
			return truncateValue(0, size), nil
		}
		if f.deviceFeatureSel == 1 {
			return truncateValue(1, size), nil
		}
		return 0, nil
	case regQueueNumMax:
		if f.queueSel < uint32(len(f.queues)) {
			return truncateValue(128, size), nil
		}
		return 0, nil
	case regQueueNum:
		if q := f.selectedQueueLocked(); q != nil {
			return truncateValue(uint64(q.size), size), nil
		}
		return 0, nil
	case regQueueReady:
		if q := f.selectedQueueLocked(); q != nil && q.ready {
			return truncateValue(1, size), nil
		}
		return 0, nil
	case regInterruptStatus:
		f.logf("mmio-read interrupt-status size=%d value=%#x", size, f.interruptStatus)
		return truncateValue(uint64(f.interruptStatus), size), nil
	case regStatus:
		f.logf("mmio-read status size=%d value=%#x", size, f.status)
		return truncateValue(uint64(f.status), size), nil
	case regConfigGen:
		return truncateValue(uint64(f.configGeneration), size), nil
	}
	if offset >= regConfig && offset+uint64(size) <= regConfig+fsCfgTotalSize {
		cfg := f.configBytesLocked()
		return truncateValue(readConfigValue(cfg[offset-regConfig:], size), size), nil
	}
	return 0, nil
}

func (f *FS) Write(addr uint64, size int, value uint64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	offset := addr - f.Base
	switch offset {
	case regQueueSel, regQueueNum, regQueueReady, regQueueNotify, regInterruptAck, regStatus:
		f.logf("mmio-write off=%#x size=%d value=%#x", offset, size, value)
	}
	switch offset {
	case regDeviceFeatSel:
		f.deviceFeatureSel = uint32(value)
	case regDriverFeatSel:
		f.driverFeatureSel = uint32(value)
	case regDriverFeatures:
		if f.driverFeatureSel == 0 {
			f.driverFeatures = (f.driverFeatures &^ 0xffffffff) | uint64(uint32(value))
		} else if f.driverFeatureSel == 1 {
			f.driverFeatures = (f.driverFeatures & 0xffffffff) | (uint64(uint32(value)) << 32)
		}
	case regQueueSel:
		f.queueSel = uint32(value)
	case regQueueNum:
		if q := f.selectedQueueLocked(); q != nil {
			q.size = uint16(value)
		}
	case regQueueReady:
		if q := f.selectedQueueLocked(); q != nil {
			q.ready = value != 0
			if value == 0 {
				q.lastAvailIdx = 0
				q.usedIdx = 0
			}
		}
	case regQueueDescLow:
		if q := f.selectedQueueLocked(); q != nil {
			f.setQueueAddr(&q.descAddr, uint32(value), true)
		}
	case regQueueDescHigh:
		if q := f.selectedQueueLocked(); q != nil {
			f.setQueueAddr(&q.descAddr, uint32(value), false)
		}
	case regQueueAvailLow:
		if q := f.selectedQueueLocked(); q != nil {
			f.setQueueAddr(&q.availAddr, uint32(value), true)
		}
	case regQueueAvailHigh:
		if q := f.selectedQueueLocked(); q != nil {
			f.setQueueAddr(&q.availAddr, uint32(value), false)
		}
	case regQueueUsedLow:
		if q := f.selectedQueueLocked(); q != nil {
			f.setQueueAddr(&q.usedAddr, uint32(value), true)
		}
	case regQueueUsedHigh:
		if q := f.selectedQueueLocked(); q != nil {
			f.setQueueAddr(&q.usedAddr, uint32(value), false)
		}
	case regInterruptAck:
		f.logf("interrupt-ack value=%#x", value)
		f.interruptStatus &^= uint32(value)
		return f.updateIRQLocked()
	case regStatus:
		f.status = uint32(value)
		if f.status == 0 {
			f.resetLocked()
		}
	case regQueueNotify:
		if int(value) < len(f.queues) {
			if err := f.processQueueLocked(int(value)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *FS) processQueueLocked(qidx int) error {
	q := &f.queues[qidx]
	if !q.ready || q.size == 0 || f.mem == nil {
		return nil
	}

	header, err := f.mem.ReadIPA(q.availAddr, 4)
	if err != nil {
		return err
	}
	availFlags := binary.LittleEndian.Uint16(header[0:2])
	availIdx := binary.LittleEndian.Uint16(header[2:4])
	interruptNeeded := false
	for q.lastAvailIdx != availIdx {
		slot := q.lastAvailIdx % q.size
		entry, err := f.mem.ReadIPA(q.availAddr+4+uint64(slot)*2, 2)
		if err != nil {
			return err
		}
		head := binary.LittleEndian.Uint16(entry)
		f.logf("queue-notify q=%d head=%d", qidx, head)
		usedLen, reply, err := f.handleRequestLocked(q, head)
		if err != nil {
			return err
		}
		if reply {
			if err := f.writeUsedLocked(q, head, usedLen); err != nil {
				return err
			}
			f.logf("used-ring q=%d head=%d len=%d", qidx, head, usedLen)
			interruptNeeded = true
		}
		q.lastAvailIdx++
	}
	if interruptNeeded && (qidx == fsQueueRequest || qidx == fsQueueHiprio) && (availFlags&1) == 0 {
		f.interruptStatus |= fsInterruptVring
		f.logf("interrupt-raise status=%#x", f.interruptStatus)
		return f.updateIRQLocked()
	}
	return nil
}

func (f *FS) handleRequestLocked(q *queue, head uint16) (uint32, bool, error) {
	descs, err := f.readDescriptorChainLocked(q, head)
	if err != nil {
		return 0, false, err
	}
	var reqDescs, respDescs []fsDesc
	for _, d := range descs {
		if d.write {
			respDescs = append(respDescs, d)
			continue
		}
		if len(respDescs) != 0 {
			return 0, false, fmt.Errorf("virtio-fs descriptor order invalid")
		}
		reqDescs = append(reqDescs, d)
	}
	if len(reqDescs) == 0 {
		return 0, false, fmt.Errorf("virtio-fs missing request descriptors")
	}
	reqLen := 0
	for _, d := range reqDescs {
		reqLen += int(d.length)
	}
	req := make([]byte, 0, reqLen)
	for _, d := range reqDescs {
		chunk, err := f.mem.ReadIPA(d.addr, int(d.length))
		if err != nil {
			return 0, false, err
		}
		req = append(req, chunk...)
	}
	reply, err := f.dispatchFUSELocked(req)
	if err != nil {
		return 0, false, err
	}
	if reply == nil {
		return 0, false, nil
	}
	offset := 0
	for _, d := range respDescs {
		if offset >= len(reply) {
			break
		}
		chunk := len(reply) - offset
		if chunk > int(d.length) {
			chunk = int(d.length)
		}
		if err := f.mem.WriteIPA(d.addr, reply[offset:offset+chunk]); err != nil {
			return 0, false, err
		}
		offset += chunk
	}
	if offset < len(reply) {
		return 0, false, fmt.Errorf("virtio-fs response truncated: need %d have %d", len(reply), offset)
	}
	return uint32(len(reply)), true, nil
}

func (f *FS) dispatchFUSELocked(req []byte) ([]byte, error) {
	if len(req) < fuseInHeaderSize {
		return nil, fmt.Errorf("virtio-fs short request: %d", len(req))
	}
	opcode := binary.LittleEndian.Uint32(req[4:8])
	unique := binary.LittleEndian.Uint64(req[8:16])
	nodeID := binary.LittleEndian.Uint64(req[16:24])
	f.logf("opcode=%d unique=%d node=%d", opcode, unique, nodeID)

	reply := func(errno int32, extra []byte) []byte {
		out := make([]byte, fuseOutHeaderSize+len(extra))
		binary.LittleEndian.PutUint32(out[0:4], uint32(len(out)))
		binary.LittleEndian.PutUint32(out[4:8], uint32(errno))
		binary.LittleEndian.PutUint64(out[8:16], unique)
		copy(out[16:], extra)
		return out
	}

	switch opcode {
	case fuseForget:
		return nil, nil
	case fuseInit:
		if len(req) < fuseInHeaderSize+16 {
			return nil, fmt.Errorf("virtio-fs INIT too short")
		}
		reqMajor := binary.LittleEndian.Uint32(req[40:44])
		reqMinor := binary.LittleEndian.Uint32(req[44:48])
		maxWrite, flags := f.backend.Init()
		if maxWrite == 0 {
			maxWrite = 128 << 10
		}
		extra := make([]byte, fuseInitOutSize)
		replyMajor := uint32(7)
		replyMinor := uint32(31)
		if reqMajor > 0 && reqMajor < replyMajor {
			replyMajor = reqMajor
		}
		if reqMajor == replyMajor && reqMinor > 0 && reqMinor < replyMinor {
			replyMinor = reqMinor
		}
		binary.LittleEndian.PutUint32(extra[0:4], replyMajor)
		binary.LittleEndian.PutUint32(extra[4:8], replyMinor)
		binary.LittleEndian.PutUint32(extra[8:12], 128<<10)
		binary.LittleEndian.PutUint32(extra[12:16], flags)
		binary.LittleEndian.PutUint16(extra[16:18], 16)
		binary.LittleEndian.PutUint16(extra[18:20], 32)
		binary.LittleEndian.PutUint32(extra[20:24], maxWrite)
		binary.LittleEndian.PutUint32(extra[24:28], 1)
		f.logf("init-reply major=%d minor=%d max_write=%d", replyMajor, replyMinor, maxWrite)
		return reply(0, extra), nil
	case fuseGetAttr:
		f.logPathf("getattr", nodeID, "")
		attr, errno := f.backend.GetAttr(nodeID)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		extra := make([]byte, fuseAttrOutSize)
		binary.LittleEndian.PutUint64(extra[0:8], 1)
		encodeFuseAttr(extra[16:], attr)
		return reply(0, extra), nil
	case fuseLookup:
		name := readCStringName(req[fuseInHeaderSize:])
		f.logPathf("lookup-parent", nodeID, fmt.Sprintf(" name=%q", name))
		childID, attr, errno := f.backend.Lookup(nodeID, path.Clean(name))
		if errno != 0 {
			return reply(errno, nil), nil
		}
		f.logPathf("lookup-child", childID, "")
		extra := make([]byte, fuseEntryOutSize)
		binary.LittleEndian.PutUint64(extra[0:8], childID)
		binary.LittleEndian.PutUint64(extra[16:24], 1)
		binary.LittleEndian.PutUint64(extra[24:32], 1)
		encodeFuseAttr(extra[40:], attr)
		return reply(0, extra), nil
	case fuseMkdir:
		if len(req) < fuseInHeaderSize+8 {
			return nil, fmt.Errorf("virtio-fs MKDIR too short")
		}
		name := readCStringName(req[fuseInHeaderSize+8:])
		mode := binary.LittleEndian.Uint32(req[40:44])
		f.logPathf("mkdir-parent", nodeID, fmt.Sprintf(" name=%q mode=%#o", name, mode))
		if be, ok := f.backend.(fsMkdirBackend); ok {
			childID, attr, errno := be.Mkdir(nodeID, path.Clean(name), mode)
			if errno != 0 {
				return reply(errno, nil), nil
			}
			extra := make([]byte, fuseEntryOutSize)
			binary.LittleEndian.PutUint64(extra[0:8], childID)
			binary.LittleEndian.PutUint64(extra[16:24], 1)
			binary.LittleEndian.PutUint64(extra[24:32], 1)
			encodeFuseAttr(extra[40:], attr)
			return reply(0, extra), nil
		}
		return nil, fmt.Errorf("virtio-fs missing mkdir backend for parent=%d name=%q", nodeID, name)
	case fuseOpen:
		if len(req) < fuseInHeaderSize+8 {
			return nil, fmt.Errorf("virtio-fs OPEN too short")
		}
		flags := binary.LittleEndian.Uint32(req[40:44])
		f.logPathf("open", nodeID, fmt.Sprintf(" flags=%#x", flags))
		fh, errno := f.backend.Open(nodeID, flags)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		extra := make([]byte, fuseOpenOutSize)
		binary.LittleEndian.PutUint64(extra[0:8], fh)
		return reply(0, extra), nil
	case fuseRead:
		if len(req) < fuseInHeaderSize+24 {
			return nil, fmt.Errorf("virtio-fs READ too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		off := binary.LittleEndian.Uint64(req[48:56])
		size := binary.LittleEndian.Uint32(req[56:60])
		f.logPathf("read", nodeID, fmt.Sprintf(" fh=%d off=%d size=%d", fh, off, size))
		data, errno := f.backend.Read(nodeID, fh, off, size)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		return reply(0, data), nil
	case fuseRelease:
		if len(req) < fuseInHeaderSize+24 {
			return nil, fmt.Errorf("virtio-fs RELEASE too short")
		}
		f.logPathf("release", nodeID, fmt.Sprintf(" fh=%d", binary.LittleEndian.Uint64(req[40:48])))
		f.backend.Release(nodeID, binary.LittleEndian.Uint64(req[40:48]))
		return reply(0, nil), nil
	case fuseOpenDir:
		if len(req) < fuseInHeaderSize+8 {
			return nil, fmt.Errorf("virtio-fs OPENDIR too short")
		}
		flags := binary.LittleEndian.Uint32(req[40:44])
		f.logPathf("opendir", nodeID, fmt.Sprintf(" flags=%#x", flags))
		fh, errno := f.backend.OpenDir(nodeID, flags)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		extra := make([]byte, fuseOpenOutSize)
		binary.LittleEndian.PutUint64(extra[0:8], fh)
		return reply(0, extra), nil
	case fuseReadDir:
		if len(req) < fuseInHeaderSize+24 {
			return nil, fmt.Errorf("virtio-fs READDIR too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		off := binary.LittleEndian.Uint64(req[48:56])
		size := binary.LittleEndian.Uint32(req[56:60])
		f.logPathf("readdir", nodeID, fmt.Sprintf(" fh=%d off=%d size=%d", fh, off, size))
		data, errno := f.backend.ReadDir(nodeID, fh, off, size)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		return reply(0, data), nil
	case fuseReleaseDir:
		if len(req) < fuseInHeaderSize+24 {
			return nil, fmt.Errorf("virtio-fs RELEASEDIR too short")
		}
		f.logPathf("releasedir", nodeID, fmt.Sprintf(" fh=%d", binary.LittleEndian.Uint64(req[40:48])))
		f.backend.ReleaseDir(nodeID, binary.LittleEndian.Uint64(req[40:48]))
		return reply(0, nil), nil
	case fuseRmDir:
		name := readCStringName(req[fuseInHeaderSize:])
		f.logPathf("rmdir-parent", nodeID, fmt.Sprintf(" name=%q", name))
		if be, ok := f.backend.(fsRmDirBackend); ok {
			errno := be.RmDir(nodeID, path.Clean(name))
			return reply(errno, nil), nil
		}
		return nil, fmt.Errorf("virtio-fs missing rmdir backend for parent=%d name=%q", nodeID, name)
	case fuseReadlink:
		f.logPathf("readlink", nodeID, "")
		target, errno := f.backend.Readlink(nodeID)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		return reply(0, []byte(target)), nil
	case fuseGetXattr:
		if len(req) < fuseInHeaderSize+8 {
			return nil, fmt.Errorf("virtio-fs GETXATTR too short")
		}
		size := binary.LittleEndian.Uint32(req[40:44])
		name := readCStringName(req[fuseInHeaderSize+8:])
		f.logPathf("getxattr", nodeID, fmt.Sprintf(" name=%q size=%d", name, size))
		if be, ok := f.backend.(fsXattrBackend); ok {
			value, errno := be.GetXattr(nodeID, name)
			if errno != 0 {
				return reply(errno, nil), nil
			}
			if size == 0 {
				extra := make([]byte, 8)
				binary.LittleEndian.PutUint32(extra[0:4], uint32(len(value)))
				return reply(0, extra), nil
			}
			if uint32(len(value)) > size {
				return reply(-linuxERANGE, nil), nil
			}
			return reply(0, value), nil
		}
		if f.Strict {
			return nil, fmt.Errorf("virtio-fs missing xattr backend for GETXATTR node=%d", nodeID)
		}
		return reply(-linuxENODATA, nil), nil
	case fuseListXattr:
		if len(req) < fuseInHeaderSize+8 {
			return nil, fmt.Errorf("virtio-fs LISTXATTR too short")
		}
		f.logPathf("listxattr", nodeID, "")
		if be, ok := f.backend.(fsXattrBackend); ok {
			value, errno := be.ListXattr(nodeID)
			if errno != 0 {
				return reply(errno, nil), nil
			}
			size := binary.LittleEndian.Uint32(req[40:44])
			if size == 0 {
				extra := make([]byte, 8)
				binary.LittleEndian.PutUint32(extra[0:4], uint32(len(value)))
				return reply(0, extra), nil
			}
			if uint32(len(value)) > size {
				return reply(-linuxERANGE, nil), nil
			}
			return reply(0, value), nil
		}
		if f.Strict {
			return nil, fmt.Errorf("virtio-fs missing xattr backend for LISTXATTR node=%d", nodeID)
		}
		return reply(0, nil), nil
	case fuseFlush:
		if len(req) < fuseInHeaderSize+24 {
			return nil, fmt.Errorf("virtio-fs FLUSH too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		lockOwner := binary.LittleEndian.Uint64(req[56:64])
		f.logPathf("flush", nodeID, fmt.Sprintf(" fh=%d lockOwner=%d", fh, lockOwner))
		if be, ok := f.backend.(fsFlushBackend); ok {
			return reply(be.Flush(nodeID, fh, lockOwner), nil), nil
		}
		if f.Strict {
			return nil, fmt.Errorf("virtio-fs missing flush backend for FLUSH node=%d fh=%d", nodeID, fh)
		}
		return reply(0, nil), nil
	case fuseAccess, fusePoll:
		if f.Strict {
			return nil, fmt.Errorf("virtio-fs unsupported opcode %s node=%d", fuseOpcodeName(opcode), nodeID)
		}
		return reply(-linuxENOSYS, nil), nil
	case fuseLseek:
		if len(req) < fuseInHeaderSize+24 {
			return nil, fmt.Errorf("virtio-fs LSEEK too short")
		}
		if be, ok := f.backend.(fsLseekBackend); ok {
			fh := binary.LittleEndian.Uint64(req[40:48])
			offset := binary.LittleEndian.Uint64(req[48:56])
			whence := binary.LittleEndian.Uint32(req[56:60])
			f.logPathf("lseek", nodeID, fmt.Sprintf(" fh=%d off=%d whence=%d", fh, offset, whence))
			newOff, errno := be.Lseek(nodeID, fh, offset, whence)
			if errno != 0 {
				return reply(errno, nil), nil
			}
			extra := make([]byte, 8)
			binary.LittleEndian.PutUint64(extra[0:8], newOff)
			return reply(0, extra), nil
		}
		if f.Strict {
			return nil, fmt.Errorf("virtio-fs missing lseek backend for LSEEK node=%d", nodeID)
		}
		return reply(-linuxENOSYS, nil), nil
	case fuseStatfs:
		blocks, bfree, bavail, files, ffree, bsize, frsize, namelen, errno := f.backend.StatFS(nodeID)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		extra := make([]byte, fuseStatfsOutSize)
		binary.LittleEndian.PutUint64(extra[0:8], blocks)
		binary.LittleEndian.PutUint64(extra[8:16], bfree)
		binary.LittleEndian.PutUint64(extra[16:24], bavail)
		binary.LittleEndian.PutUint64(extra[24:32], files)
		binary.LittleEndian.PutUint64(extra[32:40], ffree)
		binary.LittleEndian.PutUint32(extra[40:44], uint32(bsize))
		binary.LittleEndian.PutUint32(extra[44:48], uint32(namelen))
		binary.LittleEndian.PutUint32(extra[48:52], uint32(frsize))
		return reply(0, extra), nil
	case fuseSyncFS:
		return reply(0, nil), nil
	case fuseDestroy:
		return reply(0, nil), nil
	default:
		if f.Strict {
			return nil, fmt.Errorf("virtio-fs unsupported opcode %s(%d) node=%d", fuseOpcodeName(opcode), opcode, nodeID)
		}
		return reply(-linuxENOSYS, nil), nil
	}
}

func fuseOpcodeName(opcode uint32) string {
	switch opcode {
	case fuseLookup:
		return "LOOKUP"
	case fuseForget:
		return "FORGET"
	case fuseGetAttr:
		return "GETATTR"
	case fuseReadlink:
		return "READLINK"
	case fuseMkdir:
		return "MKDIR"
	case fuseRmDir:
		return "RMDIR"
	case fuseOpen:
		return "OPEN"
	case fuseRead:
		return "READ"
	case fuseStatfs:
		return "STATFS"
	case fuseRelease:
		return "RELEASE"
	case fuseGetXattr:
		return "GETXATTR"
	case fuseListXattr:
		return "LISTXATTR"
	case fuseFlush:
		return "FLUSH"
	case fuseInit:
		return "INIT"
	case fuseOpenDir:
		return "OPENDIR"
	case fuseReadDir:
		return "READDIR"
	case fuseReleaseDir:
		return "RELEASEDIR"
	case fuseAccess:
		return "ACCESS"
	case fuseDestroy:
		return "DESTROY"
	case fusePoll:
		return "POLL"
	case fuseLseek:
		return "LSEEK"
	case fuseSyncFS:
		return "SYNCFS"
	default:
		return "UNKNOWN"
	}
}

func (f *FS) logf(format string, args ...any) {
	if f.Log == nil {
		return
	}
	_, _ = fmt.Fprintf(f.Log, format+"\n", args...)
}

func (f *FS) logPathf(op string, nodeID uint64, suffix string) {
	if f.Log == nil {
		return
	}
	if resolver, ok := f.backend.(interface{ DebugPath(uint64) string }); ok {
		_, _ = fmt.Fprintf(f.Log, "%s node=%d path=%q%s\n", op, nodeID, resolver.DebugPath(nodeID), suffix)
		return
	}
	_, _ = fmt.Fprintf(f.Log, "%s node=%d%s\n", op, nodeID, suffix)
}

func (f *FS) readDescriptorChainLocked(q *queue, head uint16) ([]fsDesc, error) {
	var out []fsDesc
	index := head
	for i := uint16(0); i < q.size; i++ {
		if index >= q.size {
			return nil, fmt.Errorf("virtio-fs descriptor index %d out of range", index)
		}
		buf, err := f.mem.ReadIPA(q.descAddr+uint64(index)*16, 16)
		if err != nil {
			return nil, err
		}
		desc := fsDesc{
			addr:   binary.LittleEndian.Uint64(buf[0:8]),
			length: binary.LittleEndian.Uint32(buf[8:12]),
			flags:  binary.LittleEndian.Uint16(buf[12:14]),
			next:   binary.LittleEndian.Uint16(buf[14:16]),
		}
		desc.write = desc.flags&descFWrite != 0
		out = append(out, desc)
		if desc.flags&descFNext == 0 {
			return out, nil
		}
		index = desc.next
	}
	return nil, fmt.Errorf("virtio-fs descriptor loop")
}

func (f *FS) writeUsedLocked(q *queue, head uint16, usedLen uint32) error {
	slot := q.usedIdx % q.size
	elem := make([]byte, 8)
	binary.LittleEndian.PutUint32(elem[0:4], uint32(head))
	binary.LittleEndian.PutUint32(elem[4:8], usedLen)
	if err := f.mem.WriteIPA(q.usedAddr+4+uint64(slot)*8, elem); err != nil {
		return err
	}
	q.usedIdx++
	idx := make([]byte, 2)
	binary.LittleEndian.PutUint16(idx, q.usedIdx)
	return f.mem.WriteIPA(q.usedAddr+2, idx)
}

func (f *FS) updateIRQLocked() error {
	if f.irq == nil {
		return nil
	}
	level := f.interruptStatus != 0
	if f.irqHigh == level {
		return nil
	}
	f.irqHigh = level
	f.logf("set-irq irq=%d level=%v", f.IRQ, level)
	return f.irq.SetIRQ(f.IRQ, level)
}

func (f *FS) selectedQueueLocked() *queue {
	if f.queueSel >= uint32(len(f.queues)) {
		return nil
	}
	return &f.queues[f.queueSel]
}

func (f *FS) setQueueAddr(target *uint64, value uint32, low bool) {
	if low {
		*target = (*target &^ 0xffffffff) | uint64(value)
	} else {
		*target = (*target & 0xffffffff) | (uint64(value) << 32)
	}
}

func (f *FS) resetLocked() {
	f.deviceFeatureSel = 0
	f.driverFeatureSel = 0
	f.driverFeatures = 0
	f.queueSel = 0
	f.status = 0
	f.interruptStatus = 0
	f.irqHigh = false
	f.configGeneration++
	f.queues = [2]queue{}
}

func (f *FS) configBytesLocked() []byte {
	cfg := make([]byte, fsCfgTotalSize)
	copy(cfg[:fsCfgTagSize], f.tag[:])
	binary.LittleEndian.PutUint32(cfg[fsCfgNumQueueOff:fsCfgNumQueueOff+4], 1)
	return cfg
}

func encodeFuseAttr(dst []byte, attr FuseAttr) {
	binary.LittleEndian.PutUint64(dst[0:8], attr.Ino)
	binary.LittleEndian.PutUint64(dst[8:16], attr.Size)
	binary.LittleEndian.PutUint64(dst[16:24], attr.Blocks)
	binary.LittleEndian.PutUint64(dst[24:32], attr.ATimeSec)
	binary.LittleEndian.PutUint64(dst[32:40], attr.MTimeSec)
	binary.LittleEndian.PutUint64(dst[40:48], attr.CTimeSec)
	binary.LittleEndian.PutUint32(dst[48:52], attr.ATimeNsec)
	binary.LittleEndian.PutUint32(dst[52:56], attr.MTimeNsec)
	binary.LittleEndian.PutUint32(dst[56:60], attr.CTimeNsec)
	binary.LittleEndian.PutUint32(dst[60:64], attr.Mode)
	binary.LittleEndian.PutUint32(dst[64:68], attr.NLink)
	binary.LittleEndian.PutUint32(dst[68:72], attr.UID)
	binary.LittleEndian.PutUint32(dst[72:76], attr.GID)
	binary.LittleEndian.PutUint32(dst[76:80], attr.RDev)
	binary.LittleEndian.PutUint32(dst[80:84], attr.BlkSize)
	binary.LittleEndian.PutUint32(dst[84:88], attr.Flags)
}

func readCStringName(buf []byte) string {
	if i := bytesIndexByte(buf, 0); i >= 0 {
		buf = buf[:i]
	}
	return string(buf)
}

func bytesIndexByte(buf []byte, want byte) int {
	for i, b := range buf {
		if b == want {
			return i
		}
	}
	return -1
}

type passthroughFS struct {
	root string
	meta map[string]fsmeta.Entry

	mu         sync.Mutex
	nextNodeID uint64
	nextHandle uint64
	nodes      map[uint64]string
	pathToNode map[string]uint64
	handles    map[uint64]uint64
	dirHandles map[uint64][]dirEntry
}

type imageFS struct {
	root string

	mu         sync.Mutex
	nextNodeID uint64
	nextHandle uint64
	nodes      map[uint64]*imageNode
	handles    map[uint64]uint64
	dirHandles map[uint64][]dirEntry
}

type imageNode struct {
	id            uint64
	parent        uint64
	name          string
	mode          fs.FileMode
	rawMode       uint32
	uid           uint32
	gid           uint32
	rdev          uint32
	size          uint64
	symlinkTarget string
	entries       map[string]uint64
	modTime       time.Time
	abstractFile  imageAbstractFile
	abstractDir   imageAbstractDir
	abstractLink  imageAbstractSymlink
}

type dirEntry struct {
	name string
	typ  uint32
	ino  uint64
}

type imageAbstractFile interface {
	Stat() (size uint64, mode fs.FileMode)
	ModTime() time.Time
	ReadAt(off uint64, size uint32) ([]byte, error)
	Owner() (uid, gid uint32)
	RDev() uint32
}

type imageAbstractDir interface {
	Stat() fs.FileMode
	ModTime() time.Time
	ReadDir() ([]imageDirEnt, error)
	Lookup(name string) (imageAbstractEntry, error)
	Owner() (uid, gid uint32)
	RDev() uint32
}

type imageAbstractSymlink interface {
	Stat() fs.FileMode
	ModTime() time.Time
	Target() string
	Owner() (uid, gid uint32)
	RDev() uint32
}

type imageAbstractEntry struct {
	File    imageAbstractFile
	Dir     imageAbstractDir
	Symlink imageAbstractSymlink
}

type imageDirEnt struct {
	Name string
	Mode fs.FileMode
}

type imageHostFile struct {
	hostPath string
	mode     fs.FileMode
	uid      uint32
	gid      uint32
	rdev     uint32
	size     uint64
	modTime  time.Time
}

type imageHostDir struct {
	rootPath string
	hostPath string
	mode     fs.FileMode
	uid      uint32
	gid      uint32
	rdev     uint32
	modTime  time.Time
	meta     map[string]fsmeta.Entry
}

type imageHostSymlink struct {
	hostPath string
	mode     fs.FileMode
	uid      uint32
	gid      uint32
	rdev     uint32
	target   string
	modTime  time.Time
}

func NewPassthroughFS(root string, meta map[string]fsmeta.Entry) FSBackend {
	fs := &passthroughFS{
		root:       root,
		meta:       meta,
		nextNodeID: 2,
		nextHandle: 1,
		nodes:      map[uint64]string{1: "/"},
		pathToNode: map[string]uint64{"/": 1},
		handles:    map[uint64]uint64{},
		dirHandles: map[uint64][]dirEntry{},
	}
	return fs
}

func NewImageFS(root string, meta map[string]fsmeta.Entry) FSBackend {
	imgFS := &imageFS{
		root:       root,
		nextNodeID: 2,
		nextHandle: 1,
		nodes:      map[uint64]*imageNode{},
		handles:    map[uint64]uint64{},
		dirHandles: map[uint64][]dirEntry{},
	}
	rootMode := fs.ModeDir | 0o755
	rootUID := uint32(0)
	rootGID := uint32(0)
	rootRDev := uint32(0)
	if entry, ok := meta["/"]; ok {
		rootMode = linuxModeToGo(fsmeta.NormalizeLinuxMode(entry.Mode, fs.ModeDir|0o755))
		rootUID = entry.UID
		rootGID = entry.GID
		rootRDev = entry.RDev
	}
	rootModTime := time.Unix(0, 0)
	if info, err := os.Lstat(root); err == nil {
		rootModTime = info.ModTime()
	}
	imgFS.nodes[1] = &imageNode{
		id:      1,
		parent:  1,
		name:    "/",
		mode:    rootMode,
		uid:     rootUID,
		gid:     rootGID,
		rdev:    rootRDev,
		entries: map[string]uint64{},
		modTime: rootModTime,
		abstractDir: &imageHostDir{
			rootPath: root,
			hostPath: root,
			mode:     rootMode,
			uid:      rootUID,
			gid:      rootGID,
			rdev:     rootRDev,
			modTime:  rootModTime,
			meta:     meta,
		},
	}
	return imgFS
}

func (p *passthroughFS) Init() (uint32, uint32) {
	return 128 << 10, fuseCapPosixACL | fuseCapPosixLocks
}

func (p *passthroughFS) GetAttr(nodeID uint64) (FuseAttr, int32) {
	p.logNode("getattr", nodeID)
	host, errno := p.hostPath(nodeID)
	if errno != 0 {
		return FuseAttr{}, errno
	}
	info, err := os.Lstat(host)
	if err != nil {
		return FuseAttr{}, errnoFromError(err)
	}
	return p.fileAttr(nodeID, info), 0
}

func (p *passthroughFS) Lookup(parent uint64, name string) (uint64, FuseAttr, int32) {
	p.logNode("lookup-parent", parent)
	hostParent, errno := p.hostPath(parent)
	if errno != 0 {
		return 0, FuseAttr{}, errno
	}
	clean := path.Clean("/" + name)
	if clean == "/" {
		attr, errno := p.GetAttr(parent)
		return parent, attr, errno
	}
	rel := strings.TrimPrefix(clean, "/")
	host := filepath.Join(hostParent, filepath.FromSlash(rel))
	info, err := os.Lstat(host)
	if err != nil {
		return 0, FuseAttr{}, errnoFromError(err)
	}
	guestPath := p.guestPathForHost(host)
	if p.root != "" {
		p.logf("lookup name=%q guest=%q host=%q", name, guestPath, host)
	}
	nodeID := p.ensureNode(guestPath)
	return nodeID, p.fileAttr(nodeID, info), 0
}

func (p *passthroughFS) Mkdir(parent uint64, name string, mode uint32) (uint64, FuseAttr, int32) {
	p.logNode("mkdir-parent", parent)
	hostParent, errno := p.hostPath(parent)
	if errno != 0 {
		return 0, FuseAttr{}, errno
	}
	clean := path.Clean("/" + name)
	if clean == "/" {
		return 0, FuseAttr{}, -linuxEINVAL
	}
	rel := strings.TrimPrefix(clean, "/")
	host := filepath.Join(hostParent, filepath.FromSlash(rel))
	if err := os.Mkdir(host, fs.FileMode(mode&linuxPermMask)); err != nil {
		return 0, FuseAttr{}, errnoFromError(err)
	}
	info, err := os.Lstat(host)
	if err != nil {
		return 0, FuseAttr{}, errnoFromError(err)
	}
	guestPath := p.guestPathForHost(host)
	if p.meta != nil {
		p.mu.Lock()
		if _, ok := p.meta[guestPath]; !ok {
			p.meta[guestPath] = fsmeta.Entry{
				UID:  0,
				GID:  0,
				Mode: uint32(linuxSIFDIR) | (mode & linuxPermMask),
			}
		}
		p.mu.Unlock()
	}
	nodeID := p.ensureNode(guestPath)
	return nodeID, p.fileAttr(nodeID, info), 0
}

func (p *passthroughFS) Open(nodeID uint64, _ uint32) (uint64, int32) {
	p.logNode("open", nodeID)
	host, errno := p.hostPath(nodeID)
	if errno != 0 {
		return 0, errno
	}
	info, err := os.Lstat(host)
	if err != nil {
		return 0, errnoFromError(err)
	}
	if info.IsDir() {
		return 0, -linuxEISDIR
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	handle := p.nextHandle
	p.nextHandle++
	p.handles[handle] = nodeID
	return handle, 0
}

func (p *passthroughFS) Release(_ uint64, fh uint64) {
	p.mu.Lock()
	delete(p.handles, fh)
	p.mu.Unlock()
}

func (p *passthroughFS) Flush(_ uint64, _ uint64, _ uint64) int32 {
	return 0
}

func (p *passthroughFS) Read(nodeID uint64, fh uint64, off uint64, size uint32) ([]byte, int32) {
	p.logf("read node=%d path=%q fh=%d off=%d size=%d", nodeID, p.DebugPath(nodeID), fh, off, size)
	p.mu.Lock()
	nid, ok := p.handles[fh]
	p.mu.Unlock()
	if !ok || nid != nodeID {
		return nil, -linuxEBADF
	}
	host, errno := p.hostPath(nodeID)
	if errno != 0 {
		return nil, errno
	}
	f, err := os.Open(host)
	if err != nil {
		return nil, errnoFromError(err)
	}
	defer f.Close()
	buf := make([]byte, size)
	n, err := f.ReadAt(buf, int64(off))
	if err != nil && err != io.EOF {
		return nil, errnoFromError(err)
	}
	return buf[:n], 0
}

func (p *passthroughFS) Lseek(nodeID uint64, fh uint64, offset uint64, whence uint32) (uint64, int32) {
	p.mu.Lock()
	nid, ok := p.handles[fh]
	p.mu.Unlock()
	if !ok || nid != nodeID {
		return 0, -linuxEBADF
	}
	host, errno := p.hostPath(nodeID)
	if errno != 0 {
		return 0, errno
	}
	info, err := os.Lstat(host)
	if err != nil {
		return 0, errnoFromError(err)
	}
	if info.IsDir() {
		return 0, -linuxEISDIR
	}
	size := uint64(info.Size())
	switch whence {
	case 3: // SEEK_DATA
		if offset >= size {
			return 0, -linuxENXIO
		}
		return offset, 0
	case 4: // SEEK_HOLE
		if offset >= size {
			return offset, 0
		}
		return size, 0
	default:
		return 0, -linuxEINVAL
	}
}

func (p *passthroughFS) OpenDir(nodeID uint64, _ uint32) (uint64, int32) {
	p.logNode("opendir", nodeID)
	host, errno := p.hostPath(nodeID)
	if errno != 0 {
		return 0, errno
	}
	entries, err := os.ReadDir(host)
	if err != nil {
		return 0, errnoFromError(err)
	}
	dirEntries := []dirEntry{
		{name: ".", typ: dirTypeDir, ino: nodeID},
		{name: "..", typ: dirTypeDir, ino: nodeID},
	}
	for _, entry := range entries {
		childPath := p.guestPathForHost(filepath.Join(host, entry.Name()))
		childID := p.ensureNode(childPath)
		dirEntries = append(dirEntries, dirEntry{
			name: entry.Name(),
			typ:  dirTypeForMode(entry.Type()),
			ino:  childID,
		})
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	handle := p.nextHandle
	p.nextHandle++
	p.dirHandles[handle] = dirEntries
	return handle, 0
}

func (p *passthroughFS) ReadDir(_ uint64, fh uint64, off uint64, maxBytes uint32) ([]byte, int32) {
	p.mu.Lock()
	entries := append([]dirEntry(nil), p.dirHandles[fh]...)
	p.mu.Unlock()
	if entries == nil {
		return nil, -linuxEBADF
	}
	var out []byte
	for i := int(off); i < len(entries); i++ {
		entry := entries[i]
		nameBytes := []byte(entry.name)
		reclen := align8(fuseDirentBaseSize + len(nameBytes))
		if len(out)+reclen > int(maxBytes) {
			break
		}
		start := len(out)
		out = append(out, make([]byte, reclen)...)
		binary.LittleEndian.PutUint64(out[start:start+8], entry.ino)
		binary.LittleEndian.PutUint64(out[start+8:start+16], uint64(i+1))
		binary.LittleEndian.PutUint32(out[start+16:start+20], uint32(len(nameBytes)))
		binary.LittleEndian.PutUint32(out[start+20:start+24], entry.typ)
		copy(out[start+24:start+24+len(nameBytes)], nameBytes)
	}
	return out, 0
}

func (p *passthroughFS) ReleaseDir(_ uint64, fh uint64) {
	p.mu.Lock()
	delete(p.dirHandles, fh)
	p.mu.Unlock()
}

func (p *passthroughFS) Readlink(nodeID uint64) (string, int32) {
	p.logNode("readlink", nodeID)
	host, errno := p.hostPath(nodeID)
	if errno != 0 {
		return "", errno
	}
	target, err := os.Readlink(host)
	if err != nil {
		return "", errnoFromError(err)
	}
	return target, 0
}

func (p *passthroughFS) RmDir(parent uint64, name string) int32 {
	p.logNode("rmdir-parent", parent)
	hostParent, errno := p.hostPath(parent)
	if errno != 0 {
		return errno
	}
	clean := path.Clean("/" + name)
	if clean == "/" {
		return -linuxEINVAL
	}
	rel := strings.TrimPrefix(clean, "/")
	host := filepath.Join(hostParent, filepath.FromSlash(rel))
	if err := os.Remove(host); err != nil {
		return errnoFromError(err)
	}
	guestPath := p.guestPathForHost(host)
	p.mu.Lock()
	delete(p.meta, guestPath)
	delete(p.pathToNode, guestPath)
	for id, existing := range p.nodes {
		if existing == guestPath {
			delete(p.nodes, id)
			break
		}
	}
	p.mu.Unlock()
	return 0
}

func (p *passthroughFS) StatFS(_ uint64) (uint64, uint64, uint64, uint64, uint64, uint64, uint64, uint64, int32) {
	if p.root == "" {
		return 0, 0, 0, 0, 0, 4096, 4096, 255, 0
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(p.root, &st); err != nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, errnoFromError(err)
	}
	return st.Blocks, st.Bfree, st.Bavail, st.Files, st.Ffree, uint64(st.Bsize), uint64(st.Bsize), 255, 0
}

func (p *passthroughFS) hostPath(nodeID uint64) (string, int32) {
	p.mu.Lock()
	guest, ok := p.nodes[nodeID]
	p.mu.Unlock()
	if !ok {
		return "", -linuxENOENT
	}
	if p.root == "" {
		return "", -linuxENOENT
	}
	if guest == "/" {
		return p.root, 0
	}
	return filepath.Join(p.root, filepath.FromSlash(strings.TrimPrefix(guest, "/"))), 0
}

func (p *passthroughFS) guestPathForHost(host string) string {
	if p.root == "" {
		return "/"
	}
	rel, err := filepath.Rel(p.root, host)
	if err != nil || rel == "." {
		return "/"
	}
	return "/" + filepath.ToSlash(rel)
}

func (p *passthroughFS) DebugPath(nodeID uint64) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.nodes[nodeID]
}

func (p *passthroughFS) GetXattr(nodeID uint64, name string) ([]byte, int32) {
	p.logf("getxattr-backend node=%d path=%q name=%q", nodeID, p.DebugPath(nodeID), name)
	return nil, -linuxENODATA
}

func (p *passthroughFS) ListXattr(nodeID uint64) ([]byte, int32) {
	p.logNode("listxattr-backend", nodeID)
	return nil, 0
}

func (p *passthroughFS) logNode(op string, nodeID uint64) {
	p.logf("%s node=%d path=%q", op, nodeID, p.DebugPath(nodeID))
}

func (p *passthroughFS) logf(format string, args ...any) {
	_ = format
	_ = args
}

func (p *passthroughFS) ensureNode(guestPath string) uint64 {
	guestPath = path.Clean("/" + strings.TrimPrefix(guestPath, "/"))
	p.mu.Lock()
	defer p.mu.Unlock()
	if id, ok := p.pathToNode[guestPath]; ok {
		return id
	}
	id := p.nextNodeID
	p.nextNodeID++
	p.pathToNode[guestPath] = id
	p.nodes[id] = guestPath
	return id
}

func (p *passthroughFS) fileAttr(nodeID uint64, info os.FileInfo) FuseAttr {
	mode := goModeToLinux(info.Mode())
	if mode&os.ModeType == 0 {
		mode |= 0
	}
	attr := FuseAttr{
		Ino:     nodeID,
		Size:    uint64(info.Size()),
		Blocks:  uint64((info.Size() + 511) / 512),
		Mode:    fsmeta.NormalizeLinuxMode(0, info.Mode()),
		NLink:   1,
		UID:     0,
		GID:     0,
		BlkSize: 4096,
	}
	mod := info.ModTime()
	attr.ATimeSec = uint64(mod.Unix())
	attr.MTimeSec = uint64(mod.Unix())
	attr.CTimeSec = uint64(mod.Unix())
	attr.ATimeNsec = uint32(mod.Nanosecond())
	attr.MTimeNsec = uint32(mod.Nanosecond())
	attr.CTimeNsec = uint32(mod.Nanosecond())
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		attr.Size = uint64(st.Size)
		attr.Blocks = uint64(st.Blocks)
		attr.NLink = uint32(st.Nlink)
		if st.Blksize > 0 {
			attr.BlkSize = uint32(st.Blksize)
		}
		attr.ATimeSec = uint64(st.Atimespec.Sec)
		attr.MTimeSec = uint64(st.Mtimespec.Sec)
		attr.CTimeSec = uint64(st.Ctimespec.Sec)
		attr.ATimeNsec = uint32(st.Atimespec.Nsec)
		attr.MTimeNsec = uint32(st.Mtimespec.Nsec)
		attr.CTimeNsec = uint32(st.Ctimespec.Nsec)
	}
	if attr.Blocks == 0 && attr.Size > 0 {
		attr.Blocks = uint64((attr.Size + 511) / 512)
	}
	if attr.BlkSize == 0 {
		attr.BlkSize = 4096
	}
	if meta, ok := p.meta[p.DebugPath(nodeID)]; ok {
		attr.UID = meta.UID
		attr.GID = meta.GID
		if meta.RDev != 0 {
			attr.RDev = meta.RDev
		}
		if meta.Mode != 0 {
			attr.Mode = fsmeta.NormalizeLinuxMode(meta.Mode, info.Mode())
		}
	}
	if info.IsDir() {
		attr.NLink = maxU32(attr.NLink, 2)
	}
	return attr
}

func (p *imageFS) pathForNode(id uint64) string {
	node := p.nodes[id]
	if node == nil {
		return ""
	}
	if id == 1 {
		return "/"
	}
	var parts []string
	for node != nil && node.id != 1 {
		parts = append(parts, node.name)
		node = p.nodes[node.parent]
	}
	for i, j := 0, len(parts)-1; i < j; i, j = i+1, j-1 {
		parts[i], parts[j] = parts[j], parts[i]
	}
	return "/" + strings.Join(parts, "/")
}

func (p *imageFS) Init() (uint32, uint32) {
	return 128 << 10, fuseCapPosixACL | fuseCapPosixLocks
}

func (p *imageFS) GetAttr(nodeID uint64) (FuseAttr, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return FuseAttr{}, -linuxENOENT
	}
	return p.attr(node), 0
}

func (p *imageFS) Lookup(parent uint64, name string) (uint64, FuseAttr, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	if parentNode == nil {
		return 0, FuseAttr{}, -linuxENOENT
	}
	name = path.Base(path.Clean("/" + name))
	if name == "." {
		return parentNode.id, p.attr(parentNode), 0
	}
	childID, ok := parentNode.entries[name]
	if !ok {
		if parentNode.abstractDir == nil {
			return 0, FuseAttr{}, -linuxENOENT
		}
		entry, err := parentNode.abstractDir.Lookup(name)
		if err != nil {
			return 0, FuseAttr{}, -linuxENOENT
		}
		child, errno := p.createAbstractNode(parentNode, name, entry)
		if errno != 0 {
			return 0, FuseAttr{}, errno
		}
		return child.id, p.attr(child), 0
	}
	child := p.nodes[childID]
	if child == nil {
		return 0, FuseAttr{}, -linuxENOENT
	}
	return child.id, p.attr(child), 0
}

func (p *imageFS) Open(nodeID uint64, _ uint32) (uint64, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return 0, -linuxENOENT
	}
	if node.isDir() {
		return 0, -linuxEISDIR
	}
	fh := p.nextHandle
	p.nextHandle++
	p.handles[fh] = nodeID
	return fh, 0
}

func (p *imageFS) Release(_ uint64, fh uint64) {
	p.mu.Lock()
	delete(p.handles, fh)
	p.mu.Unlock()
}

func (p *imageFS) Flush(_ uint64, _ uint64, _ uint64) int32 {
	return 0
}

func (p *imageFS) Read(nodeID uint64, fh uint64, off uint64, size uint32) ([]byte, int32) {
	p.mu.Lock()
	nid, ok := p.handles[fh]
	node := p.nodes[nodeID]
	p.mu.Unlock()
	if !ok || nid != nodeID || node == nil {
		return nil, -linuxEBADF
	}
	if node.abstractFile == nil {
		return nil, -linuxEIO
	}
	data, err := node.abstractFile.ReadAt(off, size)
	if err != nil {
		return nil, errnoFromError(err)
	}
	if data == nil {
		return []byte{}, 0
	}
	return data, 0
}

func (p *imageFS) Lseek(nodeID uint64, fh uint64, offset uint64, whence uint32) (uint64, int32) {
	p.mu.Lock()
	nid, ok := p.handles[fh]
	node := p.nodes[nodeID]
	p.mu.Unlock()
	if !ok || nid != nodeID || node == nil {
		return 0, -linuxEBADF
	}
	switch whence {
	case 3:
		if offset >= node.size {
			return 0, -linuxENXIO
		}
		return offset, 0
	case 4:
		if offset >= node.size {
			return offset, 0
		}
		return node.size, 0
	default:
		return 0, -linuxEINVAL
	}
}

func (p *imageFS) OpenDir(nodeID uint64, _ uint32) (uint64, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return 0, -linuxENOENT
	}
	if !node.isDir() {
		return 0, -linuxENOTDIR
	}
	if node.abstractDir != nil && len(node.entries) == 0 {
		if _, errno := p.materializeDirEntriesLocked(node); errno != 0 {
			return 0, errno
		}
	}
	entries := []dirEntry{
		{name: ".", typ: dirTypeDir, ino: node.id},
		{name: "..", typ: dirTypeDir, ino: node.parent},
	}
	names := make([]string, 0, len(node.entries))
	for name := range node.entries {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		child := p.nodes[node.entries[name]]
		entries = append(entries, dirEntry{name: name, typ: p.dirType(child), ino: child.id})
	}
	fh := p.nextHandle
	p.nextHandle++
	p.dirHandles[fh] = entries
	return fh, 0
}

func (p *imageFS) ReadDir(_ uint64, fh uint64, off uint64, maxBytes uint32) ([]byte, int32) {
	p.mu.Lock()
	entries := append([]dirEntry(nil), p.dirHandles[fh]...)
	p.mu.Unlock()
	if entries == nil {
		return nil, -linuxEBADF
	}
	var out []byte
	for i := int(off); i < len(entries); i++ {
		entry := entries[i]
		nameBytes := []byte(entry.name)
		reclen := align8(fuseDirentBaseSize + len(nameBytes))
		if len(out)+reclen > int(maxBytes) {
			break
		}
		start := len(out)
		out = append(out, make([]byte, reclen)...)
		binary.LittleEndian.PutUint64(out[start:start+8], entry.ino)
		binary.LittleEndian.PutUint64(out[start+8:start+16], uint64(i+1))
		binary.LittleEndian.PutUint32(out[start+16:start+20], uint32(len(nameBytes)))
		binary.LittleEndian.PutUint32(out[start+20:start+24], entry.typ)
		copy(out[start+24:start+24+len(nameBytes)], nameBytes)
	}
	return out, 0
}

func (p *imageFS) ReleaseDir(_ uint64, fh uint64) {
	p.mu.Lock()
	delete(p.dirHandles, fh)
	p.mu.Unlock()
}

func (p *imageFS) Readlink(nodeID uint64) (string, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return "", -linuxENOENT
	}
	if !node.isSymlink() {
		return "", -linuxEINVAL
	}
	return node.symlinkTarget, 0
}

func (p *imageFS) Mkdir(parent uint64, name string, mode uint32) (uint64, FuseAttr, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	if parentNode == nil {
		return 0, FuseAttr{}, -linuxENOENT
	}
	name = path.Base(path.Clean("/" + name))
	if _, exists := parentNode.entries[name]; exists {
		return 0, FuseAttr{}, -linuxEEXIST
	}
	node := &imageNode{
		id:      p.nextNodeID,
		parent:  parent,
		name:    name,
		mode:    fs.ModeDir | fs.FileMode(mode&linuxPermMask),
		entries: map[string]uint64{},
	}
	p.nextNodeID++
	p.nodes[node.id] = node
	parentNode.entries[name] = node.id
	return node.id, p.attr(node), 0
}

func (p *imageFS) RmDir(parent uint64, name string) int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	if parentNode == nil {
		return -linuxENOENT
	}
	name = path.Base(path.Clean("/" + name))
	childID, ok := parentNode.entries[name]
	if !ok {
		return -linuxENOENT
	}
	child := p.nodes[childID]
	if child == nil {
		return -linuxENOENT
	}
	if len(child.entries) != 0 {
		return -linuxENOTEMPTY
	}
	delete(parentNode.entries, name)
	delete(p.nodes, childID)
	return 0
}

func (p *imageFS) StatFS(_ uint64) (uint64, uint64, uint64, uint64, uint64, uint64, uint64, uint64, int32) {
	if p.root == "" {
		return 0, 0, 0, 0, 0, 4096, 4096, 255, 0
	}
	var st syscall.Statfs_t
	if err := syscall.Statfs(p.root, &st); err != nil {
		return 0, 0, 0, 0, 0, 0, 0, 0, errnoFromError(err)
	}
	return st.Blocks, st.Bfree, st.Bavail, st.Files, st.Ffree, uint64(st.Bsize), uint64(st.Bsize), 255, 0
}

func (p *imageFS) GetXattr(_ uint64, _ string) ([]byte, int32) {
	return nil, -linuxENODATA
}

func (p *imageFS) ListXattr(_ uint64) ([]byte, int32) {
	return nil, 0
}

func (p *imageFS) attr(node *imageNode) FuseAttr {
	var mode uint32
	size := node.size
	modTime := node.modTime
	switch {
	case node.abstractFile != nil:
		size, node.mode = node.abstractFile.Stat()
		if mt := node.abstractFile.ModTime(); !mt.IsZero() {
			modTime = mt
		}
	case node.abstractDir != nil:
		node.mode = fs.ModeDir | node.abstractDir.Stat()
		if mt := node.abstractDir.ModTime(); !mt.IsZero() {
			modTime = mt
		}
	case node.abstractLink != nil:
		node.mode = fs.ModeSymlink | node.abstractLink.Stat().Perm()
		node.symlinkTarget = node.abstractLink.Target()
		size = uint64(len(node.symlinkTarget))
		if mt := node.abstractLink.ModTime(); !mt.IsZero() {
			modTime = mt
		}
	}
	switch {
	case node.isDir():
		mode = linuxSIFDIR | linuxModeBits(node.mode)
	case node.isSymlink():
		mode = linuxSIFLNK | linuxModeBits(node.mode)
	case node.rawMode != 0:
		mode = (node.rawMode &^ linuxPermMask) | linuxModeBits(node.mode)
	default:
		mode = linuxSIFREG | linuxModeBits(node.mode)
	}
	nlink := uint32(1)
	if node.isDir() {
		nlink = maxU32(2, 2+uint32(len(node.entries)))
	}
	return FuseAttr{
		Ino:       node.id,
		Size:      size,
		Blocks:    uint64((size + 511) / 512),
		ATimeSec:  uint64(modTime.Unix()),
		MTimeSec:  uint64(modTime.Unix()),
		CTimeSec:  uint64(modTime.Unix()),
		ATimeNsec: uint32(modTime.Nanosecond()),
		MTimeNsec: uint32(modTime.Nanosecond()),
		CTimeNsec: uint32(modTime.Nanosecond()),
		Mode:      mode,
		NLink:     nlink,
		UID:       node.uid,
		GID:       node.gid,
		RDev:      node.rdev,
		BlkSize:   4096,
	}
}

func (p *imageFS) dirType(node *imageNode) uint32 {
	switch {
	case node.isDir():
		return dirTypeDir
	case node.isSymlink():
		return dirTypeLink
	case node.rawMode&linuxSIFMT == linuxSIFCHR:
		return dirTypeChar
	case node.rawMode&linuxSIFMT == linuxSIFBLK:
		return dirTypeBlock
	case node.rawMode&linuxSIFMT == linuxSIFIFO:
		return dirTypeFIFO
	case node.rawMode&linuxSIFMT == linuxSIFSOCK:
		return dirTypeSocket
	default:
		return dirTypeFile
	}
}

func (p *imageFS) createAbstractNode(parent *imageNode, name string, entry imageAbstractEntry) (*imageNode, int32) {
	node := &imageNode{
		id:      p.nextNodeID,
		parent:  parent.id,
		name:    name,
		entries: map[string]uint64{},
		modTime: time.Unix(0, 0),
	}
	p.nextNodeID++
	switch {
	case entry.Dir != nil:
		node.abstractDir = entry.Dir
		node.mode = fs.ModeDir | entry.Dir.Stat()
		node.modTime = entry.Dir.ModTime()
		node.uid, node.gid = entry.Dir.Owner()
		node.rdev = entry.Dir.RDev()
	case entry.File != nil:
		node.abstractFile = entry.File
		node.size, node.mode = entry.File.Stat()
		node.modTime = entry.File.ModTime()
		node.uid, node.gid = entry.File.Owner()
		node.rdev = entry.File.RDev()
	case entry.Symlink != nil:
		node.abstractLink = entry.Symlink
		node.mode = fs.ModeSymlink | entry.Symlink.Stat().Perm()
		node.symlinkTarget = entry.Symlink.Target()
		node.size = uint64(len(node.symlinkTarget))
		node.modTime = entry.Symlink.ModTime()
		node.uid, node.gid = entry.Symlink.Owner()
		node.rdev = entry.Symlink.RDev()
	default:
		return nil, -linuxENOENT
	}
	if node.modTime.IsZero() {
		node.modTime = time.Unix(0, 0)
	}
	p.nodes[node.id] = node
	parent.entries[name] = node.id
	return node, 0
}

func (p *imageFS) materializeDirEntriesLocked(node *imageNode) ([]imageDirEnt, int32) {
	if node.abstractDir == nil {
		return nil, 0
	}
	ents, err := node.abstractDir.ReadDir()
	if err != nil {
		return nil, -linuxENOENT
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].Name < ents[j].Name })
	for _, ent := range ents {
		if ent.Name == "." || ent.Name == ".." {
			continue
		}
		if _, ok := node.entries[ent.Name]; ok {
			continue
		}
		entry, err := node.abstractDir.Lookup(ent.Name)
		if err != nil {
			continue
		}
		if _, errno := p.createAbstractNode(node, ent.Name, entry); errno != 0 {
			return nil, errno
		}
	}
	return ents, 0
}

func (n *imageNode) isDir() bool {
	return n.abstractDir != nil || n.mode&fs.ModeDir != 0
}

func (n *imageNode) isSymlink() bool {
	return n.abstractLink != nil || n.mode&fs.ModeSymlink != 0
}

func (f *imageHostFile) Stat() (uint64, fs.FileMode) { return f.size, f.mode }
func (f *imageHostFile) ModTime() time.Time          { return f.modTime }
func (f *imageHostFile) Owner() (uint32, uint32)     { return f.uid, f.gid }
func (f *imageHostFile) RDev() uint32                { return f.rdev }
func (f *imageHostFile) ReadAt(off uint64, size uint32) ([]byte, error) {
	file, err := os.Open(f.hostPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	buf := make([]byte, size)
	n, err := file.ReadAt(buf, int64(off))
	if err != nil && err != io.EOF {
		return nil, err
	}
	return buf[:n], nil
}

func (d *imageHostDir) Stat() fs.FileMode       { return d.mode & linuxPermMask }
func (d *imageHostDir) ModTime() time.Time      { return d.modTime }
func (d *imageHostDir) Owner() (uint32, uint32) { return d.uid, d.gid }
func (d *imageHostDir) RDev() uint32            { return d.rdev }
func (d *imageHostDir) ReadDir() ([]imageDirEnt, error) {
	entries, err := os.ReadDir(d.hostPath)
	if err != nil {
		return nil, err
	}
	out := make([]imageDirEnt, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, imageDirEnt{Name: entry.Name(), Mode: info.Mode()})
	}
	return out, nil
}
func (d *imageHostDir) Lookup(name string) (imageAbstractEntry, error) {
	host := filepath.Join(d.hostPath, filepath.FromSlash(name))
	info, err := os.Lstat(host)
	if err != nil {
		return imageAbstractEntry{}, err
	}
	rel, err := filepath.Rel(d.rootPath, host)
	if err != nil {
		return imageAbstractEntry{}, err
	}
	guest := fsmeta.Normalize(rel)
	meta := d.meta[guest]
	mode := linuxModeToGo(fsmeta.NormalizeLinuxMode(meta.Mode, info.Mode()))
	modTime := info.ModTime()
	switch {
	case info.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(host)
		if err != nil {
			return imageAbstractEntry{}, err
		}
		return imageAbstractEntry{Symlink: &imageHostSymlink{hostPath: host, mode: mode, uid: meta.UID, gid: meta.GID, rdev: meta.RDev, target: target, modTime: modTime}}, nil
	case info.IsDir():
		return imageAbstractEntry{Dir: &imageHostDir{rootPath: d.rootPath, hostPath: host, mode: mode, uid: meta.UID, gid: meta.GID, rdev: meta.RDev, modTime: modTime, meta: d.meta}}, nil
	default:
		return imageAbstractEntry{File: &imageHostFile{hostPath: host, mode: mode, uid: meta.UID, gid: meta.GID, rdev: meta.RDev, size: uint64(info.Size()), modTime: modTime}}, nil
	}
}

func (l *imageHostSymlink) Stat() fs.FileMode       { return l.mode & linuxPermMask }
func (l *imageHostSymlink) ModTime() time.Time      { return l.modTime }
func (l *imageHostSymlink) Target() string          { return l.target }
func (l *imageHostSymlink) Owner() (uint32, uint32) { return l.uid, l.gid }
func (l *imageHostSymlink) RDev() uint32            { return l.rdev }

const (
	linuxSIFMT    = 0o170000
	linuxSIFSOCK  = 0o140000
	linuxSIFLNK   = 0o120000
	linuxSIFREG   = 0o100000
	linuxSIFBLK   = 0o060000
	linuxSIFDIR   = 0o040000
	linuxSIFCHR   = 0o020000
	linuxSIFIFO   = 0o010000
	linuxPermMask = 0o7777
)

const (
	linuxEPERM     int32 = 1
	linuxENOENT    int32 = 2
	linuxENXIO     int32 = 6
	linuxEIO       int32 = 5
	linuxEBADF     int32 = 9
	linuxEEXIST    int32 = 17
	linuxENOTDIR   int32 = 20
	linuxEISDIR    int32 = 21
	linuxEINVAL    int32 = 22
	linuxERANGE    int32 = 34
	linuxENOSYS    int32 = 38
	linuxENOTEMPTY int32 = 39
	linuxENODATA   int32 = 61
	linuxETIMEDOUT int32 = 110
)

func goModeToLinux(mode fs.FileMode) fs.FileMode {
	perm := mode.Perm()
	if mode&fs.ModeSetuid != 0 {
		perm |= 0o4000
	}
	if mode&fs.ModeSetgid != 0 {
		perm |= 0o2000
	}
	if mode&fs.ModeSticky != 0 {
		perm |= 0o1000
	}
	switch {
	case mode&fs.ModeDir != 0:
		perm |= fs.FileMode(linuxSIFDIR)
	case mode&fs.ModeSymlink != 0:
		perm |= fs.FileMode(linuxSIFLNK)
	case mode&fs.ModeNamedPipe != 0:
		perm |= fs.FileMode(linuxSIFIFO)
	case mode&fs.ModeDevice != 0 && mode&fs.ModeCharDevice != 0:
		perm |= fs.FileMode(linuxSIFCHR)
	case mode&fs.ModeDevice != 0:
		perm |= fs.FileMode(linuxSIFBLK)
	case mode&fs.ModeSocket != 0:
		perm |= fs.FileMode(linuxSIFSOCK)
	default:
		perm |= fs.FileMode(linuxSIFREG)
	}
	return perm
}

func linuxModeBits(mode fs.FileMode) uint32 {
	return uint32(mode & fs.FileMode(linuxPermMask))
}

func linuxModeToGo(mode uint32) fs.FileMode {
	perm := fs.FileMode(mode & linuxPermMask)
	switch mode & linuxSIFMT {
	case linuxSIFDIR:
		perm |= fs.ModeDir
	case linuxSIFLNK:
		perm |= fs.ModeSymlink
	case linuxSIFIFO:
		perm |= fs.ModeNamedPipe
	case linuxSIFCHR:
		perm |= fs.ModeDevice | fs.ModeCharDevice
	case linuxSIFBLK:
		perm |= fs.ModeDevice
	case linuxSIFSOCK:
		perm |= fs.ModeSocket
	}
	return perm
}

func maxU32(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}

func dirTypeForMode(mode os.FileMode) uint32 {
	switch {
	case mode&os.ModeDir != 0:
		return dirTypeDir
	case mode&os.ModeSymlink != 0:
		return dirTypeLink
	case mode&os.ModeDevice != 0 && mode&os.ModeCharDevice != 0:
		return dirTypeChar
	case mode&os.ModeDevice != 0:
		return dirTypeBlock
	case mode&os.ModeNamedPipe != 0:
		return dirTypeFIFO
	case mode&os.ModeSocket != 0:
		return dirTypeSocket
	default:
		return dirTypeFile
	}
}

func errnoFromError(err error) int32 {
	var pathErr *os.PathError
	if os.IsNotExist(err) {
		return -linuxENOENT
	}
	if os.IsPermission(err) {
		return -linuxEPERM
	}
	if os.IsExist(err) {
		return -linuxEEXIST
	}
	if os.IsTimeout(err) {
		return -linuxETIMEDOUT
	}
	if strings.Contains(err.Error(), "is a directory") {
		return -linuxEISDIR
	}
	if strings.Contains(err.Error(), "not a directory") {
		return -linuxENOTDIR
	}
	if ok := errorAs(err, &pathErr); ok {
		if errno, ok := pathErr.Err.(syscall.Errno); ok {
			return -mapHostErrno(errno)
		}
	}
	if errno, ok := err.(syscall.Errno); ok {
		return -mapHostErrno(errno)
	}
	return -linuxEIO
}

func mapHostErrno(errno syscall.Errno) int32 {
	switch errno {
	case syscall.ENOENT:
		return linuxENOENT
	case syscall.EPERM:
		return linuxEPERM
	case syscall.EEXIST:
		return linuxEEXIST
	case syscall.ETIMEDOUT:
		return linuxETIMEDOUT
	case syscall.EISDIR:
		return linuxEISDIR
	case syscall.ENOTDIR:
		return linuxENOTDIR
	case syscall.EINVAL:
		return linuxEINVAL
	case syscall.EBADF:
		return linuxEBADF
	case syscall.ENXIO:
		return linuxENXIO
	case syscall.EIO:
		return linuxEIO
	case syscall.ERANGE:
		return linuxERANGE
	case syscall.ENODATA:
		return linuxENODATA
	case syscall.ENOSYS:
		return linuxENOSYS
	}
	return int32(errno)
}

func errorAs(err error, target any) bool {
	switch t := target.(type) {
	case **os.PathError:
		if v, ok := err.(*os.PathError); ok {
			*t = v
			return true
		}
	}
	return false
}

func align8(n int) int {
	return (n + 7) &^ 7
}
