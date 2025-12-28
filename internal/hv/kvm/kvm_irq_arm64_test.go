//go:build linux && arm64

package kvm

import "testing"

func TestArm64KVMIRQFromEncodedIRQLine_SPIAddsBase(t *testing.T) {
	// SPI offset 8 should become GIC INTID 40 (32 + 8).
	const encoded = (kvmArmIRQTypeSPI << armIRQTypeShift) | 8

	kvmIRQ, irqType, intid, err := arm64KVMIRQFromEncodedIRQLine(encoded)
	if err != nil {
		t.Fatalf("arm64KVMIRQFromEncodedIRQLine: %v", err)
	}
	if irqType != kvmArmIRQTypeSPI {
		t.Fatalf("irqType=%d, want %d", irqType, kvmArmIRQTypeSPI)
	}
	if intid != 40 {
		t.Fatalf("intid=%d, want 40", intid)
	}
	if kvmIRQ != ((kvmArmIRQTypeSPI << armIRQTypeShift) | 40) {
		t.Fatalf("kvmIRQ=%#x, want %#x", kvmIRQ, (kvmArmIRQTypeSPI<<armIRQTypeShift)|40)
	}
}
