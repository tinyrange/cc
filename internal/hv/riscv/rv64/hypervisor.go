package rv64

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/tinyrange/cc/internal/hv"
)

// Hypervisor implements hv.Hypervisor for RV64GC
type Hypervisor struct{}

// Open creates a new RV64GC hypervisor
func Open() (hv.Hypervisor, error) {
	return &Hypervisor{}, nil
}

// Close implements hv.Hypervisor
func (h *Hypervisor) Close() error {
	return nil
}

// Architecture implements hv.Hypervisor
func (h *Hypervisor) Architecture() hv.CpuArchitecture {
	return hv.ArchitectureRISCV64
}

// NewVirtualMachine implements hv.Hypervisor
func (h *Hypervisor) NewVirtualMachine(config hv.VMConfig) (hv.VirtualMachine, error) {
	if config == nil {
		return nil, fmt.Errorf("rv64: VMConfig is nil")
	}
	if config.CPUCount() != 1 {
		return nil, fmt.Errorf("rv64: only single CPU guests are supported")
	}

	memSize := config.MemorySize()
	if memSize == 0 {
		memSize = 64 * 1024 * 1024 // 64 MB default
	}

	// Create the machine
	machine := NewMachine(memSize, nil, nil)

	vm := &VirtualMachine{
		hv:      h,
		machine: machine,
	}
	vm.vcpu = &VirtualCPU{vm: vm, id: 0}

	// Verify memory base
	if memBase := config.MemoryBase(); memBase != 0 && memBase != machine.MemoryBase() {
		return nil, fmt.Errorf("rv64: memory base must be 0x%x (got 0x%x)", machine.MemoryBase(), memBase)
	}

	// Call OnCreateVM callback
	if cb := config.Callbacks(); cb != nil {
		if err := cb.OnCreateVM(vm); err != nil {
			return nil, fmt.Errorf("rv64: VM callback OnCreateVM: %w", err)
		}
	}

	// Load the VM
	if loader := config.Loader(); loader != nil {
		if err := loader.Load(vm); err != nil {
			return nil, fmt.Errorf("rv64: load VM: %w", err)
		}
	}

	// Call post-load callbacks
	if cb := config.Callbacks(); cb != nil {
		if err := cb.OnCreateVMWithMemory(vm); err != nil {
			return nil, fmt.Errorf("rv64: VM callback OnCreateVMWithMemory: %w", err)
		}
		if err := cb.OnCreateVCPU(vm.vcpu); err != nil {
			return nil, fmt.Errorf("rv64: VM callback OnCreateVCPU: %w", err)
		}
	}

	return vm, nil
}

// VirtualMachine implements hv.VirtualMachine for RV64GC
type VirtualMachine struct {
	hv      *Hypervisor
	machine *Machine
	vcpu    *VirtualCPU
}

// Hypervisor implements hv.VirtualMachine
func (vm *VirtualMachine) Hypervisor() hv.Hypervisor {
	return vm.hv
}

// MemorySize implements hv.VirtualMachine
func (vm *VirtualMachine) MemorySize() uint64 {
	return vm.machine.MemorySize()
}

// MemoryBase implements hv.VirtualMachine
func (vm *VirtualMachine) MemoryBase() uint64 {
	return vm.machine.MemoryBase()
}

// Close implements hv.VirtualMachine
func (vm *VirtualMachine) Close() error {
	return nil
}

// Run implements hv.VirtualMachine
func (vm *VirtualMachine) Run(ctx context.Context, cfg hv.RunConfig) error {
	if cfg == nil {
		return fmt.Errorf("rv64: RunConfig is nil")
	}
	return cfg.Run(ctx, vm.vcpu)
}

// VirtualCPUCall implements hv.VirtualMachine
func (vm *VirtualMachine) VirtualCPUCall(id int, f func(vcpu hv.VirtualCPU) error) error {
	if id != 0 {
		return fmt.Errorf("rv64: only vCPU 0 supported")
	}
	return f(vm.vcpu)
}

// AddDevice implements hv.VirtualMachine
func (vm *VirtualMachine) AddDevice(dev hv.Device) error {
	return fmt.Errorf("rv64: AddDevice not implemented")
}

// AddDeviceFromTemplate implements hv.VirtualMachine
func (vm *VirtualMachine) AddDeviceFromTemplate(template hv.DeviceTemplate) error {
	return fmt.Errorf("rv64: AddDeviceFromTemplate not implemented")
}

// AllocateMemory implements hv.VirtualMachine
func (vm *VirtualMachine) AllocateMemory(physAddr, size uint64) (hv.MemoryRegion, error) {
	region := NewMemoryRegion(size)
	vm.machine.Bus.AddDevice(physAddr, region)
	return &MemoryRegionWrapper{region: region, base: physAddr}, nil
}

// CaptureSnapshot implements hv.VirtualMachine
func (vm *VirtualMachine) CaptureSnapshot() (hv.Snapshot, error) {
	return nil, fmt.Errorf("rv64: snapshot not implemented")
}

// RestoreSnapshot implements hv.VirtualMachine
func (vm *VirtualMachine) RestoreSnapshot(snap hv.Snapshot) error {
	return fmt.Errorf("rv64: snapshot not implemented")
}

// ReadAt implements hv.VirtualMachine
func (vm *VirtualMachine) ReadAt(p []byte, off int64) (int, error) {
	return vm.machine.ReadAt(p, off)
}

// WriteAt implements hv.VirtualMachine
func (vm *VirtualMachine) WriteAt(p []byte, off int64) (int, error) {
	return vm.machine.WriteAt(p, off)
}

// SetIRQ implements hv.VirtualMachine
func (vm *VirtualMachine) SetIRQ(irqLine uint32, level bool) error {
	vm.machine.PLIC.SetPending(irqLine, level)
	return nil
}

// Machine returns the underlying machine
func (vm *VirtualMachine) Machine() *Machine {
	return vm.machine
}

// SetOutput sets the UART output
func (vm *VirtualMachine) SetOutput(w io.Writer) {
	vm.machine.UART.Output = w
}

// SetInput sets the UART input
func (vm *VirtualMachine) SetInput(r io.Reader) {
	vm.machine.UART.Input = r
}

// MemoryRegionWrapper wraps MemoryRegion for hv.MemoryRegion interface
type MemoryRegionWrapper struct {
	region *MemoryRegion
	base   uint64
}

// Size implements hv.MemoryRegion
func (m *MemoryRegionWrapper) Size() uint64 {
	return m.region.Size()
}

// ReadAt implements hv.MemoryRegion
func (m *MemoryRegionWrapper) ReadAt(p []byte, off int64) (int, error) {
	return m.region.ReadAt(p, off)
}

// WriteAt implements hv.MemoryRegion
func (m *MemoryRegionWrapper) WriteAt(p []byte, off int64) (int, error) {
	return m.region.WriteAt(p, off)
}

// VirtualCPU implements hv.VirtualCPU for RV64GC
type VirtualCPU struct {
	vm *VirtualMachine
	id int
}

// VirtualMachine implements hv.VirtualCPU
func (vcpu *VirtualCPU) VirtualMachine() hv.VirtualMachine {
	return vcpu.vm
}

// ID implements hv.VirtualCPU
func (vcpu *VirtualCPU) ID() int {
	return vcpu.id
}

// SetRegisters implements hv.VirtualCPU
func (vcpu *VirtualCPU) SetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	for reg, value := range regs {
		val64, ok := value.(hv.Register64)
		if !ok {
			return fmt.Errorf("rv64: unsupported register value type %T", value)
		}

		switch {
		case reg >= hv.RegisterRISCVX0 && reg <= hv.RegisterRISCVX31:
			idx := int(reg - hv.RegisterRISCVX0)
			vcpu.vm.machine.CPU.WriteReg(uint32(idx), uint64(val64))
		case reg == hv.RegisterRISCVPc:
			vcpu.vm.machine.SetPC(uint64(val64))
		default:
			return fmt.Errorf("rv64: unsupported register %v", reg)
		}
	}
	return nil
}

// GetRegisters implements hv.VirtualCPU
func (vcpu *VirtualCPU) GetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	for reg := range regs {
		switch {
		case reg >= hv.RegisterRISCVX0 && reg <= hv.RegisterRISCVX31:
			idx := int(reg - hv.RegisterRISCVX0)
			regs[reg] = hv.Register64(vcpu.vm.machine.CPU.ReadReg(uint32(idx)))
		case reg == hv.RegisterRISCVPc:
			regs[reg] = hv.Register64(vcpu.vm.machine.GetPC())
		default:
			return fmt.Errorf("rv64: unsupported register %v", reg)
		}
	}
	return nil
}

// Run implements hv.VirtualCPU
func (vcpu *VirtualCPU) Run(ctx context.Context) error {
	vcpu.vm.machine.SetStopOnZero(true)

	err := vcpu.vm.machine.Run(ctx, 500000)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrHalt):
		return hv.ErrVMHalted
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return hv.ErrInterrupted
	default:
		return err
	}
}

var (
	_ hv.Hypervisor     = &Hypervisor{}
	_ hv.VirtualMachine = &VirtualMachine{}
	_ hv.VirtualCPU     = &VirtualCPU{}
	_ hv.MemoryRegion   = &MemoryRegionWrapper{}
)
