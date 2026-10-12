//go:build linux && amd64

package kvm

import (
	"fmt"
	"unsafe"
)

// ReadVirtualInto observes the stopped VCPU's current translation context.
// KVM_TRANSLATE supplies page-table semantics; no registers or memory change.
func (v *VM) ReadVirtualInto(address uint64, data []byte) error {
	if len(data) > 65536 || uint64(len(data)) > ^uint64(0)-address {
		return fmt.Errorf("virtual observation must be nonwrapping and at most 64 KiB")
	}
	if len(data) != 0 && (v == nil || v.vcpufd < 0) {
		return fmt.Errorf("virtual observation requires a live stopped VCPU")
	}
	for len(data) != 0 {
		var translation struct {
			Linear, Physical           uint64
			Valid, Writeable, Usermode byte
			Padding                    [5]byte
		}
		translation.Linear = address
		if _, err := ioctlWithRetry(uintptr(v.vcpufd), 0xc018ae85, uintptr(unsafe.Pointer(&translation))); err != nil {
			return err
		}
		if translation.Valid == 0 {
			return fmt.Errorf("virtual address %#x is unmapped", address)
		}
		size := min(len(data), int(4096-address%4096))
		if err := v.ReadIPAInto(translation.Physical, data[:size]); err != nil {
			return err
		}
		address += uint64(size)
		data = data[size:]
	}
	return nil
}
