//go:build windows && arm64

package bindings

import (
	"fmt"
	"unsafe"
)

// Arm64VPExecutionState mirrors WHV_VP_EXECUTION_STATE (Bitfield).
// C Definition: 16-bit union.
type Arm64VPExecutionState uint16

// InterruptMessageHeader mirrors WHV_INTERCEPT_MESSAGE_HEADER.
// Size: 24 bytes.
type InterruptMessageHeader struct {
	VpIndex           uint32
	InstructionLength uint8
	// InterceptAccessType mirrors UINT8 InterceptAccessType.
	// Note: Use uint8/byte here to maintain strict struct alignment (offset 5).
	// Using a uint32 enum type here would break alignment.
	InterceptAccessType MemoryAccessType
	ExecutionState      Arm64VPExecutionState
	Pc                  uint64
	Cpsr                uint64
}

// RunVPExitReason mirrors WHV_RUN_VP_EXIT_REASON.
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
	// Note: This value is specific to ARM64 (0x80000050). AMD64 uses 0x00001005.
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
	default:
		return fmt.Sprintf("Unknown(%d)", r)
	}
}

// RunVPExitContext mirrors WHV_RUN_VP_EXIT_CONTEXT.
// Size: 272 bytes.
type RunVPExitContext struct {
	ExitReason RunVPExitReason
	Reserved   uint32
	Reserved1  uint64 // Specific to ARM64 layout
	payload    [256]byte
}

// Arm64ResetType mirrors WHV_ARM64_RESET_TYPE.
type Arm64ResetType uint32

const (
	Arm64ResetTypePowerOff Arm64ResetType = iota
	Arm64ResetTypeReboot
)

// Arm64ResetContext mirrors WHV_ARM64_RESET_CONTEXT.
// Size: 32 bytes.
type Arm64ResetContext struct {
	Header    InterruptMessageHeader
	ResetType Arm64ResetType
	Reserved  uint32
}

func (r *RunVPExitContext) Arm64Reset() *Arm64ResetContext {
	return (*Arm64ResetContext)(unsafe.Pointer(&r.payload[0]))
}

// MemoryAccessInfo mirrors WHV_MEMORY_ACCESS_INFO.
type MemoryAccessInfo uint8

// MemoryAccessContext mirrors WHV_MEMORY_ACCESS_CONTEXT.
// Size: 64 bytes.
type MemoryAccessContext struct {
	Header               InterruptMessageHeader // Offset 0, Size 24
	Reserved0            uint32                 // Offset 24
	InstructionByteCount uint8                  // Offset 28
	AccessInfo           MemoryAccessInfo       // Offset 29
	Reserved1            uint16                 // Offset 30
	InstructionBytes     [4]uint8               // Offset 32
	Reserved2            uint32                 // Offset 36
	Gva                  uint64                 // Offset 40
	Gpa                  uint64                 // Offset 48
	Syndrome             uint64                 // Offset 56
}

func (r *RunVPExitContext) MemoryAccess() *MemoryAccessContext {
	return (*MemoryAccessContext)(unsafe.Pointer(&r.payload[0]))
}
