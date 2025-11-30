//go:build windows && amd64

package bindings

import (
	"fmt"
	"unsafe"
)

// RunVPExitReason mirrors WHV_RUN_VP_EXIT_REASON.
type RunVPExitReason uint32

const (
	RunVPExitReasonNone                   RunVPExitReason = 0x00000000
	RunVPExitReasonMemoryAccess           RunVPExitReason = 0x00000001
	RunVPExitReasonX64IoPortAccess        RunVPExitReason = 0x00000002
	RunVPExitReasonUnrecoverableException RunVPExitReason = 0x00000004
	RunVPExitReasonInvalidVpRegisterValue RunVPExitReason = 0x00000005
	RunVPExitReasonUnsupportedFeature     RunVPExitReason = 0x00000006
	RunVPExitReasonX64InterruptWindow     RunVPExitReason = 0x00000007
	RunVPExitReasonX64Halt                RunVPExitReason = 0x00000008
	RunVPExitReasonX64ApicEoi             RunVPExitReason = 0x00000009
	RunVPExitReasonSynicSintDeliverable   RunVPExitReason = 0x0000000A // Added in newer versions

	RunVPExitReasonX64MsrAccess        RunVPExitReason = 0x00001000
	RunVPExitReasonX64Cpuid            RunVPExitReason = 0x00001001
	RunVPExitReasonException           RunVPExitReason = 0x00001002
	RunVPExitReasonX64Rdtsc            RunVPExitReason = 0x00001003
	RunVPExitReasonX64ApicSmiTrap      RunVPExitReason = 0x00001004
	RunVPExitReasonHypercall           RunVPExitReason = 0x00001005
	RunVPExitReasonX64ApicInitSipiTrap RunVPExitReason = 0x00001006
	RunVPExitReasonX64ApicWriteTrap    RunVPExitReason = 0x00001007 // Added in newer versions

	RunVPExitReasonCanceled RunVPExitReason = 0x00002001
)

func (r RunVPExitReason) String() string {
	switch r {
	case RunVPExitReasonNone:
		return "None"
	case RunVPExitReasonMemoryAccess:
		return "MemoryAccess"
	case RunVPExitReasonX64IoPortAccess:
		return "X64IoPortAccess"
	case RunVPExitReasonUnrecoverableException:
		return "UnrecoverableException"
	case RunVPExitReasonInvalidVpRegisterValue:
		return "InvalidVpRegisterValue"
	case RunVPExitReasonUnsupportedFeature:
		return "UnsupportedFeature"
	case RunVPExitReasonX64InterruptWindow:
		return "X64InterruptWindow"
	case RunVPExitReasonX64Halt:
		return "X64Halt"
	case RunVPExitReasonX64ApicEoi:
		return "X64ApicEoi"
	case RunVPExitReasonSynicSintDeliverable:
		return "SynicSintDeliverable"
	case RunVPExitReasonX64MsrAccess:
		return "X64MsrAccess"
	case RunVPExitReasonX64Cpuid:
		return "X64Cpuid"
	case RunVPExitReasonException:
		return "Exception"
	case RunVPExitReasonX64Rdtsc:
		return "X64Rdtsc"
	case RunVPExitReasonX64ApicSmiTrap:
		return "X64ApicSmiTrap"
	case RunVPExitReasonHypercall:
		return "Hypercall"
	case RunVPExitReasonX64ApicInitSipiTrap:
		return "X64ApicInitSipiTrap"
	case RunVPExitReasonX64ApicWriteTrap:
		return "X64ApicWriteTrap"
	case RunVPExitReasonCanceled:
		return "Canceled"
	default:
		return fmt.Sprintf("Unknown(%d)", r)
	}
}

// SegmentRegister mirrors WHV_X64_SEGMENT_REGISTER (16 bytes).
type SegmentRegister struct {
	Base       uint64
	Limit      uint32
	Selector   uint16
	Attributes uint16 // Bitfield
}

// RunVPExitContext mirrors WHV_RUN_VP_EXIT_CONTEXT.
// Size is exactly 224 bytes on AMD64.
type RunVPExitContext struct {
	ExitReason RunVPExitReason
	Reserved   uint32
	VpContext  VPExitContext
	// The C union starts here. Total struct size 224 - 48 (header) = 176 bytes payload.
	unionPayload [176]byte
}

// -------------------------------------------------------------------------
// Union Member Accessors
// -------------------------------------------------------------------------

// MemoryAccessInfo mirrors WHV_MEMORY_ACCESS_INFO.
type MemoryAccessInfo struct {
	AsUINT32 uint32
}

// MemoryAccessContext mirrors WHV_MEMORY_ACCESS_CONTEXT (40 bytes).
type MemoryAccessContext struct {
	InstructionByteCount uint8
	Reserved             [3]uint8
	InstructionBytes     [16]uint8
	AccessInfo           MemoryAccessInfo
	Gpa                  GuestPhysicalAddress
	Gva                  GuestVirtualAddress
}

func (c *RunVPExitContext) MemoryAccess() *MemoryAccessContext {
	return (*MemoryAccessContext)(unsafe.Pointer(&c.unionPayload[0]))
}

// X64IoPortAccessInfo mirrors WHV_X64_IO_PORT_ACCESS_INFO.
type X64IoPortAccessInfo struct {
	AsUINT32 uint32
}

func (c *RunVPExitContext) IoPortAccess() *X64IOPortAccessContext {
	return (*X64IOPortAccessContext)(unsafe.Pointer(&c.unionPayload[0]))
}

func (c *RunVPExitContext) MsrAccess() *X64MsrAccessContext {
	return (*X64MsrAccessContext)(unsafe.Pointer(&c.unionPayload[0]))
}

func (c *RunVPExitContext) CpuidAccess() *X64CpuidAccessContext {
	return (*X64CpuidAccessContext)(unsafe.Pointer(&c.unionPayload[0]))
}

func (c *RunVPExitContext) VpException() *VPExceptionContext {
	return (*VPExceptionContext)(unsafe.Pointer(&c.unionPayload[0]))
}

func (c *RunVPExitContext) InterruptWindow() *X64InterruptionDeliverableContext {
	return (*X64InterruptionDeliverableContext)(unsafe.Pointer(&c.unionPayload[0]))
}

func (c *RunVPExitContext) UnsupportedFeature() *X64UnsupportedFeatureContext {
	return (*X64UnsupportedFeatureContext)(unsafe.Pointer(&c.unionPayload[0]))
}

func (c *RunVPExitContext) CancelReason() *RunVPCanceledContext {
	return (*RunVPCanceledContext)(unsafe.Pointer(&c.unionPayload[0]))
}

func (c *RunVPExitContext) ApicEoi() *X64ApicEoiContext {
	return (*X64ApicEoiContext)(unsafe.Pointer(&c.unionPayload[0]))
}

func (c *RunVPExitContext) ReadTsc() *X64RdtscContext {
	return (*X64RdtscContext)(unsafe.Pointer(&c.unionPayload[0]))
}

func (c *RunVPExitContext) ApicSmi() *X64ApicSmiContext {
	return (*X64ApicSmiContext)(unsafe.Pointer(&c.unionPayload[0]))
}

func (c *RunVPExitContext) Hypercall() *HypercallContext {
	return (*HypercallContext)(unsafe.Pointer(&c.unionPayload[0]))
}

func (c *RunVPExitContext) ApicInitSipi() *X64ApicInitSipiContext {
	return (*X64ApicInitSipiContext)(unsafe.Pointer(&c.unionPayload[0]))
}

// X64ApicWriteContext mirrors WHV_X64_APIC_WRITE_CONTEXT (16 bytes).
type X64ApicWriteContext struct {
	Type       uint32 // WHV_X64_APIC_WRITE_TYPE
	Reserved   uint32
	WriteValue uint64
}

func (c *RunVPExitContext) ApicWrite() *X64ApicWriteContext {
	return (*X64ApicWriteContext)(unsafe.Pointer(&c.unionPayload[0]))
}

// SynicSintDeliverableContext mirrors WHV_SYNIC_SINT_DELIVERABLE_CONTEXT (8 bytes).
type SynicSintDeliverableContext struct {
	DeliverableSints uint16
	Reserved1        uint16
	Reserved2        uint32
}

func (c *RunVPExitContext) SynicSintDeliverable() *SynicSintDeliverableContext {
	return (*SynicSintDeliverableContext)(unsafe.Pointer(&c.unionPayload[0]))
}
