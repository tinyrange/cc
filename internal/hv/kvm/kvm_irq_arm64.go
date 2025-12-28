//go:build linux && arm64

package kvm

import (
	"fmt"
)

// ARM64 KVM IRQ type encoding (bits 31-24 of irq field)
const (
	kvmArmIRQTypeSPI = 1  // Shared Peripheral Interrupt
	armIRQTypeShift  = 24 // Shift for IRQ type in encoded irqLine
	armSPIBase       = 32 // GIC SPIs start at INTID 32
)

func arm64KVMIRQFromEncodedIRQLine(irqLine uint32) (kvmIRQ uint32, irqType uint32, intid uint32, err error) {
	// Extract the IRQ type from the encoded irqLine.
	irqType = (irqLine >> armIRQTypeShift) & 0xff
	if irqType == 0 {
		return 0, 0, 0, fmt.Errorf("kvm: interrupt type missing in irqLine %#x", irqLine)
	}

	// The low 16 bits carry the IRQ number. For SPI this is an SPI *offset*
	// (as used by the device tree "interrupts" property). KVM expects a GIC INTID.
	irqNum := irqLine & 0xffff

	switch irqType {
	case kvmArmIRQTypeSPI:
		intid = armSPIBase + irqNum
	default:
		return 0, irqType, irqNum, fmt.Errorf("kvm: unsupported ARM64 IRQ type %d in irqLine %#x", irqType, irqLine)
	}

	if intid > 0xffff {
		return 0, irqType, intid, fmt.Errorf("kvm: INTID %d out of range in irqLine %#x", intid, irqLine)
	}

	kvmIRQ = (irqType << armIRQTypeShift) | (intid & 0xffff)
	return kvmIRQ, irqType, intid, nil
}

// SetIRQ asserts or deasserts an interrupt line on arm64.
// The irqLine is expected to be in the encoded format produced by
// EncodeIRQLineForArch:
//   - bits 31-24: IRQ type (1 = SPI)
//   - bits 15-0:  SPI offset (INTID - 32)
func (v *virtualMachine) SetIRQ(irqLine uint32, level bool) error {
	if v == nil {
		return fmt.Errorf("kvm: virtual machine is nil")
	}

	kvmIRQ, irqType, intid, err := arm64KVMIRQFromEncodedIRQLine(irqLine)
	if err != nil {
		return err
	}

	if err := irqLevel(v.vmFd, kvmIRQ, level); err != nil {
		return fmt.Errorf("kvm: setting IRQ line (input=%#x, kvmIRQ=%#x, type=%d, intid=%d): %w",
			irqLine, kvmIRQ, irqType, intid, err)
	}

	return nil
}
