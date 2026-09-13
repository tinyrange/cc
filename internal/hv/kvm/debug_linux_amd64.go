//go:build linux && amd64

package kvm

import "unsafe"

// SetBreakpoint installs one debugger-owned execution breakpoint. A zero
// address disables it. It does not modify guest code or guest debug registers.
func (v *VM) SetBreakpoint(address uint64) error {
	var debug struct {
		Control, Padding uint32
		Registers        [8]uint64
	}
	if address != 0 {
		debug.Control = 0x20001 // ENABLE | USE_HW_BP
		debug.Registers[0] = address
		debug.Registers[7] = 0x601 // fixed DR7 bits and local breakpoint 0
	}
	_, err := ioctlWithRetry(uintptr(v.vcpus[0].fd), 0x4048ae9b, uintptr(unsafe.Pointer(&debug)))
	return err
}
