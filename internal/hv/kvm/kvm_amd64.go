//go:build linux && amd64

package kvm

import (
	"context"
	"fmt"
	"unsafe"

	"github.com/tinyrange/cc/internal/hv"
)

var (
	regularRegisters = map[hv.Register]bool{
		hv.RegisterAMD64Rax:    true,
		hv.RegisterAMD64Rbx:    true,
		hv.RegisterAMD64Rcx:    true,
		hv.RegisterAMD64Rdx:    true,
		hv.RegisterAMD64Rsi:    true,
		hv.RegisterAMD64Rdi:    true,
		hv.RegisterAMD64Rsp:    true,
		hv.RegisterAMD64Rbp:    true,
		hv.RegisterAMD64R8:     true,
		hv.RegisterAMD64R9:     true,
		hv.RegisterAMD64R10:    true,
		hv.RegisterAMD64R11:    true,
		hv.RegisterAMD64R12:    true,
		hv.RegisterAMD64R13:    true,
		hv.RegisterAMD64R14:    true,
		hv.RegisterAMD64R15:    true,
		hv.RegisterAMD64Rip:    true,
		hv.RegisterAMD64Rflags: true,
	}
)

func (v *virtualCPU) SetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	hasRegularRegister := false
	for reg := range regs {
		if regularRegisters[reg] {
			hasRegularRegister = true
			break
		} else {
			return fmt.Errorf("kvm: unsupported register %v for architecture x86_64", reg)
		}
	}

	if hasRegularRegister {
		regularRegs, err := getRegisters(v.fd)
		if err != nil {
			return fmt.Errorf("kvm: get registers: %w", err)
		}

		if v, ok := regs[hv.RegisterAMD64Rax]; ok {
			regularRegs.Rax = uint64(v.(hv.Register64))
		}
		if v, ok := regs[hv.RegisterAMD64Rbx]; ok {
			regularRegs.Rbx = uint64(v.(hv.Register64))
		}
		if v, ok := regs[hv.RegisterAMD64Rcx]; ok {
			regularRegs.Rcx = uint64(v.(hv.Register64))
		}
		if v, ok := regs[hv.RegisterAMD64Rdx]; ok {
			regularRegs.Rdx = uint64(v.(hv.Register64))
		}
		if v, ok := regs[hv.RegisterAMD64Rsi]; ok {
			regularRegs.Rsi = uint64(v.(hv.Register64))
		}
		if v, ok := regs[hv.RegisterAMD64Rdi]; ok {
			regularRegs.Rdi = uint64(v.(hv.Register64))
		}
		if v, ok := regs[hv.RegisterAMD64Rsp]; ok {
			regularRegs.Rsp = uint64(v.(hv.Register64))
		}
		if v, ok := regs[hv.RegisterAMD64Rbp]; ok {
			regularRegs.Rbp = uint64(v.(hv.Register64))
		}
		if v, ok := regs[hv.RegisterAMD64R8]; ok {
			regularRegs.R8 = uint64(v.(hv.Register64))
		}
		if v, ok := regs[hv.RegisterAMD64R9]; ok {
			regularRegs.R9 = uint64(v.(hv.Register64))
		}
		if v, ok := regs[hv.RegisterAMD64R10]; ok {
			regularRegs.R10 = uint64(v.(hv.Register64))
		}
		if v, ok := regs[hv.RegisterAMD64R11]; ok {
			regularRegs.R11 = uint64(v.(hv.Register64))
		}
		if v, ok := regs[hv.RegisterAMD64R12]; ok {
			regularRegs.R12 = uint64(v.(hv.Register64))
		}
		if v, ok := regs[hv.RegisterAMD64R13]; ok {
			regularRegs.R13 = uint64(v.(hv.Register64))
		}
		if v, ok := regs[hv.RegisterAMD64R14]; ok {
			regularRegs.R14 = uint64(v.(hv.Register64))
		}
		if v, ok := regs[hv.RegisterAMD64R15]; ok {
			regularRegs.R15 = uint64(v.(hv.Register64))
		}
		if v, ok := regs[hv.RegisterAMD64Rip]; ok {
			regularRegs.Rip = uint64(v.(hv.Register64))
		}
		if v, ok := regs[hv.RegisterAMD64Rflags]; ok {
			regularRegs.Rflags = uint64(v.(hv.Register64))
		}

		if err := setRegisters(v.fd, &regularRegs); err != nil {
			return fmt.Errorf("kvm: set registers: %w", err)
		}
	}

	return nil
}

func (v *virtualCPU) GetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	return fmt.Errorf("kvm: GetRegisters not implemented")
}

func (v *virtualCPU) Run(ctx context.Context) error {
	if _, err := ioctlWithRetry(uintptr(v.fd), uint64(kvmRun), 0); err != nil {
		return fmt.Errorf("kvm: run vCPU %d: %w", v.id, err)
	}

	run := (*kvmRunData)(unsafe.Pointer(&v.run[0]))

	reason := kvmExitReason(run.exit_reason)

	switch reason {
	case kvmExitInternalError:
		err := (*internalError)(unsafe.Pointer(&run.anon0[0]))

		return fmt.Errorf("kvm: vCPU %d exited with internal error: %s", v.id, err.Suberror)
	case kvmExitHlt:
		return hv.ErrVMHalted
	default:
		return fmt.Errorf("kvm: vCPU %d exited with unknown reason %s", v.id, reason)
	}
}

func (hv *hypervisor) archVMInit(vmFd int) error {
	if err := setTSSAddr(vmFd, 0xfffbd000); err != nil {
		return fmt.Errorf("setting TSS addr: %w", err)
	}

	return nil
}

func (hv *hypervisor) archVCPUInit(vm *virtualMachine, vcpuFd int) error {
	cpuId, err := getSupportedCpuId(hv.fd)
	if err != nil {
		return fmt.Errorf("getting vCPU ID: %w", err)
	}

	if err := setVCPUID(vcpuFd, cpuId); err != nil {
		return fmt.Errorf("setting vCPU ID: %w", err)
	}

	return nil
}

func (*hypervisor) Architecture() hv.CpuArchitecture {
	return hv.ArchitectureX86_64
}

func (vcpu *virtualCPU) SetProtectedMode() error {
	sregs, err := getSRegs(vcpu.fd)
	if err != nil {
		return err
	}

	sregs.Ds = kvmSegment{
		Base:     0,
		Limit:    0xffffffff,
		Selector: 2 << 3,
		Present:  1,
		Type:     3, // Data: read/write, accessed
		Dpl:      0,
		Db:       1,
		S:        1, // Code/data
		L:        0,
		G:        1, // 4KB granularity
	}
	sregs.Es = sregs.Ds
	sregs.Fs = sregs.Ds
	sregs.Gs = sregs.Ds
	sregs.Ss = sregs.Ds

	sregs.Cs = kvmSegment{
		Base:     0,
		Limit:    0xffffffff,
		Selector: 1 << 3,
		Present:  1,
		Type:     11, // Code: execute, read, accessed
		Dpl:      0,
		Db:       1,
		S:        1, // Code/data
		L:        0,
		G:        1, // 4KB granularity
	}

	sregs.Cr0 |= 1

	if err := setSRegs(vcpu.fd, &sregs); err != nil {
		return err
	}

	return nil
}

var (
	_ hv.VirtualCPUAmd64 = &virtualCPU{}
)
