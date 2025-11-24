//go:build windows && !cgo

package bindings

import (
	"syscall"
	"testing"
	"unsafe"
)

// hostAllocation tracks VirtualAlloc-backed buffers that are mapped into a WHP partition.
type hostAllocation struct {
	addr uintptr
	size uintptr
}

func allocateHostPages(t *testing.T, size uintptr, protect uint32) hostAllocation {
	t.Helper()
	if size == 0 || size%PAGE_SIZE != 0 {
		t.Fatalf("allocation size must be a positive multiple of %d bytes, got %d", PAGE_SIZE, size)
	}
	ptr, _, err := procVirtualAlloc.Call(0, size, MEM_COMMIT|MEM_RESERVE, uintptr(protect))
	if ptr == 0 {
		if err == syscall.Errno(0) {
			err = syscall.GetLastError()
		}
		t.Fatalf("VirtualAlloc failed: %v", err)
	}
	return hostAllocation{addr: ptr, size: size}
}

func (h hostAllocation) pointer() unsafe.Pointer {
	return unsafe.Pointer(h.addr)
}

func (h hostAllocation) bytes(offset uintptr, length uintptr) []byte {
	if length == 0 {
		return nil
	}
	if offset+length > h.size {
		panic("hostAllocation bytes request exceeds allocation")
	}
	ptr := unsafe.Add(unsafe.Pointer(h.addr), offset)
	return unsafe.Slice((*byte)(ptr), int(length))
}

func (h hostAllocation) uint64s(offset uintptr, count int) []uint64 {
	if count == 0 {
		return nil
	}
	byteLength := uintptr(count) * unsafe.Sizeof(uint64(0))
	if offset+byteLength > h.size {
		panic("hostAllocation uint64 request exceeds allocation")
	}
	ptr := unsafe.Add(unsafe.Pointer(h.addr), offset)
	return unsafe.Slice((*uint64)(ptr), count)
}

func (h hostAllocation) free(t *testing.T) {
	if h.addr == 0 {
		return
	}
	r1, _, err := procVirtualFree.Call(h.addr, 0, MEM_RELEASE)
	if r1 == 0 && err != syscall.Errno(0) {
		t.Logf("VirtualFree failed: %v", err)
	}
}

type whpResources struct {
	t          *testing.T
	partition  PartitionHandle
	hostAllocs []hostAllocation
	vpCreated  bool
}

func newWHPResources(t *testing.T, partition PartitionHandle) *whpResources {
	r := &whpResources{t: t, partition: partition}
	t.Cleanup(r.cleanup)
	return r
}

func (r *whpResources) addAllocation(a hostAllocation) {
	r.hostAllocs = append(r.hostAllocs, a)
}

func (r *whpResources) markVPCreated() {
	r.vpCreated = true
}

func (r *whpResources) cleanup() {
	if r.vpCreated {
		if err := DeleteVirtualProcessor(r.partition, 0); err != nil {
			r.t.Logf("DeleteVirtualProcessor failed: %v", err)
		}
	}
	if err := DeletePartition(r.partition); err != nil {
		r.t.Logf("DeletePartition failed: %v", err)
	}
	for _, alloc := range r.hostAllocs {
		alloc.free(r.t)
	}
}

func skipIfHypervisorUnavailable(t *testing.T) {
	t.Helper()
	if err := modWinHvPlatform.Load(); err != nil {
		t.Skipf("winhvplatform.dll unavailable: %v", err)
	}
	var present uint32
	_, err := GetCapability(CapabilityCodeHypervisorPresent, unsafe.Pointer(&present), uint32(unsafe.Sizeof(present)))
	if shouldSkipError(err) || present == 0 {
		if err != nil {
			t.Skipf("hypervisor capability not available: %v", err)
		}
		t.Skip("windows hypervisor platform not present")
	}
}

func handleWHPError(t *testing.T, action string, err error) {
	t.Helper()
	if shouldSkipError(err) {
		t.Skipf("%s unavailable: %v", action, err)
	}
	if err != nil {
		t.Fatalf("%s failed: %v", action, err)
	}
}

func createPartitionOrSkip(t *testing.T) (PartitionHandle, *whpResources) {
	t.Helper()
	handle, err := CreatePartition()
	handleWHPError(t, "WHvCreatePartition", err)
	resources := newWHPResources(t, handle)
	return handle, resources
}

func setPartitionPropertyValue[T any](t *testing.T, partition PartitionHandle, code PartitionPropertyCode, value T) {
	t.Helper()
	err := SetPartitionProperty(partition, code, unsafe.Pointer(&value), uint32(unsafe.Sizeof(value)))
	handleWHPError(t, "WHvSetPartitionProperty", err)
}

func processorVendorOrSkip(t *testing.T) ProcessorVendor {
	t.Helper()
	var vendor ProcessorVendor
	_, err := GetCapability(CapabilityCodeProcessorVendor, unsafe.Pointer(&vendor), uint32(unsafe.Sizeof(vendor)))
	handleWHPError(t, "WHvGetCapability(ProcessorVendor)", err)
	return vendor
}

func TestExampleHypercallVmcall(t *testing.T) {
	t.Parallel()
	skipIfHypervisorUnavailable(t)

	partition, resources := createPartitionOrSkip(t)

	const (
		ptePresent = 1 << 0
		pteRW      = 1 << 1
		pteUser    = 1 << 2
		ptePS      = 1 << 7

		cr0PE   = 1 << 0
		cr0PG   = 1 << 31
		cr4PSE  = 1 << 4
		cr4PAE  = 1 << 5
		eferLME = 1 << 8
		eferLMA = 1 << 10
	)

	setPartitionPropertyValue(t, partition, PartitionPropertyCodeProcessorCount, uint32(1))
	setPartitionPropertyValue(t, partition, PartitionPropertyCodeExtendedVmExits, ExtendedVmExitHypercall)

	handleWHPError(t, "WHvSetupPartition", SetupPartition(partition))

	type sampleKernel struct {
		Pml4 [512]uint64
		Pdpt [512]uint64
	}

	kernelSize := unsafe.Sizeof(sampleKernel{})
	kernelAlloc := allocateHostPages(t, kernelSize, PAGE_READWRITE)
	resources.addAllocation(kernelAlloc)
	kernel := (*sampleKernel)(kernelAlloc.pointer())

	const (
		userStart   GuestPhysicalAddress = 4 * 1024
		kernelStart GuestPhysicalAddress = 1 << 30
	)
	kernelBase := uint64(kernelStart)
	pml4Phys := kernelBase + uint64(unsafe.Offsetof(kernel.Pml4))
	// pdptPhys := kernelBase + uint64(unsafe.Offsetof(kernel.Pdpt))

	kernel.Pml4[0] = pml4Phys | (ptePresent | pteRW | pteUser)
	kernel.Pdpt[0] = 0 | (ptePresent | pteRW | pteUser | ptePS)

	handleWHPError(t, "WHvMapGpaRange(kernel)", MapGPARange(
		partition,
		kernelAlloc.pointer(),
		kernelStart,
		uint64(kernelSize),
		MapGPARangeFlagRead|MapGPARangeFlagWrite,
	))

	vendor := processorVendorOrSkip(t)
	userCode := map[ProcessorVendor][]byte{
		ProcessorVendorAmd:   {0x0f, 0x01, 0xd9},
		ProcessorVendorIntel: {0x0f, 0x01, 0xc1},
		ProcessorVendorHygon: {0x0f, 0x01, 0xd9},
	}
	code, ok := userCode[vendor]
	if !ok {
		t.Skipf("unsupported processor vendor %d", vendor)
	}

	userAlloc := allocateHostPages(t, PAGE_SIZE, PAGE_READWRITE)
	resources.addAllocation(userAlloc)
	copy(userAlloc.bytes(0, uintptr(len(code))), code)

	handleWHPError(t, "WHvMapGpaRange(user)", MapGPARange(
		partition,
		userAlloc.pointer(),
		userStart,
		PAGE_SIZE,
		MapGPARangeFlagRead|MapGPARangeFlagExecute,
	))

	handleWHPError(t, "WHvCreateVirtualProcessor", CreateVirtualProcessor(partition, 0, 0))
	resources.markVPCreated()

	names := []RegisterName{
		RegisterCr0,
		RegisterCr3,
		RegisterCr4,
		RegisterEfer,
		RegisterCs,
		RegisterSs,
		RegisterDs,
		RegisterEs,
		RegisterRip,
	}
	values := make([]RegisterValue, len(names))
	*values[0].AsUint64() = cr0PE | cr0PG
	*values[1].AsUint64() = kernelBase + uint64(unsafe.Offsetof(kernel.Pml4))
	*values[2].AsUint64() = cr4PSE | cr4PAE
	*values[3].AsUint64() = eferLME | eferLMA

	codeSegment := values[4].AsSegment()
	codeSegment.Base = 0
	codeSegment.Limit = 0xffff
	codeSegment.Selector = 0x08
	codeSegment.Attributes = 0xa0fb

	dataSegment := values[5].AsSegment()
	dataSegment.Base = 0
	dataSegment.Limit = 0xffff
	dataSegment.Selector = 0x10
	dataSegment.Attributes = 0xc0f3

	*values[6].AsSegment() = *dataSegment
	*values[7].AsSegment() = *dataSegment
	*values[8].AsUint64() = uint64(userStart)

	handleWHPError(t, "WHvSetVirtualProcessorRegisters", SetVirtualProcessorRegisters(partition, 0, names, values))

	var exit RunVPExitContext
	handleWHPError(t, "WHvRunVirtualProcessor", RunVirtualProcessorContext(partition, 0, &exit))
	if exit.ExitReason != RunVPExitReasonHypercall {
		t.Fatalf("expected hypercall exit, got %s", exit.ExitReason)
	}
}

func TestExampleLongModeRegisters(t *testing.T) {
	t.Parallel()
	skipIfHypervisorUnavailable(t)

	partition, resources := createPartitionOrSkip(t)

	setPartitionPropertyValue(t, partition, PartitionPropertyCodeProcessorCount, uint32(1))
	setPartitionPropertyValue(t, partition, PartitionPropertyCodeLocalApicEmulationMode, LocalApicEmulationModeXApic)
	setPartitionPropertyValue(t, partition, PartitionPropertyCodeExtendedVmExits, ExtendedVmExitException)
	exceptionBitmap := uint64(1) << ExceptionTypeBreakpointTrap
	setPartitionPropertyValue(t, partition, PartitionPropertyCodeExceptionExitBitmap, exceptionBitmap)

	handleWHPError(t, "WHvSetupPartition", SetupPartition(partition))
	handleWHPError(t, "WHvCreateVirtualProcessor", CreateVirtualProcessor(partition, 0, 0))
	resources.markVPCreated()

	const (
		pageSize        = PAGE_SIZE
		zeroPageSize    = pageSize
		pageTableCount  = 4
		pageTableSize   = pageTableCount * pageSize
		gdtSize         = pageSize
		codeSize        = pageSize
		pageTableStart  = zeroPageSize
		gdtStart        = pageTableStart + pageTableSize
		codeStart       = gdtStart + gdtSize
		pageEntries     = pageSize / int(unsafe.Sizeof(uint64(0)))
		pageTableFlags  = uint64(0x3)
		pageTableFlagNx = uint64(1) << 63
	)
	const addressSpaceSize = zeroPageSize + pageTableSize + gdtSize + codeSize

	addressSpace := allocateHostPages(t, uintptr(addressSpaceSize), PAGE_READWRITE)
	resources.addAllocation(addressSpace)

	handleWHPError(t, "WHvMapGpaRange(address space)", MapGPARange(
		partition,
		addressSpace.pointer(),
		0,
		uint64(addressSpaceSize),
		MapGPARangeFlagRead|MapGPARangeFlagWrite|MapGPARangeFlagExecute,
	))

	pageTables := addressSpace.uint64s(uintptr(pageTableStart), pageTableSize/8)
	nextPhys := uint64(pageTableStart + pageSize)
	offset := 0
	for range pageTableCount - 1 {
		pageTables[offset] = nextPhys | pageTableFlags
		offset += pageEntries
		nextPhys += pageSize
	}
	pageTable := pageTables[offset : offset+pageEntries]
	entry := pageTableFlags | pageTableFlagNx
	for i := range pageTable {
		pageTable[i] = entry
		entry += pageSize
	}
	pageTable[codeStart/pageSize] &^= pageTableFlagNx

	gdtEntries := addressSpace.uint64s(uintptr(gdtStart), gdtSize/8)
	const (
		gdtNullIndex = 0
		gdtCsIndex   = 1
		csAttributes = uint16(0xa09b)
	)
	gdtEntries[gdtNullIndex] = 0
	gdtEntries[gdtCsIndex] = uint64(csAttributes) << 20

	code := []byte{
		0x48, 0xc7, 0xc0, 'W', 0x00, 0x00, 0x00,
		0x48, 0xc7, 0xc1, 'H', 0x00, 0x00, 0x00,
		0x48, 0xc7, 0xc2, 'v', 0x00, 0x00, 0x00,
		0x48, 0xc7, 0xc3, '6', 0x00, 0x00, 0x00,
		0x49, 0xc7, 0xc0, '4', 0x00, 0x00, 0x00,
		0x49, 0xc7, 0xc1, '!', 0x00, 0x00, 0x00,
		0xcc,
		0xf4,
	}
	copy(addressSpace.bytes(uintptr(codeStart), uintptr(len(code))), code)

	names := []RegisterName{
		RegisterRip,
		RegisterCs,
		RegisterGdtr,
		RegisterCr0,
		RegisterCr3,
		RegisterCr4,
		RegisterEfer,
		RegisterPat,
	}
	values := make([]RegisterValue, len(names))
	*values[0].AsUint64() = codeStart

	cs := values[1].AsSegment()
	cs.Base = 0
	cs.Limit = 0xffffffff
	cs.Selector = uint16(gdtCsIndex * 8)
	cs.Attributes = csAttributes

	gdt := values[2].AsTable()
	gdt.Limit = uint16((2 * 8) - 1)
	gdt.Base = gdtStart

	const (
		cr0  = 0x80000001
		cr3  = pageTableStart
		cr4  = 0x20
		efer = 0xD00
		pat  = 0x0007040600070406
	)
	*values[3].AsUint64() = cr0
	*values[4].AsUint64() = cr3
	*values[5].AsUint64() = cr4
	*values[6].AsUint64() = efer
	*values[7].AsUint64() = pat

	handleWHPError(t, "WHvSetVirtualProcessorRegisters", SetVirtualProcessorRegisters(partition, 0, names, values))

	const maxRuns = 8
	hitBreakpoint := false
	for i := 0; i < maxRuns; i++ {
		var exit RunVPExitContext
		handleWHPError(t, "WHvRunVirtualProcessor", RunVirtualProcessorContext(partition, 0, &exit))
		if exit.ExitReason == RunVPExitReasonException {
			if ExceptionType(exit.VpException().ExceptionType) != ExceptionTypeBreakpointTrap {
				t.Fatalf("unexpected exception type %d", exit.VpException().ExceptionType)
			}
			hitBreakpoint = true
			break
		}
	}
	if !hitBreakpoint {
		t.Fatalf("virtual processor never exited on breakpoint trap")
	}

	regNames := []RegisterName{
		RegisterRax,
		RegisterRcx,
		RegisterRdx,
		RegisterRbx,
		RegisterR8,
		RegisterR9,
	}
	regValues := make([]RegisterValue, len(regNames))
	handleWHPError(t, "WHvGetVirtualProcessorRegisters", GetVirtualProcessorRegisters(partition, 0, regNames, regValues))

	msg := make([]byte, 0, len(regValues))
	for _, v := range regValues {
		msg = append(msg, byte(*v.AsUint64()))
	}
	if string(msg) != "WHv64!" {
		t.Fatalf("unexpected message %q", msg)
	}
}

func TestExampleRealModeRegisters(t *testing.T) {
	t.Parallel()
	skipIfHypervisorUnavailable(t)

	partition, resources := createPartitionOrSkip(t)

	setPartitionPropertyValue(t, partition, PartitionPropertyCodeProcessorCount, uint32(1))
	setPartitionPropertyValue(t, partition, PartitionPropertyCodeLocalApicEmulationMode, LocalApicEmulationModeXApic)
	setPartitionPropertyValue(t, partition, PartitionPropertyCodeExtendedVmExits, ExtendedVmExitException)
	exceptionBitmap := uint64(1) << ExceptionTypeBreakpointTrap
	setPartitionPropertyValue(t, partition, PartitionPropertyCodeExceptionExitBitmap, exceptionBitmap)

	handleWHPError(t, "WHvSetupPartition", SetupPartition(partition))
	handleWHPError(t, "WHvCreateVirtualProcessor", CreateVirtualProcessor(partition, 0, 0))
	resources.markVPCreated()

	const (
		codeStart = 0x1000
		codeSize  = PAGE_SIZE
	)

	code := []byte{
		0xb0, 'W',
		0xb1, 'H',
		0xb2, 'v',
		0xb3, '!',
		0xcc,
		0xf4,
	}

	codeAlloc := allocateHostPages(t, codeSize, PAGE_READWRITE)
	resources.addAllocation(codeAlloc)
	copy(codeAlloc.bytes(0, uintptr(len(code))), code)

	handleWHPError(t, "WHvMapGpaRange(code)", MapGPARange(
		partition,
		codeAlloc.pointer(),
		codeStart,
		codeSize,
		MapGPARangeFlagRead|MapGPARangeFlagExecute,
	))

	names := []RegisterName{RegisterCs, RegisterRip}
	values := make([]RegisterValue, len(names))
	cs := values[0].AsSegment()
	cs.Base = codeStart
	cs.Limit = codeSize
	cs.Attributes = 0x9b
	*values[1].AsUint64() = 0

	handleWHPError(t, "WHvSetVirtualProcessorRegisters", SetVirtualProcessorRegisters(partition, 0, names, values))

	const maxRuns = 8
	hitBreakpoint := false
	for i := 0; i < maxRuns; i++ {
		var exit RunVPExitContext
		handleWHPError(t, "WHvRunVirtualProcessor", RunVirtualProcessorContext(partition, 0, &exit))
		if exit.ExitReason == RunVPExitReasonException {
			if ExceptionType(exit.VpException().ExceptionType) != ExceptionTypeBreakpointTrap {
				t.Fatalf("unexpected exception type %d", exit.VpException().ExceptionType)
			}
			hitBreakpoint = true
			break
		}
	}
	if !hitBreakpoint {
		t.Fatalf("virtual processor never exited on breakpoint trap")
	}

	regNames := []RegisterName{RegisterRax, RegisterRcx, RegisterRdx, RegisterRbx}
	regValues := make([]RegisterValue, len(regNames))
	handleWHPError(t, "WHvGetVirtualProcessorRegisters", GetVirtualProcessorRegisters(partition, 0, regNames, regValues))

	msg := make([]byte, 0, len(regValues))
	for _, v := range regValues {
		msg = append(msg, byte(*v.AsUint64()))
	}
	if string(msg) != "WHv!" {
		t.Fatalf("unexpected message %q", msg)
	}
}
