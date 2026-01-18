package virtio

import (
	"github.com/tinyrange/cc/internal/hv"
)

// VirtioMMIOBus manages a contiguous region of virtio MMIO slots.
// Empty slots return magic=0 to indicate no device present.
// This allows guest OSes to scan for virtio devices without causing
// MMIO faults on empty slots.
type VirtioMMIOBus struct {
	vm        hv.VirtualMachine
	baseAddr  uint64
	slotSize  uint64
	slotCount int
	devices   []hv.MemoryMappedIODevice // slot index -> device (nil = empty)
}

// NewVirtioMMIOBus creates a new VirtioMMIOBus with the given parameters.
// baseAddr is the starting address (e.g., 0x0a000000)
// slotSize is the size of each slot (typically 0x200 for virtio-mmio)
// slotCount is the number of slots to manage
func NewVirtioMMIOBus(baseAddr, slotSize uint64, slotCount int) *VirtioMMIOBus {
	return &VirtioMMIOBus{
		baseAddr:  baseAddr,
		slotSize:  slotSize,
		slotCount: slotCount,
		devices:   make([]hv.MemoryMappedIODevice, slotCount),
	}
}

// AttachDevice attaches a virtio device to a specific slot.
// The device's MMIO base address should match the slot's address.
func (b *VirtioMMIOBus) AttachDevice(slot int, dev hv.MemoryMappedIODevice) {
	if slot >= 0 && slot < b.slotCount {
		b.devices[slot] = dev
	}
}

// SlotAddress returns the MMIO base address for a given slot.
func (b *VirtioMMIOBus) SlotAddress(slot int) uint64 {
	return b.baseAddr + uint64(slot)*b.slotSize
}

// Init implements hv.Device.
func (b *VirtioMMIOBus) Init(vm hv.VirtualMachine) error {
	b.vm = vm
	// Initialize any attached devices
	for _, dev := range b.devices {
		if dev != nil {
			if err := dev.Init(vm); err != nil {
				return err
			}
		}
	}
	return nil
}

// MMIORegions implements hv.MemoryMappedIODevice.
// Returns a single region covering all slots.
func (b *VirtioMMIOBus) MMIORegions() []hv.MMIORegion {
	return []hv.MMIORegion{{
		Address: b.baseAddr,
		Size:    b.slotSize * uint64(b.slotCount),
	}}
}

// ReadMMIO implements hv.MemoryMappedIODevice.
// Dispatches to the appropriate device or returns 0 for empty slots.
func (b *VirtioMMIOBus) ReadMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	slot := int((addr - b.baseAddr) / b.slotSize)
	offset := (addr - b.baseAddr) % b.slotSize

	// Bounds check
	if slot < 0 || slot >= b.slotCount {
		// Out of bounds - return 0
		for i := range data {
			data[i] = 0
		}
		return nil
	}

	dev := b.devices[slot]
	if dev == nil {
		// Empty slot - return 0 for all reads
		// This tells the guest there's no device here (magic = 0)
		for i := range data {
			data[i] = 0
		}
		return nil
	}

	// Dispatch to the device using the device's base address + offset
	slotBase := b.SlotAddress(slot)
	return dev.ReadMMIO(ctx, slotBase+offset, data)
}

// WriteMMIO implements hv.MemoryMappedIODevice.
// Dispatches to the appropriate device or ignores writes to empty slots.
func (b *VirtioMMIOBus) WriteMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	slot := int((addr - b.baseAddr) / b.slotSize)
	offset := (addr - b.baseAddr) % b.slotSize

	// Bounds check
	if slot < 0 || slot >= b.slotCount {
		// Out of bounds - ignore
		return nil
	}

	dev := b.devices[slot]
	if dev == nil {
		// Empty slot - ignore writes
		return nil
	}

	// Dispatch to the device using the device's base address + offset
	slotBase := b.SlotAddress(slot)
	return dev.WriteMMIO(ctx, slotBase+offset, data)
}

var _ hv.MemoryMappedIODevice = (*VirtioMMIOBus)(nil)

// NewInputForBusSlot creates a virtio-input device configured for a specific bus slot.
// The device will use the slot's base address and the provided IRQ line.
func NewInputForBusSlot(vm hv.VirtualMachine, slotBase uint64, irqLine uint32, inputType InputType, name string) (*Input, error) {
	arch := hv.ArchitectureARM64
	if vm != nil && vm.Hypervisor() != nil {
		arch = vm.Hypervisor().Architecture()
	}

	encodedLine := EncodeIRQLineForArch(arch, irqLine)

	if name == "" {
		if inputType == InputTypeKeyboard {
			name = "Virtio Keyboard"
		} else {
			name = "Virtio Tablet"
		}
	}

	input := &Input{
		base:      slotBase,
		size:      InputDefaultMMIOSize,
		irqLine:   encodedLine,
		inputType: inputType,
		name:      name,
	}

	// Initialize the device
	input.setupDevice(vm)

	return input, nil
}

// NewBlkForBusSlot creates a virtio-blk device configured for a specific bus slot.
// The device will use the slot's base address and the provided IRQ line.
func NewBlkForBusSlot(vm hv.VirtualMachine, slotBase uint64, irqLine uint32, template BlkTemplate) (*Blk, error) {
	arch := hv.ArchitectureARM64
	if vm != nil && vm.Hypervisor() != nil {
		arch = vm.Hypervisor().Architecture()
	}

	encodedLine := EncodeIRQLineForArch(arch, irqLine)
	config := blkDeviceConfig

	blk := &Blk{
		MMIODeviceBase: NewMMIODeviceBase(
			slotBase,
			config.DefaultMMIOSize,
			encodedLine,
			config,
		),
		file:     template.File,
		readonly: template.ReadOnly,
	}

	// Get file size to determine capacity
	if blk.file != nil {
		fi, err := blk.file.Stat()
		if err != nil {
			return nil, err
		}
		blk.capacity = uint64(fi.Size()) / 512
	}

	// Initialize the base but don't register MMIO (bus handles that)
	if err := blk.InitBase(vm, blk); err != nil {
		return nil, err
	}

	return blk, nil
}

// VirtioMMIOBusConstants holds standard virtio MMIO bus configuration.
const (
	// VirtioMMIOBusBase is the standard base address for virtio MMIO devices.
	VirtioMMIOBusBase = 0x0a000000

	// VirtioMMIOSlotSize is the standard size of each virtio MMIO slot.
	VirtioMMIOSlotSize = 0x200

	// VirtioMMIOSlotCount is the standard number of virtio MMIO slots.
	VirtioMMIOSlotCount = 32

	// VirtioMMIOBusIRQBase is the base IRQ for virtio MMIO devices (SPI 48).
	VirtioMMIOBusIRQBase = 48
)

// EmptySlotDevice is a minimal device that returns magic=0 for empty slots.
// This is used internally by VirtioMMIOBus but can also be used standalone.
type EmptySlotDevice struct {
	base uint64
	size uint64
}

// NewEmptySlotDevice creates an empty slot device at the given address.
func NewEmptySlotDevice(base, size uint64) *EmptySlotDevice {
	return &EmptySlotDevice{base: base, size: size}
}

// Init implements hv.Device.
func (d *EmptySlotDevice) Init(vm hv.VirtualMachine) error {
	return nil
}

// MMIORegions implements hv.MemoryMappedIODevice.
func (d *EmptySlotDevice) MMIORegions() []hv.MMIORegion {
	return []hv.MMIORegion{{Address: d.base, Size: d.size}}
}

// ReadMMIO implements hv.MemoryMappedIODevice.
func (d *EmptySlotDevice) ReadMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	// Return 0 for all reads - magic=0 means no device
	for i := range data {
		data[i] = 0
	}
	return nil
}

// WriteMMIO implements hv.MemoryMappedIODevice.
func (d *EmptySlotDevice) WriteMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	// Ignore all writes to empty slots
	return nil
}

var _ hv.MemoryMappedIODevice = (*EmptySlotDevice)(nil)
