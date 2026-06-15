//go:build linux && amd64

package kvm

import (
	"encoding/binary"
	"testing"

	"j5.nz/cc/internal/virtio"
)

func TestPCIBusExposesVirtioBlockLegacyIOBAR(t *testing.T) {
	block := virtio.NewBlock(0, 0x1000, 10, nil)
	bus := NewPCIBus(NewVirtioBlockPCIDevice(1, 0x1000, 10, block))

	writePCIAddress(t, bus, 0, 1, 0, 0)
	data := readPort(t, bus, pciConfigDataPort, 4)
	if vendor := binary.LittleEndian.Uint16(data[0:2]); vendor != pciVendorQumranet {
		t.Fatalf("vendor = %#x", vendor)
	}
	if device := binary.LittleEndian.Uint16(data[2:4]); device != pciVirtioBlockDeviceID {
		t.Fatalf("device = %#x", device)
	}

	writePCIAddress(t, bus, 0, 1, 0, 0x10)
	data = readPort(t, bus, pciConfigDataPort, 4)
	if bar := binary.LittleEndian.Uint32(data); bar != 0x1001 {
		t.Fatalf("bar = %#x", bar)
	}

	writePort(t, bus, pciConfigDataPort, []byte{0xff, 0xff, 0xff, 0xff})
	data = readPort(t, bus, pciConfigDataPort, 4)
	if mask := binary.LittleEndian.Uint32(data); mask != 0xffffff01 {
		t.Fatalf("bar mask = %#x", mask)
	}

	writePCIAddress(t, bus, 0, 1, 0, 0x3c)
	data = readPort(t, bus, pciConfigDataPort, 2)
	if data[0] != 10 || data[1] != 1 {
		t.Fatalf("interrupt line/pin = %v", data[:2])
	}
}

func writePCIAddress(t *testing.T, bus *PCIBus, busNo, devNo, fnNo uint8, reg uint8) {
	t.Helper()
	value := uint32(1<<31) | uint32(busNo)<<16 | uint32(devNo)<<11 | uint32(fnNo)<<8 | uint32(reg&0xfc)
	var data [4]byte
	binary.LittleEndian.PutUint32(data[:], value)
	writePort(t, bus, pciConfigAddressPort, data[:])
}

func readPort(t *testing.T, bus *PCIBus, port uint16, size uint8) []byte {
	t.Helper()
	data := make([]byte, size)
	handled, err := bus.HandleIO(IOExit{Port: port, Data: data, Size: size, Count: 1})
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatalf("port %#x was not handled", port)
	}
	return data
}

func writePort(t *testing.T, bus *PCIBus, port uint16, data []byte) {
	t.Helper()
	handled, err := bus.HandleIO(IOExit{Port: port, Data: data, Size: uint8(len(data)), Count: 1, Write: true})
	if err != nil {
		t.Fatal(err)
	}
	if !handled {
		t.Fatalf("port %#x was not handled", port)
	}
}
