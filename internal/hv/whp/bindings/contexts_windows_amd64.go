//go:build windows && amd64

package bindings

import "unsafe"

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
	RunVPExitReasonX64MsrAccess           RunVPExitReason = 0x00001000
	RunVPExitReasonX64Cpuid               RunVPExitReason = 0x00001001
	RunVPExitReasonException              RunVPExitReason = 0x00001002
	RunVPExitReasonX64Rdtsc               RunVPExitReason = 0x00001003
	RunVPExitReasonX64ApicSmiTrap         RunVPExitReason = 0x00001004
	RunVPExitReasonHypercall              RunVPExitReason = 0x00001005
	RunVPExitReasonX64ApicInitSipiTrap    RunVPExitReason = 0x00001006
	RunVPExitReasonCanceled               RunVPExitReason = 0x00002001
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
	case RunVPExitReasonCanceled:
		return "Canceled"
	default:
		return "Unknown"
	}
}

// MemoryAccessInfo mirrors WHV_MEMORY_ACCESS_INFO.
type MemoryAccessInfo struct {
	AsUINT32 uint32
}

// MemoryAccessContext mirrors WHV_MEMORY_ACCESS_CONTEXT.
type MemoryAccessContext struct {
	InstructionByteCount uint8
	Reserved             [3]uint8
	InstructionBytes     [16]uint8
	AccessInfo           MemoryAccessInfo
	Gpa                  GuestPhysicalAddress
	Gva                  GuestVirtualAddress
}

// RunVPExitContext mirrors WHV_RUN_VP_EXIT_CONTEXT.
type RunVPExitContext struct {
	ExitReason RunVPExitReason
	Reserved   uint32
	VpContext  VPExitContext
	payload    [176]byte
}

// MemoryAccess returns the WHV_MEMORY_ACCESS_CONTEXT view of the payload.
func (c *RunVPExitContext) MemoryAccess() *MemoryAccessContext {
	return (*MemoryAccessContext)(unsafe.Pointer(&c.payload[0]))
}

// IoPortAccess returns the WHV_X64_IO_PORT_ACCESS_CONTEXT view of the payload.
func (c *RunVPExitContext) IoPortAccess() *X64IOPortAccessContext {
	return (*X64IOPortAccessContext)(unsafe.Pointer(&c.payload[0]))
}

// MsrAccess returns the WHV_X64_MSR_ACCESS_CONTEXT view of the payload.
func (c *RunVPExitContext) MsrAccess() *X64MsrAccessContext {
	return (*X64MsrAccessContext)(unsafe.Pointer(&c.payload[0]))
}

// CpuidAccess returns the WHV_X64_CPUID_ACCESS_CONTEXT view of the payload.
func (c *RunVPExitContext) CpuidAccess() *X64CpuidAccessContext {
	return (*X64CpuidAccessContext)(unsafe.Pointer(&c.payload[0]))
}

// VpException returns the WHV_VP_EXCEPTION_CONTEXT view of the payload.
func (c *RunVPExitContext) VpException() *VPExceptionContext {
	return (*VPExceptionContext)(unsafe.Pointer(&c.payload[0]))
}

// InterruptWindow returns the WHV_X64_INTERRUPTION_DELIVERABLE_CONTEXT view.
func (c *RunVPExitContext) InterruptWindow() *X64InterruptionDeliverableContext {
	return (*X64InterruptionDeliverableContext)(unsafe.Pointer(&c.payload[0]))
}

// UnsupportedFeature returns the WHV_X64_UNSUPPORTED_FEATURE_CONTEXT view.
func (c *RunVPExitContext) UnsupportedFeature() *X64UnsupportedFeatureContext {
	return (*X64UnsupportedFeatureContext)(unsafe.Pointer(&c.payload[0]))
}

// CancelReason returns the WHV_RUN_VP_CANCELED_CONTEXT view.
func (c *RunVPExitContext) CancelReason() *RunVPCanceledContext {
	return (*RunVPCanceledContext)(unsafe.Pointer(&c.payload[0]))
}

// ApicEoi returns the WHV_X64_APIC_EOI_CONTEXT view.
func (c *RunVPExitContext) ApicEoi() *X64ApicEoiContext {
	return (*X64ApicEoiContext)(unsafe.Pointer(&c.payload[0]))
}

// ReadTsc returns the WHV_X64_RDTSC_CONTEXT view.
func (c *RunVPExitContext) ReadTsc() *X64RdtscContext {
	return (*X64RdtscContext)(unsafe.Pointer(&c.payload[0]))
}

// ApicSmi returns the WHV_X64_APIC_SMI_CONTEXT view.
func (c *RunVPExitContext) ApicSmi() *X64ApicSmiContext {
	return (*X64ApicSmiContext)(unsafe.Pointer(&c.payload[0]))
}

// Hypercall returns the WHV_HYPERCALL_CONTEXT view.
func (c *RunVPExitContext) Hypercall() *HypercallContext {
	return (*HypercallContext)(unsafe.Pointer(&c.payload[0]))
}

// ApicInitSipi returns the WHV_X64_APIC_INIT_SIPI_CONTEXT view.
func (c *RunVPExitContext) ApicInitSipi() *X64ApicInitSipiContext {
	return (*X64ApicInitSipiContext)(unsafe.Pointer(&c.payload[0]))
}
