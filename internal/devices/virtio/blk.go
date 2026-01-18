package virtio

import (
	"encoding/binary"
	"fmt"
	"os"
	"sync"

	"github.com/tinyrange/cc/internal/debug"
	"github.com/tinyrange/cc/internal/hv"
)

const (
	BlkDefaultMMIOBase = 0xd0002000
	BlkDefaultMMIOSize = 0x200
	BlkDefaultIRQLine  = 12
	armBlkDefaultIRQ   = 42

	blkQueueCount   = 1
	blkQueueNumMax  = 128
	blkVendorID     = 0x554d4551 // "QEMU"
	blkVersion      = 2
	blkDeviceID     = 2
	blkInterruptBit = 0x1

	blkQueueRequest = 0
)

// Virtio block request types
const (
	VIRTIO_BLK_T_IN          = 0 // Read
	VIRTIO_BLK_T_OUT         = 1 // Write
	VIRTIO_BLK_T_FLUSH       = 4 // Flush
	VIRTIO_BLK_T_GET_ID      = 8 // Get device ID
	VIRTIO_BLK_T_DISCARD     = 11
	VIRTIO_BLK_T_WRITE_ZEROES = 13
)

// Virtio block status codes
const (
	VIRTIO_BLK_S_OK     = 0
	VIRTIO_BLK_S_IOERR  = 1
	VIRTIO_BLK_S_UNSUPP = 2
)

// Virtio block feature bits
const (
	VIRTIO_BLK_F_SIZE_MAX  = 1 << 1  // Max size of any single segment
	VIRTIO_BLK_F_SEG_MAX   = 1 << 2  // Max number of segments
	VIRTIO_BLK_F_GEOMETRY  = 1 << 4  // Disk geometry available
	VIRTIO_BLK_F_RO        = 1 << 5  // Read-only device
	VIRTIO_BLK_F_BLK_SIZE  = 1 << 6  // Block size available
	VIRTIO_BLK_F_FLUSH     = 1 << 9  // Flush command supported
	VIRTIO_BLK_F_TOPOLOGY  = 1 << 10 // Topology info available
	VIRTIO_BLK_F_CONFIG_WCE = 1 << 11 // Writeback mode available
)

// blkDeviceConfig is the shared configuration for block devices.
var blkDeviceConfig = &MMIODeviceConfig{
	DefaultMMIOBase:   BlkDefaultMMIOBase,
	DefaultMMIOSize:   BlkDefaultMMIOSize,
	DefaultIRQLine:    BlkDefaultIRQLine,
	ArmDefaultIRQLine: armBlkDefaultIRQ,
	DeviceID:          blkDeviceID,
	VendorID:          blkVendorID,
	Version:           blkVersion,
	QueueCount:        blkQueueCount,
	QueueMaxSize:      blkQueueNumMax,
	FeatureBits:       []uint64{virtioFeatureVersion1 | VIRTIO_BLK_F_SIZE_MAX | VIRTIO_BLK_F_SEG_MAX | VIRTIO_BLK_F_BLK_SIZE | VIRTIO_BLK_F_FLUSH},
	DeviceName:        "virtio-blk",
}

// BlkDeviceConfig returns the shared configuration for block devices.
func BlkDeviceConfig() *MMIODeviceConfig {
	return blkDeviceConfig
}

// BlkTemplate is the template for creating virtio-blk devices.
type BlkTemplate struct {
	MMIODeviceTemplateBase
	File     *os.File
	ReadOnly bool
}

// NewBlkTemplate creates a BlkTemplate with proper configuration.
func NewBlkTemplate(file *os.File, readonly bool) BlkTemplate {
	return BlkTemplate{
		MMIODeviceTemplateBase: MMIODeviceTemplateBase{Config: blkDeviceConfig},
		File:                   file,
		ReadOnly:               readonly,
	}
}

func (t BlkTemplate) Create(vm hv.VirtualMachine) (hv.Device, error) {
	config := t.Config
	if config == nil {
		config = blkDeviceConfig
	}

	arch := t.ArchOrDefault(vm)
	irqLine := t.IRQLineForArch(arch)
	encodedLine := EncodeIRQLineForArch(arch, irqLine)

	// Allocate MMIO region dynamically
	mmioBase := config.DefaultMMIOBase
	if vm != nil {
		alloc, err := vm.AllocateMMIO(hv.MMIOAllocationRequest{
			Name:      config.DeviceName,
			Size:      config.DefaultMMIOSize,
			Alignment: 0x1000,
		})
		if err != nil {
			return nil, fmt.Errorf("virtio-blk: allocate MMIO: %w", err)
		}
		mmioBase = alloc.Base
	}

	blk := &Blk{
		MMIODeviceBase: NewMMIODeviceBase(
			mmioBase,
			config.DefaultMMIOSize,
			encodedLine,
			config,
		),
		file:     t.File,
		readonly: t.ReadOnly,
	}
	if err := blk.Init(vm); err != nil {
		return nil, fmt.Errorf("virtio-blk: initialize device: %w", err)
	}
	return blk, nil
}

var (
	_ hv.DeviceTemplate = BlkTemplate{}
	_ VirtioMMIODevice  = BlkTemplate{}
)

// Blk implements a virtio block device.
type Blk struct {
	MMIODeviceBase
	mu       sync.Mutex
	file     *os.File
	readonly bool
	capacity uint64 // in 512-byte sectors
}

// blkConfig is the virtio-blk configuration structure.
type blkConfig struct {
	capacity   uint64 // Number of 512-byte sectors
	sizeMax    uint32 // Max size of any single segment
	segMax     uint32 // Max number of segments
	cylinders  uint16 // Geometry: cylinders
	heads      uint8  // Geometry: heads
	sectors    uint8  // Geometry: sectors
	blkSize    uint32 // Block size
}

// Init implements hv.MemoryMappedIODevice.
func (b *Blk) Init(vm hv.VirtualMachine) error {
	if b.Device() == nil {
		// Get file size to determine capacity
		if b.file != nil {
			fi, err := b.file.Stat()
			if err != nil {
				return fmt.Errorf("virtio-blk: stat file: %w", err)
			}
			b.capacity = uint64(fi.Size()) / 512
		}

		if err := b.InitBase(vm, b); err != nil {
			return err
		}
		return nil
	}
	if mmio, ok := b.Device().(*mmioDevice); ok && vm != nil {
		mmio.vm = vm
	}
	return nil
}

// Stop implements Stoppable.
func (b *Blk) Stop() error {
	return nil
}

func (b *Blk) OnReset(device) {
	// Nothing to reset
}

func (b *Blk) OnQueueNotify(ctx hv.ExitContext, dev device, queue int) error {
	debug.Writef("virtio-blk.OnQueueNotify", "queue=%d", queue)
	if queue != blkQueueRequest {
		return nil
	}
	return b.processRequestQueue(dev, dev.queue(queue))
}

func (b *Blk) ReadConfig(ctx hv.ExitContext, dev device, offset uint64) (uint32, bool, error) {
	return ReadConfigWindow(offset, b.configBytes())
}

func (b *Blk) WriteConfig(ctx hv.ExitContext, dev device, offset uint64, value uint32) (bool, error) {
	return WriteConfigNoop(offset)
}

func (b *Blk) processRequestQueue(dev device, q *queue) error {
	processed, err := ProcessQueueNotifications(dev, q, b.processRequest)
	if err != nil {
		return err
	}
	if ShouldRaiseInterrupt(dev, q, processed) {
		dev.raiseInterrupt(blkInterruptBit)
	}
	return nil
}

// virtioBlkReqHdr is the request header structure
type virtioBlkReqHdr struct {
	reqType  uint32
	reserved uint32
	sector   uint64
}

func (b *Blk) processRequest(dev device, q *queue, head uint16) (uint32, error) {
	// Read the descriptor chain
	// Format: [header descriptor] [data descriptors...] [status descriptor]
	// Header: read-only, contains request type and sector
	// Data: read-only for writes, write-only for reads
	// Status: write-only, single byte

	index := head
	var hdr virtioBlkReqHdr
	var dataDescs []virtqDescriptor
	var statusDesc virtqDescriptor
	var statusDescIdx uint16

	// First, collect all descriptors
	for i := uint16(0); i < q.size; i++ {
		desc, err := dev.readDescriptor(q, index)
		if err != nil {
			return 0, err
		}

		if i == 0 {
			// First descriptor is the header (read-only)
			if desc.flags&virtqDescFWrite != 0 {
				return 0, fmt.Errorf("virtio-blk: header descriptor is writable")
			}
			if desc.length < 16 {
				return 0, fmt.Errorf("virtio-blk: header too short: %d", desc.length)
			}
			hdrData, err := dev.readGuest(desc.addr, 16)
			if err != nil {
				return 0, err
			}
			hdr.reqType = binary.LittleEndian.Uint32(hdrData[0:4])
			hdr.reserved = binary.LittleEndian.Uint32(hdrData[4:8])
			hdr.sector = binary.LittleEndian.Uint64(hdrData[8:16])
		} else if desc.flags&virtqDescFNext == 0 || i == q.size-1 {
			// Last descriptor is status (write-only)
			statusDesc = desc
			statusDescIdx = index
		} else {
			// Middle descriptors are data
			dataDescs = append(dataDescs, desc)
		}

		if desc.flags&virtqDescFNext == 0 {
			break
		}
		index = desc.next
	}

	// Process the request
	status := b.executeRequest(dev, hdr, dataDescs)

	// Write status
	if err := dev.writeGuest(statusDesc.addr, []byte{status}); err != nil {
		return 0, err
	}

	_ = statusDescIdx // Avoid unused variable warning

	// Calculate total bytes written (status byte)
	return 1, nil
}

func (b *Blk) executeRequest(dev device, hdr virtioBlkReqHdr, dataDescs []virtqDescriptor) byte {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.file == nil {
		return VIRTIO_BLK_S_IOERR
	}

	offset := int64(hdr.sector) * 512

	switch hdr.reqType {
	case VIRTIO_BLK_T_IN: // Read
		for _, desc := range dataDescs {
			if desc.flags&virtqDescFWrite == 0 {
				// Read request should have writable data descriptors
				return VIRTIO_BLK_S_IOERR
			}
			data := make([]byte, desc.length)
			n, err := b.file.ReadAt(data, offset)
			if err != nil && n == 0 {
				debug.Writef("virtio-blk.read", "err=%v offset=%d len=%d", err, offset, desc.length)
				return VIRTIO_BLK_S_IOERR
			}
			if err := dev.writeGuest(desc.addr, data[:n]); err != nil {
				return VIRTIO_BLK_S_IOERR
			}
			offset += int64(n)
		}
		return VIRTIO_BLK_S_OK

	case VIRTIO_BLK_T_OUT: // Write
		if b.readonly {
			return VIRTIO_BLK_S_IOERR
		}
		for _, desc := range dataDescs {
			if desc.flags&virtqDescFWrite != 0 {
				// Write request should have read-only data descriptors
				return VIRTIO_BLK_S_IOERR
			}
			data, err := dev.readGuest(desc.addr, desc.length)
			if err != nil {
				return VIRTIO_BLK_S_IOERR
			}
			n, err := b.file.WriteAt(data, offset)
			if err != nil {
				debug.Writef("virtio-blk.write", "err=%v offset=%d len=%d", err, offset, desc.length)
				return VIRTIO_BLK_S_IOERR
			}
			offset += int64(n)
		}
		return VIRTIO_BLK_S_OK

	case VIRTIO_BLK_T_FLUSH:
		if err := b.file.Sync(); err != nil {
			return VIRTIO_BLK_S_IOERR
		}
		return VIRTIO_BLK_S_OK

	case VIRTIO_BLK_T_GET_ID:
		// Return device ID (20 bytes, null-padded)
		id := make([]byte, 20)
		copy(id, "virtio-blk")
		if len(dataDescs) > 0 && dataDescs[0].flags&virtqDescFWrite != 0 {
			if err := dev.writeGuest(dataDescs[0].addr, id); err != nil {
				return VIRTIO_BLK_S_IOERR
			}
		}
		return VIRTIO_BLK_S_OK

	default:
		return VIRTIO_BLK_S_UNSUPP
	}
}

func (b *Blk) configBytes() []byte {
	b.mu.Lock()
	capacity := b.capacity
	b.mu.Unlock()

	cfg := blkConfig{
		capacity: capacity,
		sizeMax:  1 << 20,    // 1MB max segment
		segMax:   128,        // Max segments
		blkSize:  512,        // Block size
	}

	// Serialize config to bytes
	var buf [32]byte
	binary.LittleEndian.PutUint64(buf[0:8], cfg.capacity)
	binary.LittleEndian.PutUint32(buf[8:12], cfg.sizeMax)
	binary.LittleEndian.PutUint32(buf[12:16], cfg.segMax)
	binary.LittleEndian.PutUint16(buf[16:18], cfg.cylinders)
	buf[18] = cfg.heads
	buf[19] = cfg.sectors
	binary.LittleEndian.PutUint32(buf[20:24], cfg.blkSize)
	return buf[:]
}

var (
	_ hv.MemoryMappedIODevice = (*Blk)(nil)
	_ deviceHandler           = (*Blk)(nil)
	_ Stoppable               = (*Blk)(nil)
)
