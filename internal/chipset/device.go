package chipset

import (
	"context"

	"github.com/tinyrange/cc/internal/hv"
)

// PortIOHandler handles reads and writes to individual I/O ports.
type PortIOHandler interface {
	ReadIOPort(ctx hv.ExitContext, port uint16, data []byte) error
	WriteIOPort(ctx hv.ExitContext, port uint16, data []byte) error
}

// PortIOIntercept describes the ports a device wants to serve and the handler for them.
type PortIOIntercept struct {
	Ports   []uint16
	Handler PortIOHandler
}

// MmioHandler handles reads and writes to memory-mapped regions.
type MmioHandler interface {
	ReadMMIO(ctx hv.ExitContext, addr uint64, data []byte) error
	WriteMMIO(ctx hv.ExitContext, addr uint64, data []byte) error
}

// MmioIntercept describes the MMIO regions a device serves and the handler for them.
type MmioIntercept struct {
	Regions []hv.MMIORegion
	Handler MmioHandler
}

// PollHandler performs periodic maintenance for a device that requires polling.
type PollHandler interface {
	Poll(ctx context.Context) error
}

// PollDevice registers a poll-capable device with the chipset.
type PollDevice struct {
	Handler PollHandler
}

// LineInterrupt models an interrupt line that supports level and edge semantics.
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

// ChangeDeviceState exposes lifecycle hooks for chipset devices.
type ChangeDeviceState interface {
	Start() error
	Stop() error
	Reset() error
}

// ChipsetDevice is the unified interface all chipset devices must implement.
type ChipsetDevice interface {
	hv.Device
	ChangeDeviceState

	SupportsPortIO() *PortIOIntercept
	SupportsMmio() *MmioIntercept
	SupportsPollDevice() *PollDevice
}
