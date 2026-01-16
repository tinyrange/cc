package virtio

import (
	"fmt"

	"github.com/tinyrange/cc/internal/fdt"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/timeslice"
)

// MMIODeviceConfig holds the configuration for an MMIO virtio device.
// Device-specific constants are provided here to avoid interface pollution.
type MMIODeviceConfig struct {
	// MMIO region configuration
	DefaultMMIOBase uint64
	DefaultMMIOSize uint64

	// IRQ configuration
	DefaultIRQLine    uint32
	ArmDefaultIRQLine uint32

	// Virtio device identification
	DeviceID uint32
	VendorID uint32
	Version  uint32

	// Queue configuration
	QueueCount   int
	QueueMaxSize uint16

	// Feature bits
	FeatureBits []uint64

	// Device name for error messages
	DeviceName string

	// Timeslice IDs (optional, can be 0)
	TimesliceRead  timeslice.TimesliceID
	TimesliceWrite timeslice.TimesliceID
}

// MMIODeviceTemplateBase provides shared implementation for VirtioMMIODevice.
// Device templates should embed this type.
type MMIODeviceTemplateBase struct {
	Arch    hv.CpuArchitecture
	IRQLine uint32
	Config  *MMIODeviceConfig
}

// ArchOrDefault returns the architecture, defaulting to VM's architecture.
func (b MMIODeviceTemplateBase) ArchOrDefault(vm hv.VirtualMachine) hv.CpuArchitecture {
	if b.Arch != "" && b.Arch != hv.ArchitectureInvalid {
		return b.Arch
	}
	if vm != nil && vm.Hypervisor() != nil {
		return vm.Hypervisor().Architecture()
	}
	return hv.ArchitectureInvalid
}

// IRQLineForArch returns the IRQ line for the given architecture.
func (b MMIODeviceTemplateBase) IRQLineForArch(arch hv.CpuArchitecture) uint32 {
	if b.IRQLine != 0 {
		return b.IRQLine
	}
	if arch == hv.ArchitectureARM64 {
		return b.Config.ArmDefaultIRQLine
	}
	return b.Config.DefaultIRQLine
}

// GetLinuxCommandLineParam implements VirtioMMIODevice.
func (b MMIODeviceTemplateBase) GetLinuxCommandLineParam() ([]string, error) {
	irqLine := b.IRQLineForArch(b.Arch)
	param := fmt.Sprintf(
		"virtio_mmio.device=4k@0x%x:%d",
		b.Config.DefaultMMIOBase,
		irqLine,
	)
	return []string{param}, nil
}

// DeviceTreeNodes implements VirtioMMIODevice.
func (b MMIODeviceTemplateBase) DeviceTreeNodes() ([]fdt.Node, error) {
	irqLine := b.IRQLineForArch(b.Arch)
	node := fdt.Node{
		Name: fmt.Sprintf("virtio@%x", b.Config.DefaultMMIOBase),
		Properties: map[string]fdt.Property{
			"compatible": {Strings: []string{"virtio,mmio"}},
			"reg":        {U64: []uint64{b.Config.DefaultMMIOBase, b.Config.DefaultMMIOSize}},
			"interrupts": {U32: []uint32{0, irqLine, 4}},
			"status":     {Strings: []string{"okay"}},
		},
	}
	return []fdt.Node{node}, nil
}

// GetACPIDeviceInfo implements VirtioMMIODevice.
func (b MMIODeviceTemplateBase) GetACPIDeviceInfo() ACPIDeviceInfo {
	irqLine := b.IRQLineForArch(b.ArchOrDefault(nil))
	return ACPIDeviceInfo{
		BaseAddr: b.Config.DefaultMMIOBase,
		Size:     b.Config.DefaultMMIOSize,
		GSI:      irqLine,
	}
}

// MMIODeviceBase provides shared implementation for MMIO virtio devices.
// Device structs should embed this type.
type MMIODeviceBase struct {
	dev     device
	base    uint64
	size    uint64
	irqLine uint32
	arch    hv.CpuArchitecture
	config  *MMIODeviceConfig
}

// InitBase initializes the device base. Call this from the embedding device's Init().
// handler is the device-specific handler implementing deviceHandler.
func (b *MMIODeviceBase) InitBase(vm hv.VirtualMachine, handler deviceHandler) error {
	if b.dev == nil {
		if vm == nil {
			return fmt.Errorf("%s: virtual machine is nil", b.config.DeviceName)
		}
		b.setupDevice(vm, handler)
		return nil
	}
	if mmio, ok := b.dev.(*mmioDevice); ok && vm != nil {
		mmio.vm = vm
	}
	return nil
}

func (b *MMIODeviceBase) setupDevice(vm hv.VirtualMachine, handler deviceHandler) {
	if vm != nil && vm.Hypervisor() != nil {
		b.arch = vm.Hypervisor().Architecture()
	}
	b.dev = newMMIODevice(
		vm, b.base, b.size, b.irqLine,
		b.config.DeviceID, b.config.VendorID, b.config.Version,
		b.config.FeatureBits, handler,
	)
	if mmio, ok := b.dev.(*mmioDevice); ok && vm != nil {
		mmio.vm = vm
	}
}

// MMIORegions implements hv.MemoryMappedIODevice.
func (b *MMIODeviceBase) MMIORegions() []hv.MMIORegion {
	if b.size == 0 {
		return nil
	}
	return []hv.MMIORegion{{
		Address: b.base,
		Size:    b.size,
	}}
}

// ReadMMIO implements hv.MemoryMappedIODevice.
func (b *MMIODeviceBase) ReadMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	if b.config.TimesliceRead != 0 {
		ctx.SetExitTimeslice(b.config.TimesliceRead)
	}
	dev, err := b.RequireDevice()
	if err != nil {
		return err
	}
	return dev.readMMIO(ctx, addr, data)
}

// WriteMMIO implements hv.MemoryMappedIODevice.
func (b *MMIODeviceBase) WriteMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	if b.config.TimesliceWrite != 0 {
		ctx.SetExitTimeslice(b.config.TimesliceWrite)
	}
	dev, err := b.RequireDevice()
	if err != nil {
		return err
	}
	return dev.writeMMIO(ctx, addr, data)
}

// RequireDevice returns the underlying device or an error if not initialized.
func (b *MMIODeviceBase) RequireDevice() (device, error) {
	if b.dev == nil {
		return nil, fmt.Errorf("%s: device not initialized", b.config.DeviceName)
	}
	return b.dev, nil
}

// Device returns the underlying device transport.
func (b *MMIODeviceBase) Device() device {
	return b.dev
}

// NumQueues implements deviceHandler (returns config value).
func (b *MMIODeviceBase) NumQueues() int {
	return b.config.QueueCount
}

// QueueMaxSize implements deviceHandler (returns config value).
func (b *MMIODeviceBase) QueueMaxSize(queue int) uint16 {
	return b.config.QueueMaxSize
}

// Arch returns the CPU architecture.
func (b *MMIODeviceBase) Arch() hv.CpuArchitecture {
	return b.arch
}

// Base returns the MMIO base address.
func (b *MMIODeviceBase) Base() uint64 {
	return b.base
}

// Size returns the MMIO region size.
func (b *MMIODeviceBase) Size() uint64 {
	return b.size
}

// IRQLine returns the IRQ line.
func (b *MMIODeviceBase) IRQLine() uint32 {
	return b.irqLine
}

// AllocatedMMIOBase implements AllocatedVirtioMMIODevice.
func (b *MMIODeviceBase) AllocatedMMIOBase() uint64 {
	return b.base
}

// AllocatedMMIOSize implements AllocatedVirtioMMIODevice.
func (b *MMIODeviceBase) AllocatedMMIOSize() uint64 {
	return b.size
}

// AllocatedIRQLine implements AllocatedVirtioMMIODevice.
func (b *MMIODeviceBase) AllocatedIRQLine() uint32 {
	return b.irqLine
}

// NewMMIODeviceBase creates a new MMIODeviceBase with the given configuration.
func NewMMIODeviceBase(base, size uint64, irqLine uint32, config *MMIODeviceConfig) MMIODeviceBase {
	return MMIODeviceBase{
		base:    base,
		size:    size,
		irqLine: irqLine,
		config:  config,
	}
}

// RestoreBase restores the base fields from a snapshot. Used by device restore.
func (b *MMIODeviceBase) RestoreBase(arch hv.CpuArchitecture, base, size uint64, irqLine uint32) {
	b.arch = arch
	b.base = base
	b.size = size
	b.irqLine = irqLine
}

// SyncToTransport updates the underlying MMIO transport with current base values.
// Call this after RestoreBase if needed.
func (b *MMIODeviceBase) SyncToTransport() {
	if mmio, ok := b.dev.(*mmioDevice); ok {
		mmio.base = b.base
		mmio.size = b.size
	}
}

// Stoppable is implemented by devices that have background resources to clean up.
type Stoppable interface {
	Stop() error
}
