//go:build linux && amd64

package kvm

import (
	"encoding/binary"
	"testing"

	"j5.nz/cc/internal/nvme"
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

func TestPCIBusExposesType2ConfigSpace(t *testing.T) {
	block := virtio.NewBlock(0, 0x1000, 10, nil)
	bus := NewPCIBus(NewVirtioBlockPCIDevice(1, 0x1000, 10, block))

	writePort(t, bus, pciConfigAddressPort, []byte{0xf0})
	writePort(t, bus, pciConfigAddressPort+2, []byte{0})
	data := readPort(t, bus, pciConfigType2Port|0x100, 4)
	if vendor := binary.LittleEndian.Uint16(data[0:2]); vendor != pciVendorQumranet {
		t.Fatalf("type 2 vendor = %#x", vendor)
	}
	if device := binary.LittleEndian.Uint16(data[2:4]); device != pciVirtioBlockDeviceID {
		t.Fatalf("type 2 device = %#x", device)
	}
	data = readPort(t, bus, pciConfigType2Port|0x110, 4)
	if bar := binary.LittleEndian.Uint32(data); bar != 0x1001 {
		t.Fatalf("type 2 bar = %#x", bar)
	}
}

func TestNVMePCIDeviceConfigUsesMemoryBAR(t *testing.T) {
	ctrl := nvme.NewController(nil)
	dev := NewNVMePCIDevice(1, 0xfeb00000, 10, ctrl)
	bus := NewPCIBus(dev)

	var cfg [4]byte
	bus.readConfigAt(0, 1, 0, 0x08, cfg[:])
	if class := cfg[3]; class != 0x01 {
		t.Fatalf("class = %#x", class)
	}
	if subclass := cfg[2]; subclass != 0x08 {
		t.Fatalf("subclass = %#x", subclass)
	}
	if progIF := cfg[1]; progIF != 0x02 {
		t.Fatalf("progIF = %#x", progIF)
	}

	bus.readConfigAt(0, 1, 0, 0x10, cfg[:])
	if bar := binary.LittleEndian.Uint32(cfg[:]); bar != 0xfeb00000 {
		t.Fatalf("BAR0 = %#x", bar)
	}

	bus.writeConfigAt(0, 1, 0, 0x10, []byte{0xff, 0xff, 0xff, 0xff})
	bus.readConfigAt(0, 1, 0, 0x10, cfg[:])
	if mask := binary.LittleEndian.Uint32(cfg[:]); mask != 0xffffc000 {
		t.Fatalf("BAR0 probe mask = %#x", mask)
	}

	newBAR := []byte{0x00, 0x00, 0xa0, 0xfe}
	bus.writeConfigAt(0, 1, 0, 0x10, newBAR)
	if dev.MMIOBAR != 0xfea00000 {
		t.Fatalf("device MMIOBAR = %#x", dev.MMIOBAR)
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
