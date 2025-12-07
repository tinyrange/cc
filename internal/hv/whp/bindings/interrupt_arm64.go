//go:build windows && arm64

package bindings

// InterruptType mirrors WHV_INTERRUPT_TYPE for ARM64.
type InterruptType uint32

const (
	InterruptTypeFixed   InterruptType = 0x0000
	InterruptTypeMaximum InterruptType = 0x0008 // exclusive upper bound
)

// Destination/trigger enums are still surfaced in WHP APIs (e.g. GetInterruptTargetVpSet)
// even though ARM64 IRQ injection uses InterruptControl2.
type InterruptDestinationMode uint32

const (
	InterruptDestinationPhysical InterruptDestinationMode = 0
	InterruptDestinationLogical  InterruptDestinationMode = 1
)

type InterruptTriggerMode uint32

const (
	InterruptTriggerEdge  InterruptTriggerMode = 0
	InterruptTriggerLevel InterruptTriggerMode = 1
)

// InterruptControl2 mirrors WHV_INTERRUPT_CONTROL2.
// Bit layout:
// [0..31]   InterruptType
// [32..33]  Reserved1
// [34]      Asserted
// [35]      Retarget
// [36..63]  Reserved2
type InterruptControl2 struct {
	AsUINT64 uint64
}

func MakeInterruptControl2(intType InterruptType, asserted bool, retarget bool) InterruptControl2 {
	const (
		assertedBit = 1 << 34
		retargetBit = 1 << 35
	)
	val := uint64(intType)
	if asserted {
		val |= assertedBit
	}
	if retarget {
		val |= retargetBit
	}
	return InterruptControl2{AsUINT64: val}
}

// InterruptControl mirrors WHV_INTERRUPT_CONTROL (32 bytes) for ARM64.
type InterruptControl struct {
	TargetPartition    GuestPhysicalAddress
	InterruptControl   InterruptControl2
	DestinationAddress GuestPhysicalAddress
	RequestedVector    uint32
	TargetVtl          uint8
	ReservedZ0         uint8
	ReservedZ1         uint16
}
