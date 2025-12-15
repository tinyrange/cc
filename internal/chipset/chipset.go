package chipset

import (
	"context"
	"fmt"
	"sort"
)

// Start activates all registered devices.
func (c *Chipset) Start() error {
	for _, name := range c.deviceNames() {
		if err := c.devices[name].Start(); err != nil {
			return fmt.Errorf("chipset: start device %q: %w", name, err)
		}
	}
	return nil
}

// Stop deactivates all registered devices.
func (c *Chipset) Stop() error {
	for _, name := range c.deviceNames() {
		if err := c.devices[name].Stop(); err != nil {
			return fmt.Errorf("chipset: stop device %q: %w", name, err)
		}
	}
	return nil
}

// Reset resets all registered devices.
func (c *Chipset) Reset() error {
	for _, name := range c.deviceNames() {
		if err := c.devices[name].Reset(); err != nil {
			return fmt.Errorf("chipset: reset device %q: %w", name, err)
		}
	}
	return nil
}

// HandlePIO dispatches an I/O port access to the registered device.
func (c *Chipset) HandlePIO(port uint16, data []byte, isWrite bool) error {
	handler, ok := c.pio[port]
	if !ok {
		return fmt.Errorf("chipset: no handler for I/O port 0x%04x", port)
	}
	if isWrite {
		return handler.WriteIOPort(port, data)
	}
	return handler.ReadIOPort(port, data)
}

// HandleMMIO dispatches an MMIO access to the registered device.
func (c *Chipset) HandleMMIO(addr uint64, data []byte, isWrite bool) error {
	accessEnd := addr + uint64(len(data))
	if accessEnd < addr {
		return fmt.Errorf("chipset: MMIO access overflow at 0x%016x", addr)
	}

	for _, binding := range c.mmio {
		start := binding.region.Address
		end := start + binding.region.Size
		if addr >= start && accessEnd <= end {
			if isWrite {
				return binding.handler.WriteMMIO(addr, data)
			}
			return binding.handler.ReadMMIO(addr, data)
		}
	}

	return fmt.Errorf("chipset: no handler for MMIO address 0x%016x", addr)
}

// Poll executes Poll on all poll-capable devices.
func (c *Chipset) Poll(ctx context.Context) error {
	for _, handler := range c.polls {
		if err := handler.Poll(ctx); err != nil {
			return fmt.Errorf("chipset: poll: %w", err)
		}
	}
	return nil
}

func (c *Chipset) deviceNames() []string {
	names := make([]string, 0, len(c.devices))
	for name := range c.devices {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
