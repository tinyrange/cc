//go:build linux && amd64

package kvm

import (
	"encoding/binary"
	"testing"
)

func TestISAParentIsDiscoverableThroughPCIConfig(t *testing.T) {
	bus := NewPCIBus(NewISABridgePCIDevice(31))
	writePCIAddress(t, bus, 0, 31, 0, 0)
	identity := readPort(t, bus, pciConfigDataPort, 4)
	if binary.LittleEndian.Uint16(identity) != 0x8086 || binary.LittleEndian.Uint16(identity[2:]) != 0x7000 {
		t.Fatalf("PIIX3 identity: %x", identity)
	}
	writePCIAddress(t, bus, 0, 31, 0, 8)
	class := readPort(t, bus, pciConfigDataPort, 4)
	if class[3] != 6 || class[2] != 1 {
		t.Fatalf("expected PCI ISA bridge class, got %x", class)
	}
	writePCIAddress(t, bus, 0, 31, 0, 4)
	command := readPort(t, bus, pciConfigDataPort, 4)
	if binary.LittleEndian.Uint16(command)&1 == 0 {
		t.Fatal("legacy I/O decoding disabled")
	}
}
