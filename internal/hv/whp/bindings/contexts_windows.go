//go:build windows

package bindings

import (
	"fmt"
	"unsafe"
)

// Uint128 mirrors WHV_UINT128.
// C_ASSERT(sizeof(WHV_UINT128) == 16);
type Uint128 struct {
	Low64  uint64
	High64 uint64
}

// X64FPRegister mirrors WHV_X64_FP_REGISTER.
// C_ASSERT(sizeof(WHV_X64_FP_REGISTER) == 16);
type X64FPRegister struct {
	Mantissa           uint64
	BiasedExponentSign uint64 // Bitfield: BiasedExponent:15, Sign:1, Reserved:48
}

// X64FPControlStatusRegister mirrors WHV_X64_FP_CONTROL_STATUS_REGISTER.
// C_ASSERT(sizeof(WHV_X64_FP_CONTROL_STATUS_REGISTER) == 16);
type X64FPControlStatusRegister struct {
	FpControl uint16
	FpStatus  uint16
	FpTag     uint8
	Reserved  uint8
	LastFpOp  uint16
	// Union handling: Long Mode (Rip) vs 32-bit (Eip/Cs).
	// We map to the largest member (uint64) for memory layout.
	LastFpRip uint64
}

// X64XmmControlStatusRegister mirrors WHV_X64_XMM_CONTROL_STATUS_REGISTER.
// C_ASSERT(sizeof(WHV_X64_XMM_CONTROL_STATUS_REGISTER) == 16);
type X64XmmControlStatusRegister struct {
	LastFpRdp            uint64
	XmmStatusControl     uint32
	XmmStatusControlMask uint32
}

// X64SegmentRegister mirrors WHV_X64_SEGMENT_REGISTER.
// C_ASSERT(sizeof(WHV_X64_SEGMENT_REGISTER) == 16);
type X64SegmentRegister struct {
	Base       uint64
	Limit      uint32
	Selector   uint16
	Attributes uint16 // Bitfield: SegmentType:4, NonSystem:1, DPL:2, Present:1, Reserved:4, Avail:1, Long:1, Default:1, Gran:1
}

// X64TableRegister mirrors WHV_X64_TABLE_REGISTER.
// C_ASSERT(sizeof(WHV_X64_TABLE_REGISTER) == 16);
type X64TableRegister struct {
	Pad   [3]uint16
	Limit uint16
	Base  uint64
}

// X64InterruptStateRegister mirrors WHV_X64_INTERRUPT_STATE_REGISTER.
// C_ASSERT(sizeof(WHV_X64_INTERRUPT_STATE_REGISTER) == 8);
type X64InterruptStateRegister struct {
	AsUINT64 uint64 // Bitfield: InterruptShadow:1, NmiMasked:1, Reserved:62
}

// X64PendingInterruptionRegister mirrors WHV_X64_PENDING_INTERRUPTION_REGISTER.
// C_ASSERT(sizeof(WHV_X64_PENDING_INTERRUPTION_REGISTER) == 8);
type X64PendingInterruptionRegister struct {
	AsUINT64 uint64 // Bitfield: Pending:1, Type:3, ErrorCode:1, Len:4, Nested:1, Reserved:6, Vector:16, ErrorCode:32
}

// X64DeliverabilityNotificationsRegister mirrors WHV_X64_DELIVERABILITY_NOTIFICATIONS_REGISTER.
// C_ASSERT(sizeof(WHV_DELIVERABILITY_NOTIFICATIONS_REGISTER) == 8);
type X64DeliverabilityNotificationsRegister struct {
	AsUINT64 uint64 // Bitfield: Nmi:1, Int:1, Priority:4, Reserved:42, Sint:16
}

// X64PendingEventType mirrors WHV_X64_PENDING_EVENT_TYPE.
type X64PendingEventType uint32

const (
	PendingEventException     X64PendingEventType = 0
	PendingEventExtInt        X64PendingEventType = 5
	PendingEventSvmNestedExit X64PendingEventType = 7
	PendingEventVmxNestedExit X64PendingEventType = 8
)

// X64PendingExceptionEvent mirrors WHV_X64_PENDING_EXCEPTION_EVENT.
// C_ASSERT(sizeof(WHV_X64_PENDING_EXCEPTION_EVENT) == 16);
type X64PendingExceptionEvent struct {
	// Bitfield: Pending:1, Type:3, Res:4, DeliverErr:1, Res:7, Vector:16
	Info               uint32
	ErrorCode          uint32
	ExceptionParameter uint64
}

// X64PendingExtIntEvent mirrors WHV_X64_PENDING_EXT_INT_EVENT.
// C_ASSERT(sizeof(WHV_X64_PENDING_EXT_INT_EVENT) == 16);
type X64PendingExtIntEvent struct {
	// Bitfield: Pending:1, Type:3, Res:4, Vector:8, Res:48
	Info     uint64
	Reserved uint64
}

// InternalActivityRegister mirrors WHV_INTERNAL_ACTIVITY_REGISTER.
// C_ASSERT(sizeof(WHV_INTERNAL_ACTIVITY_REGISTER) == 8);
type InternalActivityRegister struct {
	AsUINT64 uint64 // Bitfield: StartupSuspend:1, HaltSuspend:1, IdleSuspend:1, Reserved:61
}

// X64PendingDebugException mirrors WHV_X64_PENDING_DEBUG_EXCEPTION.
// C_ASSERT(sizeof(WHV_X64_PENDING_DEBUG_EXCEPTION) == 8);
type X64PendingDebugException struct {
	AsUINT64 uint64 // Bitfield: Bp0:1, Bp1:1, Bp2:1, Bp3:1, SingleStep:1, Reserved:59
}

// X64CpuidResult mirrors WHV_X64_CPUID_RESULT.
// C_ASSERT(sizeof(WHV_X64_CPUID_RESULT) == 32);
type X64CpuidResult struct {
	Function uint32
	Reserved [3]uint32
	Eax      uint32
	Ebx      uint32
	Ecx      uint32
	Edx      uint32
}

// RegisterValue mirrors WHV_REGISTER_VALUE.
// C_ASSERT(sizeof(WHV_REGISTER_VALUE) == 16);
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

// X64VPExecutionState mirrors WHV_X64_VP_EXECUTION_STATE.
// C_ASSERT(sizeof(WHV_X64_VP_EXECUTION_STATE) == 2);
type X64VPExecutionState struct {
	AsUINT16 uint16 // Bitfield: Cpl:2, Cr0Pe:1, Cr0Am:1, EferLma:1, DebugActive:1, IntPending:1, Res:5, IntShadow:1, Res:3
}

// VPExitContext mirrors WHV_VP_EXIT_CONTEXT (WHV_X64_VP_EXIT_CONTEXT).
// C_ASSERT(sizeof(WHV_X64_VP_EXIT_CONTEXT) == 40);
type VPExitContext struct {
	ExecutionState       X64VPExecutionState
	InstructionLengthCr8 uint8 // Bitfield: InstructionLength:4, Cr8:4
	Reserved             uint8
	Reserved2            uint32
	Cs                   X64SegmentRegister
	Rip                  uint64
	Rflags               uint64
}

// X64IOPortAccessInfo mirrors WHV_X64_IO_PORT_ACCESS_INFO.
// C_ASSERT(sizeof(WHV_X64_IO_PORT_ACCESS_INFO) == 4);
type X64IOPortAccessInfo struct {
	AsUINT32 uint32 // Bitfield: IsWrite:1, AccessSize:3, StringOp:1, RepPrefix:1, Reserved:26
}

// X64IOPortAccessContext mirrors WHV_X64_IO_PORT_ACCESS_CONTEXT.
// C_ASSERT(sizeof(WHV_X64_IO_PORT_ACCESS_CONTEXT) == 96);
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
// C_ASSERT(sizeof(WHV_X64_MSR_ACCESS_INFO) == 4);
type X64MsrAccessInfo struct {
	AsUINT32 uint32 // Bitfield: IsWrite:1, Reserved:31
}

func (v *X64MsrAccessInfo) IsWrite() bool {
	return (v.AsUINT32 & 0x1) != 0
}

// X64MsrAccessContext mirrors WHV_X64_MSR_ACCESS_CONTEXT.
// C_ASSERT(sizeof(WHV_X64_MSR_ACCESS_CONTEXT) == 24);
type X64MsrAccessContext struct {
	AccessInfo X64MsrAccessInfo
	MsrNumber  uint32
	Rax        uint64
	Rdx        uint64
}

// X64CpuidAccessContext mirrors WHV_X64_CPUID_ACCESS_CONTEXT.
// C_ASSERT(sizeof(WHV_X64_CPUID_ACCESS_CONTEXT) == 64);
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
// C_ASSERT(sizeof(WHV_VP_EXCEPTION_INFO) == 4);
type VPExceptionInfo struct {
	AsUINT32 uint32 // Bitfield: ErrorCodeValid:1, SoftwareException:1, Reserved:30
}

// VPExceptionContext mirrors WHV_VP_EXCEPTION_CONTEXT.
// C_ASSERT(sizeof(WHV_VP_EXCEPTION_CONTEXT) == 40);
type VPExceptionContext struct {
	InstructionByteCount uint8
	Reserved             [3]uint8
	InstructionBytes     [16]uint8
	ExceptionInfo        VPExceptionInfo
	ExceptionType        uint8 // WHV_EXCEPTION_TYPE
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
// C_ASSERT(sizeof(WHV_X64_UNSUPPORTED_FEATURE_CONTEXT) == 16);
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
// C_ASSERT(sizeof(WHV_RUN_VP_CANCELED_CONTEXT) == 4);
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
// C_ASSERT(sizeof(WHV_X64_INTERRUPTION_DELIVERABLE_CONTEXT) == 4);
type X64InterruptionDeliverableContext struct {
	DeliverableType X64PendingInterruptionType
}

// X64ApicEoiContext mirrors WHV_X64_APIC_EOI_CONTEXT.
// C_ASSERT(sizeof(WHV_X64_APIC_EOI_CONTEXT) == 4);
type X64ApicEoiContext struct {
	InterruptVector uint32
}

// X64RdtscInfo mirrors WHV_X64_RDTSC_INFO.
// C_ASSERT(sizeof(WHV_X64_RDTSC_INFO) == 8);
type X64RdtscInfo struct {
	AsUINT64 uint64 // Bitfield: IsRdtscp:1, Reserved:63
}

// X64RdtscContext mirrors WHV_X64_RDTSC_CONTEXT.
// C_ASSERT(sizeof(WHV_X64_RDTSC_CONTEXT) == 40);
type X64RdtscContext struct {
	TscAux        uint64
	VirtualOffset uint64
	Tsc           uint64
	ReferenceTime uint64
	RdtscInfo     X64RdtscInfo
}

// X64ApicSmiContext mirrors WHV_X64_APIC_SMI_CONTEXT.
// C_ASSERT(sizeof(WHV_X64_APIC_SMI_CONTEXT) == 8);
type X64ApicSmiContext struct {
	ApicIcr uint64
}

const HypercallContextMaxXmm = 6

// HypercallContext mirrors WHV_HYPERCALL_CONTEXT.
// C_ASSERT(sizeof(WHV_HYPERCALL_CONTEXT) == 176);
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
// C_ASSERT(sizeof(WHV_X64_APIC_INIT_SIPI_CONTEXT) == 8);
type X64ApicInitSipiContext struct {
	ApicIcr uint64
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

type InterruptControlKind uint64

// Bitfield packing for WHV_INTERRUPT_CONTROL.
// UINT64 Type : 8;             // WHV_INTERRUPT_TYPE
// UINT64 DestinationMode : 4;  // WHV_INTERRUPT_DESTINATION_MODE
// UINT64 TriggerMode : 4;      // WHV_INTERRUPT_TRIGGER_MODE
// UINT64 TargetVtl : 8;        // WHV_VTL (New in 2025 Header)
// UINT64 Reserved : 40;

// MakeInterruptControlKind creates the bitfield for InterruptControl.
// Updated to include vtl (8 bits).
func MakeInterruptControlKind(
	intType InterruptType,
	destMode InterruptDestinationMode,
	trigMode InterruptTriggerMode,
	targetVtl uint8,
) InterruptControlKind {
	return InterruptControlKind(uint64(intType)&0xFF) |
		(InterruptControlKind(uint64(destMode)&0xF) << 8) |
		(InterruptControlKind(uint64(trigMode)&0xF) << 12) |
		(InterruptControlKind(uint64(targetVtl)&0xFF) << 16)
}

// InterruptControl mirrors WHV_INTERRUPT_CONTROL.
// C_ASSERT(sizeof(WHV_INTERRUPT_CONTROL) == 16);
type InterruptControl struct {
	Control     InterruptControlKind
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
// C_ASSERT(sizeof(WHV_DOORBELL_MATCH_DATA) == 24);
type DoorbellMatchData struct {
	GuestAddress GuestPhysicalAddress
	Value        uint64
	Length       uint32
	MatchFlags   DoorbellMatchFlags // Bitfield: MatchOnValue:1, MatchOnLength:1, Reserved:30
}

// SynicEventParameters mirrors WHV_SYNIC_EVENT_PARAMETERS.
// C_ASSERT(sizeof(WHV_SYNIC_EVENT_PARAMETERS) == 8);
type SynicEventParameters struct {
	VpIndex    uint32
	TargetSint uint8
	TargetVtl  uint8
	FlagNumber uint16
}

// TriggerType mirrors WHV_TRIGGER_TYPE.
type TriggerType int32

const (
	TriggerTypeInterrupt       TriggerType = 0
	TriggerTypeSynicEvent      TriggerType = 1
	TriggerTypeDeviceInterrupt TriggerType = 2
)

// TriggerParameters mirrors WHV_TRIGGER_PARAMETERS (New 2025).
// C_ASSERT(sizeof(WHV_TRIGGER_PARAMETERS) == 32);
type TriggerParameters struct {
	TriggerType TriggerType
	Reserved    uint32
	// Union:
	// WHV_INTERRUPT_CONTROL Interrupt; (16 bytes)
	// WHV_SYNIC_EVENT_PARAMETERS SynicEvent; (8 bytes)
	// DeviceInterrupt struct (24 bytes)
	// Max size of union members is 24 bytes.
	// However, C struct size is 32 total. 4 (Enum) + 4 (Reserved) + 24 (Union/Padding).
	Data [24]byte
}

func (t *TriggerParameters) SetInterrupt(ctrl InterruptControl) {
	*t = TriggerParameters{
		TriggerType: TriggerTypeInterrupt,
	}
	*(*InterruptControl)(unsafe.Pointer(&t.Data[0])) = ctrl
}

func (t *TriggerParameters) SetSynicEvent(params SynicEventParameters) {
	*t = TriggerParameters{
		TriggerType: TriggerTypeSynicEvent,
	}
	*(*SynicEventParameters)(unsafe.Pointer(&t.Data[0])) = params
}

// PartitionCounterSet mirrors WHV_PARTITION_COUNTER_SET.
type PartitionCounterSet uint32

const (
	PartitionCounterSetMemory PartitionCounterSet = 0
)

// PartitionMemoryCounters mirrors WHV_PARTITION_MEMORY_COUNTERS.
// C_ASSERT(sizeof(WHV_PARTITION_MEMORY_COUNTERS) == 24);
type PartitionMemoryCounters struct {
	Mapped4KPageCount uint64
	Mapped2MPageCount uint64
	Mapped1GPageCount uint64
}

// ProcessorCounterSet mirrors WHV_PROCESSOR_COUNTER_SET.
type ProcessorCounterSet uint32

const (
	ProcessorCounterSetRuntime           ProcessorCounterSet = 0
	ProcessorCounterSetIntercepts        ProcessorCounterSet = 1
	ProcessorCounterSetEvents            ProcessorCounterSet = 2
	ProcessorCounterSetApic              ProcessorCounterSet = 3
	ProcessorCounterSetSyntheticFeatures ProcessorCounterSet = 4
)

// ProcessorRuntimeCounters mirrors WHV_PROCESSOR_RUNTIME_COUNTERS.
// C_ASSERT(sizeof(WHV_PROCESSOR_RUNTIME_COUNTERS) == 16);
type ProcessorRuntimeCounters struct {
	TotalRuntime100ns      uint64
	HypervisorRuntime100ns uint64
}

// ProcessorInterceptCounter mirrors WHV_PROCESSOR_INTERCEPT_COUNTER.
// C_ASSERT(sizeof(WHV_PROCESSOR_INTERCEPT_COUNTER) == 16);
type ProcessorInterceptCounter struct {
	Count     uint64
	Time100ns uint64
}

// ProcessorInterceptCounters mirrors WHV_PROCESSOR_INTERCEPT_COUNTERS (AMD64).
// C_ASSERT(sizeof(WHV_PROCESSOR_ACTIVITY_COUNTERS) == 224);
type ProcessorInterceptCounters struct {
	PageInvalidations         ProcessorInterceptCounter
	ControlRegisterAccesses   ProcessorInterceptCounter
	IoInstructions            ProcessorInterceptCounter
	HaltInstructions          ProcessorInterceptCounter
	CpuidInstructions         ProcessorInterceptCounter
	MsrAccesses               ProcessorInterceptCounter
	OtherIntercepts           ProcessorInterceptCounter
	PendingInterrupts         ProcessorInterceptCounter
	EmulatedInstructions      ProcessorInterceptCounter
	DebugRegisterAccesses     ProcessorInterceptCounter
	PageFaultIntercepts       ProcessorInterceptCounter
	NestedPageFaultIntercepts ProcessorInterceptCounter // Added to match 2025 header
	Hypercalls                ProcessorInterceptCounter // Added to match 2025 header
	RdpmcInstructions         ProcessorInterceptCounter // Added to match 2025 header
}

// ProcessorEventCounters mirrors WHV_PROCESSOR_EVENT_COUNTERS (WHV_PROCESSOR_GUEST_EVENT_COUNTERS).
// C_ASSERT(sizeof(WHV_PROCESSOR_GUEST_EVENT_COUNTERS) == 24);
type ProcessorEventCounters struct {
	PageFaultCount uint64
	ExceptionCount uint64
	InterruptCount uint64
}

// ProcessorApicCounters mirrors WHV_PROCESSOR_APIC_COUNTERS.
// C_ASSERT(sizeof(WHV_PROCESSOR_APIC_COUNTERS) == 40);
type ProcessorApicCounters struct {
	MmioAccessCount uint64
	EoiAccessCount  uint64
	TprAccessCount  uint64
	SentIpiCount    uint64
	SelfIpiCount    uint64
}

// MemoryAccessType mirrors WHV_MEMORY_ACCESS_TYPE.
type MemoryAccessType uint8

const (
	MemoryAccessRead    MemoryAccessType = 0
	MemoryAccessWrite   MemoryAccessType = 1
	MemoryAccessExecute MemoryAccessType = 2
)
