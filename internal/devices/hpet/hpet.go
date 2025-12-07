package hpet

import (
	"fmt"
	"sync"
	"time"

	"github.com/tinyrange/cc/internal/hv"
)

// InterruptSink defines where the HPET sends its signals (usually the IOAPIC).
type InterruptSink interface {
	SetIRQ(irq uint32, level bool) error
}

const (
	clockPeriodFemtoseconds = 10_000_000 // 10ns
	vendorID                = 0x8086
	numTimers               = 3 // enough for typical guests

	regGenCap      = 0x000
	regGenConfig   = 0x010
	regIntStatus   = 0x020
	regMainCounter = 0x0F0
	regTimerConfig = 0x100
	regTimerCmp    = 0x108
	regTimerRoute  = 0x110
	timerStride    = 0x20

	MMIOWindowSize = 0x400
)

type timer struct {
	config     uint64
	comparator uint64
	fsRoute    uint64
}

type Device struct {
	bases []uint64
	sink  InterruptSink

	mu            sync.Mutex
	generalConfig uint64
	intStatus     uint64
	counter       uint64
	lastUpdate    time.Time
	enabled       bool

	timers [numTimers]timer
}

// New constructs an HPET device mapped at base (and optional aliases).
// sink is typically the virtual machine implementing SetIRQ.
func New(base uint64, sink InterruptSink, aliases ...uint64) *Device {
	bases := make([]uint64, 0, 1+len(aliases))
	seen := make(map[uint64]struct{}, 1+len(aliases))
	add := func(addr uint64) {
		if addr == 0 {
			return
		}
		if _, ok := seen[addr]; ok {
			return
		}
		seen[addr] = struct{}{}
		bases = append(bases, addr)
	}
	add(base)
	for _, a := range aliases {
		add(a)
	}

	return &Device{
		bases:      bases,
		sink:       sink,
		lastUpdate: time.Now(),
	}
}

func (d *Device) Init(vm hv.VirtualMachine) error { return nil }

func (d *Device) MMIORegions() []hv.MMIORegion {
	regs := make([]hv.MMIORegion, 0, len(d.bases))
	for _, base := range d.bases {
		regs = append(regs, hv.MMIORegion{Address: base, Size: MMIOWindowSize})
	}
	return regs
}

func (d *Device) offsetFor(addr uint64) (uint64, error) {
	for _, base := range d.bases {
		if addr >= base && addr < base+MMIOWindowSize {
			return addr - base, nil
		}
	}
	return 0, fmt.Errorf("hpet: address 0x%x outside configured MMIO windows", addr)
}

// ReadMMIO handles HPET register reads.
func (d *Device) ReadMMIO(addr uint64, data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.updateCounterLocked()

	offset, err := d.offsetFor(addr)
	if err != nil {
		return err
	}
	val := uint64(0)

	switch {
	case offset == regGenCap:
		val = uint64(clockPeriodFemtoseconds)<<32 | uint64(vendorID)<<16 | uint64(1)<<13 | (numTimers - 1)
	case offset == regGenConfig:
		val = d.generalConfig
	case offset == regIntStatus:
		val = d.intStatus
	case offset == regMainCounter:
		val = d.counter
	case offset >= regTimerConfig:
		idx := (offset - regTimerConfig) / timerStride
		if idx >= numTimers {
			return nil
		}
		reg := (offset - regTimerConfig) % timerStride
		t := &d.timers[idx]
		switch reg {
		case 0x00:
			val = t.config
		case 0x08:
			val = t.comparator
		case 0x10:
			val = t.fsRoute
		}
	}

	if len(data) > 8 {
		return fmt.Errorf("hpet: invalid read size %d", len(data))
	}
	for i := 0; i < len(data); i++ {
		data[i] = byte(val >> (i * 8))
	}
	return nil
}

func (d *Device) WriteMMIO(addr uint64, data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	offset, err := d.offsetFor(addr)
	if err != nil {
		return err
	}
	var val uint64
	for i := 0; i < len(data) && i < 8; i++ {
		val |= uint64(data[i]) << (i * 8)
	}

	switch {
	case offset == regGenConfig:
		d.updateCounterLocked()
		d.generalConfig = val & 0x3
		enabled := (d.generalConfig & 1) == 1
		if enabled && !d.enabled {
			d.lastUpdate = time.Now()
		}
		d.enabled = enabled
	case offset == regIntStatus:
		d.intStatus &= ^val
	case offset == regMainCounter:
		d.counter = val
		if d.enabled {
			d.lastUpdate = time.Now()
		}
	case offset >= regTimerConfig:
		idx := (offset - regTimerConfig) / timerStride
		if idx >= numTimers {
			return nil
		}
		reg := (offset - regTimerConfig) % timerStride
		t := &d.timers[idx]
		switch reg {
		case 0x00:
			t.config = val
		case 0x08:
			t.comparator = val
		case 0x10:
			t.fsRoute = val
		}
	}
	return nil
}

func (d *Device) updateCounterLocked() {
	if !d.enabled {
		return
	}
	now := time.Now()
	if now.Before(d.lastUpdate) {
		d.lastUpdate = now
		return
	}
	elapsed := now.Sub(d.lastUpdate)
	ticks := (uint64(elapsed.Nanoseconds()) * 1_000_000) / clockPeriodFemtoseconds
	d.counter += ticks
	d.lastUpdate = now
	d.checkTimersLocked(ticks)
}

func (d *Device) checkTimersLocked(delta uint64) {
	for i := range d.timers {
		t := &d.timers[i]
		if (t.config & 4) == 0 {
			continue
		}
		if d.counter >= t.comparator && (d.counter-delta) < t.comparator {
			irq := int((t.config >> 9) & 0x1F)
			if (d.generalConfig & 2) != 0 {
				if i == 0 {
					irq = 0
				}
				if i == 1 {
					irq = 8
				}
			}
			if d.sink != nil {
				_ = d.sink.SetIRQ(uint32(irq), true)
				_ = d.sink.SetIRQ(uint32(irq), false)
			}
			d.intStatus |= (1 << i)
		}
	}
}

var (
	_ hv.MemoryMappedIODevice = (*Device)(nil)
)
