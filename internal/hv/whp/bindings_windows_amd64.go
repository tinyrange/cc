//go:build windows && amd64

package whp

import (
	"fmt"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	winhvplatform  = syscall.NewLazyDLL("winhvplatform.dll")
	winhvemulation = syscall.NewLazyDLL("winhvemulation.dll")
	kernel32       = syscall.NewLazyDLL("kernel32.dll")

	procWHvGetCapability                = winhvplatform.NewProc("WHvGetCapability")
	procWHvCreatePartition              = winhvplatform.NewProc("WHvCreatePartition")
	procWHvSetupPartition               = winhvplatform.NewProc("WHvSetupPartition")
	procWHvDeletePartition              = winhvplatform.NewProc("WHvDeletePartition")
	procWHvSetPartitionProperty         = winhvplatform.NewProc("WHvSetPartitionProperty")
	procWHvMapGpaRange                  = winhvplatform.NewProc("WHvMapGpaRange")
	procWHvUnmapGpaRange                = winhvplatform.NewProc("WHvUnmapGpaRange")
	procWHvTranslateGva                 = winhvplatform.NewProc("WHvTranslateGva")
	procWHvCreateVirtualProcessor       = winhvplatform.NewProc("WHvCreateVirtualProcessor")
	procWHvDeleteVirtualProcessor       = winhvplatform.NewProc("WHvDeleteVirtualProcessor")
	procWHvRunVirtualProcessor          = winhvplatform.NewProc("WHvRunVirtualProcessor")
	procWHvGetVirtualProcessorRegisters = winhvplatform.NewProc("WHvGetVirtualProcessorRegisters")
	procWHvSetVirtualProcessorRegisters = winhvplatform.NewProc("WHvSetVirtualProcessorRegisters")
	procWHvCancelRunVirtualProcessor    = winhvplatform.NewProc("WHvCancelRunVirtualProcessor")
	procWHvRequestInterrupt             = winhvplatform.NewProc("WHvRequestInterrupt")
	procWHvEmulatorCreateEmulator       = winhvemulation.NewProc("WHvEmulatorCreateEmulator")
	procWHvEmulatorDestroyEmulator      = winhvemulation.NewProc("WHvEmulatorDestroyEmulator")
	procWHvEmulatorTryIoEmulation       = winhvemulation.NewProc("WHvEmulatorTryIoEmulation")
	procWHvEmulatorTryMmioEmulation     = winhvemulation.NewProc("WHvEmulatorTryMmioEmulation")
	procVirtualAlloc                    = kernel32.NewProc("VirtualAlloc")
	procVirtualFree                     = kernel32.NewProc("VirtualFree")
)

type hresult int32

func (hr hresult) Err() error {
	if hr >= 0 {
		return nil
	}
	return fmt.Errorf("HRESULT 0x%08x: %s", uint32(hr), syscall.Errno(hr))
}

func callHRESULT(proc *syscall.LazyProc, args ...uintptr) error {
	r1, _, callErr := proc.Call(args...)
	if callErr != syscall.Errno(0) && r1 == 0 {
		return callErr
	}
	return hresult(int32(r1)).Err()
}

type partitionHandle syscall.Handle
type guestPhysicalAddress uint64
type guestVirtualAddress uint64

type capabilityCode uint32

const capabilityCodeHypervisorPresent capabilityCode = 0

type partitionPropertyCode uint32

const partitionPropertyCodeProcessorCount partitionPropertyCode = 0x00001fff

const partitionPropertyCodeLocalAPICEmulationMode partitionPropertyCode = 0x00001005

type localAPICEmulationMode uint32

const localAPICEmulationModeXAPIC localAPICEmulationMode = 1

type mapGPARangeFlags uint32

const (
	mapGPARangeFlagRead    mapGPARangeFlags = 0x1
	mapGPARangeFlagWrite   mapGPARangeFlags = 0x2
	mapGPARangeFlagExecute mapGPARangeFlags = 0x4
)

type interruptType uint32

const interruptTypeFixed interruptType = 0

type interruptDestinationMode uint32

const interruptDestinationPhysical interruptDestinationMode = 0

type interruptTriggerMode uint32

const interruptTriggerEdge interruptTriggerMode = 0

type interruptControl struct {
	Control     uint64
	Destination uint32
	Vector      uint32
}

type translateGVAFlags uint32
type translateGVAResultCode uint32

const translateGVAResultSuccess translateGVAResultCode = 0

type translateGVAResult struct {
	ResultCode translateGVAResultCode
	Reserved   uint32
}

type registerName uint32

const (
	registerRax    registerName = 0x00000000
	registerRcx    registerName = 0x00000001
	registerRdx    registerName = 0x00000002
	registerRbx    registerName = 0x00000003
	registerRsp    registerName = 0x00000004
	registerRbp    registerName = 0x00000005
	registerRsi    registerName = 0x00000006
	registerRdi    registerName = 0x00000007
	registerR8     registerName = 0x00000008
	registerR9     registerName = 0x00000009
	registerR10    registerName = 0x0000000a
	registerR11    registerName = 0x0000000b
	registerR12    registerName = 0x0000000c
	registerR13    registerName = 0x0000000d
	registerR14    registerName = 0x0000000e
	registerR15    registerName = 0x0000000f
	registerRip    registerName = 0x00000010
	registerRflags registerName = 0x00000011
	registerEs     registerName = 0x00000012
	registerCs     registerName = 0x00000013
	registerSs     registerName = 0x00000014
	registerDs     registerName = 0x00000015
	registerFs     registerName = 0x00000016
	registerGs     registerName = 0x00000017
	registerCr0    registerName = 0x0000001c
	registerCr3    registerName = 0x0000001e
	registerCr4    registerName = 0x0000001f
	registerEfer   registerName = 0x00002001
)

type uint128 struct {
	Low64  uint64
	High64 uint64
}

type registerValue struct {
	raw uint128
}

func uint64RegisterValue(v uint64) registerValue {
	var out registerValue
	*(*uint64)(unsafe.Pointer(&out)) = v
	return out
}

func segmentRegisterValue(v x64SegmentRegister) registerValue {
	var out registerValue
	*(*x64SegmentRegister)(unsafe.Pointer(&out)) = v
	return out
}

func (v *registerValue) uint64() uint64 {
	return *(*uint64)(unsafe.Pointer(v))
}

type x64SegmentRegister struct {
	Base       uint64
	Limit      uint32
	Selector   uint16
	Attributes uint16
}

type x64VPExecutionState struct {
	AsUint16 uint16
}

type vpExitContext struct {
	ExecutionState       x64VPExecutionState
	InstructionLengthCr8 uint8
	Reserved             uint8
	Reserved2            uint32
	Cs                   x64SegmentRegister
	Rip                  uint64
	Rflags               uint64
}

type runVPExitReason uint32

const (
	runVPExitReasonNone                   runVPExitReason = 0x00000000
	runVPExitReasonMemoryAccess           runVPExitReason = 0x00000001
	runVPExitReasonX64IoPortAccess        runVPExitReason = 0x00000002
	runVPExitReasonUnrecoverableException runVPExitReason = 0x00000004
	runVPExitReasonInvalidVpRegisterValue runVPExitReason = 0x00000005
	runVPExitReasonUnsupportedFeature     runVPExitReason = 0x00000006
	runVPExitReasonX64InterruptWindow     runVPExitReason = 0x00000007
	runVPExitReasonX64Halt                runVPExitReason = 0x00000008
	runVPExitReasonX64ApicEoi             runVPExitReason = 0x00000009
	runVPExitReasonX64MsrAccess           runVPExitReason = 0x00001000
	runVPExitReasonX64Cpuid               runVPExitReason = 0x00001001
	runVPExitReasonException              runVPExitReason = 0x00001002
	runVPExitReasonCanceled               runVPExitReason = 0x00002001
)

func (r runVPExitReason) String() string {
	switch r {
	case runVPExitReasonNone:
		return "None"
	case runVPExitReasonMemoryAccess:
		return "MemoryAccess"
	case runVPExitReasonX64IoPortAccess:
		return "X64IoPortAccess"
	case runVPExitReasonUnrecoverableException:
		return "UnrecoverableException"
	case runVPExitReasonInvalidVpRegisterValue:
		return "InvalidVpRegisterValue"
	case runVPExitReasonUnsupportedFeature:
		return "UnsupportedFeature"
	case runVPExitReasonX64InterruptWindow:
		return "X64InterruptWindow"
	case runVPExitReasonX64Halt:
		return "X64Halt"
	case runVPExitReasonX64ApicEoi:
		return "X64ApicEoi"
	case runVPExitReasonX64MsrAccess:
		return "X64MsrAccess"
	case runVPExitReasonX64Cpuid:
		return "X64Cpuid"
	case runVPExitReasonException:
		return "Exception"
	case runVPExitReasonCanceled:
		return "Canceled"
	default:
		return fmt.Sprintf("Unknown(%d)", r)
	}
}

type runVPExitContext struct {
	ExitReason runVPExitReason
	Reserved   uint32
	VpContext  vpExitContext
	Payload    [176]byte
}

type memoryAccessInfo struct {
	AsUint32 uint32
}

func (i memoryAccessInfo) accessType() uint8 {
	return uint8(i.AsUint32 & 0x3)
}

type memoryAccessContext struct {
	InstructionByteCount uint8
	Reserved             [3]uint8
	InstructionBytes     [16]uint8
	AccessInfo           memoryAccessInfo
	GPA                  guestPhysicalAddress
	GVA                  uint64
}

func (c *runVPExitContext) memoryAccess() *memoryAccessContext {
	return (*memoryAccessContext)(unsafe.Pointer(&c.Payload[0]))
}

type x64IOPortAccessInfo struct {
	AsUint32 uint32
}

func (i x64IOPortAccessInfo) isWrite() bool {
	return i.AsUint32&0x1 != 0
}

func (i x64IOPortAccessInfo) accessSize() uint8 {
	return uint8((i.AsUint32 >> 1) & 0x7)
}

type x64IOPortAccessContext struct {
	InstructionByteCount uint8
	Reserved             [3]uint8
	InstructionBytes     [16]uint8
	AccessInfo           x64IOPortAccessInfo
	Port                 uint16
	Reserved2            [3]uint16
	Rax                  uint64
	Rcx                  uint64
	Rsi                  uint64
	Rdi                  uint64
	Ds                   x64SegmentRegister
	Es                   x64SegmentRegister
}

func (c *runVPExitContext) ioPortAccess() *x64IOPortAccessContext {
	return (*x64IOPortAccessContext)(unsafe.Pointer(&c.Payload[0]))
}

type x64ApicEoiContext struct {
	InterruptVector uint32
}

func (c *runVPExitContext) apicEoi() *x64ApicEoiContext {
	return (*x64ApicEoiContext)(unsafe.Pointer(&c.Payload[0]))
}

type allocation struct {
	addr uintptr
	size uintptr
}

func virtualAlloc(size uintptr) (*allocation, error) {
	const (
		memCommit            = 0x1000
		memReserve           = 0x2000
		pageExecuteReadWrite = 0x40
	)
	ptr, _, err := procVirtualAlloc.Call(0, size, memCommit|memReserve, pageExecuteReadWrite)
	if ptr == 0 {
		if err == syscall.Errno(0) {
			err = syscall.GetLastError()
		}
		return nil, err
	}
	return &allocation{addr: ptr, size: size}, nil
}

func (a *allocation) bytes() []byte {
	return unsafe.Slice((*byte)(unsafe.Pointer(a.addr)), int(a.size))
}

func (a *allocation) free() error {
	if a == nil || a.addr == 0 {
		return nil
	}
	const memRelease = 0x8000
	r1, _, err := procVirtualFree.Call(a.addr, 0, memRelease)
	if r1 == 0 {
		if err == syscall.Errno(0) {
			err = syscall.GetLastError()
		}
		return err
	}
	a.addr = 0
	a.size = 0
	return nil
}

func isHypervisorPresent() (bool, error) {
	var present uint32
	var written uint32
	err := callHRESULT(
		procWHvGetCapability,
		uintptr(capabilityCodeHypervisorPresent),
		uintptr(unsafe.Pointer(&present)),
		unsafe.Sizeof(present),
		uintptr(unsafe.Pointer(&written)),
	)
	if err != nil {
		return false, err
	}
	if written < uint32(unsafe.Sizeof(present)) {
		return false, fmt.Errorf("WHvGetCapability wrote %d bytes, want at least %d", written, unsafe.Sizeof(present))
	}
	return present != 0, nil
}

func createPartition() (partitionHandle, error) {
	var part partitionHandle
	err := callHRESULT(procWHvCreatePartition, uintptr(unsafe.Pointer(&part)))
	return part, err
}

func setPartitionProperty[T any](part partitionHandle, code partitionPropertyCode, value T) error {
	return callHRESULT(
		procWHvSetPartitionProperty,
		uintptr(part),
		uintptr(code),
		uintptr(unsafe.Pointer(&value)),
		unsafe.Sizeof(value),
	)
}

func setupPartition(part partitionHandle) error {
	return callHRESULT(procWHvSetupPartition, uintptr(part))
}

func deletePartition(part partitionHandle) error {
	return callHRESULT(procWHvDeletePartition, uintptr(part))
}

func mapGPARange(part partitionHandle, source unsafe.Pointer, guestAddress uint64, size uint64, flags mapGPARangeFlags) error {
	return callHRESULT(
		procWHvMapGpaRange,
		uintptr(part),
		uintptr(source),
		uintptr(guestPhysicalAddress(guestAddress)),
		uintptr(size),
		uintptr(flags),
	)
}

func unmapGPARange(part partitionHandle, guestAddress uint64, size uint64) error {
	return callHRESULT(procWHvUnmapGpaRange, uintptr(part), uintptr(guestPhysicalAddress(guestAddress)), uintptr(size))
}

func translateGVA(part partitionHandle, vpIndex uint32, gva guestVirtualAddress, flags translateGVAFlags, result *translateGVAResult, gpa *guestPhysicalAddress) error {
	return callHRESULT(
		procWHvTranslateGva,
		uintptr(part),
		uintptr(vpIndex),
		uintptr(gva),
		uintptr(flags),
		uintptr(unsafe.Pointer(result)),
		uintptr(unsafe.Pointer(gpa)),
	)
}

func createVirtualProcessor(part partitionHandle, vpIndex uint32) error {
	return callHRESULT(procWHvCreateVirtualProcessor, uintptr(part), uintptr(vpIndex), 0)
}

func deleteVirtualProcessor(part partitionHandle, vpIndex uint32) error {
	return callHRESULT(procWHvDeleteVirtualProcessor, uintptr(part), uintptr(vpIndex))
}

func runVirtualProcessor(part partitionHandle, vpIndex uint32, exit *runVPExitContext) error {
	return callHRESULT(
		procWHvRunVirtualProcessor,
		uintptr(part),
		uintptr(vpIndex),
		uintptr(unsafe.Pointer(exit)),
		unsafe.Sizeof(*exit),
	)
}

func getVirtualProcessorRegisters(part partitionHandle, vpIndex uint32, names []registerName, values []registerValue) error {
	if len(values) < len(names) {
		return fmt.Errorf("register values slice too small")
	}
	err := callHRESULT(
		procWHvGetVirtualProcessorRegisters,
		uintptr(part),
		uintptr(vpIndex),
		uintptr(unsafe.Pointer(&names[0])),
		uintptr(len(names)),
		uintptr(unsafe.Pointer(&values[0])),
	)
	runtime.KeepAlive(names)
	runtime.KeepAlive(values)
	return err
}

func setVirtualProcessorRegisters(part partitionHandle, vpIndex uint32, names []registerName, values []registerValue) error {
	if len(values) < len(names) {
		return fmt.Errorf("register values slice too small")
	}
	err := callHRESULT(
		procWHvSetVirtualProcessorRegisters,
		uintptr(part),
		uintptr(vpIndex),
		uintptr(unsafe.Pointer(&names[0])),
		uintptr(len(names)),
		uintptr(unsafe.Pointer(&values[0])),
	)
	runtime.KeepAlive(names)
	runtime.KeepAlive(values)
	return err
}

func cancelRunVirtualProcessor(part partitionHandle, vpIndex uint32) error {
	return callHRESULT(procWHvCancelRunVirtualProcessor, uintptr(part), uintptr(vpIndex), 0)
}

func requestInterrupt(part partitionHandle, vector uint32) error {
	control := interruptControl{
		Control:     uint64(interruptTypeFixed) | uint64(interruptDestinationPhysical)<<8 | uint64(interruptTriggerEdge)<<12,
		Destination: 0,
		Vector:      vector,
	}
	return callHRESULT(
		procWHvRequestInterrupt,
		uintptr(part),
		uintptr(unsafe.Pointer(&control)),
		unsafe.Sizeof(control),
	)
}
