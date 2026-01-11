//go:build windows && arm64

package bindings

// VirtualProcessorStateType constants for ARM64 GIC state.
const (
	// VirtualProcessorStateTypeInterruptControllerState2 is the per-vCPU local GIC state.
	// Buffer type: Arm64LocalInterruptControllerState
	VirtualProcessorStateTypeInterruptControllerState2 VirtualProcessorStateType = 0x00001000

	// VirtualProcessorStateTypeGlobalInterruptState is the global GIC distributor state.
	// This uses the "any VP" flag so can be called on any vCPU index.
	// Buffer type: Arm64GlobalInterruptControllerState (variable size)
	VirtualProcessorStateTypeGlobalInterruptState VirtualProcessorStateType = 0x00000006 | (1 << 30) | (1 << 31)
)

// Arm64InterruptState mirrors WHV_ARM64_INTERRUPT_STATE (3 bytes).
type Arm64InterruptState struct {
	// Flags byte: Enabled:1, EdgeTriggered:1, Asserted:1, SetPending:1, Active:1, Direct:1, Reserved:2
	Flags uint8

	// GicrIpriorityrConfigured is the configured priority.
	GicrIpriorityrConfigured uint8

	// GicrIpriorityrActive is the active priority.
	GicrIpriorityrActive uint8
}

// Arm64LocalInterruptControllerState mirrors WHV_ARM64_LOCAL_INTERRUPT_CONTROLLER_STATE.
// This represents the per-vCPU GIC redistributor state.
type Arm64LocalInterruptControllerState struct {
	Version    uint8
	GicVersion uint8
	Reserved0  [6]uint8

	IccIgrpen1El1     uint64
	GicrCtlrEnableLpis uint64
	IccBpr1El1        uint64
	IccPmrEl1         uint64
	GicrPropbaser     uint64
	GicrPendbaser     uint64
	IchAp1REl2        [4]uint32

	// BankedInterruptState contains state for 32 banked interrupts (SGIs and PPIs).
	BankedInterruptState [32]Arm64InterruptState
}

// Arm64GlobalInterruptState mirrors WHV_ARM64_GLOBAL_INTERRUPT_STATE (4 bytes).
type Arm64GlobalInterruptState struct {
	InterruptState Arm64InterruptState
	Padding        uint8 // Alignment padding
}

// Arm64GlobalInterruptControllerStateHeader is the fixed-size header of the global GIC state.
// The full state is variable size: header + NumInterrupts * Arm64GlobalInterruptState
type Arm64GlobalInterruptControllerStateHeader struct {
	Version       uint8
	GicVersion    uint8
	Reserved0     [2]uint8
	NumInterrupts uint32
	GicdCtlrEnableGrp1A uint64
}

// Arm64GlobalInterruptControllerState version constant.
const Arm64GlobalInterruptControllerStateVersion = 1

// Arm64LocalInterruptControllerState version constant.
const Arm64LocalInterruptControllerStateVersion = 1
