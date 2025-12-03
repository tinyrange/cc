package virtio

import (
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"

	"github.com/tinyrange/cc/internal/fdt"
	"github.com/tinyrange/cc/internal/hv"
)

type VirtioMMIODevice interface {
	GetLinuxCommandLineParam() ([]string, error)
	DeviceTreeNodes() ([]fdt.Node, error)
}

type irqSetter interface {
	SetIRQ(irqLine uint32, level bool) error
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

	virtioFeatureVersion1 = uint64(1) << 32

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
	raiseInterrupt(uint32)
	readGuest(addr uint64, length uint32) ([]byte, error)
	writeGuest(addr uint64, data []byte) error
	eventIdxEnabled() bool
	setAvailEvent(*queue, uint16) error
	readMMIO(addr uint64, data []byte) error
	writeMMIO(addr uint64, data []byte) error
}

type deviceHandler interface {
	NumQueues() int
	QueueMaxSize(queue int) uint16
	OnReset(dev device)
	OnQueueNotify(dev device, queue int) error
	ReadConfig(dev device, offset uint64) (value uint32, handled bool, err error)
	WriteConfig(dev device, offset uint64, value uint32) (handled bool, err error)
}

type mmioDevice struct {
	vm hv.VirtualMachine

	base    uint64
	size    uint64
	irqLine uint32

	deviceID uint32
	vendorID uint32
	version  uint32

	handler deviceHandler

	deviceFeatureSel uint32
	driverFeatureSel uint32

	defaultDeviceFeatures []uint32
	deviceFeatures        []uint32
	driverFeatures        []uint32

	queueSel        uint32
	deviceStatus    uint32
	interruptStatus uint32

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

func (d *mmioDevice) writeMMIO(addr uint64, data []byte) error {
	if err := d.checkMMIOBounds(addr, uint64(len(data))); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > 8 {
		return fmt.Errorf("unsupported MMIO write length %d", len(data))
	}
	value := littleEndianValue(data, uint32(len(data)))
	return d.writeRegister(addr-d.base, value)
}

func (d *mmioDevice) readMMIO(addr uint64, data []byte) error {
	if err := d.checkMMIOBounds(addr, uint64(len(data))); err != nil {
		return err
	}
	if len(data) == 0 || len(data) > 8 {
		return fmt.Errorf("unsupported MMIO read length %d", len(data))
	}
	value, err := d.readRegister(addr - d.base)
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

func (d *mmioDevice) writeRegister(offset uint64, value uint32) error {
	// Helper logger
	logAccess := func(name string) {
		// slog.Info("virtio-mmio: write", "reg", name, "val", fmt.Sprintf("%#x", value), "queue_sel", d.queueSel)
	}

	switch offset {
	case VIRTIO_MMIO_DEVICE_FEATURES_SEL:
		d.deviceFeatureSel = value
	case VIRTIO_MMIO_DRIVER_FEATURES_SEL:
		d.driverFeatureSel = value
	case VIRTIO_MMIO_DRIVER_FEATURES:
		logAccess("DRIVER_FEATURES")
		if d.driverFeatureSel < uint32(len(d.driverFeatures)) {
			d.driverFeatures[d.driverFeatureSel] = value
		}
	case VIRTIO_MMIO_QUEUE_SEL:
		logAccess("QUEUE_SEL")
		d.queueSel = value
	case VIRTIO_MMIO_QUEUE_NUM:
		logAccess("QUEUE_NUM")
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
			q.ready = true
		}
	case VIRTIO_MMIO_QUEUE_DESC_LOW:
		if q := d.currentQueue(); q != nil {
			q.descAddr = (q.descAddr &^ 0xffffffff) | uint64(value)
		}
	case VIRTIO_MMIO_QUEUE_DESC_HIGH:
		if q := d.currentQueue(); q != nil {
			q.descAddr = (q.descAddr &^ (uint64(0xffffffff) << 32)) | (uint64(value) << 32)
		}
	case VIRTIO_MMIO_QUEUE_AVAIL_LOW:
		if q := d.currentQueue(); q != nil {
			q.availAddr = (q.availAddr &^ 0xffffffff) | uint64(value)
		}
	case VIRTIO_MMIO_QUEUE_AVAIL_HIGH:
		if q := d.currentQueue(); q != nil {
			q.availAddr = (q.availAddr &^ (uint64(0xffffffff) << 32)) | (uint64(value) << 32)
		}
	case VIRTIO_MMIO_QUEUE_USED_LOW:
		if q := d.currentQueue(); q != nil {
			q.usedAddr = (q.usedAddr &^ 0xffffffff) | uint64(value)
		}
	case VIRTIO_MMIO_QUEUE_USED_HIGH:
		if q := d.currentQueue(); q != nil {
			q.usedAddr = (q.usedAddr &^ (uint64(0xffffffff) << 32)) | (uint64(value) << 32)
		}
	case VIRTIO_MMIO_QUEUE_NOTIFY:
		if d.handler != nil {
			return d.handler.OnQueueNotify(d, int(value))
		}
	case VIRTIO_MMIO_INTERRUPT_ACK:
		d.interruptStatus &^= value
	case VIRTIO_MMIO_STATUS:
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
		if d.handler != nil {
			handled, err := d.handler.WriteConfig(d, offset, value)
			if handled {
				return err
			} else {
				logAccess(fmt.Sprintf("CONFIG_OFFSET_%#x", offset))
			}
		}
	}
	return nil
}

func (d *mmioDevice) readRegister(offset uint64) (uint32, error) {
	switch offset {
	case VIRTIO_MMIO_MAGIC_VALUE:
		return 0x74726976, nil
	case VIRTIO_MMIO_VERSION:
		return d.version, nil
	case VIRTIO_MMIO_DEVICE_ID:
		return d.deviceID, nil
	case VIRTIO_MMIO_VENDOR_ID:
		return d.vendorID, nil
	case VIRTIO_MMIO_DEVICE_FEATURES:
		if d.deviceFeatureSel < uint32(len(d.deviceFeatures)) {
			return d.deviceFeatures[d.deviceFeatureSel], nil
		}
		return 0, nil
	case VIRTIO_MMIO_DEVICE_FEATURES_SEL:
		return d.deviceFeatureSel, nil
	case VIRTIO_MMIO_DRIVER_FEATURES:
		if d.driverFeatureSel < uint32(len(d.driverFeatures)) {
			return d.driverFeatures[d.driverFeatureSel], nil
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
			return uint32(q.size), nil
		}
		return 0, nil
	case VIRTIO_MMIO_QUEUE_READY:
		if q := d.currentQueue(); q != nil && q.ready {
			return 1, nil
		}
		return 0, nil
	case VIRTIO_MMIO_QUEUE_DESC_LOW:
		if q := d.currentQueue(); q != nil {
			return uint32(q.descAddr), nil
		}
		return 0, nil
	case VIRTIO_MMIO_QUEUE_DESC_HIGH:
		if q := d.currentQueue(); q != nil {
			return uint32(q.descAddr >> 32), nil
		}
		return 0, nil
	case VIRTIO_MMIO_QUEUE_AVAIL_LOW:
		if q := d.currentQueue(); q != nil {
			return uint32(q.availAddr), nil
		}
		return 0, nil
	case VIRTIO_MMIO_QUEUE_AVAIL_HIGH:
		if q := d.currentQueue(); q != nil {
			return uint32(q.availAddr >> 32), nil
		}
		return 0, nil
	case VIRTIO_MMIO_QUEUE_USED_LOW:
		if q := d.currentQueue(); q != nil {
			return uint32(q.usedAddr), nil
		}
		return 0, nil
	case VIRTIO_MMIO_QUEUE_USED_HIGH:
		if q := d.currentQueue(); q != nil {
			return uint32(q.usedAddr >> 32), nil
		}
		return 0, nil
	case VIRTIO_MMIO_INTERRUPT_STATUS:
		return d.interruptStatus, nil
	case VIRTIO_MMIO_STATUS:
		return d.deviceStatus, nil
	case VIRTIO_MMIO_CONFIG_GENERATION:
		return 0, nil
	default:
		if d.handler != nil {
			value, handled, err := d.handler.ReadConfig(d, offset)
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
	d.interruptStatus = 0
	for i := range d.queues {
		d.queues[i].reset()
		d.queues[i].maxSize = d.handler.QueueMaxSize(i)
	}
	if d.handler != nil {
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

func (d *mmioDevice) raiseInterrupt(bit uint32) {
	d.interruptStatus |= bit
	if d.vm == nil || d.irqLine == 0 {
		return
	}
	if setter, ok := d.vm.(irqSetter); ok {
		if err := setter.SetIRQ(d.irqLine, d.interruptStatus != 0); err != nil {
			slog.Error("virtio: pulse irq failed", "irq", d.irqLine, "err", err)
		}
	}
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
