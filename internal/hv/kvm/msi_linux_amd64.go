//go:build linux && amd64

package kvm

import (
	"fmt"
	"runtime"
	"unsafe"
)

// SetMSI injects a PCI message through the VM IRQ chip, not a vCPU ioctl.
func (v *VM) SetMSI(addr uint64, data uint32) error {
	if v == nil {
		return fmt.Errorf("vm is nil")
	}
	// Linux UAPI struct kvm_msi; flags/devid/padding remain zero.
	msg := [8]uint32{uint32(addr), uint32(addr >> 32), data}
	_, err := ioctl(uintptr(v.vmfd), 0x4020aea5, uintptr(unsafe.Pointer(&msg)))
	runtime.KeepAlive(&msg)
	if err != nil {
		return fmt.Errorf("signal MSI: %w", err)
	}
	return nil
}
