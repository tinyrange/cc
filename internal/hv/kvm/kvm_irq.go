//go:build linux && amd64

package kvm

import (
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
)

func (v *virtualMachine) SetIRQ(irqLine uint32, level bool) error {
	if v == nil {
		return fmt.Errorf("kvm: virtual machine is nil")
	}

	// Debug: uncomment to trace IRQ activity
	// if irqLine <= 4 && level {
	// 	fmt.Printf("kvm: SetIRQ line=%d level=%v\n", irqLine, level)
	// }

	if v.hv.Architecture() == hv.ArchitectureX86_64 && !v.hasIRQChip {
		return fmt.Errorf("kvm: cannot pulse IRQ without irqchip")
	}

	// For x86_64 with userspace IOAPIC, route device IRQ lines through it.
	// Raw IRQ lines have no chip ID in the high bits (irqLine>>16 == 0).
	// The IOAPIC will look up the redirection entry and call InjectInterrupt.
	if v.hv.Architecture() == hv.ArchitectureX86_64 && v.ioapic != nil && irqLine>>16 == 0 {
		v.ioapic.SetIRQ(irqLine, level)
		return nil
	}

	// For ARM64 or when IOAPIC is not available, use KVM_IRQ_LINE directly.
	if v.hv.Architecture() == hv.ArchitectureX86_64 && irqLine>>16 == 0 {
		irqLine = (irqChipIOAPIC << 16) | irqLine
	}

	if err := irqLevel(v.vmFd, irqLine, level); err != nil {
		return fmt.Errorf("setting IRQ line: %w", err)
	}

	return nil
}

// InjectInterrupt injects an interrupt into the guest using MSI.
// This is called by the userspace IOAPIC routing callback.
// vector: The IDT vector (0-255)
// dest: The destination APIC ID
// destMode: 0 for Physical, 1 for Logical
// deliveryMode: 0 for Fixed, 1 for LowestPriority, etc.
func (v *virtualMachine) InjectInterrupt(vector, dest, destMode, deliveryMode uint8) error {
	if v == nil {
		return fmt.Errorf("kvm: virtual machine is nil")
	}

	// Build MSI address and data according to Intel x86 MSI format.
	// Address format (for physical destination mode):
	//   bits 31:20 = 0xFEE (fixed)
	//   bits 19:12 = Destination APIC ID
	//   bit 11:4   = Reserved (0)
	//   bit 3      = Redirection hint (0 = no redirection)
	//   bit 2      = Destination mode (0 = physical, 1 = logical)
	//   bits 1:0   = Reserved (0)
	addressLo := uint32(0xFEE00000) | (uint32(dest) << 12)
	if destMode == 1 {
		addressLo |= (1 << 2) // Logical destination mode
	}

	// Data format:
	//   bits 15:8 = Delivery mode
	//   bits 7:0  = Vector
	data := uint32(vector) | (uint32(deliveryMode) << 8)

	if err := signalMSI(v.vmFd, addressLo, 0, data); err != nil {
		return fmt.Errorf("kvm: signal MSI: %w", err)
	}

	return nil
}
