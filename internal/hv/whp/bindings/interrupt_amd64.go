//go:build windows && amd64

package bindings

// InterruptType mirrors WHV_INTERRUPT_TYPE for x86/x64.
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

// Bitfield packing for WHV_INTERRUPT_CONTROL (x86/x64).
// UINT64 Type : 8;             // WHV_INTERRUPT_TYPE
// UINT64 DestinationMode : 4;  // WHV_INTERRUPT_DESTINATION_MODE
// UINT64 TriggerMode : 4;      // WHV_INTERRUPT_TRIGGER_MODE
// UINT64 TargetVtl : 8;        // WHV_VTL (New in 2025 Header)
// UINT64 Reserved : 40;
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

// InterruptControl mirrors WHV_INTERRUPT_CONTROL (16 bytes) for x86/x64.
type InterruptControl struct {
	Control     InterruptControlKind
	Destination uint32
	Vector      uint32
}
