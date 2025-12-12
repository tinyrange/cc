package chipset

import (
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
)

// InterruptSink receives interrupt assertions for a given line.
type InterruptSink interface {
	SetIRQ(line uint8, level bool)
}

type mmioBinding struct {
	region  hv.MMIORegion
	handler MmioHandler
}

// ChipsetBuilder registers devices and their intercepts before creating a Chipset.
type ChipsetBuilder struct {
	devices    map[string]ChipsetDevice
	pio        map[uint16]PortIOHandler
	mmio       []mmioBinding
	interrupts map[uint8]InterruptSink
	polls      []PollHandler
}

// NewBuilder returns an empty ChipsetBuilder instance.
func NewBuilder() *ChipsetBuilder {
	return &ChipsetBuilder{
		devices:    make(map[string]ChipsetDevice),
		pio:        make(map[uint16]PortIOHandler),
		interrupts: make(map[uint8]InterruptSink),
	}
}

// RegisterDevice adds a chipset device and wires up its intercepts.
func (b *ChipsetBuilder) RegisterDevice(name string, dev ChipsetDevice) error {
	if b == nil {
		return fmt.Errorf("chipset builder is nil")
	}
	if name == "" {
		return fmt.Errorf("device name is empty")
	}
	if dev == nil {
		return fmt.Errorf("device %q is nil", name)
	}
	if _, exists := b.devices[name]; exists {
		return fmt.Errorf("device %q already registered", name)
	}

	if intercept := dev.SupportsPortIO(); intercept != nil {
		if intercept.Handler == nil {
			return fmt.Errorf("device %q provided port I/O ports with nil handler", name)
		}
		for _, port := range intercept.Ports {
			if err := b.WithPioPort(port, intercept.Handler); err != nil {
				return fmt.Errorf("device %q: %w", name, err)
			}
		}
	}

	if intercept := dev.SupportsMmio(); intercept != nil {
		if intercept.Handler == nil {
			return fmt.Errorf("device %q provided MMIO regions with nil handler", name)
		}
		for _, region := range intercept.Regions {
			if err := b.WithMmioRegion(region.Address, region.Size, intercept.Handler); err != nil {
				return fmt.Errorf("device %q: %w", name, err)
			}
		}
	}

	if poll := dev.SupportsPollDevice(); poll != nil {
		if poll.Handler == nil {
			return fmt.Errorf("device %q provided poll handler nil", name)
		}
		b.polls = append(b.polls, poll.Handler)
	}

	b.devices[name] = dev
	return nil
}

// WithPioPort registers a single I/O port handler.
func (b *ChipsetBuilder) WithPioPort(port uint16, handler PortIOHandler) error {
	if handler == nil {
		return fmt.Errorf("PIO handler for port 0x%x is nil", port)
	}
	if _, exists := b.pio[port]; exists {
		return fmt.Errorf("PIO port 0x%x already registered", port)
	}
	b.pio[port] = handler
	return nil
}

// WithMmioRegion registers a memory-mapped region handler.
func (b *ChipsetBuilder) WithMmioRegion(base, size uint64, handler MmioHandler) error {
	if handler == nil {
		return fmt.Errorf("MMIO handler for region 0x%x size 0x%x is nil", base, size)
	}
	if size == 0 {
		return fmt.Errorf("MMIO region at 0x%x has zero size", base)
	}
	if base+size < base {
		return fmt.Errorf("MMIO region at 0x%x with size 0x%x overflows", base, size)
	}
	for _, existing := range b.mmio {
		if regionsOverlap(base, size, existing.region.Address, existing.region.Size) {
			return fmt.Errorf(
				"MMIO region 0x%x-0x%x overlaps existing region 0x%x-0x%x",
				base, base+size-1, existing.region.Address, existing.region.Address+existing.region.Size-1)
		}
	}

	b.mmio = append(b.mmio, mmioBinding{
		region: hv.MMIORegion{
			Address: base,
			Size:    size,
		},
		handler: handler,
	})
	return nil
}

// WithInterruptLine registers a sink for a specific interrupt line.
func (b *ChipsetBuilder) WithInterruptLine(line uint8, sink InterruptSink) error {
	if sink == nil {
		return fmt.Errorf("interrupt sink for line %d is nil", line)
	}
	if _, exists := b.interrupts[line]; exists {
		return fmt.Errorf("interrupt line %d already registered", line)
	}
	b.interrupts[line] = sink
	return nil
}

// Build finalizes the chipset layout and returns the constructed Chipset.
func (b *ChipsetBuilder) Build() (*Chipset, error) {
	if b == nil {
		return nil, fmt.Errorf("chipset builder is nil")
	}

	devices := make(map[string]ChipsetDevice, len(b.devices))
	for name, dev := range b.devices {
		devices[name] = dev
	}

	pio := make(map[uint16]PortIOHandler, len(b.pio))
	for port, handler := range b.pio {
		pio[port] = handler
	}

	mmio := make([]mmioBinding, len(b.mmio))
	copy(mmio, b.mmio)

	interrupts := make(map[uint8]InterruptSink, len(b.interrupts))
	for line, sink := range b.interrupts {
		interrupts[line] = sink
	}

	polls := make([]PollHandler, len(b.polls))
	copy(polls, b.polls)

	return &Chipset{
		devices:    devices,
		pio:        pio,
		mmio:       mmio,
		interrupts: interrupts,
		polls:      polls,
	}, nil
}

func regionsOverlap(baseA, sizeA, baseB, sizeB uint64) bool {
	endA := baseA + sizeA
	endB := baseB + sizeB
	return baseA < endB && baseB < endA
}

// Chipset represents the built dispatch tables for chipset devices.
type Chipset struct {
	devices    map[string]ChipsetDevice
	pio        map[uint16]PortIOHandler
	mmio       []mmioBinding
	interrupts map[uint8]InterruptSink
	polls      []PollHandler
}
