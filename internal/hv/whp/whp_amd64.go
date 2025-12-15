//go:build windows && amd64

package whp

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"unsafe"

	"github.com/tinyrange/cc/internal/devices/amd64/chipset"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/whp/bindings"
)

const (
	ioapicBaseAddress        = 0xFEC00000
	hpetBaseAddress          = 0xFED00000
	hpetAlternateBaseAddress = 0xFED80000
)

// Architecture implements hv.Hypervisor.
func (h *hypervisor) Architecture() hv.CpuArchitecture {
	return hv.ArchitectureX86_64
}

// Run implements hv.VirtualCPU.
func (v *virtualCPU) Run(ctx context.Context) error {
	var exit bindings.RunVPExitContext

	if ctx.Done() != nil {
		stop := context.AfterFunc(ctx, func() {
			// Best-effort request to break out of WHP run loop.
			_ = bindings.CancelRunVirtualProcessor(v.vm.part, uint32(v.id), 0)
		})
		defer stop()
	}

	if err := bindings.RunVirtualProcessorContext(v.vm.part, uint32(v.id), &exit); err != nil {
		return fmt.Errorf("whp: RunVirtualProcessorContext failed: %w", err)
	}

	// slog.Info(
	// 	"vCPU exited",
	// 	"reason", exit.ExitReason.String(),
	// 	"rip", fmt.Sprintf("0x%x", exit.VpContext.Rip),
	// )

	switch exit.ExitReason {
	case bindings.RunVPExitReasonX64Halt:
		return hv.ErrVMHalted
	case bindings.RunVPExitReasonMemoryAccess:
		mem := exit.MemoryAccess()

		// IMPORTANT: Use heap-allocated status to work around a Go runtime issue on Windows
		// where stack-allocated variables initialized to zero don't get written by WHP APIs.
		status := new(bindings.EmulatorStatus)

		// WORKAROUND: See comment in I/O emulation case below for explanation
		fmt.Fprintf(io.Discard, "%p", status)

		v.pendingError = nil

		// Attempt to emulate the instruction that caused the memory access exit.
		// The emulator will call the registered callbacks (MemoryCallback, TranslateGva, etc.)
		// to fetch instructions and perform the memory operation.
		if err := bindings.EmulatorTryMmioEmulation(
			v.vm.emu,
			unsafe.Pointer(v),
			&exit.VpContext,
			mem,
			status,
		); err != nil {
			return fmt.Errorf("EmulatorTryMmioEmulation failed: %w", err)
		}

		// Check if an error occurred inside a callback (e.g. MMIO write failure)
		if v.pendingError != nil {
			return v.pendingError
		}

		// If the emulator returns success but the status indicates failure (0),
		// it usually means it couldn't fetch the instruction or the instruction
		// didn't match the exit reason.
		if !status.EmulationSuccessful() {
			// We return a detailed error to help debugging, including the RIP and GPA
			rip := exit.VpContext.Rip
			gpa := mem.Gpa
			return fmt.Errorf("whp: emulation failed (Status=%v) at RIP=0x%x accessing GPA=0x%x.", *status, rip, gpa)
		}

		return nil
	case bindings.RunVPExitReasonX64IoPortAccess:
		ioCtx := exit.IoPortAccess()

		// IMPORTANT: Use heap-allocated status to work around a Go runtime issue on Windows
		// where stack-allocated variables initialized to zero don't get written by WHP APIs.
		status := new(bindings.EmulatorStatus)

		// WORKAROUND: On Windows, there's a bug where WHP emulation callbacks don't work correctly
		// unless fmt's pointer formatting is invoked on the status pointer before the API call.
		// The exact root cause is unknown, but this triggers some initialization in the Go runtime
		// or fmt package that makes the syscall callbacks work correctly. Without this, the
		// EmulatorStatus remains 0 (None) even when the callback succeeds.
		// This writes nothing visible to stdout (io.Discard) but is necessary for correctness.
		fmt.Fprintf(io.Discard, "%p", status)

		v.pendingError = nil
		if err := bindings.EmulatorTryIoEmulation(
			v.vm.emu,
			unsafe.Pointer(v),
			&exit.VpContext,
			ioCtx,
			status,
		); err != nil {
			return fmt.Errorf("EmulatorTryIoEmulation failed: %w", err)
		}

		if v.pendingError != nil {
			return v.pendingError
		}

		if !status.EmulationSuccessful() {
			return fmt.Errorf("whp: io emulation failed with status %v at port 0x%x", *status, ioCtx.Port)
		}

		return nil
	case bindings.RunVPExitReasonX64Cpuid:
		info := exit.CpuidAccess()

		return v.handleCpuid(exit.VpContext, info)
	case bindings.RunVPExitReasonX64MsrAccess:
		info := exit.MsrAccess()

		return v.handleMsr(exit.VpContext, info)
	case bindings.RunVPExitReasonX64ApicEoi:
		if v.vm != nil && v.vm.ioapic != nil {
			if ctx := exit.ApicEoi(); ctx != nil {
				v.vm.ioapic.HandleEOI(ctx.InterruptVector)
			}
		}
		return nil
	case bindings.RunVPExitReasonCanceled:
		if err := ctx.Err(); err != nil {
			return err
		}
		return fmt.Errorf("whp: virtual processor canceled without context error")
	case bindings.RunVPExitReasonUnrecoverableException:
		return fmt.Errorf("whp: unrecoverable exception in guest")
	default:
		return fmt.Errorf("whp: unsupported vCPU exit reason %s", exit.ExitReason)
	}
}

func (vm *virtualCPU) handleCpuid(ctx bindings.VPExitContext, info *bindings.X64CpuidAccessContext) error {
	slog.Debug(
		"CPUID access",
		"rax", fmt.Sprintf("0x%X", info.Rax),
		"rbx", fmt.Sprintf("0x%X", info.Rbx),
		"rcx", fmt.Sprintf("0x%X", info.Rcx),
		"rdx", fmt.Sprintf("0x%X", info.Rdx),
	)

	if err := vm.SetRegisters(map[hv.Register]hv.RegisterValue{
		hv.RegisterAMD64Rax: hv.Register64(info.DefaultResultRax),
		hv.RegisterAMD64Rbx: hv.Register64(info.DefaultResultRbx),
		hv.RegisterAMD64Rcx: hv.Register64(info.DefaultResultRcx),
		hv.RegisterAMD64Rdx: hv.Register64(info.DefaultResultRdx),
		hv.RegisterAMD64Rip: hv.Register64(ctx.Rip + uint64(2)),
	}); err != nil {
		return fmt.Errorf("handleCpuid: set registers: %w", err)
	}

	return nil
}

func (vm *virtualCPU) handleMsr(ctx bindings.VPExitContext, info *bindings.X64MsrAccessContext) error {
	if info.AccessInfo.IsWrite() {
		slog.Debug(
			"MSR write",
			"msr", fmt.Sprintf("0x%X", info.MsrNumber),
			"rax", fmt.Sprintf("0x%X", info.Rax),
			"rdx", fmt.Sprintf("0x%X", info.Rdx),
		)

		// do nothing and increment RIP by 2

		if err := vm.SetRegisters(map[hv.Register]hv.RegisterValue{
			hv.RegisterAMD64Rip: hv.Register64(ctx.Rip + uint64(2)),
		}); err != nil {
			return fmt.Errorf("handleMsr: set registers: %w", err)
		}

		return nil
	} else {
		slog.Debug(
			"MSR read",
			"msr", fmt.Sprintf("0x%X", info.MsrNumber),
		)

		// set rax and rdx to 0 and increment RIP by 2
		if err := vm.SetRegisters(map[hv.Register]hv.RegisterValue{
			hv.RegisterAMD64Rax: hv.Register64(0),
			hv.RegisterAMD64Rdx: hv.Register64(0),
			hv.RegisterAMD64Rip: hv.Register64(ctx.Rip + uint64(2)),
		}); err != nil {
			return fmt.Errorf("handleMsr: set registers: %w", err)
		}

		return nil
	}
}

func createEmulator() (bindings.EmulatorHandle, error) {
	ioFn := func(ctx unsafe.Pointer, access *bindings.EmulatorIOAccessInfo) bindings.HRESULT {
		vcpu := (*virtualCPU)(ctx)

		if err := vcpu.handleIOPortAccess(access); err != nil {
			vcpu.pendingError = err
			return bindings.HRESULTFail
		}

		return 0
	}

	memFn := func(
		ctx unsafe.Pointer,
		access *bindings.EmulatorMemoryAccessInfo,
	) bindings.HRESULT {
		vcpu := (*virtualCPU)(ctx)

		if err := vcpu.handleMemoryAccess(access); err != nil {
			vcpu.pendingError = err
		}

		return 0
	}

	getFn := func(
		ctx unsafe.Pointer,
		names []bindings.RegisterName,
		values []bindings.RegisterValue,
	) bindings.HRESULT {
		vcpu := (*virtualCPU)(ctx)

		// Note: We use the backing store (bindings.GetVirtualProcessorRegisters) to populate the emulator.
		if err := bindings.GetVirtualProcessorRegisters(vcpu.vm.part, uint32(vcpu.id), names, values); err != nil {
			// Panic here is risky but acceptable if we can't get registers the emulator critically needs.
			panic(fmt.Sprintf("GetVirtualProcessorRegisters failed: %v", err))
		}

		return 0
	}

	setFn := func(
		ctx unsafe.Pointer,
		names []bindings.RegisterName,
		values []bindings.RegisterValue,
	) bindings.HRESULT {
		vcpu := (*virtualCPU)(ctx)

		if err := bindings.SetVirtualProcessorRegisters(vcpu.vm.part, uint32(vcpu.id), names, values); err != nil {
			panic(fmt.Sprintf("SetVirtualProcessorRegisters failed: %v", err))
		}

		return 0
	}

	translateFn := func(
		ctx unsafe.Pointer,
		gva bindings.GuestVirtualAddress,
		flags bindings.TranslateGVAFlags,
		result *bindings.TranslateGVAResultCode,
		gpa *bindings.GuestPhysicalAddress,
	) bindings.HRESULT {
		vcpu := (*virtualCPU)(ctx)

		var tResult bindings.TranslateGVAResult
		var tGPA bindings.GuestPhysicalAddress

		// Ensure we pass flags correctly, specifically allowing override if needed by the bindings.
		if err := bindings.TranslateGVA(
			vcpu.vm.part,
			uint32(vcpu.id),
			gva,
			flags,
			&tResult,
			&tGPA,
		); err != nil {
			// Panic is too strong here, but bindings panic if we return Fail usually.
			// Ideally we log this.
			panic(fmt.Sprintf("TranslateGvaPage failed: %v", err))
		}

		*result = tResult.ResultCode
		*gpa = tGPA

		return 0
	}

	emu, err := bindings.EmulatorCreate(&bindings.EmulatorCallbacks{
		IoPortCallback:               bindings.NewEmulatorIoPortCallback(ioFn),
		MemoryCallback:               bindings.NewEmulatorMemoryCallback(memFn),
		GetVirtualProcessorRegisters: bindings.NewEmulatorGetRegistersCallback(getFn),
		SetVirtualProcessorRegisters: bindings.NewEmulatorSetRegistersCallback(setFn),
		TranslateGvaPage:             bindings.NewEmulatorTranslateGvaCallback(translateFn),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to create emulator: %w", err)
	}

	return emu, nil
}

func (vm *virtualMachine) installACPI() error {
	return nil
	// return acpi.Install(vm, acpi.Config{
	// 	MemoryBase: vm.memoryBase,
	// 	MemorySize: uint64(vm.memory.Size()),
	// 	HPET: &acpi.HPETConfig{
	// 		Address: hpetBaseAddress,
	// 	},
	// })
}

func (h *hypervisor) archVMInit(vm *virtualMachine, config hv.VMConfig) error {
	emu, err := createEmulator()
	if err != nil {
		return fmt.Errorf("failed to create emulator for vm: %w", err)
	}
	vm.emu = emu

	if config.NeedsInterruptSupport() {
		if err := bindings.SetPartitionPropertyUnsafe(
			vm.part,
			bindings.PartitionPropertyCodeLocalApicEmulationMode,
			bindings.LocalApicEmulationModeXApic,
		); err != nil {
			return fmt.Errorf("failed to enable xAPIC mode: %w", err)
		}
	}

	// set the processor features
	features, err := bindings.GetProcessorFeatures()
	if err != nil {
		return fmt.Errorf("failed to get processor features: %w", err)
	}

	xsaveFeatures, err := bindings.GetProcessorXsaveFeatures()
	if err != nil {
		return fmt.Errorf("failed to get processor xsave features: %w", err)
	}

	exits := bindings.ExtendedVmExitX64Msr | bindings.ExtendedVmExitX64Cpuid

	if err := bindings.SetPartitionProperties(vm.part,
		bindings.PartitionProperties{
			ProcessorFeatures:      &features,
			ProcessorXsaveFeatures: &xsaveFeatures,
			ExtendedVmExits:        &exits,
		},
	); err != nil {
		return fmt.Errorf("failed to set processor features: %w", err)
	}

	return nil
}

func (h *hypervisor) archVMInitWithMemory(vm *virtualMachine, config hv.VMConfig) error {
	if config.NeedsInterruptSupport() {
		if err := vm.installACPI(); err != nil {
			return fmt.Errorf("install ACPI tables: %w", err)
		}
		if err := vm.AddDevice(NewHPETDevice(hpetBaseAddress, vm, hpetAlternateBaseAddress)); err != nil {
			return fmt.Errorf("add HPET device: %w", err)
		}
		vm.ioapic = chipset.NewIOAPIC(24)
		vm.ioapic.SetRouting(chipset.IoApicRoutingFunc(func(vector, dest, destMode, deliveryMode uint8, level bool) {
			intType := bindings.InterruptTypeFixed
			switch deliveryMode {
			case 0x1: // Lowest priority
				intType = bindings.InterruptTypeLowestPriority
			case 0x4: // NMI
				intType = bindings.InterruptTypeNmi
			default:
				intType = bindings.InterruptTypeFixed
			}

			destModeKind := bindings.InterruptDestinationPhysical
			if destMode != 0 {
				destModeKind = bindings.InterruptDestinationLogical
			}

			trigger := bindings.InterruptTriggerEdge
			if level {
				trigger = bindings.InterruptTriggerLevel
			}

			if err := vm.RequestInterrupt(uint32(dest), uint32(vector), intType, destModeKind, trigger); err != nil {
				slog.Error("inject IOAPIC interrupt", "vector", vector, "dest", dest, "err", err)
			}
		}))
		if err := vm.AddDevice(vm.ioapic); err != nil {
			return fmt.Errorf("add IOAPIC device: %w", err)
		}
	}

	return nil
}

func (h *hypervisor) archVCPUInit(vm *virtualMachine, vcpu *virtualCPU) error {
	// Currently, there are no architecture-specific initializations needed for AMD64.
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

// helper to pack x86 segment attributes into the uint16 format expected by WHP/hardware
func makeSegmentAttributes(typeVal, s, dpl, p, avl, l, db, g uint16) uint16 {
	return (typeVal & 0xF) |
		((s & 0x1) << 4) |
		((dpl & 0x3) << 5) |
		((p & 0x1) << 7) |
		((avl & 0x1) << 12) | // AVL is bit 12
		((l & 0x1) << 13) | // L (Long Mode) is bit 13
		((db & 0x1) << 14) | // D/B is bit 14
		((g & 0x1) << 15) // G is bit 15
}

// SetLongModeWithSelectors implements hv.VirtualCPUAmd64.
func (v *virtualCPU) SetLongModeWithSelectors(pagingBase uint64, addrSpaceSize int, codeSelector uint16, dataSelector uint16) error {
	mem := v.vm.memory
	memoryBase := v.vm.memoryBase

	// 1. Setup Identity Mapping in Memory (Same logic as KVM)

	// Translate a guest-phys address to an index into mem.Data.
	host := func(gpa uint64) int {
		if gpa < memoryBase {
			panic("GPA below memory base")
		}
		off := gpa - memoryBase
		if off > mem.Size() {
			panic(fmt.Sprintf("GPA 0x%X out of bounds (memory size 0x%X)", gpa, mem.Size()))
		}
		return int(off)
	}

	// All paging structures must be 4KiB aligned GPAs.
	pml4Addr := (memoryBase + pagingBase + 0x0000) &^ 0xFFF
	pdptAddr := (memoryBase + pagingBase + 0x1000) &^ 0xFFF
	pdBase := (memoryBase + pagingBase + 0x2000) &^ 0xFFF // room for 4 PDs

	pml4 := (*[512]uint64)(unsafe.Pointer(uintptr(mem.Pointer()) + uintptr(host(pml4Addr))))[:]
	pdpt := (*[512]uint64)(unsafe.Pointer(uintptr(mem.Pointer()) + uintptr(host(pdptAddr))))[:]

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
		pd := (*[512]uint64)(unsafe.Pointer(uintptr(mem.Pointer()) + uintptr(host(pdAddr))))[:]
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

	// 2. Setup Registers

	names := []bindings.RegisterName{
		bindings.RegisterCr3,
		bindings.RegisterCr4,
		bindings.RegisterCr0,
		bindings.RegisterEfer,
		bindings.RegisterCs,
		bindings.RegisterDs,
		bindings.RegisterEs,
		bindings.RegisterFs,
		bindings.RegisterGs,
		bindings.RegisterSs,
	}

	values := make([]bindings.RegisterValue, len(names))

	// Read current state
	if err := bindings.GetVirtualProcessorRegisters(v.vm.part, uint32(v.id), names, values); err != nil {
		return err
	}

	// Map values to variables for clarity
	// Note: In Go, taking the address of the slice element is safe here because we won't resize 'values'
	vCr3 := values[0].AsUint64()
	vCr4 := values[1].AsUint64()
	vCr0 := values[2].AsUint64()
	vEfer := values[3].AsUint64()

	// Set CR3 to PML4 base
	*vCr3 = pml4Addr

	// Set CR4 (Enable PAE)
	*vCr4 |= cr4_PAE

	// Set CR0 (Enable Paging and Protection)
	*vCr0 |= cr0_PE | cr0_MP | cr0_ET | cr0_NE | cr0_WP | cr0_AM | cr0_PG

	// Set EFER (Long Mode Enable + Active)
	*vEfer |= efer_LME | efer_LMA

	// Setup Segments
	// CS: Code Segment (L=1, D=0)
	// Type: 11 (Code: execute, read, accessed), S: 1, DPL: 0, P: 1, L: 1, D: 0, G: 1
	codeAttrs := makeSegmentAttributes(11, 1, 0, 1, 0, 1, 0, 1)
	cs := values[4].AsSegment()
	cs.Base = 0
	cs.Limit = 0xffffffff
	cs.Selector = codeSelector
	cs.Attributes = codeAttrs

	// Data Segments
	// Type: 3 (Data: read/write, accessed), S: 1, DPL: 0, P: 1, L: 0, D: 1, G: 1
	// Note: L is ignored for data segments, but Db=1 is required for 4GB flat segment (Linux boot proto)
	dataAttrs := makeSegmentAttributes(3, 1, 0, 1, 0, 0, 1, 1)

	// Update DS (Index 5)
	ds := values[5].AsSegment()
	ds.Base = 0
	ds.Limit = 0xffffffff
	ds.Selector = dataSelector
	ds.Attributes = dataAttrs

	// Copy DS to ES, FS, GS, SS
	values[6] = values[5] // ES
	values[7] = values[5] // FS
	values[8] = values[5] // GS
	values[9] = values[5] // SS

	// Write back all modified registers
	return bindings.SetVirtualProcessorRegisters(v.vm.part, uint32(v.id), names, values)
}

// SetProtectedMode implements hv.VirtualCPUAmd64.
func (v *virtualCPU) SetProtectedMode() error {
	panic("unimplemented")
}

func (v *virtualMachine) SetIRQ(irqLine uint32, level bool) error {
	if v.ioapic == nil {
		return fmt.Errorf("ioapic not initialized")
	}
	v.ioapic.SetIRQ(irqLine, level)
	return nil
}

func (v *virtualMachine) RequestInterrupt(dest uint32, vector uint32, intType bindings.InterruptType, destMode bindings.InterruptDestinationMode, trigger bindings.InterruptTriggerMode) error {
	if trigger == 0 {
		trigger = bindings.InterruptTriggerEdge
	}
	return bindings.RequestInterrupt(v.part, &bindings.InterruptControl{
		Control: bindings.MakeInterruptControlKind(
			intType,
			destMode,
			trigger,
			0,
		),
		Destination: dest,
		Vector:      vector,
	})
}

var (
	_ hv.VirtualCPUAmd64     = &virtualCPU{}
	_ hv.VirtualMachineAmd64 = &virtualMachine{}
)
