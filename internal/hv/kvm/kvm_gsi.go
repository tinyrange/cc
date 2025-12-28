//go:build linux && amd64

package kvm

import (
	"fmt"
	"unsafe"

	"github.com/tinyrange/cc/internal/debug"
	"golang.org/x/sys/unix"
)

// initGSIRouting installs a simple IOAPIC routing table for GSIs [0,numGSIs).
// This mirrors what QEMU does for in-kernel irqchip: each GSI is mapped to the
// in-kernel IOAPIC with the same pin number.
func initGSIRouting(vmFd int, systemFd int, numGSIs int) error {
	debug.Writef("kvm hypervisor initGSIRouting", "vmFd: %d, systemFd: %d, numGSIs: %d", vmFd, systemFd, numGSIs)

	if numGSIs <= 0 {
		return nil
	}

	if ok, err := checkExtension(systemFd, kvmCapIrqRouting); err != nil {
		return fmt.Errorf("check KVM_CAP_IRQ_ROUTING: %w", err)
	} else if !ok {
		return nil
	}

	entries := make([]kvmIrqRoutingEntry, 0, numGSIs)
	for gsi := 0; gsi < numGSIs; gsi++ {
		entries = append(entries, kvmIrqRoutingEntry{
			GSI:   uint32(gsi),
			Type:  kvmIRQRoutingIoapic,
			Flags: 0,
			u: kvmIrqRoutingIoapic{
				IRQChip: irqChipIOAPIC,
				Pin:     uint32(gsi),
			},
		})
	}

	table := kvmIrqRouting{
		NR:      uint32(len(entries)),
		Flags:   0,
		Entries: entries,
	}

	if err := setIrqRouting(vmFd, &table); err != nil {
		if err == unix.EINVAL || err == unix.ENOTTY {
			// Some KVM builds only allow GSI routing with split irqchip; fall back to defaults.
			return nil
		}
		return fmt.Errorf("set IRQ routing: %w", err)
	}
	return nil
}

// KVM irq routing structures adapted from asm/kvm.h
const (
	kvmIRQRoutingIoapic = 1
)

type kvmIrqRoutingEntry struct {
	GSI   uint32
	Type  uint32
	Flags uint32
	u     kvmIrqRoutingIoapic
	_     [8]byte // pad to match the kernel struct size expectations
}

type kvmIrqRoutingIoapic struct {
	IRQChip uint32
	Pin     uint32
}

type kvmIrqRouting struct {
	NR      uint32
	Flags   uint32
	Entries []kvmIrqRoutingEntry
}

type kvmIrqRoutingHeader struct {
	NR    uint32
	Flags uint32
}

const (
	kvmCapIrqRouting = 25
)

func setIrqRouting(vmFd int, table *kvmIrqRouting) error {
	debug.Writef("kvm hypervisor setIrqRouting", "vmFd: %d, table: %+v", vmFd, table)

	// The KVM_SET_GSI_ROUTING ioctl expects the entries to be inline after the header.
	headerSize := int(unsafe.Sizeof(kvmIrqRoutingHeader{}))
	size := headerSize + len(table.Entries)*int(unsafe.Sizeof(kvmIrqRoutingEntry{}))
	buf := make([]byte, size)

	// Copy header
	header := (*kvmIrqRoutingHeader)(unsafe.Pointer(&buf[0]))
	header.NR = table.NR
	header.Flags = table.Flags

	// Copy entries
	entrySize := int(unsafe.Sizeof(kvmIrqRoutingEntry{}))
	for i, ent := range table.Entries {
		offset := headerSize + i*entrySize
		*(*kvmIrqRoutingEntry)(unsafe.Pointer(&buf[offset])) = ent
	}

	if _, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(vmFd), uintptr(kvmSetGsiRouting), uintptr(unsafe.Pointer(&buf[0]))); e != 0 {
		return e
	}
	return nil
}

func checkExtension(systemFd int, cap int) (bool, error) {
	debug.Writef("kvm hypervisor checkExtension", "systemFd: %d, cap: %d", systemFd, cap)

	ret, _, err := unix.Syscall(unix.SYS_IOCTL, uintptr(systemFd), uintptr(kvmCheckExtension), uintptr(cap))
	if err != 0 {
		return false, err
	}

	debug.Writef("kvm hypervisor checkExtension", "ret: %d", ret)

	return ret != 0, nil
}
