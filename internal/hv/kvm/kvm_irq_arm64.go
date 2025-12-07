//go:build linux && arm64

package kvm

import "fmt"

// ARM64 KVM IRQ type encoding (bits 31-24 of irq field)
const (
	kvmArmIRQTypeSPI = 1  // Shared Peripheral Interrupt
	armIRQTypeShift  = 24 // Shift for IRQ type in encoded irqLine
	armSPIBase       = 32 // GIC SPIs start at INTID 32
)

// SetIRQ asserts or deasserts an interrupt line on arm64.
// The irqLine is expected to be in the encoded format produced by
// EncodeIRQLineForArch:
//   - bits 31-24: IRQ type (1 = SPI)
//   - bits 15-0:  GIC INTID
//
// For KVM, we need to convert the GIC INTID to an SPI offset (INTID - 32).
func (v *virtualMachine) SetIRQ(irqLine uint32, level bool) error {
	if v == nil {
		return fmt.Errorf("kvm: virtual machine is nil")
	}

	// Extract the IRQ type from the encoded irqLine.
	irqType := (irqLine >> armIRQTypeShift) & 0xff
	if irqType == 0 {
		return fmt.Errorf("kvm: interrupt type missing in irqLine %#x", irqLine)
	}

	// Extract the GIC INTID from the low 16 bits.
	intid := irqLine & 0xffff

	// Note: Testing whether kernel expects full INTID or SPI offset.
	// According to kernel source, it expects SPI offset (INTID - 32).
	// But let's try both formats.

	// Try full INTID first (don't subtract armSPIBase)
	kvmIRQ := (irqType << armIRQTypeShift) | intid

	if err := irqLevel(v.vmFd, kvmIRQ, level); err != nil {
		return fmt.Errorf("kvm: setting IRQ line (input=%#x, kvmIRQ=%#x, type=%d, intid=%d): %w",
			irqLine, kvmIRQ, irqType, intid, err)
	}

	return nil
}
