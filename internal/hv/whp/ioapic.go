//go:build windows && amd64

package whp

import (
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/whp/bindings"
)

type IOAPIC struct {
	base uint64
	vm   *virtualMachine // Interface to call WHvRequestInterrupt

	mu       sync.Mutex
	idxReg   uint32
	id       uint32
	redirtbl [24]uint64 // Standard 24 pins
}

func NewIOAPIC(base uint64, id uint32, vm *virtualMachine) *IOAPIC {
	return &IOAPIC{
		base: base,
		id:   id,
		vm:   vm,
	}
}

func (d *IOAPIC) MMIORegions() []hv.MMIORegion {
	return []hv.MMIORegion{{Address: d.base, Size: 0x20}} // Standard 32 bytes window
}

func (d *IOAPIC) ReadMMIO(addr uint64, data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(data) != 4 {
		return fmt.Errorf("ioapic: only 32-bit reads supported")
	}

	offset := addr - d.base
	val := uint32(0)

	switch offset {
	case 0x00: // IOREGSEL
		val = d.idxReg
	case 0x10: // IOWIN
		val = d.readRegister(d.idxReg)
	}

	binary.LittleEndian.PutUint32(data, val)
	return nil
}

func (d *IOAPIC) WriteMMIO(addr uint64, data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if len(data) != 4 {
		return fmt.Errorf("ioapic: only 32-bit writes supported")
	}

	offset := addr - d.base
	val := binary.LittleEndian.Uint32(data)

	switch offset {
	case 0x00: // IOREGSEL
		d.idxReg = val & 0xFF
	case 0x10: // IOWIN
		d.writeRegister(d.idxReg, val)
	}
	return nil
}

func (d *IOAPIC) readRegister(idx uint32) uint32 {
	switch idx {
	case 0x00: // ID
		return d.id << 24
	case 0x01: // Version
		// Version 0x11, Max Redir Entry = 23 (24 entries)
		return 0x00170011
	case 0x02: // Arbitration ID
		return d.id << 24
	default:
		if idx >= 0x10 && idx <= 0x3F {
			entryIdx := (idx - 0x10) / 2
			isHigh := (idx % 2) != 0

			entry := d.redirtbl[entryIdx]
			if isHigh {
				return uint32(entry >> 32)
			}
			return uint32(entry & 0xFFFFFFFF)
		}
	}
	return 0
}

func (d *IOAPIC) writeRegister(idx uint32, val uint32) {
	if idx >= 0x10 && idx <= 0x3F {
		entryIdx := (idx - 0x10) / 2
		isHigh := (idx % 2) != 0

		if isHigh {
			d.redirtbl[entryIdx] = (d.redirtbl[entryIdx] & 0x00000000FFFFFFFF) | (uint64(val) << 32)
		} else {
			// Preserves upper 32 bits, writes lower, handles masking if needed
			d.redirtbl[entryIdx] = (d.redirtbl[entryIdx] & 0xFFFFFFFF00000000) | uint64(val)
		}
	}
}

// SetIrq is called by devices (HPET, etc.) to trigger an interrupt
func (d *IOAPIC) SetIrq(irq int, level int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if irq < 0 || irq >= 24 {
		return nil
	}

	entry := d.redirtbl[irq]

	// Mask bit (bit 16) - if 1, interrupt is masked
	if (entry & (1 << 16)) != 0 {
		return nil
	}

	// Trigger logic (Level vs Edge) is simplified here.
	// For Edge (0), we usually fire on 0->1 transition.
	// For Level (1), we fire if level is high.

	// Ideally, you state-track previous level. Assuming Edge for HPET mostly.
	if level == 1 {
		vector := uint32(entry & 0xFF)
		deliveryMode := bindings.InterruptType((entry >> 8) & 0x7)
		destMode := uint32((entry >> 11) & 0x1)
		// triggerMode := (entry >> 15) & 0x1
		dest := uint32((entry >> 56) & 0xFF)

		// Call WHP API
		// Note: LowestPriority (1) is hard to do correctly in userspace without
		// peeking at TPRs. Mapping it to Fixed (0) to the specific Dest often works
		// if the OS targets a specific CPU.
		if deliveryMode == bindings.InterruptTypeLowestPriority {
			deliveryMode = bindings.InterruptTypeFixed
		}

		// Construct WHP Interrupt Control struct
		// This requires mapping your internal hv package to WHvRequestInterrupt
		// Assuming vm.RequestInterrupt(dest, vector, type, destMode)

		return d.vm.RequestInterrupt(dest, vector, deliveryMode, destMode)
	}

	return nil
}

func (d *IOAPIC) Init(vm hv.VirtualMachine) error { return nil }
