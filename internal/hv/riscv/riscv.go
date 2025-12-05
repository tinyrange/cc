package riscv

import (
	"context"
	"errors"
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/riscv/ccvm"
)

type hypervisor struct{}

type virtualMachine struct {
	hv         *hypervisor
	machine    *ccvm.Machine
	vcpu       *virtualCPU
	memoryBase uint64
}

type virtualCPU struct {
	vm *virtualMachine
	id int
}

func Open() (hv.Hypervisor, error) {
	return &hypervisor{}, nil
}

func (h *hypervisor) Close() error {
	return nil
}

func (h *hypervisor) Architecture() hv.CpuArchitecture {
	return hv.ArchitectureRISCV64
}

func (h *hypervisor) NewVirtualMachine(config hv.VMConfig) (hv.VirtualMachine, error) {
	if config == nil {
		return nil, fmt.Errorf("riscv: VMConfig is nil")
	}
	if config.CPUCount() != 1 {
		return nil, fmt.Errorf("riscv: only single CPU guests are supported")
	}

	memSize := config.MemorySize()
	if memSize == 0 {
		memSize = 64 * 1024 * 1024
	}

	machine, err := ccvm.NewMachine(memSize)
	if err != nil {
		return nil, fmt.Errorf("riscv: create machine: %w", err)
	}

	if memBase := config.MemoryBase(); memBase != 0 && memBase != machine.MemoryBase() {
		return nil, fmt.Errorf("riscv: memory base must be 0x%x (got 0x%x)", machine.MemoryBase(), memBase)
	}

	vm := &virtualMachine{
		hv:         h,
		machine:    machine,
		memoryBase: machine.MemoryBase(),
	}
	vm.vcpu = &virtualCPU{vm: vm, id: 0}

	if cb := config.Callbacks(); cb != nil {
		if err := cb.OnCreateVM(vm); err != nil {
			return nil, fmt.Errorf("riscv: VM callback OnCreateVM: %w", err)
		}
	}

	if loader := config.Loader(); loader != nil {
		if err := loader.Load(vm); err != nil {
			return nil, fmt.Errorf("riscv: load VM: %w", err)
		}
	}

	if cb := config.Callbacks(); cb != nil {
		if err := cb.OnCreateVMWithMemory(vm); err != nil {
			return nil, fmt.Errorf("riscv: VM callback OnCreateVMWithMemory: %w", err)
		}
		if err := cb.OnCreateVCPU(vm.vcpu); err != nil {
			return nil, fmt.Errorf("riscv: VM callback OnCreateVCPU: %w", err)
		}
	}

	return vm, nil
}

// implements hv.VirtualMachine.
func (v *virtualMachine) Hypervisor() hv.Hypervisor { return v.hv }
func (v *virtualMachine) MemorySize() uint64        { return v.machine.MemorySize() }
func (v *virtualMachine) MemoryBase() uint64        { return v.memoryBase }

func (v *virtualMachine) Close() error {
	return v.machine.Close()
}

func (v *virtualMachine) Run(ctx context.Context, cfg hv.RunConfig) error {
	if cfg == nil {
		return fmt.Errorf("riscv: RunConfig is nil")
	}
	return cfg.Run(ctx, v.vcpu)
}

func (v *virtualMachine) VirtualCPUCall(id int, f func(vcpu hv.VirtualCPU) error) error {
	if id != 0 {
		return fmt.Errorf("riscv: only vCPU 0 supported")
	}
	return f(v.vcpu)
}

func (v *virtualMachine) AddDevice(dev hv.Device) error {
	return fmt.Errorf("riscv: device support not implemented")
}

func (v *virtualMachine) AddDeviceFromTemplate(template hv.DeviceTemplate) error {
	return fmt.Errorf("riscv: device template support not implemented")
}

func (v *virtualMachine) AllocateMemory(physAddr, size uint64) (hv.MemoryRegion, error) {
	region, err := v.machine.AllocateMemory(physAddr, size)
	if err != nil {
		return nil, err
	}
	return &memoryRegion{region: region}, nil
}

func (v *virtualMachine) CaptureSnapshot() (hv.Snapshot, error) {
	return nil, fmt.Errorf("riscv: snapshot not implemented")
}

func (v *virtualMachine) RestoreSnapshot(hv.Snapshot) error {
	return fmt.Errorf("riscv: snapshot not implemented")
}

func (v *virtualMachine) ReadAt(p []byte, off int64) (n int, err error) {
	return v.machine.ReadAt(p, off)
}

func (v *virtualMachine) WriteAt(p []byte, off int64) (n int, err error) {
	return v.machine.WriteAt(p, off)
}

type memoryRegion struct {
	region ccvm.MemoryRegion
}

func (m *memoryRegion) Size() uint64 {
	return uint64(m.region.Size())
}

func (m *memoryRegion) ReadAt(p []byte, off int64) (n int, err error) {
	return m.region.ReadAt(p, off)
}

func (m *memoryRegion) WriteAt(p []byte, off int64) (n int, err error) {
	return m.region.WriteAt(p, off)
}

// implements hv.VirtualCPU.
func (v *virtualCPU) VirtualMachine() hv.VirtualMachine { return v.vm }
func (v *virtualCPU) ID() int                           { return v.id }

func (v *virtualCPU) SetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	for reg, value := range regs {
		val64, ok := value.(hv.Register64)
		if !ok {
			return fmt.Errorf("riscv: unsupported register value type %T", value)
		}

		switch {
		case reg >= hv.RegisterRISCVX0 && reg <= hv.RegisterRISCVX31:
			idx := int(reg - hv.RegisterRISCVX0)
			if err := v.vm.machine.SetRegister(idx, uint64(val64)); err != nil {
				return err
			}
		case reg == hv.RegisterRISCVPc:
			v.vm.machine.SetPC(uint64(val64))
		default:
			return fmt.Errorf("riscv: unsupported register %v", reg)
		}
	}
	return nil
}

func (v *virtualCPU) GetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	for reg := range regs {
		switch {
		case reg >= hv.RegisterRISCVX0 && reg <= hv.RegisterRISCVX31:
			idx := int(reg - hv.RegisterRISCVX0)
			val, err := v.vm.machine.Register(idx)
			if err != nil {
				return err
			}
			regs[reg] = hv.Register64(val)
		case reg == hv.RegisterRISCVPc:
			regs[reg] = hv.Register64(v.vm.machine.PC())
		default:
			return fmt.Errorf("riscv: unsupported register %v", reg)
		}
	}
	return nil
}

func (v *virtualCPU) Run(ctx context.Context) error {
	v.vm.machine.EnableStopOnZero()

	err := v.vm.machine.Run(ctx, 500000)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ccvm.ErrStopOnZero):
		return hv.ErrVMHalted
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return hv.ErrInterrupted
	default:
		return err
	}
}

var (
	_ hv.Hypervisor     = &hypervisor{}
	_ hv.VirtualCPU     = &virtualCPU{}
	_ hv.VirtualMachine = &virtualMachine{}
)
