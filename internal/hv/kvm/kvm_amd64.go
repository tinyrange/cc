//go:build linux && amd64

package kvm

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"github.com/tinyrange/cc/internal/hv"
	"golang.org/x/sys/unix"
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

	specialRegisters = map[hv.Register]bool{
		hv.RegisterAMD64Cr3: true,
	}
)

func (v *virtualCPU) SetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	hasRegularRegister := false
	hasSpecialRegisters := false
	for reg := range regs {
		if regularRegisters[reg] {
			hasRegularRegister = true
		} else if specialRegisters[reg] {
			hasSpecialRegisters = true
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

	if hasSpecialRegisters {
		specialRegs, err := getSRegs(v.fd)
		if err != nil {
			return fmt.Errorf("kvm: get special registers: %w", err)
		}

		if v, ok := regs[hv.RegisterAMD64Cr3]; ok {
			specialRegs.Cr3 = uint64(v.(hv.Register64))
		}

		if err := setSRegs(v.fd, &specialRegs); err != nil {
			return fmt.Errorf("kvm: set special registers: %w", err)
		}
	}

	return nil
}

func (v *virtualCPU) GetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	hasRegularRegister := false
	hasSpecialRegisters := false

	for reg := range regs {
		if regularRegisters[reg] {
			hasRegularRegister = true
		} else if specialRegisters[reg] {
			hasSpecialRegisters = true
		} else {
			return fmt.Errorf("kvm: unsupported register %v for architecture x86_64", reg)
		}
	}

	if hasRegularRegister {
		regularRegs, err := getRegisters(v.fd)
		if err != nil {
			return fmt.Errorf("kvm: get registers: %w", err)
		}

		for reg := range regs {
			switch reg {
			case hv.RegisterAMD64Rax:
				regs[reg] = hv.Register64(regularRegs.Rax)
			case hv.RegisterAMD64Rbx:
				regs[reg] = hv.Register64(regularRegs.Rbx)
			case hv.RegisterAMD64Rcx:
				regs[reg] = hv.Register64(regularRegs.Rcx)
			case hv.RegisterAMD64Rdx:
				regs[reg] = hv.Register64(regularRegs.Rdx)
			case hv.RegisterAMD64Rsi:
				regs[reg] = hv.Register64(regularRegs.Rsi)
			case hv.RegisterAMD64Rdi:
				regs[reg] = hv.Register64(regularRegs.Rdi)
			case hv.RegisterAMD64Rsp:
				regs[reg] = hv.Register64(regularRegs.Rsp)
			case hv.RegisterAMD64Rbp:
				regs[reg] = hv.Register64(regularRegs.Rbp)
			case hv.RegisterAMD64R8:
				regs[reg] = hv.Register64(regularRegs.R8)
			case hv.RegisterAMD64R9:
				regs[reg] = hv.Register64(regularRegs.R9)
			case hv.RegisterAMD64R10:
				regs[reg] = hv.Register64(regularRegs.R10)
			case hv.RegisterAMD64R11:
				regs[reg] = hv.Register64(regularRegs.R11)
			case hv.RegisterAMD64R12:
				regs[reg] = hv.Register64(regularRegs.R12)
			case hv.RegisterAMD64R13:
				regs[reg] = hv.Register64(regularRegs.R13)
			case hv.RegisterAMD64R14:
				regs[reg] = hv.Register64(regularRegs.R14)
			case hv.RegisterAMD64R15:
				regs[reg] = hv.Register64(regularRegs.R15)
			case hv.RegisterAMD64Rip:
				regs[reg] = hv.Register64(regularRegs.Rip)
			case hv.RegisterAMD64Rflags:
				regs[reg] = hv.Register64(regularRegs.Rflags)
			}
		}
	}

	if hasSpecialRegisters {
		specialRegs, err := getSRegs(v.fd)
		if err != nil {
			return fmt.Errorf("kvm: get special registers: %w", err)
		}

		for reg := range regs {
			switch reg {
			case hv.RegisterAMD64Cr3:
				regs[reg] = hv.Register64(specialRegs.Cr3)
			}
		}
	}

	return nil
}

func (v *virtualCPU) Run(ctx context.Context) error {
	usingContext := false
	var stopNotify func() bool
	if done := ctx.Done(); done != nil {
		usingContext = true
		tid := unix.Gettid()
		stopNotify = context.AfterFunc(ctx, func() {
			_ = v.RequestImmediateExit(tid)
		})
	}
	if stopNotify != nil {
		defer stopNotify()
	}

	run := (*kvmRunData)(unsafe.Pointer(&v.run[0]))

	// clear immediate_exit in case it was set
	run.immediate_exit = 0

	// keep trying to run the vCPU until it exits or an error occurs
	for {
		_, err := ioctl(uintptr(v.fd), uint64(kvmRun), 0)
		if errors.Is(err, unix.EINTR) {
			if usingContext && (errors.Is(ctx.Err(), context.Canceled) ||
				errors.Is(ctx.Err(), context.DeadlineExceeded)) {
				return ctx.Err()
			}

			continue
		} else if err != nil {
			return fmt.Errorf("kvm: run vCPU %d: %w", v.id, err)
		}

		break
	}

	reason := kvmExitReason(run.exit_reason)

	switch reason {
	case kvmExitInternalError:
		err := (*internalError)(unsafe.Pointer(&run.anon0[0]))

		return fmt.Errorf("kvm: vCPU %d exited with internal error: %s", v.id, err.Suberror)
	case kvmExitHlt:
		return hv.ErrVMHalted
	case kvmExitIo:
		ioData := (*kvmExitIoData)(unsafe.Pointer(&run.anon0[0]))

		return v.handleIO(ioData)
	case kvmExitMmio:
		mmioData := (*kvmExitMMIOData)(unsafe.Pointer(&run.anon0[0]))

		return v.handleMMIO(mmioData)
	case kvmExitShutdown:
		return hv.ErrVMHalted
	case kvmExitSystemEvent:
		system := (*kvmSystemEvent)(unsafe.Pointer(&run.anon0[0]))
		if system.typ == uint32(kvmSystemEventShutdown) {
			return hv.ErrVMHalted
		}
		return fmt.Errorf("kvm: vCPU %d exited with system event %d", v.id, system.typ)
	default:
		return fmt.Errorf("kvm: vCPU %d exited with unknown reason %s", v.id, reason)
	}
}

func (v *virtualCPU) handleIO(ioData *kvmExitIoData) error {
	for _, dev := range v.vm.devices {
		if kvmIoPortDevice, ok := dev.(hv.X86IOPortDevice); ok {
			ports := kvmIoPortDevice.IOPorts()
			for _, port := range ports {
				if port == ioData.port {
					data := v.run[ioData.dataOffset : ioData.dataOffset+uint64(ioData.size)*uint64(ioData.count)]

					if ioData.direction == 0 {
						if err := kvmIoPortDevice.ReadIOPort(ioData.port, data); err != nil {
							return fmt.Errorf("I/O port 0x%04x read: %w", ioData.port, err)
						}
					} else {
						if err := kvmIoPortDevice.WriteIOPort(ioData.port, data); err != nil {
							return fmt.Errorf("I/O port 0x%04x write: %w", ioData.port, err)
						}
					}

					return nil
				}
			}
		}
	}

	return fmt.Errorf("no device handles I/O port 0x%04x", ioData.port)
}

func (v *virtualCPU) handleMMIO(mmioData *kvmExitMMIOData) error {
	for _, dev := range v.vm.devices {
		if kvmMmioDevice, ok := dev.(hv.MemoryMappedIODevice); ok {
			addr := mmioData.physAddr
			size := uint32(len(mmioData.data))
			regions := kvmMmioDevice.MMIORegions()
			for _, region := range regions {
				if addr >= region.Address && addr+uint64(size) <= region.Address+region.Size {
					data := mmioData.data[:size]

					if mmioData.isWrite == 0 {
						if err := kvmMmioDevice.ReadMMIO(addr, data); err != nil {
							return fmt.Errorf("MMIO read at 0x%016x: %w", addr, err)
						}
					} else {
						if err := kvmMmioDevice.WriteMMIO(addr, data); err != nil {
							return fmt.Errorf("MMIO write at 0x%016x: %w", addr, err)
						}
					}

					return nil
				}
			}
		}
	}

	return fmt.Errorf("no device handles MMIO at 0x%016x", mmioData.physAddr)
}

func (hv *hypervisor) archVMInit(vm *virtualMachine, config hv.VMConfig) error {
	if err := setTSSAddr(vm.vmFd, 0xfffbd000); err != nil {
		return fmt.Errorf("setting TSS addr: %w", err)
	}

	if config.NeedsInterruptSupport() {
		if err := createIRQChip(vm.vmFd); err != nil {
			return fmt.Errorf("creating IRQ chip: %w", err)
		}

		vm.hasIRQChip = true

		if err := createPIT(vm.vmFd); err != nil {
			return fmt.Errorf("creating PIT: %w", err)
		}
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

// CR0 bits
const (
	cr0_PE = 1
	cr0_MP = (1 << 1)
	cr0_EM = (1 << 2)
	cr0_TS = (1 << 3)
	cr0_ET = (1 << 4)
	cr0_NE = (1 << 5)
	cr0_WP = (1 << 16)
	cr0_AM = (1 << 18)
	cr0_NW = (1 << 29)
	cr0_CD = (1 << 30)
	cr0_PG = (1 << 31)
)

// CR4 bits
const (
	cr4_VME        = 1
	cr4_PVI        = (1 << 1)
	cr4_TSD        = (1 << 2)
	cr4_DE         = (1 << 3)
	cr4_PSE        = (1 << 4)
	cr4_PAE        = (1 << 5)
	cr4_MCE        = (1 << 6)
	cr4_PGE        = (1 << 7)
	cr4_PCE        = (1 << 8)
	cr4_OSFXSR     = (1 << 8)
	cr4_OSXMMEXCPT = (1 << 10)
	cr4_UMIP       = (1 << 11)
	cr4_VMXE       = (1 << 13)
	cr4_SMXE       = (1 << 14)
	cr4_FSGSBASE   = (1 << 16)
	cr4_PCIDE      = (1 << 17)
	cr4_OSXSAVE    = (1 << 18)
	cr4_SMEP       = (1 << 20)
	cr4_SMAP       = (1 << 21)
)

// EFER bits
const (
	efer_SCE   = 1
	efer_LME   = (1 << 8)
	efer_LMA   = (1 << 10)
	efer_NXE   = (1 << 11)
	efer_SVME  = (1 << 12)
	efer_LMSLE = (1 << 13)
	efer_FFXSR = (1 << 14)
)

const (
	p  = 1 << 0 // present
	rw = 1 << 1 // writable
	us = 1 << 2 // user
	ps = 1 << 7 // page-size (2MiB when set in PDE)
)

func (vcpu *virtualCPU) SetLongModeWithSelectors(
	pagingBase uint64,
	addrSpaceSize int,
	codeSelector, dataSelector uint16,
) error {
	memBase := vcpu.vm.memoryBase
	memData := vcpu.vm.memory

	// Translate a guest-phys address to an index into mem.Data.
	host := func(gpa uint64) int {
		if gpa < memBase {
			panic("GPA below memory base")
		}
		off := gpa - memBase
		if off > uint64(len(memData)) {
			panic("GPA outside allocated mem")
		}
		return int(off)
	}

	// All paging structures must be 4KiB aligned GPAs.
	pml4Addr := (memBase + pagingBase + 0x0000) &^ 0xFFF
	pdptAddr := (memBase + pagingBase + 0x1000) &^ 0xFFF
	pdBase := (memBase + pagingBase + 0x2000) &^ 0xFFF // room for 4 PDs

	pml4 := (*[512]uint64)(unsafe.Pointer(&memData[host(pml4Addr)]))[:]
	pdpt := (*[512]uint64)(unsafe.Pointer(&memData[host(pdptAddr)]))[:]

	// Zero tables (paranoia / re-run friendly)
	for i := range pml4 {
		pml4[i] = 0
	}
	for i := range pdpt {
		pdpt[i] = 0
	}

	// Allocate & hook 4 PDs at pdBase + n*0x1000
	for giB := 0; giB < addrSpaceSize; giB++ {
		pdAddr := pdBase + uint64(giB)*0x1000
		pd := (*[512]uint64)(unsafe.Pointer(&memData[host(pdAddr)]))[:]
		for i := range pd {
			pd[i] = 0
		}

		// PML4[0] -> PDPT (single PML4 covers low 512 GiB)
		pml4[0] = (pdptAddr &^ 0xFFF) | p | rw | us

		// PDPT[giB] -> PD[giB]
		pdpt[giB] = (pdAddr &^ 0xFFF) | p | rw | us

		// Fill PD with 2MiB identity mappings for this 1 GiB slice
		// Base address of this GiB chunk:
		baseGiB := uint64(giB) << 30
		for i := range 512 {
			phys := baseGiB | (uint64(i) << 21) // 2MiB step
			pd[i] = (phys &^ 0x1FFFFF) | p | rw | us | ps
		}
	}

	// ---- control regs & segments -------------------------------------------
	sregs, err := getSRegs(vcpu.fd)
	if err != nil {
		return err
	}

	sregs.Cr3 = pml4Addr
	sregs.Cr4 |= cr4_PAE
	sregs.Cr0 |= cr0_PE | cr0_MP | cr0_ET | cr0_NE | cr0_WP | cr0_AM | cr0_PG
	sregs.Efer = efer_LME | efer_LMA

	// 64-bit code segment (CS.L=1, D=0), flat data segments
	code := kvmSegment{
		Base:     0,
		Limit:    0xffffffff,
		Selector: codeSelector,
		Present:  1,
		Type:     11, // code: exec/read/accessed
		Dpl:      0,
		Db:       0, // MUST be 0 in 64-bit
		S:        1, // code/data
		L:        1, // 64-bit
		G:        1,
	}
	sregs.Cs = code

	data := code
	data.Type = 3 // data: read/write/accessed
	data.L = 0    // data segments ignore L, keep conventional values
	data.Db = 1   // 4 GiB flat segment as required by Linux boot proto
	data.Selector = dataSelector
	sregs.Ds, sregs.Es, sregs.Fs, sregs.Gs, sregs.Ss = data, data, data, data, data

	if err := setSRegs(vcpu.fd, &sregs); err != nil {
		return err
	}

	return nil
}

var (
	_ hv.VirtualCPUAmd64 = &virtualCPU{}
)

func (v *virtualMachine) PulseIRQ(irqLine uint32) error {
	if !v.hasIRQChip {
		return fmt.Errorf("kvm: cannot pulse IRQ without irqchip")
	}

	return pulseIRQ(v.vmFd, irqLine)
}

var (
	_ hv.VirtualMachineAmd64 = &virtualMachine{}
)
