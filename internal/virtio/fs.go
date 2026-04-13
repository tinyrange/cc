package virtio

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"j5.nz/cc/internal/fdt"
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
	fuseOpen       = 14
	fuseRead       = 15
	fuseStatfs     = 17
	fuseRelease    = 18
	fuseInit       = 26
	fuseOpenDir    = 27
	fuseReadDir    = 28
	fuseReleaseDir = 29
	fuseDestroy    = 38
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
	Base uint64
	Size uint64
	IRQ  uint32
	Log  io.Writer

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
		fs.backend = NewPassthroughFS("")
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
	availIdx := binary.LittleEndian.Uint16(header[2:4])
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
		}
		q.lastAvailIdx++
	}
	if qidx == fsQueueRequest || qidx == fsQueueHiprio {
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
		childID, attr, errno := f.backend.Lookup(nodeID, path.Clean(name))
		if errno != 0 {
			return reply(errno, nil), nil
		}
		extra := make([]byte, fuseEntryOutSize)
		binary.LittleEndian.PutUint64(extra[0:8], childID)
		binary.LittleEndian.PutUint64(extra[16:24], 1)
		binary.LittleEndian.PutUint64(extra[24:32], 1)
		encodeFuseAttr(extra[40:], attr)
		return reply(0, extra), nil
	case fuseOpen:
		if len(req) < fuseInHeaderSize+8 {
			return nil, fmt.Errorf("virtio-fs OPEN too short")
		}
		flags := binary.LittleEndian.Uint32(req[40:44])
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
		data, errno := f.backend.Read(nodeID, fh, off, size)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		return reply(0, data), nil
	case fuseRelease:
		if len(req) < fuseInHeaderSize+24 {
			return nil, fmt.Errorf("virtio-fs RELEASE too short")
		}
		f.backend.Release(nodeID, binary.LittleEndian.Uint64(req[40:48]))
		return reply(0, nil), nil
	case fuseOpenDir:
		if len(req) < fuseInHeaderSize+8 {
			return nil, fmt.Errorf("virtio-fs OPENDIR too short")
		}
		flags := binary.LittleEndian.Uint32(req[40:44])
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
		data, errno := f.backend.ReadDir(nodeID, fh, off, size)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		return reply(0, data), nil
	case fuseReleaseDir:
		if len(req) < fuseInHeaderSize+24 {
			return nil, fmt.Errorf("virtio-fs RELEASEDIR too short")
		}
		f.backend.ReleaseDir(nodeID, binary.LittleEndian.Uint64(req[40:48]))
		return reply(0, nil), nil
	case fuseReadlink:
		target, errno := f.backend.Readlink(nodeID)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		return reply(0, []byte(target)), nil
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
	case fuseDestroy:
		return reply(0, nil), nil
	default:
		return reply(-int32(syscall.ENOSYS), nil), nil
	}
}

func (f *FS) logf(format string, args ...any) {
	if f.Log == nil {
		return
	}
	_, _ = fmt.Fprintf(f.Log, format+"\n", args...)
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

	mu         sync.Mutex
	nextNodeID uint64
	nextHandle uint64
	nodes      map[uint64]string
	pathToNode map[string]uint64
	files      map[uint64]*os.File
	dirHandles map[uint64][]dirEntry
}

type dirEntry struct {
	name string
	typ  uint32
	ino  uint64
}

func NewPassthroughFS(root string) FSBackend {
	fs := &passthroughFS{
		root:       root,
		nextNodeID: 2,
		nextHandle: 1,
		nodes:      map[uint64]string{1: "/"},
		pathToNode: map[string]uint64{"/": 1},
		files:      map[uint64]*os.File{},
		dirHandles: map[uint64][]dirEntry{},
	}
	return fs
}

func (p *passthroughFS) Init() (uint32, uint32) { return 128 << 10, 0 }

func (p *passthroughFS) GetAttr(nodeID uint64) (FuseAttr, int32) {
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
	nodeID := p.ensureNode(guestPath)
	return nodeID, p.fileAttr(nodeID, info), 0
}

func (p *passthroughFS) Open(nodeID uint64, _ uint32) (uint64, int32) {
	host, errno := p.hostPath(nodeID)
	if errno != 0 {
		return 0, errno
	}
	f, err := os.Open(host)
	if err != nil {
		return 0, errnoFromError(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	handle := p.nextHandle
	p.nextHandle++
	p.files[handle] = f
	return handle, 0
}

func (p *passthroughFS) Release(_ uint64, fh uint64) {
	p.mu.Lock()
	f := p.files[fh]
	delete(p.files, fh)
	p.mu.Unlock()
	if f != nil {
		_ = f.Close()
	}
}

func (p *passthroughFS) Read(nodeID uint64, fh uint64, off uint64, size uint32) ([]byte, int32) {
	p.mu.Lock()
	f := p.files[fh]
	p.mu.Unlock()
	if f == nil {
		host, errno := p.hostPath(nodeID)
		if errno != 0 {
			return nil, errno
		}
		var err error
		f, err = os.Open(host)
		if err != nil {
			return nil, errnoFromError(err)
		}
		defer f.Close()
	}
	buf := make([]byte, size)
	n, err := f.ReadAt(buf, int64(off))
	if err != nil && err != io.EOF {
		return nil, errnoFromError(err)
	}
	return buf[:n], 0
}

func (p *passthroughFS) OpenDir(nodeID uint64, _ uint32) (uint64, int32) {
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
		return nil, -int32(syscall.EBADF)
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
		return "", -int32(syscall.ENOENT)
	}
	if p.root == "" {
		return "", -int32(syscall.ENOENT)
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
	attr := FuseAttr{
		Ino:     nodeID,
		Size:    uint64(info.Size()),
		Mode:    uint32(info.Mode()),
		NLink:   1,
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
		attr.Mode = uint32(st.Mode)
		attr.NLink = uint32(st.Nlink)
		attr.UID = uint32(st.Uid)
		attr.GID = uint32(st.Gid)
		attr.RDev = uint32(st.Rdev)
		attr.BlkSize = uint32(st.Blksize)
		attr.ATimeSec = uint64(st.Atimespec.Sec)
		attr.MTimeSec = uint64(st.Mtimespec.Sec)
		attr.CTimeSec = uint64(st.Ctimespec.Sec)
		attr.ATimeNsec = uint32(st.Atimespec.Nsec)
		attr.MTimeNsec = uint32(st.Mtimespec.Nsec)
		attr.CTimeNsec = uint32(st.Ctimespec.Nsec)
	}
	return attr
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
		return -int32(syscall.ENOENT)
	}
	if os.IsPermission(err) {
		return -int32(syscall.EPERM)
	}
	if os.IsExist(err) {
		return -int32(syscall.EEXIST)
	}
	if os.IsTimeout(err) {
		return -int32(syscall.ETIMEDOUT)
	}
	if strings.Contains(err.Error(), "is a directory") {
		return -int32(syscall.EISDIR)
	}
	if strings.Contains(err.Error(), "not a directory") {
		return -int32(syscall.ENOTDIR)
	}
	if ok := errorAs(err, &pathErr); ok {
		if errno, ok := pathErr.Err.(syscall.Errno); ok {
			return -int32(errno)
		}
	}
	if errno, ok := err.(syscall.Errno); ok {
		return -int32(errno)
	}
	return -int32(syscall.EIO)
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
