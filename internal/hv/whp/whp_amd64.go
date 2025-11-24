//go:build windows && amd64

package whp

import (
	"fmt"
	"unsafe"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/whp/bindings"
)

// Architecture implements hv.Hypervisor.
func (h *hypervisor) Architecture() hv.CpuArchitecture {
	return hv.ArchitectureX86_64
}

func (h *hypervisor) archVMInit(vm *virtualMachine, config hv.VMConfig) error {
	// Currently, there are no architecture-specific initializations needed for AMD64.
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

var (
	_ hv.VirtualCPUAmd64 = &virtualCPU{}
)
