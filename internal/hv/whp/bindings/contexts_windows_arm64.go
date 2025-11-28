//go:build windows && arm64

package bindings

import "unsafe"

type Arm64VPExecutionState uint16

type InterruptMessageHeader struct {
	VpIndex             uint32
	InstructionLength   uint8
	InterceptAccessType MemoryAccessType
	ExecutionState      Arm64VPExecutionState
	Pc                  uint64
	Cpsr                uint64
}

type RunVPExitReason uint32

const (
	WHvRunVpExitReasonNone RunVPExitReason = 0x00000000

	// Standard exits caused by operations of the virtual processor
	WHvRunVpExitReasonUnmappedGpa            RunVPExitReason = 0x80000000
	WHvRunVpExitReasonGpaIntercept           RunVPExitReason = 0x80000001
	WHvRunVpExitReasonUnrecoverableException RunVPExitReason = 0x80000021
	WHvRunVpExitReasonInvalidVpRegisterValue RunVPExitReason = 0x80000020
	WHvRunVpExitReasonUnsupportedFeature     RunVPExitReason = 0x80000022
	WHvRunVpExitReasonSynicSintDeliverable   RunVPExitReason = 0x80000062
	WHvMessageTypeRegisterIntercept          RunVPExitReason = 0x80010006
	WHvRunVpExitReasonArm64Reset             RunVPExitReason = 0x8001000c

	// Additional exits that can be configured through partition properties
	WHvRunVpExitReasonHypercall RunVPExitReason = 0x80000050

	WHvRunVpExitReasonCanceled RunVPExitReason = 0xFFFFFFFF
)

func (r RunVPExitReason) String() string {
	switch r {
	case WHvRunVpExitReasonNone:
		return "None"
	case WHvRunVpExitReasonUnmappedGpa:
		return "UnmappedGpa"
	case WHvRunVpExitReasonGpaIntercept:
		return "GpaIntercept"
	case WHvRunVpExitReasonUnrecoverableException:
		return "UnrecoverableException"
	case WHvRunVpExitReasonInvalidVpRegisterValue:
		return "InvalidVpRegisterValue"
	case WHvRunVpExitReasonUnsupportedFeature:
		return "UnsupportedFeature"
	case WHvRunVpExitReasonSynicSintDeliverable:
		return "SynicSintDeliverable"
	case WHvMessageTypeRegisterIntercept:
		return "MessageTypeRegisterIntercept"
	case WHvRunVpExitReasonArm64Reset:
		return "Arm64Reset"
	case WHvRunVpExitReasonHypercall:
		return "Hypercall"
	case WHvRunVpExitReasonCanceled:
		return "Canceled"
	}
	return "Unknown"
}

// RunVPExitContext mirrors WHV_RUN_VP_EXIT_CONTEXT.
type RunVPExitContext struct {
	ExitReason RunVPExitReason
	Reserved   uint32
	Reserved1  uint64
	payload    [256]byte
}

type Arm64ResetType uint32

const (
	Arm64ResetTypePowerOff Arm64ResetType = iota
	WHvArm64ResetTypeReboot
)

type Arm64ResetContext struct {
	Header    InterruptMessageHeader
	ResetType Arm64ResetType
	Reserved  uint32
}

func (r *RunVPExitContext) Arm64Reset() *Arm64ResetContext {
	return (*Arm64ResetContext)(unsafe.Pointer(&r.payload[0]))
}

type MemoryAccessInfo uint8

type MemoryAccessContext struct {
	// WHV_INTERCEPT_MESSAGE_HEADER Header;
	Header InterruptMessageHeader
	// UINT32 Reserved0;
	Reserved0 uint32
	// UINT8 InstructionByteCount;
	InstructionByteCount uint8
	// WHV_MEMORY_ACCESS_INFO AccessInfo;
	AccessInfo MemoryAccessInfo
	// UINT16 Reserved1;
	Reserved1 uint16
	// UINT8 InstructionBytes[4];
	InstructionBytes [4]uint8
	// UINT32 Reserved2;
	Reserved2 uint32
	// UINT64 Gva;
	Gva uint64
	// UINT64 Gpa;
	Gpa uint64
	// UINT64 Syndrome;
	Syndrome uint64
}

func (r *RunVPExitContext) MemoryAccess() *MemoryAccessContext {
	return (*MemoryAccessContext)(unsafe.Pointer(&r.payload[0]))
}
