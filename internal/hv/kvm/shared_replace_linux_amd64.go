//go:build linux && amd64

package kvm

import (
	"errors"
	"fmt"
	"unsafe"
)

// SharedMemoryRegion is a borrowed mapping. The owner keeps Memory alive until
// all native execution and Close have completed.
type SharedMemoryRegion struct {
	Address uint64
	Memory  []byte
}

// ReplaceSharedMemory replaces a layout made exclusively with MapSharedMemory.
// It must be serialized with vCPU execution. This permits native address-line
// aliases without copying RAM or changing the guest instructions.
func (v *VM) ReplaceSharedMemory(layout []SharedMemoryRegion) error {
	v.lifecycleMu.Lock()
	defer v.lifecycleMu.Unlock()
	if v.kvm == nil || v.memoryClosing || len(v.mem) != 0 {
		return fmt.Errorf("VM cannot replace shared layout")
	}
	for _, r := range v.regions {
		if r.ownsMapping {
			return fmt.Errorf("layout contains owned mappings")
		}
	}
	for i, r := range layout {
		size := uint64(len(r.Memory))
		if size == 0 || (r.Address|size|uint64(uintptr(unsafe.Pointer(&r.Memory[0]))))%4096 != 0 || r.Address > ^uint64(0)-size {
			return fmt.Errorf("invalid shared replacement region %d", i)
		}
		if r.Address < 0xd1000000 && 0xd0000000 < r.Address+size {
			return fmt.Errorf("shared replacement overlaps platform aperture")
		}
		for _, prior := range layout[:i] {
			if rangesOverlapGuestMemory(r.Address, size, prior.Address, uint64(len(prior.Memory))) {
				return fmt.Errorf("overlapping shared replacement regions")
			}
		}
	}
	old := append([]memoryMapping(nil), v.regions...)
	slots := max(len(old), len(layout))
	set := func(slot int, address uint64, memory []byte) error {
		r := kvmUserspaceMemoryRegion{Slot: uint32(slot + 1), GuestPhysAddr: address, MemorySize: uint64(len(memory))}
		if len(memory) != 0 {
			r.UserspaceAddr = uint64(uintptr(unsafe.Pointer(&memory[0])))
		}
		return setUserMemoryRegion(v.vmfd, &r)
	}
	restore := func() error {
		var err error
		for i := 0; i < slots; i++ {
			err = errors.Join(err, set(i, 0, nil))
		}
		for i, r := range old {
			err = errors.Join(err, set(i, r.guestPhysAddr, r.mem))
		}
		return err
	}
	// Remove old slots first: changing a slot's size or address while overlapping
	// another installed slot is rejected by KVM.
	for i := range old {
		if err := set(i, 0, nil); err != nil {
			return errors.Join(err, restore())
		}
	}
	for i, r := range layout {
		if err := set(i, r.Address, r.Memory); err != nil {
			return errors.Join(err, restore())
		}
	}
	v.regions = nil
	for _, r := range layout {
		v.regions = append(v.regions, memoryMapping{guestPhysAddr: r.Address, mem: r.Memory})
	}
	return nil
}
