//go:build linux && amd64

package kvm

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"github.com/tinyrange/cc/internal/debug"
	x86chipset "github.com/tinyrange/cc/internal/devices/amd64/chipset"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/timeslice"
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

	debug.Writef("kvm-amd64.Run run", "vCPU %d running", v.id)

	v.rec.Record(tsKvmHostTime)

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

	v.rec.Record(tsKvmGuestTime)

	exitCtx := &exitContext{
		timeslice: timeslice.InvalidTimesliceID,
	}

	reason := kvmExitReason(run.exit_reason)

	debug.Writef("kvm-amd64.Run exit", "vCPU %d exited with reason %s", v.id, reason)

	switch reason {
	case kvmExitInternalError:
		err := (*internalError)(unsafe.Pointer(&run.anon0[0]))

		return fmt.Errorf("kvm: vCPU %d exited with internal error: %s", v.id, err.Suberror)
	case kvmExitHlt:
		return hv.ErrVMHalted
	case kvmExitIo:
		ioData := (*kvmExitIoData)(unsafe.Pointer(&run.anon0[0]))

		if err := v.handleIO(exitCtx, ioData); err != nil {
			return fmt.Errorf("handle I/O: %w", err)
		}
	case kvmExitMmio:
		mmioData := (*kvmExitMMIOData)(unsafe.Pointer(&run.anon0[0]))

		if err := v.handleMMIO(exitCtx, mmioData); err != nil {
			return fmt.Errorf("handle MMIO: %w", err)
		}
	case kvmExitIoapicEoi:
		eoiData := (*kvmExitIoapicEoiData)(unsafe.Pointer(&run.anon0[0]))

		if err := v.handleIoapicEoi(eoiData); err != nil {
			return fmt.Errorf("handle IOAPIC EOI: %w", err)
		}
	case kvmExitShutdown:
		debug.Writef("kvm-amd64.Run shutdown", "vCPU %d exited with shutdown reason", v.id)

		return hv.ErrVMHalted
	case kvmExitSystemEvent:
		system := (*kvmSystemEvent)(unsafe.Pointer(&run.anon0[0]))

		debug.Writef("kvm-amd64.Run system event", "vCPU %d exited with system event %d", v.id, system.typ)

		if system.typ == uint32(kvmSystemEventShutdown) {
			return hv.ErrVMHalted
		} else if system.typ == uint32(kvmSystemEventReset) {
			return hv.ErrGuestRequestedReboot
		}
		return fmt.Errorf("kvm: vCPU %d exited with system event %d", v.id, system.typ)
	default:
		return fmt.Errorf("kvm: vCPU %d exited with unknown reason %s", v.id, reason)
	}

	if exitCtx.timeslice != timeslice.InvalidTimesliceID {
		v.rec.Record(exitCtx.timeslice)
	}

	return nil
}

func (v *virtualCPU) handleIO(exitCtx *exitContext, ioData *kvmExitIoData) error {
	data := v.run[ioData.dataOffset : ioData.dataOffset+uint64(ioData.size)*uint64(ioData.count)]

	debug.Writef("kvm-amd64.handleIO", "handleIO port=0x%04x size=%d count=%d direction=%d data=% x", ioData.port, ioData.size, ioData.count, ioData.direction, data)

	cs, err := v.vm.ensureChipset()
	if err != nil {
		return fmt.Errorf("initialize chipset: %w", err)
	}

	isWrite := ioData.direction != 0
	if err := cs.HandlePIO(exitCtx, ioData.port, data, isWrite); err != nil {
		return fmt.Errorf("I/O port 0x%04x: %w", ioData.port, err)
	}

	// Poll devices after I/O to allow serial device to read input
	if err := cs.Poll(context.Background()); err != nil {
		return fmt.Errorf("poll devices: %w", err)
	}

	return nil
}

func (v *virtualCPU) handleMMIO(exitCtx *exitContext, mmioData *kvmExitMMIOData) error {
	debug.Writef("kvm-amd64.handleMMIO", "handleMMIO physAddr=0x%016x size=%d isWrite=%d data=% x", mmioData.physAddr, mmioData.len, mmioData.isWrite, mmioData.data)

	cs, err := v.vm.ensureChipset()
	if err != nil {
		return fmt.Errorf("initialize chipset: %w", err)
	}

	size := int(mmioData.len)
	if size < 0 || size > len(mmioData.data) {
		return fmt.Errorf("MMIO length %d out of bounds (data len %d)", size, len(mmioData.data))
	}
	data := mmioData.data[:size]
	isWrite := mmioData.isWrite != 0

	if err := cs.HandleMMIO(exitCtx, mmioData.physAddr, data, isWrite); err != nil {
		return fmt.Errorf("MMIO at 0x%016x: %w", mmioData.physAddr, err)
	}

	// Poll devices after MMIO to allow serial device to read input
	if err := cs.Poll(context.Background()); err != nil {
		return fmt.Errorf("poll devices: %w", err)
	}

	return nil
}

func (v *virtualCPU) handleIoapicEoi(eoiData *kvmExitIoapicEoiData) error {
	debug.Writef("kvm-amd64.handleIoapicEoi", "handleIoapicEoi vector=%d", eoiData.vector)

	if v.vm.ioapic != nil {
		v.vm.ioapic.HandleEOI(uint32(eoiData.vector))
	}
	return nil
}

var (
	tsKvmSetTSSAddr          = timeslice.RegisterKind("kvm_set_tss_addr", 0)
	tsKvmEnabledSplitIRQChip = timeslice.RegisterKind("kvm_enabled_split_irqchip", 0)
	tsKvmCreatedIRQChip      = timeslice.RegisterKind("kvm_created_irqchip", 0)
	tsKvmCreatedIOAPIC       = timeslice.RegisterKind("kvm_created_ioapic", 0)
)

func (hv *hypervisor) archVMInit(vm *virtualMachine, config hv.VMConfig) error {
	debug.Writef("kvm-amd64.archVMInit", "archVMInit")

	if err := setTSSAddr(vm.vmFd, 0xfffbd000); err != nil {
		return fmt.Errorf("setting TSS addr: %w", err)
	}

	vm.rec.Record(tsKvmSetTSSAddr)

	if config.NeedsInterruptSupport() {
		// Enable split IRQ chip so IOAPIC is handled in userspace and LAPIC remains in-kernel.
		// In split mode, PIC and IOAPIC are NOT created in kernel - only LAPIC is.
		if err := enableSplitIRQChip(vm.vmFd, 24); err != nil && err != unix.EINVAL && err != unix.ENOTTY {
			return fmt.Errorf("enable split irqchip: %w", err)
		}
		vm.splitIRQChip = true

		vm.rec.Record(tsKvmEnabledSplitIRQChip)

		if err := createIRQChip(vm.vmFd); err != nil && err != unix.EEXIST {
			return fmt.Errorf("creating IRQ chip: %w", err)
		}

		vm.rec.Record(tsKvmCreatedIRQChip)

		vm.hasIRQChip = true

		vm.ioapic = x86chipset.NewIOAPIC(24)
		vm.ioapic.SetRouting(x86chipset.IoApicRoutingFunc(func(vector, dest, destMode, deliveryMode uint8, level bool) {
			// In split IRQ chip mode, we inject interrupts via MSI to the in-kernel LAPIC.
			// IOAPIC edge-triggered lines set level=false, but still require injection.
			// fmt.Printf("ioapic: assert vec=%02x dest=%d destMode=%d delivery=%d level=%v\n", vector, dest, destMode, deliveryMode, level)
			if err := vm.InjectInterrupt(vector, dest, destMode, deliveryMode); err != nil {
				// Best-effort log; avoid hard fail to keep guest progressing.
				fmt.Printf("kvm: inject IOAPIC interrupt vec=%d dest=%d err=%v\n", vector, dest, err)
			}
		}))
		if err := vm.AddDevice(vm.ioapic); err != nil {
			return fmt.Errorf("add IOAPIC device: %w", err)
		}

		vm.rec.Record(tsKvmCreatedIOAPIC)
	}

	return nil
}

// archPostVCPUInit is called after all vCPUs are created.
// On x86, no post-vCPU initialization is needed.
func (hv *hypervisor) archPostVCPUInit(vm *virtualMachine, config hv.VMConfig) error {
	return nil
}

var (
	tsKvmGetSupportedCpuId = timeslice.RegisterKind("kvm_get_supported_cpu_id", 0)
	tsKvmSetVCPUID         = timeslice.RegisterKind("kvm_set_vcpu_id", 0)
)

func (hv *hypervisor) archVCPUInit(vm *virtualMachine, vcpuFd int) error {
	debug.Writef("kvm-amd64.archVCPUInit", "archVCPUInit")

	cpuId, err := getSupportedCpuId(hv.fd)
	if err != nil {
		return fmt.Errorf("getting vCPU ID: %w", err)
	}

	vm.rec.Record(tsKvmGetSupportedCpuId)

	// Normalize CPUID-reported APIC IDs to match LAPIC ID 0 in our ACPI/MADT.
	// Hosts may return a non-zero APIC ID in leaf 0x1 EBX[31:24], which leads
	// to kernel warnings and can break timer setup during boot.
	entries := unsafe.Slice((*kvmCPUIDEntry2)(unsafe.Pointer(uintptr(unsafe.Pointer(cpuId))+unsafe.Sizeof(*cpuId))), cpuId.Nr)
	for i := range entries {
		switch entries[i].Function {
		case 0x1:
			entries[i].Ebx &^= 0xFF000000 // clear initial APIC ID
		case 0xB: // extended topology (x2APIC ID in EDX)
			entries[i].Ebx = 1 // one logical processor at this level
			entries[i].Edx = 0 // x2APIC ID
		}
	}

	// Inject KVM paravirt CPUID leaves for kvmclock support.
	// This allows the guest to use the paravirt clock which doesn't drift under load.
	cpuId = injectKvmParavirtCpuid(cpuId)

	if err := setVCPUID(vcpuFd, cpuId); err != nil {
		return fmt.Errorf("setting vCPU ID: %w", err)
	}

	vm.rec.Record(tsKvmSetVCPUID)

	return nil
}

// injectKvmParavirtCpuid adds or updates the KVM paravirt CPUID leaves (0x40000000, 0x40000001)
// to enable kvmclock support in the guest kernel.
func injectKvmParavirtCpuid(cpuId *kvmCPUID2) *kvmCPUID2 {
	entries := unsafe.Slice((*kvmCPUIDEntry2)(unsafe.Pointer(uintptr(unsafe.Pointer(cpuId))+unsafe.Sizeof(*cpuId))), 255)

	// Track indices of existing KVM leaves
	leaf0Index := -1
	leaf1Index := -1

	for i := 0; i < int(cpuId.Nr); i++ {
		switch entries[i].Function {
		case 0x40000000:
			leaf0Index = i
		case 0x40000001:
			leaf1Index = i
		}
	}

	// KVM signature: "KVMKVMKVM\0\0\0" encoded as EBX, ECX, EDX
	// Each 4-byte chunk is stored as a little-endian uint32:
	// "KVMK" → K(0x4B) V(0x56) M(0x4D) K(0x4B) → 0x4B4D564B
	// "VMKV" → V(0x56) M(0x4D) K(0x4B) V(0x56) → 0x564B4D56
	// "M\0\0\0" → M(0x4D) 0 0 0 → 0x0000004D
	const (
		kvmSigEbx = 0x4b4d564b // "KVMK"
		kvmSigEcx = 0x564b4d56 // "VMKV"
		kvmSigEdx = 0x0000004d // "M\0\0\0"
	)

	// Add or update leaf 0x40000000 (KVM signature)
	if leaf0Index < 0 {
		leaf0Index = int(cpuId.Nr)
		cpuId.Nr++
	}
	entries[leaf0Index] = kvmCPUIDEntry2{
		Function: 0x40000000,
		Eax:      0x40000001, // max supported KVM leaf
		Ebx:      kvmSigEbx,
		Ecx:      kvmSigEcx,
		Edx:      kvmSigEdx,
	}

	// Add or update leaf 0x40000001 (KVM features)
	if leaf1Index < 0 {
		leaf1Index = int(cpuId.Nr)
		cpuId.Nr++
	}
	entries[leaf1Index] = kvmCPUIDEntry2{
		Function: 0x40000001,
		Eax:      kvmFeatureClockSource | kvmFeatureClockSource2 | kvmFeatureClockSourceStable,
		Ebx:      0,
		Ecx:      0,
		Edx:      0,
	}

	return cpuId
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
	_ hv.VirtualCPUAmd64     = &virtualCPU{}
	_ hv.VirtualMachineAmd64 = &virtualMachine{}
)

// Snapshot Support

const (
	irqChipPICMaster = 0
	irqChipPICSlave  = 1
	irqChipIOAPIC    = 2
)

type vcpuSnapshot struct {
	Regs         kvmRegs
	SRegs        kvmSRegs
	FPU          kvmFPU
	Lapic        kvmLapicState
	LapicPresent bool
	Xsave        kvmXsave
	Xcrs         kvmXcrs
	Msrs         []kvmMsrEntry
}

type snapshot struct {
	cpuStates map[int]vcpuSnapshot

	deviceSnapshots map[string]interface{}
	memory          []byte
	clockData       *kvmClockData
	irqChips        []kvmIRQChip
	pitState        *kvmPitState2
}

func (v *virtualCPU) captureSnapshot() (vcpuSnapshot, error) {
	var ret vcpuSnapshot

	regs, err := getRegisters(v.fd)
	if err != nil {
		return ret, fmt.Errorf("capture general registers: %w", err)
	}
	ret.Regs = regs

	sregs, err := getSRegs(v.fd)
	if err != nil {
		return ret, fmt.Errorf("capture special registers: %w", err)
	}
	ret.SRegs = sregs

	fpu, err := getFPU(v.fd)
	if err != nil {
		return ret, fmt.Errorf("capture FPU state: %w", err)
	}
	ret.FPU = fpu

	lapic, err := getLapic(v.fd)
	if err != nil {
		if !errors.Is(err, unix.EINVAL) {
			return ret, fmt.Errorf("capture LAPIC state: %w", err)
		}
	} else {
		ret.Lapic = lapic
		ret.LapicPresent = true
	}

	xsave, err := getXsave(v.fd)
	if err != nil {
		return ret, fmt.Errorf("capture XSAVE state: %w", err)
	}
	ret.Xsave = xsave

	xcrs, err := getXcrs(v.fd)
	if err != nil {
		return ret, fmt.Errorf("capture XCRs: %w", err)
	}
	ret.Xcrs = xcrs

	msrIndices, err := v.vm.hv.snapshotMSRs()
	if err != nil {
		return ret, fmt.Errorf("enumerate snapshot MSRs: %w", err)
	}

	msrs, err := getMsrs(v.fd, msrIndices)
	if err != nil {
		return ret, fmt.Errorf("capture MSRs: %w", err)
	}
	ret.Msrs = msrs

	return ret, nil
}

func (v *virtualCPU) restoreSnapshot(snap vcpuSnapshot) error {
	if err := setRegisters(v.fd, &snap.Regs); err != nil {
		return fmt.Errorf("restore general registers: %w", err)
	}

	if err := setSRegs(v.fd, &snap.SRegs); err != nil {
		return fmt.Errorf("restore special registers: %w", err)
	}

	if err := setFPU(v.fd, &snap.FPU); err != nil {
		return fmt.Errorf("restore FPU state: %w", err)
	}

	if snap.LapicPresent {
		if err := setLapic(v.fd, &snap.Lapic); err != nil {
			return fmt.Errorf("restore LAPIC state: %w", err)
		}
	}

	if err := setXsave(v.fd, &snap.Xsave); err != nil {
		return fmt.Errorf("restore XSAVE state: %w", err)
	}

	if err := setXcrs(v.fd, &snap.Xcrs); err != nil {
		return fmt.Errorf("restore XCRs: %w", err)
	}

	if err := setMsrs(v.fd, snap.Msrs); err != nil {
		return fmt.Errorf("restore MSRs: %w", err)
	}

	return nil
}

// CaptureSnapshot implements hv.VirtualMachine.
func (v *virtualMachine) CaptureSnapshot() (hv.Snapshot, error) {
	ret := &snapshot{
		cpuStates:       make(map[int]vcpuSnapshot),
		deviceSnapshots: make(map[string]interface{}),
	}

	// Capture state from each vCPU
	for i := range v.vcpus {
		if err := v.VirtualCPUCall(i, func(vcpu hv.VirtualCPU) error {
			state, err := vcpu.(*virtualCPU).captureSnapshot()
			if err != nil {
				return err
			}

			ret.cpuStates[i] = state

			return nil
		}); err != nil {
			return nil, fmt.Errorf("capture vCPU %d snapshot: %w", i, err)
		}
	}

	if clock, err := getClock(v.vmFd); err != nil {
		if !errors.Is(err, unix.ENOTTY) && !errors.Is(err, unix.EINVAL) {
			return nil, fmt.Errorf("capture clock: %w", err)
		}
	} else {
		ret.clockData = &clock
	}

	// In split IRQ chip mode, only LAPIC is in kernel - PIC/IOAPIC are in userspace.
	// The userspace IOAPIC device saves its state via the device snapshot mechanism.
	if v.hasIRQChip && !v.splitIRQChip {
		var chips []kvmIRQChip
		for _, chipID := range []uint32{irqChipPICMaster, irqChipPICSlave, irqChipIOAPIC} {
			chip, err := getIRQChip(v.vmFd, chipID)
			if err != nil {
				if errors.Is(err, unix.EINVAL) {
					continue
				}
				return nil, fmt.Errorf("capture IRQ chip %d: %w", chipID, err)
			}
			chips = append(chips, chip)
		}
		if len(chips) == 0 {
			return nil, fmt.Errorf("capture IRQ chip: no chip data returned")
		}
		ret.irqChips = chips
	}

	if v.hasPIT {
		pit, err := getPitState(v.vmFd)
		if err != nil {
			return nil, fmt.Errorf("capture PIT state: %w", err)
		}
		ret.pitState = &pit
	}

	// Capture state from each device
	for _, dev := range v.devices {
		if snapshotter, ok := dev.(hv.DeviceSnapshotter); ok {
			id := snapshotter.DeviceId()

			snap, err := snapshotter.CaptureSnapshot()
			if err != nil {
				return nil, fmt.Errorf("capture device %s snapshot: %w", id, err)
			}

			ret.deviceSnapshots[id] = snap
		}
	}

	v.memMu.Lock()
	if len(v.memory) > 0 {
		ret.memory = make([]byte, len(v.memory))
		copy(ret.memory, v.memory)
	}
	v.memMu.Unlock()

	return ret, nil
}

// RestoreSnapshot implements hv.VirtualMachine.
func (v *virtualMachine) RestoreSnapshot(snap hv.Snapshot) error {
	// Type assert to our snapshot type
	snapshotData, ok := snap.(*snapshot)
	if !ok {
		return fmt.Errorf("invalid snapshot type")
	}

	v.memMu.Lock()
	if len(v.memory) != len(snapshotData.memory) {
		v.memMu.Unlock()
		return fmt.Errorf("snapshot memory size mismatch: got %d bytes, want %d bytes",
			len(snapshotData.memory), len(v.memory))
	}
	if len(v.memory) > 0 {
		copy(v.memory, snapshotData.memory)
	}
	v.memMu.Unlock()

	// Restore state to each vCPU
	for i := range v.vcpus {
		state, ok := snapshotData.cpuStates[i]
		if !ok {
			return fmt.Errorf("missing vCPU %d state in snapshot", i)
		}

		if err := v.VirtualCPUCall(i, func(vcpu hv.VirtualCPU) error {
			if err := vcpu.(*virtualCPU).restoreSnapshot(state); err != nil {
				return err
			}

			return nil
		}); err != nil {
			return fmt.Errorf("restore vCPU %d snapshot: %w", i, err)
		}
	}

	if snapshotData.clockData != nil {
		if err := setClock(v.vmFd, snapshotData.clockData); err != nil {
			return fmt.Errorf("restore clock: %w", err)
		}
	}

	// In split IRQ chip mode, PIC/IOAPIC are in userspace and restored via device snapshots.
	// Only restore kernel IRQ chip state if not in split mode.
	if len(snapshotData.irqChips) > 0 {
		if !v.hasIRQChip {
			return fmt.Errorf("snapshot contains IRQ chip state but VM lacks irqchip")
		}
		if v.splitIRQChip {
			return fmt.Errorf("snapshot contains IRQ chip state but VM uses split irqchip mode")
		}
		for _, chip := range snapshotData.irqChips {
			chipCopy := chip
			if err := setIRQChip(v.vmFd, &chipCopy); err != nil {
				return fmt.Errorf("restore IRQ chip %d: %w", chipCopy.ChipID, err)
			}
		}
	} else if v.hasIRQChip && !v.splitIRQChip {
		return fmt.Errorf("snapshot missing IRQ chip state")
	}

	switch {
	case snapshotData.pitState != nil && v.hasPIT:
		if err := setPitState(v.vmFd, snapshotData.pitState); err != nil {
			return fmt.Errorf("restore PIT state: %w", err)
		}
	case snapshotData.pitState == nil && v.hasPIT:
		return fmt.Errorf("snapshot missing PIT state")
	case snapshotData.pitState != nil && !v.hasPIT:
		return fmt.Errorf("snapshot provides PIT state but VM lacks PIT")
	}

	// Restore state to each device
	for _, dev := range v.devices {
		if snapshotter, ok := dev.(hv.DeviceSnapshotter); ok {
			id := snapshotter.DeviceId()

			snapData, ok := snapshotData.deviceSnapshots[id]
			if !ok {
				return fmt.Errorf("missing device %s snapshot", id)
			}

			if err := snapshotter.RestoreSnapshot(snapData); err != nil {
				return fmt.Errorf("restore device %s snapshot: %w", id, err)
			}
		}
	}

	return nil
}
