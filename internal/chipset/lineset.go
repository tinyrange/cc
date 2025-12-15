package chipset

import "sync"

// LineSet manages interrupt lines and EOI callbacks.
type LineSet struct {
	mu sync.Mutex

	sink InterruptSink

	eoiTarget EOITarget

	lines map[uint8]*lineState
	eoi   map[uint8][]func()
}

// NewLineSet builds a LineSet that forwards assertions to the provided sink.
func NewLineSet(sink InterruptSink) *LineSet {
	if sink == nil {
		sink = noopInterruptSink{}
	}
	return &LineSet{
		sink:  sink,
		lines: make(map[uint8]*lineState),
		eoi:   make(map[uint8][]func()),
	}
}

// AttachEOITarget wires EOI broadcasts to any target exposing HandleEOI(uint32).
func (l *LineSet) AttachEOITarget(target EOITarget) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.eoiTarget = target
}

// AllocateLine returns a LineInterrupt handle for the given IRQ line.
func (l *LineSet) AllocateLine(irq uint8) LineInterrupt {
	l.mu.Lock()
	defer l.mu.Unlock()
	if _, ok := l.lines[irq]; !ok {
		l.lines[irq] = &lineState{}
	}
	return &lineHandle{owner: l, irq: irq}
}

// RegisterEOICallback registers a callback for the given vector.
// The callback is invoked when BroadcastEOI is called with the same vector.
func (l *LineSet) RegisterEOICallback(line uint8, fn func()) {
	if fn == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.eoi[line] = append(l.eoi[line], fn)
}

// BroadcastEOI notifies listeners that an EOI was signalled for the vector.
func (l *LineSet) BroadcastEOI(vector uint8) {
	l.mu.Lock()
	callbacks := append([]func(){}, l.eoi[vector]...)
	target := l.eoiTarget
	l.mu.Unlock()
	if target != nil {
		target.HandleEOI(uint32(vector))
	}
	for _, fn := range callbacks {
		fn()
	}
}

// EOITarget is the minimal interface for receivers of EOI broadcasts (e.g. IOAPIC).
type EOITarget interface {
	HandleEOI(uint32)
}

type lineState struct {
	level bool
}

type lineHandle struct {
	owner *LineSet
	irq   uint8
}

func (h *lineHandle) SetLevel(high bool) {
	h.owner.setLevel(h.irq, high)
}

func (h *lineHandle) PulseInterrupt() {
	h.owner.pulse(h.irq)
}

func (l *LineSet) setLevel(irq uint8, high bool) {
	l.mu.Lock()
	state := l.lines[irq]
	if state == nil {
		state = &lineState{}
		l.lines[irq] = state
	}
	changed := state.level != high
	state.level = high
	l.mu.Unlock()

	if changed {
		l.sink.SetIRQ(irq, high)
	}
}

func (l *LineSet) pulse(irq uint8) {
	l.sink.SetIRQ(irq, true)
	l.sink.SetIRQ(irq, false)
}

type noopInterruptSink struct{}

func (noopInterruptSink) SetIRQ(uint8, bool) {}
