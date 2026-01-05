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

	timerConfIntType     uint64 = 1 << 1 // level vs edge
	timerConfIntEnable   uint64 = 1 << 2 // INT_ENB_CNF
	timerConfPeriodic    uint64 = 1 << 3 // TYPE_CNF
	timerConfPeriodicCap uint64 = 1 << 4 // PER_INT_CAP
	timerConfSizeCap     uint64 = 1 << 5 // SIZE_CAP
	timerConfValSet      uint64 = 1 << 6 // VAL_SET_CNF
	timerConf32Bit       uint64 = 1 << 8 // 32MODE_CNF

	timerConfIntRouteShift uint64 = 9
	timerConfIntRouteMask  uint64 = 0x1F << timerConfIntRouteShift

	timerConfFSBEnable uint64 = 1 << 14
	timerConfFSBCap    uint64 = 1 << 15

	timerWritableMask = timerConfIntType | timerConfIntEnable | timerConfPeriodic |
		timerConfValSet | timerConf32Bit | timerConfIntRouteMask | timerConfFSBEnable

	legacyReplacementCap = uint64(1 << 15)

	hpetPollInterval = 100 * time.Microsecond

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
	caps       uint64
	comparator uint64
	period     uint64
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

	ticker *time.Ticker

	debugConfigWrites int
	debugTimerConfig  [numTimers]int
	debugTimerCmp     [numTimers]int
	debugTimerIRQs    [numTimers]int
	debugReads        int
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

	dev := &Device{
		bases:      bases,
		sink:       sink,
		lastUpdate: time.Now(),
	}

	for i := range dev.timers {
		caps := timerConfPeriodicCap | timerConfSizeCap | (uint64(0xffffffff) << 32)
		caps &^= timerConfFSBCap
		dev.timers[i].caps = caps
		dev.timers[i].config = caps
	}

	dev.ticker = time.NewTicker(hpetPollInterval)
	go dev.run()

	return dev
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
func (d *Device) ReadMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.advanceCounterLocked(time.Now())

	offset, err := d.offsetFor(addr)
	if err != nil {
		return err
	}
	if d.debugReads < 12 {
		d.debugReads++
		fmt.Printf("hpet: read offset=%#x size=%d\n", offset, len(data))
	}
	val := uint64(0)

	switch {
	case offset == regGenCap:
		val = uint64(clockPeriodFemtoseconds)<<32 | uint64(vendorID)<<16 | uint64(1)<<13 | (numTimers - 1) | legacyReplacementCap
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

func (d *Device) WriteMMIO(ctx hv.ExitContext, addr uint64, data []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	now := time.Now()
	d.advanceCounterLocked(now)

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
		d.generalConfig = val & 0x3
		enabled := (d.generalConfig & 1) == 1
		if d.debugConfigWrites < 4 {
			d.debugConfigWrites++
			fmt.Printf("hpet: general config=%#x enable=%v legacy=%v\n", val, enabled, (val&2) != 0)
		}
		if enabled && !d.enabled {
			d.lastUpdate = now
		}
		d.enabled = enabled
	case offset == regIntStatus:
		d.intStatus &= ^val
	case offset == regMainCounter:
		d.counter = val
		if d.enabled {
			d.lastUpdate = now
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
			t.config = (val & timerWritableMask) | t.caps
			if (t.config & timerConf32Bit) != 0 {
				t.comparator &= 0xffffffff
				t.period &= 0xffffffff
			}
			if d.debugTimerConfig[idx] < 4 {
				d.debugTimerConfig[idx]++
				fmt.Printf("hpet: timer%d config=%#x enable=%v periodic=%v route=%d fsb=%v\n",
					idx, val, (val&timerConfIntEnable) != 0, (val&timerConfPeriodic) != 0,
					(val&timerConfIntRouteMask)>>timerConfIntRouteShift, (val&timerConfFSBEnable) != 0)
			}
		case 0x08:
			if (t.config & timerConf32Bit) != 0 {
				val &= 0xffffffff
			}
			t.comparator = val
			t.period = val
			if d.debugTimerCmp[idx] < 4 {
				d.debugTimerCmp[idx]++
				fmt.Printf("hpet: timer%d comparator=%#x period=%#x enable=%v periodic=%v\n",
					idx, t.comparator, t.period, (t.config&timerConfIntEnable) != 0, (t.config&timerConfPeriodic) != 0)
			}
		case 0x10:
			t.fsRoute = val
		}
	}
	return nil
}

func (d *Device) advanceCounterLocked(now time.Time) {
	if now.Before(d.lastUpdate) {
		d.lastUpdate = now
		return
	}

	prev := d.counter
	if d.enabled {
		elapsed := now.Sub(d.lastUpdate)
		ticks := (uint64(elapsed.Nanoseconds()) * 1_000_000) / clockPeriodFemtoseconds
		d.counter += ticks
	}
	d.lastUpdate = now

	if d.enabled {
		d.checkTimersLocked(prev)
	}
}

func (d *Device) checkTimersLocked(prev uint64) {
	current := d.counter
	for i := range d.timers {
		t := &d.timers[i]
		if (t.config & timerConfIntEnable) == 0 {
			continue
		}
		// MSI/FSB delivery is not implemented.
		if (t.config & timerConfFSBEnable) != 0 {
			continue
		}

		period := t.period
		if (t.config&timerConfPeriodic) != 0 && period == 0 {
			period = t.comparator
		}

		if (t.config&timerConfPeriodic) == 0 || period == 0 {
			if prev < t.comparator && current >= t.comparator {
				d.raiseIRQLocked(i, t)
			}
			continue
		}

		fired := false
		comp := t.comparator
		for period > 0 && current >= comp {
			fired = true
			comp += period
		}
		t.comparator = comp
		t.period = period
		if fired {
			d.raiseIRQLocked(i, t)
		}
	}
}

func (d *Device) raiseIRQLocked(idx int, t *timer) {
	irq := d.routeForTimerLocked(idx, t)
	d.intStatus |= 1 << idx
	if d.debugTimerIRQs[idx] < 8 {
		d.debugTimerIRQs[idx]++
		fmt.Printf("hpet: timer%d IRQ route=%d status=%#x\n", idx, irq, d.intStatus)
	}
	if d.sink == nil {
		return
	}
	_ = d.sink.SetIRQ(uint32(irq), true)
	_ = d.sink.SetIRQ(uint32(irq), false)
}

func (d *Device) routeForTimerLocked(idx int, t *timer) int {
	if (d.generalConfig & 2) != 0 {
		if idx == 0 {
			return 0
		}
		if idx == 1 {
			return 8
		}
	}
	route := (t.config & timerConfIntRouteMask) >> timerConfIntRouteShift
	return int(route)
}

func (d *Device) run() {
	for now := range d.ticker.C {
		d.mu.Lock()
		d.advanceCounterLocked(now)
		d.mu.Unlock()
	}
}

var (
	_ hv.MemoryMappedIODevice = (*Device)(nil)
)
