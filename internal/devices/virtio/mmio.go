package virtio

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"sync/atomic"

	"github.com/tinyrange/cc/internal/fdt"
	"github.com/tinyrange/cc/internal/hv"
)

// ACPIDeviceInfo contains information needed to generate ACPI DSDT entries.
type ACPIDeviceInfo struct {
	BaseAddr uint64
	Size     uint64
	GSI      uint32
}

type VirtioMMIODevice interface {
	GetLinuxCommandLineParam() ([]string, error)
	DeviceTreeNodes() ([]fdt.Node, error)
	// GetACPIDeviceInfo returns information needed to generate ACPI DSDT entries.
	GetACPIDeviceInfo() ACPIDeviceInfo
}

// AllocatedVirtioMMIODevice is implemented by created VirtIO MMIO devices
// to report their actual allocated MMIO addresses. This is used by the
// LinuxLoader to generate cmdline and device tree entries after device
// creation, when the actual addresses are known.
type AllocatedVirtioMMIODevice interface {
	// AllocatedMMIOBase returns the actual allocated MMIO base address
	AllocatedMMIOBase() uint64
	// AllocatedMMIOSize returns the MMIO region size
	AllocatedMMIOSize() uint64
	// AllocatedIRQLine returns the allocated IRQ line (already encoded for architecture)
	AllocatedIRQLine() uint32
}

// GetAllocatedLinuxCommandLineParam returns the cmdline parameter for an
// allocated VirtIO MMIO device using its actual addresses.
func GetAllocatedLinuxCommandLineParam(dev AllocatedVirtioMMIODevice) string {
	// The IRQ line stored in devices is encoded (for ARM64 it has SPI type bits).
	// For cmdline we need the raw SPI offset.
	irqLine := dev.AllocatedIRQLine()
	// Mask off the type bits to get the raw IRQ number
	rawIRQ := irqLine & 0xFFFF
	return fmt.Sprintf("virtio_mmio.device=4k@0x%x:%d", dev.AllocatedMMIOBase(), rawIRQ)
}

// GetAllocatedDeviceTreeNode returns an FDT node for an allocated VirtIO
// MMIO device using its actual addresses.
func GetAllocatedDeviceTreeNode(dev AllocatedVirtioMMIODevice) fdt.Node {
	base := dev.AllocatedMMIOBase()
	size := dev.AllocatedMMIOSize()
	// The IRQ line stored in devices is encoded (for ARM64 it has SPI type bits).
	// For device tree we need the raw SPI offset.
	irqLine := dev.AllocatedIRQLine()
	rawIRQ := irqLine & 0xFFFF

	return fdt.Node{
		Name: fmt.Sprintf("virtio@%x", base),
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"virtio,mmio"}},
			"reg":        {U64: []uint64{base, size}},
			"interrupts": {U32: []uint32{0, rawIRQ, 4}},
			"status":     {Strings: []string{"okay"}},
		},
	}
}

// GetAllocatedACPIDeviceInfo returns ACPI device info for an allocated
// VirtIO MMIO device using its actual addresses.
func GetAllocatedACPIDeviceInfo(dev AllocatedVirtioMMIODevice) ACPIDeviceInfo {
	// For ACPI GSI we need the raw IRQ number
	irqLine := dev.AllocatedIRQLine()
	rawIRQ := irqLine & 0xFFFF
	return ACPIDeviceInfo{
		BaseAddr: dev.AllocatedMMIOBase(),
		Size:     dev.AllocatedMMIOSize(),
		GSI:      rawIRQ,
	}
}

const (
	VIRTIO_MMIO_MAGIC_VALUE         = 0x000
	VIRTIO_MMIO_VERSION             = 0x004
	VIRTIO_MMIO_DEVICE_ID           = 0x008
	VIRTIO_MMIO_VENDOR_ID           = 0x00c
	VIRTIO_MMIO_DEVICE_FEATURES     = 0x010
	VIRTIO_MMIO_DEVICE_FEATURES_SEL = 0x014
	VIRTIO_MMIO_DRIVER_FEATURES     = 0x020
	VIRTIO_MMIO_DRIVER_FEATURES_SEL = 0x024
	VIRTIO_MMIO_QUEUE_SEL           = 0x030
	VIRTIO_MMIO_QUEUE_NUM_MAX       = 0x034
	VIRTIO_MMIO_QUEUE_NUM           = 0x038
	VIRTIO_MMIO_QUEUE_READY         = 0x044
	VIRTIO_MMIO_QUEUE_NOTIFY        = 0x050
	VIRTIO_MMIO_INTERRUPT_STATUS    = 0x060
	VIRTIO_MMIO_INTERRUPT_ACK       = 0x064
	VIRTIO_MMIO_STATUS              = 0x070
	VIRTIO_MMIO_QUEUE_DESC_LOW      = 0x080
	VIRTIO_MMIO_QUEUE_DESC_HIGH     = 0x084
	VIRTIO_MMIO_QUEUE_AVAIL_LOW     = 0x090
	VIRTIO_MMIO_QUEUE_AVAIL_HIGH    = 0x094
	VIRTIO_MMIO_QUEUE_USED_LOW      = 0x0a0
	VIRTIO_MMIO_QUEUE_USED_HIGH     = 0x0a4
	VIRTIO_MMIO_CONFIG_GENERATION   = 0x0fc
	VIRTIO_MMIO_CONFIG              = 0x100

	// Shared memory region registers (virtio-mmio v2)
	VIRTIO_MMIO_SHM_SEL       = 0x0ac
	VIRTIO_MMIO_SHM_LEN_LOW   = 0x0b0
	VIRTIO_MMIO_SHM_LEN_HIGH  = 0x0b4
	VIRTIO_MMIO_SHM_BASE_LOW  = 0x0b8
	VIRTIO_MMIO_SHM_BASE_HIGH = 0x0bc

	virtioFeatureVersion1 = uint64(1) << 32

	// Interrupt status bits
	VIRTIO_MMIO_INT_VRING  = 0x1 // Used buffer notification
	VIRTIO_MMIO_INT_CONFIG = 0x2 // Configuration change

	virtqDescFNext               = 1
	virtqDescFWrite              = 2
	virtioRingFeatureEventIdxBit = 29
)

type device interface {
	queue(index int) *queue
	readAvailState(*queue) (flags uint16, idx uint16, err error)
	readAvailEntry(*queue, uint16) (uint16, error)
	readDescriptor(*queue, uint16) (virtqDescriptor, error)
	recordUsedElement(*queue, uint16, uint32) error
	raiseInterrupt(uint32) error
	readGuest(addr uint64, length uint32) ([]byte, error)
	writeGuest(addr uint64, data []byte) error
	eventIdxEnabled() bool
	setAvailEvent(*queue, uint16) error
	readMMIO(ctx hv.ExitContext, addr uint64, data []byte) error
	writeMMIO(ctx hv.ExitContext, addr uint64, data []byte) error
	memSlice(addr uint64, length uint64) ([]byte, error)
	queuePointers(q *queue) (descTable []byte, avail []byte, used []byte, err error)
}

type deviceHandler interface {
	NumQueues() int
	QueueMaxSize(queue int) uint16
	OnReset(dev device)
	OnQueueNotify(ctx hv.ExitContext, dev device, queue int) error
	ReadConfig(ctx hv.ExitContext, dev device, offset uint64) (value uint32, handled bool, err error)
	WriteConfig(ctx hv.ExitContext, dev device, offset uint64, value uint32) (handled bool, err error)
}

// deviceHandlerAdapter adapts a deviceHandler to the VirtioDevice interface.
// This allows backward compatibility with existing deviceHandler implementations.
type deviceHandlerAdapter struct {
	handler  deviceHandler
	dev      device
	deviceID uint16
	features uint64
}

func (a *deviceHandlerAdapter) DeviceID() uint16 {
	return a.deviceID
}

func (a *deviceHandlerAdapter) DeviceFeatures() uint64 {
	return a.features
}

func (a *deviceHandlerAdapter) MaxQueues() uint16 {
	return uint16(a.handler.NumQueues())
}

func (a *deviceHandlerAdapter) ReadConfig(ctx hv.ExitContext, offset uint16) uint32 {
	value, handled, _ := a.handler.ReadConfig(ctx, a.dev, uint64(offset))
	if handled {
		return value
	}
	return 0
}

func (a *deviceHandlerAdapter) WriteConfig(ctx hv.ExitContext, offset uint16, val uint32) {
	_, _ = a.handler.WriteConfig(ctx, a.dev, uint64(offset), val)
}

func (a *deviceHandlerAdapter) Enable(features uint64, queues []*VirtQueue) {
	// For deviceHandler, Enable is handled through OnReset and OnQueueNotify
	// This is a no-op adapter
}

func (a *deviceHandlerAdapter) Disable() {
	if a.handler != nil {
		a.handler.OnReset(a.dev)
	}
}

type mmioDevice struct {
	vm hv.VirtualMachine

	base    uint64
	size    uint64
	irqLine uint32
	irqHigh atomic.Bool

	deviceID uint32
	vendorID uint32
	version  uint32

	handler      deviceHandler
	virtioDevice VirtioDevice // New interface, takes precedence if set

	deviceFeatureSel uint32
	driverFeatureSel uint32

	defaultDeviceFeatures []uint32
	deviceFeatures        []uint32
	driverFeatures        []uint32

	queueSel         uint32
	deviceStatus     uint32
	interruptStatus  atomic.Uint32
	configGeneration uint32 // Incremented on config changes
	shmSel           uint32 // Shared memory region selector

	queues []queue
}

type queue struct {
	size         uint16
	maxSize      uint16
	ready        bool
	descAddr     uint64
	availAddr    uint64
	usedAddr     uint64
	lastAvailIdx uint16
	usedIdx      uint16

	enable bool

	notifyOff  uint16
	msixVector uint16
}

type virtqDescriptor struct {
	addr   uint64
	length uint32
	flags  uint16
	next   uint16
}

func (q *queue) reset() {
	q.size = 0
	q.ready = false
	q.descAddr = 0
	q.availAddr = 0
	q.usedAddr = 0
	q.lastAvailIdx = 0
	q.usedIdx = 0
	q.enable = false
}

func ensureQueueReady(q *queue) error {
	if q == nil || !q.ready || q.size == 0 {
		return fmt.Errorf("queue not ready")
	}
	return nil
}

// newMMIODevice creates a new MMIO virtio device.
// It accepts either a deviceHandler (for backward compatibility) or a VirtioDevice.
// If both are provided, VirtioDevice takes precedence.
func newMMIODevice(vm hv.VirtualMachine, base uint64, size uint64, irqLine uint32, deviceID, vendorID, version uint32, featureBits []uint64, handler deviceHandler) *mmioDevice {
	if handler == nil {
		panic("virtio MMIO device requires a handler")
	}
	queueCount := handler.NumQueues()
	if queueCount <= 0 {
		panic("virtio device must expose at least one queue")
	}

	device := &mmioDevice{
		vm:      vm,
		base:    base,
		size:    size,
		irqLine: irqLine,

		deviceID: deviceID,
		vendorID: vendorID,
		version:  version,
		handler:  handler,
	}

	// Create adapter for backward compatibility
	// deviceHandler and VirtioDevice have conflicting method signatures,
	// so we always use the adapter
	device.virtioDevice = &deviceHandlerAdapter{
		handler:  handler,
		dev:      device,
		deviceID: uint16(deviceID),
		features: 0, // Will be set from featureBits
	}

	featureWords := len(featureBits)
	if featureWords == 0 {
		featureWords = 1
	}
	device.defaultDeviceFeatures = make([]uint32, featureWords*2)
	idx := 0
	for _, bitset := range featureBits {
		device.defaultDeviceFeatures[idx] = uint32(bitset & 0xffffffff)
		device.defaultDeviceFeatures[idx+1] = uint32(bitset >> 32)
		idx += 2
	}
	if len(featureBits) == 0 {
		device.defaultDeviceFeatures[0] = 0
		device.defaultDeviceFeatures[1] = 0
	}

	// Set features in adapter if using one
	if adapter, ok := device.virtioDevice.(*deviceHandlerAdapter); ok {
		adapter.features = 0
		for _, bitset := range featureBits {
			adapter.features |= bitset
		}
	}

	device.deviceFeatures = make([]uint32, len(device.defaultDeviceFeatures))
	device.driverFeatures = make([]uint32, len(device.defaultDeviceFeatures))

	device.queues = make([]queue, queueCount)
	for i := range device.queues {
		device.queues[i].maxSize = handler.QueueMaxSize(i)
		if device.queues[i].maxSize == 0 {
			panic(fmt.Sprintf("virtio device queue %d has zero max size", i))
		}
	}

	device.reset()
	return device
}

func (d *mmioDevice) writeMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	if err := d.checkMMIOBounds(addr, uint64(len(data))); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > 8 {
		return fmt.Errorf("unsupported MMIO write length %d", len(data))
	}
	value := littleEndianValue(data, uint32(len(data)))
	return d.writeRegister(ctx, addr-d.base, value)
}

func (d *mmioDevice) readMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	if err := d.checkMMIOBounds(addr, uint64(len(data))); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > 8 {
		return fmt.Errorf("unsupported MMIO read length %d", len(data))
	}
	value, err := d.readRegister(ctx, addr-d.base)
	if err != nil {
		return err
	}
	storeLittleEndian(data, uint32(len(data)), value)
	return nil
}

func (d *mmioDevice) checkMMIOBounds(addr, length uint64) error {
	if addr < d.base || addr+length > d.base+d.size {
		return fmt.Errorf("virtio: mmio access outside region base=%#x size=%#x addr=%#x length=%#x", d.base, d.size, addr, length)
	}
	return nil
}

func (d *mmioDevice) writeRegister(ctx hv.ExitContext, offset uint64, value uint32) error {
	// Helper logger
	logAccess := func(name string) {
		// slog.Info("virtio-mmio: write", "reg", name, "val", fmt.Sprintf("%#x", value), "queue_sel", d.queueSel)
	}

	switch offset {
	case VIRTIO_MMIO_DEVICE_FEATURES_SEL:
		// fmt.Fprintf(os.Stderr, "virtio-mmio: write DEVICE_FEATURES_SEL -> %d\n", value)
		d.deviceFeatureSel = value
	case VIRTIO_MMIO_DRIVER_FEATURES_SEL:
		// fmt.Fprintf(os.Stderr, "virtio-mmio: write DRIVER_FEATURES_SEL -> %d\n", value)
		d.driverFeatureSel = value
	case VIRTIO_MMIO_DRIVER_FEATURES:
		logAccess("DRIVER_FEATURES")
		// fmt.Fprintf(os.Stderr, "virtio-mmio: write DRIVER_FEATURES sel=%d val=%#x\n", d.driverFeatureSel, value)
		if d.driverFeatureSel < uint32(len(d.driverFeatures)) {
			oldValue := d.driverFeatures[d.driverFeatureSel]
			d.driverFeatures[d.driverFeatureSel] = value
			// Increment config generation when features change (feature negotiation)
			if oldValue != value {
				d.configGeneration++
				d.raiseInterrupt(VIRTIO_MMIO_INT_CONFIG)
			}
		}
	case VIRTIO_MMIO_QUEUE_SEL:
		d.queueSel = value
	case VIRTIO_MMIO_SHM_SEL:
		d.shmSel = value
	case VIRTIO_MMIO_QUEUE_NUM:
		logAccess("QUEUE_NUM")
		// fmt.Fprintf(os.Stderr, "virtio-mmio: queue %d size -> %d\n", d.queueSel, value)
		if q := d.currentQueue(); q != nil {
			// RELAXATION: Linux might write 0 during reset. Don't error, just accept it.
			if value > uint32(q.maxSize) {
				slog.Error("virtio-mmio: invalid queue size", "size", value, "max", q.maxSize)
				return fmt.Errorf("queue size %d invalid", value)
			}
			q.size = uint16(value)
		}
	case VIRTIO_MMIO_QUEUE_READY:
		logAccess("QUEUE_READY")
		if q := d.currentQueue(); q != nil {
			if value&0x1 == 0 {
				q.reset()
				return nil
			}
			if q.size == 0 {
				// LOG THE FAILURE HERE
				slog.Error("virtio-mmio: attempt to ready queue with size 0", "idx", d.queueSel)
				return fmt.Errorf("queue ready set before queue size")
			}
			// fmt.Fprintf(os.Stderr, "virtio-mmio: queue %d ready size=%d desc=%#x avail=%#x used=%#x\n", d.queueSel, q.size, q.descAddr, q.availAddr, q.usedAddr)
			q.ready = true

			// Check if all queues are ready and device is enabled, then call Enable()
			if d.deviceStatus&0x4 != 0 { // FEATURES_OK bit set
				allReady := true
				for i := range d.queues {
					if !d.queues[i].ready {
						allReady = false
						break
					}
				}
				if allReady && d.virtioDevice != nil {
					// Convert negotiated features
					negotiatedFeatures := uint64(0)
					for i := range d.driverFeatures {
						negotiatedFeatures |= uint64(d.driverFeatures[i]) << (32 * uint(i))
					}
					// Convert queues to VirtQueue format
					virtQueues := make([]*VirtQueue, len(d.queues))
					for i := range d.queues {
						q := &d.queues[i]
						vq := NewVirtQueue(d.vm, q.maxSize)
						vq.SetAddresses(q.descAddr, q.availAddr, q.usedAddr)
						vq.SetSize(q.size)
						vq.SetReady(true)
						virtQueues[i] = vq
					}
					d.virtioDevice.Enable(negotiatedFeatures, virtQueues)
				}
			}
		}
	case VIRTIO_MMIO_QUEUE_DESC_LOW:
		if q := d.currentQueue(); q != nil {
			q.descAddr = (q.descAddr &^ 0xffffffff) | uint64(value)
			// fmt.Fprintf(os.Stderr, "virtio-mmio: queue %d desc low -> %#x\n", d.queueSel, q.descAddr)
		}
	case VIRTIO_MMIO_QUEUE_DESC_HIGH:
		if q := d.currentQueue(); q != nil {
			q.descAddr = (q.descAddr &^ (uint64(0xffffffff) << 32)) | (uint64(value) << 32)
			// fmt.Fprintf(os.Stderr, "virtio-mmio: queue %d desc high -> %#x\n", d.queueSel, q.descAddr)
		}
	case VIRTIO_MMIO_QUEUE_AVAIL_LOW:
		if q := d.currentQueue(); q != nil {
			q.availAddr = (q.availAddr &^ 0xffffffff) | uint64(value)
			// fmt.Fprintf(os.Stderr, "virtio-mmio: queue %d avail low -> %#x\n", d.queueSel, q.availAddr)
		}
	case VIRTIO_MMIO_QUEUE_AVAIL_HIGH:
		if q := d.currentQueue(); q != nil {
			q.availAddr = (q.availAddr &^ (uint64(0xffffffff) << 32)) | (uint64(value) << 32)
			// fmt.Fprintf(os.Stderr, "virtio-mmio: queue %d avail high -> %#x\n", d.queueSel, q.availAddr)
		}
	case VIRTIO_MMIO_QUEUE_USED_LOW:
		if q := d.currentQueue(); q != nil {
			q.usedAddr = (q.usedAddr &^ 0xffffffff) | uint64(value)
			// fmt.Fprintf(os.Stderr, "virtio-mmio: queue %d used low -> %#x\n", d.queueSel, q.usedAddr)
		}
	case VIRTIO_MMIO_QUEUE_USED_HIGH:
		if q := d.currentQueue(); q != nil {
			q.usedAddr = (q.usedAddr &^ (uint64(0xffffffff) << 32)) | (uint64(value) << 32)
			// fmt.Fprintf(os.Stderr, "virtio-mmio: queue %d used high -> %#x\n", d.queueSel, q.usedAddr)
		}
	case VIRTIO_MMIO_QUEUE_NOTIFY:
		if d.handler != nil {
			err := d.handler.OnQueueNotify(ctx, d, int(value))
			// Queue notification doesn't directly set interrupt status,
			// but the handler may call raiseInterrupt
			return err
		}
	case VIRTIO_MMIO_INTERRUPT_ACK:
		for {
			prev := d.interruptStatus.Load()
			newVal := prev &^ value
			if d.interruptStatus.CompareAndSwap(prev, newVal) {
				if prev != newVal {
					d.updateInterruptLine()
				}
				break
			}
		}
	case VIRTIO_MMIO_STATUS:
		// fmt.Fprintf(os.Stderr, "virtio-mmio: write STATUS -> %#x\n", value)
		if value == 0 {
			d.reset()
			return nil
		}
		d.deviceStatus = value
	case 0x040: // VIRTIO_MMIO_QUEUE_PFN (Legacy)
		slog.Warn("virtio-mmio: linux attempted legacy PFN write - we are modern only!")
		// If you see this log, your Feature Negotiation (Bit 32) is failing.
		return nil
	default:
		if offset >= VIRTIO_MMIO_CONFIG {
			// Device-specific config write
			if d.virtioDevice != nil {
				relOffset := uint16(offset - VIRTIO_MMIO_CONFIG)
				d.virtioDevice.WriteConfig(ctx, relOffset, value)
				// Increment config generation on config change
				d.configGeneration++
				d.raiseInterrupt(VIRTIO_MMIO_INT_CONFIG)
			} else if d.handler != nil {
				handled, err := d.handler.WriteConfig(ctx, d, offset, value)
				if handled {
					// Increment config generation on config change
					d.configGeneration++
					d.raiseInterrupt(VIRTIO_MMIO_INT_CONFIG)
					return err
				}
			}
			logAccess(fmt.Sprintf("CONFIG_OFFSET_%#x", offset))
		} else if d.handler != nil {
			handled, err := d.handler.WriteConfig(ctx, d, offset, value)
			if handled {
				return err
			} else {
				logAccess(fmt.Sprintf("CONFIG_OFFSET_%#x", offset))
			}
		}
	}
	return nil
}

func (d *mmioDevice) readRegister(ctx hv.ExitContext, offset uint64) (uint32, error) {
	switch offset {
	case VIRTIO_MMIO_MAGIC_VALUE:
		// fmt.Fprintf(os.Stderr, "virtio-mmio: read MAGIC -> %#x\n", 0x74726976)
		return 0x74726976, nil
	case VIRTIO_MMIO_VERSION:
		// fmt.Fprintf(os.Stderr, "virtio-mmio: read VERSION -> %d\n", d.version)
		return d.version, nil
	case VIRTIO_MMIO_DEVICE_ID:
		// fmt.Fprintf(os.Stderr, "virtio-mmio: read DEVICE_ID -> %d\n", d.deviceID)
		return d.deviceID, nil
	case VIRTIO_MMIO_VENDOR_ID:
		// fmt.Fprintf(os.Stderr, "virtio-mmio: read VENDOR_ID -> %d\n", d.vendorID)
		return d.vendorID, nil
	case VIRTIO_MMIO_DEVICE_FEATURES:
		if d.deviceFeatureSel < uint32(len(d.deviceFeatures)) {
			val := d.deviceFeatures[d.deviceFeatureSel]
			// fmt.Fprintf(os.Stderr, "virtio-mmio: read DEVICE_FEATURES sel=%d -> %#x\n", d.deviceFeatureSel, val)
			return val, nil
		}
		return 0, nil
	case VIRTIO_MMIO_DEVICE_FEATURES_SEL:
		return d.deviceFeatureSel, nil
	case VIRTIO_MMIO_DRIVER_FEATURES:
		if d.driverFeatureSel < uint32(len(d.driverFeatures)) {
			val := d.driverFeatures[d.driverFeatureSel]
			// fmt.Fprintf(os.Stderr, "virtio-mmio: read DRIVER_FEATURES sel=%d -> %#x\n", d.driverFeatureSel, val)
			return val, nil
		}
		return 0, nil
	case VIRTIO_MMIO_DRIVER_FEATURES_SEL:
		return d.driverFeatureSel, nil
	case VIRTIO_MMIO_QUEUE_SEL:
		return d.queueSel, nil
	case VIRTIO_MMIO_QUEUE_NUM_MAX:
		if q := d.selectedQueue(); q != nil {
			return uint32(q.maxSize), nil
		}
		return 0, nil
	case VIRTIO_MMIO_QUEUE_NUM:
		if q := d.currentQueue(); q != nil {
			// fmt.Fprintf(os.Stderr, "virtio-mmio: read queue %d size -> %d\n", d.queueSel, q.size)
			return uint32(q.size), nil
		}
		return 0, nil
	case VIRTIO_MMIO_QUEUE_READY:
		if q := d.currentQueue(); q != nil && q.ready {
			// fmt.Fprintf(os.Stderr, "virtio-mmio: read queue %d ready -> %t\n", d.queueSel, q.ready)
			return 1, nil
		}
		return 0, nil
	case VIRTIO_MMIO_QUEUE_DESC_LOW:
		if q := d.currentQueue(); q != nil {
			// fmt.Fprintf(os.Stderr, "virtio-mmio: read queue %d desc low -> %#x\n", d.queueSel, q.descAddr)
			return uint32(q.descAddr), nil
		}
		return 0, nil
	case VIRTIO_MMIO_QUEUE_DESC_HIGH:
		if q := d.currentQueue(); q != nil {
			// fmt.Fprintf(os.Stderr, "virtio-mmio: read queue %d desc high -> %#x\n", d.queueSel, q.descAddr)
			return uint32(q.descAddr >> 32), nil
		}
		return 0, nil
	case VIRTIO_MMIO_QUEUE_AVAIL_LOW:
		if q := d.currentQueue(); q != nil {
			// fmt.Fprintf(os.Stderr, "virtio-mmio: read queue %d avail low -> %#x\n", d.queueSel, q.availAddr)
			return uint32(q.availAddr), nil
		}
		return 0, nil
	case VIRTIO_MMIO_QUEUE_AVAIL_HIGH:
		if q := d.currentQueue(); q != nil {
			// fmt.Fprintf(os.Stderr, "virtio-mmio: read queue %d avail high -> %#x\n", d.queueSel, q.availAddr)
			return uint32(q.availAddr >> 32), nil
		}
		return 0, nil
	case VIRTIO_MMIO_QUEUE_USED_LOW:
		if q := d.currentQueue(); q != nil {
			// fmt.Fprintf(os.Stderr, "virtio-mmio: read queue %d used low -> %#x\n", d.queueSel, q.usedAddr)
			return uint32(q.usedAddr), nil
		}
		return 0, nil
	case VIRTIO_MMIO_QUEUE_USED_HIGH:
		if q := d.currentQueue(); q != nil {
			// fmt.Fprintf(os.Stderr, "virtio-mmio: read queue %d used high -> %#x\n", d.queueSel, q.usedAddr)
			return uint32(q.usedAddr >> 32), nil
		}
		return 0, nil
	case VIRTIO_MMIO_INTERRUPT_STATUS:
		return d.interruptStatus.Load(), nil
	case VIRTIO_MMIO_STATUS:
		// fmt.Fprintf(os.Stderr, "virtio-mmio: read STATUS -> %#x\n", d.deviceStatus)
		return d.deviceStatus, nil
	case VIRTIO_MMIO_SHM_SEL:
		return d.shmSel, nil
	case VIRTIO_MMIO_SHM_LEN_LOW:
		// Return ~0 to indicate no shared memory region exists
		return 0xFFFFFFFF, nil
	case VIRTIO_MMIO_SHM_LEN_HIGH:
		// Return ~0 to indicate no shared memory region exists
		return 0xFFFFFFFF, nil
	case VIRTIO_MMIO_SHM_BASE_LOW:
		// Return ~0 to indicate no shared memory region exists
		return 0xFFFFFFFF, nil
	case VIRTIO_MMIO_SHM_BASE_HIGH:
		// Return ~0 to indicate no shared memory region exists
		return 0xFFFFFFFF, nil
	case VIRTIO_MMIO_CONFIG_GENERATION:
		return d.configGeneration, nil
	default:
		if offset >= VIRTIO_MMIO_CONFIG {
			// Device-specific config read
			if d.virtioDevice != nil {
				relOffset := uint16(offset - VIRTIO_MMIO_CONFIG)
				return d.virtioDevice.ReadConfig(ctx, relOffset), nil
			} else if d.handler != nil {
				value, handled, err := d.handler.ReadConfig(ctx, d, offset)
				if handled {
					return value, err
				}
			}
			return 0, nil
		} else if d.handler != nil {
			value, handled, err := d.handler.ReadConfig(ctx, d, offset)
			if handled {
				return value, err
			}
		}
		return 0, nil
	}
}

func (d *mmioDevice) reset() {
	d.deviceFeatureSel = 0
	d.driverFeatureSel = 0
	copy(d.deviceFeatures, d.defaultDeviceFeatures)
	for i := range d.driverFeatures {
		d.driverFeatures[i] = 0
	}
	d.queueSel = 0
	d.deviceStatus = 0
	d.interruptStatus.Store(0)
	d.irqHigh.Store(false)
	d.configGeneration = 0
	for i := range d.queues {
		d.queues[i].reset()
		d.queues[i].maxSize = d.handler.QueueMaxSize(i)
	}
	if d.virtioDevice != nil {
		d.virtioDevice.Disable()
	} else if d.handler != nil {
		d.handler.OnReset(d)
	}
}

func (d *mmioDevice) currentQueue() *queue {
	idx := int(d.queueSel)
	if idx < 0 || idx >= len(d.queues) {
		return nil
	}
	return &d.queues[idx]
}

func (d *mmioDevice) selectedQueue() *queue {
	return d.currentQueue()
}

func (d *mmioDevice) queue(index int) *queue {
	if index < 0 || index >= len(d.queues) {
		return nil
	}
	return &d.queues[index]
}

func (d *mmioDevice) raiseInterrupt(bit uint32) error {
	d.interruptStatus.Or(bit)
	return d.updateInterruptLine()
}

func (d *mmioDevice) updateInterruptLine() error {
	if d.vm == nil || d.irqLine == 0 {
		return fmt.Errorf("virtio: virtual machine or irq line is nil")
	}
	levelAsserted := d.interruptStatus.Load() != 0
	// Only call SetIRQ if the level actually changed to avoid spurious interrupts.
	// Use Swap to atomically update and get the previous value.
	prevHigh := d.irqHigh.Swap(levelAsserted)
	if levelAsserted == prevHigh {
		return nil
	}
	if err := d.vm.SetIRQ(d.irqLine, levelAsserted); err != nil {
		slog.Error("virtio: pulse irq failed", "irq", fmt.Sprintf("0x%X", d.irqLine), "err", err)
		return err
	}
	return nil
}

func (d *mmioDevice) readAvailState(q *queue) (uint16, uint16, error) {
	if err := ensureQueueReady(q); err != nil {
		return 0, 0, err
	}
	var header [4]byte
	if err := d.readGuestInto(q.availAddr, header[:]); err != nil {
		return 0, 0, err
	}
	flags := binary.LittleEndian.Uint16(header[0:2])
	idx := binary.LittleEndian.Uint16(header[2:4])
	return flags, idx, nil
}

func (d *mmioDevice) readAvailEntry(q *queue, ringIndex uint16) (uint16, error) {
	if err := ensureQueueReady(q); err != nil {
		return 0, err
	}
	if ringIndex >= q.size {
		return 0, fmt.Errorf("avail ring index %d out of bounds", ringIndex)
	}
	var buf [2]byte
	offset := q.availAddr + 4 + uint64(ringIndex)*2
	if err := d.readGuestInto(offset, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(buf[:]), nil
}

func (d *mmioDevice) readDescriptor(q *queue, index uint16) (virtqDescriptor, error) {
	if err := ensureQueueReady(q); err != nil {
		return virtqDescriptor{}, err
	}
	if index >= q.size {
		return virtqDescriptor{}, fmt.Errorf("descriptor index %d out of bounds", index)
	}
	var buf [16]byte
	offset := q.descAddr + uint64(index)*16
	if err := d.readGuestInto(offset, buf[:]); err != nil {
		return virtqDescriptor{}, err
	}
	return virtqDescriptor{
		addr:   binary.LittleEndian.Uint64(buf[0:8]),
		length: binary.LittleEndian.Uint32(buf[8:12]),
		flags:  binary.LittleEndian.Uint16(buf[12:14]),
		next:   binary.LittleEndian.Uint16(buf[14:16]),
	}, nil
}

func (d *mmioDevice) readGuest(addr uint64, length uint32) ([]byte, error) {
	if length == 0 {
		return nil, nil
	}
	buf := make([]byte, length)
	if err := d.readGuestInto(addr, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (d *mmioDevice) writeGuest(addr uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return d.writeGuestFrom(addr, data)
}

func (d *mmioDevice) readGuestInto(addr uint64, buf []byte) error {
	if len(buf) == 0 {
		return nil
	}
	if d.vm == nil {
		return fmt.Errorf("virtio: virtual machine is nil")
	}
	off, err := guestOffset(addr, len(buf))
	if err != nil {
		return err
	}
	n, err := d.vm.ReadAt(buf, off)
	if err != nil {
		return err
	}
	if n != len(buf) {
		return fmt.Errorf("virtio: short guest memory read (want %d, got %d)", len(buf), n)
	}
	return nil
}

func (d *mmioDevice) writeGuestFrom(addr uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if d.vm == nil {
		return fmt.Errorf("virtio: virtual machine is nil")
	}
	off, err := guestOffset(addr, len(data))
	if err != nil {
		return err
	}
	n, err := d.vm.WriteAt(data, off)
	if err != nil {
		return err
	}
	if n != len(data) {
		return fmt.Errorf("virtio: short guest memory write (want %d, got %d)", len(data), n)
	}
	return nil
}

func guestOffset(addr uint64, length int) (int64, error) {
	if length < 0 {
		return 0, fmt.Errorf("virtio: negative length %d", length)
	}
	if addr > math.MaxInt64 {
		return 0, fmt.Errorf("virtio: guest address %#x out of range", addr)
	}
	if uint64(length) > uint64(math.MaxInt64)-addr {
		return 0, fmt.Errorf("virtio: guest access length overflow addr=%#x length=%d", addr, length)
	}
	return int64(addr), nil
}

func (d *mmioDevice) recordUsedElement(q *queue, head uint16, length uint32) error {
	if err := ensureQueueReady(q); err != nil {
		return err
	}
	usedIdx := q.usedIdx % q.size
	base := q.usedAddr + 4 + uint64(usedIdx)*8
	if err := d.writeGuestUint32(base, uint32(head)); err != nil {
		return err
	}
	if err := d.writeGuestUint32(base+4, length); err != nil {
		return err
	}
	q.usedIdx++
	return d.writeGuestUint16(q.usedAddr+2, q.usedIdx)
}

func (d *mmioDevice) readGuestUint16(addr uint64) (uint16, error) {
	var buf [2]byte
	if err := d.readGuestInto(addr, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(buf[:]), nil
}

func (d *mmioDevice) writeGuestUint16(addr uint64, value uint16) error {
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], value)
	return d.writeGuestFrom(addr, buf[:])
}

func (d *mmioDevice) writeGuestUint32(addr uint64, value uint32) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], value)
	return d.writeGuestFrom(addr, buf[:])
}

func (d *mmioDevice) driverFeatureEnabled(bit uint32) bool {
	index := bit / 32
	offset := bit % 32
	if int(index) >= len(d.driverFeatures) {
		return false
	}
	return d.driverFeatures[index]&(1<<offset) != 0
}

func (d *mmioDevice) eventIdxEnabled() bool {
	return d.driverFeatureEnabled(virtioRingFeatureEventIdxBit)
}

func (d *mmioDevice) setAvailEvent(q *queue, value uint16) error {
	if err := ensureQueueReady(q); err != nil {
		return err
	}
	if !d.eventIdxEnabled() {
		return nil
	}
	offset := q.usedAddr + 4 + uint64(q.size)*8
	return d.writeGuestUint16(offset, value)
}

func (d *mmioDevice) memSlice(addr uint64, length uint64) ([]byte, error) {
	if length > math.MaxUint32 {
		return nil, fmt.Errorf("memSlice: length %d exceeds uint32 max", length)
	}
	return d.readGuest(addr, uint32(length))
}

func (d *mmioDevice) queuePointers(q *queue) (descTable []byte, avail []byte, used []byte, err error) {
	if err := ensureQueueReady(q); err != nil {
		return nil, nil, nil, err
	}

	// Read descriptor table
	descTableSize := uint64(q.size) * 16
	descTable, err = d.readGuest(q.descAddr, uint32(descTableSize))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read descriptor table: %w", err)
	}

	// Read available ring (header + ring + event idx if enabled)
	availSize := 4 + uint64(q.size)*2
	if d.eventIdxEnabled() {
		availSize += 2
	}
	avail, err = d.readGuest(q.availAddr, uint32(availSize))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read available ring: %w", err)
	}

	// Read used ring (header + ring + event idx if enabled)
	usedSize := 4 + uint64(q.size)*8
	if d.eventIdxEnabled() {
		usedSize += 2
	}
	used, err = d.readGuest(q.usedAddr, uint32(usedSize))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read used ring: %w", err)
	}

	return descTable, avail, used, nil
}

// QueueSnapshot holds the state of a virtio queue for snapshotting
type QueueSnapshot struct {
	Size         uint16
	MaxSize      uint16
	Ready        bool
	DescAddr     uint64
	AvailAddr    uint64
	UsedAddr     uint64
	LastAvailIdx uint16
	UsedIdx      uint16
	Enable       bool
}

// MMIODeviceSnapshot holds the base MMIO device state for snapshotting
type MMIODeviceSnapshot struct {
	DeviceFeatureSel uint32
	DriverFeatureSel uint32
	DeviceFeatures   []uint32
	DriverFeatures   []uint32
	QueueSel         uint32
	DeviceStatus     uint32
	InterruptStatus  uint32
	ConfigGeneration uint32
	Queues           []QueueSnapshot
}

// captureQueueSnapshot captures the state of a single queue
func (q *queue) captureSnapshot() QueueSnapshot {
	return QueueSnapshot{
		Size:         q.size,
		MaxSize:      q.maxSize,
		Ready:        q.ready,
		DescAddr:     q.descAddr,
		AvailAddr:    q.availAddr,
		UsedAddr:     q.usedAddr,
		LastAvailIdx: q.lastAvailIdx,
		UsedIdx:      q.usedIdx,
		Enable:       q.enable,
	}
}

// restoreSnapshot restores a queue from a snapshot
func (q *queue) restoreSnapshot(snap QueueSnapshot) {
	q.size = snap.Size
	q.maxSize = snap.MaxSize
	q.ready = snap.Ready
	q.descAddr = snap.DescAddr
	q.availAddr = snap.AvailAddr
	q.usedAddr = snap.UsedAddr
	q.lastAvailIdx = snap.LastAvailIdx
	q.usedIdx = snap.UsedIdx
	q.enable = snap.Enable
}

// CaptureMMIOSnapshot captures the base MMIO device state
func (d *mmioDevice) CaptureMMIOSnapshot() MMIODeviceSnapshot {
	snap := MMIODeviceSnapshot{
		DeviceFeatureSel: d.deviceFeatureSel,
		DriverFeatureSel: d.driverFeatureSel,
		DeviceFeatures:   make([]uint32, len(d.deviceFeatures)),
		DriverFeatures:   make([]uint32, len(d.driverFeatures)),
		QueueSel:         d.queueSel,
		DeviceStatus:     d.deviceStatus,
		InterruptStatus:  d.interruptStatus.Load(),
		ConfigGeneration: d.configGeneration,
		Queues:           make([]QueueSnapshot, len(d.queues)),
	}
	copy(snap.DeviceFeatures, d.deviceFeatures)
	copy(snap.DriverFeatures, d.driverFeatures)
	for i := range d.queues {
		snap.Queues[i] = d.queues[i].captureSnapshot()
	}
	return snap
}

// RestoreMMIOSnapshot restores the base MMIO device state from a snapshot
func (d *mmioDevice) RestoreMMIOSnapshot(snap MMIODeviceSnapshot) error {
	if len(snap.Queues) != len(d.queues) {
		return fmt.Errorf("queue count mismatch: snapshot has %d, device has %d", len(snap.Queues), len(d.queues))
	}
	if len(snap.DeviceFeatures) != len(d.deviceFeatures) {
		return fmt.Errorf("device features length mismatch: snapshot has %d, device has %d", len(snap.DeviceFeatures), len(d.deviceFeatures))
	}
	if len(snap.DriverFeatures) != len(d.driverFeatures) {
		return fmt.Errorf("driver features length mismatch: snapshot has %d, device has %d", len(snap.DriverFeatures), len(d.driverFeatures))
	}

	d.deviceFeatureSel = snap.DeviceFeatureSel
	d.driverFeatureSel = snap.DriverFeatureSel
	copy(d.deviceFeatures, snap.DeviceFeatures)
	copy(d.driverFeatures, snap.DriverFeatures)
	d.queueSel = snap.QueueSel
	d.deviceStatus = snap.DeviceStatus
	d.interruptStatus.Store(snap.InterruptStatus)
	d.configGeneration = snap.ConfigGeneration

	for i := range d.queues {
		d.queues[i].restoreSnapshot(snap.Queues[i])
	}

	return nil
}

func littleEndianValue(buf []byte, length uint32) uint32 {
	switch length {
	case 1:
		return uint32(buf[0])
	case 2:
		return uint32(binary.LittleEndian.Uint16(buf))
	case 4:
		return binary.LittleEndian.Uint32(buf)
	case 8:
		return uint32(binary.LittleEndian.Uint64(buf))
	default:
		panic(fmt.Sprintf("unsupported little-endian width %d", length))
	}
}

func storeLittleEndian(buf []byte, length uint32, value uint32) {
	switch length {
	case 1:
		buf[0] = byte(value)
	case 2:
		binary.LittleEndian.PutUint16(buf, uint16(value))
	case 4:
		binary.LittleEndian.PutUint32(buf, value)
	case 8:
		binary.LittleEndian.PutUint64(buf, uint64(value))
	default:
		panic(fmt.Sprintf("unsupported little-endian width %d", length))
	}
}
