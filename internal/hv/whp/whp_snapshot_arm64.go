//go:build windows && arm64

package whp

import (
	"fmt"
	"unsafe"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/whp/bindings"
)

// captureArchSnapshot captures ARM64-specific state (GIC).
func (v *virtualMachine) captureArchSnapshot(snap *whpSnapshot) error {
	gicSnap, err := v.captureArm64GICSnapshot()
	if err != nil {
		return fmt.Errorf("capture ARM64 GIC state: %w", err)
	}
	snap.Arm64GICState = gicSnap
	return nil
}

// restoreArchSnapshot restores ARM64-specific state (GIC).
func (v *virtualMachine) restoreArchSnapshot(snap *whpSnapshot) error {
	if err := v.restoreArm64GICSnapshot(snap.Arm64GICState); err != nil {
		return fmt.Errorf("restore ARM64 GIC state: %w", err)
	}
	return nil
}

// arm64GICSnapshot holds the GIC state for ARM64 snapshots.
type arm64GICSnapshot struct {
	// LocalStates holds the per-vCPU GIC redistributor state, keyed by vCPU ID.
	LocalStates map[int]bindings.Arm64LocalInterruptControllerState

	// GlobalState holds the global GIC distributor state (variable size).
	// The header is followed by per-interrupt state.
	GlobalStateHeader bindings.Arm64GlobalInterruptControllerStateHeader
	GlobalInterrupts  []bindings.Arm64GlobalInterruptState

	// AssertedIRQs tracks which IRQs are currently asserted (for re-injection).
	AssertedIRQs map[uint32]bool
}

// captureArm64GICSnapshot captures the GIC state from WHP.
func (v *virtualMachine) captureArm64GICSnapshot() (*arm64GICSnapshot, error) {
	if v.arm64GICInfo.Version == hv.Arm64GICVersionUnknown {
		return nil, nil // No GIC configured
	}

	snap := &arm64GICSnapshot{
		LocalStates:  make(map[int]bindings.Arm64LocalInterruptControllerState),
		AssertedIRQs: make(map[uint32]bool),
	}

	// Capture per-vCPU local GIC state (redistributor)
	for id := range v.vcpus {
		var localState bindings.Arm64LocalInterruptControllerState
		_, err := bindings.GetVirtualProcessorState(
			v.part,
			uint32(id),
			bindings.VirtualProcessorStateTypeInterruptControllerState2,
			unsafe.Pointer(&localState),
			uint32(unsafe.Sizeof(localState)),
		)
		if err != nil {
			return nil, fmt.Errorf("capture vCPU %d GIC local state: %w", id, err)
		}
		snap.LocalStates[id] = localState
	}

	// Capture global GIC distributor state
	// First, get the size needed by querying with a small buffer
	var headerOnly bindings.Arm64GlobalInterruptControllerStateHeader
	_, err := bindings.GetVirtualProcessorState(
		v.part,
		0, // Can use any vCPU index due to ANY_VP flag
		bindings.VirtualProcessorStateTypeGlobalInterruptState,
		unsafe.Pointer(&headerOnly),
		uint32(unsafe.Sizeof(headerOnly)),
	)
	if err != nil {
		return nil, fmt.Errorf("capture GIC global state header: %w", err)
	}

	snap.GlobalStateHeader = headerOnly

	// Now allocate a buffer for the full state and read it
	if headerOnly.NumInterrupts > 0 {
		// Allocate buffer: header + NumInterrupts * sizeof(Arm64GlobalInterruptState)
		interruptStateSize := uint32(unsafe.Sizeof(bindings.Arm64GlobalInterruptState{}))
		totalSize := uint32(unsafe.Sizeof(headerOnly)) + headerOnly.NumInterrupts*interruptStateSize
		buffer := make([]byte, totalSize)

		_, err = bindings.GetVirtualProcessorState(
			v.part,
			0,
			bindings.VirtualProcessorStateTypeGlobalInterruptState,
			unsafe.Pointer(&buffer[0]),
			totalSize,
		)
		if err != nil {
			return nil, fmt.Errorf("capture GIC global state: %w", err)
		}

		// Parse the buffer - header is at the start, interrupts follow
		headerSize := unsafe.Sizeof(headerOnly)
		snap.GlobalInterrupts = make([]bindings.Arm64GlobalInterruptState, headerOnly.NumInterrupts)
		for i := uint32(0); i < headerOnly.NumInterrupts; i++ {
			offset := headerSize + uintptr(i)*unsafe.Sizeof(bindings.Arm64GlobalInterruptState{})
			snap.GlobalInterrupts[i] = *(*bindings.Arm64GlobalInterruptState)(unsafe.Pointer(&buffer[offset]))
		}
	}

	// Copy the asserted IRQ tracking state
	v.arm64GICMu.Lock()
	for irq, asserted := range v.arm64GICAsserted {
		snap.AssertedIRQs[irq] = asserted
	}
	v.arm64GICMu.Unlock()

	return snap, nil
}

// restoreArm64GICSnapshot restores the GIC state to WHP.
func (v *virtualMachine) restoreArm64GICSnapshot(snap *arm64GICSnapshot) error {
	if snap == nil {
		return nil // No GIC state to restore
	}

	// Restore global GIC distributor state first
	if snap.GlobalStateHeader.NumInterrupts > 0 {
		// Build the full buffer: header + interrupts
		interruptStateSize := uint32(unsafe.Sizeof(bindings.Arm64GlobalInterruptState{}))
		headerSize := uint32(unsafe.Sizeof(snap.GlobalStateHeader))
		totalSize := headerSize + snap.GlobalStateHeader.NumInterrupts*interruptStateSize
		buffer := make([]byte, totalSize)

		// Copy header
		*(*bindings.Arm64GlobalInterruptControllerStateHeader)(unsafe.Pointer(&buffer[0])) = snap.GlobalStateHeader

		// Copy interrupts
		for i := uint32(0); i < snap.GlobalStateHeader.NumInterrupts; i++ {
			offset := uintptr(headerSize) + uintptr(i)*unsafe.Sizeof(bindings.Arm64GlobalInterruptState{})
			*(*bindings.Arm64GlobalInterruptState)(unsafe.Pointer(&buffer[offset])) = snap.GlobalInterrupts[i]
		}

		err := bindings.SetVirtualProcessorState(
			v.part,
			0, // Can use any vCPU index due to ANY_VP flag
			bindings.VirtualProcessorStateTypeGlobalInterruptState,
			unsafe.Pointer(&buffer[0]),
			totalSize,
		)
		if err != nil {
			return fmt.Errorf("restore GIC global state: %w", err)
		}
	}

	// Restore per-vCPU local GIC state (redistributor)
	for id := range v.vcpus {
		localState, ok := snap.LocalStates[id]
		if !ok {
			return fmt.Errorf("missing GIC local state for vCPU %d", id)
		}

		err := bindings.SetVirtualProcessorState(
			v.part,
			uint32(id),
			bindings.VirtualProcessorStateTypeInterruptControllerState2,
			unsafe.Pointer(&localState),
			uint32(unsafe.Sizeof(localState)),
		)
		if err != nil {
			return fmt.Errorf("restore vCPU %d GIC local state: %w", id, err)
		}
	}

	// Restore the asserted IRQ tracking state
	v.arm64GICMu.Lock()
	v.arm64GICAsserted = make(map[uint32]bool)
	for irq, asserted := range snap.AssertedIRQs {
		v.arm64GICAsserted[irq] = asserted
	}
	v.arm64GICMu.Unlock()

	return nil
}
