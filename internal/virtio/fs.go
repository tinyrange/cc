package virtio

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
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

	fsCfgTagSize           = 36
	fsCfgNumQueueOff       = fsCfgTagSize
	fsCfgTotalSize         = fsCfgTagSize + 4
	fsInterruptVring       = 0x1
	fsAvailNoInterrupt     = 0x1
	virtioStatusFeaturesOK = 0x8
	featureRingEventIdx    = uint64(1) << 29
	fuseInHeaderSize       = 40
	fuseOutHeaderSize      = 16
	fuseAttrSize           = 88
	fuseEntryOutSize       = 40 + fuseAttrSize
	fuseAttrOutSize        = 16 + fuseAttrSize
	fuseOpenOutSize        = 16
	fuseInitOutSize        = 40
	fuseStatfsOutSize      = 80
	fuseStatxOutSize       = 288
	fuseDirentBaseSize     = 24
	fuseWriteOutSize       = 8
	fuseLKInSize           = 48
	fuseLKOutSize          = 24
	linuxFUnlck            = 2
)

const (
	fuseLookup      = 1
	fuseForget      = 2
	fuseGetAttr     = 3
	fuseSetAttr     = 4
	fuseReadlink    = 5
	fuseSymlink     = 6
	fuseMknod       = 8
	fuseMkdir       = 9
	fuseUnlink      = 10
	fuseRmDir       = 11
	fuseRename      = 12
	fuseLink        = 13
	fuseOpen        = 14
	fuseRead        = 15
	fuseWrite       = 16
	fuseStatfs      = 17
	fuseRelease     = 18
	fuseFsync       = 20
	fuseSetXattr    = 21
	fuseGetXattr    = 22
	fuseListXattr   = 23
	fuseRemoveXattr = 24
	fuseFlush       = 25
	fuseInit        = 26
	fuseOpenDir     = 27
	fuseReadDir     = 28
	fuseReleaseDir  = 29
	fuseFsyncDir    = 30
	fuseGetLK       = 31
	fuseSetLK       = 32
	fuseSetLKW      = 33
	fuseAccess      = 34
	fuseCreate      = 35
	fuseDestroy     = 38
	fuseIoctl       = 39
	fusePoll        = 40
	fuseRename2     = 45
	fuseLseek       = 46
	fuseSyncFS      = 50
	fuseTmpfile     = 51
	fuseStatx       = 52
)

const (
	fattrMode     = 1 << 0
	fattrUID      = 1 << 1
	fattrGID      = 1 << 2
	fattrSize     = 1 << 3
	fattrATime    = 1 << 4
	fattrMTime    = 1 << 5
	fattrFH       = 1 << 6
	fattrATimeNow = 1 << 7
	fattrMTimeNow = 1 << 8
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
	linuxRenameNoReplace = 1
	linuxRenameExchange  = 2
)

const (
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

type fsXattrMutationBackend interface {
	SetXattr(nodeID uint64, name string, value []byte, flags uint32) int32
	RemoveXattr(nodeID uint64, name string) int32
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

type fsLinkBackend interface {
	Link(nodeID uint64, newParent uint64, newName string) (newNodeID uint64, attr FuseAttr, errno int32)
}

type fsLinkCallerBackend interface {
	LinkForCaller(nodeID uint64, newParent uint64, newName string, uid uint32, gid uint32) (newNodeID uint64, attr FuseAttr, errno int32)
}

type fsRmDirBackend interface {
	RmDir(parent uint64, name string) int32
}

type fsRmDirCallerBackend interface {
	RmDirForCaller(parent uint64, name string, uid uint32, gid uint32) int32
}

type fsCreateBackend interface {
	Create(parent uint64, name string, flags uint32, mode uint32, uid uint32, gid uint32) (nodeID uint64, fh uint64, attr FuseAttr, errno int32)
}

type fsOpenCallerBackend interface {
	OpenForCaller(nodeID uint64, flags uint32, uid uint32, gid uint32) (uint64, int32)
}

type fsCreateCallerBackend interface {
	CreateForCaller(parent uint64, name string, flags uint32, mode uint32, uid uint32, gid uint32) (nodeID uint64, fh uint64, attr FuseAttr, errno int32)
}

type fsWriteBackend interface {
	Write(nodeID uint64, fh uint64, off uint64, data []byte, flags uint32) (uint32, int32)
}

type fsWriteCallerBackend interface {
	WriteForCaller(nodeID uint64, fh uint64, off uint64, data []byte, flags uint32, uid uint32, gid uint32) (uint32, int32)
}

type fsSetAttrBackend interface {
	SetAttr(nodeID uint64, valid uint32, fh uint64, size uint64, mode uint32, uid uint32, gid uint32, atime time.Time, mtime time.Time) (FuseAttr, int32)
}

type fsSetAttrCallerBackend interface {
	SetAttrForCaller(nodeID uint64, valid uint32, fh uint64, size uint64, mode uint32, uid uint32, gid uint32, atime time.Time, mtime time.Time, callerUID uint32, callerGID uint32) (FuseAttr, int32)
}

type fsUnlinkBackend interface {
	Unlink(parent uint64, name string) int32
}

type fsUnlinkCallerBackend interface {
	UnlinkForCaller(parent uint64, name string, uid uint32, gid uint32) int32
}

type fsRenameBackend interface {
	Rename(parent uint64, name string, newParent uint64, newName string, flags uint32) int32
}

type fsRenameCallerBackend interface {
	RenameForCaller(parent uint64, name string, newParent uint64, newName string, flags uint32, uid uint32, gid uint32) int32
}

type fsWritebackCacheBackend interface {
	SetWritebackCache(enabled bool)
}

type fsCachePolicyBackend interface {
	CachePolicy(nodeID uint64) FSCachePolicy
}

type FSCachePolicy struct {
	Mode     string
	EntryTTL time.Duration
	AttrTTL  time.Duration
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
	directMemory   bool
	eventIdx       bool
	kickPoll       bool
	kickPollIdle   time.Duration
	kickPollSleep  time.Duration
	kickPollActive bool
	workerCount    int
	entryTTL       time.Duration
	attrTTL        time.Duration

	mu                  sync.Mutex
	workerOnce          sync.Once
	mem                 GuestMemory
	irq                 IRQController
	backend             FSBackend
	backingUsageTracker *FSBackingUsageTracker
	tag                 [fsCfgTagSize]byte
	deviceFeatureSel    uint32
	driverFeatureSel    uint32
	driverFeatures      uint64
	sharedMemorySel     uint32
	queueSel            uint32
	status              uint32
	interruptStatus     uint32
	irqHigh             bool
	configGeneration    uint32
	queues              []queue
	mmioReads           uint64
	mmioWrites          uint64
	queueNotifies       []uint64
	kickPollLoops       uint64
	kickPollHits        uint64
	kickPollMisses      uint64
	kickPollWorks       uint64
	fuseRequests        atomic.Uint64
	interruptRaises     uint64
	irqTransitions      uint64
	closeOnce           sync.Once
	closed              chan struct{}
	workerWG            sync.WaitGroup
	kickPollWG          sync.WaitGroup
	workCh              chan *fsWork
	nextWorkSeq         []uint64
	nextCompleteSeq     []uint64
	completions         map[fsCompletionKey]fsCompletion
	fuseOpStats         [fuseStatsSlots]fuseOpStat
	stageStats          [fsStageCount]timingStat
	scratch16           [16]byte
	scratch8            [8]byte
	scratch4            [4]byte
	scratch2            [2]byte
}

const fuseStatsSlots = 64
const fsStageCount = 4
const fsInlineRespDescs = 32
const fsPooledReqSize = 4096
const fsWorkQueueDepth = 128

const (
	fsStageQueueHarvest = iota
	fsStageInlineDispatch
	fsStageInlineComplete
	fsStageAsyncComplete
)

type fsWork struct {
	qidx       int
	head       uint16
	seq        uint64
	generation uint32
	unique     uint64
	opcode     uint32
	req        []byte
	pooledReq  bool
	respCount  int
	respDescs  [fsInlineRespDescs]fsDesc
	respExtra  []fsDesc
}

type fsCompletionKey struct {
	qidx int
	seq  uint64
}

type fsCompletion struct {
	work  fsWork
	reply fsReply
	err   error
}

type fsInlineCompletion struct {
	work  fsWork
	reply fsReply
	err   error
}

var fsReqPool = sync.Pool{
	New: func() any {
		return make([]byte, fsPooledReqSize)
	},
}

type FSStats struct {
	Tag                           string        `json:"tag"`
	Async                         bool          `json:"async"`
	WorkerCount                   int           `json:"worker_count"`
	CacheMode                     string        `json:"cache_mode"`
	WritebackCache                bool          `json:"writeback_cache"`
	EventIdx                      bool          `json:"event_idx"`
	MMIOReads                     uint64        `json:"mmio_reads"`
	MMIOWrites                    uint64        `json:"mmio_writes"`
	QueueNotifies                 []uint64      `json:"queue_notifies"`
	KickPollLoops                 uint64        `json:"kick_poll_loops"`
	KickPollHits                  uint64        `json:"kick_poll_hits"`
	KickPollMisses                uint64        `json:"kick_poll_misses"`
	KickPollWorks                 uint64        `json:"kick_poll_works"`
	FUSERequests                  uint64        `json:"fuse_requests"`
	InterruptRaises               uint64        `json:"interrupt_raises"`
	IRQTransitions                uint64        `json:"irq_transitions"`
	IRQHigh                       bool          `json:"irq_high"`
	InterruptStatus               uint32        `json:"interrupt_status"`
	QueueReady                    []bool        `json:"queue_ready"`
	QueueLastAvail                []uint16      `json:"queue_last_avail"`
	QueueAvailIdx                 []uint16      `json:"queue_avail_idx"`
	QueueUsedIdx                  []uint16      `json:"queue_used_idx"`
	QueueNoNotify                 []bool        `json:"queue_no_notify"`
	FUSEOps                       []FUSEOpStats `json:"fuse_ops"`
	Stages                        []TimingStats `json:"stages"`
	BackingBytes                  uint64        `json:"backing_bytes,omitempty"`
	BackingHighWaterBytes         uint64        `json:"backing_high_water_bytes,omitempty"`
	BackingPhysicalBytes          uint64        `json:"backing_physical_bytes,omitempty"`
	BackingMetadataBytes          uint64        `json:"backing_metadata_bytes,omitempty"`
	BackingMetadataHighWaterBytes uint64        `json:"backing_metadata_high_water_bytes,omitempty"`
	BackingReclaimError           string        `json:"backing_reclaim_error,omitempty"`
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

type guestMemorySlicer interface {
	SliceIPA(addr uint64, size int) ([]byte, error)
}

func NewFS(base, size uint64, irq uint32, tag string, backend FSBackend) *FS {
	cacheMode, entryTTL, attrTTL := resolveFSCachePolicy()
	fs := &FS{
		Base:           base,
		Size:           size,
		IRQ:            irq,
		backend:        backend,
		Async:          resolveVirtioFSAsync(),
		cacheMode:      cacheMode,
		writebackCache: strings.TrimSpace(os.Getenv("CCX3_VIRTIOFS_WRITEBACK")) != "",
		directMemory:   resolveVirtioFSDirectMemory(),
		eventIdx:       resolveVirtioFSEventIdx(),
		kickPoll:       resolveVirtioFSKickPoll(),
		kickPollIdle:   resolveVirtioFSKickPollDuration("CCX3_VIRTIOFS_KICK_POLL_IDLE", 500*time.Microsecond),
		kickPollSleep:  resolveVirtioFSKickPollDuration("CCX3_VIRTIOFS_KICK_POLL_SLEEP", 5*time.Microsecond),
		workerCount:    resolveVirtioFSWorkerCount(),
		entryTTL:       entryTTL,
		attrTTL:        attrTTL,
		closed:         make(chan struct{}),
		// A virtqueue can expose at most 128 heads in one notification. Queue
		// pointers so an idle device does not reserve the large inline descriptor
		// array in every fsWork slot.
		workCh:      make(chan *fsWork, fsWorkQueueDepth),
		completions: make(map[fsCompletionKey]fsCompletion),
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

func (f *FS) Close() error {
	if f == nil {
		return nil
	}
	closedNow := false
	f.closeOnce.Do(func() {
		close(f.closed)
		closedNow = true
	})
	if closedNow {
		f.mu.Lock()
		f.kickPoll = false
		f.kickPollActive = false
		f.configGeneration++
		f.mu.Unlock()
	}
	f.workerWG.Wait()
	f.kickPollWG.Wait()
	for {
		select {
		case work := <-f.workCh:
			putFSReqBuffer(work.req, work.pooledReq)
		default:
			f.mu.Lock()
			irq := f.irq
			f.irq = nil
			f.mem = nil
			f.clearQueueCachesLocked()
			f.interruptStatus = 0
			f.irqHigh = false
			f.mu.Unlock()
			if irq != nil {
				_ = irq.SetIRQ(f.IRQ, false)
			}
			if closedNow {
				if closer, ok := f.backend.(interface{ Close() error }); ok {
					return closer.Close()
				}
			}
			return nil
		}
	}
}

func resolveVirtioFSKickPoll() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CCX3_VIRTIOFS_KICK_POLL"))) {
	case "1", "true", "yes", "on":
		return true
	case "", "0", "false", "no", "off":
		return false
	default:
		return false
	}
}

func resolveVirtioFSEventIdx() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CCX3_VIRTIOFS_EVENT_IDX"))) {
	case "1", "true", "yes", "on":
		return true
	case "", "0", "false", "no", "off":
		return false
	default:
		return false
	}
}

func resolveVirtioFSKickPollDuration(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed < 0 {
		return fallback
	}
	return parsed
}

func resolveVirtioFSDirectMemory() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CCX3_VIRTIOFS_DIRECT_MEMORY"))) {
	case "", "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func resolveVirtioFSAsync() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CCX3_VIRTIOFS_ASYNC"))) {
	case "", "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return true
	}
}

func resolveVirtioFSWorkerCount() int {
	const maxWorkers = 64
	// One worker per device preserves concurrency between VMs without retaining
	// GOMAXPROCS-sized worker sets for every mostly idle guest. Workloads that
	// benefit from parallel requests within one guest can raise this explicitly.
	workers := 1
	if value := strings.TrimSpace(os.Getenv("CCX3_VIRTIOFS_WORKERS")); value != "" {
		if parsed, err := strconv.Atoi(value); err == nil {
			workers = parsed
		}
	}
	if workers < 1 {
		return 1
	}
	if workers > maxWorkers {
		return maxWorkers
	}
	return workers
}

func resolveFSCachePolicy() (string, time.Duration, time.Duration) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("CCX3_VIRTIOFS_CACHE"))) {
	case fsCacheStrict:
		return fsCacheStrict, 0, 0
	case fsCacheAggressive:
		return fsCacheAggressive, 60 * time.Second, 60 * time.Second
	default:
		return fsCacheNormal, 0, 0
	}
}

func cachePolicyForMode(mode string) FSCachePolicy {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case fsCacheStrict:
		return FSCachePolicy{Mode: fsCacheStrict}
	case fsCacheAggressive:
		return FSCachePolicy{Mode: fsCacheAggressive, EntryTTL: 60 * time.Second, AttrTTL: 60 * time.Second}
	default:
		return FSCachePolicy{Mode: fsCacheNormal}
	}
}

func (f *FS) cachePolicy(nodeID uint64) FSCachePolicy {
	if be, ok := f.backend.(fsCachePolicyBackend); ok {
		policy := be.CachePolicy(nodeID)
		if policy.Mode != "" {
			return policy
		}
	}
	return FSCachePolicy{Mode: f.cacheMode, EntryTTL: f.entryTTL, AttrTTL: f.attrTTL}
}

func (f *FS) Attach(mem GuestMemory, irq IRQController) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.mem = mem
	f.irq = irq
	f.clearQueueCachesLocked()
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
			return truncateValue(f.deviceFeaturesLocked(), size), nil
		}
		if f.deviceFeatureSel == 1 {
			return truncateValue(f.deviceFeaturesLocked()>>32, size), nil
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
		if f.Log != nil {
			f.logf("mmio-read interrupt-status size=%d value=%#x", size, f.interruptStatus)
		}
		return truncateValue(uint64(f.interruptStatus), size), nil
	case regStatus:
		if f.Log != nil {
			f.logf("mmio-read status size=%d value=%#x", size, f.status)
		}
		return truncateValue(uint64(f.status), size), nil
	case regSharedMemoryLenLow, regSharedMemoryLenHigh:
		return truncateValue(^uint64(0), size), nil
	case regSharedMemoryBaseLow, regSharedMemoryBaseHigh:
		return truncateValue(^uint64(0), size), nil
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
	if f.Log != nil {
		switch offset {
		case regQueueSel, regQueueNum, regQueueReady, regInterruptAck, regStatus:
			f.logf("mmio-write off=%#x size=%d value=%#x", offset, size, value)
		}
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
	case regSharedMemorySel:
		f.sharedMemorySel = uint32(value)
	case regQueueSel:
		f.queueSel = uint32(value)
	case regQueueNum:
		if q := f.selectedQueueLocked(); q != nil {
			q.size = uint16(value)
			q.clearCache()
		}
	case regQueueReady:
		if q := f.selectedQueueLocked(); q != nil {
			q.ready = value != 0
			if value == 0 {
				q.lastAvailIdx = 0
				q.usedIdx = 0
				q.noNotify = false
				q.clearCache()
			} else if f.driverFeatures&featureRingEventIdx != 0 {
				if err := f.ensureQueueCacheLocked(q); err != nil {
					f.mu.Unlock()
					return err
				}
				if err := f.writeAvailEventLocked(q); err != nil {
					f.mu.Unlock()
					return err
				}
			}
		}
	case regQueueDescLow:
		if q := f.selectedQueueLocked(); q != nil {
			f.setQueueAddr(&q.descAddr, uint32(value), true)
			q.clearCache()
		}
	case regQueueDescHigh:
		if q := f.selectedQueueLocked(); q != nil {
			f.setQueueAddr(&q.descAddr, uint32(value), false)
			q.clearCache()
		}
	case regQueueAvailLow:
		if q := f.selectedQueueLocked(); q != nil {
			f.setQueueAddr(&q.availAddr, uint32(value), true)
			q.clearCache()
		}
	case regQueueAvailHigh:
		if q := f.selectedQueueLocked(); q != nil {
			f.setQueueAddr(&q.availAddr, uint32(value), false)
			q.clearCache()
		}
	case regQueueUsedLow:
		if q := f.selectedQueueLocked(); q != nil {
			f.setQueueAddr(&q.usedAddr, uint32(value), true)
			q.clearCache()
		}
	case regQueueUsedHigh:
		if q := f.selectedQueueLocked(); q != nil {
			f.setQueueAddr(&q.usedAddr, uint32(value), false)
			q.clearCache()
		}
	case regInterruptAck:
		if f.Log != nil {
			f.logf("interrupt-ack value=%#x", value)
		}
		f.interruptStatus &^= uint32(value)
		err := f.updateIRQLocked()
		f.mu.Unlock()
		return err
	case regStatus:
		status := uint32(value)
		if status&virtioStatusFeaturesOK != 0 && f.driverFeatures&^f.deviceFeaturesLocked() != 0 {
			status &^= virtioStatusFeaturesOK
		}
		f.status = status
		if f.status == 0 {
			f.resetLocked()
		}
	case regQueueNotify:
		if int(value) < len(f.queues) {
			f.queueNotifies[int(value)]++
			harvestStart := time.Now()
			var workScratch [16]fsWork
			works, err := f.processQueueAsyncLocked(int(value), workScratch[:0])
			f.recordStageDuration(fsStageQueueHarvest, time.Since(harvestStart))
			if err != nil {
				f.mu.Unlock()
				return err
			}
			if f.Async && f.kickPoll && f.driverFeatures&featureRingEventIdx == 0 {
				// Keep guest kicks enabled while polling. Suppressing them creates
				// a lost-wakeup window when the poller becomes idle while the guest
				// is posting a descriptor based on the previous no-notify flag.
				f.startKickPollerLocked()
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

	oldUsedIdx := q.usedIdx
	interruptNeeded := false
	availFlags := uint16(0)
	for {
		flags, availIdx, err := f.readAvailHeaderLocked(q)
		if err != nil {
			return err
		}
		availFlags = flags
		for q.lastAvailIdx != availIdx {
			slot := q.lastAvailIdx % q.size
			head, err := f.readAvailRingEntryLocked(q, slot)
			if err != nil {
				return err
			}
			if f.Log != nil {
				f.logf("queue-notify q=%d head=%d", qidx, head)
			}
			usedLen, reply, err := f.handleRequestLocked(q, head)
			if err != nil {
				return err
			}
			if reply {
				if err := f.writeUsedLocked(q, head, usedLen); err != nil {
					return err
				}
				if f.Log != nil {
					f.logf("used-ring q=%d head=%d len=%d", qidx, head, usedLen)
				}
				interruptNeeded = true
			}
			q.lastAvailIdx++
		}
		if f.driverFeatures&featureRingEventIdx == 0 {
			break
		}
		if err := f.writeAvailEventLocked(q); err != nil {
			return err
		}
		_, latestAvailIdx, err := f.readAvailHeaderLocked(q)
		if err != nil {
			return err
		}
		if q.lastAvailIdx == latestAvailIdx {
			break
		}
	}
	if interruptNeeded && f.isCompletingQueue(qidx) && f.shouldInterruptLocked(q, oldUsedIdx, q.usedIdx, availFlags) {
		f.interruptStatus |= fsInterruptVring
		f.interruptRaises++
		if f.Log != nil {
			f.logf("interrupt-raise status=%#x", f.interruptStatus)
		}
		return f.updateIRQLocked()
	}
	return nil
}

func (f *FS) processQueueAsyncLocked(qidx int, works []fsWork) ([]fsWork, error) {
	q := &f.queues[qidx]
	if !q.ready || q.size == 0 || f.mem == nil {
		return nil, nil
	}

	for {
		_, availIdx, err := f.readAvailHeaderLocked(q)
		if err != nil {
			return nil, err
		}
		for q.lastAvailIdx != availIdx {
			slot := q.lastAvailIdx % q.size
			head, err := f.readAvailRingEntryLocked(q, slot)
			if err != nil {
				return nil, err
			}
			if f.Log != nil {
				f.logf("queue-notify q=%d head=%d", qidx, head)
			}
			work, err := f.prepareRequestLocked(qidx, q, head)
			if err != nil {
				return nil, err
			}
			works = append(works, work)
			q.lastAvailIdx++
		}
		if f.driverFeatures&featureRingEventIdx == 0 {
			break
		}
		if err := f.writeAvailEventLocked(q); err != nil {
			return nil, err
		}
		_, latestAvailIdx, err := f.readAvailHeaderLocked(q)
		if err != nil {
			return nil, err
		}
		if q.lastAvailIdx == latestAvailIdx {
			break
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
	reply, err := f.dispatchFUSEReplyLocked(req)
	if err != nil {
		return 0, false, err
	}
	if !reply.ok {
		return 0, false, nil
	}
	var work fsWork
	work.respCount = len(respDescs)
	if len(respDescs) <= len(work.respDescs) {
		copy(work.respDescs[:], respDescs)
	} else {
		work.respExtra = append([]fsDesc(nil), respDescs...)
	}
	if err := f.writeReplyToResponseDescsLocked(work, reply); err != nil {
		return 0, false, err
	}
	return uint32(reply.Len()), true, nil
}

func (f *FS) prepareRequestLocked(qidx int, q *queue, head uint16) (fsWork, error) {
	var prepareStart time.Time
	if f.RecordTiming != nil {
		prepareStart = time.Now()
	}
	seq := f.nextWorkSeq[qidx]
	f.nextWorkSeq[qidx]++
	work := fsWork{qidx: qidx, head: head, seq: seq, generation: f.configGeneration}
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
		work.opcode = binary.LittleEndian.Uint32(req[4:8])
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
	if !prepareStart.IsZero() {
		f.recordOpcodeTiming("prepare", work.opcode, time.Since(prepareStart))
	}
	return work, nil
}

func (f *FS) enqueueWorks(works []fsWork) {
	if len(works) == 0 {
		return
	}
	f.workerOnce.Do(f.startWorkers)
	for i := range works {
		work := new(fsWork)
		*work = works[i]
		select {
		case <-f.closed:
			putFSReqBuffer(work.req, work.pooledReq)
		case f.workCh <- work:
		}
	}
}

func (f *FS) startKickPollerLocked() {
	if f.kickPollActive {
		return
	}
	f.kickPollActive = true
	generation := f.configGeneration
	f.kickPollWG.Add(1)
	go f.runKickPoller(generation)
}

func (f *FS) runKickPoller(generation uint32) {
	defer f.kickPollWG.Done()
	idleUntil := time.Now().Add(f.kickPollIdle)
	for {
		select {
		case <-f.closed:
			f.mu.Lock()
			if generation == f.configGeneration {
				f.kickPollActive = false
			}
			f.mu.Unlock()
			return
		default:
		}
		var allWorks []fsWork
		processed := 0
		stop := false

		f.mu.Lock()
		if generation != f.configGeneration || !f.kickPoll {
			f.kickPollActive = false
			f.mu.Unlock()
			return
		}
		f.kickPollLoops++
		for qidx := fsQueueRequest; qidx < fsQueueRequest+fsRequestQueueCount && qidx < len(f.queues); qidx++ {
			var workScratch [16]fsWork
			works, err := f.processQueueAsyncLocked(qidx, workScratch[:0])
			if err != nil {
				f.logf("kick-poll q=%d: %v", qidx, err)
				continue
			}
			if len(works) == 0 {
				continue
			}
			processed += len(works)
			allWorks = append(allWorks, works...)
		}
		if processed != 0 {
			f.kickPollHits++
			f.kickPollWorks += uint64(processed)
			idleUntil = time.Now().Add(f.kickPollIdle)
		} else if !time.Now().Before(idleUntil) {
			f.kickPollMisses++
			if err := f.setRequestQueueNoNotifyLocked(false); err != nil {
				f.logf("kick-poll clear no-notify: %v", err)
			}
			// Publish the notification re-enable before the final queue check.
			// Without this release/acquire boundary, a guest can observe the old
			// no-notify flag, post work without kicking, and race past the
			// poller's final check, leaving the request asleep indefinitely.
			f.mu.Unlock()
			runtime.Gosched()
			f.mu.Lock()
			if generation != f.configGeneration || !f.kickPoll {
				f.kickPollActive = false
				f.mu.Unlock()
				f.enqueueWorks(allWorks)
				return
			}
			for qidx := fsQueueRequest; qidx < fsQueueRequest+fsRequestQueueCount && qidx < len(f.queues); qidx++ {
				var workScratch [16]fsWork
				works, err := f.processQueueAsyncLocked(qidx, workScratch[:0])
				if err != nil {
					f.logf("kick-poll final q=%d: %v", qidx, err)
					continue
				}
				if len(works) == 0 {
					continue
				}
				processed += len(works)
				allWorks = append(allWorks, works...)
			}
			if processed != 0 {
				f.kickPollHits++
				f.kickPollWorks += uint64(processed)
				idleUntil = time.Now().Add(f.kickPollIdle)
			} else {
				f.kickPollActive = false
				stop = true
			}
		} else {
			f.kickPollMisses++
		}
		f.mu.Unlock()

		f.enqueueWorks(allWorks)
		if stop {
			return
		}
		if processed == 0 && f.kickPollSleep > 0 {
			timer := time.NewTimer(f.kickPollSleep)
			select {
			case <-f.closed:
				timer.Stop()
				f.mu.Lock()
				if generation == f.configGeneration {
					f.kickPollActive = false
				}
				f.mu.Unlock()
				return
			case <-timer.C:
			}
		} else {
			runtime.Gosched()
		}
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
	count := f.workerCount
	if count < 1 {
		count = 1
	}
	for i := 0; i < count; i++ {
		f.workerWG.Add(1)
		go f.runWorker()
	}
}

func (f *FS) runWorker() {
	defer f.workerWG.Done()
	for {
		var work *fsWork
		select {
		case <-f.closed:
			return
		case work = <-f.workCh:
		}
		reply, err := f.dispatchFUSE(work.req)
		putFSReqBuffer(work.req, work.pooledReq)
		work.req = nil
		work.pooledReq = false
		if err != nil {
			f.logf("async-fuse-error q=%d head=%d: %v", work.qidx, work.head, err)
			reply = fuseReply(work.unique, -linuxEIO, nil)
			err = nil
		}
		select {
		case <-f.closed:
			return
		default:
		}
		if err := f.completeWork(*work, reply, err); err != nil {
			f.logf("async-complete-error q=%d head=%d: %v", work.qidx, work.head, err)
		}
	}
}

func (f *FS) completeWork(work fsWork, reply fsReply, workErr error) error {
	defer f.recordStageTiming(fsStageAsyncComplete, time.Now())
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-f.closed:
		return nil
	default:
	}
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
		for index < len(completions) && completions[index].work.qidx == qidx {
			completion := completions[index]
			index++
			if completion.work.generation != f.configGeneration {
				continue
			}
			if completion.err != nil {
				return completion.err
			}
			if !completion.reply.ok {
				continue
			}
			if err := f.writeCompletionUsedLocked(q, completion.work, completion.reply); err != nil {
				return err
			}
			wroteCompletion = true
		}
		if !wroteCompletion || !f.isCompletingQueue(qidx) {
			continue
		}
		shouldInterrupt, err := f.shouldInterruptCompletionLocked(q, oldUsedIdx)
		if err != nil {
			return err
		}
		if shouldInterrupt {
			f.interruptStatus |= fsInterruptVring
			f.interruptRaises++
			if f.Log != nil {
				f.logf("interrupt-raise status=%#x", f.interruptStatus)
			}
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
		if !completion.reply.ok {
			continue
		}
		if err := f.writeCompletionLocked(q, completion.work, completion.reply); err != nil {
			return err
		}
	}
}

func (f *FS) writeCompletionLocked(q *queue, work fsWork, reply fsReply) error {
	oldUsedIdx := q.usedIdx
	if err := f.writeCompletionUsedLocked(q, work, reply); err != nil {
		return err
	}
	if f.isCompletingQueue(work.qidx) {
		shouldInterrupt, err := f.shouldInterruptCompletionLocked(q, oldUsedIdx)
		if err != nil {
			return err
		}
		if !shouldInterrupt {
			return nil
		}
		f.interruptStatus |= fsInterruptVring
		f.interruptRaises++
		if f.Log != nil {
			f.logf("interrupt-raise status=%#x", f.interruptStatus)
		}
		return f.updateIRQLocked()
	}
	return nil
}

func (f *FS) shouldInterruptCompletionLocked(q *queue, oldUsedIdx uint16) (bool, error) {
	if f.driverFeatures&featureRingEventIdx != 0 {
		return f.shouldInterruptLocked(q, oldUsedIdx, q.usedIdx, 0), nil
	}
	flags, _, err := f.readAvailHeaderLocked(q)
	if err != nil {
		return false, err
	}
	return flags&fsAvailNoInterrupt == 0, nil
}

func (f *FS) writeCompletionUsedLocked(q *queue, work fsWork, reply fsReply) error {
	var completeStart time.Time
	if f.RecordTiming != nil {
		completeStart = time.Now()
	}
	if err := f.writeReplyToResponseDescsLocked(work, reply); err != nil {
		return err
	}
	if err := f.writeUsedLocked(q, work.head, uint32(reply.Len())); err != nil {
		return err
	}
	if f.Log != nil {
		f.logf("used-ring q=%d head=%d len=%d", work.qidx, work.head, reply.Len())
	}
	if !completeStart.IsZero() {
		f.recordOpcodeTiming("complete", work.opcode, time.Since(completeStart))
	}
	return nil
}

func (f *FS) writeReplyToResponseDescsLocked(work fsWork, reply fsReply) error {
	if !reply.ok {
		return nil
	}
	reply.EncodeHeader(f.scratch16[:])
	segments := [2][]byte{f.scratch16[:], reply.extra}
	descIndex := 0
	descOffset := uint32(0)
	written := 0
	for _, segment := range segments {
		for len(segment) != 0 {
			if descIndex >= work.respCount {
				return fmt.Errorf("virtio-fs response truncated: need %d have %d", reply.Len(), written)
			}
			desc := work.responseDesc(descIndex)
			if descOffset >= desc.length {
				descIndex++
				descOffset = 0
				continue
			}
			chunk := len(segment)
			space := int(desc.length - descOffset)
			if chunk > space {
				chunk = space
			}
			if err := f.writeIPAFrom(desc.addr+uint64(descOffset), segment[:chunk]); err != nil {
				return err
			}
			segment = segment[chunk:]
			descOffset += uint32(chunk)
			written += chunk
		}
	}
	return nil
}

func (w *fsWork) responseDesc(index int) fsDesc {
	if w.respExtra != nil {
		return w.respExtra[index]
	}
	return w.respDescs[index]
}

func (f *FS) dispatchFUSELocked(req []byte) ([]byte, error) {
	reply, err := f.dispatchFUSEReplyLocked(req)
	if err != nil || !reply.ok {
		return nil, err
	}
	return reply.Bytes(), nil
}

func (f *FS) dispatchFUSEReplyLocked(req []byte) (fsReply, error) {
	return f.dispatchFUSE(req)
}

func (f *FS) dispatchFUSE(req []byte) (fsReply, error) {
	if len(req) < fuseInHeaderSize {
		return fsReply{}, fmt.Errorf("virtio-fs short request: %d", len(req))
	}
	opcode := binary.LittleEndian.Uint32(req[4:8])
	tracker := f.backingUsageTracker
	if fuseMayChangeBacking(opcode) {
		defer tracker.TrackMutation()()
	}
	unique := binary.LittleEndian.Uint64(req[8:16])
	nodeID := binary.LittleEndian.Uint64(req[16:24])
	callerUID := binary.LittleEndian.Uint32(req[24:28])
	callerGID := binary.LittleEndian.Uint32(req[28:32])
	opStart := time.Now()
	defer f.recordFUSEDispatchTiming(opcode, opStart)
	logEnabled := f.Log != nil
	if logEnabled {
		f.logf("opcode=%d unique=%d node=%d", opcode, unique, nodeID)
	}

	reply := func(errno int32, extra []byte) fsReply {
		return fuseReply(unique, errno, extra)
	}

	switch opcode {
	case fuseForget:
		return fsReply{}, nil
	case fuseInit:
		if len(req) < fuseInHeaderSize+16 {
			return fsReply{}, fmt.Errorf("virtio-fs INIT too short")
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
		if logEnabled {
			f.logf("init-reply major=%d minor=%d max_write=%d", replyMajor, replyMinor, maxWrite)
		}
		return reply(0, extra), nil
	case fuseGetAttr:
		if logEnabled {
			f.logPathf("getattr", nodeID, "")
		}
		attr, errno := f.backend.GetAttr(nodeID)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		extra := make([]byte, fuseAttrOutSize)
		encodeFuseAttrTTL(extra[0:16], f.cachePolicy(nodeID).AttrTTL)
		encodeFuseAttr(extra[16:], attr)
		return reply(0, extra), nil
	case fuseSetAttr:
		if len(req) < fuseInHeaderSize+88 {
			return fsReply{}, fmt.Errorf("virtio-fs SETATTR too short")
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
			var attr FuseAttr
			var errno int32
			if callerBE, ok := f.backend.(fsSetAttrCallerBackend); ok {
				attr, errno = callerBE.SetAttrForCaller(nodeID, valid, fh, size, mode, uid, gid, atime, mtime, callerUID, callerGID)
			} else {
				attr, errno = be.SetAttr(nodeID, valid, fh, size, mode, uid, gid, atime, mtime)
			}
			if errno != 0 {
				return reply(errno, nil), nil
			}
			extra := make([]byte, fuseAttrOutSize)
			encodeFuseAttrTTL(extra[0:16], f.cachePolicy(nodeID).AttrTTL)
			encodeFuseAttr(extra[16:], attr)
			return reply(0, extra), nil
		}
		return reply(-linuxENOSYS, nil), nil
	case fuseLookup:
		name := readCStringName(req[fuseInHeaderSize:])
		if logEnabled {
			f.logPathf("lookup-parent", nodeID, fmt.Sprintf(" name=%q", name))
		}
		childID, attr, errno := f.backend.Lookup(nodeID, path.Clean(name))
		if errno != 0 {
			return reply(errno, nil), nil
		}
		if logEnabled {
			f.logPathf("lookup-child", childID, "")
		}
		extra := make([]byte, fuseEntryOutSize)
		f.encodeFuseEntryOut(extra, childID)
		encodeFuseAttr(extra[40:], attr)
		return reply(0, extra), nil
	case fuseMkdir:
		if len(req) < fuseInHeaderSize+8 {
			return fsReply{}, fmt.Errorf("virtio-fs MKDIR too short")
		}
		name := readCStringName(req[fuseInHeaderSize+8:])
		mode := binary.LittleEndian.Uint32(req[40:44])
		if logEnabled {
			f.logPathf("mkdir-parent", nodeID, fmt.Sprintf(" name=%q mode=%#o", name, mode))
		}
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
		return fsReply{}, fmt.Errorf("virtio-fs missing mkdir backend for parent=%d name=%q", nodeID, name)
	case fuseMknod:
		if len(req) < fuseInHeaderSize+16 {
			return fsReply{}, fmt.Errorf("virtio-fs MKNOD too short")
		}
		name := readCStringName(req[fuseInHeaderSize+16:])
		mode := binary.LittleEndian.Uint32(req[40:44])
		rdev := binary.LittleEndian.Uint32(req[44:48])
		if logEnabled {
			f.logPathf("mknod-parent", nodeID, fmt.Sprintf(" name=%q mode=%#o rdev=%#x", name, mode, rdev))
		}
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
			return fsReply{}, fmt.Errorf("virtio-fs SYMLINK malformed payload")
		}
		if logEnabled {
			f.logPathf("symlink-parent", nodeID, fmt.Sprintf(" name=%q target=%q", name, target))
		}
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
			if callerBE, ok := f.backend.(fsUnlinkCallerBackend); ok {
				return reply(callerBE.UnlinkForCaller(nodeID, path.Clean(name), callerUID, callerGID), nil), nil
			}
			return reply(be.Unlink(nodeID, path.Clean(name)), nil), nil
		}
		return reply(-linuxENOSYS, nil), nil
	case fuseOpen:
		if len(req) < fuseInHeaderSize+8 {
			return fsReply{}, fmt.Errorf("virtio-fs OPEN too short")
		}
		flags := binary.LittleEndian.Uint32(req[40:44])
		if logEnabled {
			f.logPathf("open", nodeID, fmt.Sprintf(" flags=%#x", flags))
		}
		var fh uint64
		var errno int32
		if be, ok := f.backend.(fsOpenCallerBackend); ok {
			fh, errno = be.OpenForCaller(nodeID, flags, callerUID, callerGID)
		} else {
			fh, errno = f.backend.Open(nodeID, flags)
		}
		if errno != 0 {
			return reply(errno, nil), nil
		}
		extra := make([]byte, fuseOpenOutSize)
		binary.LittleEndian.PutUint64(extra[0:8], fh)
		binary.LittleEndian.PutUint32(extra[8:12], f.openResponseFlags(nodeID, flags, false))
		return reply(0, extra), nil
	case fuseRead:
		if len(req) < fuseInHeaderSize+24 {
			return fsReply{}, fmt.Errorf("virtio-fs READ too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		off := binary.LittleEndian.Uint64(req[48:56])
		size := binary.LittleEndian.Uint32(req[56:60])
		if logEnabled {
			f.logPathf("read", nodeID, fmt.Sprintf(" fh=%d off=%d size=%d", fh, off, size))
		}
		data, errno := f.backend.Read(nodeID, fh, off, size)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		return reply(0, data), nil
	case fuseWrite:
		if len(req) < fuseInHeaderSize+40 {
			return fsReply{}, fmt.Errorf("virtio-fs WRITE too short")
		}
		if be, ok := f.backend.(fsWriteBackend); ok {
			fh := binary.LittleEndian.Uint64(req[40:48])
			off := binary.LittleEndian.Uint64(req[48:56])
			size := binary.LittleEndian.Uint32(req[56:60])
			writeFlags := binary.LittleEndian.Uint32(req[60:64])
			dataStart := fuseInHeaderSize + 40
			if len(req) < dataStart+int(size) {
				return fsReply{}, fmt.Errorf("virtio-fs WRITE short payload")
			}
			var count uint32
			var errno int32
			if callerBE, ok := f.backend.(fsWriteCallerBackend); ok {
				count, errno = callerBE.WriteForCaller(nodeID, fh, off, req[dataStart:dataStart+int(size)], writeFlags, callerUID, callerGID)
			} else {
				count, errno = be.Write(nodeID, fh, off, req[dataStart:dataStart+int(size)], writeFlags)
			}
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
			return fsReply{}, fmt.Errorf("virtio-fs RELEASE too short")
		}
		if logEnabled {
			f.logPathf("release", nodeID, fmt.Sprintf(" fh=%d", binary.LittleEndian.Uint64(req[40:48])))
		}
		f.backend.Release(nodeID, binary.LittleEndian.Uint64(req[40:48]))
		return reply(0, nil), nil
	case fuseFsync:
		if len(req) < fuseInHeaderSize+16 {
			return fsReply{}, fmt.Errorf("virtio-fs FSYNC too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		flags := binary.LittleEndian.Uint32(req[48:52])
		if logEnabled {
			f.logPathf("fsync", nodeID, fmt.Sprintf(" fh=%d flags=%#x", fh, flags))
		}
		if be, ok := f.backend.(fsFsyncBackend); ok {
			return reply(be.Fsync(nodeID, fh, flags), nil), nil
		}
		if f.Strict {
			return fsReply{}, fmt.Errorf("virtio-fs missing fsync backend for FSYNC node=%d fh=%d", nodeID, fh)
		}
		return reply(0, nil), nil
	case fuseOpenDir:
		if len(req) < fuseInHeaderSize+8 {
			return fsReply{}, fmt.Errorf("virtio-fs OPENDIR too short")
		}
		flags := binary.LittleEndian.Uint32(req[40:44])
		if logEnabled {
			f.logPathf("opendir", nodeID, fmt.Sprintf(" flags=%#x", flags))
		}
		fh, errno := f.backend.OpenDir(nodeID, flags)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		extra := make([]byte, fuseOpenOutSize)
		binary.LittleEndian.PutUint64(extra[0:8], fh)
		binary.LittleEndian.PutUint32(extra[8:12], f.openResponseFlags(nodeID, flags, true))
		return reply(0, extra), nil
	case fuseReadDir:
		if len(req) < fuseInHeaderSize+24 {
			return fsReply{}, fmt.Errorf("virtio-fs READDIR too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		off := binary.LittleEndian.Uint64(req[48:56])
		size := binary.LittleEndian.Uint32(req[56:60])
		if logEnabled {
			f.logPathf("readdir", nodeID, fmt.Sprintf(" fh=%d off=%d size=%d", fh, off, size))
		}
		data, errno := f.backend.ReadDir(nodeID, fh, off, size)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		return reply(0, data), nil
	case fuseReleaseDir:
		if len(req) < fuseInHeaderSize+24 {
			return fsReply{}, fmt.Errorf("virtio-fs RELEASEDIR too short")
		}
		if logEnabled {
			f.logPathf("releasedir", nodeID, fmt.Sprintf(" fh=%d", binary.LittleEndian.Uint64(req[40:48])))
		}
		f.backend.ReleaseDir(nodeID, binary.LittleEndian.Uint64(req[40:48]))
		return reply(0, nil), nil
	case fuseFsyncDir:
		if len(req) < fuseInHeaderSize+16 {
			return fsReply{}, fmt.Errorf("virtio-fs FSYNCDIR too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		flags := binary.LittleEndian.Uint32(req[48:52])
		if logEnabled {
			f.logPathf("fsyncdir", nodeID, fmt.Sprintf(" fh=%d flags=%#x", fh, flags))
		}
		if be, ok := f.backend.(fsFsyncDirBackend); ok {
			return reply(be.FsyncDir(nodeID, fh, flags), nil), nil
		}
		if f.Strict {
			return fsReply{}, fmt.Errorf("virtio-fs missing fsyncdir backend for FSYNCDIR node=%d fh=%d", nodeID, fh)
		}
		return reply(0, nil), nil
	case fuseGetLK:
		if len(req) < fuseInHeaderSize+fuseLKInSize {
			return fsReply{}, fmt.Errorf("virtio-fs GETLK too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		if logEnabled {
			f.logPathf("getlk", nodeID, fmt.Sprintf(" fh=%d", fh))
		}
		return reply(-linuxENOSYS, nil), nil
	case fuseSetLK, fuseSetLKW:
		if len(req) < fuseInHeaderSize+fuseLKInSize {
			return fsReply{}, fmt.Errorf("virtio-fs %s too short", fuseOpcodeName(opcode))
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		lockType := binary.LittleEndian.Uint32(req[72:76])
		if logEnabled {
			f.logPathf(strings.ToLower(fuseOpcodeName(opcode)), nodeID, fmt.Sprintf(" fh=%d type=%d", fh, lockType))
		}
		return reply(-linuxENOSYS, nil), nil
	case fuseRmDir:
		name := readCStringName(req[fuseInHeaderSize:])
		if logEnabled {
			f.logPathf("rmdir-parent", nodeID, fmt.Sprintf(" name=%q", name))
		}
		if be, ok := f.backend.(fsRmDirBackend); ok {
			var errno int32
			if callerBE, ok := f.backend.(fsRmDirCallerBackend); ok {
				errno = callerBE.RmDirForCaller(nodeID, path.Clean(name), callerUID, callerGID)
			} else {
				errno = be.RmDir(nodeID, path.Clean(name))
			}
			return reply(errno, nil), nil
		}
		return fsReply{}, fmt.Errorf("virtio-fs missing rmdir backend for parent=%d name=%q", nodeID, name)
	case fuseRename:
		if len(req) < fuseInHeaderSize+8 {
			return fsReply{}, fmt.Errorf("virtio-fs RENAME too short")
		}
		if be, ok := f.backend.(fsRenameBackend); ok {
			newParent := binary.LittleEndian.Uint64(req[40:48])
			names := req[fuseInHeaderSize+8:]
			split := bytesIndexByte(names, 0)
			if split < 0 {
				return fsReply{}, fmt.Errorf("virtio-fs RENAME missing old name")
			}
			oldName := string(names[:split])
			newName := readCStringName(names[split+1:])
			if callerBE, ok := f.backend.(fsRenameCallerBackend); ok {
				return reply(callerBE.RenameForCaller(nodeID, path.Clean(oldName), newParent, path.Clean(newName), 0, callerUID, callerGID), nil), nil
			}
			return reply(be.Rename(nodeID, path.Clean(oldName), newParent, path.Clean(newName), 0), nil), nil
		}
		return reply(-linuxENOSYS, nil), nil
	case fuseRename2:
		if len(req) < fuseInHeaderSize+16 {
			return fsReply{}, fmt.Errorf("virtio-fs RENAME2 too short")
		}
		if be, ok := f.backend.(fsRenameBackend); ok {
			newParent := binary.LittleEndian.Uint64(req[40:48])
			flags := binary.LittleEndian.Uint32(req[48:52])
			// The in-memory backend can exchange both directory entries
			// atomically, but Linux's mounted FUSE path has exhibited stale
			// dentry aliasing after a successful exchange. Refuse the operation
			// until the kernel-cache coherence contract is implemented; an
			// explicit unsupported result is safer than reporting success after
			// data loss.
			if flags&linuxRenameExchange != 0 {
				// ENOSYS makes Linux permanently disable the entire RENAME2
				// opcode for this mount. Reject only this flag so later
				// RENAME_NOREPLACE requests continue reaching the backend.
				return reply(-linuxEOPNOTSUPP, nil), nil
			}
			names := req[fuseInHeaderSize+16:]
			split := bytesIndexByte(names, 0)
			if split < 0 {
				return fsReply{}, fmt.Errorf("virtio-fs RENAME2 missing old name")
			}
			oldName := string(names[:split])
			newName := readCStringName(names[split+1:])
			if callerBE, ok := f.backend.(fsRenameCallerBackend); ok {
				return reply(callerBE.RenameForCaller(nodeID, path.Clean(oldName), newParent, path.Clean(newName), flags, callerUID, callerGID), nil), nil
			}
			return reply(be.Rename(nodeID, path.Clean(oldName), newParent, path.Clean(newName), flags), nil), nil
		}
		return reply(-linuxENOSYS, nil), nil
	case fuseLink:
		if len(req) < fuseInHeaderSize+8 {
			return fsReply{}, fmt.Errorf("virtio-fs LINK too short")
		}
		if be, ok := f.backend.(fsLinkBackend); ok {
			oldNodeID := binary.LittleEndian.Uint64(req[40:48])
			newParent := nodeID
			newName := readCStringName(req[fuseInHeaderSize+8:])
			var childID uint64
			var attr FuseAttr
			var errno int32
			if callerBE, ok := f.backend.(fsLinkCallerBackend); ok {
				childID, attr, errno = callerBE.LinkForCaller(oldNodeID, newParent, path.Clean(newName), callerUID, callerGID)
			} else {
				childID, attr, errno = be.Link(oldNodeID, newParent, path.Clean(newName))
			}
			if errno != 0 {
				return reply(errno, nil), nil
			}
			extra := make([]byte, fuseEntryOutSize)
			f.encodeFuseEntryOut(extra, childID)
			encodeFuseAttr(extra[40:], attr)
			return reply(0, extra), nil
		}
		return reply(-linuxENOSYS, nil), nil
	case fuseReadlink:
		if logEnabled {
			f.logPathf("readlink", nodeID, "")
		}
		target, errno := f.backend.Readlink(nodeID)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		return reply(0, []byte(target)), nil
	case fuseSetXattr:
		if len(req) < fuseInHeaderSize+8 {
			return fsReply{}, fmt.Errorf("virtio-fs SETXATTR too short")
		}
		size := binary.LittleEndian.Uint32(req[40:44])
		flags := binary.LittleEndian.Uint32(req[44:48])
		payload := req[fuseInHeaderSize+8:]
		split := bytesIndexByte(payload, 0)
		if split < 0 || uint64(split+1)+uint64(size) > uint64(len(payload)) {
			return fsReply{}, fmt.Errorf("virtio-fs SETXATTR malformed payload")
		}
		name := string(payload[:split])
		value := payload[split+1 : split+1+int(size)]
		if be, ok := f.backend.(fsXattrMutationBackend); ok {
			return reply(be.SetXattr(nodeID, name, value, flags), nil), nil
		}
		return reply(-linuxENOSYS, nil), nil
	case fuseGetXattr:
		if len(req) < fuseInHeaderSize+8 {
			return fsReply{}, fmt.Errorf("virtio-fs GETXATTR too short")
		}
		size := binary.LittleEndian.Uint32(req[40:44])
		name := readCStringName(req[fuseInHeaderSize+8:])
		if logEnabled {
			f.logPathf("getxattr", nodeID, fmt.Sprintf(" name=%q size=%d", name, size))
		}
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
			return fsReply{}, fmt.Errorf("virtio-fs missing xattr backend for GETXATTR node=%d", nodeID)
		}
		return reply(-linuxENODATA, nil), nil
	case fuseListXattr:
		if len(req) < fuseInHeaderSize+8 {
			return fsReply{}, fmt.Errorf("virtio-fs LISTXATTR too short")
		}
		if logEnabled {
			f.logPathf("listxattr", nodeID, "")
		}
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
			return fsReply{}, fmt.Errorf("virtio-fs missing xattr backend for LISTXATTR node=%d", nodeID)
		}
		return reply(0, nil), nil
	case fuseRemoveXattr:
		name := readCStringName(req[fuseInHeaderSize:])
		if be, ok := f.backend.(fsXattrMutationBackend); ok {
			return reply(be.RemoveXattr(nodeID, name), nil), nil
		}
		return reply(-linuxENOSYS, nil), nil
	case fuseFlush:
		if len(req) < fuseInHeaderSize+24 {
			return fsReply{}, fmt.Errorf("virtio-fs FLUSH too short")
		}
		fh := binary.LittleEndian.Uint64(req[40:48])
		lockOwner := binary.LittleEndian.Uint64(req[56:64])
		if logEnabled {
			f.logPathf("flush", nodeID, fmt.Sprintf(" fh=%d lockOwner=%d", fh, lockOwner))
		}
		if be, ok := f.backend.(fsFlushBackend); ok {
			return reply(be.Flush(nodeID, fh, lockOwner), nil), nil
		}
		if f.Strict {
			return fsReply{}, fmt.Errorf("virtio-fs missing flush backend for FLUSH node=%d fh=%d", nodeID, fh)
		}
		return reply(0, nil), nil
	case fuseAccess:
		if f.Strict {
			return fsReply{}, fmt.Errorf("virtio-fs unsupported opcode %s node=%d", fuseOpcodeName(opcode), nodeID)
		}
		return reply(-linuxENOSYS, nil), nil
	case fusePoll:
		// Regular files are always ready for I/O and this device has no
		// FUSE_NOTIFY_POLL implementation. ENOSYS makes the kernel remember
		// that poll is unsupported and use its default regular-file mask.
		// Returning an empty mask instead registers a waiter that can never be
		// notified, which stalls io_uring reads indefinitely.
		return reply(-linuxENOSYS, nil), nil
	case fuseLseek:
		if len(req) < fuseInHeaderSize+24 {
			return fsReply{}, fmt.Errorf("virtio-fs LSEEK too short")
		}
		if be, ok := f.backend.(fsLseekBackend); ok {
			fh := binary.LittleEndian.Uint64(req[40:48])
			offset := binary.LittleEndian.Uint64(req[48:56])
			whence := binary.LittleEndian.Uint32(req[56:60])
			if logEnabled {
				f.logPathf("lseek", nodeID, fmt.Sprintf(" fh=%d off=%d whence=%d", fh, offset, whence))
			}
			newOff, errno := be.Lseek(nodeID, fh, offset, whence)
			if errno != 0 {
				return reply(errno, nil), nil
			}
			extra := make([]byte, 8)
			binary.LittleEndian.PutUint64(extra[0:8], newOff)
			return reply(0, extra), nil
		}
		if f.Strict {
			return fsReply{}, fmt.Errorf("virtio-fs missing lseek backend for LSEEK node=%d", nodeID)
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
			return fsReply{}, fmt.Errorf("virtio-fs STATX too short")
		}
		if logEnabled {
			f.logPathf("statx", nodeID, fmt.Sprintf(" mask=%#x", binary.LittleEndian.Uint32(req[60:64])))
		}
		attr, errno := f.backend.GetAttr(nodeID)
		if errno != 0 {
			return reply(errno, nil), nil
		}
		extra := make([]byte, fuseStatxOutSize)
		encodeFuseAttrTTL(extra[0:16], f.cachePolicy(nodeID).AttrTTL)
		encodeFuseStatx(extra[32:], attr)
		return reply(0, extra), nil
	case fuseSyncFS:
		return reply(0, nil), nil
	case fuseTmpfile:
		if logEnabled {
			f.logPathf("tmpfile", nodeID, "")
		}
		return reply(-linuxENOSYS, nil), nil
	case fuseDestroy:
		return reply(0, nil), nil
	case fuseIoctl:
		if logEnabled {
			f.logPathf("ioctl", nodeID, "")
		}
		return reply(-linuxENOTTY, nil), nil
	case fuseCreate:
		if len(req) < fuseInHeaderSize+16 {
			return fsReply{}, fmt.Errorf("virtio-fs CREATE too short")
		}
		if be, ok := f.backend.(fsCreateBackend); ok {
			flags := binary.LittleEndian.Uint32(req[40:44])
			mode := binary.LittleEndian.Uint32(req[44:48])
			name := readCStringName(req[fuseInHeaderSize+16:])
			var childID uint64
			var fh uint64
			var attr FuseAttr
			var errno int32
			if callerBE, ok := f.backend.(fsCreateCallerBackend); ok {
				childID, fh, attr, errno = callerBE.CreateForCaller(nodeID, path.Clean(name), flags, mode, callerUID, callerGID)
			} else {
				childID, fh, attr, errno = be.Create(nodeID, path.Clean(name), flags, mode, callerUID, callerGID)
			}
			if errno != 0 {
				return reply(errno, nil), nil
			}
			extra := make([]byte, fuseEntryOutSize+fuseOpenOutSize)
			f.encodeFuseEntryOut(extra[:fuseEntryOutSize], childID)
			encodeFuseAttr(extra[40:], attr)
			binary.LittleEndian.PutUint64(extra[fuseEntryOutSize:fuseEntryOutSize+8], fh)
			binary.LittleEndian.PutUint32(extra[fuseEntryOutSize+8:fuseEntryOutSize+12], f.openResponseFlags(childID, flags, false))
			return reply(0, extra), nil
		}
		return reply(-linuxENOSYS, nil), nil
	default:
		return reply(-linuxENOSYS, nil), nil
	}
}

func fuseMayChangeBacking(opcode uint32) bool {
	switch opcode {
	case fuseLookup, fuseForget, fuseSetAttr, fuseSymlink, fuseMknod, fuseMkdir,
		fuseUnlink, fuseRmDir, fuseRename, fuseRename2, fuseLink, fuseOpen,
		fuseWrite, fuseRelease, fuseSetXattr, fuseRemoveXattr, fuseOpenDir,
		fuseReleaseDir, fuseCreate, fuseTmpfile, fuseDestroy:
		return true
	default:
		return false
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
	case fuseRename2:
		return "RENAME2"
	case fuseLink:
		return "LINK"
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
	case fuseSetXattr:
		return "SETXATTR"
	case fuseGetXattr:
		return "GETXATTR"
	case fuseListXattr:
		return "LISTXATTR"
	case fuseRemoveXattr:
		return "REMOVEXATTR"
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
	f.recordOpcodeTiming("fuse", opcode, duration)
}

func (f *FS) recordOpcodeTiming(stage string, opcode uint32, duration time.Duration) {
	if f.RecordTiming == nil {
		return
	}
	tag := strings.TrimRight(string(f.tag[:]), "\x00")
	if tag == "" {
		tag = "unknown"
	}
	f.RecordTiming("virtio.fs."+tag+"."+stage+"."+fuseOpcodeMetricName(opcode), duration)
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

type fsReply struct {
	unique uint64
	errno  int32
	extra  []byte
	ok     bool
}

func fuseReply(unique uint64, errno int32, extra []byte) fsReply {
	return fsReply{unique: unique, errno: errno, extra: extra, ok: true}
}

func (r fsReply) Len() int {
	if !r.ok {
		return 0
	}
	return fuseOutHeaderSize + len(r.extra)
}

func (r fsReply) EncodeHeader(dst []byte) {
	binary.LittleEndian.PutUint32(dst[0:4], uint32(r.Len()))
	binary.LittleEndian.PutUint32(dst[4:8], uint32(r.errno))
	binary.LittleEndian.PutUint64(dst[8:16], r.unique)
}

func (r fsReply) Bytes() []byte {
	if !r.ok {
		return nil
	}
	out := make([]byte, r.Len())
	r.EncodeHeader(out[:fuseOutHeaderSize])
	copy(out[fuseOutHeaderSize:], r.extra)
	return out
}

func (f *FS) encodeFuseEntryOut(dst []byte, nodeID uint64) {
	policy := f.cachePolicy(nodeID)
	binary.LittleEndian.PutUint64(dst[0:8], nodeID)
	binary.LittleEndian.PutUint64(dst[8:16], 1)
	encodeFuseTTL(dst[16:24], dst[32:36], policy.EntryTTL)
	encodeFuseTTL(dst[24:32], dst[36:40], policy.AttrTTL)
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

func (f *FS) openResponseFlags(nodeID uint64, openFlags uint32, dir bool) uint32 {
	policy := f.cachePolicy(nodeID)
	flags := uint32(fuseOpenNoFlush)
	if policy.Mode == fsCacheStrict {
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
	if f.mem == nil {
		return fmt.Errorf("guest memory is detached")
	}
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

func (f *FS) writeIPAFrom(addr uint64, src []byte) error {
	if len(src) == 0 {
		return nil
	}
	if f.mem == nil {
		return fmt.Errorf("guest memory is detached")
	}
	return f.mem.WriteIPA(addr, src)
}

func (f *FS) ensureQueueCacheLocked(q *queue) error {
	if q.size == 0 || q.descAddr == 0 || q.availAddr == 0 || q.usedAddr == 0 {
		return nil
	}
	if q.descMem != nil && q.availMem != nil && q.usedMem != nil {
		return nil
	}
	if !f.directMemory {
		return nil
	}
	slicer, ok := f.mem.(guestMemorySlicer)
	if !ok {
		return nil
	}
	descLen := int(q.size) * 16
	availLen := 4 + int(q.size)*2 + 2
	usedLen := 4 + int(q.size)*8 + 2
	descMem, err := slicer.SliceIPA(q.descAddr, descLen)
	if err != nil {
		return err
	}
	availMem, err := slicer.SliceIPA(q.availAddr, availLen)
	if err != nil {
		return err
	}
	usedMem, err := slicer.SliceIPA(q.usedAddr, usedLen)
	if err != nil {
		return err
	}
	q.descMem = descMem
	q.availMem = availMem
	q.usedMem = usedMem
	return nil
}

func (f *FS) readAvailHeaderLocked(q *queue) (flags uint16, idx uint16, err error) {
	if err := f.ensureQueueCacheLocked(q); err != nil {
		return 0, 0, err
	}
	if len(q.availMem) >= 4 {
		return binary.LittleEndian.Uint16(q.availMem[0:2]), binary.LittleEndian.Uint16(q.availMem[2:4]), nil
	}
	if err := f.readIPAInto(q.availAddr, f.scratch4[:]); err != nil {
		return 0, 0, err
	}
	return binary.LittleEndian.Uint16(f.scratch4[0:2]), binary.LittleEndian.Uint16(f.scratch4[2:4]), nil
}

func (f *FS) readAvailRingEntryLocked(q *queue, slot uint16) (uint16, error) {
	offset := 4 + int(slot)*2
	if len(q.availMem) >= offset+2 {
		return binary.LittleEndian.Uint16(q.availMem[offset : offset+2]), nil
	}
	if err := f.readIPAInto(q.availAddr+uint64(offset), f.scratch2[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(f.scratch2[:]), nil
}

func (f *FS) readDescriptorChainLocked(q *queue, head uint16, out []fsDesc) ([]fsDesc, error) {
	if err := f.ensureQueueCacheLocked(q); err != nil {
		return nil, err
	}
	index := head
	for i := uint16(0); i < q.size; i++ {
		if index >= q.size {
			return nil, fmt.Errorf("virtio-fs descriptor index %d out of range", index)
		}
		var raw []byte
		offset := int(index) * 16
		if len(q.descMem) >= offset+16 {
			raw = q.descMem[offset : offset+16]
		} else {
			if err := f.readIPAInto(q.descAddr+uint64(offset), f.scratch16[:]); err != nil {
				return nil, err
			}
			raw = f.scratch16[:]
		}
		desc := fsDesc{
			addr:   binary.LittleEndian.Uint64(raw[0:8]),
			length: binary.LittleEndian.Uint32(raw[8:12]),
			flags:  binary.LittleEndian.Uint16(raw[12:14]),
			next:   binary.LittleEndian.Uint16(raw[14:16]),
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
	if err := f.ensureQueueCacheLocked(q); err != nil {
		return err
	}
	offset := 4 + int(slot)*8
	if len(q.usedMem) >= offset+8 {
		binary.LittleEndian.PutUint32(q.usedMem[offset:offset+4], uint32(head))
		binary.LittleEndian.PutUint32(q.usedMem[offset+4:offset+8], usedLen)
	} else {
		binary.LittleEndian.PutUint32(f.scratch8[0:4], uint32(head))
		binary.LittleEndian.PutUint32(f.scratch8[4:8], usedLen)
		if err := f.writeIPAFrom(q.usedAddr+4+uint64(slot)*8, f.scratch8[:]); err != nil {
			return err
		}
	}
	q.usedIdx++
	if len(q.usedMem) >= 4 {
		binary.LittleEndian.PutUint16(q.usedMem[2:4], q.usedIdx)
		return nil
	}
	binary.LittleEndian.PutUint16(f.scratch2[:], q.usedIdx)
	return f.writeIPAFrom(q.usedAddr+2, f.scratch2[:])
}

func (f *FS) shouldInterruptLocked(q *queue, oldUsedIdx, newUsedIdx, availFlags uint16) bool {
	if oldUsedIdx == newUsedIdx {
		return false
	}
	if f.driverFeatures&featureRingEventIdx == 0 {
		return availFlags&1 == 0
	}
	offset := 4 + int(q.size)*2
	if len(q.availMem) >= offset+2 {
		usedEvent := binary.LittleEndian.Uint16(q.availMem[offset : offset+2])
		return vringNeedEvent(usedEvent, newUsedIdx, oldUsedIdx)
	}
	if err := f.readIPAInto(q.availAddr+uint64(offset), f.scratch2[:]); err != nil {
		if f.Log != nil {
			f.logf("used-event-read-error: %v", err)
		}
		return true
	}
	usedEvent := binary.LittleEndian.Uint16(f.scratch2[:])
	return vringNeedEvent(usedEvent, newUsedIdx, oldUsedIdx)
}

func (f *FS) setRequestQueueNoNotifyLocked(suppress bool) error {
	for qidx := fsQueueRequest; qidx < fsQueueRequest+fsRequestQueueCount && qidx < len(f.queues); qidx++ {
		q := &f.queues[qidx]
		if !q.ready || q.size == 0 {
			continue
		}
		if err := f.setQueueNoNotifyLocked(q, suppress); err != nil {
			return err
		}
	}
	return nil
}

func (f *FS) setQueueNoNotifyLocked(q *queue, suppress bool) error {
	if q.size == 0 || q.usedAddr == 0 || q.noNotify == suppress {
		return nil
	}
	if err := f.ensureQueueCacheLocked(q); err != nil {
		return err
	}
	flags := uint16(0)
	if suppress {
		flags = 1
	}
	if len(q.usedMem) >= 2 {
		binary.LittleEndian.PutUint16(q.usedMem[0:2], flags)
	} else {
		binary.LittleEndian.PutUint16(f.scratch2[:], flags)
		if err := f.writeIPAFrom(q.usedAddr, f.scratch2[:]); err != nil {
			return err
		}
	}
	q.noNotify = suppress
	return nil
}

func (f *FS) writeAvailEventLocked(q *queue) error {
	if q.size == 0 || q.usedAddr == 0 {
		return nil
	}
	if err := f.ensureQueueCacheLocked(q); err != nil {
		return err
	}
	offset := 4 + int(q.size)*8
	if len(q.usedMem) >= offset+2 {
		binary.LittleEndian.PutUint16(q.usedMem[offset:offset+2], q.lastAvailIdx)
		return nil
	}
	binary.LittleEndian.PutUint16(f.scratch2[:], q.lastAvailIdx)
	return f.writeIPAFrom(q.usedAddr+uint64(offset), f.scratch2[:])
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
		if level {
			return f.irq.SetIRQ(f.IRQ, true)
		}
		return nil
	}
	f.irqHigh = level
	f.irqTransitions++
	if f.Log != nil {
		f.logf("set-irq irq=%d level=%v", f.IRQ, level)
	}
	return f.irq.SetIRQ(f.IRQ, level)
}

func (f *FS) IRQAsserted() bool {
	if f == nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.interruptStatus != 0 || f.irqHigh
}

// Poke asks the guest driver to rescan completed filesystem requests. It is a
// recovery path for a guest that went to sleep across an interrupt-suppression
// transition after the host had already published a used-ring entry.
func (f *FS) Poke() error {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.irq == nil {
		return nil
	}
	f.interruptStatus |= fsInterruptVring
	if f.irqHigh {
		if err := f.irq.SetIRQ(f.IRQ, false); err != nil {
			return err
		}
		f.irqHigh = false
		f.irqTransitions++
	}
	return f.updateIRQLocked()
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
	for opcode := range f.fuseOpStats {
		stat := &f.fuseOpStats[opcode].timingStat
		count := stat.count.Load()
		if count == 0 {
			continue
		}
		totalNanos := stat.totalNanos.Load()
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
			MaxNanos:     stat.maxNanos.Load(),
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
	for stage := range f.stageStats {
		if stats, ok := timingStatsSnapshot(fsStageName(stage), &f.stageStats[stage]); ok {
			stages = append(stages, stats)
		}
	}
	stats := FSStats{
		Tag:             tag,
		Async:           f.Async,
		WorkerCount:     f.workerCount,
		CacheMode:       f.cacheMode,
		WritebackCache:  f.writebackCache,
		EventIdx:        f.eventIdx,
		MMIOReads:       f.mmioReads,
		MMIOWrites:      f.mmioWrites,
		QueueNotifies:   append([]uint64(nil), f.queueNotifies...),
		KickPollLoops:   f.kickPollLoops,
		KickPollHits:    f.kickPollHits,
		KickPollMisses:  f.kickPollMisses,
		KickPollWorks:   f.kickPollWorks,
		FUSERequests:    f.fuseRequests.Load(),
		InterruptRaises: f.interruptRaises,
		IRQTransitions:  f.irqTransitions,
		IRQHigh:         f.irqHigh,
		InterruptStatus: f.interruptStatus,
		QueueReady:      f.queueReadySnapshotLocked(),
		QueueLastAvail:  f.queueLastAvailSnapshotLocked(),
		QueueAvailIdx:   f.queueAvailIdxSnapshotLocked(),
		QueueUsedIdx:    f.queueUsedIdxSnapshotLocked(),
		QueueNoNotify:   f.queueNoNotifySnapshotLocked(),
		FUSEOps:         ops,
		Stages:          stages,
	}
	if provider, ok := f.backend.(interface {
		BackingUsage() (uint64, uint64, uint64, error)
	}); ok {
		current, highWater, physical, usageErr := provider.BackingUsage()
		stats.BackingBytes, stats.BackingHighWaterBytes = current, highWater
		stats.BackingPhysicalBytes = physical
		if usageErr != nil {
			stats.BackingReclaimError = usageErr.Error()
		}
	}
	if provider, ok := f.backend.(interface{ BackingMetadataUsage() (uint64, uint64) }); ok {
		stats.BackingMetadataBytes, stats.BackingMetadataHighWaterBytes = provider.BackingMetadataUsage()
	}
	return stats
}

func (f *FS) BackingUsage() (current, highWater, physical uint64, reclaimErr error) {
	if f == nil {
		return 0, 0, 0, nil
	}
	f.mu.Lock()
	backend := f.backend
	f.mu.Unlock()
	provider, ok := backend.(interface {
		BackingUsage() (uint64, uint64, uint64, error)
	})
	if !ok {
		return 0, 0, 0, nil
	}
	return provider.BackingUsage()
}

func (f *FS) BackingUsageTracker() *FSBackingUsageTracker {
	if f == nil {
		return nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.backingUsageTracker
}

func (f *FS) BackingMetadataUsage() (current, highWater uint64) {
	if f == nil {
		return 0, 0
	}
	f.mu.Lock()
	backend := f.backend
	f.mu.Unlock()
	provider, ok := backend.(interface{ BackingMetadataUsage() (uint64, uint64) })
	if !ok {
		return 0, 0
	}
	return provider.BackingMetadataUsage()
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

func (f *FS) queueAvailIdxSnapshotLocked() []uint16 {
	idxs := make([]uint16, len(f.queues))
	for i := range f.queues {
		if !f.queues[i].ready || f.queues[i].size == 0 {
			continue
		}
		_, idx, err := f.readAvailHeaderLocked(&f.queues[i])
		if err == nil {
			idxs[i] = idx
		}
	}
	return idxs
}

func (f *FS) queueUsedIdxSnapshotLocked() []uint16 {
	idxs := make([]uint16, len(f.queues))
	for i := range f.queues {
		idxs[i] = f.queues[i].usedIdx
	}
	return idxs
}

func (f *FS) queueNoNotifySnapshotLocked() []bool {
	flags := make([]bool, len(f.queues))
	for i := range f.queues {
		flags[i] = f.queues[i].noNotify
	}
	return flags
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
	f.sharedMemorySel = 0
	f.queueSel = 0
	f.status = 0
	f.interruptStatus = 0
	f.irqHigh = false
	f.configGeneration++
	f.kickPollActive = false
	f.resetQueueStateLocked()
}

func (f *FS) clearQueueCachesLocked() {
	for i := range f.queues {
		f.queues[i].descMem = nil
		f.queues[i].availMem = nil
		f.queues[i].usedMem = nil
	}
}

func (f *FS) deviceFeaturesLocked() uint64 {
	features := featureVersion1
	if f.eventIdx && !f.kickPoll {
		features |= featureRingEventIdx
	}
	return features
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
	if strings.IndexByte(name, '/') < 0 && name != "." && name != ".." {
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
	ownerUID       uint32
	ownerGID       uint32
	mapOwner       bool

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
	root       string
	dataStore  *imageDataStore
	ownerUID   uint32
	ownerGID   uint32
	mapOwner   bool
	debugPaths []string
	debugLog   io.Writer

	mu                    sync.Mutex
	nextNodeID            uint64
	nextHandle            uint64
	nodes                 map[uint64]*imageNode
	handles               map[uint64]imageHandle
	dirHandles            map[uint64][]dirEntry
	dirHandleNodes        map[uint64]uint64
	xattrBytes            uint64
	metadataHighWater     uint64
	retainedNodes         int
	retainedHandles       int
	retainedDirHandles    int
	retainedEntries       int
	retainedWhiteouts     int
	dynamicMetadata       uint64
	materializations      map[uint64]*imageDirMaterialization
	materializationCtx    context.Context
	materializationCancel context.CancelFunc
	materializationWG     sync.WaitGroup
	closeStart            sync.Once
	closeDone             chan struct{}
	closeErr              error
	closed                bool
}

type imageHandle struct {
	nodeID uint64
	reader io.ReaderAt
	closer io.Closer
}

type imageNode struct {
	id                uint64
	inode             uint64
	parent            uint64
	name              string
	mode              fs.FileMode
	rawMode           uint32
	uid               uint32
	gid               uint32
	rdev              uint32
	size              uint64
	nlink             uint32
	data              sparseImageData
	symlinkTarget     string
	entries           map[string]uint64
	whiteouts         map[string]bool
	retainedEntries   int
	retainedWhiteouts int
	accountedMetadata uint64
	entriesDone       bool
	atime             time.Time
	modTime           time.Time
	ctime             time.Time
	xattrs            map[string][]byte
	abstractFile      imagefs.File
	// lowerFile remains immutable after the first writable operation. data is
	// a page overlay, so metadata changes and small writes do not eagerly copy
	// the lower file or expand its sparse holes.
	lowerFile    imagefs.File
	lowerSize    uint64
	abstractDir  imagefs.Directory
	abstractLink imagefs.Symlink
}

const imageDataPageSize = uint64(4096)

const (
	imageXattrEntryOverhead   = 64
	imageMaxXattrBytesPerNode = 256 << 10
	imageMaxXattrBytes        = 16 << 20
)

// sparseImageData stores only runs of pages which have actually been written.
// Both logical page indexes and allocation tokens are sequential for ordinary
// writes, so an extent avoids one heap map entry per 4 KiB page while retaining
// sparse/random-write semantics.
type sparseImageData struct {
	extents []imageDataExtent
}

type imageDataExtent struct {
	page     uint64
	location uint64
	count    uint64
}

func (d sparseImageData) location(page uint64) (uint64, bool) {
	i := sort.Search(len(d.extents), func(i int) bool {
		return d.extents[i].page+d.extents[i].count > page
	})
	if i >= len(d.extents) || page < d.extents[i].page {
		return 0, false
	}
	extent := d.extents[i]
	return extent.location + page - extent.page, true
}

func (d sparseImageData) nextDataPage(page uint64) (uint64, bool) {
	i := sort.Search(len(d.extents), func(i int) bool {
		return d.extents[i].page+d.extents[i].count > page
	})
	if i >= len(d.extents) {
		return 0, false
	}
	if page < d.extents[i].page {
		return d.extents[i].page, true
	}
	return page, true
}

func (d sparseImageData) nextHolePage(page uint64) uint64 {
	i := sort.Search(len(d.extents), func(i int) bool {
		return d.extents[i].page+d.extents[i].count > page
	})
	if i >= len(d.extents) || page < d.extents[i].page {
		return page
	}
	return d.extents[i].page + d.extents[i].count
}

func (d *sparseImageData) insert(page, location uint64) {
	i := sort.Search(len(d.extents), func(i int) bool { return d.extents[i].page >= page })
	if i > 0 {
		previous := &d.extents[i-1]
		if previous.page+previous.count == page && previous.location+previous.count == location {
			previous.count++
			if i < len(d.extents) && page+1 == d.extents[i].page && location+1 == d.extents[i].location {
				previous.count += d.extents[i].count
				d.extents = append(d.extents[:i], d.extents[i+1:]...)
			}
			return
		}
	}
	if i < len(d.extents) && page+1 == d.extents[i].page && location+1 == d.extents[i].location {
		d.extents[i].page = page
		d.extents[i].location = location
		d.extents[i].count++
		return
	}
	d.extents = append(d.extents, imageDataExtent{})
	copy(d.extents[i+1:], d.extents[i:])
	d.extents[i] = imageDataExtent{page: page, location: location, count: 1}
}

func (d *sparseImageData) replace(page, location uint64) {
	for i, extent := range d.extents {
		if page < extent.page || page >= extent.page+extent.count {
			continue
		}
		rebuilt := make([]imageDataExtent, 0, len(d.extents)+1)
		rebuilt = append(rebuilt, d.extents[:i]...)
		if left := page - extent.page; left != 0 {
			rebuilt = append(rebuilt, imageDataExtent{page: extent.page, location: extent.location, count: left})
		}
		if right := extent.page + extent.count - page - 1; right != 0 {
			rebuilt = append(rebuilt, imageDataExtent{page: page + 1, location: extent.location + (page - extent.page) + 1, count: right})
		}
		rebuilt = append(rebuilt, d.extents[i+1:]...)
		d.extents = rebuilt
		d.insert(page, location)
		return
	}
	d.insert(page, location)
}

func (d sparseImageData) readAt(store *imageDataStore, dst []byte, off uint64) error {
	for len(dst) > 0 {
		pageIndex := off / imageDataPageSize
		pageOffset := off % imageDataPageSize
		n := min(len(dst), int(imageDataPageSize-pageOffset))
		if location, ok := d.location(pageIndex); ok {
			var page [imageDataPageSize]byte
			if err := store.readPage(location, page[:]); err != nil {
				return err
			}
			copy(dst[:n], page[pageOffset:pageOffset+uint64(n)])
		}
		dst = dst[n:]
		off += uint64(n)
	}
	return nil
}

func (d *sparseImageData) writeAt(store *imageDataStore, src []byte, off uint64) (int, error) {
	written := 0
	for len(src) > 0 {
		pageIndex := off / imageDataPageSize
		pageOffset := off % imageDataPageSize
		n := min(len(src), int(imageDataPageSize-pageOffset))
		location, ok := d.location(pageIndex)
		if !ok {
			var page [imageDataPageSize]byte
			copy(page[pageOffset:pageOffset+uint64(n)], src[:n])
			var err error
			location, err = store.allocatePage(page[:])
			if err != nil {
				return written, err
			}
			d.insert(pageIndex, location)
		} else {
			newLocation, err := store.writeAtCOW(location, pageOffset, src[:n])
			if err != nil {
				return written, err
			}
			if newLocation != location {
				d.replace(pageIndex, newLocation)
			}
		}
		src = src[n:]
		off += uint64(n)
		written += n
	}
	return written, nil
}

func (d *sparseImageData) truncate(store *imageDataStore, size uint64) error {
	keepPages := size / imageDataPageSize
	if size%imageDataPageSize != 0 {
		keepPages++
	}
	kept := d.extents[:0]
	for _, extent := range d.extents {
		keep := min(extent.count, max(uint64(0), keepPages-min(keepPages, extent.page)))
		if extent.page >= keepPages {
			keep = 0
		}
		for page := keep; page < extent.count; page++ {
			store.releasePage(extent.location + page)
		}
		if keep != 0 {
			extent.count = keep
			kept = append(kept, extent)
		}
	}
	d.extents = kept
	if size == 0 || size%imageDataPageSize == 0 {
		return nil
	}
	pageIndex := size / imageDataPageSize
	if location, ok := d.location(pageIndex); ok {
		var zero [imageDataPageSize]byte
		newLocation, err := store.writeAtCOW(location, size%imageDataPageSize, zero[:imageDataPageSize-size%imageDataPageSize])
		if err != nil {
			return err
		}
		if newLocation != location {
			d.replace(pageIndex, newLocation)
		}
	}
	return nil
}

func (d sparseImageData) release(store *imageDataStore) {
	for _, extent := range d.extents {
		for page := uint64(0); page < extent.count; page++ {
			store.releasePage(extent.location + page)
		}
	}
}

func (d sparseImageData) allocatedBytes(size uint64) uint64 {
	var allocated uint64
	fullPages := size / imageDataPageSize
	partialBytes := size % imageDataPageSize
	for _, extent := range d.extents {
		if extent.page < fullPages {
			pages := min(extent.count, fullPages-extent.page)
			allocated += pages * imageDataPageSize
		}
		if partialBytes != 0 && extent.page <= fullPages && fullPages-extent.page < extent.count {
			allocated += partialBytes
		}
	}
	return allocated
}

type dirEntry struct {
	name string
	typ  uint32
	ino  uint64
}

func NewPassthroughFS(root string, meta map[string]fsmeta.Entry) FSBackend {
	return newPassthroughFS(root, meta, 0, 0, false)
}

func NewPassthroughFSWithOwner(root string, meta map[string]fsmeta.Entry, uid, gid uint32) FSBackend {
	return newPassthroughFS(root, meta, uid, gid, true)
}

func newPassthroughFS(root string, meta map[string]fsmeta.Entry, uid, gid uint32, mapOwner bool) FSBackend {
	fs := &passthroughFS{
		root:       root,
		meta:       meta,
		ownerUID:   uid,
		ownerGID:   gid,
		mapOwner:   mapOwner,
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
	materializationCtx, materializationCancel := context.WithCancel(context.Background())
	imgFS := &imageFS{
		dataStore:             newImageDataStore(),
		ownerUID:              uid,
		ownerGID:              gid,
		mapOwner:              mapOwner,
		debugPaths:            virtioFSDebugPathsFromEnv(),
		debugLog:              os.Stderr,
		nextNodeID:            2,
		nextHandle:            1,
		nodes:                 map[uint64]*imageNode{},
		handles:               map[uint64]imageHandle{},
		dirHandles:            map[uint64][]dirEntry{},
		dirHandleNodes:        map[uint64]uint64{},
		retainedNodes:         1,
		materializationCtx:    materializationCtx,
		materializationCancel: materializationCancel,
	}
	imgFS.root = imgFS.dataStore.dir
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
	imgFS.refreshImageNodeMetadataLocked(imgFS.nodes[1])
	return imgFS
}

func (p *passthroughFS) Init() (uint32, uint32) {
	return 128 << 10, 0
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
	return p.fileAttr(nodeID, host, info), 0
}

func (p *passthroughFS) Lookup(parent uint64, name string) (uint64, FuseAttr, int32) {
	p.logNode("lookup-parent", parent)
	hostParent, guestParent, errno := p.hostAndGuestPath(parent)
	if errno != 0 {
		return 0, FuseAttr{}, errno
	}
	switch name {
	case ".":
		attr, errno := p.GetAttr(parent)
		return parent, attr, errno
	case "..":
		guestPath := path.Dir(guestParent)
		if guestPath == "." {
			guestPath = "/"
		}
		nodeID := p.ensureNode(guestPath)
		attr, errno := p.GetAttr(nodeID)
		return nodeID, attr, errno
	}
	rel, ok := cleanChildName(name)
	if !ok {
		return 0, FuseAttr{}, -linuxEINVAL
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
	return nodeID, p.fileAttr(nodeID, host, info), 0
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
	return nodeID, p.fileAttr(nodeID, host, info), 0
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
	return nodeID, p.fileAttr(nodeID, host, info), 0
}

func (p *passthroughFS) Link(nodeID uint64, newParent uint64, newName string) (uint64, FuseAttr, int32) {
	host, errno := p.hostPath(nodeID)
	if errno != 0 {
		return 0, FuseAttr{}, errno
	}
	hostParent, guestParent, errno := p.hostAndGuestPath(newParent)
	if errno != 0 {
		return 0, FuseAttr{}, errno
	}
	rel, ok := cleanChildName(newName)
	if !ok {
		return 0, FuseAttr{}, -linuxEINVAL
	}
	dst := filepath.Join(hostParent, filepath.FromSlash(rel))
	if err := os.Link(host, dst); err != nil {
		return 0, FuseAttr{}, errnoFromError(err)
	}
	info, err := os.Lstat(dst)
	if err != nil {
		return 0, FuseAttr{}, errnoFromError(err)
	}
	guestPath := joinGuestChild(guestParent, rel)
	newNodeID := p.ensureNode(guestPath)
	return newNodeID, p.fileAttr(newNodeID, dst, info), 0
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
	return nodeID, handle, p.fileAttr(nodeID, host, info), 0
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
	parentID := nodeID
	if guest != "/" {
		parentID = p.ensureNode(path.Dir(guest))
	}
	dirEntries := []dirEntry{
		{name: ".", typ: dirTypeDir, ino: nodeID},
		{name: "..", typ: dirTypeDir, ino: parentID},
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
	return p.fileAttr(nodeID, host, info), 0
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

func (p *passthroughFS) fileAttr(nodeID uint64, hostPath string, info os.FileInfo) FuseAttr {
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
	enrichHostFileAttr(hostPath, info, &attr)
	if p.mapOwner {
		attr.UID = p.ownerUID
		attr.GID = p.ownerGID
	}
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

func (p *imageFS) DebugPath(nodeID uint64) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pathForNode(nodeID)
}

func (p *imageFS) SnapshotNodePaths() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	ids := make([]int, 0, len(p.nodes))
	for id := range p.nodes {
		ids = append(ids, int(id))
	}
	sort.Ints(ids)
	paths := make([]string, 0, len(ids))
	for _, id := range ids {
		if path := p.pathForNode(uint64(id)); path != "" {
			paths = append(paths, path)
		}
	}
	return paths
}

func (p *imageFS) RestoreNodePaths(paths []string) error {
	for _, nodePath := range paths {
		nodePath = path.Clean("/" + strings.TrimPrefix(nodePath, "/"))
		if nodePath == "/" {
			continue
		}
		if err := p.restoreNodePath(nodePath); err != nil {
			return err
		}
	}
	return nil
}

func (p *imageFS) restoreNodePath(nodePath string) error {
	parentPath, name := path.Split(nodePath)
	parentPath = path.Clean(parentPath)
	if parentPath == "." {
		parentPath = "/"
	}
	parentID, ok := p.nodeIDForPath(parentPath)
	if !ok {
		if err := p.restoreNodePath(parentPath); err != nil {
			return err
		}
		parentID, ok = p.nodeIDForPath(parentPath)
		if !ok {
			return fmt.Errorf("restore imagefs node %q: parent %q was not created", nodePath, parentPath)
		}
	}
	childID, _, errno := p.Lookup(parentID, name)
	if errno != 0 {
		return fmt.Errorf("restore imagefs node %q: lookup errno %d", nodePath, errno)
	}
	if restoredPath := p.DebugPath(childID); restoredPath != nodePath {
		return fmt.Errorf("restore imagefs node %q: got node %d path %q", nodePath, childID, restoredPath)
	}
	return nil
}

func (p *imageFS) nodeIDForPath(nodePath string) (uint64, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id := range p.nodes {
		if p.pathForNode(id) == nodePath {
			return id, true
		}
	}
	return 0, false
}

func virtioFSDebugPathsFromEnv() []string {
	raw := strings.TrimSpace(os.Getenv("CCX3_DEBUG_VIRTIOFS_PATHS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("CCX3_DEBUG_VIRTIOFS_PATH"))
	}
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.HasPrefix(part, "/") {
			part = "/" + part
		}
		paths = append(paths, path.Clean(part))
	}
	return paths
}

func (p *imageFS) debugPathMatchLocked(guestPath string) bool {
	if len(p.debugPaths) == 0 || guestPath == "" {
		return false
	}
	guestPath = path.Clean(guestPath)
	for _, prefix := range p.debugPaths {
		if guestPath == prefix || strings.HasPrefix(guestPath, strings.TrimSuffix(prefix, "/")+"/") {
			return true
		}
	}
	return false
}

func (p *imageFS) debugfLocked(format string, args ...any) {
	if p.debugLog == nil {
		return
	}
	_, _ = fmt.Fprintf(p.debugLog, "virtiofs:image "+format+"\n", args...)
}

func (p *imageFS) debugNodefLocked(op string, nodeID uint64, format string, args ...any) {
	if len(p.debugPaths) == 0 || p.debugLog == nil {
		return
	}
	guestPath := p.pathForNode(nodeID)
	if !p.debugPathMatchLocked(guestPath) {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if msg != "" {
		msg = " " + msg
	}
	p.debugfLocked("%s path=%q node=%d%s", op, guestPath, nodeID, msg)
}

func (p *imageFS) debugChildfLocked(op string, parent uint64, name string, format string, args ...any) {
	if len(p.debugPaths) == 0 || p.debugLog == nil {
		return
	}
	parentPath := p.pathForNode(parent)
	childName, ok := cleanChildName(name)
	if !ok {
		childName = name
	}
	childPath := path.Join(parentPath, childName)
	if !p.debugPathMatchLocked(childPath) && !p.debugPathMatchLocked(parentPath) {
		return
	}
	msg := fmt.Sprintf(format, args...)
	if msg != "" {
		msg = " " + msg
	}
	p.debugfLocked("%s parent=%q name=%q child=%q%s", op, parentPath, name, childPath, msg)
}

func (p *imageFS) Init() (uint32, uint32) {
	return 128 << 10, 0
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
	parentNode := p.nodes[parent]
	if parentNode == nil {
		p.mu.Unlock()
		return 0, FuseAttr{}, -linuxENOENT
	}
	switch name {
	case ".":
		attr := p.attr(parentNode)
		p.mu.Unlock()
		return parentNode.id, attr, 0
	case "..":
		node := p.nodes[parentNode.parent]
		if node == nil {
			p.mu.Unlock()
			return 0, FuseAttr{}, -linuxENOENT
		}
		attr := p.attr(node)
		p.mu.Unlock()
		return node.id, attr, 0
	}
	var ok bool
	name, ok = cleanChildName(name)
	if !ok {
		p.mu.Unlock()
		return 0, FuseAttr{}, -linuxEINVAL
	}
	p.debugChildfLocked("lookup", parent, name, "")
	childID, ok := parentNode.entries[name]
	if !ok {
		if parentNode.whiteouts[name] {
			p.debugChildfLocked("lookup-whiteout", parent, name, "errno=%d", -linuxENOENT)
			p.mu.Unlock()
			return 0, FuseAttr{}, -linuxENOENT
		}
		if parentNode.abstractDir == nil {
			p.debugChildfLocked("lookup-miss", parent, name, "errno=%d", -linuxENOENT)
			p.mu.Unlock()
			return 0, FuseAttr{}, -linuxENOENT
		}
		lowerDir := parentNode.abstractDir
		lowerParent := parentNode
		p.mu.Unlock()
		entry, err := lowerDir.Lookup(name)
		if err != nil {
			errno := errnoFromError(err)
			p.mu.Lock()
			p.debugChildfLocked("lookup-lower-error", parent, name, "errno=%d", errno)
			p.mu.Unlock()
			return 0, FuseAttr{}, errno
		}
		p.mu.Lock()
		parentNode = p.nodes[parent]
		if parentNode == nil {
			p.mu.Unlock()
			return 0, FuseAttr{}, -linuxENOENT
		}
		if parentNode.whiteouts[name] {
			p.debugChildfLocked("lookup-whiteout-after-lower", parent, name, "errno=%d", -linuxENOENT)
			p.mu.Unlock()
			return 0, FuseAttr{}, -linuxENOENT
		}
		if existingID, exists := parentNode.entries[name]; exists {
			child := p.nodes[existingID]
			if child == nil {
				p.debugChildfLocked("lookup-stale-node", parent, name, "node=%d errno=%d", existingID, -linuxENOENT)
				p.mu.Unlock()
				return 0, FuseAttr{}, -linuxENOENT
			}
			attr := p.attr(child)
			p.debugChildfLocked("lookup-raced-hit", parent, name, "node=%d mode=%#o", child.id, attr.Mode)
			p.mu.Unlock()
			return child.id, attr, 0
		}
		if parentNode != lowerParent {
			p.debugChildfLocked("lookup-lower-miss", parent, name, "errno=%d", -linuxENOENT)
			p.mu.Unlock()
			return 0, FuseAttr{}, -linuxENOENT
		}
		child, errno := p.createAbstractNode(parentNode, name, entry)
		if errno != 0 {
			p.debugChildfLocked("lookup-lower-error", parent, name, "errno=%d", errno)
			p.mu.Unlock()
			return 0, FuseAttr{}, errno
		}
		attr := p.attr(child)
		p.debugChildfLocked("lookup-lower-hit", parent, name, "node=%d mode=%#o", child.id, attr.Mode)
		p.mu.Unlock()
		return child.id, attr, 0
	}
	child := p.nodes[childID]
	if child == nil {
		p.debugChildfLocked("lookup-stale-node", parent, name, "node=%d errno=%d", childID, -linuxENOENT)
		p.mu.Unlock()
		return 0, FuseAttr{}, -linuxENOENT
	}
	attr := p.attr(child)
	p.debugChildfLocked("lookup-hit", parent, name, "node=%d mode=%#o", child.id, attr.Mode)
	p.mu.Unlock()
	return child.id, attr, 0
}

func (p *imageFS) Open(nodeID uint64, flags uint32) (uint64, int32) {
	return p.OpenForCaller(nodeID, flags, 0, 0)
}

func (p *imageFS) OpenForCaller(nodeID uint64, flags uint32, uid uint32, gid uint32) (uint64, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return 0, -linuxENOENT
	}
	p.debugNodefLocked("open", nodeID, "flags=%#x", flags)
	if node.isDir() {
		return 0, -linuxEISDIR
	}
	if flags&linuxOACCMODE != linuxORDONLY {
		if errno := p.copyUpFileLocked(node); errno != 0 {
			return 0, errno
		}
		if flags&linuxOTRUNC != 0 {
			node.data.release(p.dataStore)
			node.data = sparseImageData{}
			node.lowerFile = nil
			node.lowerSize = 0
			node.size = 0
			if uid != 0 {
				node.mode &^= fs.FileMode(0o6000)
			}
			now := time.Now()
			node.modTime = now
			node.ctime = now
			p.refreshImageNodeMetadataLocked(node)
		}
	}
	fh := p.nextHandle
	p.nextHandle++
	handle := imageHandle{nodeID: nodeID}
	readerFile := node.abstractFile
	if readerFile == nil {
		readerFile = node.lowerFile
	}
	if openable, ok := readerFile.(imagefs.OpenReaderFile); ok {
		reader, closer, err := openable.OpenReader()
		if err != nil {
			return 0, errnoFromError(err)
		}
		handle.reader = reader
		handle.closer = closer
	}
	p.handles[fh] = handle
	p.noteImageHandleAddedLocked()
	return fh, 0
}

func (p *imageFS) Release(_ uint64, fh uint64) {
	p.mu.Lock()
	handle, ok := p.handles[fh]
	delete(p.handles, fh)
	p.compactImageHandleMapsLocked()
	if ok {
		p.collectImageNodeLocked(handle.nodeID)
	}
	p.mu.Unlock()
	if ok && handle.closer != nil {
		_ = handle.closer.Close()
	}
}

func (p *imageFS) Flush(_ uint64, _ uint64, _ uint64) int32 {
	return 0
}

func (p *imageFS) Fsync(_ uint64, _ uint64, _ uint32) int32 {
	if err := p.dataStore.sync(); err != nil {
		return errnoFromError(err)
	}
	return 0
}

func (p *imageFS) FsyncDir(_ uint64, _ uint64, _ uint32) int32 {
	if err := p.dataStore.sync(); err != nil {
		return errnoFromError(err)
	}
	return 0
}

func (p *imageFS) Read(nodeID uint64, fh uint64, off uint64, size uint32) ([]byte, int32) {
	p.mu.Lock()
	handle, ok := p.handles[fh]
	node := p.nodes[nodeID]
	if !ok || handle.nodeID != nodeID || node == nil {
		p.mu.Unlock()
		return nil, -linuxEBADF
	}
	if node.abstractFile == nil {
		end := off + uint64(size)
		if off >= node.size || size == 0 {
			p.mu.Unlock()
			return []byte{}, 0
		}
		if end > node.size {
			end = node.size
		}
		data := make([]byte, end-off)
		if node.lowerFile != nil && off < node.lowerSize {
			lowerEnd := min(end, node.lowerSize)
			var err error
			if handle.reader != nil {
				var n int
				n, err = handle.reader.ReadAt(data[:lowerEnd-off], int64(off))
				if err != nil && err != io.EOF {
					p.mu.Unlock()
					return nil, errnoFromError(err)
				}
				if uint64(n) != lowerEnd-off {
					p.mu.Unlock()
					return nil, -linuxEIO
				}
			} else {
				var lower []byte
				lower, err = node.lowerFile.ReadAt(off, uint32(lowerEnd-off))
				if err != nil {
					p.mu.Unlock()
					return nil, errnoFromError(err)
				}
				if uint64(len(lower)) != lowerEnd-off {
					p.mu.Unlock()
					return nil, -linuxEIO
				}
				copy(data, lower)
			}
		}
		if err := node.data.readAt(p.dataStore, data, off); err != nil {
			p.mu.Unlock()
			return nil, errnoFromError(err)
		}
		node.atime = time.Now()
		p.mu.Unlock()
		return data, 0
	}
	abstractFile := node.abstractFile
	p.mu.Unlock()
	if handle.reader != nil {
		buf := make([]byte, size)
		n, err := handle.reader.ReadAt(buf, int64(off))
		if err != nil && err != io.EOF {
			return nil, errnoFromError(err)
		}
		p.mu.Lock()
		if current := p.nodes[nodeID]; current != nil {
			current.atime = time.Now()
		}
		p.mu.Unlock()
		return buf[:n], 0
	}
	data, err := abstractFile.ReadAt(off, size)
	if err != nil {
		return nil, errnoFromError(err)
	}
	if data == nil {
		return []byte{}, 0
	}
	p.mu.Lock()
	if current := p.nodes[nodeID]; current != nil {
		current.atime = time.Now()
	}
	p.mu.Unlock()
	return data, 0
}

func (p *imageFS) Lseek(nodeID uint64, fh uint64, offset uint64, whence uint32) (uint64, int32) {
	p.mu.Lock()
	handle, ok := p.handles[fh]
	node := p.nodes[nodeID]
	if !ok || handle.nodeID != nodeID || node == nil {
		p.mu.Unlock()
		return 0, -linuxEBADF
	}
	if offset >= node.size {
		p.mu.Unlock()
		return 0, -linuxENXIO
	}
	if node.abstractFile != nil || node.lowerFile != nil && offset < node.lowerSize {
		size := node.size
		lowerSize := node.lowerSize
		if node.abstractFile != nil {
			lowerSize = size
		}
		if offset < lowerSize {
			p.mu.Unlock()
			if whence == 3 {
				return offset, 0
			}
			if whence == 4 {
				return lowerSize, 0
			}
			return 0, -linuxEINVAL
		}
	}
	switch whence {
	case 3: // SEEK_DATA
		page := offset / imageDataPageSize
		if _, ok := node.data.location(page); ok {
			p.mu.Unlock()
			return offset, 0
		}
		if candidate, ok := node.data.nextDataPage(page + 1); ok && candidate*imageDataPageSize < node.size {
			p.mu.Unlock()
			return candidate * imageDataPageSize, 0
		}
		p.mu.Unlock()
		return 0, -linuxENXIO
	case 4: // SEEK_HOLE
		page := offset / imageDataPageSize
		if _, ok := node.data.location(page); !ok {
			p.mu.Unlock()
			return offset, 0
		}
		if candidate := node.data.nextHolePage(page); candidate*imageDataPageSize < node.size {
			p.mu.Unlock()
			return candidate * imageDataPageSize, 0
		}
		size := node.size
		p.mu.Unlock()
		return size, 0
	default:
		p.mu.Unlock()
		return 0, -linuxEINVAL
	}
}

func (p *imageFS) OpenDir(nodeID uint64, _ uint32) (uint64, int32) {
	if errno := p.materializeDirEntries(nodeID); errno != 0 {
		return 0, errno
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return 0, -linuxENOENT
	}
	if !node.isDir() {
		return 0, -linuxENOTDIR
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
	p.dirHandleNodes[fh] = nodeID
	p.noteImageDirHandleAddedLocked()
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
	nodeID := p.dirHandleNodes[fh]
	delete(p.dirHandles, fh)
	delete(p.dirHandleNodes, fh)
	p.compactImageDirHandleMapsLocked()
	p.collectImageNodeLocked(nodeID)
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

func (p *imageFS) inheritImageCreateLocked(parent *imageNode, mode uint32, gid uint32, directory bool) (uint32, uint32) {
	if parent == nil || linuxModeBits(parent.mode)&0o2000 == 0 {
		return mode, gid
	}
	gid = p.attr(parent).GID
	if directory {
		mode |= 0o2000
	}
	return mode, gid
}

func (p *imageFS) Mkdir(parent uint64, name string, mode uint32, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	if parentNode == nil {
		return 0, FuseAttr{}, -linuxENOENT
	}
	mode, gid = p.inheritImageCreateLocked(parentNode, mode, gid, true)
	var ok bool
	name, ok = cleanChildName(name)
	if !ok {
		return 0, FuseAttr{}, -linuxEINVAL
	}
	p.debugChildfLocked("mkdir", parent, name, "mode=%#o uid=%d gid=%d", mode, uid, gid)
	if _, exists := parentNode.entries[name]; exists {
		p.debugChildfLocked("mkdir-exists", parent, name, "errno=%d", -linuxEEXIST)
		return 0, FuseAttr{}, -linuxEEXIST
	}
	now := time.Now()
	node := &imageNode{
		id:      p.nextNodeID,
		parent:  parent,
		name:    name,
		mode:    fs.ModeDir | fs.FileMode(mode&linuxPermMask),
		uid:     uid,
		gid:     gid,
		entries: map[string]uint64{},
		atime:   now,
		modTime: now,
		ctime:   now,
	}
	p.nextNodeID++
	p.nodes[node.id] = node
	p.noteImageNodeAddedLocked(node)
	p.registerImageNodeLinkLocked(node)
	parentNode.entries[name] = node.id
	p.noteImageEntryAddedLocked(parentNode)
	p.touchImageDirectoryLocked(parentNode, now)
	return node.id, p.attr(node), 0
}

func (p *imageFS) Mknod(parent uint64, name string, mode uint32, rdev uint32, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	if parentNode == nil {
		return 0, FuseAttr{}, -linuxENOENT
	}
	mode, gid = p.inheritImageCreateLocked(parentNode, mode, gid, false)
	var ok bool
	name, ok = cleanChildName(name)
	if !ok {
		return 0, FuseAttr{}, -linuxEINVAL
	}
	fileType := mode & linuxSIFMT
	p.debugChildfLocked("mknod", parent, name, "mode=%#o type=%#o rdev=%d uid=%d gid=%d", mode, fileType, rdev, uid, gid)
	switch fileType {
	case linuxSIFREG, linuxSIFCHR, linuxSIFBLK, linuxSIFIFO, linuxSIFSOCK:
	default:
		p.debugChildfLocked("mknod-invalid-type", parent, name, "mode=%#o errno=%d", mode, -linuxEINVAL)
		return 0, FuseAttr{}, -linuxEINVAL
	}
	if existingID, exists := parentNode.entries[name]; exists {
		existing := p.nodes[existingID]
		if existing == nil {
			p.debugChildfLocked("mknod-stale-existing", parent, name, "existing=%d errno=%d", existingID, -linuxENOENT)
			return 0, FuseAttr{}, -linuxENOENT
		}
		p.debugChildfLocked("mknod-exists", parent, name, "existing=%d existing_dir=%v errno=%d", existingID, existing.isDir(), -linuxEEXIST)
		return 0, FuseAttr{}, -linuxEEXIST
	}
	if parentNode.whiteouts != nil {
		delete(parentNode.whiteouts, name)
	}
	now := time.Now()
	node := &imageNode{
		id:      p.nextNodeID,
		parent:  parent,
		name:    name,
		mode:    linuxModeToGo(fileType | (mode & linuxPermMask)),
		rawMode: fileType | (mode & linuxPermMask),
		uid:     uid,
		gid:     gid,
		rdev:    rdev,
		atime:   now,
		modTime: now,
		ctime:   now,
	}
	p.nextNodeID++
	p.nodes[node.id] = node
	p.noteImageNodeAddedLocked(node)
	p.registerImageNodeLinkLocked(node)
	parentNode.entries[name] = node.id
	p.noteImageEntryAddedLocked(parentNode)
	p.touchImageDirectoryLocked(parentNode, now)
	return node.id, p.attr(node), 0
}

func (p *imageFS) Symlink(parent uint64, name string, target string, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	if parentNode == nil {
		return 0, FuseAttr{}, -linuxENOENT
	}
	_, gid = p.inheritImageCreateLocked(parentNode, 0, gid, false)
	var ok bool
	name, ok = cleanChildName(name)
	if !ok {
		return 0, FuseAttr{}, -linuxEINVAL
	}
	p.debugChildfLocked("symlink", parent, name, "target=%q uid=%d gid=%d", target, uid, gid)
	if _, exists := parentNode.entries[name]; exists {
		p.debugChildfLocked("symlink-exists", parent, name, "errno=%d", -linuxEEXIST)
		return 0, FuseAttr{}, -linuxEEXIST
	}
	now := time.Now()
	node := &imageNode{
		id:            p.nextNodeID,
		parent:        parent,
		name:          name,
		mode:          fs.ModeSymlink | 0o777,
		uid:           uid,
		gid:           gid,
		size:          uint64(len(target)),
		symlinkTarget: target,
		atime:         now,
		modTime:       now,
		ctime:         now,
	}
	p.nextNodeID++
	p.nodes[node.id] = node
	p.noteImageNodeAddedLocked(node)
	parentNode.entries[name] = node.id
	p.noteImageEntryAddedLocked(parentNode)
	p.touchImageDirectoryLocked(parentNode, now)
	return node.id, p.attr(node), 0
}

func (p *imageFS) Link(nodeID uint64, newParent uint64, newName string) (uint64, FuseAttr, int32) {
	return p.LinkForCaller(nodeID, newParent, newName, 0, 0)
}

func (p *imageFS) LinkForCaller(nodeID uint64, newParent uint64, newName string, uid uint32, gid uint32) (uint64, FuseAttr, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	parentNode := p.nodes[newParent]
	if node == nil || parentNode == nil {
		return 0, FuseAttr{}, -linuxENOENT
	}
	if node.isDir() {
		return 0, FuseAttr{}, -linuxEPERM
	}
	name, ok := cleanChildName(newName)
	if !ok {
		return 0, FuseAttr{}, -linuxEINVAL
	}
	p.debugChildfLocked("link", newParent, name, "old_path=%q old_node=%d", p.pathForNode(nodeID), nodeID)
	if _, exists := parentNode.entries[name]; exists {
		p.debugChildfLocked("link-exists", newParent, name, "errno=%d", -linuxEEXIST)
		return 0, FuseAttr{}, -linuxEEXIST
	}
	if parentNode.whiteouts != nil {
		delete(parentNode.whiteouts, name)
	}
	if node.abstractFile != nil {
		if errno := p.copyUpFileLocked(node); errno != 0 {
			return 0, FuseAttr{}, errno
		}
	}
	parentNode.entries[name] = node.id
	p.noteImageEntryAddedLocked(parentNode)
	now := time.Now()
	node.ctime = now
	p.touchImageDirectoryLocked(parentNode, now)
	p.refreshImageNodeLinksLocked(node)
	return node.id, p.attr(node), 0
}

func (p *imageFS) RmDir(parent uint64, name string) int32 {
	return p.RmDirForCaller(parent, name, 0, 0)
}

func (p *imageFS) RmDirForCaller(parent uint64, name string, uid uint32, gid uint32) int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	if parentNode == nil {
		return -linuxENOENT
	}
	var ok bool
	name, ok = cleanChildName(name)
	if !ok {
		return -linuxEINVAL
	}
	p.debugChildfLocked("rmdir", parent, name, "")
	childID, ok := parentNode.entries[name]
	if !ok {
		p.debugChildfLocked("rmdir-miss", parent, name, "errno=%d", -linuxENOENT)
		return -linuxENOENT
	}
	child := p.nodes[childID]
	if child == nil {
		return -linuxENOENT
	}
	if len(child.entries) != 0 {
		p.debugChildfLocked("rmdir-not-empty", parent, name, "node=%d entries=%d errno=%d", childID, len(child.entries), -linuxENOTEMPTY)
		return -linuxENOTEMPTY
	}
	delete(parentNode.entries, name)
	if parentNode.abstractDir != nil {
		if parentNode.whiteouts == nil {
			parentNode.whiteouts = map[string]bool{}
		}
		parentNode.whiteouts[name] = true
		p.noteImageWhiteoutAddedLocked(parentNode)
	}
	p.collectImageNodeLocked(childID)
	p.touchImageDirectoryLocked(parentNode, time.Now())
	p.compactImageNodeMapsLocked(parentNode)
	return 0
}

func (p *imageFS) Create(parent uint64, name string, flags uint32, mode uint32, uid uint32, gid uint32) (uint64, uint64, FuseAttr, int32) {
	return p.CreateForCaller(parent, name, flags, mode, uid, gid)
}

func (p *imageFS) CreateForCaller(parent uint64, name string, flags uint32, mode uint32, uid uint32, gid uint32) (uint64, uint64, FuseAttr, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	if parentNode == nil {
		return 0, 0, FuseAttr{}, -linuxENOENT
	}
	var ok bool
	name, ok = cleanChildName(name)
	if !ok {
		return 0, 0, FuseAttr{}, -linuxEINVAL
	}
	p.debugChildfLocked("create", parent, name, "flags=%#x mode=%#o uid=%d gid=%d", flags, mode, uid, gid)
	if existingID, exists := parentNode.entries[name]; exists {
		if flags&linuxOEXCL != 0 {
			p.debugChildfLocked("create-excl-exists", parent, name, "existing=%d errno=%d", existingID, -linuxEEXIST)
			return 0, 0, FuseAttr{}, -linuxEEXIST
		}
		node := p.nodes[existingID]
		if node == nil {
			p.debugChildfLocked("create-stale-existing", parent, name, "existing=%d errno=%d", existingID, -linuxENOENT)
			return 0, 0, FuseAttr{}, -linuxENOENT
		}
		if node.isDir() {
			p.debugChildfLocked("create-existing-dir", parent, name, "existing=%d errno=%d", existingID, -linuxEISDIR)
			return 0, 0, FuseAttr{}, -linuxEISDIR
		}
		p.debugChildfLocked("create-open-existing", parent, name, "existing=%d flags=%#x", existingID, flags)
		if flags&linuxOACCMODE != linuxORDONLY {
			if errno := p.copyUpFileLocked(node); errno != 0 {
				p.debugChildfLocked("create-copyup-error", parent, name, "existing=%d errno=%d", existingID, errno)
				return 0, 0, FuseAttr{}, errno
			}
			if flags&linuxOTRUNC != 0 {
				p.debugChildfLocked("create-truncate-existing", parent, name, "existing=%d", existingID)
				node.data.release(p.dataStore)
				node.data = sparseImageData{}
				node.lowerFile = nil
				node.lowerSize = 0
				node.size = 0
				if uid != 0 {
					node.mode &^= fs.FileMode(0o6000)
				}
				now := time.Now()
				node.modTime = now
				node.ctime = now
				p.refreshImageNodeMetadataLocked(node)
			}
		}
		fh := p.nextHandle
		p.nextHandle++
		p.handles[fh] = imageHandle{nodeID: node.id}
		p.noteImageHandleAddedLocked()
		return node.id, fh, p.attr(node), 0
	}
	mode, gid = p.inheritImageCreateLocked(parentNode, mode, gid, false)
	if parentNode.whiteouts != nil {
		delete(parentNode.whiteouts, name)
	}
	now := time.Now()
	node := &imageNode{
		id:      p.nextNodeID,
		parent:  parent,
		name:    name,
		mode:    fs.FileMode(mode & linuxPermMask),
		uid:     uid,
		gid:     gid,
		atime:   now,
		modTime: now,
		ctime:   now,
	}
	p.nextNodeID++
	p.nodes[node.id] = node
	p.noteImageNodeAddedLocked(node)
	p.registerImageNodeLinkLocked(node)
	parentNode.entries[name] = node.id
	p.noteImageEntryAddedLocked(parentNode)
	p.touchImageDirectoryLocked(parentNode, now)
	fh := p.nextHandle
	p.nextHandle++
	p.handles[fh] = imageHandle{nodeID: node.id}
	p.noteImageHandleAddedLocked()
	return node.id, fh, p.attr(node), 0
}

func (p *imageFS) Write(nodeID uint64, fh uint64, off uint64, data []byte, _ uint32) (uint32, int32) {
	return p.WriteForCaller(nodeID, fh, off, data, 0, 0, 0)
}

func (p *imageFS) WriteForCaller(nodeID uint64, fh uint64, off uint64, data []byte, flags uint32, uid uint32, gid uint32) (uint32, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	handle, ok := p.handles[fh]
	node := p.nodes[nodeID]
	if !ok || handle.nodeID != nodeID || node == nil {
		return 0, -linuxEBADF
	}
	if errno := p.copyUpFileLocked(node); errno != 0 {
		return 0, errno
	}
	end := off + uint64(len(data))
	if end < off || end > uint64(^uint(0)>>1) {
		return 0, -linuxEFBIG
	}
	if errno := p.prepareImageOverlayWriteLocked(node, off, uint64(len(data))); errno != 0 {
		return 0, errno
	}
	written, err := node.data.writeAt(p.dataStore, data, off)
	p.refreshImageNodeMetadataLocked(node)
	if err != nil {
		if written > 0 && off+uint64(written) > node.size {
			node.size = off + uint64(written)
		}
		return uint32(written), errnoFromError(err)
	}
	if end > node.size {
		node.size = end
	}
	now := time.Now()
	if uid != 0 && uint32(node.mode)&0o6000 != 0 {
		node.mode &^= fs.FileMode(0o6000)
	}
	node.modTime = now
	node.ctime = now
	return uint32(len(data)), 0
}

func (p *imageFS) SetAttr(nodeID uint64, valid uint32, _ uint64, size uint64, mode uint32, uid uint32, gid uint32, _ time.Time, mtime time.Time) (FuseAttr, int32) {
	return p.SetAttrForCaller(nodeID, valid, 0, size, mode, uid, gid, time.Time{}, mtime, 0, 0)
}

func (p *imageFS) SetAttrForCaller(nodeID uint64, valid uint32, _ uint64, size uint64, mode uint32, uid uint32, gid uint32, atime time.Time, mtime time.Time, callerUID uint32, callerGID uint32) (FuseAttr, int32) {
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
	if valid&(fattrUID|fattrGID) != 0 && valid&fattrMode == 0 {
		node.mode &^= fs.FileMode(0o6000)
	}
	if valid&fattrUID != 0 {
		node.uid = uid
	}
	if valid&fattrGID != 0 {
		node.gid = gid
	}
	if valid&fattrSize != 0 {
		if err := node.data.truncate(p.dataStore, size); err != nil {
			return FuseAttr{}, errnoFromError(err)
		}
		if size < node.lowerSize {
			node.lowerSize = size
		}
		node.size = size
		p.refreshImageNodeMetadataLocked(node)
		if callerUID != 0 {
			node.mode &^= fs.FileMode(0o6000)
		}
	}
	now := time.Now()
	if valid&fattrSize != 0 {
		node.modTime = now
	}
	if valid&fattrATimeNow != 0 {
		node.atime = now
	} else if valid&fattrATime != 0 && !atime.IsZero() {
		node.atime = atime
	}
	if valid&fattrMTimeNow != 0 {
		node.modTime = now
	} else if valid&fattrMTime != 0 && !mtime.IsZero() {
		node.modTime = mtime
	}
	if valid&(fattrMode|fattrUID|fattrGID|fattrSize|fattrATime|fattrMTime|fattrATimeNow|fattrMTimeNow) != 0 {
		node.ctime = now
	}
	return p.attr(node), 0
}

func (p *imageFS) Unlink(parent uint64, name string) int32 {
	return p.UnlinkForCaller(parent, name, 0, 0)
}

func (p *imageFS) UnlinkForCaller(parent uint64, name string, uid uint32, gid uint32) int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	if parentNode == nil {
		return -linuxENOENT
	}
	var ok bool
	name, ok = cleanChildName(name)
	if !ok {
		return -linuxEINVAL
	}
	childID, ok := parentNode.entries[name]
	if !ok {
		p.debugChildfLocked("unlink-miss", parent, name, "errno=%d", -linuxENOENT)
		return -linuxENOENT
	}
	child := p.nodes[childID]
	if child == nil {
		p.debugChildfLocked("unlink-stale-node", parent, name, "node=%d errno=%d", childID, -linuxENOENT)
		return -linuxENOENT
	}
	if child.isDir() {
		p.debugChildfLocked("unlink-dir", parent, name, "node=%d errno=%d", childID, -linuxEISDIR)
		return -linuxEISDIR
	}
	p.debugChildfLocked("unlink", parent, name, "node=%d", childID)
	delete(parentNode.entries, name)
	if parentNode.abstractDir != nil {
		if parentNode.whiteouts == nil {
			parentNode.whiteouts = map[string]bool{}
		}
		parentNode.whiteouts[name] = true
		p.noteImageWhiteoutAddedLocked(parentNode)
	}
	child.ctime = time.Now()
	p.touchImageDirectoryLocked(parentNode, child.ctime)
	if p.imageNodeReferenceCountLocked(childID) == 0 {
		p.collectImageNodeLocked(childID)
	} else {
		p.refreshImageNodeLinksLocked(child)
	}
	p.compactImageNodeMapsLocked(parentNode)
	return 0
}

func (p *imageFS) Rename(parent uint64, name string, newParent uint64, newName string, flags uint32) int32 {
	return p.RenameForCaller(parent, name, newParent, newName, flags, 0, 0)
}

func (p *imageFS) RenameForCaller(parent uint64, name string, newParent uint64, newName string, flags uint32, uid uint32, gid uint32) int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	parentNode := p.nodes[parent]
	newParentNode := p.nodes[newParent]
	if parentNode == nil || newParentNode == nil {
		return -linuxENOENT
	}
	var ok bool
	name, ok = cleanChildName(name)
	if !ok {
		return -linuxEINVAL
	}
	newName, ok = cleanChildName(newName)
	if !ok {
		return -linuxEINVAL
	}
	p.debugChildfLocked("rename-old", parent, name, "new_parent=%q new_name=%q flags=%#x", p.pathForNode(newParent), newName, flags)
	p.debugChildfLocked("rename-new", newParent, newName, "old_parent=%q old_name=%q flags=%#x", p.pathForNode(parent), name, flags)
	childID, ok := parentNode.entries[name]
	if !ok {
		p.debugChildfLocked("rename-miss", parent, name, "new_parent=%q new_name=%q errno=%d", p.pathForNode(newParent), newName, -linuxENOENT)
		return -linuxENOENT
	}
	child := p.nodes[childID]
	if child == nil {
		p.debugChildfLocked("rename-stale-node", parent, name, "node=%d errno=%d", childID, -linuxENOENT)
		return -linuxENOENT
	}
	existingID, targetExists := newParentNode.entries[newName]
	if flags&^(linuxRenameNoReplace|linuxRenameExchange) != 0 || flags == linuxRenameNoReplace|linuxRenameExchange {
		return -linuxEINVAL
	}
	if flags&linuxRenameNoReplace != 0 && targetExists {
		return -linuxEEXIST
	}
	if flags&linuxRenameExchange != 0 {
		if !targetExists {
			return -linuxENOENT
		}
		other := p.nodes[existingID]
		if other == nil {
			return -linuxENOENT
		}
		if existingID == childID {
			return 0
		}
		parentNode.entries[name] = existingID
		newParentNode.entries[newName] = childID
		child.parent, child.name = newParent, newName
		other.parent, other.name = parent, name
		p.refreshImageNodeMetadataLocked(child)
		p.refreshImageNodeMetadataLocked(other)
		now := time.Now()
		child.ctime, other.ctime = now, now
		parentNode.modTime, parentNode.ctime = now, now
		newParentNode.modTime, newParentNode.ctime = now, now
		return 0
	}
	// POSIX requires renaming one hard link over another link to the same
	// inode to succeed without removing either directory entry.
	if targetExists && existingID == childID {
		return 0
	}
	var replaced *imageNode
	if existingID, exists := newParentNode.entries[newName]; exists {
		replaced = p.nodes[existingID]
		if replaced != nil && replaced.isDir() && !child.isDir() {
			p.debugChildfLocked("rename-target-dir", newParent, newName, "existing=%d errno=%d", existingID, -linuxEISDIR)
			return -linuxEISDIR
		}
		if replaced != nil && !replaced.isDir() && child.isDir() {
			p.debugChildfLocked("rename-target-not-dir", newParent, newName, "existing=%d errno=%d", existingID, -linuxENOTDIR)
			return -linuxENOTDIR
		}
		p.debugChildfLocked("rename-replace-target", newParent, newName, "existing=%d", existingID)
	}
	delete(parentNode.entries, name)
	if parentNode.abstractDir != nil {
		if parentNode.whiteouts == nil {
			parentNode.whiteouts = map[string]bool{}
		}
		parentNode.whiteouts[name] = true
		p.noteImageWhiteoutAddedLocked(parentNode)
	}
	if newParentNode.whiteouts != nil {
		delete(newParentNode.whiteouts, newName)
	}
	newParentNode.entries[newName] = childID
	p.noteImageEntryAddedLocked(newParentNode)
	child.parent = newParent
	child.name = newName
	p.refreshImageNodeMetadataLocked(child)
	now := time.Now()
	child.ctime = now
	parentNode.modTime, parentNode.ctime = now, now
	newParentNode.modTime, newParentNode.ctime = now, now
	if replaced != nil {
		if p.imageNodeReferenceCountLocked(replaced.id) == 0 {
			p.collectImageNodeLocked(replaced.id)
		} else {
			p.refreshImageNodeLinksLocked(replaced)
		}
	}
	p.refreshImageNodeLinksLocked(child)
	p.compactImageNodeMapsLocked(parentNode)
	if newParentNode != parentNode {
		p.compactImageNodeMapsLocked(newParentNode)
	}
	return 0
}

func (p *imageFS) StatFS(_ uint64) (uint64, uint64, uint64, uint64, uint64, uint64, uint64, uint64, int32) {
	if p.root == "" {
		return 0, 0, 0, 0, 0, 4096, 4096, 255, 0
	}
	return hostStatFS(p.root)
}

func (p *imageFS) GetXattr(nodeID uint64, name string) ([]byte, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return nil, -linuxENOENT
	}
	value, ok := node.xattrs[name]
	if !ok {
		return nil, -linuxENODATA
	}
	return append([]byte(nil), value...), 0
}

func (p *imageFS) ListXattr(nodeID uint64) ([]byte, int32) {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return nil, -linuxENOENT
	}
	names := make([]string, 0, len(node.xattrs))
	for name := range node.xattrs {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []byte
	for _, name := range names {
		out = append(out, name...)
		out = append(out, 0)
	}
	return out, 0
}

func (p *imageFS) SetXattr(nodeID uint64, name string, value []byte, flags uint32) int32 {
	if name == "" || len(name) > 255 || len(value) > 64<<10 || flags&^uint32(3) != 0 || flags == 3 {
		return -linuxEINVAL
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return -linuxENOENT
	}
	_, exists := node.xattrs[name]
	if flags&1 != 0 && exists {
		return -linuxEEXIST
	}
	if flags&2 != 0 && !exists {
		return -linuxENODATA
	}
	oldBytes := 0
	if exists {
		oldBytes = len(name) + len(node.xattrs[name]) + imageXattrEntryOverhead
	}
	newBytes := len(name) + len(value) + imageXattrEntryOverhead
	nodeBytes := imageNodeXattrBytes(node) - oldBytes + newBytes
	if nodeBytes > imageMaxXattrBytesPerNode || int64(p.xattrBytes)-int64(oldBytes)+int64(newBytes) > imageMaxXattrBytes {
		return -linuxENOSPC
	}
	if node.xattrs == nil {
		node.xattrs = make(map[string][]byte)
	}
	node.xattrs[name] = append([]byte(nil), value...)
	p.xattrBytes = uint64(int64(p.xattrBytes) - int64(oldBytes) + int64(newBytes))
	p.refreshImageNodeMetadataLocked(node)
	node.ctime = time.Now()
	return 0
}

func (p *imageFS) RemoveXattr(nodeID uint64, name string) int32 {
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if node == nil {
		return -linuxENOENT
	}
	if _, exists := node.xattrs[name]; !exists {
		return -linuxENODATA
	}
	p.xattrBytes -= uint64(len(name) + len(node.xattrs[name]) + imageXattrEntryOverhead)
	delete(node.xattrs, name)
	p.refreshImageNodeMetadataLocked(node)
	node.ctime = time.Now()
	return 0
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
	isDir := node.isDir()
	isSymlink := node.isSymlink()
	switch {
	case isDir:
		mode = linuxSIFDIR | linuxModeBits(nodeMode)
	case isSymlink:
		mode = linuxSIFLNK | linuxModeBits(nodeMode)
	case node.rawMode != 0:
		mode = (node.rawMode &^ linuxPermMask) | linuxModeBits(nodeMode)
	default:
		mode = linuxSIFREG | linuxModeBits(nodeMode)
	}
	nlink := uint32(1)
	if isDir {
		nlink = 2
		for _, childID := range node.entries {
			if child := p.nodes[childID]; child != nil && child.isDir() {
				nlink++
			}
		}
	} else {
		nlink = p.imageNodeLinkCountLocked(node)
	}
	attrUID, attrGID := node.uid, node.gid
	if p.mapOwner {
		attrUID, attrGID = p.ownerUID, p.ownerGID
	}
	atime, ctime := node.atime, node.ctime
	if atime.IsZero() {
		atime = modTime
	}
	if ctime.IsZero() {
		ctime = modTime
	}
	allocatedBytes := node.data.allocatedBytes(size)
	if node.abstractFile != nil || node.lowerFile != nil {
		allocatedBytes = size
	}
	return FuseAttr{
		Ino:       p.imageNodeInodeLocked(node),
		Size:      size,
		Blocks:    (allocatedBytes + 511) / 512,
		ATimeSec:  uint64(atime.Unix()),
		MTimeSec:  uint64(modTime.Unix()),
		CTimeSec:  uint64(ctime.Unix()),
		ATimeNsec: uint32(atime.Nanosecond()),
		MTimeNsec: uint32(modTime.Nanosecond()),
		CTimeNsec: uint32(ctime.Nanosecond()),
		Mode:      mode,
		NLink:     nlink,
		UID:       attrUID,
		GID:       attrGID,
		RDev:      node.rdev,
		BlkSize:   4096,
	}
}

func (p *imageFS) registerImageNodeLinkLocked(node *imageNode) {
	if node == nil || node.isDir() {
		return
	}
	if node.nlink == 0 {
		node.nlink = 1
	}
}

func (p *imageFS) imageNodeLinkCountLocked(node *imageNode) uint32 {
	if node == nil || node.isDir() {
		return 1
	}
	if node.nlink != 0 {
		return node.nlink
	}
	return 1
}

func (p *imageFS) refreshImageNodeLinksLocked(node *imageNode) {
	if node == nil || node.isDir() {
		return
	}
	inode := p.imageNodeInodeLocked(node)
	nlink := uint32(0)
	for id, candidate := range p.nodes {
		if candidate != nil && !candidate.isDir() && p.imageNodeInodeLocked(candidate) == inode {
			nlink += p.imageNodeReferenceCountLocked(id)
		}
	}
	if nlink == 0 {
		nlink = 1
	}
	for _, candidate := range p.nodes {
		if candidate != nil && !candidate.isDir() && p.imageNodeInodeLocked(candidate) == inode {
			candidate.nlink = nlink
		}
	}
}

func (p *imageFS) imageNodeReferenceCountLocked(nodeID uint64) uint32 {
	var count uint32
	for _, candidate := range p.nodes {
		if candidate == nil || !candidate.isDir() {
			continue
		}
		for _, childID := range candidate.entries {
			if childID == nodeID {
				count++
			}
		}
	}
	return count
}

func (p *imageFS) imageNodeHasHandleLocked(nodeID uint64) bool {
	for _, handle := range p.handles {
		if handle.nodeID == nodeID {
			return true
		}
	}
	for _, handleNodeID := range p.dirHandleNodes {
		if handleNodeID == nodeID {
			return true
		}
	}
	return false
}

func (p *imageFS) touchImageDirectoryLocked(node *imageNode, now time.Time) {
	if node == nil || !node.isDir() {
		return
	}
	node.modTime = now
	node.ctime = now
}

func (p *imageFS) collectImageNodeLocked(nodeID uint64) {
	// The root has no parent directory entry, so its reference count is always
	// zero. Releasing an open root directory must never collect it: persistent
	// guest shells routinely open and close / while resolving paths.
	if nodeID == 1 {
		return
	}
	if p.imageNodeReferenceCountLocked(nodeID) == 0 && !p.imageNodeHasHandleLocked(nodeID) {
		if node := p.nodes[nodeID]; node != nil {
			p.xattrBytes -= uint64(imageNodeXattrBytes(node))
			p.retainedEntries -= node.retainedEntries
			p.retainedWhiteouts -= node.retainedWhiteouts
			node.data.release(p.dataStore)
			p.dynamicMetadata -= node.accountedMetadata
		}
		delete(p.nodes, nodeID)
		p.compactImageNodesLocked()
	}
}

func (p *imageFS) Close() error {
	return p.closeWithin(2 * time.Second)
}

func (p *imageFS) closeWithin(timeout time.Duration) error {
	if p == nil {
		return nil
	}
	p.closeStart.Do(func() {
		p.mu.Lock()
		p.closed = true
		p.materializationCancel()
		for _, materialization := range p.materializations {
			materialization.cancel()
		}
		p.closeDone = make(chan struct{})
		done := p.closeDone
		p.mu.Unlock()
		// Legacy lower filesystems cannot always interrupt an in-flight host I/O
		// operation. Keep ownership of both the worker and backing store until it
		// actually returns, but do not make VM teardown wait without a bound.
		go func() {
			p.materializationWG.Wait()
			p.closeErr = p.dataStore.close()
			close(done)
		}()
	})
	p.mu.Lock()
	done := p.closeDone
	p.mu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
		return p.closeErr
	case <-timer.C:
		return fmt.Errorf("close image filesystem: lower filesystem operation did not stop within %s; backing storage remains quarantined until it exits", timeout)
	}
}

func (p *imageFS) BackingUsage() (uint64, uint64, uint64, error) {
	if p == nil {
		return 0, 0, 0, nil
	}
	return p.dataStore.usage()
}

func (p *imageFS) BackingCurrent() uint64 {
	if p == nil {
		return 0
	}
	p.dataStore.mu.Lock()
	defer p.dataStore.mu.Unlock()
	return p.dataStore.current
}

func (p *imageFS) BackingMetadataUsage() (uint64, uint64) {
	if p == nil {
		return 0, 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	// This is deliberately accounting, not a guest quota. Empty files and page
	// indexes consume host heap even when no backing blocks are allocated; make
	// that pressure visible to the same host recovery telemetry as file data.
	metadata := uint64(p.retainedNodes)*256 + uint64(p.retainedHandles)*64 + uint64(p.retainedDirHandles)*64 + p.xattrBytes
	metadata += uint64(p.retainedEntries+p.retainedWhiteouts) * 64
	metadata += uint64(len(p.materializations)) * 128
	metadata += p.dynamicMetadata
	metadata += p.dataStore.metadataUsage()
	if metadata > p.metadataHighWater {
		p.metadataHighWater = metadata
	}
	return metadata, p.metadataHighWater
}

func (p *imageFS) noteImageNodeAddedLocked(node *imageNode) {
	p.retainedNodes = max(p.retainedNodes, len(p.nodes))
	p.refreshImageNodeMetadataLocked(node)
	p.bumpImageMetadataFloorLocked()
}

func (p *imageFS) refreshImageNodeMetadataLocked(node *imageNode) {
	if node == nil {
		return
	}
	current := uint64(len(node.name)+len(node.symlinkTarget)) + uint64(cap(node.data.extents))*32
	for _, value := range node.xattrs {
		current += uint64(cap(value) - len(value))
	}
	if current >= node.accountedMetadata {
		p.dynamicMetadata += current - node.accountedMetadata
	} else {
		p.dynamicMetadata -= node.accountedMetadata - current
	}
	node.accountedMetadata = current
}

func (p *imageFS) noteImageHandleAddedLocked() {
	p.retainedHandles = max(p.retainedHandles, len(p.handles))
	p.bumpImageMetadataFloorLocked()
}

func (p *imageFS) noteImageDirHandleAddedLocked() {
	p.retainedDirHandles = max(p.retainedDirHandles, len(p.dirHandles))
	p.bumpImageMetadataFloorLocked()
}

func (p *imageFS) noteImageEntryAddedLocked(node *imageNode) {
	if node == nil || len(node.entries) <= node.retainedEntries {
		return
	}
	p.retainedEntries += len(node.entries) - node.retainedEntries
	node.retainedEntries = len(node.entries)
	p.bumpImageMetadataFloorLocked()
}

func (p *imageFS) noteImageWhiteoutAddedLocked(node *imageNode) {
	if node == nil || len(node.whiteouts) <= node.retainedWhiteouts {
		return
	}
	p.retainedWhiteouts += len(node.whiteouts) - node.retainedWhiteouts
	node.retainedWhiteouts = len(node.whiteouts)
	p.bumpImageMetadataFloorLocked()
}

func (p *imageFS) compactImageNodeMapsLocked(node *imageNode) {
	if node == nil {
		return
	}
	if len(node.entries)*4 < node.retainedEntries && (node.retainedEntries >= 64 || len(node.entries) <= 4) {
		rebuilt := make(map[string]uint64, len(node.entries))
		for name, id := range node.entries {
			rebuilt[name] = id
		}
		p.retainedEntries -= node.retainedEntries - len(rebuilt)
		node.retainedEntries = len(rebuilt)
		node.entries = rebuilt
	}
	if len(node.whiteouts)*4 < node.retainedWhiteouts && (node.retainedWhiteouts >= 64 || len(node.whiteouts) <= 4) {
		rebuilt := make(map[string]bool, len(node.whiteouts))
		for name, present := range node.whiteouts {
			rebuilt[name] = present
		}
		p.retainedWhiteouts -= node.retainedWhiteouts - len(rebuilt)
		node.retainedWhiteouts = len(rebuilt)
		node.whiteouts = rebuilt
	}
}

func (p *imageFS) compactImageNodesLocked() {
	if len(p.nodes)*4 >= p.retainedNodes || p.retainedNodes < 64 && len(p.nodes) > 4 {
		return
	}
	rebuilt := make(map[uint64]*imageNode, len(p.nodes))
	for id, node := range p.nodes {
		rebuilt[id] = node
	}
	p.nodes = rebuilt
	p.retainedNodes = len(rebuilt)
}

func (p *imageFS) compactImageHandleMapsLocked() {
	if len(p.handles)*4 >= p.retainedHandles || p.retainedHandles < 64 && !(len(p.handles) == 0 && p.retainedHandles >= 16) {
		return
	}
	rebuilt := make(map[uint64]imageHandle, len(p.handles))
	for id, handle := range p.handles {
		rebuilt[id] = handle
	}
	p.handles = rebuilt
	p.retainedHandles = len(rebuilt)
}

func (p *imageFS) compactImageDirHandleMapsLocked() {
	if len(p.dirHandles)*4 >= p.retainedDirHandles || p.retainedDirHandles < 64 && !(len(p.dirHandles) == 0 && p.retainedDirHandles >= 16) {
		return
	}
	handles := make(map[uint64][]dirEntry, len(p.dirHandles))
	nodes := make(map[uint64]uint64, len(p.dirHandleNodes))
	for id, entries := range p.dirHandles {
		handles[id] = entries
	}
	for id, node := range p.dirHandleNodes {
		nodes[id] = node
	}
	p.dirHandles = handles
	p.dirHandleNodes = nodes
	p.retainedDirHandles = len(handles)
}

func (p *imageFS) bumpImageMetadataFloorLocked() {
	floor := uint64(p.retainedNodes)*256 + uint64(p.retainedHandles+p.retainedDirHandles+p.retainedEntries+p.retainedWhiteouts)*64 + p.xattrBytes
	if floor > p.metadataHighWater {
		p.metadataHighWater = floor
	}
}

func imageNodeXattrBytes(node *imageNode) int {
	if node == nil {
		return 0
	}
	total := 0
	for name, value := range node.xattrs {
		total += len(name) + len(value) + imageXattrEntryOverhead
	}
	return total
}

func (p *imageFS) imageNodeInodeLocked(node *imageNode) uint64 {
	if node == nil {
		return 0
	}
	if node.inode != 0 {
		return node.inode
	}
	return node.id
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
	p.noteImageNodeAddedLocked(node)
	p.registerImageNodeLinkLocked(node)
	parent.entries[name] = node.id
	p.noteImageEntryAddedLocked(parent)
	return node, 0
}

func (p *imageFS) materializeDirEntriesLocked(node *imageNode) ([]imagefs.DirEnt, int32) {
	if node.abstractDir == nil {
		return nil, 0
	}
	ents, err := node.abstractDir.ReadDir()
	if err != nil {
		return nil, errnoFromError(err)
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
			return nil, -linuxEIO
		}
		if _, errno := p.createAbstractNode(node, ent.Name, entry); errno != 0 {
			return nil, errno
		}
	}
	node.entriesDone = true
	return ents, 0
}

func (p *imageFS) materializeDirEntries(nodeID uint64) int32 {
	p.mu.Lock()
	node := p.nodes[nodeID]
	if node == nil {
		p.mu.Unlock()
		return -linuxENOENT
	}
	if !node.isDir() {
		p.mu.Unlock()
		return -linuxENOTDIR
	}
	lowerDir := node.abstractDir
	lowerNode := node
	if lowerDir == nil || node.entriesDone {
		p.mu.Unlock()
		return 0
	}
	p.mu.Unlock()

	ents, err := lowerDir.ReadDir()
	if err != nil {
		return errnoFromError(err)
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].Name < ents[j].Name })
	type lowerEntry struct {
		name  string
		entry imagefs.Entry
	}
	lowerEntries := make([]lowerEntry, 0, len(ents))
	for _, ent := range ents {
		if ent.Name == "." || ent.Name == ".." {
			continue
		}
		entry, err := lowerDir.Lookup(ent.Name)
		if err != nil {
			return errnoFromError(err)
		}
		lowerEntries = append(lowerEntries, lowerEntry{name: ent.Name, entry: entry})
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	node = p.nodes[nodeID]
	if node == nil {
		return -linuxENOENT
	}
	if node != lowerNode {
		return 0
	}
	if node.entriesDone {
		return 0
	}
	for _, ent := range lowerEntries {
		if node.whiteouts[ent.name] {
			continue
		}
		if _, ok := node.entries[ent.name]; ok {
			continue
		}
		if _, errno := p.createAbstractNode(node, ent.name, ent.entry); errno != 0 {
			return errno
		}
	}
	node.entriesDone = true
	return 0
}

type imageLowerEntry struct {
	name  string
	entry imagefs.Entry
}

type imageDirMaterialization struct {
	done   chan struct{}
	cancel context.CancelFunc
	node   *imageNode
	err    error
}

func readImageLowerEntries(ctx context.Context, lowerDir imagefs.Directory) ([]imageLowerEntry, error) {
	ents, err := imagefs.ReadDirContext(ctx, lowerDir)
	if err != nil {
		return nil, err
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].Name < ents[j].Name })
	entries := make([]imageLowerEntry, 0, len(ents))
	for _, ent := range ents {
		if ent.Name == "." || ent.Name == ".." {
			continue
		}
		entry, err := imagefs.LookupContext(ctx, lowerDir, ent.Name)
		if err != nil {
			return nil, fmt.Errorf("lookup lower entry %q: %w", ent.Name, err)
		}
		entries = append(entries, imageLowerEntry{name: ent.Name, entry: entry})
	}
	return entries, nil
}

// materializeDirEntriesContext keeps potentially blocking lower-filesystem I/O
// outside imageFS.mu. Legacy imagefs implementations do not accept a context,
// so one background read may outlive a canceled caller. The in-flight record
// prevents repeated cancellations from creating unbounded readers for the same
// directory, and the eventual result is committed or discarded under the lock.
func (p *imageFS) materializeDirEntriesContext(ctx context.Context, nodeID uint64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	node := p.nodes[nodeID]
	if node == nil {
		p.mu.Unlock()
		return os.ErrNotExist
	}
	if !node.isDir() {
		p.mu.Unlock()
		return fmt.Errorf("%s is not a directory", p.pathForNode(nodeID))
	}
	lowerDir := node.abstractDir
	if lowerDir == nil || node.entriesDone {
		p.mu.Unlock()
		return nil
	}
	if p.closed {
		p.mu.Unlock()
		return os.ErrClosed
	}
	materialization := p.materializations[nodeID]
	if materialization == nil {
		if p.materializations == nil {
			p.materializations = make(map[uint64]*imageDirMaterialization)
		}
		materializationCtx, cancel := context.WithCancel(p.materializationCtx)
		materialization = &imageDirMaterialization{done: make(chan struct{}), cancel: cancel, node: node}
		p.materializations[nodeID] = materialization
		p.materializationWG.Add(1)
		go p.finishDirMaterialization(materializationCtx, nodeID, lowerDir, materialization)
	}
	p.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-materialization.done:
		return materialization.err
	}
}

func (p *imageFS) finishDirMaterialization(ctx context.Context, nodeID uint64, lowerDir imagefs.Directory, materialization *imageDirMaterialization) {
	defer p.materializationWG.Done()
	defer materialization.cancel()
	entries, err := readImageLowerEntries(ctx, lowerDir)
	p.mu.Lock()
	defer p.mu.Unlock()
	node := p.nodes[nodeID]
	if err == nil && node == materialization.node && !node.entriesDone {
		for _, ent := range entries {
			if node.whiteouts[ent.name] {
				continue
			}
			if _, ok := node.entries[ent.name]; ok {
				continue
			}
			if _, errno := p.createAbstractNode(node, ent.name, ent.entry); errno != 0 {
				err = fmt.Errorf("materialize %s: errno %d", p.pathForNode(nodeID), errno)
				break
			}
		}
		if err == nil {
			node.entriesDone = true
		}
	} else if err == nil && node == nil {
		err = os.ErrNotExist
	}
	materialization.err = err
	if p.materializations[nodeID] == materialization {
		delete(p.materializations, nodeID)
	}
	close(materialization.done)
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
	node.size = size
	node.mode = mode
	node.lowerFile = node.abstractFile
	node.lowerSize = size
	node.abstractFile = nil
	if node.modTime.IsZero() {
		node.modTime = time.Now()
	}
	return 0
}

// prepareImageOverlayWriteLocked materializes only partially overwritten lower
// pages. Complete pages can be created directly from the write payload. This is
// the page-level COW boundary that prevents a one-byte change from copying an
// entire lower file (and from expanding sparse lower files).
func (p *imageFS) prepareImageOverlayWriteLocked(node *imageNode, off, length uint64) int32 {
	if node == nil || node.lowerFile == nil || length == 0 {
		return 0
	}
	defer p.refreshImageNodeMetadataLocked(node)
	end := off + length
	if end < off {
		return -linuxEFBIG
	}
	for cursor := off; cursor < end; {
		page := cursor / imageDataPageSize
		pageStart := page * imageDataPageSize
		pageEnd := pageStart + imageDataPageSize
		writeEnd := min(end, pageEnd)
		fullPage := cursor == pageStart && writeEnd == pageEnd
		if !fullPage {
			if _, exists := node.data.location(page); !exists && pageStart < node.lowerSize {
				visible := min(imageDataPageSize, node.lowerSize-pageStart)
				data, err := node.lowerFile.ReadAt(pageStart, uint32(visible))
				if err != nil {
					return errnoFromError(err)
				}
				if uint64(len(data)) != visible {
					return -linuxEIO
				}
				location, err := p.dataStore.allocatePage(data)
				if err != nil {
					return errnoFromError(err)
				}
				node.data.insert(page, location)
			}
		}
		cursor = writeEnd
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
	linuxEPERM      = linuxabi.EPERM
	linuxENOENT     = linuxabi.ENOENT
	linuxENXIO      = linuxabi.ENXIO
	linuxEIO        = linuxabi.EIO
	linuxEBADF      = linuxabi.EBADF
	linuxEACCES     = linuxabi.EACCES
	linuxEPIPE      = linuxabi.EPIPE
	linuxEEXIST     = linuxabi.EEXIST
	linuxENOTDIR    = linuxabi.ENOTDIR
	linuxEISDIR     = linuxabi.EISDIR
	linuxEINVAL     = linuxabi.EINVAL
	linuxENOTTY     = linuxabi.ENOTTY
	linuxEFBIG      = linuxabi.EFBIG
	linuxENOSPC     = linuxabi.ENOSPC
	linuxERANGE     = linuxabi.ERANGE
	linuxENOSYS     = linuxabi.ENOSYS
	linuxENOTEMPTY  = linuxabi.ENOTEMPTY
	linuxENODATA    = linuxabi.ENODATA
	linuxEOPNOTSUPP = linuxabi.EOPNOTSUPP
	linuxETIMEDOUT  = linuxabi.ETIMEDOUT
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
	var linkErr *os.LinkError
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
	if errors.As(err, &linkErr) {
		if errno, ok := mapHostError(linkErr.Err); ok {
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
