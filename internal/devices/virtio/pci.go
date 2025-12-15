package virtio

import (
	"encoding/binary"
	"fmt"
	"log/slog"

	"github.com/tinyrange/cc/internal/devices/pci"
	"github.com/tinyrange/cc/internal/hv"
)

const (
	// PCI Vendor and Device IDs
	VIRTIO_PCI_VENDOR_ID      = 0x1AF4
	VIRTIO_PCI_DEVICE_ID_BASE = 0x1040 // Modern VirtIO devices start at 0x1040

	// VirtIO PCI Capability Types
	VIRTIO_PCI_CAP_COMMON_CFG = 1
	VIRTIO_PCI_CAP_NOTIFY_CFG = 2
	VIRTIO_PCI_CAP_ISR_CFG    = 3
	VIRTIO_PCI_CAP_DEVICE_CFG = 4
	VIRTIO_PCI_CAP_PCI_CFG    = 5

	// Common Configuration Structure offsets
	VIRTIO_PCI_COMMON_DFSELECT      = 0x00 // Device Feature Select
	VIRTIO_PCI_COMMON_DF            = 0x04 // Device Features
	VIRTIO_PCI_COMMON_GFSELECT      = 0x08 // Guest Feature Select
	VIRTIO_PCI_COMMON_GF            = 0x0C // Guest Features
	VIRTIO_PCI_COMMON_MSIX          = 0x10 // MSI-X Config Vector
	VIRTIO_PCI_COMMON_NUMQ          = 0x12 // Number of Queues
	VIRTIO_PCI_COMMON_STATUS        = 0x14 // Device Status
	VIRTIO_PCI_COMMON_CFGGENERATION = 0x15 // Config Generation
	VIRTIO_PCI_COMMON_Q_SELECT      = 0x16 // Queue Select
	VIRTIO_PCI_COMMON_Q_SIZE        = 0x18 // Queue Size
	VIRTIO_PCI_COMMON_Q_MSIX        = 0x1A // Queue MSI-X Vector
	VIRTIO_PCI_COMMON_Q_ENABLE      = 0x1C // Queue Enable
	VIRTIO_PCI_COMMON_Q_NOFF        = 0x1E // Queue Notify Off
	VIRTIO_PCI_COMMON_Q_DESCLO      = 0x20 // Queue Descriptor Low
	VIRTIO_PCI_COMMON_Q_DESCHI      = 0x24 // Queue Descriptor High
	VIRTIO_PCI_COMMON_Q_AVAILLO     = 0x28 // Queue Available Low
	VIRTIO_PCI_COMMON_Q_AVAILHI     = 0x2C // Queue Available High
	VIRTIO_PCI_COMMON_Q_USEDLO      = 0x30 // Queue Used Low
	VIRTIO_PCI_COMMON_Q_USEDHI      = 0x34 // Queue Used High

	// MSI-X
	VIRTIO_MSI_NO_VECTOR = 0xFFFF
)

const (
	virtioVendorCapID     = 0x09
	virtioPCICapLen       = 16
	virtioPCINotifyCapLen = 20
	virtioPCICapStart     = 0x60
	msiCapabilityOffset   = 0x40
	msixCapabilityOffset  = 0x50
)

const (
	barAttrMaskMemory uint32 = 0xf
	barAttrMaskIO     uint32 = 0x3
	type0BARCount            = 6
	type0BAROffset           = 0x10
)

const invalidBARIndex = -1

const (
	msiControl64BitCap        = uint16(1 << 7)
	pciStatusCapabilitiesList = 0x10
	pciCapIDMSIX              = 0x11
)

const (
	virtioPCIDefaultIRQLine = 10
	pciInterruptPinINTA     = 0x01
	pciCommandIntxDisable   = 1 << 10
	virtioPCIExposeMSI      = true
	virtioPCIExposeMSIX     = true
	virtioPCIDebug          = false
)

const (
	msixControlEnableBit    = uint16(1 << 15)
	msixControlFunctionMask = uint16(1 << 14)
	msixTableSizeMask       = uint16(0x07ff)
	msixEntrySize           = 16
)

type msiCapableVM interface {
	SignalMSI(addr uint64, data uint32, flags uint32) error
}

func shouldExposePCIMSI(vm hv.VirtualMachine) bool {
	if !virtioPCIExposeMSI {
		return false
	}
	if vm == nil {
		return false
	}
	if _, ok := vm.(msiCapableVM); !ok {
		return false
	}
	return true
}

func shouldExposePCIMSIX(vm hv.VirtualMachine) bool {
	if !virtioPCIExposeMSIX {
		return false
	}
	if vm == nil {
		return false
	}
	if _, ok := vm.(msiCapableVM); !ok {
		return false
	}
	return true
}

type pciBAR struct {
	size       uint64
	attributes uint32
	isIO       bool
	is64       bool
	aliasOf    int

	rawLow  uint32
	rawHigh uint32
	value   uint64

	sizing bool
}

func (b *pciBAR) sizeMask() uint64 {
	if b == nil || b.size == 0 {
		return 0
	}
	mask := ^(b.size - 1)
	if b.isIO {
		return mask & 0xfffffffffffffffC
	}
	return mask & 0xfffffffffffffff0
}

func regionContains(base uint64, length uint32, addr uint64, accessLen uint32) bool {
	if length == 0 || accessLen == 0 {
		return false
	}
	end := base + uint64(length)
	accessEnd := addr + uint64(accessLen)
	return base != 0 && addr >= base && accessEnd <= end
}

type msixEntry struct {
	addr   uint64
	data   uint32
	masked bool
}

// VirtioPCIDevice implements a virtio device using the PCI transport.
type VirtioPCIDevice struct {
	vm hv.VirtualMachine

	exposeMSI  bool
	exposeMSIX bool

	// PCI Configuration
	busNum         uint8
	devNum         uint8
	funcNum        uint8
	pciHost        *pci.HostBridge
	endpointHandle *pci.DeviceHandle

	irqLine       uint32
	interruptLine uint8
	interruptPin  uint8
	capPointer    uint8

	command uint16
	status  uint16

	bars [type0BARCount]pciBAR

	// BAR mappings
	commonCfgBAR    uint8
	commonCfgOffset uint32
	commonCfgLength uint32

	notifyCfgBAR        uint8
	notifyCfgOffset     uint32
	notifyCfgLength     uint32
	notifyOffMultiplier uint32

	isrCfgBAR    uint8
	isrCfgOffset uint32
	isrCfgLength uint32

	deviceCfgBAR    uint8
	deviceCfgOffset uint32
	deviceCfgLength uint32

	// Device properties
	commonCfgAddr uint64
	notifyCfgAddr uint64
	isrCfgAddr    uint64
	deviceCfgAddr uint64
	msiCapOffset  uint16
	msiCapNext    uint8
	msiControl    uint16
	msiAddress    uint64
	msiData       uint16

	commonCfgCapOffset uint16
	notifyCfgCapOffset uint16
	isrCfgCapOffset    uint16
	deviceCfgCapOffset uint16

	commonCfgCapData []byte
	notifyCfgCapData []byte
	isrCfgCapData    []byte
	deviceCfgCapData []byte

	deviceID          uint16
	vendorID          uint16
	subsystemDeviceID uint16
	subsystemVendorID uint16

	handler      deviceHandler
	virtioDevice VirtioDevice // New interface, takes precedence if set

	// Feature negotiation
	deviceFeatureSel      uint32
	guestFeatureSel       uint32
	defaultDeviceFeatures []uint32
	deviceFeatures        []uint32
	guestFeatures         []uint32

	// Device state
	queueSel        uint16
	deviceStatus    uint8
	cfgGeneration   uint8
	interruptStatus uint8

	// MSI-X
	msixConfigVector uint16
	supportsMSIX     bool
	msixCapOffset    uint16
	msixCapNext      uint8
	msixControl      uint16
	msixTableBAR     uint8
	msixTableOffset  uint32
	msixTableLength  uint32
	msixPBABAR       uint8
	msixPBAOffset    uint32
	msixPBALength    uint32
	msixTableAddr    uint64
	msixPBAAddr      uint64
	msixEntries      []msixEntry
	msixPending      []uint64

	// Queues
	queues []queue
}

// NewVirtioPCIDevice creates a new PCI virtio device.
// It accepts either a deviceHandler (for backward compatibility) or a VirtioDevice.
// If both are provided, VirtioDevice takes precedence.
func NewVirtioPCIDevice(vm hv.VirtualMachine, host *pci.HostBridge, busNum, devNum, funcNum uint8, virtioDeviceID, subsystemDeviceID uint16, featureBits []uint64, handler deviceHandler) (*VirtioPCIDevice, error) {
	if handler == nil {
		panic("virtio PCI device requires a handler")
	}
	queueCount := handler.NumQueues()
	if queueCount <= 0 {
		panic("virtio device must expose at least one queue")
	}

	pciDeviceID := VIRTIO_PCI_DEVICE_ID_BASE + virtioDeviceID

	device := &VirtioPCIDevice{
		vm:         vm,
		exposeMSI:  shouldExposePCIMSI(vm),
		exposeMSIX: shouldExposePCIMSIX(vm),
		busNum:     busNum,
		devNum:     devNum,
		funcNum:    funcNum,

		deviceID:          pciDeviceID,
		vendorID:          VIRTIO_PCI_VENDOR_ID,
		subsystemDeviceID: subsystemDeviceID,
		subsystemVendorID: VIRTIO_PCI_VENDOR_ID,

		handler: handler,

		// Default BAR layout
		commonCfgBAR:    0,
		commonCfgOffset: 0x0000,
		commonCfgLength: 0x38,

		notifyCfgBAR:        2,
		notifyCfgOffset:     0x0000,
		notifyCfgLength:     uint32(queueCount) * 4,
		notifyOffMultiplier: 4,

		isrCfgBAR:    1,
		isrCfgOffset: 0x0000,
		isrCfgLength: 0x1,

		deviceCfgBAR:    4,
		deviceCfgOffset: 0x0000,
		deviceCfgLength: 0x1000,

		irqLine:       virtioPCIDefaultIRQLine,
		interruptLine: uint8(virtioPCIDefaultIRQLine),
		interruptPin:  pciInterruptPinINTA,

		msixConfigVector: VIRTIO_MSI_NO_VECTOR,
	}

	// Create adapter for backward compatibility
	device.virtioDevice = &deviceHandlerAdapter{
		handler:  handler,
		dev:      device,
		deviceID: uint16(virtioDeviceID),
		features: 0, // Will be set from featureBits
	}
	// Set features in adapter
	if adapter, ok := device.virtioDevice.(*deviceHandlerAdapter); ok {
		adapter.features = 0
		for _, bitset := range featureBits {
			adapter.features |= bitset
		}
	}

	// Initialize queues before configuring capabilities so MSI-X sizing works.
	device.queues = make([]queue, queueCount)
	for i := range device.queues {
		device.queues[i].maxSize = handler.QueueMaxSize(i)
		device.queues[i].msixVector = VIRTIO_MSI_NO_VECTOR
		device.queues[i].notifyOff = uint16(i)
		if device.queues[i].maxSize == 0 {
			panic(fmt.Sprintf("virtio device queue %d has zero max size", i))
		}
	}

	device.initBARs()
	if device.exposeMSIX {
		device.configureMSIXCapability(msixCapabilityOffset)
	}

	if host != nil {
		endpointHandle, err := host.RegisterEndpoint(busNum, devNum, funcNum, device)
		if err != nil {
			return nil, fmt.Errorf("register pci endpoint: %w", err)
		}
		device.pciHost = host
		device.endpointHandle = endpointHandle
		if err := device.allocateBARs(); err != nil {
			return nil, fmt.Errorf("allocate pci bars: %w", err)
		}
	}

	if device.exposeMSI {
		device.configureMSICapability(msiCapabilityOffset, 0)
	}
	device.configureVirtioCapabilities(virtioPCICapStart)

	// Setup feature bits
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
		// Set VirtIO 1.0 feature by default
		device.defaultDeviceFeatures[0] = 0
		device.defaultDeviceFeatures[1] = uint32(virtioFeatureVersion1 >> 32)
	}

	device.deviceFeatures = make([]uint32, len(device.defaultDeviceFeatures))
	device.guestFeatures = make([]uint32, len(device.defaultDeviceFeatures))

	device.reset()
	return device, nil
}

// ConfigSpace implements pci.Endpoint.
func (d *VirtioPCIDevice) ConfigSpace() pci.ConfigSpace {
	return d
}

// OnBARReprogram implements pci.Endpoint.
func (d *VirtioPCIDevice) OnBARReprogram(index int, value uint32) error {
	if index < 0 || index >= len(d.bars) {
		return fmt.Errorf("BAR index %d out of range", index)
	}

	bar := d.baseBAR(index)
	if bar == nil {
		return fmt.Errorf("BAR %d not configured", index)
	}

	isHigh := d.barIsHigh(index)
	if isHigh {
		if !bar.is64 {
			return nil
		}
		bar.rawHigh = value
	} else {
		attrMask := barAttrMaskMemory
		if bar.isIO {
			attrMask = barAttrMaskIO
		}
		bar.rawLow = (value &^ attrMask) | (bar.attributes & attrMask)
		if !bar.is64 {
			bar.rawHigh = 0
		}
		bar.sizing = false
	}

	if bar.is64 {
		bar.value = (uint64(bar.rawHigh) << 32) | uint64(bar.rawLow&0xffff_fff0)
	} else if bar.isIO {
		bar.value = uint64(bar.rawLow & 0xffff_fffc)
	} else {
		bar.value = uint64(bar.rawLow & 0xffff_fff0)
	}

	d.recomputeRegionAddrs()
	return nil
}

// ReadConfig implements pci.ConfigSpace.
func (d *VirtioPCIDevice) ReadConfig(offset uint16, size uint8) (uint32, error) {
	if size != 1 && size != 2 && size != 4 {
		return 0, fmt.Errorf("unsupported config read size %d", size)
	}
	if size == 4 && offset&0x3 != 0 {
		return 0, fmt.Errorf("unaligned 32-bit config read at %#x", offset)
	}
	base := offset &^ 0x3
	value, err := d.readConfigDWord(base)
	if err != nil {
		return 0, err
	}
	shift := (offset - base) * 8
	value >>= shift
	mask := uint32((uint64(1) << (size * 8)) - 1)
	return value & mask, nil
}

// WriteConfig implements pci.ConfigSpace.
func (d *VirtioPCIDevice) WriteConfig(offset uint16, size uint8, value uint32) error {
	if size != 1 && size != 2 && size != 4 {
		return fmt.Errorf("unsupported config write size %d", size)
	}
	if size == 4 && offset&0x3 != 0 {
		return fmt.Errorf("unaligned 32-bit config write at %#x", offset)
	}
	base := offset &^ 0x3
	if size == 4 && offset == base {
		return d.writeConfigDWord(base, value)
	}

	current, err := d.readConfigDWord(base)
	if err != nil {
		return err
	}
	shift := (offset - base) * 8
	mask := uint32((uint64(1) << (size * 8)) - 1)
	newValue := (current & ^(mask << shift)) | ((value & mask) << shift)
	return d.writeConfigDWord(base, newValue)
}

// ReadMMIO implements hv.MemoryMappedIODevice.
func (d *VirtioPCIDevice) ReadMMIO(addr uint64, data []byte) error {
	return d.mmioAccess(addr, data, false)
}

// WriteMMIO implements hv.MemoryMappedIODevice.
func (d *VirtioPCIDevice) WriteMMIO(addr uint64, data []byte) error {
	return d.mmioAccess(addr, data, true)
}

func (d *VirtioPCIDevice) mmioAccess(addr uint64, data []byte, write bool) error {
	width := uint32(len(data))
	if width == 0 {
		return nil
	}

	switch {
	case regionContains(d.commonCfgAddr, d.commonCfgLength, addr, width):
		offset := uint32(addr - d.commonCfgAddr)
		if write {
			return d.writeCommonBlock(offset, data)
		}
		return d.readCommonBlock(offset, data)
	case regionContains(d.notifyCfgAddr, d.notifyCfgLength, addr, width):
		if width != 2 && width != 4 {
			return fmt.Errorf("virtio-pci: unsupported notify width %d", width)
		}
		offset := uint32(addr - d.notifyCfgAddr)
		if write {
			value := littleEndianValue(data, width)
			if value != 0 && virtioPCIDebug {
				slog.Info("virtio-pci: notify write", "offset", offset, "value", value)
			}
			if err := d.handleNotifyWrite(offset, uint16(value)); err != nil {
				return err
			}
		} else {
			storeLittleEndian(data, width, 0)
		}
	case regionContains(d.isrCfgAddr, d.isrCfgLength, addr, width):
		if width != 1 {
			return fmt.Errorf("virtio-pci: unsupported ISR access width %d", width)
		}
		if write {
			d.interruptStatus &^= data[0]
		} else {
			data[0] = d.handleISRRead()
		}
	case regionContains(d.deviceCfgAddr, d.deviceCfgLength, addr, width):
		if width != 1 && width != 2 && width != 4 {
			return fmt.Errorf("virtio-pci: unsupported device config width %d", width)
		}
		offset := uint32(addr - d.deviceCfgAddr)
		if write {
			value := littleEndianValue(data, width)
			if err := d.writeDeviceConfig(offset, value, width); err != nil {
				return err
			}
		} else {
			value, err := d.readDeviceConfig(offset, width)
			if err != nil {
				return err
			}
			storeLittleEndian(data, width, value)
		}
	case d.supportsMSIX && regionContains(d.msixTableAddr, d.msixTableLength, addr, width):
		if write {
			return d.writeMSIXTable(addr, data)
		}
		return d.readMSIXTable(addr, data)
	case d.supportsMSIX && regionContains(d.msixPBAAddr, d.msixPBALength, addr, width):
		if write {
			// PBA is read-only, ignore writes
			return nil
		}
		return d.readMSIXPBA(addr, data)
	default:
		return fmt.Errorf("virtio-pci: unhandled MMIO access addr=%#x width=%d", addr, width)
	}
	return nil
}

// MMIORegions returns the MMIO regions used by this device.
func (d *VirtioPCIDevice) MMIORegions() []hv.MMIORegion {
	regions := make([]hv.MMIORegion, 0, 4)
	add := func(base uint64, length uint32) {
		if base == 0 || length == 0 {
			return
		}
		regions = append(regions, hv.MMIORegion{
			Address: base,
			Size:    uint64(length),
		})
	}
	add(d.commonCfgAddr, d.commonCfgLength)
	add(d.notifyCfgAddr, d.notifyCfgLength)
	add(d.isrCfgAddr, d.isrCfgLength)
	add(d.deviceCfgAddr, d.deviceCfgLength)
	if d.supportsMSIX {
		add(d.msixTableAddr, d.msixTableLength)
		add(d.msixPBAAddr, d.msixPBALength)
	}
	return regions
}

// Implement device interface methods (shared with mmioDevice)

func (d *VirtioPCIDevice) queue(index int) *queue {
	if index < 0 || index >= len(d.queues) {
		return nil
	}
	return &d.queues[index]
}

func (d *VirtioPCIDevice) readAvailState(q *queue) (uint16, uint16, error) {
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

func (d *VirtioPCIDevice) readAvailEntry(q *queue, ringIndex uint16) (uint16, error) {
	if err := ensureQueueReady(q); err != nil {
		return 0, err
	}
	var buf [2]byte
	offset := q.availAddr + 4 + uint64(ringIndex)*2
	if err := d.readGuestInto(offset, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(buf[:]), nil
}

func (d *VirtioPCIDevice) readDescriptor(q *queue, index uint16) (virtqDescriptor, error) {
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

func (d *VirtioPCIDevice) recordUsedElement(q *queue, head uint16, length uint32) error {
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

func (d *VirtioPCIDevice) raiseInterrupt(bit uint32) {
	d.interruptStatus |= uint8(bit)
	if d.vm == nil {
		return
	}
	if d.msixEnabled() {
		vector := d.msixConfigVector
		if delivered, blocked := d.trySignalMSIX(vector); delivered || blocked {
			return
		}
	}
	if d.msiEnabled() {
		if vm, ok := d.vm.(msiCapableVM); ok {
			if err := vm.SignalMSI(d.msiAddress, uint32(d.msiData), 0); err != nil {
				slog.Error("virtio-pci: signal MSI failed", "err", err)
			}
			return
		}
	}
	// Fall back to legacy INTx (not implemented yet)
}

func (d *VirtioPCIDevice) readGuest(addr uint64, length uint32) ([]byte, error) {
	if length == 0 {
		return nil, nil
	}
	buf := make([]byte, length)
	if err := d.readGuestInto(addr, buf); err != nil {
		return nil, err
	}
	return buf, nil
}

func (d *VirtioPCIDevice) writeGuest(addr uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return d.writeGuestFrom(addr, data)
}

func (d *VirtioPCIDevice) readMMIO(addr uint64, data []byte) error {
	return d.mmioAccess(addr, data, false)
}

func (d *VirtioPCIDevice) writeMMIO(addr uint64, data []byte) error {
	return d.mmioAccess(addr, data, true)
}

func (d *VirtioPCIDevice) eventIdxEnabled() bool {
	return d.guestFeatureEnabled(virtioRingFeatureEventIdxBit)
}

func (d *VirtioPCIDevice) setAvailEvent(q *queue, value uint16) error {
	if err := ensureQueueReady(q); err != nil {
		return err
	}
	if !d.eventIdxEnabled() {
		return nil
	}
	offset := q.usedAddr + 4 + uint64(q.size)*8
	return d.writeGuestUint16(offset, value)
}

func (d *VirtioPCIDevice) readGuestInto(addr uint64, buf []byte) error {
	if len(buf) == 0 {
		return nil
	}
	if d.vm == nil {
		return fmt.Errorf("virtio-pci: virtual machine is nil")
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
		return fmt.Errorf("virtio-pci: short guest memory read (want %d, got %d)", len(buf), n)
	}
	return nil
}

func (d *VirtioPCIDevice) writeGuestFrom(addr uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if d.vm == nil {
		return fmt.Errorf("virtio-pci: virtual machine is nil")
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
		return fmt.Errorf("virtio-pci: short guest memory write (want %d, got %d)", len(data), n)
	}
	return nil
}

func (d *VirtioPCIDevice) readGuestUint16(addr uint64) (uint16, error) {
	var buf [2]byte
	if err := d.readGuestInto(addr, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(buf[:]), nil
}

func (d *VirtioPCIDevice) writeGuestUint16(addr uint64, value uint16) error {
	var buf [2]byte
	binary.LittleEndian.PutUint16(buf[:], value)
	return d.writeGuestFrom(addr, buf[:])
}

func (d *VirtioPCIDevice) writeGuestUint32(addr uint64, value uint32) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], value)
	return d.writeGuestFrom(addr, buf[:])
}

func (d *VirtioPCIDevice) guestFeatureEnabled(bit uint32) bool {
	index := bit / 32
	offset := bit % 32
	if int(index) >= len(d.guestFeatures) {
		return false
	}
	return d.guestFeatures[index]&(1<<offset) != 0
}

// Helper methods for PCI device

func (d *VirtioPCIDevice) initBARs() {
	for i := range d.bars {
		d.bars[i] = pciBAR{
			aliasOf: invalidBARIndex,
		}
	}

	d.setMemoryBAR(0, sizeForLength(d.commonCfgLength))
	d.setMemoryBAR(1, sizeForLength(d.isrCfgLength))
	d.setMemoryBAR(2, sizeForLength(d.notifyCfgLength))
	d.bars[3] = pciBAR{aliasOf: invalidBARIndex}
	d.setMemoryBAR64(4, sizeForLength(d.deviceCfgLength))

	d.recomputeRegionAddrs()
}

func sizeForLength(length uint32) uint64 {
	if length == 0 {
		return 0x1000
	}
	size := uint64(1)
	target := uint64(length)
	for size < target {
		size <<= 1
	}
	if size < 0x1000 {
		return 0x1000
	}
	return size
}

func (d *VirtioPCIDevice) setMemoryBAR(index int, size uint64) {
	if index < 0 || index >= len(d.bars) {
		return
	}
	d.bars[index] = pciBAR{
		size:       size,
		attributes: 0x0,
		isIO:       false,
		is64:       false,
		aliasOf:    invalidBARIndex,
		rawLow:     0x0,
		rawHigh:    0x0,
		value:      0,
	}
}

func alignUp32(value, alignment uint32) uint32 {
	if alignment == 0 {
		return value
	}
	return (value + alignment - 1) &^ (alignment - 1)
}

func (d *VirtioPCIDevice) setMemoryBAR64(index int, size uint64) {
	if index < 0 || index >= len(d.bars) {
		return
	}
	attrs := uint32(0x4)
	d.bars[index] = pciBAR{
		size:       size,
		attributes: attrs,
		isIO:       false,
		is64:       true,
		aliasOf:    invalidBARIndex,
		rawLow:     attrs,
		rawHigh:    0,
		value:      0,
	}
	if index+1 < len(d.bars) {
		d.bars[index+1] = pciBAR{
			aliasOf: index,
		}
	}
}

func (d *VirtioPCIDevice) baseBAR(index int) *pciBAR {
	if index < 0 || index >= len(d.bars) {
		return nil
	}
	alias := d.bars[index].aliasOf
	if alias >= 0 {
		return &d.bars[alias]
	}
	return &d.bars[index]
}

func (d *VirtioPCIDevice) barIsHigh(index int) bool {
	if index < 0 || index >= len(d.bars) {
		return false
	}
	return d.bars[index].aliasOf >= 0
}

func (d *VirtioPCIDevice) barBase(index uint8) uint64 {
	if index >= uint8(len(d.bars)) {
		return 0
	}
	bar := d.baseBAR(int(index))
	if bar == nil {
		return 0
	}
	return bar.value
}

func (d *VirtioPCIDevice) recomputeRegionAddrs() {
	d.commonCfgAddr = d.barBase(d.commonCfgBAR) + uint64(d.commonCfgOffset)
	d.notifyCfgAddr = d.barBase(d.notifyCfgBAR) + uint64(d.notifyCfgOffset)
	d.isrCfgAddr = d.barBase(d.isrCfgBAR) + uint64(d.isrCfgOffset)
	d.deviceCfgAddr = d.barBase(d.deviceCfgBAR) + uint64(d.deviceCfgOffset)
	if d.supportsMSIX {
		d.msixTableAddr = d.barBase(d.msixTableBAR) + uint64(d.msixTableOffset)
		d.msixPBAAddr = d.barBase(d.msixPBABAR) + uint64(d.msixPBAOffset)
	}
}

func (d *VirtioPCIDevice) configureMSICapability(offset uint16, next uint8) {
	if !d.exposeMSI {
		return
	}
	d.msiCapOffset = offset
	d.msiCapNext = next
	d.msiControl = msiControl64BitCap
	d.msiAddress = 0
	d.msiData = 0
	d.registerCapability(offset)
}

func (d *VirtioPCIDevice) configureMSIXCapability(offset uint16) {
	if !d.exposeMSIX {
		return
	}
	vectorCount := d.msixVectorCount()
	if offset == 0 || vectorCount == 0 {
		return
	}

	d.msixCapOffset = offset
	d.msixCapNext = 0
	d.supportsMSIX = true

	d.msixEntries = make([]msixEntry, vectorCount)
	pendingWords := (vectorCount + 63) / 64
	if pendingWords == 0 {
		pendingWords = 1
	}
	d.msixPending = make([]uint64, pendingWords)
	d.msixControl = uint16(vectorCount-1) & msixTableSizeMask

	d.msixTableBAR = 3
	d.msixPBABAR = 3
	d.msixTableOffset = 0
	d.msixTableLength = uint32(vectorCount * msixEntrySize)
	d.msixPBAOffset = alignUp32(d.msixTableLength, 8)
	d.msixPBALength = uint32(len(d.msixPending) * 8)

	totalSize := d.msixPBAOffset + d.msixPBALength
	d.setMemoryBAR(int(d.msixTableBAR), sizeForLength(totalSize))

	for i := range d.msixEntries {
		d.msixEntries[i].masked = true
	}

	d.registerCapability(offset)
}

func (d *VirtioPCIDevice) msixVectorCount() int {
	queueCount := len(d.queues)
	if queueCount == 0 {
		return 0
	}
	return queueCount + 1
}

func (d *VirtioPCIDevice) configureVirtioCapabilities(start uint16) {
	if start == 0 {
		return
	}

	d.registerCapability(start)
	d.commonCfgCapOffset = start
	d.notifyCfgCapOffset = d.commonCfgCapOffset + uint16(virtioPCICapLen)
	d.isrCfgCapOffset = d.notifyCfgCapOffset + uint16(virtioPCINotifyCapLen)
	d.deviceCfgCapOffset = d.isrCfgCapOffset + uint16(virtioPCICapLen)

	d.commonCfgCapData = make([]byte, virtioPCICapLen)
	d.notifyCfgCapData = make([]byte, virtioPCINotifyCapLen)
	d.isrCfgCapData = make([]byte, virtioPCICapLen)
	d.deviceCfgCapData = make([]byte, virtioPCICapLen)

	d.initVirtioCap(d.commonCfgCapData, capPointer(d.notifyCfgCapOffset), VIRTIO_PCI_CAP_COMMON_CFG, d.commonCfgBAR, d.commonCfgOffset, d.commonCfgLength)
	d.initVirtioCap(d.notifyCfgCapData, capPointer(d.isrCfgCapOffset), VIRTIO_PCI_CAP_NOTIFY_CFG, d.notifyCfgBAR, d.notifyCfgOffset, d.notifyCfgLength)
	binary.LittleEndian.PutUint32(d.notifyCfgCapData[16:], d.notifyOffMultiplier)
	d.initVirtioCap(d.isrCfgCapData, capPointer(d.deviceCfgCapOffset), VIRTIO_PCI_CAP_ISR_CFG, d.isrCfgBAR, d.isrCfgOffset, d.isrCfgLength)
	d.initVirtioCap(d.deviceCfgCapData, 0, VIRTIO_PCI_CAP_DEVICE_CFG, d.deviceCfgBAR, d.deviceCfgOffset, d.deviceCfgLength)

	d.updateCapabilityChain()
}

func (d *VirtioPCIDevice) updateCapabilityChain() {
	next := capPointer(d.commonCfgCapOffset)
	if d.msixCapOffset != 0 {
		d.msixCapNext = next
		next = capPointer(d.msixCapOffset)
	}
	if d.msiCapOffset != 0 {
		d.msiCapNext = next
	}
}

func (d *VirtioPCIDevice) initVirtioCap(buf []byte, next uint8, cfgType uint8, bar uint8, offset uint32, length uint32) {
	if len(buf) < virtioPCICapLen {
		return
	}
	buf[0] = virtioVendorCapID
	buf[1] = next
	buf[2] = uint8(len(buf))
	buf[3] = cfgType
	buf[4] = bar
	buf[5] = 0
	buf[6] = 0
	buf[7] = 0
	binary.LittleEndian.PutUint32(buf[8:12], offset)
	binary.LittleEndian.PutUint32(buf[12:16], length)
}

func capPointer(offset uint16) uint8 {
	if offset == 0 || offset > 0xff {
		return 0
	}
	return uint8(offset)
}

func (d *VirtioPCIDevice) registerCapability(offset uint16) {
	ptr := capPointer(offset)
	if ptr == 0 {
		return
	}
	if d.capPointer == 0 || ptr < d.capPointer {
		d.capPointer = ptr
	}
	d.status |= pciStatusCapabilitiesList
}

func (d *VirtioPCIDevice) allocateBARs() error {
	if d.endpointHandle == nil {
		return nil
	}

	indices := []int{
		int(d.commonCfgBAR),
		int(d.isrCfgBAR),
		int(d.notifyCfgBAR),
		int(d.deviceCfgBAR),
	}
	if d.supportsMSIX {
		indices = append(indices, int(d.msixTableBAR))
		if d.msixPBABAR != d.msixTableBAR {
			indices = append(indices, int(d.msixPBABAR))
		}
	}

	seen := make(map[int]struct{}, len(indices))
	for _, idx := range indices {
		if idx < 0 || idx >= len(d.bars) {
			continue
		}
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		if d.bars[idx].aliasOf >= 0 {
			continue
		}
		if err := d.allocateBAR(idx); err != nil {
			return err
		}
	}
	return nil
}

func (d *VirtioPCIDevice) allocateBAR(index int) error {
	if index < 0 || index >= len(d.bars) {
		return fmt.Errorf("BAR index %d out of range", index)
	}
	bar := &d.bars[index]
	if bar.size == 0 {
		return nil
	}
	if bar.size > uint64(^uint32(0)) {
		return fmt.Errorf("BAR %d size %#x exceeds allocator range", index, bar.size)
	}

	size := uint32(bar.size)
	align := size
	if align == 0 || align&(align-1) != 0 {
		align = 0x1000
	}

	base, err := d.endpointHandle.AllocateMemoryBAR(index, size, align)
	if err != nil {
		return err
	}
	if err := d.OnBARReprogram(index, uint32(base)); err != nil {
		return err
	}
	if bar.is64 {
		if err := d.OnBARReprogram(index+1, uint32(base>>32)); err != nil {
			return err
		}
	}
	return nil
}

func (d *VirtioPCIDevice) readConfigDWord(offset uint16) (uint32, error) {
	switch offset {
	case 0x00:
		return uint32(d.vendorID) | (uint32(d.deviceID) << 16), nil
	case 0x04:
		return uint32(d.command) | (uint32(d.status) << 16), nil
	case 0x08:
		return 0x00000001, nil // revision 1, other device class
	case 0x0c:
		return 0x00000000, nil // header type 0
	case 0x3c:
		return uint32(d.interruptLine) | (uint32(d.interruptPin) << 8), nil
	}

	if offset >= type0BAROffset && offset < type0BAROffset+type0BARCount*4 {
		return d.readBAR(uint16(offset))
	}

	if value, ok := d.readMSICap(offset); ok {
		return value, nil
	}
	if value, ok := d.readMSIXCap(offset); ok {
		return value, nil
	}
	if value, ok := d.readVirtioCap(offset); ok {
		return value, nil
	}

	switch offset {
	case 0x2c:
		return uint32(d.subsystemVendorID) | (uint32(d.subsystemDeviceID) << 16), nil
	case 0x30:
		return 0, nil // expansion ROM not implemented
	case 0x34:
		return uint32(d.capPointer), nil
	default:
		return 0, nil
	}
}

func (d *VirtioPCIDevice) writeConfigDWord(offset uint16, value uint32) error {
	switch offset {
	case 0x04:
		d.command = uint16(value & 0xffff)
		statusMask := uint16(value >> 16)
		d.status &^= statusMask
		d.status |= pciStatusCapabilitiesList
		return nil
	case 0x3c:
		d.interruptLine = uint8(value & 0xff)
		return nil
	}

	if offset >= type0BAROffset && offset < type0BAROffset+type0BARCount*4 {
		return d.writeBAR(uint16(offset), value)
	}

	if d.writeMSICap(offset, value) {
		return nil
	}
	if d.writeMSIXCap(offset, value) {
		return nil
	}

	return nil
}

func (d *VirtioPCIDevice) readBAR(offset uint16) (uint32, error) {
	if offset < type0BAROffset {
		return 0, fmt.Errorf("invalid BAR offset %#x", offset)
	}
	index := int((offset - type0BAROffset) / 4)
	if index < 0 || index >= len(d.bars) {
		return 0, fmt.Errorf("BAR index %d out of range", index)
	}

	bar := d.baseBAR(index)
	if bar == nil {
		return 0, nil
	}
	isHigh := d.barIsHigh(index)

	if bar.sizing {
		mask := bar.sizeMask()
		if isHigh {
			return uint32(mask >> 32), nil
		}
		return uint32(mask & 0xffff_ffff), nil
	}

	if isHigh {
		if !bar.is64 {
			return 0, nil
		}
		return bar.rawHigh, nil
	}
	return bar.rawLow, nil
}

func (d *VirtioPCIDevice) writeBAR(offset uint16, value uint32) error {
	if offset < type0BAROffset {
		return fmt.Errorf("invalid BAR offset %#x", offset)
	}
	index := int((offset - type0BAROffset) / 4)
	if index < 0 || index >= len(d.bars) {
		return fmt.Errorf("BAR index %d out of range", index)
	}

	bar := d.baseBAR(index)
	if bar == nil {
		return nil
	}

	isHigh := d.barIsHigh(index)
	if value == 0xffff_ffff {
		if !isHigh {
			bar.sizing = true
		}
		return nil
	}

	if !isHigh {
		bar.sizing = false
	}

	return nil
}

func (d *VirtioPCIDevice) readMSICap(offset uint16) (uint32, bool) {
	if d.msiCapOffset == 0 {
		return 0, false
	}
	base := d.msiCapOffset
	switch offset {
	case base:
		header := uint32(0x05) | (uint32(d.msiCapNext) << 8) | (uint32(d.msiControl) << 16)
		return header, true
	case base + 4:
		return uint32(d.msiAddress & 0xffff_ffff), true
	case base + 8:
		if d.msiControl&msiControl64BitCap != 0 {
			return uint32(d.msiAddress >> 32), true
		}
		return uint32(d.msiData), true
	case base + 12:
		if d.msiControl&msiControl64BitCap == 0 {
			return 0, false
		}
		return uint32(d.msiData), true
	default:
		return 0, false
	}
}

func (d *VirtioPCIDevice) writeMSICap(offset uint16, value uint32) bool {
	if d.msiCapOffset == 0 {
		return false
	}
	base := d.msiCapOffset
	switch offset {
	case base:
		writable := uint16(value >> 16)
		d.msiControl = (writable & ^msiControl64BitCap) | msiControl64BitCap
		return true
	case base + 4:
		d.msiAddress = (d.msiAddress & 0xffff_ffff00000000) | uint64(value)
		return true
	case base + 8:
		if d.msiControl&msiControl64BitCap != 0 {
			d.msiAddress = (d.msiAddress & 0x00000000ffffffff) | (uint64(value) << 32)
		} else {
			d.msiData = uint16(value & 0xffff)
		}
		return true
	case base + 12:
		if d.msiControl&msiControl64BitCap != 0 {
			d.msiData = uint16(value & 0xffff)
			return true
		}
	}
	return false
}

func (d *VirtioPCIDevice) readMSIXCap(offset uint16) (uint32, bool) {
	if d.msixCapOffset == 0 {
		return 0, false
	}
	base := d.msixCapOffset
	switch offset {
	case base:
		header := uint32(pciCapIDMSIX) | (uint32(d.msixCapNext) << 8) | (uint32(d.msixControl) << 16)
		return header, true
	case base + 4:
		value := (uint32(d.msixTableOffset) &^ 0x7) | uint32(d.msixTableBAR&0x7)
		return value, true
	case base + 8:
		value := (uint32(d.msixPBAOffset) &^ 0x7) | uint32(d.msixPBABAR&0x7)
		return value, true
	default:
		return 0, false
	}
}

func (d *VirtioPCIDevice) writeMSIXCap(offset uint16, value uint32) bool {
	if d.msixCapOffset == 0 {
		return false
	}
	if offset != d.msixCapOffset {
		return false
	}
	d.updateMSIXControl(uint16(value >> 16))
	return true
}

func (d *VirtioPCIDevice) updateMSIXControl(value uint16) {
	if !d.supportsMSIX {
		return
	}
	sizeBits := uint16(0)
	if len(d.msixEntries) > 0 {
		sizeBits = uint16(len(d.msixEntries)-1) & msixTableSizeMask
	}
	oldMask := d.msixControl & msixControlFunctionMask
	oldEnable := d.msixControl & msixControlEnableBit
	d.msixControl = sizeBits | (value & (msixControlEnableBit | msixControlFunctionMask))
	if (oldMask != 0 && d.msixControl&msixControlFunctionMask == 0) ||
		(oldEnable == 0 && d.msixControl&msixControlEnableBit != 0) {
		d.flushMSIXPending()
	}
}

func (d *VirtioPCIDevice) readVirtioCap(offset uint16) (uint32, bool) {
	if value, ok := readCapabilityRegion(d.commonCfgCapData, d.commonCfgCapOffset, offset); ok {
		return value, true
	}
	if value, ok := readCapabilityRegion(d.notifyCfgCapData, d.notifyCfgCapOffset, offset); ok {
		return value, true
	}
	if value, ok := readCapabilityRegion(d.isrCfgCapData, d.isrCfgCapOffset, offset); ok {
		return value, true
	}
	if value, ok := readCapabilityRegion(d.deviceCfgCapData, d.deviceCfgCapOffset, offset); ok {
		return value, true
	}
	return 0, false
}

func readCapabilityRegion(data []byte, base uint16, offset uint16) (uint32, bool) {
	if len(data) == 0 || offset < base {
		return 0, false
	}
	rel := offset - base
	if int(rel) >= len(data) {
		return 0, false
	}
	return readCapabilityDWord(data, rel), true
}

func readCapabilityDWord(data []byte, rel uint16) uint32 {
	base := int(rel &^ 0x3)
	var value uint32
	for i := 0; i < 4; i++ {
		idx := base + i
		if idx >= len(data) {
			break
		}
		value |= uint32(data[idx]) << (8 * i)
	}
	return value
}

func (d *VirtioPCIDevice) readCommonBlock(offset uint32, data []byte) error {
	for len(data) > 0 {
		width := commonFieldWidth(offset)
		if width == 0 || len(data) < int(width) {
			return fmt.Errorf("virtio-pci: invalid common read at offset %#x (len=%d)", offset, len(data))
		}
		value, err := d.readCommon(offset, width)
		if err != nil {
			return err
		}
		storeLittleEndian(data[:width], width, value)
		offset += width
		data = data[width:]
	}
	return nil
}

func (d *VirtioPCIDevice) writeCommonBlock(offset uint32, data []byte) error {
	for len(data) > 0 {
		width := commonFieldWidth(offset)
		if width == 0 || len(data) < int(width) {
			return fmt.Errorf("virtio-pci: invalid common write at offset %#x (len=%d)", offset, len(data))
		}
		value := littleEndianValue(data[:width], width)
		if err := d.writeCommon(offset, value, width); err != nil {
			return err
		}
		offset += width
		data = data[width:]
	}
	return nil
}

func commonFieldWidth(offset uint32) uint32 {
	switch offset {
	case VIRTIO_PCI_COMMON_DFSELECT,
		VIRTIO_PCI_COMMON_DF,
		VIRTIO_PCI_COMMON_GFSELECT,
		VIRTIO_PCI_COMMON_GF,
		VIRTIO_PCI_COMMON_Q_DESCLO,
		VIRTIO_PCI_COMMON_Q_DESCHI,
		VIRTIO_PCI_COMMON_Q_AVAILLO,
		VIRTIO_PCI_COMMON_Q_AVAILHI,
		VIRTIO_PCI_COMMON_Q_USEDLO,
		VIRTIO_PCI_COMMON_Q_USEDHI:
		return 4
	case VIRTIO_PCI_COMMON_MSIX,
		VIRTIO_PCI_COMMON_NUMQ,
		VIRTIO_PCI_COMMON_Q_SELECT,
		VIRTIO_PCI_COMMON_Q_SIZE,
		VIRTIO_PCI_COMMON_Q_MSIX,
		VIRTIO_PCI_COMMON_Q_ENABLE,
		VIRTIO_PCI_COMMON_Q_NOFF:
		return 2
	case VIRTIO_PCI_COMMON_STATUS,
		VIRTIO_PCI_COMMON_CFGGENERATION:
		return 1
	}
	return 0
}

func (d *VirtioPCIDevice) readCommon(offset uint32, width uint32) (uint32, error) {
	switch width {
	case 1, 2:
		value, err := d.handleCommonCfgRead(offset)
		if err != nil {
			return 0, err
		}
		mask := uint32((1 << (width * 8)) - 1)
		return value & mask, nil
	case 4:
		if offset&0x3 != 0 {
			aligned := offset &^ 0x3
			value, err := d.handleCommonCfgRead(aligned)
			if err != nil {
				return 0, err
			}
			shift := (offset - aligned) * 8
			return value >> shift, nil
		}
		return d.handleCommonCfgRead(offset)
	default:
		return 0, fmt.Errorf("unsupported common cfg read width %d", width)
	}
}

func (d *VirtioPCIDevice) writeCommon(offset uint32, value uint32, width uint32) error {
	switch width {
	case 1, 2:
		mask := uint32((1 << (width * 8)) - 1)
		return d.handleCommonCfgWrite(offset, value&mask)
	case 4:
		if offset&0x3 != 0 {
			aligned := offset &^ 0x3
			current, err := d.handleCommonCfgRead(aligned)
			if err != nil {
				return err
			}
			shift := (offset - aligned) * 8
			newValue := (current & ^(0xffffffff << shift)) | (value << shift)
			return d.handleCommonCfgWrite(aligned, newValue)
		}
		return d.handleCommonCfgWrite(offset, value)
	default:
		return fmt.Errorf("unsupported common cfg write width %d", width)
	}
}

func (d *VirtioPCIDevice) handleCommonCfgRead(offset uint32) (uint32, error) {
	switch offset {
	case VIRTIO_PCI_COMMON_DFSELECT:
		return d.deviceFeatureSel, nil
	case VIRTIO_PCI_COMMON_DF:
		if d.deviceFeatureSel < uint32(len(d.deviceFeatures)) {
			return d.deviceFeatures[d.deviceFeatureSel], nil
		}
		return 0, nil
	case VIRTIO_PCI_COMMON_GFSELECT:
		return d.guestFeatureSel, nil
	case VIRTIO_PCI_COMMON_GF:
		if d.guestFeatureSel < uint32(len(d.guestFeatures)) {
			return d.guestFeatures[d.guestFeatureSel], nil
		}
		return 0, nil
	case VIRTIO_PCI_COMMON_MSIX:
		if !d.supportsMSIX {
			return VIRTIO_MSI_NO_VECTOR, nil
		}
		return uint32(d.msixConfigVector), nil
	case VIRTIO_PCI_COMMON_NUMQ:
		return uint32(len(d.queues)), nil
	case VIRTIO_PCI_COMMON_STATUS:
		return uint32(d.deviceStatus), nil
	case VIRTIO_PCI_COMMON_CFGGENERATION:
		return uint32(d.cfgGeneration), nil
	case VIRTIO_PCI_COMMON_Q_SELECT:
		return uint32(d.queueSel), nil
	case VIRTIO_PCI_COMMON_Q_SIZE:
		if q := d.currentQueue(); q != nil {
			value := uint16(q.maxSize)
			if q.size != 0 {
				value = q.size
			}
			return uint32(value), nil
		}
		return 0, nil
	case VIRTIO_PCI_COMMON_Q_MSIX:
		if !d.supportsMSIX {
			return VIRTIO_MSI_NO_VECTOR, nil
		}
		if q := d.currentQueue(); q != nil {
			return uint32(q.msixVector), nil
		}
		return VIRTIO_MSI_NO_VECTOR, nil
	case VIRTIO_PCI_COMMON_Q_ENABLE:
		if q := d.currentQueue(); q != nil && q.enable {
			return 1, nil
		}
		return 0, nil
	case VIRTIO_PCI_COMMON_Q_NOFF:
		if q := d.currentQueue(); q != nil {
			return uint32(q.notifyOff), nil
		}
		return 0, nil
	case VIRTIO_PCI_COMMON_Q_DESCLO:
		if q := d.currentQueue(); q != nil {
			return uint32(q.descAddr), nil
		}
		return 0, nil
	case VIRTIO_PCI_COMMON_Q_DESCHI:
		if q := d.currentQueue(); q != nil {
			return uint32(q.descAddr >> 32), nil
		}
		return 0, nil
	case VIRTIO_PCI_COMMON_Q_AVAILLO:
		if q := d.currentQueue(); q != nil {
			return uint32(q.availAddr), nil
		}
		return 0, nil
	case VIRTIO_PCI_COMMON_Q_AVAILHI:
		if q := d.currentQueue(); q != nil {
			return uint32(q.availAddr >> 32), nil
		}
		return 0, nil
	case VIRTIO_PCI_COMMON_Q_USEDLO:
		if q := d.currentQueue(); q != nil {
			return uint32(q.usedAddr), nil
		}
		return 0, nil
	case VIRTIO_PCI_COMMON_Q_USEDHI:
		if q := d.currentQueue(); q != nil {
			return uint32(q.usedAddr >> 32), nil
		}
		return 0, nil
	default:
		return 0, fmt.Errorf("invalid common config offset %#x", offset)
	}
}

func (d *VirtioPCIDevice) handleCommonCfgWrite(offset uint32, value uint32) error {
	switch offset {
	case VIRTIO_PCI_COMMON_DFSELECT:
		d.deviceFeatureSel = value
	case VIRTIO_PCI_COMMON_DF:
		// read-only
	case VIRTIO_PCI_COMMON_GFSELECT:
		d.guestFeatureSel = value
	case VIRTIO_PCI_COMMON_GF:
		if d.guestFeatureSel < uint32(len(d.guestFeatures)) {
			oldValue := d.guestFeatures[d.guestFeatureSel]
			d.guestFeatures[d.guestFeatureSel] = value
			if oldValue != value {
				d.cfgGeneration++
			}
		}
	case VIRTIO_PCI_COMMON_MSIX:
		if d.supportsMSIX {
			d.msixConfigVector = uint16(value)
		}
	case VIRTIO_PCI_COMMON_NUMQ:
		// read-only
	case VIRTIO_PCI_COMMON_STATUS:
		if value == 0 {
			d.reset()
			return nil
		}
		d.deviceStatus = uint8(value)
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
				negotiatedFeatures := uint64(0)
				for i := range d.guestFeatures {
					negotiatedFeatures |= uint64(d.guestFeatures[i]) << (32 * uint(i))
				}
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
	case VIRTIO_PCI_COMMON_CFGGENERATION:
		// read-only
	case VIRTIO_PCI_COMMON_Q_SELECT:
		d.queueSel = uint16(value)
	case VIRTIO_PCI_COMMON_Q_SIZE:
		if q := d.currentQueue(); q != nil {
			if value == 0 {
				q.size = 0
				return nil
			}
			if value > uint32(q.maxSize) {
				return fmt.Errorf("invalid queue size %d", value)
			}
			q.size = uint16(value)
		}
	case VIRTIO_PCI_COMMON_Q_MSIX:
		if d.supportsMSIX {
			if q := d.currentQueue(); q != nil {
				q.msixVector = uint16(value)
			}
		}
	case VIRTIO_PCI_COMMON_Q_ENABLE:
		if q := d.currentQueue(); q != nil {
			if value&0x1 == 0 {
				q.ready = false
				q.enable = false
			} else {
				if q.size == 0 {
					return fmt.Errorf("queue enable set before queue size")
				}
				q.ready = true
				q.enable = true
			}
		}
	case VIRTIO_PCI_COMMON_Q_DESCLO:
		if q := d.currentQueue(); q != nil {
			q.descAddr = (q.descAddr &^ 0xffffffff) | uint64(value)
		}
	case VIRTIO_PCI_COMMON_Q_DESCHI:
		if q := d.currentQueue(); q != nil {
			q.descAddr = (q.descAddr &^ (uint64(0xffffffff) << 32)) | (uint64(value) << 32)
		}
	case VIRTIO_PCI_COMMON_Q_AVAILLO:
		if q := d.currentQueue(); q != nil {
			q.availAddr = (q.availAddr &^ 0xffffffff) | uint64(value)
		}
	case VIRTIO_PCI_COMMON_Q_AVAILHI:
		if q := d.currentQueue(); q != nil {
			q.availAddr = (q.availAddr &^ (uint64(0xffffffff) << 32)) | (uint64(value) << 32)
		}
	case VIRTIO_PCI_COMMON_Q_USEDLO:
		if q := d.currentQueue(); q != nil {
			q.usedAddr = (q.usedAddr &^ 0xffffffff) | uint64(value)
		}
	case VIRTIO_PCI_COMMON_Q_USEDHI:
		if q := d.currentQueue(); q != nil {
			q.usedAddr = (q.usedAddr &^ (uint64(0xffffffff) << 32)) | (uint64(value) << 32)
		}
	case VIRTIO_PCI_COMMON_Q_NOFF:
		// read-only
	default:
		return fmt.Errorf("invalid common config offset %#x", offset)
	}
	return nil
}

func (d *VirtioPCIDevice) currentQueue() *queue {
	idx := int(d.queueSel)
	if idx < 0 || idx >= len(d.queues) {
		return nil
	}
	return &d.queues[idx]
}

func (d *VirtioPCIDevice) handleNotifyWrite(offset uint32, value uint16) error {
	queueIdx := int(value)
	if queueIdx < 0 || queueIdx >= len(d.queues) {
		queueIdx = int(offset / d.notifyOffMultiplier)
	}
	if d.handler != nil {
		return d.handler.OnQueueNotify(d, queueIdx)
	}
	return nil
}

func (d *VirtioPCIDevice) handleISRRead() uint8 {
	value := d.interruptStatus
	d.interruptStatus = 0
	return value
}

func (d *VirtioPCIDevice) readDeviceConfig(offset uint32, width uint32) (uint32, error) {
	value, err := d.handleDeviceCfgRead(offset &^ 0x3)
	if err != nil {
		return 0, err
	}
	shift := (offset & 0x3) * 8
	mask := uint32((uint64(1) << (width * 8)) - 1)
	return (value >> shift) & mask, nil
}

func (d *VirtioPCIDevice) writeDeviceConfig(offset uint32, value uint32, width uint32) error {
	aligned := offset &^ 0x3
	if width == 4 && offset == aligned {
		return d.handleDeviceCfgWrite(aligned, value)
	}
	current, err := d.handleDeviceCfgRead(aligned)
	if err != nil {
		return err
	}
	shift := (offset - aligned) * 8
	mask := uint32((uint64(1) << (width * 8)) - 1)
	newValue := (current & ^(mask << shift)) | ((value & mask) << shift)
	return d.handleDeviceCfgWrite(aligned, newValue)
}

func (d *VirtioPCIDevice) handleDeviceCfgRead(offset uint32) (uint32, error) {
	if d.virtioDevice != nil {
		relOffset := uint16(offset)
		return d.virtioDevice.ReadConfig(relOffset), nil
	} else if d.handler != nil {
		value, handled, err := d.handler.ReadConfig(d, uint64(offset))
		if handled {
			return value, err
		}
	}
	return 0, nil
}

func (d *VirtioPCIDevice) handleDeviceCfgWrite(offset uint32, value uint32) error {
	if d.virtioDevice != nil {
		relOffset := uint16(offset)
		d.virtioDevice.WriteConfig(relOffset, value)
		d.cfgGeneration++
		return nil
	} else if d.handler != nil {
		handled, err := d.handler.WriteConfig(d, uint64(offset), value)
		if handled {
			d.cfgGeneration++
			return err
		}
	}
	return nil
}

func (d *VirtioPCIDevice) readMSIXTable(addr uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if len(d.msixEntries) == 0 {
		return fmt.Errorf("virtio-pci: MSI-X table not configured")
	}
	base := d.msixTableAddr
	for i := range data {
		byteOffset := uint64(addr-base) + uint64(i)
		entryIdx := int(byteOffset / msixEntrySize)
		if entryIdx < 0 || entryIdx >= len(d.msixEntries) {
			return fmt.Errorf("virtio-pci: MSI-X table read out of range")
		}
		entryOffset := int(byteOffset % msixEntrySize)
		data[i] = d.msixEntryByte(entryIdx, entryOffset)
	}
	return nil
}

func (d *VirtioPCIDevice) writeMSIXTable(addr uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if len(d.msixEntries) == 0 {
		return fmt.Errorf("virtio-pci: MSI-X table not configured")
	}
	base := d.msixTableAddr
	for i := range data {
		byteOffset := uint64(addr-base) + uint64(i)
		entryIdx := int(byteOffset / msixEntrySize)
		if entryIdx < 0 || entryIdx >= len(d.msixEntries) {
			return fmt.Errorf("virtio-pci: MSI-X table write out of range")
		}
		entryOffset := int(byteOffset % msixEntrySize)
		d.writeMSIXEntryByte(entryIdx, entryOffset, data[i])
	}
	return nil
}

func (d *VirtioPCIDevice) msixEntryByte(entryIdx, entryOffset int) byte {
	entry := d.msixEntries[entryIdx]
	switch {
	case entryOffset < 8:
		shift := uint(entryOffset * 8)
		return byte(entry.addr >> shift)
	case entryOffset < 12:
		shift := uint((entryOffset - 8) * 8)
		return byte(entry.data >> shift)
	case entryOffset == 12:
		if entry.masked {
			return 1
		}
		return 0
	default:
		return 0
	}
}

func (d *VirtioPCIDevice) writeMSIXEntryByte(entryIdx, entryOffset int, value byte) {
	entry := &d.msixEntries[entryIdx]
	switch {
	case entryOffset < 8:
		shift := uint(entryOffset * 8)
		mask := uint64(0xff) << shift
		entry.addr = (entry.addr & ^mask) | (uint64(value) << shift)
	case entryOffset < 12:
		shift := uint((entryOffset - 8) * 8)
		mask := uint32(0xff) << shift
		entry.data = (entry.data & ^mask) | (uint32(value) << shift)
	case entryOffset == 12:
		prevMasked := entry.masked
		entry.masked = value&0x1 != 0
		if prevMasked && !entry.masked {
			d.emitPendingVector(uint16(entryIdx))
		}
	default:
		// remaining bytes are reserved
	}
}

func (d *VirtioPCIDevice) readMSIXPBA(addr uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if len(d.msixPending) == 0 {
		return fmt.Errorf("virtio-pci: MSI-X PBA not configured")
	}
	base := d.msixPBAAddr
	for i := range data {
		byteOffset := uint64(addr-base) + uint64(i)
		wordIdx := int(byteOffset / 8)
		if wordIdx < 0 || wordIdx >= len(d.msixPending) {
			return fmt.Errorf("virtio-pci: MSI-X PBA read out of range")
		}
		shift := uint((byteOffset % 8) * 8)
		data[i] = byte(d.msixPending[wordIdx] >> shift)
	}
	return nil
}

func (d *VirtioPCIDevice) msixEnabled() bool {
	return d.supportsMSIX && (d.msixControl&msixControlEnableBit) != 0
}

func (d *VirtioPCIDevice) msiEnabled() bool {
	if d.msixEnabled() {
		return false
	}
	return d.msiControl&0x1 != 0 && d.msiAddress != 0
}

func (d *VirtioPCIDevice) trySignalMSIX(vector uint16) (bool, bool) {
	if !d.msixEnabled() {
		return false, false
	}
	if vector == VIRTIO_MSI_NO_VECTOR {
		return false, false
	}
	if int(vector) >= len(d.msixEntries) {
		return false, false
	}
	if d.msixControl&msixControlFunctionMask != 0 || d.msixEntries[vector].masked {
		d.setMSIXPendingBit(vector)
		return false, true
	}
	entry := d.msixEntries[vector]
	if entry.addr == 0 {
		return false, false
	}
	vm, ok := d.vm.(msiCapableVM)
	if !ok {
		return false, false
	}
	if err := vm.SignalMSI(entry.addr, entry.data, 0); err != nil {
		slog.Error("virtio-pci: signal MSI-X failed", "vector", vector, "err", err)
		return false, false
	}
	d.clearMSIXPendingBit(vector)
	return true, false
}

func (d *VirtioPCIDevice) setMSIXPendingBit(vector uint16) {
	if int(vector) >= len(d.msixEntries) {
		return
	}
	idx := int(vector) / 64
	if idx < 0 || idx >= len(d.msixPending) {
		return
	}
	bit := uint(vector % 64)
	d.msixPending[idx] |= uint64(1) << bit
}

func (d *VirtioPCIDevice) clearMSIXPendingBit(vector uint16) {
	if int(vector) >= len(d.msixEntries) {
		return
	}
	idx := int(vector) / 64
	if idx < 0 || idx >= len(d.msixPending) {
		return
	}
	bit := uint(vector % 64)
	d.msixPending[idx] &^= uint64(1) << bit
}

func (d *VirtioPCIDevice) emitPendingVector(vector uint16) {
	idx := int(vector) / 64
	if idx < 0 || idx >= len(d.msixPending) {
		return
	}
	if d.msixPending[idx]&(uint64(1)<<uint(vector%64)) == 0 {
		return
	}
	_, _ = d.trySignalMSIX(vector)
}

func (d *VirtioPCIDevice) flushMSIXPending() {
	for idx := range d.msixPending {
		bits := d.msixPending[idx]
		if bits == 0 {
			continue
		}
		for bit := 0; bit < 64; bit++ {
			mask := uint64(1) << uint(bit)
			if bits&mask == 0 {
				continue
			}
			vector := uint16(idx*64 + bit)
			if int(vector) >= len(d.msixEntries) {
				continue
			}
			if d.msixEntries[vector].masked {
				continue
			}
			d.emitPendingVector(vector)
		}
	}
}

func (d *VirtioPCIDevice) reset() {
	d.deviceFeatureSel = 0
	d.guestFeatureSel = 0
	copy(d.deviceFeatures, d.defaultDeviceFeatures)
	for i := range d.guestFeatures {
		d.guestFeatures[i] = 0
	}
	d.queueSel = 0
	d.deviceStatus = 0
	d.cfgGeneration = 0
	d.interruptStatus = 0
	d.msixConfigVector = VIRTIO_MSI_NO_VECTOR
	d.msiControl = msiControl64BitCap
	d.msiAddress = 0
	d.msiData = 0
	if d.supportsMSIX {
		if len(d.msixEntries) > 0 {
			d.msixControl = uint16(len(d.msixEntries)-1) & msixTableSizeMask
		} else {
			d.msixControl = 0
		}
		for i := range d.msixEntries {
			d.msixEntries[i].addr = 0
			d.msixEntries[i].data = 0
			d.msixEntries[i].masked = true
		}
		for i := range d.msixPending {
			d.msixPending[i] = 0
		}
	}

	for i := range d.queues {
		d.queues[i].reset()
		d.queues[i].maxSize = d.handler.QueueMaxSize(i)
		d.queues[i].notifyOff = uint16(i)
		d.queues[i].msixVector = VIRTIO_MSI_NO_VECTOR
	}

	if d.virtioDevice != nil {
		d.virtioDevice.Disable()
	} else if d.handler != nil {
		d.handler.OnReset(d)
	}
}
