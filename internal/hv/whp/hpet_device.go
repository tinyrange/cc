//go:build windows && amd64

package whp

import (
	"fmt"
	"sync"
	"time"

	"github.com/tinyrange/cc/internal/hv"
)

// InterruptSink defines where the HPET sends its signals (usually the IOAPIC)
type InterruptSink interface {
	SetIRQ(irq uint32, level bool) error
}

const (
	hpetClockPeriodFemtoseconds = 10_000_000 // 10ns
	hpetVendorID                = 0x8086
	hpetNumTimers               = 3 // Windows usually expects at least 3

	hpetRegGenCap       = 0x000
	hpetRegGenConfig    = 0x010
	hpetRegIntStatus    = 0x020
	hpetRegMainCounter  = 0x0F0
	hpetTimerConfig     = 0x100
	hpetTimerComparator = 0x108
	hpetTimerFsRoute    = 0x110
	hpetTimerStep       = 0x20 // Offset between timers
)

type hpetTimer struct {
	config     uint64
	comparator uint64
	fsRoute    uint64
}

const (
	hpetMMIOWindowSize = 0x400
)

type hpetDevice struct {
	bases []uint64
	sink  InterruptSink

	mu            sync.Mutex
	generalConfig uint64
	intStatus     uint64
	counter       uint64
	lastUpdate    time.Time
	enabled       bool

	timers [hpetNumTimers]hpetTimer
}

func NewHPETDevice(base uint64, sink InterruptSink, aliases ...uint64) *hpetDevice {
	bases := make([]uint64, 0, 1+len(aliases))
	seen := make(map[uint64]struct{}, 1+len(aliases))
	addBase := func(addr uint64) {
		if addr == 0 {
			return
		}
		if _, ok := seen[addr]; ok {
			return
		}
		seen[addr] = struct{}{}
		bases = append(bases, addr)
	}

	addBase(base)
	for _, alias := range aliases {
		addBase(alias)
	}

	return &hpetDevice{
		bases:      bases,
		sink:       sink,
		lastUpdate: time.Now(),
	}
}

func (d *hpetDevice) MMIORegions() []hv.MMIORegion {
	regions := make([]hv.MMIORegion, 0, len(d.bases))
	for _, base := range d.bases {
		regions = append(regions, hv.MMIORegion{Address: base, Size: hpetMMIOWindowSize})
	}
	return regions
}

func (d *hpetDevice) offsetFor(addr uint64) (uint64, error) {
	for _, base := range d.bases {
		if addr >= base && addr < base+hpetMMIOWindowSize {
			return addr - base, nil
		}
	}
	return 0, fmt.Errorf("hpet: address 0x%x outside configured MMIO windows", addr)
}

// ReadMMIO handles standard reads. Windows relies heavily on the Main Counter read.
func (d *hpetDevice) ReadMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	// Update counter before read to ensure freshness
	d.updateCounterLocked()

	offset, err := d.offsetFor(addr)
	if err != nil {
		return err
	}
	val := uint64(0)

	switch {
	case offset == hpetRegGenCap:
		// 10ns period, Vendor ID, 64-bit counter capable, 3 timers
		val = uint64(hpetClockPeriodFemtoseconds)<<32 | uint64(hpetVendorID)<<16 | uint64(1)<<13 | (hpetNumTimers - 1)
	case offset == hpetRegGenConfig:
		val = d.generalConfig
	case offset == hpetRegIntStatus:
		val = d.intStatus
	case offset == hpetRegMainCounter:
		val = d.counter
	case offset >= hpetTimerConfig:
		timerIdx := (offset - 0x100) / 0x20
		if timerIdx >= hpetNumTimers {
			return nil
		}
		reg := (offset - 0x100) % 0x20
		t := &d.timers[timerIdx]
		switch reg {
		case 0x00: // Config
			val = t.config
		case 0x08: // Comparator
			val = t.comparator
		case 0x10: // FSB Route
			val = t.fsRoute
		}
	}

	// Helper to write uint64 into data slice respecting size
	if len(data) > 8 {
		return fmt.Errorf("hpet: invalid read size %d", len(data))
	}
	// Copy the lower bytes of val into data
	for i := 0; i < len(data); i++ {
		data[i] = byte(val >> (i * 8))
	}

	return nil
}

func (d *hpetDevice) WriteMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	offset, err := d.offsetFor(addr)
	if err != nil {
		return err
	}

	// Convert data to uint64 for easier handling
	var val uint64
	for i := 0; i < len(data) && i < 8; i++ {
		val |= uint64(data[i]) << (i * 8)
	}

	switch {
	case offset == hpetRegGenConfig:
		d.updateCounterLocked()
		// Only bits 0 (Enable) and 1 (Legacy Replacement) are typically writable
		d.generalConfig = val & 0x3
		newEnabled := (d.generalConfig & 1) == 1

		if newEnabled && !d.enabled {
			d.lastUpdate = time.Now()
		}
		d.enabled = newEnabled

	case offset == hpetRegGenCap:
		// Read-only

	case offset == hpetRegIntStatus:
		// Write-1-to-clear
		d.intStatus &= ^val

	case offset == hpetRegMainCounter:
		d.counter = val
		if d.enabled {
			d.lastUpdate = time.Now()
		}

	case offset >= hpetTimerConfig:
		timerIdx := (offset - 0x100) / 0x20
		if timerIdx >= hpetNumTimers {
			return nil
		}
		reg := (offset - 0x100) % 0x20
		t := &d.timers[timerIdx]

		switch reg {
		case 0x00: // Timer Config & Capabilities
			// Preserve read-only bits (interrupt routing caps)
			// For emulation, we simplify and allow basic config
			t.config = val
		case 0x08: // Comparator
			t.comparator = val
			// If setting comparator, check if we need to fire (simplified for non-periodic)
		case 0x10: // FSB Route
			t.fsRoute = val
		}
	}
	return nil
}

func (d *hpetDevice) updateCounterLocked() {
	if !d.enabled {
		return
	}

	now := time.Now()
	// Prevent backward time jumps
	if now.Before(d.lastUpdate) {
		d.lastUpdate = now
		return
	}

	elapsed := now.Sub(d.lastUpdate)
	ticks := (uint64(elapsed.Nanoseconds()) * 1_000_000) / hpetClockPeriodFemtoseconds

	d.counter += ticks
	d.lastUpdate = now

	// Check timers (Simplistic implementation for standard Windows usage)
	// Real implementation handles wrap-around and periodic mode
	for i := range d.timers {
		t := &d.timers[i]
		// Check if enabled (bit 2)
		if (t.config & 4) == 4 {
			// This is a naive equality check. In reality, you check if you passed the threshold.
			// However, for high-freq polling loops in Windows, this is often sufficient,
			// or Windows sets a one-shot and waits for interrupt.

			// If we passed the comparator in this update window
			if d.counter >= t.comparator && (d.counter-ticks) < t.comparator {
				// Fire Interrupt
				// Extract IRQ line from config (bits 9:13)
				irq := int((t.config >> 9) & 0x1F)

				// Handle Legacy Replacement Route (usually Timer 0 -> IRQ 0, Timer 1 -> IRQ 8)
				if (d.generalConfig & 2) != 0 {
					if i == 0 {
						irq = 0
					}
					if i == 1 {
						irq = 8
					}
				}

				if d.sink != nil {
					// Pulse the interrupt (High then Low for Edge)
					d.sink.SetIRQ(uint32(irq), true)
					d.sink.SetIRQ(uint32(irq), false)
				}

				// Set interrupt status bit
				d.intStatus |= (1 << i)
			}
		}
	}
}

// No-op for init, handled in constructor
func (d *hpetDevice) Init(vm hv.VirtualMachine) error { return nil }

var (
	_ hv.MemoryMappedIODevice = &hpetDevice{}
)
