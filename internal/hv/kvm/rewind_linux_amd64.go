//go:build linux && amd64

package kvm

import (
	"bytes"
	"fmt"
	"math/bits"
	"runtime"
	"sort"
	"unsafe"
)

const (
	kvmLogDirtyPages            = 1
	kvmGetDirtyLog              = 0x4010ae42
	kvmRAMPage                  = 4096
	kvmEnableCap                = 0x4068aea3
	kvmCapManualDirtyLogProtect = 168
)

type rewindSlot struct {
	memory []byte
	bitmap []uint64
	seen   []uint64
}

type rewindRange struct {
	start, end uint64
	zero       bool
}

type rewindPages struct {
	pages  map[uint64]bool
	ranges []rewindRange
}

func (r *rewindPages) add(offset uint64, template []byte) bool {
	if _, exists := r.pages[offset]; exists {
		return false
	}
	var zero [kvmRAMPage]byte
	r.pages[offset] = bytes.Equal(template[offset:offset+kvmRAMPage], zero[:])
	return true
}

func (r *rewindPages) rebuildRanges() {
	offsets := make([]uint64, 0, len(r.pages))
	for offset := range r.pages {
		offsets = append(offsets, offset)
	}
	sort.Slice(offsets, func(i, j int) bool { return offsets[i] < offsets[j] })
	r.ranges = r.ranges[:0]
	for _, offset := range offsets {
		zero := r.pages[offset]
		last := len(r.ranges) - 1
		if last >= 0 && offset == r.ranges[last].end && zero == r.ranges[last].zero {
			r.ranges[last].end += kvmRAMPage
		} else {
			r.ranges = append(r.ranges, rewindRange{start: offset, end: offset + kvmRAMPage, zero: zero})
		}
	}
}

// PrepareRewind captures a direct vCPU and enables logging on its RAM slots.
// The caller must serialize Run, host writes and restore, and exclude device
// activity after capture. Only pages in the supplied private mapping may be
// restored. Host writes do not appear in the guest dirty log.
func (v *VM) PrepareRewind(memory, template []byte) (func([]uint64) (int, error), error) {
	if !v.noPIT || len(v.vcpus) != 1 || len(memory) == 0 || len(memory)%kvmRAMPage != 0 || len(template) != len(memory) {
		return nil, fmt.Errorf("rewind requires a direct single-vCPU VM with page-aligned private RAM")
	}
	// Keep the bitmap cumulative. Reset copies every page ever written, so
	// there is no need to revoke KVM's writable translations every request.
	capability := struct {
		capability, flags uint32
		args              [4]uint64
		padding           [64]byte
	}{capability: kvmCapManualDirtyLogProtect, args: [4]uint64{1}}
	if _, err := ioctl(uintptr(v.vmfd), kvmEnableCap, uintptr(unsafe.Pointer(&capability))); err != nil {
		return nil, fmt.Errorf("enable manual KVM dirty logging: %w", err)
	}
	initial, err := snapshotKVMVCPU(v, 0)
	if err != nil {
		return nil, err
	}
	var irqchips [3]kvmIRQChip
	for index := range irqchips {
		irqchips[index], err = getIRQChip(v.vmfd, uint32(index))
		if err != nil {
			return nil, fmt.Errorf("capture IRQ chip %d: %w", index, err)
		}
	}
	slots := make([]rewindSlot, len(v.regions))
	for index, mapping := range v.regions {
		words := (len(mapping.mem)/kvmRAMPage + 63) / 64
		slots[index] = rewindSlot{mapping.mem, make([]uint64, words), make([]uint64, words)}
		region := kvmUserspaceMemoryRegion{
			Slot: uint32(index + 1), Flags: kvmLogDirtyPages, GuestPhysAddr: mapping.guestPhysAddr,
			MemorySize: uint64(len(mapping.mem)), UserspaceAddr: uint64(uintptr(unsafe.Pointer(&mapping.mem[0]))),
		}
		if err := setUserMemoryRegion(v.vmfd, &region); err != nil {
			return nil, fmt.Errorf("log dirty RAM slot %d: %w", index+1, err)
		}
	}
	dirty := rewindPages{pages: make(map[uint64]bool)}
	return func(hostPages []uint64) (int, error) {
		return v.rewind(initial, irqchips, memory, template, slots, hostPages, &dirty)
	}, nil
}

func (v *VM) rewind(initial kvmSnapshotVCPU, irqchips [3]kvmIRQChip, memory, template []byte, slots []rewindSlot, hostPages []uint64, dirty *rewindPages) (int, error) {
	base := uintptr(unsafe.Pointer(&memory[0]))
	changed := false
	for _, offset := range hostPages {
		if offset%kvmRAMPage != 0 || offset >= uint64(len(memory)) {
			return 0, fmt.Errorf("host write outside the private rewind mapping")
		}
		changed = dirty.add(offset, template) || changed
	}
	for index, slot := range slots {
		log := struct {
			slot, padding uint32
			bitmap        *uint64
		}{slot: uint32(index + 1), bitmap: &slot.bitmap[0]}
		_, err := ioctl(uintptr(v.vmfd), kvmGetDirtyLog, uintptr(unsafe.Pointer(&log)))
		runtime.KeepAlive(log)
		if err != nil {
			return 0, fmt.Errorf("read dirty RAM slot %d: %w", index+1, err)
		}
		for wordIndex, bitmap := range slot.bitmap {
			word := bitmap &^ slot.seen[wordIndex]
			slot.seen[wordIndex] |= bitmap
			for word != 0 {
				page := wordIndex*64 + bits.TrailingZeros64(word)
				offset := uintptr(unsafe.Pointer(&slot.memory[0])) + uintptr(page)*kvmRAMPage - base
				if offset >= uintptr(len(memory)) {
					return 0, fmt.Errorf("guest dirtied memory outside the private rewind mapping")
				}
				changed = dirty.add(uint64(offset), template) || changed
				word &= word - 1
			}
		}
	}
	if changed {
		dirty.rebuildRanges()
	}
	for _, span := range dirty.ranges {
		if span.zero {
			clear(memory[span.start:span.end])
		} else {
			copy(memory[span.start:span.end], template[span.start:span.end])
		}
	}
	for index := range irqchips {
		if err := setIRQChip(v.vmfd, &irqchips[index]); err != nil {
			return 0, fmt.Errorf("restore IRQ chip %d: %w", index, err)
		}
	}
	// Rewinding RAM can restore guest PTEs while keeping their root address.
	// Force a guest translation-context change before restoring that root.
	translation := initial.SRegs
	translation.Cr3 = 0
	if err := setSRegs(v.vcpufd, &translation); err != nil {
		return 0, fmt.Errorf("invalidate guest translations: %w", err)
	}
	if err := restoreKVMVCPU(v, 0, initial); err != nil {
		return 0, err
	}
	return len(dirty.pages), nil
}
