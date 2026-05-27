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
	"sync/atomic"
	"time"

	"j5.nz/cc/internal/fdt"
	"j5.nz/cc/internal/fsmeta"
	"j5.nz/cc/internal/imagefs"
	"j5.nz/cc/internal/linuxabi"
)

const (
	mmioDeviceIDFS = 26

	fsQueueHiprio       = 0
	fsControlQueueCount = 1
	fsQueueRequest      = fsQueueHiprio + fsControlQueueCount
	fsRequestQueueCount = 4
	fsQueueCount        = fsControlQueueCount + fsRequestQueueCount

	fsCfgTagSize        = 36
	fsCfgNumQueueOff    = fsCfgTagSize
	fsCfgTotalSize      = fsCfgTagSize + 4
	fsInterruptVring    = 0x1
	featureRingEventIdx = uint64(1) << 29
	fuseInHeaderSize    = 40
	fuseOutHeaderSize   = 16
	fuseAttrSize        = 88
	fuseEntryOutSize    = 40 + fuseAttrSize
	fuseAttrOutSize     = 16 + fuseAttrSize
	fuseOpenOutSize     = 16
	fuseInitOutSize     = 40
	fuseStatfsOutSize   = 80
	fuseStatxOutSize    = 288
	fuseDirentBaseSize  = 24
	fuseWriteOutSize    = 8
	fuseLKInSize        = 48
	fuseLKOutSize       = 24
	linuxFUnlck         = 2
)

const (
	fuseLookup     = 1
	fuseForget     = 2
	fuseGetAttr    = 3
	fuseSetAttr    = 4
	fuseReadlink   = 5
	fuseSymlink    = 6
	fuseMknod      = 8
	fuseMkdir      = 9
	fuseUnlink     = 10
	fuseRmDir      = 11
	fuseRename     = 12
	fuseOpen       = 14
	fuseRead       = 15
	fuseWrite      = 16
	fuseStatfs     = 17
	fuseRelease    = 18
	fuseFsync      = 20
	fuseGetXattr   = 22
	fuseListXattr  = 23
	fuseFlush      = 25
	fuseInit       = 26
	fuseOpenDir    = 27
	fuseReadDir    = 28
	fuseReleaseDir = 29
	fuseFsyncDir   = 30
	fuseGetLK      = 31
	fuseSetLK      = 32
	fuseSetLKW     = 33
	fuseAccess     = 34
	fuseCreate     = 35
	fuseDestroy    = 38
	fuseIoctl      = 39
	fusePoll       = 40
	fuseLseek      = 46
	fuseSyncFS     = 50
	fuseTmpfile    = 51
	fuseStatx      = 52
)

const (
	fattrMode  = 1 << 0
	fattrUID   = 1 << 1
	fattrGID   = 1 << 2
	fattrSize  = 1 << 3
	fattrATime = 1 << 4
	fattrMTime = 1 << 5
	fattrFH    = 1 << 6
)

const (
	linuxOACCMODE = 3
	linuxORDONLY  = 0
	linuxOWRONLY  = 1
	linuxORDWR    = 2
	linuxOCREAT   = 0x40
	linuxOEXCL    = 0x80
	linuxOTRUNC   = 0x200
	linuxOAPPEND  = 0x400
)

const (
	fuseCapPosixLocks     = 1 << 1
	fuseCapBigWrites      = 1 << 5
	fuseCapWritebackCache = 1 << 16
	fuseCapMaxPages       = 1 << 22
)

const (
	fuseOpenKeepCache = 1 << 1
	fuseOpenCacheDir  = 1 << 3
	fuseOpenNoFlush   = 1 << 5
)

const (
	fsCacheStrict     = "strict"
	fsCacheNormal     = "normal"
	fsCacheAggressive = "aggressive"
)

const (
	statxBasicStats = 0x000007ff
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

type fsFsyncBackend interface {
	Fsync(nodeID uint64, fh uint64, flags uint32) int32
}

type fsFsyncDirBackend interface {
	FsyncDir(nodeID uint64, fh uint64, flags uint32) int32
}

type fsLseekBackend interface {
	Lseek(nodeID uint64, fh uint64, offset uint64, whence uint32) (uint64, int32)
}

type fsMkdirBackend interface {
	Mkdir(parent uint64, name string, mode uint32, uid uint32, gid uint32) (nodeID uint64, attr FuseAttr, errno int32)
}

type fsMknodBackend interface {
	Mknod(parent uint64, name string, mode uint32, rdev uint32, uid uint32, gid uint32) (nodeID uint64, attr FuseAttr, errno int32)
}

type fsSymlinkBackend interface {
	Symlink(parent uint64, name string, target string, uid uint32, gid uint32) (nodeID uint64, attr FuseAttr, errno int32)
}

type fsRmDirBackend interface {
	RmDir(parent uint64, name string) int32
}

type fsCreateBackend interface {
	Create(parent uint64, name string, flags uint32, mode uint32, uid uint32, gid uint32) (nodeID uint64, fh uint64, attr FuseAttr, errno int32)
}

type fsWriteBackend interface {
	Write(nodeID uint64, fh uint64, off uint64, data []byte, flags uint32) (uint32, int32)
}

type fsSetAttrBackend interface {
	SetAttr(nodeID uint64, valid uint32, fh uint64, size uint64, mode uint32, uid uint32, gid uint32, atime time.Time, mtime time.Time) (FuseAttr, int32)
}

type fsUnlinkBackend interface {
	Unlink(parent uint64, name string) int32
}

type fsRenameBackend interface {
	Rename(parent uint64, name string, newParent uint64, newName string, flags uint32) int32
}

type fsWritebackCacheBackend interface {
	SetWritebackCache(enabled bool)
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
	Base           uint64
	Size           uint64
	IRQ            uint32
	Log            io.Writer
	Strict         bool
	Async          bool
	RecordTiming   func(name string, duration time.Duration)
	cacheMode      string
	writebackCache bool
	entryTTL       time.Duration
	attrTTL        time.Duration

	mu               sync.Mutex
	workerOnce       sync.Once
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
	queues           []queue
	mmioReads        uint64
	mmioWrites       uint64
	queueNotifies    []uint64
	fuseRequests     atomic.Uint64
	interruptRaises  uint64
	irqTransitions   uint64
	workCh           chan fsWork
	nextWorkSeq      []uint64
	nextCompleteSeq  []uint64
	completions      map[fsCompletionKey]fsCompletion
	fuseOpStats      [fuseStatsSlots]fuseOpStat
	stageStats       [fsStageCount]timingStat
}

const fuseStatsSlots = 64
const fsStageCount = 4
const fsWorkerCount = 1
const fsInlineRespDescs = 32
const fsPooledReqSize = 4096

const (
	fsStageQueueHarvest = iota
	fsStageInlineDispatch
	fsStageInlineComplete
	fsStageAsyncComplete
)

type fsWork struct {
	qidx              int
	head              uint16
	seq               uint64
	generation        uint32
	unique            uint64
	req               []byte
	pooledReq         bool
	suppressInterrupt bool
	respCount         int
	respDescs         [fsInlineRespDescs]fsDesc
	respExtra         []fsDesc
}

type fsCompletionKey struct {
	qidx int
	seq  uint64
}

type fsCompletion struct {
	work  fsWork
	reply []byte
	err   error
}

type fsInlineCompletion struct {
	work  fsWork
	reply []byte
	err   error
}

var fsReqPool = sync.Pool{
	New: func() any {
		return make([]byte, fsPooledReqSize)
	},
}

type FSStats struct {
	Tag             string        `json:"tag"`
	MMIOReads       uint64        `json:"mmio_reads"`
	MMIOWrites      uint64        `json:"mmio_writes"`
	QueueNotifies   []uint64      `json:"queue_notifies"`
	FUSERequests    uint64        `json:"fuse_requests"`
	InterruptRaises uint64        `json:"interrupt_raises"`
	IRQTransitions  uint64        `json:"irq_transitions"`
	IRQHigh         bool          `json:"irq_high"`
	InterruptStatus uint32        `json:"interrupt_status"`
	QueueReady      []bool        `json:"queue_ready"`
	QueueLastAvail  []uint16      `json:"queue_last_avail"`
	FUSEOps         []FUSEOpStats `json:"fuse_ops"`
	Stages          []TimingStats `json:"stages"`
}

type FUSEOpStats struct {
	Opcode       uint32 `json:"opcode"`
	Name         string `json:"name"`
	Count        uint64 `json:"count"`
	TotalNanos   int64  `json:"total_nanos"`
	MaxNanos     int64  `json:"max_nanos"`
	AverageNanos int64  `json:"average_nanos"`
}

type fuseOpStat struct {
	timingStat
}

type TimingStats struct {
	Name         string `json:"name"`
	Count        uint64 `json:"count"`
	TotalNanos   int64  `json:"total_nanos"`
	MaxNanos     int64  `json:"max_nanos"`
	AverageNanos int64  `json:"average_nanos"`
}

type timingStat struct {
	count      atomic.Uint64
	totalNanos atomic.Int64
	maxNanos   atomic.Int64
}

type fsDesc struct {
	addr   uint64
	length uint32
	flags  uint16
	next   uint16
	write  bool
}

func NewFS(base, size uint64, irq uint32, tag string, backend FSBackend) *FS {
	cacheMode, entryTTL, attrTTL := resolveFSCachePolicy()
	fs := &FS{
		Base:           base,
		Size:           size,
		IRQ:            irq,
		backend:        backend,
		Async:          strings.TrimSpace(os.Getenv("CCX3_VIRTIOFS_ASYNC")) != "",
		cacheMode:      cacheMode,
		writebackCache: strings.TrimSpace(os.Getenv("CCX3_VIRTIOFS_WRITEBACK")) != "",
		entryTTL:       entryTTL,
		attrTTL:        attrTTL,
		workCh:         make(chan fsWork, 1024),
		completions:    make(map[fsCompletionKey]fsCompletion),
	}
	fs.resetQueueStateLocked()
	if fs.backend == nil {
		fs.backend = NewPassthroughFS("", nil)
	}
	if be, ok := fs.backend.(fsWritebackCacheBackend); ok {
		be.SetWritebackCache(fs.writebackCache)
	}
	copy(fs.tag[:], []byte(tag))
	fs.resetLocked()
	return fs
}

func resolveFSCachePolicy() (string, time.Duration, time.Duration) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CCX3_VIRTIOFS_CACHE"))) {
	case fsCacheStrict:
		return fsCacheStrict, 0, 0
	case fsCacheAggressive:
		return fsCacheAggressive, 60 * time.Second, 60 * time.Second
	default:
		return fsCacheNormal, time.Second, time.Second
	}
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
	f.mmioReads++

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
			return truncateValue(featureRingEventIdx, size), nil
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
	f.mmioWrites++

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
			} else if f.driverFeatures&featureRingEventIdx != 0 {
				if err := f.writeAvailEventLocked(q); err != nil {
					f.mu.Unlock()
					return err
				}
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
		err := f.updateIRQLocked()
		f.mu.Unlock()
		return err
	case regStatus:
		f.status = uint32(value)
		if f.status == 0 {
			f.resetLocked()
		}
	case regQueueNotify:
		if int(value) < len(f.queues) {
			f.queueNotifies[int(value)]++
			harvestStart := time.Now()
			works, err := f.processQueueAsyncLocked(int(value))
			f.recordStageDuration(fsStageQueueHarvest, time.Since(harvestStart))
			if err != nil {
				f.mu.Unlock()
				return err
			}
			f.mu.Unlock()
			if f.Async {
				f.enqueueWorks(works)
				return nil
			}
			return f.processWorksInline(works)
		}
	}
	f.mu.Unlock()
	return nil
}

func (f *FS) processQueueLocked(qidx int) error {
	q := &f.queues[qidx]
	if !q.ready || q.size == 0 || f.mem == nil {
		return nil
	}

	var header [4]byte
	if err := f.readIPAInto(q.availAddr, header[:]); err != nil {
		return err
	}
	availFlags := binary.LittleEndian.Uint16(header[0:2])
	availIdx := binary.LittleEndian.Uint16(header[2:4])
	oldUsedIdx := q.usedIdx
	interruptNeeded := false
	for q.lastAvailIdx != availIdx {
		slot := q.lastAvailIdx % q.size
		var entry [2]byte
		if err := f.readIPAInto(q.availAddr+4+uint64(slot)*2, entry[:]); err != nil {
			return err
		}
		head := binary.LittleEndian.Uint16(entry[:])
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
	if f.driverFeatures&featureRingEventIdx != 0 {
		if err := f.writeAvailEventLocked(q); err != nil {
			return err
		}
	}
	if interruptNeeded && f.isCompletingQueue(qidx) && f.shouldInterruptLocked(q, oldUsedIdx, q.usedIdx, availFlags) {
		f.interruptStatus |= fsInterruptVring
		f.interruptRaises++
		f.logf("interrupt-raise status=%#x", f.interruptStatus)
		return f.updateIRQLocked()
	}
	return nil
}

func (f *FS) processQueueAsyncLocked(qidx int) ([]fsWork, error) {
	q := &f.queues[qidx]
	if !q.ready || q.size == 0 || f.mem == nil {
		return nil, nil
	}

	var header [4]byte
	if err := f.readIPAInto(q.availAddr, header[:]); err != nil {
		return nil, err
	}
	suppressInterrupt := binary.LittleEndian.Uint16(header[0:2])&1 != 0
	availIdx := binary.LittleEndian.Uint16(header[2:4])
	var works []fsWork
	for q.lastAvailIdx != availIdx {
		slot := q.lastAvailIdx % q.size
		var entry [2]byte
		if err := f.readIPAInto(q.availAddr+4+uint64(slot)*2, entry[:]); err != nil {
			return nil, err
		}
		head := binary.LittleEndian.Uint16(entry[:])
		f.logf("queue-notify q=%d head=%d", qidx, head)
		work, err := f.prepareRequestLocked(qidx, q, head, suppressInterrupt)
		if err != nil {
			return nil, err
		}
		works = append(works, work)
		q.lastAvailIdx++
	}
	if f.driverFeatures&featureRingEventIdx != 0 {
		if err := f.writeAvailEventLocked(q); err != nil {
			return nil, err
		}
	}
	return works, nil
}

func (f *FS) handleRequestLocked(q *queue, head uint16) (uint32, bool, error) {
	var descScratch [8]fsDesc
	descs, err := f.readDescriptorChainLocked(q, head, descScratch[:0])
	if err != nil {
		return 0, false, err
	}
	var reqScratch [4]fsDesc
	var respScratch [4]fsDesc
	reqDescs := reqScratch[:0]
	respDescs := respScratch[:0]
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
	var reqStack [4096]byte
	var req []byte
	if reqLen <= len(reqStack) {
		req = reqStack[:reqLen]
	} else {
		req = make([]byte, reqLen)
	}
	reqOff := 0
	for _, d := range reqDescs {
		if err := f.readIPAInto(d.addr, req[reqOff:reqOff+int(d.length)]); err != nil {
			return 0, false, err
		}
		reqOff += int(d.length)
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

func (f *FS) prepareRequestLocked(qidx int, q *queue, head uint16, suppressInterrupt bool) (fsWork, error) {
	seq := f.nextWorkSeq[qidx]
	f.nextWorkSeq[qidx]++
	work := fsWork{qidx: qidx, head: head, seq: seq, generation: f.configGeneration, suppressInterrupt: suppressInterrupt}
	var descScratch [8]fsDesc
	descs, err := f.readDescriptorChainLocked(q, head, descScratch[:0])
	if err != nil {
		return work, err
	}
	var reqScratch [4]fsDesc
	var respScratch [4]fsDesc
	reqDescs := reqScratch[:0]
	respDescs := respScratch[:0]
	for _, d := range descs {
		if d.write {
			respDescs = append(respDescs, d)
			continue
		}
		if len(respDescs) != 0 {
			return work, fmt.Errorf("virtio-fs descriptor order invalid")
		}
		reqDescs = append(reqDescs, d)
	}
	if len(reqDescs) == 0 {
		return work, fmt.Errorf("virtio-fs missing request descriptors")
	}
	reqLen := 0
	for _, d := range reqDescs {
		reqLen += int(d.length)
	}
	req, pooledReq := takeFSReqBuffer(reqLen)
	reqOff := 0
	for _, d := range reqDescs {
		if err := f.readIPAInto(d.addr, req[reqOff:reqOff+int(d.length)]); err != nil {
			putFSReqBuffer(req, pooledReq)
			return work, err
		}
		reqOff += int(d.length)
	}
	if len(req) >= 16 {
		work.unique = binary.LittleEndian.Uint64(req[8:16])
	}
	work.req = req
	work.pooledReq = pooledReq
	work.respCount = len(respDescs)
	if len(respDescs) <= len(work.respDescs) {
		copy(work.respDescs[:], respDescs)
	} else {
		work.respExtra = append([]fsDesc(nil), respDescs...)
	}
	return work, nil
}

func (f *FS) enqueueWorks(works []fsWork) {
	if len(works) == 0 {
		return
	}
	f.workerOnce.Do(f.startWorkers)
	for _, work := range works {
		f.workCh <- work
	}
}

func (f *FS) processWorksInline(works []fsWork) error {
	if len(works) == 0 {
		return nil
	}
	completions := make([]fsInlineCompletion, 0, len(works))
	dispatchStart := time.Now()
	for _, work := range works {
		reply, err := f.dispatchFUSE(work.req)
		putFSReqBuffer(work.req, work.pooledReq)
		work.req = nil
		work.pooledReq = false
		if err != nil {
			return err
		}
		completions = append(completions, fsInlineCompletion{work: work, reply: reply})
	}
	f.recordStageDuration(fsStageInlineDispatch, time.Since(dispatchStart))
	return f.completeWorksInline(completions)
}

func (f *FS) startWorkers() {
	for i := 0; i < fsWorkerCount; i++ {
		go f.runWorker()
	}
}

func (f *FS) runWorker() {
	for work := range f.workCh {
		reply, err := f.dispatchFUSE(work.req)
		putFSReqBuffer(work.req, work.pooledReq)
		work.req = nil
		work.pooledReq = false
		if err != nil {
			f.logf("async-fuse-error q=%d head=%d: %v", work.qidx, work.head, err)
			reply = fuseReply(work.unique, -linuxEIO, nil)
			err = nil
		}
		if err := f.completeWork(work, reply, err); err != nil {
			f.logf("async-complete-error q=%d head=%d: %v", work.qidx, work.head, err)
		}
	}
}

func (f *FS) completeWork(work fsWork, reply []byte, workErr error) error {
	defer f.recordStageTiming(fsStageAsyncComplete, time.Now())
	f.mu.Lock()
	defer f.mu.Unlock()
	if work.generation != f.configGeneration || work.qidx < 0 || work.qidx >= len(f.queues) {
		return nil
	}
	if f.completions == nil {
		f.completions = make(map[fsCompletionKey]fsCompletion)
	}
	f.completions[fsCompletionKey{qidx: work.qidx, seq: work.seq}] = fsCompletion{work: work, reply: reply, err: workErr}
	return f.drainCompletionsLocked(work.qidx)
}

func (f *FS) completeWorksInline(completions []fsInlineCompletion) error {
	if len(completions) == 0 {
		return nil
	}
	defer f.recordStageTiming(fsStageInlineComplete, time.Now())
	f.mu.Lock()
	defer f.mu.Unlock()
	for index := 0; index < len(completions); {
		qidx := completions[index].work.qidx
		if qidx < 0 || qidx >= len(f.queues) {
			index++
			continue
		}
		q := &f.queues[qidx]
		oldUsedIdx := q.usedIdx
		wroteCompletion := false
		interruptSuppressed := true
		for index < len(completions) && completions[index].work.qidx == qidx {
			completion := completions[index]
			index++
			if completion.work.generation != f.configGeneration {
				continue
			}
			if completion.err != nil {
				return completion.err
			}
			if completion.reply == nil {
				continue
			}
			if err := f.writeCompletionUsedLocked(q, completion.work, completion.reply); err != nil {
				return err
			}
			wroteCompletion = true
			if !completion.work.suppressInterrupt {
				interruptSuppressed = false
			}
		}
		if !wroteCompletion || !f.isCompletingQueue(qidx) {
			continue
		}
		shouldInterrupt := false
		if f.driverFeatures&featureRingEventIdx != 0 {
			shouldInterrupt = f.shouldInterruptLocked(q, oldUsedIdx, q.usedIdx, 0)
		} else {
			shouldInterrupt = !interruptSuppressed
		}
		if shouldInterrupt {
			f.interruptStatus |= fsInterruptVring
			f.interruptRaises++
			f.logf("interrupt-raise status=%#x", f.interruptStatus)
			if err := f.updateIRQLocked(); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *FS) drainCompletionsLocked(qidx int) error {
	q := &f.queues[qidx]
	for {
		seq := f.nextCompleteSeq[qidx]
		key := fsCompletionKey{qidx: qidx, seq: seq}
		completion, ok := f.completions[key]
		if !ok {
			return nil
		}
		delete(f.completions, key)
		f.nextCompleteSeq[qidx]++
		if completion.err != nil {
			return completion.err
		}
		if completion.reply == nil {
			continue
		}
		if err := f.writeCompletionLocked(q, completion.work, completion.reply); err != nil {
			return err
		}
	}
}

func (f *FS) writeCompletionLocked(q *queue, work fsWork, reply []byte) error {
	if err := f.writeCompletionUsedLocked(q, work, reply); err != nil {
		return err
	}
	if (f.driverFeatures&featureRingEventIdx != 0 || !work.suppressInterrupt) && f.isCompletingQueue(work.qidx) {
		if f.driverFeatures&featureRingEventIdx != 0 && !f.shouldInterruptLocked(q, q.usedIdx-1, q.usedIdx, 0) {
			return nil
		}
		f.interruptStatus |= fsInterruptVring
		f.interruptRaises++
		f.logf("interrupt-raise status=%#x", f.interruptStatus)
		return f.updateIRQLocked()
	}
	return nil
}

func (f *FS) writeCompletionUsedLocked(q *queue, work fsWork, reply []byte) error {
	offset := 0
	for i := 0; i < work.respCount; i++ {
		d := work.responseDesc(i)
		if offset >= len(reply) {
			break
		}
		chunk := len(reply) - offset
		if chunk > int(d.length) {
			chunk = int(d.length)
		}
		if err := f.mem.WriteIPA(d.addr, reply[offset:offset+chunk]); err != nil {
			return err
		}
		offset += chunk
	}
	if offset < len(reply) {
		return fmt.Errorf("virtio-fs response truncated: need %d have %d", len(reply), offset)
	}
	if err := f.writeUsedLocked(q, work.head, uint32(len(reply))); err != nil {
		return err
	}
	f.logf("used-ring q=%d head=%d len=%d", work.qidx, work.head, len(reply))
	return nil
}

func (w *fsWork) responseDesc(index int) fsDesc {
	if w.respExtra != nil {
		return w.respExtra[index]
	}
	return w.respDescs[index]
}

func (f *FS) dispatchFUSELocked(req []byte) ([]byte, error) {
	return f.dispatchFUSE(req)
}

func (f *FS) dispatchFUSE(req []byte) ([]byte, error) {
	if len(req) < fuseInHeaderSize {
		return nil, fmt.Errorf("virtio-fs short request: %d", len(req))
	}
	opcode := binary.LittleEndian.Uint32(req[4:8])
	unique := binary.LittleEndian.Uint64(req[8:16])
	nodeID := binary.LittleEndian.Uint64(req[16:24])
	callerUID := binary.LittleEndian.Uint32(req[24:28])
	callerGID := binary.LittleEndian.Uint32(req[28:32])
	opStart := time.Now()
	defer f.recordFUSEDispatchTiming(opcode, opStart)
	f.logf("opcode=%d unique=%d node=%d", opcode, unique, nodeID)

	reply := func(errno int32, extra []byte) []byte {
		return fuseReply(unique, errno, extra)
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
		if maxWrite > 4096 {
			flags |= fuseCapBigWrites | fuseCapMaxPages
		}
		if f.writebackCache {
			flags |= fuseCapWritebackCache
		}
		maxPages := (maxWrite + 4095) / 4096
		if maxPages == 0 {
			maxPages = 1
		}
		if maxPages > 0xffff {
			maxPages = 0xffff
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
		maxBackground := uint16(16)
		congestionThreshold := uint16(32)
		if f.writebackCache {
			maxBackground = 256
			congestionThreshold = 192
		}
		binary.LittleEndian.PutUint16(extra[16:18], maxBackground)
		binary.LittleEndian.PutUint16(extra[18:20], congestionThreshold)
		binary.LittleEndian.PutUint32(extra[20:24], maxWrite)
		binary.LittleEndian.PutUint32(extra[24:28], 1)
		binary.LittleEndian.PutUint16(extra[28:30], uint16(maxPages))
		f.logf("init-reply major=%d minor=%d max_write=%d", replyMajor, replyMinor, maxWrite)
		return reply(0, extra), nil
	case fuseGetAttr:
		f.logPathf("getattr", nodeID, "")
		attr, errno := f.backend.GetAttr(nodeID)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		extra := make([]byte, fuseAttrOutSize)
		encodeFuseAttrTTL(extra[0:16], f.attrTTL)
		encodeFuseAttr(extra[16:], attr)
		return reply(0, extra), nil
	case fuseSetAttr:
		if len(req) < fuseInHeaderSize+88 {
			return nil, fmt.Errorf("virtio-fs SETATTR too short")
		}
		if be, ok := f.backend.(fsSetAttrBackend); ok {
			valid := binary.LittleEndian.Uint32(req[40:44])
			fh := binary.LittleEndian.Uint64(req[48:56])
			size := binary.LittleEndian.Uint64(req[56:64])
			atime := time.Unix(int64(binary.LittleEndian.Uint64(req[72:80])), int64(binary.LittleEndian.Uint32(req[96:100])))
			mtime := time.Unix(int64(binary.LittleEndian.Uint64(req[80:88])), int64(binary.LittleEndian.Uint32(req[100:104])))
			mode := binary.LittleEndian.Uint32(req[108:112])
			uid := binary.LittleEndian.Uint32(req[116:120])
			gid := binary.LittleEndian.Uint32(req[120:124])
			attr, errno := be.SetAttr(nodeID, valid, fh, size, mode, uid, gid, atime, mtime)
			if errno != 0 {
				return reply(errno, nil), nil
			}
			extra := make([]byte, fuseAttrOutSize)
			encodeFuseAttrTTL(extra[0:16], f.attrTTL)
			encodeFuseAttr(extra[16:], attr)
			return reply(0, extra), nil
		}
		return reply(-linuxENOSYS, nil), nil
	case fuseLookup:
		name := readCStringName(req[fuseInHeaderSize:])
		f.logPathf("lookup-parent", nodeID, fmt.Sprintf(" name=%q", name))
		childID, attr, errno := f.backend.Lookup(nodeID, path.Clean(name))
		if errno != 0 {
			return reply(errno, nil), nil
		}
		f.logPathf("lookup-child", childID, "")
		extra := make([]byte, fuseEntryOutSize)
		f.encodeFuseEntryOut(extra, childID)
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
			childID, attr, errno := be.Mkdir(nodeID, path.Clean(name), mode, callerUID, callerGID)
			if errno != 0 {
				return reply(errno, nil), nil
			}
			extra := make([]byte, fuseEntryOutSize)
			f.encodeFuseEntryOut(extra, childID)
			encodeFuseAttr(extra[40:], attr)
			return reply(0, extra), nil
		}
		return nil, fmt.Errorf("virtio-fs missing mkdir backend for parent=%d name=%q", nodeID, name)
	case fuseMknod:
		if len(req) < fuseInHeaderSize+16 {
			return nil, fmt.Errorf("virtio-fs MKNOD too short")
		}
		name := readCStringName(req[fuseInHeaderSize+16:])
		mode := binary.LittleEndian.Uint32(req[40:44])
		rdev := binary.LittleEndian.Uint32(req[44:48])
		f.logPathf("mknod-parent", nodeID, fmt.Sprintf(" name=%q mode=%#o rdev=%#x", name, mode, rdev))
		if be, ok := f.backend.(fsMknodBackend); ok {
			childID, attr, errno := be.Mknod(nodeID, path.Clean(name), mode, rdev, callerUID, callerGID)
			if errno != 0 {
				return reply(errno, nil), nil
			}
			extra := make([]byte, fuseEntryOutSize)
			f.encodeFuseEntryOut(extra, childID)
			encodeFuseAttr(extra[40:], attr)
			return reply(0, extra), nil
		}
		return reply(-linuxENOSYS, nil), nil
	case fuseSymlink:
		name, target, ok := readTwoCStringNames(req[fuseInHeaderSize:])
		if !ok {
			return nil, fmt.Errorf("virtio-fs SYMLINK malformed payload")
		}
		f.logPathf("symlink-parent", nodeID, fmt.Sprintf(" name=%q target=%q", name, target))
		if be, ok := f.backend.(fsSymlinkBackend); ok {
			childID, attr, errno := be.Symlink(nodeID, path.Clean(name), target, callerUID, callerGID)
			if errno != 0 {
				return reply(errno, nil), nil
			}
			extra := make([]byte, fuseEntryOutSize)
			f.encodeFuseEntryOut(extra, childID)
			encodeFuseAttr(extra[40:], attr)
			return reply(0, extra), nil
		}
		return reply(-linuxENOSYS, nil), nil
	case fuseUnlink:
		name := readCStringName(req[fuseInHeaderSize:])
		if be, ok := f.backend.(fsUnlinkBackend); ok {
			return reply(be.Unlink(nodeID, path.Clean(name)), nil), nil
		}
		return reply(-linuxENOSYS, nil), nil
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
		binary.LittleEndian.PutUint32(extra[8:12], f.openResponseFlags(flags, false))
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
	case fuseWrite:
		if len(req) < fuseInHeaderSize+40 {
			return nil, fmt.Errorf("virtio-fs WRITE too short")
		}
		if be, ok := f.backend.(fsWriteBackend); ok {
			fh := binary.LittleEndian.Uint64(req[40:48])
			off := binary.LittleEndian.Uint64(req[48:56])
			size := binary.LittleEndian.Uint32(req[56:60])
			writeFlags := binary.LittleEndian.Uint32(req[60:64])
			dataStart := fuseInHeaderSize + 40
			if len(req) < dataStart+int(size) {
				return nil, fmt.Errorf("virtio-fs WRITE short payload")
			}
			count, errno := be.Write(nodeID, fh, off, req[dataStart:dataStart+int(size)], writeFlags)
			if errno != 0 {
				return reply(errno, nil), nil
			}
			extra := make([]byte, fuseWriteOutSize)
			binary.LittleEndian.PutUint32(extra[0:4], count)
			return reply(0, extra), nil
		}
		return reply(-linuxENOSYS, nil), nil
	case fuseRelease:
		if len(req) < fuseInHeaderSize+24 {
			return nil, fmt.Errorf("virtio-fs RELEASE too short")
		}
		f.logPathf("release", nodeID, fmt.Sprintf(" fh=%d", binary.LittleEndian.Uint64(req[40:48])))
		f.backend.Release(nodeID, binary.LittleEndian.Uint64(req[40:48]))
		return reply(0, nil), nil
	case fuseFsync:
		if len(req) < fuseInHeaderSize+16 {
			return nil, fmt.Errorf("virtio-fs FSYNC too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		flags := binary.LittleEndian.Uint32(req[48:52])
		f.logPathf("fsync", nodeID, fmt.Sprintf(" fh=%d flags=%#x", fh, flags))
		if be, ok := f.backend.(fsFsyncBackend); ok {
			return reply(be.Fsync(nodeID, fh, flags), nil), nil
		}
		if f.Strict {
			return nil, fmt.Errorf("virtio-fs missing fsync backend for FSYNC node=%d fh=%d", nodeID, fh)
		}
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
		binary.LittleEndian.PutUint32(extra[8:12], f.openResponseFlags(flags, true))
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
	case fuseFsyncDir:
		if len(req) < fuseInHeaderSize+16 {
			return nil, fmt.Errorf("virtio-fs FSYNCDIR too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		flags := binary.LittleEndian.Uint32(req[48:52])
		f.logPathf("fsyncdir", nodeID, fmt.Sprintf(" fh=%d flags=%#x", fh, flags))
		if be, ok := f.backend.(fsFsyncDirBackend); ok {
			return reply(be.FsyncDir(nodeID, fh, flags), nil), nil
		}
		if f.Strict {
			return nil, fmt.Errorf("virtio-fs missing fsyncdir backend for FSYNCDIR node=%d fh=%d", nodeID, fh)
		}
		return reply(0, nil), nil
	case fuseGetLK:
		if len(req) < fuseInHeaderSize+fuseLKInSize {
			return nil, fmt.Errorf("virtio-fs GETLK too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		f.logPathf("getlk", nodeID, fmt.Sprintf(" fh=%d", fh))
		extra := make([]byte, fuseLKOutSize)
		binary.LittleEndian.PutUint32(extra[16:20], linuxFUnlck)
		return reply(0, extra), nil
	case fuseSetLK, fuseSetLKW:
		if len(req) < fuseInHeaderSize+fuseLKInSize {
			return nil, fmt.Errorf("virtio-fs %s too short", fuseOpcodeName(opcode))
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		lockType := binary.LittleEndian.Uint32(req[72:76])
		f.logPathf(strings.ToLower(fuseOpcodeName(opcode)), nodeID, fmt.Sprintf(" fh=%d type=%d", fh, lockType))
		return reply(0, nil), nil
	case fuseRmDir:
		name := readCStringName(req[fuseInHeaderSize:])
		f.logPathf("rmdir-parent", nodeID, fmt.Sprintf(" name=%q", name))
		if be, ok := f.backend.(fsRmDirBackend); ok {
			errno := be.RmDir(nodeID, path.Clean(name))
			return reply(errno, nil), nil
		}
		return nil, fmt.Errorf("virtio-fs missing rmdir backend for parent=%d name=%q", nodeID, name)
	case fuseRename:
		if len(req) < fuseInHeaderSize+8 {
			return nil, fmt.Errorf("virtio-fs RENAME too short")
		}
		if be, ok := f.backend.(fsRenameBackend); ok {
			newParent := binary.LittleEndian.Uint64(req[40:48])
			names := req[fuseInHeaderSize+8:]
			split := bytesIndexByte(names, 0)
			if split < 0 {
				return nil, fmt.Errorf("virtio-fs RENAME missing old name")
			}
			oldName := string(names[:split])
			newName := readCStringName(names[split+1:])
			return reply(be.Rename(nodeID, path.Clean(oldName), newParent, path.Clean(newName), 0), nil), nil
		}
		return reply(-linuxENOSYS, nil), nil
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
	case fuseAccess:
		if f.Strict {
			return nil, fmt.Errorf("virtio-fs unsupported opcode %s node=%d", fuseOpcodeName(opcode), nodeID)
		}
		return reply(-linuxENOSYS, nil), nil
	case fusePoll:
		return reply(0, make([]byte, 8)), nil
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
	case fuseStatx:
		if len(req) < fuseInHeaderSize+24 {
			return nil, fmt.Errorf("virtio-fs STATX too short")
		}
		f.logPathf("statx", nodeID, fmt.Sprintf(" mask=%#x", binary.LittleEndian.Uint32(req[60:64])))
		attr, errno := f.backend.GetAttr(nodeID)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		extra := make([]byte, fuseStatxOutSize)
		encodeFuseAttrTTL(extra[0:16], f.attrTTL)
		encodeFuseStatx(extra[32:], attr)
		return reply(0, extra), nil
	case fuseSyncFS:
		return reply(0, nil), nil
	case fuseTmpfile:
		f.logPathf("tmpfile", nodeID, "")
		return reply(-linuxENOSYS, nil), nil
	case fuseDestroy:
		return reply(0, nil), nil
	case fuseIoctl:
		f.logPathf("ioctl", nodeID, "")
		return reply(-linuxENOTTY, nil), nil
	case fuseCreate:
		if len(req) < fuseInHeaderSize+16 {
			return nil, fmt.Errorf("virtio-fs CREATE too short")
		}
		if be, ok := f.backend.(fsCreateBackend); ok {
			flags := binary.LittleEndian.Uint32(req[40:44])
			mode := binary.LittleEndian.Uint32(req[44:48])
			name := readCStringName(req[fuseInHeaderSize+16:])
			childID, fh, attr, errno := be.Create(nodeID, path.Clean(name), flags, mode, callerUID, callerGID)
			if errno != 0 {
				return reply(errno, nil), nil
			}
			extra := make([]byte, fuseEntryOutSize+fuseOpenOutSize)
			f.encodeFuseEntryOut(extra[:fuseEntryOutSize], childID)
			encodeFuseAttr(extra[40:], attr)
			binary.LittleEndian.PutUint64(extra[fuseEntryOutSize:fuseEntryOutSize+8], fh)
			binary.LittleEndian.PutUint32(extra[fuseEntryOutSize+8:fuseEntryOutSize+12], f.openResponseFlags(flags, false))
			return reply(0, extra), nil
		}
		return reply(-linuxENOSYS, nil), nil
	default:
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
	case fuseSetAttr:
		return "SETATTR"
	case fuseReadlink:
		return "READLINK"
	case fuseSymlink:
		return "SYMLINK"
	case fuseMknod:
		return "MKNOD"
	case fuseMkdir:
		return "MKDIR"
	case fuseUnlink:
		return "UNLINK"
	case fuseRmDir:
		return "RMDIR"
	case fuseRename:
		return "RENAME"
	case fuseOpen:
		return "OPEN"
	case fuseRead:
		return "READ"
	case fuseWrite:
		return "WRITE"
	case fuseStatfs:
		return "STATFS"
	case fuseRelease:
		return "RELEASE"
	case fuseFsync:
		return "FSYNC"
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
	case fuseFsyncDir:
		return "FSYNCDIR"
	case fuseGetLK:
		return "GETLK"
	case fuseSetLK:
		return "SETLK"
	case fuseSetLKW:
		return "SETLKW"
	case fuseAccess:
		return "ACCESS"
	case fuseCreate:
		return "CREATE"
	case fuseDestroy:
		return "DESTROY"
	case fuseIoctl:
		return "IOCTL"
	case fusePoll:
		return "POLL"
	case fuseLseek:
		return "LSEEK"
	case fuseSyncFS:
		return "SYNCFS"
	case fuseTmpfile:
		return "TMPFILE"
	case fuseStatx:
		return "STATX"
	default:
		return "UNKNOWN"
	}
}

func fuseOpcodeMetricName(opcode uint32) string {
	return strings.ToLower(fuseOpcodeName(opcode))
}

func (f *FS) recordFUSEDispatchTiming(opcode uint32, start time.Time) {
	duration := time.Since(start)
	f.fuseRequests.Add(1)
	if opcode < uint32(len(f.fuseOpStats)) {
		recordTimingStat(&f.fuseOpStats[opcode].timingStat, duration)
	}
	if f.RecordTiming == nil {
		return
	}
	tag := strings.TrimRight(string(f.tag[:]), "\x00")
	if tag == "" {
		tag = "unknown"
	}
	f.RecordTiming("virtio.fs."+tag+".fuse."+fuseOpcodeMetricName(opcode), duration)
}

func (f *FS) recordStageTiming(stage int, start time.Time) {
	f.recordStageDuration(stage, time.Since(start))
}

func (f *FS) recordStageDuration(stage int, duration time.Duration) {
	if stage < 0 || stage >= len(f.stageStats) {
		return
	}
	recordTimingStat(&f.stageStats[stage], duration)
	if f.RecordTiming == nil {
		return
	}
	tag := strings.TrimRight(string(f.tag[:]), "\x00")
	if tag == "" {
		tag = "unknown"
	}
	f.RecordTiming("virtio.fs."+tag+".stage."+fsStageName(stage), duration)
}

func recordTimingStat(stat *timingStat, duration time.Duration) {
	nanos := duration.Nanoseconds()
	stat.count.Add(1)
	stat.totalNanos.Add(nanos)
	for {
		oldMax := stat.maxNanos.Load()
		if nanos <= oldMax || stat.maxNanos.CompareAndSwap(oldMax, nanos) {
			break
		}
	}
}

func fsStageName(stage int) string {
	switch stage {
	case fsStageQueueHarvest:
		return "queue_harvest"
	case fsStageInlineDispatch:
		return "inline_dispatch"
	case fsStageInlineComplete:
		return "inline_complete"
	case fsStageAsyncComplete:
		return "async_complete"
	default:
		return "unknown"
	}
}

func fuseReply(unique uint64, errno int32, extra []byte) []byte {
	out := make([]byte, fuseOutHeaderSize+len(extra))
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(out)))
	binary.LittleEndian.PutUint32(out[4:8], uint32(errno))
	binary.LittleEndian.PutUint64(out[8:16], unique)
	copy(out[16:], extra)
	return out
}

func (f *FS) encodeFuseEntryOut(dst []byte, nodeID uint64) {
	binary.LittleEndian.PutUint64(dst[0:8], nodeID)
	binary.LittleEndian.PutUint64(dst[8:16], 1)
	encodeFuseTTL(dst[16:24], dst[32:36], f.entryTTL)
	encodeFuseTTL(dst[24:32], dst[36:40], f.attrTTL)
}

func encodeFuseAttrTTL(dst []byte, ttl time.Duration) {
	encodeFuseTTL(dst[0:8], dst[8:12], ttl)
}

func encodeFuseTTL(secDst []byte, nsecDst []byte, ttl time.Duration) {
	if ttl < 0 {
		ttl = 0
	}
	sec := ttl / time.Second
	nsec := ttl % time.Second
	binary.LittleEndian.PutUint64(secDst, uint64(sec))
	binary.LittleEndian.PutUint32(nsecDst, uint32(nsec))
}

func (f *FS) openResponseFlags(openFlags uint32, dir bool) uint32 {
	flags := uint32(fuseOpenNoFlush)
	if f.cacheMode == fsCacheStrict {
		return flags
	}
	if dir {
		return flags | fuseOpenCacheDir
	}
	if openFlags&linuxOACCMODE == linuxORDONLY {
		flags |= fuseOpenKeepCache
	}
	return flags
}

func takeFSReqBuffer(size int) ([]byte, bool) {
	if size > fsPooledReqSize {
		return make([]byte, size), false
	}
	buf := fsReqPool.Get().([]byte)
	return buf[:size], true
}

func putFSReqBuffer(buf []byte, pooled bool) {
	if !pooled {
		return
	}
	buf = buf[:fsPooledReqSize]
	fsReqPool.Put(buf)
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

func (f *FS) readIPAInto(addr uint64, dst []byte) error {
	if reader, ok := f.mem.(guestMemoryReaderInto); ok {
		return reader.ReadIPAInto(addr, dst)
	}
	buf, err := f.mem.ReadIPA(addr, len(dst))
	if err != nil {
		return err
	}
	copy(dst, buf)
	return nil
}

func (f *FS) readDescriptorChainLocked(q *queue, head uint16, out []fsDesc) ([]fsDesc, error) {
	index := head
	for i := uint16(0); i < q.size; i++ {
		if index >= q.size {
			return nil, fmt.Errorf("virtio-fs descriptor index %d out of range", index)
		}
		var buf [16]byte
		if err := f.readIPAInto(q.descAddr+uint64(index)*16, buf[:]); err != nil {
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
	var elem [8]byte
	binary.LittleEndian.PutUint32(elem[0:4], uint32(head))
	binary.LittleEndian.PutUint32(elem[4:8], usedLen)
	if err := f.mem.WriteIPA(q.usedAddr+4+uint64(slot)*8, elem[:]); err != nil {
		return err
	}
	q.usedIdx++
	var idx [2]byte
	binary.LittleEndian.PutUint16(idx[:], q.usedIdx)
	return f.mem.WriteIPA(q.usedAddr+2, idx[:])
}

func (f *FS) shouldInterruptLocked(q *queue, oldUsedIdx, newUsedIdx, availFlags uint16) bool {
	if oldUsedIdx == newUsedIdx {
		return false
	}
	if f.driverFeatures&featureRingEventIdx == 0 {
		return availFlags&1 == 0
	}
	var buf [2]byte
	if err := f.readIPAInto(q.availAddr+4+uint64(q.size)*2, buf[:]); err != nil {
		f.logf("used-event-read-error: %v", err)
		return true
	}
	usedEvent := binary.LittleEndian.Uint16(buf[:])
	return vringNeedEvent(usedEvent, newUsedIdx, oldUsedIdx)
}

func (f *FS) writeAvailEventLocked(q *queue) error {
	if q.size == 0 || q.usedAddr == 0 {
		return nil
	}
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], q.lastAvailIdx)
	return f.mem.WriteIPA(q.usedAddr+4+uint64(q.size)*8, buf[:])
}

func vringNeedEvent(eventIdx, newIdx, oldIdx uint16) bool {
	return uint16(newIdx-eventIdx-1) < uint16(newIdx-oldIdx)
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
	f.irqTransitions++
	f.logf("set-irq irq=%d level=%v", f.IRQ, level)
	return f.irq.SetIRQ(f.IRQ, level)
}

func (f *FS) Summary() string {
	if f == nil {
		return "virtio-fs=<nil>"
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	tag := strings.TrimRight(string(f.tag[:]), "\x00")
	queueNotifies := append([]uint64(nil), f.queueNotifies...)
	queueReady := f.queueReadySnapshotLocked()
	queueLastAvail := f.queueLastAvailSnapshotLocked()
	return fmt.Sprintf(
		"virtio-fs tag=%q mmio_reads=%d mmio_writes=%d status=%#x queue_notifies=%v fuse_requests=%d interrupt_raises=%d irq_transitions=%d irq_high=%t interrupt_status=%#x queue_ready=%v queue_last=%v",
		tag,
		f.mmioReads,
		f.mmioWrites,
		f.status,
		queueNotifies,
		f.fuseRequests.Load(),
		f.interruptRaises,
		f.irqTransitions,
		f.irqHigh,
		f.interruptStatus,
		queueReady,
		queueLastAvail,
	)
}

func (f *FS) Stats() FSStats {
	if f == nil {
		return FSStats{}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	tag := strings.TrimRight(string(f.tag[:]), "\x00")
	ops := make([]FUSEOpStats, 0, len(f.fuseOpStats))
	for opcode, stat := range f.fuseOpStats {
		count := stat.timingStat.count.Load()
		if count == 0 {
			continue
		}
		totalNanos := stat.timingStat.totalNanos.Load()
		avg := int64(0)
		if count != 0 {
			avg = totalNanos / int64(count)
		}
		opcodeValue := uint32(opcode)
		ops = append(ops, FUSEOpStats{
			Opcode:       opcodeValue,
			Name:         fuseOpcodeName(opcodeValue),
			Count:        count,
			TotalNanos:   totalNanos,
			MaxNanos:     stat.timingStat.maxNanos.Load(),
			AverageNanos: avg,
		})
	}
	sort.Slice(ops, func(i, j int) bool {
		if ops[i].Count == ops[j].Count {
			return ops[i].Opcode < ops[j].Opcode
		}
		return ops[i].Count > ops[j].Count
	})
	stages := make([]TimingStats, 0, len(f.stageStats))
	for stage, stat := range f.stageStats {
		if stats, ok := timingStatsSnapshot(fsStageName(stage), &stat); ok {
			stages = append(stages, stats)
		}
	}
	return FSStats{
		Tag:             tag,
		MMIOReads:       f.mmioReads,
		MMIOWrites:      f.mmioWrites,
		QueueNotifies:   append([]uint64(nil), f.queueNotifies...),
		FUSERequests:    f.fuseRequests.Load(),
		InterruptRaises: f.interruptRaises,
		IRQTransitions:  f.irqTransitions,
		IRQHigh:         f.irqHigh,
		InterruptStatus: f.interruptStatus,
		QueueReady:      f.queueReadySnapshotLocked(),
		QueueLastAvail:  f.queueLastAvailSnapshotLocked(),
		FUSEOps:         ops,
		Stages:          stages,
	}
}

func timingStatsSnapshot(name string, stat *timingStat) (TimingStats, bool) {
	count := stat.count.Load()
	if count == 0 {
		return TimingStats{}, false
	}
	totalNanos := stat.totalNanos.Load()
	avg := int64(0)
	if count != 0 {
		avg = totalNanos / int64(count)
	}
	return TimingStats{
		Name:         name,
		Count:        count,
		TotalNanos:   totalNanos,
		MaxNanos:     stat.maxNanos.Load(),
		AverageNanos: avg,
	}, true
}

func (f *FS) queueReadySnapshotLocked() []bool {
	ready := make([]bool, len(f.queues))
	for i := range f.queues {
		ready[i] = f.queues[i].ready
	}
	return ready
}

func (f *FS) queueLastAvailSnapshotLocked() []uint16 {
	last := make([]uint16, len(f.queues))
	for i := range f.queues {
		last[i] = f.queues[i].lastAvailIdx
	}
	return last
}

func (f *FS) isCompletingQueue(qidx int) bool {
	return qidx >= 0 && qidx < len(f.queues)
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
	f.resetQueueStateLocked()
}

func (f *FS) configBytesLocked() []byte {
	cfg := make([]byte, fsCfgTotalSize)
	copy(cfg[:fsCfgTagSize], f.tag[:])
	binary.LittleEndian.PutUint32(cfg[fsCfgNumQueueOff:fsCfgNumQueueOff+4], fsRequestQueueCount)
	return cfg
}

func (f *FS) resetQueueStateLocked() {
	queueCount := fsQueueCount
	if cap(f.queues) < queueCount {
		f.queues = make([]queue, queueCount)
	} else {
		f.queues = f.queues[:queueCount]
		clear(f.queues)
	}
	if len(f.queueNotifies) != queueCount {
		old := f.queueNotifies
		f.queueNotifies = make([]uint64, queueCount)
		copy(f.queueNotifies, old)
	}
	if cap(f.nextWorkSeq) < queueCount {
		f.nextWorkSeq = make([]uint64, queueCount)
	} else {
		f.nextWorkSeq = f.nextWorkSeq[:queueCount]
		clear(f.nextWorkSeq)
	}
	if cap(f.nextCompleteSeq) < queueCount {
		f.nextCompleteSeq = make([]uint64, queueCount)
	} else {
		f.nextCompleteSeq = f.nextCompleteSeq[:queueCount]
		clear(f.nextCompleteSeq)
	}
	f.completions = make(map[fsCompletionKey]fsCompletion)
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

func encodeFuseStatx(dst []byte, attr FuseAttr) {
	blkSize := attr.BlkSize
	if blkSize == 0 {
		blkSize = 4096
	}
	binary.LittleEndian.PutUint32(dst[0:4], statxBasicStats)
	binary.LittleEndian.PutUint32(dst[4:8], blkSize)
	binary.LittleEndian.PutUint32(dst[16:20], attr.NLink)
	binary.LittleEndian.PutUint32(dst[20:24], attr.UID)
	binary.LittleEndian.PutUint32(dst[24:28], attr.GID)
	binary.LittleEndian.PutUint16(dst[28:30], uint16(attr.Mode))
	binary.LittleEndian.PutUint64(dst[32:40], attr.Ino)
	binary.LittleEndian.PutUint64(dst[40:48], attr.Size)
	binary.LittleEndian.PutUint64(dst[48:56], attr.Blocks)
	encodeFuseStatxTime(dst[64:80], attr.ATimeSec, attr.ATimeNsec)
	encodeFuseStatxTime(dst[96:112], attr.CTimeSec, attr.CTimeNsec)
	encodeFuseStatxTime(dst[112:128], attr.MTimeSec, attr.MTimeNsec)
}

func encodeFuseStatxTime(dst []byte, sec uint64, nsec uint32) {
	binary.LittleEndian.PutUint64(dst[0:8], sec)
	binary.LittleEndian.PutUint32(dst[8:12], nsec)
}

func readCStringName(buf []byte) string {
	if i := bytesIndexByte(buf, 0); i >= 0 {
		buf = buf[:i]
	}
	return string(buf)
}

func readTwoCStringNames(buf []byte) (string, string, bool) {
	firstEnd := bytesIndexByte(buf, 0)
	if firstEnd < 0 {
		return "", "", false
	}
	second := buf[firstEnd+1:]
	secondEnd := bytesIndexByte(second, 0)
	if secondEnd < 0 {
		return "", "", false
	}
	return string(buf[:firstEnd]), string(second[:secondEnd]), true
}

func cleanChildName(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if !strings.Contains(name, "/") && name != "." && name != ".." {
		return name, true
	}
	clean := path.Clean("/" + name)
	if clean == "/" {
		return "", false
	}
	return strings.TrimPrefix(clean, "/"), true
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
	root           string
	meta           map[string]fsmeta.Entry
	writebackCache bool

	mu         sync.RWMutex
	nextNodeID uint64
	nextHandle uint64
	nodes      map[uint64]string
	pathToNode map[string]uint64
	handles    map[uint64]*passthroughHandle
	dirHandles map[uint64][]dirEntry
}

type passthroughHandle struct {
	nodeID uint64
	file   *os.File
	append bool
}

type imageFS struct {
	root     string
	ownerUID uint32
	ownerGID uint32
	mapOwner bool

	mu         sync.Mutex
	nextNodeID uint64
	nextHandle uint64
	nodes      map[uint64]*imageNode
	handles    map[uint64]*imageHandle
	dirHandles map[uint64][]dirEntry
}

type imageHandle struct {
	nodeID uint64
	reader io.ReaderAt
	closer io.Closer
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
	data          []byte
	symlinkTarget string
	entries       map[string]uint64
	whiteouts     map[string]bool
	entriesDone   bool
	modTime       time.Time
	abstractFile  imagefs.File
	abstractDir   imagefs.Directory
	abstractLink  imagefs.Symlink
}

type dirEntry struct {
	name string
	typ  uint32
	ino  uint64
}

func NewPassthroughFS(root string, meta map[string]fsmeta.Entry) FSBackend {
	fs := &passthroughFS{
		root:       root,
		meta:       meta,
		nextNodeID: 2,
		nextHandle: 1,
		nodes:      map[uint64]string{1: "/"},
		pathToNode: map[string]uint64{"/": 1},
		handles:    map[uint64]*passthroughHandle{},
		dirHandles: map[uint64][]dirEntry{},
	}
	return fs
}

func NewImageFS(root imagefs.Directory, statfsPath string) FSBackend {
	return newImageFS(root, statfsPath, 0, 0, false)
}

func NewImageFSWithOwner(root imagefs.Directory, statfsPath string, uid, gid uint32) FSBackend {
	return newImageFS(root, statfsPath, uid, gid, true)
}

func newImageFS(root imagefs.Directory, statfsPath string, uid, gid uint32, mapOwner bool) FSBackend {
	imgFS := &imageFS{
		root:       statfsPath,
		ownerUID:   uid,
		ownerGID:   gid,
		mapOwner:   mapOwner,
		nextNodeID: 2,
		nextHandle: 1,
		nodes:      map[uint64]*imageNode{},
		handles:    map[uint64]*imageHandle{},
		dirHandles: map[uint64][]dirEntry{},
	}
	if root == nil {
		root = imagefs.NewHostFS("", nil)
	}
	rootMode := fs.ModeDir | root.Stat()
	rootUID, rootGID := root.Owner()
	rootRDev := root.RDev()
	rootModTime := root.ModTime()
	if rootModTime.IsZero() {
		rootModTime = time.Unix(0, 0)
	}
	imgFS.nodes[1] = &imageNode{
		id:          1,
		parent:      1,
		name:        "/",
		mode:        rootMode,
		uid:         rootUID,
		gid:         rootGID,
		rdev:        rootRDev,
		entries:     map[string]uint64{},
		modTime:     rootModTime,
		abstractDir: root,
	}
	return imgFS
}

func (p *passthroughFS) Init() (uint32, uint32) {
	return 128 << 10, fuseCapPosixLocks
}

func (p *passthroughFS) SetWritebackCache(enabled bool) {
	p.mu.Lock()
	p.writebackCache = enabled
	p.mu.Unlock()
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
	hostParent, guestParent, errno := p.hostAndGuestPath(parent)
	if errno != 0 {
		return 0, FuseAttr{}, errno
	}
	rel, ok := cleanChildName(name)
	if !ok {
		attr, errno := p.GetAttr(parent)
		return parent, attr, errno
	}
	host := filepath.Join(hostParent, filepath.FromSlash(rel))
	info, err := os.Lstat(host)
	if err != nil {
		return 0, FuseAttr{}, errnoFromError(err)
	}
	guestPath := joinGuestChild(guestParent, rel)
	if p.root != "" {
		p.logf("lookup name=%q guest=%q host=%q", name, guestPath, host)
	}
	nodeID := p.ensureNode(guestPath)
	return nodeID, p.fileAttr(nodeID, info), 0
}

func (p *passthroughFS) Mkdir(parent uint64, name string, mode uint32, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
	p.logNode("mkdir-parent", parent)
	hostParent, guestParent, errno := p.hostAndGuestPath(parent)
	if errno != 0 {
		return 0, FuseAttr{}, errno
	}
	rel, ok := cleanChildName(name)
	if !ok {
		return 0, FuseAttr{}, -linuxEINVAL
	}
	host := filepath.Join(hostParent, filepath.FromSlash(rel))
	if err := os.Mkdir(host, fs.FileMode(mode&linuxPermMask)); err != nil {
		return 0, FuseAttr{}, errnoFromError(err)
	}
	info, err := os.Lstat(host)
	if err != nil {
		return 0, FuseAttr{}, errnoFromError(err)
	}
	guestPath := joinGuestChild(guestParent, rel)
	if p.meta != nil {
		p.mu.Lock()
		if _, ok := p.meta[guestPath]; !ok {
			p.meta[guestPath] = fsmeta.Entry{
				UID:  uid,
				GID:  gid,
				Mode: uint32(linuxSIFDIR) | (mode & linuxPermMask),
			}
		}
		p.mu.Unlock()
	}
	nodeID := p.ensureNode(guestPath)
	return nodeID, p.fileAttr(nodeID, info), 0
}

func (p *passthroughFS) Symlink(parent uint64, name string, target string, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
	hostParent, guestParent, errno := p.hostAndGuestPath(parent)
	if errno != 0 {
		return 0, FuseAttr{}, errno
	}
	rel, ok := cleanChildName(name)
	if !ok {
		return 0, FuseAttr{}, -linuxEINVAL
	}
	host := filepath.Join(hostParent, filepath.FromSlash(rel))
	if err := os.Symlink(target, host); err != nil {
		return 0, FuseAttr{}, errnoFromError(err)
	}
	info, err := os.Lstat(host)
	if err != nil {
		return 0, FuseAttr{}, errnoFromError(err)
	}
	guestPath := joinGuestChild(guestParent, rel)
	if p.meta != nil {
		p.mu.Lock()
		if _, ok := p.meta[guestPath]; !ok {
			p.meta[guestPath] = fsmeta.Entry{
				UID:  uid,
				GID:  gid,
				Mode: uint32(linuxSIFLNK) | 0o777,
			}
		}
		p.mu.Unlock()
	}
	nodeID := p.ensureNode(guestPath)
	return nodeID, p.fileAttr(nodeID, info), 0
}

func (p *passthroughFS) Create(parent uint64, name string, flags uint32, mode uint32, uid uint32, gid uint32) (uint64, uint64, FuseAttr, int32) {
	p.logNode("create-parent", parent)
	hostParent, guestParent, errno := p.hostAndGuestPath(parent)
	if errno != 0 {
		return 0, 0, FuseAttr{}, errno
	}
	rel, ok := cleanChildName(name)
	if !ok {
		return 0, 0, FuseAttr{}, -linuxEINVAL
	}
	host := filepath.Join(hostParent, filepath.FromSlash(rel))
	file, err := os.OpenFile(host, p.translateOpenFlags(flags)|os.O_CREATE, fs.FileMode(mode&linuxPermMask))
	if err != nil {
		return 0, 0, FuseAttr{}, errnoFromError(err)
	}
	info, err := os.Lstat(host)
	if err != nil {
		_ = file.Close()
		return 0, 0, FuseAttr{}, errnoFromError(err)
	}
	guestPath := joinGuestChild(guestParent, rel)
	nodeID := p.ensureNode(guestPath)
	p.mu.Lock()
	if p.meta != nil {
		if _, ok := p.meta[guestPath]; !ok {
			p.meta[guestPath] = fsmeta.Entry{
				UID:  uid,
				GID:  gid,
				Mode: uint32(linuxSIFREG) | (mode & linuxPermMask),
			}
		}
	}
	handle := p.nextHandle
	p.nextHandle++
	p.handles[handle] = &passthroughHandle{nodeID: nodeID, file: file, append: flags&linuxOAPPEND != 0}
	p.mu.Unlock()
	return nodeID, handle, p.fileAttr(nodeID, info), 0
}

func (p *passthroughFS) Open(nodeID uint64, flags uint32) (uint64, int32) {
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
	file, err := os.OpenFile(host, p.translateOpenFlags(flags), 0)
	if err != nil {
		return 0, errnoFromError(err)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	handle := p.nextHandle
	p.nextHandle++
	p.handles[handle] = &passthroughHandle{nodeID: nodeID, file: file, append: flags&linuxOAPPEND != 0}
	return handle, 0
}

func (p *passthroughFS) Release(_ uint64, fh uint64) {
	p.mu.Lock()
	handle := p.handles[fh]
	delete(p.handles, fh)
	p.mu.Unlock()
	if handle != nil && handle.file != nil {
		_ = handle.file.Close()
	}
}

func (p *passthroughFS) Flush(_ uint64, fh uint64, _ uint64) int32 {
	p.mu.RLock()
	handle := p.handles[fh]
	p.mu.RUnlock()
	if handle == nil || handle.file == nil {
		return -linuxEBADF
	}
	return 0
}

func (p *passthroughFS) Fsync(_ uint64, fh uint64, _ uint32) int32 {
	p.mu.RLock()
	handle := p.handles[fh]
	p.mu.RUnlock()
	if handle == nil || handle.file == nil {
		return -linuxEBADF
	}
	if err := handle.file.Sync(); err != nil {
		return errnoFromError(err)
	}
	return 0
}

func (p *passthroughFS) FsyncDir(_ uint64, fh uint64, _ uint32) int32 {
	p.mu.RLock()
	_, ok := p.dirHandles[fh]
	p.mu.RUnlock()
	if !ok {
		return -linuxEBADF
	}
	return 0
}

func (p *passthroughFS) Read(nodeID uint64, fh uint64, off uint64, size uint32) ([]byte, int32) {
	p.logf("read node=%d fh=%d off=%d size=%d", nodeID, fh, off, size)
	p.mu.RLock()
	handle, ok := p.handles[fh]
	p.mu.RUnlock()
	if !ok || handle == nil || handle.nodeID != nodeID || handle.file == nil {
		return nil, -linuxEBADF
	}
	buf := make([]byte, size)
	n, err := handle.file.ReadAt(buf, int64(off))
	if err != nil && err != io.EOF {
		return nil, errnoFromError(err)
	}
	return buf[:n], 0
}

func (p *passthroughFS) Lseek(nodeID uint64, fh uint64, offset uint64, whence uint32) (uint64, int32) {
	p.mu.RLock()
	handle, ok := p.handles[fh]
	p.mu.RUnlock()
	if !ok || handle == nil || handle.nodeID != nodeID {
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
	host, guest, errno := p.hostAndGuestPath(nodeID)
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
		childPath := joinGuestChild(guest, entry.Name())
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

func (p *passthroughFS) Write(nodeID uint64, fh uint64, off uint64, data []byte, _ uint32) (uint32, int32) {
	p.mu.RLock()
	handle := p.handles[fh]
	p.mu.RUnlock()
	if handle == nil || handle.nodeID != nodeID || handle.file == nil {
		return 0, -linuxEBADF
	}
	var (
		n   int
		err error
	)
	if handle.append {
		n, err = handle.file.Write(data)
	} else {
		n, err = handle.file.WriteAt(data, int64(off))
	}
	if err != nil {
		return uint32(n), errnoFromError(err)
	}
	return uint32(n), 0
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
	hostParent, guestParent, errno := p.hostAndGuestPath(parent)
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
	guestPath := joinGuestChild(guestParent, rel)
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

func (p *passthroughFS) Unlink(parent uint64, name string) int32 {
	hostParent, guestParent, errno := p.hostAndGuestPath(parent)
	if errno != 0 {
		return errno
	}
	clean := path.Clean("/" + name)
	if clean == "/" {
		return -linuxEINVAL
	}
	host := filepath.Join(hostParent, filepath.FromSlash(strings.TrimPrefix(clean, "/")))
	if err := os.Remove(host); err != nil {
		return errnoFromError(err)
	}
	p.removeNodeForGuestPath(joinGuestChild(guestParent, strings.TrimPrefix(clean, "/")))
	return 0
}

func (p *passthroughFS) Rename(parent uint64, name string, newParent uint64, newName string, _ uint32) int32 {
	oldParent, oldGuestParent, errno := p.hostAndGuestPath(parent)
	if errno != 0 {
		return errno
	}
	newParentPath, newGuestParent, errno := p.hostAndGuestPath(newParent)
	if errno != 0 {
		return errno
	}
	oldRel := strings.TrimPrefix(path.Clean("/"+name), "/")
	newRel := strings.TrimPrefix(path.Clean("/"+newName), "/")
	oldHost := filepath.Join(oldParent, filepath.FromSlash(oldRel))
	newHost := filepath.Join(newParentPath, filepath.FromSlash(newRel))
	if err := os.Rename(oldHost, newHost); err != nil {
		return errnoFromError(err)
	}
	p.renameNodeGuestPath(joinGuestChild(oldGuestParent, oldRel), joinGuestChild(newGuestParent, newRel))
	return 0
}

func (p *passthroughFS) SetAttr(nodeID uint64, valid uint32, fh uint64, size uint64, mode uint32, uid uint32, gid uint32, atime time.Time, mtime time.Time) (FuseAttr, int32) {
	host, errno := p.hostPath(nodeID)
	if errno != 0 {
		return FuseAttr{}, errno
	}
	var file *os.File
	if valid&fattrFH != 0 {
		p.mu.Lock()
		handle := p.handles[fh]
		p.mu.Unlock()
		if handle == nil || handle.nodeID != nodeID {
			return FuseAttr{}, -linuxEBADF
		}
		file = handle.file
	}
	if valid&fattrSize != 0 {
		if file != nil {
			if err := file.Truncate(int64(size)); err != nil {
				return FuseAttr{}, errnoFromError(err)
			}
		} else if err := os.Truncate(host, int64(size)); err != nil {
			return FuseAttr{}, errnoFromError(err)
		}
	}
	if valid&fattrMode != 0 {
		if err := os.Chmod(host, fs.FileMode(mode&linuxPermMask)); err != nil {
			return FuseAttr{}, errnoFromError(err)
		}
	}
	if valid&(fattrUID|fattrGID) != 0 {
		if err := os.Chown(host, int(uid), int(gid)); err != nil {
			return FuseAttr{}, errnoFromError(err)
		}
	}
	if valid&(fattrATime|fattrMTime) != 0 {
		current := time.Now()
		if valid&fattrATime == 0 {
			atime = current
		}
		if valid&fattrMTime == 0 {
			mtime = current
		}
		if err := os.Chtimes(host, atime, mtime); err != nil {
			return FuseAttr{}, errnoFromError(err)
		}
	}
	info, err := os.Lstat(host)
	if err != nil {
		return FuseAttr{}, errnoFromError(err)
	}
	return p.fileAttr(nodeID, info), 0
}

func (p *passthroughFS) StatFS(_ uint64) (uint64, uint64, uint64, uint64, uint64, uint64, uint64, uint64, int32) {
	if p.root == "" {
		return 0, 0, 0, 0, 0, 4096, 4096, 255, 0
	}
	return hostStatFS(p.root)
}

func (p *passthroughFS) hostPath(nodeID uint64) (string, int32) {
	host, _, errno := p.hostAndGuestPath(nodeID)
	return host, errno
}

func (p *passthroughFS) translateOpenFlags(flags uint32) int {
	p.mu.RLock()
	writebackCache := p.writebackCache
	p.mu.RUnlock()
	return translateLinuxOpenFlags(flags, writebackCache)
}

func (p *passthroughFS) hostAndGuestPath(nodeID uint64) (string, string, int32) {
	p.mu.RLock()
	guest, ok := p.nodes[nodeID]
	p.mu.RUnlock()
	if !ok {
		return "", "", -linuxENOENT
	}
	if p.root == "" {
		return "", "", -linuxENOENT
	}
	if guest == "/" {
		return p.root, guest, 0
	}
	return filepath.Join(p.root, filepath.FromSlash(strings.TrimPrefix(guest, "/"))), guest, 0
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

func joinGuestChild(parentGuest, rel string) string {
	if rel == "" {
		return path.Clean(parentGuest)
	}
	return path.Join(parentGuest, filepath.ToSlash(rel))
}

func (p *passthroughFS) DebugPath(nodeID uint64) string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.nodes[nodeID]
}

func (p *passthroughFS) GetXattr(nodeID uint64, name string) ([]byte, int32) {
	p.logf("getxattr-backend node=%d name=%q", nodeID, name)
	return nil, -linuxENODATA
}

func (p *passthroughFS) ListXattr(nodeID uint64) ([]byte, int32) {
	p.logNode("listxattr-backend", nodeID)
	return nil, 0
}

func (p *passthroughFS) logNode(op string, nodeID uint64) {
	p.logf("%s node=%d", op, nodeID)
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

func (p *passthroughFS) removeNodeForGuestPath(guestPath string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.pathToNode, guestPath)
	for id, existing := range p.nodes {
		if existing == guestPath {
			delete(p.nodes, id)
			break
		}
	}
}

func (p *passthroughFS) renameNodeGuestPath(oldPath, newPath string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if id, ok := p.pathToNode[oldPath]; ok {
		delete(p.pathToNode, oldPath)
		p.pathToNode[newPath] = id
		p.nodes[id] = newPath
	}
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
	enrichHostFileAttr(info, &attr)
	if attr.Blocks == 0 && attr.Size > 0 {
		attr.Blocks = uint64((attr.Size + 511) / 512)
	}
	if attr.BlkSize == 0 {
		attr.BlkSize = 4096
	}
	if p.meta != nil {
		p.mu.RLock()
		guestPath := p.nodes[nodeID]
		meta, ok := p.meta[guestPath]
		p.mu.RUnlock()
		if ok {
			attr.UID = meta.UID
			attr.GID = meta.GID
			if meta.RDev != 0 {
				attr.RDev = meta.RDev
			}
			if meta.Mode != 0 {
				attr.Mode = fsmeta.NormalizeLinuxMode(meta.Mode, info.Mode())
			}
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
	return 128 << 10, fuseCapPosixLocks
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
		if parentNode.whiteouts[name] {
			return 0, FuseAttr{}, -linuxENOENT
		}
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

func (p *imageFS) Open(nodeID uint64, flags uint32) (uint64, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return 0, -linuxENOENT
	}
	if node.isDir() {
		return 0, -linuxEISDIR
	}
	if flags&linuxOACCMODE != linuxORDONLY {
		if errno := p.copyUpFileLocked(node); errno != 0 {
			return 0, errno
		}
		if flags&linuxOTRUNC != 0 {
			node.data = nil
			node.size = 0
			node.modTime = time.Now()
		}
	}
	fh := p.nextHandle
	p.nextHandle++
	handle := &imageHandle{nodeID: nodeID}
	if openable, ok := node.abstractFile.(imagefs.OpenReaderFile); ok {
		reader, closer, err := openable.OpenReader()
		if err != nil {
			return 0, errnoFromError(err)
		}
		handle.reader = reader
		handle.closer = closer
	}
	p.handles[fh] = handle
	return fh, 0
}

func (p *imageFS) Release(_ uint64, fh uint64) {
	p.mu.Lock()
	handle := p.handles[fh]
	delete(p.handles, fh)
	p.mu.Unlock()
	if handle != nil && handle.closer != nil {
		_ = handle.closer.Close()
	}
}

func (p *imageFS) Flush(_ uint64, _ uint64, _ uint64) int32 {
	return 0
}

func (p *imageFS) Fsync(_ uint64, _ uint64, _ uint32) int32 {
	return 0
}

func (p *imageFS) FsyncDir(_ uint64, _ uint64, _ uint32) int32 {
	return 0
}

func (p *imageFS) Read(nodeID uint64, fh uint64, off uint64, size uint32) ([]byte, int32) {
	p.mu.Lock()
	handle, ok := p.handles[fh]
	node := p.nodes[nodeID]
	p.mu.Unlock()
	if !ok || handle == nil || handle.nodeID != nodeID || node == nil {
		return nil, -linuxEBADF
	}
	if node.abstractFile == nil {
		end := off + uint64(size)
		if off >= node.size || size == 0 {
			return []byte{}, 0
		}
		if end > node.size {
			end = node.size
		}
		return append([]byte(nil), node.data[off:end]...), 0
	}
	if handle.reader != nil {
		buf := make([]byte, size)
		n, err := handle.reader.ReadAt(buf, int64(off))
		if err != nil && err != io.EOF {
			return nil, errnoFromError(err)
		}
		return buf[:n], 0
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
	handle, ok := p.handles[fh]
	node := p.nodes[nodeID]
	p.mu.Unlock()
	if !ok || handle == nil || handle.nodeID != nodeID || node == nil {
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
	if node.abstractDir != nil && !node.entriesDone {
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

func (p *imageFS) Mkdir(parent uint64, name string, mode uint32, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
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
		uid:     uid,
		gid:     gid,
		entries: map[string]uint64{},
	}
	p.nextNodeID++
	p.nodes[node.id] = node
	parentNode.entries[name] = node.id
	return node.id, p.attr(node), 0
}

func (p *imageFS) Mknod(parent uint64, name string, mode uint32, rdev uint32, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
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
	fileType := mode & linuxSIFMT
	switch fileType {
	case linuxSIFREG, linuxSIFCHR, linuxSIFBLK, linuxSIFIFO, linuxSIFSOCK:
	default:
		return 0, FuseAttr{}, -linuxEINVAL
	}
	if parentNode.whiteouts != nil {
		delete(parentNode.whiteouts, name)
	}
	node := &imageNode{
		id:      p.nextNodeID,
		parent:  parent,
		name:    name,
		mode:    linuxModeToGo(fileType | (mode & linuxPermMask)),
		rawMode: fileType | (mode & linuxPermMask),
		uid:     uid,
		gid:     gid,
		rdev:    rdev,
		modTime: time.Now(),
	}
	p.nextNodeID++
	p.nodes[node.id] = node
	parentNode.entries[name] = node.id
	return node.id, p.attr(node), 0
}

func (p *imageFS) Symlink(parent uint64, name string, target string, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
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
		id:            p.nextNodeID,
		parent:        parent,
		name:          name,
		mode:          fs.ModeSymlink | 0o777,
		uid:           uid,
		gid:           gid,
		size:          uint64(len(target)),
		symlinkTarget: target,
		modTime:       time.Now(),
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
	if parentNode.abstractDir != nil {
		if parentNode.whiteouts == nil {
			parentNode.whiteouts = map[string]bool{}
		}
		parentNode.whiteouts[name] = true
	}
	delete(p.nodes, childID)
	return 0
}

func (p *imageFS) Create(parent uint64, name string, _ uint32, mode uint32, uid uint32, gid uint32) (uint64, uint64, FuseAttr, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	if parentNode == nil {
		return 0, 0, FuseAttr{}, -linuxENOENT
	}
	name = path.Base(path.Clean("/" + name))
	if _, exists := parentNode.entries[name]; exists {
		return 0, 0, FuseAttr{}, -linuxEEXIST
	}
	if parentNode.whiteouts != nil {
		delete(parentNode.whiteouts, name)
	}
	node := &imageNode{
		id:      p.nextNodeID,
		parent:  parent,
		name:    name,
		mode:    fs.FileMode(mode & linuxPermMask),
		uid:     uid,
		gid:     gid,
		modTime: time.Now(),
	}
	p.nextNodeID++
	p.nodes[node.id] = node
	parentNode.entries[name] = node.id
	fh := p.nextHandle
	p.nextHandle++
	p.handles[fh] = &imageHandle{nodeID: node.id}
	return node.id, fh, p.attr(node), 0
}

func (p *imageFS) Write(nodeID uint64, fh uint64, off uint64, data []byte, _ uint32) (uint32, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	handle := p.handles[fh]
	node := p.nodes[nodeID]
	if handle == nil || handle.nodeID != nodeID || node == nil {
		return 0, -linuxEBADF
	}
	if errno := p.copyUpFileLocked(node); errno != 0 {
		return 0, errno
	}
	end := off + uint64(len(data))
	if end < off || end > uint64(^uint(0)>>1) {
		return 0, -linuxEFBIG
	}
	if errno := growImageNodeData(node, end); errno != 0 {
		return 0, errno
	}
	copy(node.data[off:end], data)
	if end > node.size {
		node.size = end
	}
	node.modTime = time.Now()
	return uint32(len(data)), 0
}

func growImageNodeData(node *imageNode, size uint64) int32 {
	if size <= uint64(len(node.data)) {
		return 0
	}
	if size > uint64(^uint(0)>>1) {
		return -linuxEFBIG
	}
	wantLen := int(size)
	if size <= uint64(cap(node.data)) {
		node.data = node.data[:wantLen]
		return 0
	}
	newCap := cap(node.data)
	if newCap < 4096 {
		newCap = 4096
	}
	for newCap < wantLen {
		if newCap > int(^uint(0)>>1)/2 {
			newCap = wantLen
			break
		}
		newCap *= 2
	}
	grown := make([]byte, wantLen, newCap)
	copy(grown, node.data)
	node.data = grown
	return 0
}

func (p *imageFS) SetAttr(nodeID uint64, valid uint32, _ uint64, size uint64, mode uint32, uid uint32, gid uint32, _ time.Time, mtime time.Time) (FuseAttr, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return FuseAttr{}, -linuxENOENT
	}
	if !node.isDir() {
		if errno := p.copyUpFileLocked(node); errno != 0 {
			return FuseAttr{}, errno
		}
	}
	if valid&fattrMode != 0 {
		if mode&linuxSIFMT != 0 {
			node.mode = linuxModeToGo(mode)
		} else {
			node.mode = (node.mode &^ fs.FileMode(linuxPermMask)) | fs.FileMode(mode&linuxPermMask)
		}
	}
	if valid&fattrUID != 0 {
		node.uid = uid
	}
	if valid&fattrGID != 0 {
		node.gid = gid
	}
	if valid&fattrSize != 0 {
		if size < uint64(len(node.data)) {
			node.data = node.data[:size]
		} else if size > uint64(len(node.data)) {
			if errno := growImageNodeData(node, size); errno != 0 {
				return FuseAttr{}, errno
			}
		}
		node.size = size
	}
	if valid&fattrMTime != 0 && !mtime.IsZero() {
		node.modTime = mtime
	} else {
		node.modTime = time.Now()
	}
	return p.attr(node), 0
}

func (p *imageFS) Unlink(parent uint64, name string) int32 {
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
	if child.isDir() {
		return -linuxEISDIR
	}
	delete(parentNode.entries, name)
	if parentNode.abstractDir != nil {
		if parentNode.whiteouts == nil {
			parentNode.whiteouts = map[string]bool{}
		}
		parentNode.whiteouts[name] = true
	}
	delete(p.nodes, childID)
	return 0
}

func (p *imageFS) Rename(parent uint64, name string, newParent uint64, newName string, _ uint32) int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	newParentNode := p.nodes[newParent]
	if parentNode == nil || newParentNode == nil {
		return -linuxENOENT
	}
	name = path.Base(path.Clean("/" + name))
	newName = path.Base(path.Clean("/" + newName))
	childID, ok := parentNode.entries[name]
	if !ok {
		return -linuxENOENT
	}
	child := p.nodes[childID]
	if child == nil {
		return -linuxENOENT
	}
	if existingID, exists := newParentNode.entries[newName]; exists && existingID != childID {
		existing := p.nodes[existingID]
		if existing != nil && existing.isDir() && !child.isDir() {
			return -linuxEISDIR
		}
		if existing != nil && !existing.isDir() && child.isDir() {
			return -linuxENOTDIR
		}
		delete(p.nodes, existingID)
	}
	delete(parentNode.entries, name)
	if parentNode.abstractDir != nil {
		if parentNode.whiteouts == nil {
			parentNode.whiteouts = map[string]bool{}
		}
		parentNode.whiteouts[name] = true
	}
	if newParentNode.whiteouts != nil {
		delete(newParentNode.whiteouts, newName)
	}
	newParentNode.entries[newName] = childID
	child.parent = newParent
	child.name = newName
	child.modTime = time.Now()
	return 0
}

func (p *imageFS) StatFS(_ uint64) (uint64, uint64, uint64, uint64, uint64, uint64, uint64, uint64, int32) {
	if p.root == "" {
		return 0, 0, 0, 0, 0, 4096, 4096, 255, 0
	}
	return hostStatFS(p.root)
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
	nodeMode := node.mode
	switch {
	case node.abstractFile != nil:
		size, nodeMode = node.abstractFile.Stat()
		if mt := node.abstractFile.ModTime(); !mt.IsZero() {
			modTime = mt
		}
	case node.abstractDir != nil:
		nodeMode = fs.ModeDir | node.abstractDir.Stat()
		if mt := node.abstractDir.ModTime(); !mt.IsZero() {
			modTime = mt
		}
	case node.abstractLink != nil:
		nodeMode = fs.ModeSymlink | node.abstractLink.Stat().Perm()
		node.symlinkTarget = node.abstractLink.Target()
		size = uint64(len(node.symlinkTarget))
		if mt := node.abstractLink.ModTime(); !mt.IsZero() {
			modTime = mt
		}
	}
	switch {
	case node.isDir():
		// Image roots are served as an ephemeral writable layer, similar to
		// Apptainer's writable-tmpfs mode. Preserve read-only semantics for
		// explicit non-writable mounts in mountedFS.
		mode = linuxSIFDIR | linuxModeBits(nodeMode|0o222)
	case node.isSymlink():
		mode = linuxSIFLNK | linuxModeBits(nodeMode)
	case node.rawMode != 0:
		mode = (node.rawMode &^ linuxPermMask) | linuxModeBits(nodeMode)
	default:
		mode = linuxSIFREG | linuxModeBits(nodeMode)
	}
	nlink := uint32(1)
	if node.isDir() {
		nlink = maxU32(2, 2+uint32(len(node.entries)))
	}
	attrUID, attrGID := node.uid, node.gid
	if p.mapOwner {
		attrUID, attrGID = p.ownerUID, p.ownerGID
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
		UID:       attrUID,
		GID:       attrGID,
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

func (p *imageFS) createAbstractNode(parent *imageNode, name string, entry imagefs.Entry) (*imageNode, int32) {
	if parent.whiteouts[name] {
		return nil, -linuxENOENT
	}
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

func (p *imageFS) materializeDirEntriesLocked(node *imageNode) ([]imagefs.DirEnt, int32) {
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
		if node.whiteouts[ent.Name] {
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
	node.entriesDone = true
	return ents, 0
}

func (p *imageFS) copyUpFileLocked(node *imageNode) int32 {
	if node == nil {
		return -linuxENOENT
	}
	if node.abstractDir != nil {
		return -linuxEISDIR
	}
	if node.abstractLink != nil {
		return -linuxEINVAL
	}
	if node.abstractFile == nil {
		return 0
	}
	size, mode := node.abstractFile.Stat()
	data, err := node.abstractFile.ReadAt(0, uint32(size))
	if err != nil {
		return errnoFromError(err)
	}
	node.data = append(node.data[:0], data...)
	node.size = uint64(len(node.data))
	node.mode = mode
	node.abstractFile = nil
	if node.modTime.IsZero() {
		node.modTime = time.Now()
	}
	return 0
}

func (n *imageNode) isDir() bool {
	return n.abstractDir != nil || n.mode&fs.ModeDir != 0
}

func (n *imageNode) isSymlink() bool {
	return n.abstractLink != nil || n.mode&fs.ModeSymlink != 0
}

const (
	linuxSIFMT    = linuxabi.SIFMT
	linuxSIFSOCK  = linuxabi.SIFSOCK
	linuxSIFLNK   = linuxabi.SIFLNK
	linuxSIFREG   = linuxabi.SIFREG
	linuxSIFBLK   = linuxabi.SIFBLK
	linuxSIFDIR   = linuxabi.SIFDIR
	linuxSIFCHR   = linuxabi.SIFCHR
	linuxSIFIFO   = linuxabi.SIFIFO
	linuxPermMask = linuxabi.PermMask
)

const (
	linuxEPERM     = linuxabi.EPERM
	linuxENOENT    = linuxabi.ENOENT
	linuxENXIO     = linuxabi.ENXIO
	linuxEIO       = linuxabi.EIO
	linuxEBADF     = linuxabi.EBADF
	linuxEPIPE     = linuxabi.EPIPE
	linuxEEXIST    = linuxabi.EEXIST
	linuxENOTDIR   = linuxabi.ENOTDIR
	linuxEISDIR    = linuxabi.EISDIR
	linuxEINVAL    = linuxabi.EINVAL
	linuxENOTTY    = linuxabi.ENOTTY
	linuxEFBIG     = linuxabi.EFBIG
	linuxERANGE    = linuxabi.ERANGE
	linuxENOSYS    = linuxabi.ENOSYS
	linuxENOTEMPTY = linuxabi.ENOTEMPTY
	linuxENODATA   = linuxabi.ENODATA
	linuxETIMEDOUT = linuxabi.ETIMEDOUT
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
		if errno, ok := mapHostError(pathErr.Err); ok {
			return -errno
		}
	}
	if errno, ok := mapHostError(err); ok {
		return -errno
	}
	return -linuxEIO
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

func translateLinuxOpenFlags(flags uint32, writebackCache bool) int {
	openFlags := 0
	switch flags & 0x3 {
	case linuxOWRONLY:
		if writebackCache {
			openFlags |= os.O_RDWR
		} else {
			openFlags |= os.O_WRONLY
		}
	case linuxORDWR:
		openFlags |= os.O_RDWR
	default:
		openFlags |= os.O_RDONLY
	}
	if flags&linuxOCREAT != 0 {
		openFlags |= os.O_CREATE
	}
	if flags&linuxOEXCL != 0 {
		openFlags |= os.O_EXCL
	}
	if flags&linuxOTRUNC != 0 {
		openFlags |= os.O_TRUNC
	}
	if flags&linuxOAPPEND != 0 {
		openFlags |= os.O_APPEND
	}
	return openFlags
}

func align8(n int) int {
	return (n + 7) &^ 7
}
