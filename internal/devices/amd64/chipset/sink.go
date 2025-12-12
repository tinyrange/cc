package chipset

// readySink models the PIC "INT" output line. Implementations can
// propagate level changes to the hypervisor or to a test double.
type readySink interface {
	SetLevel(level bool)
}

// ReadySinkFunc adapts a function to the readySink interface.
type ReadySinkFunc func(level bool)

// SetLevel implements readySink.
func (f ReadySinkFunc) SetLevel(level bool) {
	if f != nil {
		f(level)
	}
}

type noopReadySink struct{}

func (noopReadySink) SetLevel(bool) {}

// irqLine models a legacy ISA IRQ sink (e.g. PIC input lines).
type irqLine interface {
	SetIRQ(line uint8, level bool)
}

// IRQLineFunc adapts a function to the irqLine interface.
type IRQLineFunc func(line uint8, level bool)

// SetIRQ implements irqLine.
func (f IRQLineFunc) SetIRQ(line uint8, level bool) {
	if f != nil {
		f(line, level)
	}
}

type noopIRQLine struct{}

func (noopIRQLine) SetIRQ(uint8, bool) {}

// LineInterrupt models a shared interrupt line (level-triggered or edge).
type LineInterrupt interface {
	SetLevel(high bool)
	PulseInterrupt()
}

type noopLineInterrupt struct{}

func (noopLineInterrupt) SetLevel(bool)   {}
func (noopLineInterrupt) PulseInterrupt() {}

// LineInterruptDetached returns a LineInterrupt that drops all signals.
func LineInterruptDetached() LineInterrupt {
	return noopLineInterrupt{}
}

// LineInterruptFromFunc adapts a simple level function to LineInterrupt.
func LineInterruptFromFunc(fn func(bool)) LineInterrupt {
	return lineInterruptFunc(fn)
}

type lineInterruptFunc func(bool)

func (f lineInterruptFunc) SetLevel(level bool) {
	if f != nil {
		f(level)
	}
}

func (f lineInterruptFunc) PulseInterrupt() {
	if f != nil {
		f(true)
		f(false)
	}
}
