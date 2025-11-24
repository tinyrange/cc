//go:build windows

package bindings

import (
	"fmt"
	"unsafe"
)

// Uint128 mirrors WHV_UINT128.
type Uint128 struct {
	Low64  uint64
	High64 uint64
}

// X64FPRegister mirrors WHV_X64_FP_REGISTER.
type X64FPRegister struct {
	Mantissa           uint64
	ExponentSignUnused uint64
}

// X64FPControlStatusRegister mirrors WHV_X64_FP_CONTROL_STATUS_REGISTER.
type X64FPControlStatusRegister struct {
	FpControl uint16
	FpStatus  uint16
	FpTag     uint8
	Reserved  uint8
	LastFpOp  uint16
	LastFpRip uint64
}

// X64XmmControlStatusRegister mirrors WHV_X64_XMM_CONTROL_STATUS_REGISTER.
type X64XmmControlStatusRegister struct {
	LastFpRdp            uint64
	XmmStatusControl     uint32
	XmmStatusControlMask uint32
}

// X64SegmentRegister mirrors WHV_X64_SEGMENT_REGISTER.
type X64SegmentRegister struct {
	Base       uint64
	Limit      uint32
	Selector   uint16
	Attributes uint16
}

// X64TableRegister mirrors WHV_X64_TABLE_REGISTER.
type X64TableRegister struct {
	Pad   [3]uint16
	Limit uint16
	Base  uint64
}

// X64InterruptStateRegister mirrors WHV_X64_INTERRUPT_STATE_REGISTER.
type X64InterruptStateRegister struct {
	AsUINT64 uint64
}

// X64PendingInterruptionRegister mirrors WHV_X64_PENDING_INTERRUPTION_REGISTER.
type X64PendingInterruptionRegister struct {
	AsUINT64 uint64
}

// X64DeliverabilityNotificationsRegister mirrors WHV_X64_DELIVERABILITY_NOTIFICATIONS_REGISTER.
type X64DeliverabilityNotificationsRegister struct {
	AsUINT64 uint64
}

// X64PendingEventType mirrors WHV_X64_PENDING_EVENT_TYPE.
type X64PendingEventType uint32

const (
	PendingEventException X64PendingEventType = 0
	PendingEventExtInt    X64PendingEventType = 5
)

// X64PendingExceptionEvent mirrors WHV_X64_PENDING_EXCEPTION_EVENT.
type X64PendingExceptionEvent struct {
	Info               uint32
	ErrorCode          uint32
	ExceptionParameter uint64
}

// X64PendingExtIntEvent mirrors WHV_X64_PENDING_EXT_INT_EVENT.
type X64PendingExtIntEvent struct {
	Info     uint64
	Reserved uint64
}

// InternalActivityRegister mirrors WHV_INTERNAL_ACTIVITY_REGISTER.
type InternalActivityRegister struct {
	AsUINT64 uint64
}

// X64PendingDebugException mirrors WHV_X64_PENDING_DEBUG_EXCEPTION.
type X64PendingDebugException struct {
	AsUINT64 uint64
}

// X64CpuidResult mirrors WHV_X64_CPUID_RESULT.
type X64CpuidResult struct {
	Function uint32
	Reserved [3]uint32
	Eax      uint32
	Ebx      uint32
	Ecx      uint32
	Edx      uint32
}

// RegisterValue mirrors WHV_REGISTER_VALUE.
type RegisterValue struct {
	Raw Uint128
}

func (v RegisterValue) String() string {
	return fmt.Sprintf("{Raw: {Low64: %#x, High64: %#x}}", v.Raw.Low64, v.Raw.High64)
}

// SetUint64 sets the union to a 64-bit register.
func (v *RegisterValue) SetUint64(val uint64) {
	*v = RegisterValue{}
	*(*uint64)(unsafe.Pointer(v)) = val
}

// AsUint128 interprets the union as Uint128.
func (v *RegisterValue) AsUint128() *Uint128 {
	return (*Uint128)(unsafe.Pointer(v))
}

// AsUint64 interprets the union as a 64-bit register.
func (v *RegisterValue) AsUint64() *uint64 {
	return (*uint64)(unsafe.Pointer(v))
}

// AsUint32 interprets the union as a 32-bit register.
func (v *RegisterValue) AsUint32() *uint32 {
	return (*uint32)(unsafe.Pointer(v))
}

// AsSegment interprets the union as a segment register.
func (v *RegisterValue) AsSegment() *X64SegmentRegister {
	return (*X64SegmentRegister)(unsafe.Pointer(v))
}

// AsTable interprets the union as a table register.
func (v *RegisterValue) AsTable() *X64TableRegister {
	return (*X64TableRegister)(unsafe.Pointer(v))
}

// AsInterruptState interprets the union as an interrupt state register.
func (v *RegisterValue) AsInterruptState() *X64InterruptStateRegister {
	return (*X64InterruptStateRegister)(unsafe.Pointer(v))
}

func Uint64RegisterValue(val uint64) RegisterValue {
	var rv RegisterValue
	rv.SetUint64(val)
	return rv
}

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

// X64VPExecutionState mirrors WHV_X64_VP_EXECUTION_STATE.
type X64VPExecutionState struct {
	AsUINT16 uint16
}

// VPExitContext mirrors WHV_VP_EXIT_CONTEXT.
type VPExitContext struct {
	ExecutionState       X64VPExecutionState
	InstructionLengthCr8 uint8
	Reserved             uint8
	Reserved2            uint32
	Cs                   X64SegmentRegister
	Rip                  uint64
	Rflags               uint64
}

// MemoryAccessType mirrors WHV_MEMORY_ACCESS_TYPE.
type MemoryAccessType uint32

const (
	MemoryAccessRead    MemoryAccessType = 0
	MemoryAccessWrite   MemoryAccessType = 1
	MemoryAccessExecute MemoryAccessType = 2
)

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

// X64IOPortAccessInfo mirrors WHV_X64_IO_PORT_ACCESS_INFO.
type X64IOPortAccessInfo struct {
	AsUINT32 uint32
}

// X64IOPortAccessContext mirrors WHV_X64_IO_PORT_ACCESS_CONTEXT.
type X64IOPortAccessContext struct {
	InstructionByteCount uint8
	Reserved             [3]uint8
	InstructionBytes     [16]uint8
	AccessInfo           X64IOPortAccessInfo
	Port                 uint16
	Reserved2            [3]uint16
	Rax                  uint64
	Rcx                  uint64
	Rsi                  uint64
	Rdi                  uint64
	Ds                   X64SegmentRegister
	Es                   X64SegmentRegister
}

// X64MsrAccessInfo mirrors WHV_X64_MSR_ACCESS_INFO.
type X64MsrAccessInfo struct {
	AsUINT32 uint32
}

// X64MsrAccessContext mirrors WHV_X64_MSR_ACCESS_CONTEXT.
type X64MsrAccessContext struct {
	AccessInfo X64MsrAccessInfo
	MsrNumber  uint32
	Rax        uint64
	Rdx        uint64
}

// X64CpuidAccessContext mirrors WHV_X64_CPUID_ACCESS_CONTEXT.
type X64CpuidAccessContext struct {
	Rax              uint64
	Rcx              uint64
	Rdx              uint64
	Rbx              uint64
	DefaultResultRax uint64
	DefaultResultRcx uint64
	DefaultResultRdx uint64
	DefaultResultRbx uint64
}

// VPExceptionInfo mirrors WHV_VP_EXCEPTION_INFO.
type VPExceptionInfo struct {
	AsUINT32 uint32
}

// VPExceptionContext mirrors WHV_VP_EXCEPTION_CONTEXT.
type VPExceptionContext struct {
	InstructionByteCount uint8
	Reserved             [3]uint8
	InstructionBytes     [16]uint8
	ExceptionInfo        VPExceptionInfo
	ExceptionType        uint8
	Reserved2            [3]uint8
	ErrorCode            uint32
	ExceptionParameter   uint64
}

// X64UnsupportedFeatureCode mirrors WHV_X64_UNSUPPORTED_FEATURE_CODE.
type X64UnsupportedFeatureCode uint32

const (
	UnsupportedFeatureIntercept     X64UnsupportedFeatureCode = 1
	UnsupportedFeatureTaskSwitchTss X64UnsupportedFeatureCode = 2
)

// X64UnsupportedFeatureContext mirrors WHV_X64_UNSUPPORTED_FEATURE_CONTEXT.
type X64UnsupportedFeatureContext struct {
	FeatureCode      X64UnsupportedFeatureCode
	Reserved         uint32
	FeatureParameter uint64
}

// RunVPCancelReason mirrors WHV_RUN_VP_CANCEL_REASON.
type RunVPCancelReason uint32

const (
	RunVPCancelReasonUser RunVPCancelReason = 0
)

// RunVPCanceledContext mirrors WHV_RUN_VP_CANCELED_CONTEXT.
type RunVPCanceledContext struct {
	CancelReason RunVPCancelReason
}

// X64PendingInterruptionType mirrors WHV_X64_PENDING_INTERRUPTION_TYPE.
type X64PendingInterruptionType uint32

const (
	PendingInterruptionInterrupt X64PendingInterruptionType = 0
	PendingInterruptionNmi       X64PendingInterruptionType = 2
	PendingInterruptionException X64PendingInterruptionType = 3
)

// X64InterruptionDeliverableContext mirrors WHV_X64_INTERRUPTION_DELIVERABLE_CONTEXT.
type X64InterruptionDeliverableContext struct {
	DeliverableType X64PendingInterruptionType
}

// X64ApicEoiContext mirrors WHV_X64_APIC_EOI_CONTEXT.
type X64ApicEoiContext struct {
	InterruptVector uint32
}

// X64RdtscInfo mirrors WHV_X64_RDTSC_INFO.
type X64RdtscInfo struct {
	AsUINT64 uint64
}

// X64RdtscContext mirrors WHV_X64_RDTSC_CONTEXT.
type X64RdtscContext struct {
	TscAux        uint64
	VirtualOffset uint64
	Tsc           uint64
	ReferenceTime uint64
	RdtscInfo     X64RdtscInfo
}

// X64ApicSmiContext mirrors WHV_X64_APIC_SMI_CONTEXT.
type X64ApicSmiContext struct {
	ApicIcr uint64
}

const HypercallContextMaxXmm = 6

// HypercallContext mirrors WHV_HYPERCALL_CONTEXT.
type HypercallContext struct {
	Rax          uint64
	Rbx          uint64
	Rcx          uint64
	Rdx          uint64
	R8           uint64
	Rsi          uint64
	Rdi          uint64
	Reserved0    uint64
	XmmRegisters [HypercallContextMaxXmm]Uint128
	Reserved1    [2]uint64
}

// X64ApicInitSipiContext mirrors WHV_X64_APIC_INIT_SIPI_CONTEXT.
type X64ApicInitSipiContext struct {
	ApicIcr uint64
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

// InterruptType mirrors WHV_INTERRUPT_TYPE.
type InterruptType uint32

const (
	InterruptTypeFixed          InterruptType = 0
	InterruptTypeLowestPriority InterruptType = 1
	InterruptTypeNmi            InterruptType = 4
	InterruptTypeInit           InterruptType = 5
	InterruptTypeSipi           InterruptType = 6
	InterruptTypeLocalInt1      InterruptType = 9
)

// InterruptDestinationMode mirrors WHV_INTERRUPT_DESTINATION_MODE.
type InterruptDestinationMode uint32

const (
	InterruptDestinationPhysical InterruptDestinationMode = 0
	InterruptDestinationLogical  InterruptDestinationMode = 1
)

// InterruptTriggerMode mirrors WHV_INTERRUPT_TRIGGER_MODE.
type InterruptTriggerMode uint32

const (
	InterruptTriggerEdge  InterruptTriggerMode = 0
	InterruptTriggerLevel InterruptTriggerMode = 1
)

// InterruptControl mirrors WHV_INTERRUPT_CONTROL.
type InterruptControl struct {
	Control     uint64
	Destination uint32
	Vector      uint32
}

// DoorbellMatchFlags controls how doorbell events are matched.
type DoorbellMatchFlags uint32

const (
	DoorbellMatchFlagValue  DoorbellMatchFlags = 1 << 0
	DoorbellMatchFlagLength DoorbellMatchFlags = 1 << 1
)

// DoorbellMatchData mirrors WHV_DOORBELL_MATCH_DATA.
type DoorbellMatchData struct {
	GuestAddress GuestPhysicalAddress
	Value        uint64
	Length       uint32
	MatchFlags   DoorbellMatchFlags
}

// PartitionCounterSet mirrors WHV_PARTITION_COUNTER_SET.
type PartitionCounterSet uint32

const (
	PartitionCounterSetMemory PartitionCounterSet = 0
)

// PartitionMemoryCounters mirrors WHV_PARTITION_MEMORY_COUNTERS.
type PartitionMemoryCounters struct {
	Mapped4KPageCount uint64
	Mapped2MPageCount uint64
	Mapped1GPageCount uint64
}

// ProcessorCounterSet mirrors WHV_PROCESSOR_COUNTER_SET.
type ProcessorCounterSet uint32

const (
	ProcessorCounterSetRuntime    ProcessorCounterSet = 0
	ProcessorCounterSetIntercepts ProcessorCounterSet = 1
	ProcessorCounterSetEvents     ProcessorCounterSet = 2
	ProcessorCounterSetApic       ProcessorCounterSet = 3
)

// ProcessorRuntimeCounters mirrors WHV_PROCESSOR_RUNTIME_COUNTERS.
type ProcessorRuntimeCounters struct {
	TotalRuntime100ns      uint64
	HypervisorRuntime100ns uint64
}

// ProcessorInterceptCounter mirrors WHV_PROCESSOR_INTERCEPT_COUNTER.
type ProcessorInterceptCounter struct {
	Count     uint64
	Time100ns uint64
}

// ProcessorInterceptCounters mirrors WHV_PROCESSOR_INTERCEPT_COUNTERS.
type ProcessorInterceptCounters struct {
	PageInvalidations       ProcessorInterceptCounter
	ControlRegisterAccesses ProcessorInterceptCounter
	IoInstructions          ProcessorInterceptCounter
	HaltInstructions        ProcessorInterceptCounter
	CpuidInstructions       ProcessorInterceptCounter
	MsrAccesses             ProcessorInterceptCounter
	OtherIntercepts         ProcessorInterceptCounter
	PendingInterrupts       ProcessorInterceptCounter
	EmulatedInstructions    ProcessorInterceptCounter
	DebugRegisterAccesses   ProcessorInterceptCounter
	PageFaultIntercepts     ProcessorInterceptCounter
}

// ProcessorEventCounters mirrors WHV_PROCESSOR_EVENT_COUNTERS.
type ProcessorEventCounters struct {
	PageFaultCount uint64
	ExceptionCount uint64
	InterruptCount uint64
}

// ProcessorApicCounters mirrors WHV_PROCESSOR_APIC_COUNTERS.
type ProcessorApicCounters struct {
	MmioAccessCount uint64
	EoiAccessCount  uint64
	TprAccessCount  uint64
	SentIpiCount    uint64
	SelfIpiCount    uint64
}
