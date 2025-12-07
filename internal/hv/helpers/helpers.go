package helpers

import (
	"context"
	"errors"
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/ir"
)

type LoadMode int

const (
	ModeInvalid LoadMode = iota
	ModeProtectedMode
	Mode64BitIdentityMapping
)

func (m LoadMode) String() string {
	switch m {
	case ModeProtectedMode:
		return "protected-mode"
	case Mode64BitIdentityMapping:
		return "64bit-identity-mapping"
	default:
		return "invalid"
	}
}

type ProgramLoader struct {
	Program           ir.Program
	Mode              LoadMode
	BaseAddr          uint64
	MaxLoopIterations uint64

	Arm64ExceptionVectors *Arm64ExceptionVectorConfig

	arm64VectorBase uint64
}

type Arm64ExceptionVectorConfig struct {
	Table []byte
	Align uint64
}

const (
	psrModeEL1h = 0x5
	psrDBit     = 0x200
	psrABit     = 0x100
	psrIBit     = 0x80
	psrFBit     = 0x40
)

// Run implements hv.RunConfig.
func (p *ProgramLoader) Run(ctx context.Context, vcpu hv.VirtualCPU) error {
	arch := vcpu.VirtualMachine().Hypervisor().Architecture()
	switch arch {
	case hv.ArchitectureX86_64:
		switch p.Mode {
		case ModeProtectedMode:
			if err := vcpu.(hv.VirtualCPUAmd64).SetProtectedMode(); err != nil {
				return fmt.Errorf("set protected mode: %w", err)
			}
		case Mode64BitIdentityMapping:
			if err := vcpu.(hv.VirtualCPUAmd64).SetLongModeWithSelectors(
				0x20000, // paging structures base addr in guest memory
				4,       // 4GiB address space
				0x10,    // code selector
				0x18,    // data selector
			); err != nil {
				return fmt.Errorf("set long mode with selectors: %w", err)
			}
		default:
			return fmt.Errorf("unsupported load mode %v for architecture %v", p.Mode, arch)
		}

		if err := vcpu.SetRegisters(map[hv.Register]hv.RegisterValue{
			hv.RegisterAMD64Rflags: hv.Register64(0x2),
			hv.RegisterAMD64Rip:    hv.Register64(p.BaseAddr),
		}); err != nil {
			return fmt.Errorf("set initial registers: %w", err)
		}

		for range p.MaxLoopIterations {
			if err := vcpu.Run(ctx); err != nil {
				if errors.Is(err, hv.ErrVMHalted) ||
					errors.Is(err, hv.ErrGuestRequestedReboot) ||
					errors.Is(err, hv.ErrYield) {
					return nil
				}
				return fmt.Errorf("run vCPU: %w", err)
			}
		}

		return fmt.Errorf("maximum loop iterations (%d) exceeded", p.MaxLoopIterations)
	case hv.ArchitectureARM64:
		if p.Mode != Mode64BitIdentityMapping {
			return fmt.Errorf("unsupported load mode %v for architecture %v", p.Mode, arch)
		}

		regs := map[hv.Register]hv.RegisterValue{
			hv.RegisterARM64Pc:     hv.Register64(p.BaseAddr),
			hv.RegisterARM64Pstate: hv.Register64(psrModeEL1h | psrDBit | psrABit | psrIBit | psrFBit),
		}
		if p.Arm64ExceptionVectors != nil {
			if p.arm64VectorBase == 0 {
				return fmt.Errorf("arm64 exception vectors requested but not initialized by loader")
			}
			regs[hv.RegisterARM64Vbar] = hv.Register64(p.arm64VectorBase)
		}
		if err := vcpu.SetRegisters(regs); err != nil {
			return fmt.Errorf("set initial registers: %w", err)
		}

		for range p.MaxLoopIterations {
			if err := vcpu.Run(ctx); err != nil {
				if errors.Is(err, hv.ErrVMHalted) ||
					errors.Is(err, hv.ErrGuestRequestedReboot) ||
					errors.Is(err, hv.ErrYield) {
					return nil
				}
				return fmt.Errorf("run vCPU: %w", err)
			}
		}

		return fmt.Errorf("maximum loop iterations (%d) exceeded", p.MaxLoopIterations)
	case hv.ArchitectureRISCV64:
		if p.Mode != Mode64BitIdentityMapping {
			return fmt.Errorf("unsupported load mode %v for architecture %v", p.Mode, arch)
		}

		regs := map[hv.Register]hv.RegisterValue{
			hv.RegisterRISCVPc: hv.Register64(p.BaseAddr),
		}
		if err := vcpu.SetRegisters(regs); err != nil {
			return fmt.Errorf("set initial registers: %w", err)
		}

		for range p.MaxLoopIterations {
			if err := vcpu.Run(ctx); err != nil {
				if errors.Is(err, hv.ErrVMHalted) ||
					errors.Is(err, hv.ErrGuestRequestedReboot) ||
					errors.Is(err, hv.ErrYield) {
					return nil
				}
				return fmt.Errorf("run vCPU: %w", err)
			}
		}

		return fmt.Errorf("maximum loop iterations (%d) exceeded", p.MaxLoopIterations)
	default:
		return fmt.Errorf("unsupported architecture: %v", arch)
	}
}

// Load implements hv.VMLoader.
func (p *ProgramLoader) Load(vm hv.VirtualMachine) error {
	var target hv.CpuArchitecture
	switch vm.Hypervisor().Architecture() {
	case hv.ArchitectureX86_64:
		target = hv.ArchitectureX86_64
	case hv.ArchitectureARM64:
		target = hv.ArchitectureARM64
	case hv.ArchitectureRISCV64:
		target = hv.ArchitectureRISCV64
	default:
		return fmt.Errorf("unsupported architecture: %v", vm.Hypervisor().Architecture())
	}

	prog, err := ir.BuildStandaloneProgramForArch(target, &p.Program)
	if err != nil {
		return fmt.Errorf("build standalone program: %w", err)
	}

	bytes := prog.RelocatedCopy(uintptr(p.BaseAddr))
	if _, err := vm.WriteAt(bytes, int64(p.BaseAddr)); err != nil {
		return fmt.Errorf("write program to vm memory: %w", err)
	}

	if target == hv.ArchitectureARM64 && p.Arm64ExceptionVectors != nil {
		if len(p.Arm64ExceptionVectors.Table) == 0 {
			return fmt.Errorf("arm64 exception vector table is empty")
		}
		align := p.Arm64ExceptionVectors.Align
		if align == 0 {
			align = 0x800
		}
		if align&(align-1) != 0 {
			return fmt.Errorf("arm64 exception vector alignment must be power of two (got %d)", align)
		}
		base := alignUp(uint64(p.BaseAddr)+uint64(len(bytes))+uint64(prog.BSSSize()), align)
		if _, err := vm.WriteAt(p.Arm64ExceptionVectors.Table, int64(base)); err != nil {
			return fmt.Errorf("write arm64 vector table: %w", err)
		}
		p.arm64VectorBase = base
	}

	return nil
}

var (
	_ hv.VMLoader  = &ProgramLoader{}
	_ hv.RunConfig = &ProgramLoader{}
)

func alignUp(value, align uint64) uint64 {
	if align == 0 {
		return value
	}
	mask := align - 1
	return (value + mask) &^ mask
}
