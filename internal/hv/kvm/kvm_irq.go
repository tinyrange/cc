//go:build linux

package kvm

import (
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
)

func (v *virtualMachine) SetIRQ(irqLine uint32, level bool) error {
	if v == nil {
		return fmt.Errorf("kvm: virtual machine is nil")
	}

	if v.hv.Architecture() == hv.ArchitectureX86_64 && !v.hasIRQChip {
		return fmt.Errorf("kvm: cannot pulse IRQ without irqchip")
	}

	if err := irqLevel(v.vmFd, irqLine, level); err != nil {
		return fmt.Errorf("setting IRQ line: %w", err)
	}

	return nil
}
