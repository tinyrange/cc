//go:build linux && amd64

package kvm

import "unsafe"

// SetSingleStep selects debugger-owned instruction stepping. It replaces the
// debugger breakpoint configuration, without editing guest code or DR state.
// The owner must serialize configuration with native execution.
func (v *VM) SetSingleStep(enabled bool) error {
	var debug struct {
		Control, Padding uint32
		Registers        [8]uint64
	}
	if enabled {
		debug.Control = 3
	} // ENABLE | SINGLESTEP
	_, err := ioctlWithRetry(uintptr(v.vcpus[0].fd), 0x4048ae9b, uintptr(unsafe.Pointer(&debug)))
	if err == nil {
		v.debugSingleStep = enabled
	}
	return err
}
