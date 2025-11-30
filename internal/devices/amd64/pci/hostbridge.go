package pci

import (
	"encoding/binary"
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
)

// HostBridge implements a minimal PCI host bridge that services legacy
// configuration space accesses through ports 0xCF8-0xCFF. Only bus 0 / device 0
// / function 0 is populated; reads to other devices return 0xFF and writes are
// ignored. This is sufficient for Linux to probe PCI early in boot without
// triple faulting.
type HostBridge struct {
	vm      hv.VirtualMachine
	address uint32

	config   map[pciLocation][]byte
	readOnly map[pciLocation]map[uint32]struct{}
}

type pciLocation struct {
	bus      uint8
	device   uint8
	function uint8
}

const (
	pciConfigAddressPort = 0x0cf8
	pciConfigDataPort    = 0x0cfc

	piix4PMIOBase = 0x0b000
	piix4PMIOSize = 64
)

func NewHostBridge() *HostBridge {
	hb := &HostBridge{
		config:   make(map[pciLocation][]byte),
		readOnly: make(map[pciLocation]map[uint32]struct{}),
	}

	// PCI host bridge (bus 0, device 0, function 0)
	host := make([]byte, 256)
	binary.LittleEndian.PutUint16(host[0x00:], 0x8086) // Vendor ID
	binary.LittleEndian.PutUint16(host[0x02:], 0x1237) // Device ID (82441FX)
	host[0x08] = 0x02                                  // Revision
	host[0x09] = 0x00                                  // Prog IF
	host[0x0A] = 0x00                                  // Subclass: host bridge
	host[0x0B] = 0x06                                  // Class: bridge
	host[0x0E] = 0x00                                  // Header type
	hb.addDevice(pciLocation{bus: 0, device: 0, function: 0}, host)
	hb.setReadOnlyRange(pciLocation{bus: 0, device: 0, function: 0}, 0x00, 0x03)
	hb.setReadOnlyRange(pciLocation{bus: 0, device: 0, function: 0}, 0x08, 0x0B)
	hb.setReadOnlyRange(pciLocation{bus: 0, device: 0, function: 0}, 0x0E, 0x0E)

	return hb
}

// Init implements hv.Device.
func (hb *HostBridge) Init(vm hv.VirtualMachine) error {
	if _, ok := vm.(hv.VirtualMachineAmd64); !ok {
		return fmt.Errorf("pci host bridge requires an x86_64 VM")
	}
	hb.vm = vm
	return nil
}

// IOPorts implements hv.X86IOPortDevice.
func (hb *HostBridge) IOPorts() []uint16 {
	return []uint16{
		0x0cf8, 0x0cf9, 0x0cfa, 0x0cfb,
		0x0cfc, 0x0cfd, 0x0cfe, 0x0cff,
	}
}

// ReadIOPort implements hv.X86IOPortDevice.
func (hb *HostBridge) ReadIOPort(port uint16, data []byte) error {
	// slog.Info("pci host bridge: read I/O port", "port", fmt.Sprintf("0x%04x", port), "size", len(data))

	for i := range data {
		cur := port + uint16(i)
		switch {
		case cur >= pciConfigAddressPort && cur <= pciConfigAddressPort+3:
			shift := (cur - pciConfigAddressPort) * 8
			data[i] = byte(hb.address >> shift)
		case cur >= pciConfigDataPort && cur <= pciConfigDataPort+3:
			value, err := hb.readConfigByte(uint16(cur - pciConfigDataPort))
			if err != nil {
				return err
			}
			data[i] = value
		default:
			return fmt.Errorf("pci host bridge: unhandled read from I/O port 0x%04x", cur)
		}
	}
	return nil
}

// WriteIOPort implements hv.X86IOPortDevice.
func (hb *HostBridge) WriteIOPort(port uint16, data []byte) error {
	// slog.Info("pci host bridge: write I/O port", "port", fmt.Sprintf("0x%04x", port), "size", len(data), "data", data)

	for i, b := range data {
		cur := port + uint16(i)
		switch {
		case cur >= pciConfigAddressPort && cur <= pciConfigAddressPort+3:
			shift := (cur - pciConfigAddressPort) * 8
			mask := uint32(0xFF) << shift
			hb.address = (hb.address &^ mask) | (uint32(b) << shift)
		case cur >= pciConfigDataPort && cur <= pciConfigDataPort+3:
			if err := hb.writeConfigByte(uint16(cur-pciConfigDataPort), b); err != nil {
				return err
			}
		default:
			return fmt.Errorf("pci host bridge: unhandled write to I/O port 0x%04x", cur)
		}
	}
	return nil
}

func (hb *HostBridge) readConfigByte(offset uint16) (byte, error) {
	cfg, reg, _, ok := hb.configTarget(offset)
	if !ok {
		return 0xFF, nil
	}
	if reg >= uint32(len(cfg)) {
		return 0xFF, nil
	}
	return cfg[reg], nil
}

func (hb *HostBridge) writeConfigByte(offset uint16, value byte) error {
	cfg, reg, loc, ok := hb.configTarget(offset)
	if !ok {
		return nil
	}
	if reg >= uint32(len(cfg)) {
		return nil
	}

	if hb.isReadOnly(loc, reg) {
		return nil
	}

	cfg[reg] = value
	return nil
}

func (hb *HostBridge) configTarget(offset uint16) ([]byte, uint32, pciLocation, bool) {
	if hb.address&(1<<31) == 0 {
		return nil, 0, pciLocation{}, false
	}

	loc := pciLocation{
		bus:      uint8((hb.address >> 16) & 0xFF),
		device:   uint8((hb.address >> 11) & 0x1F),
		function: uint8((hb.address >> 8) & 0x7),
	}
	cfg, ok := hb.config[loc]
	if !ok {
		return nil, 0, pciLocation{}, false
	}

	reg := (hb.address & 0xFC) + uint32(offset)
	return cfg, reg, loc, true
}

func (hb *HostBridge) addDevice(loc pciLocation, cfg []byte) {
	hb.config[loc] = cfg
}

func (hb *HostBridge) setReadOnlyRange(loc pciLocation, start, end uint32) {
	if hb.readOnly[loc] == nil {
		hb.readOnly[loc] = make(map[uint32]struct{})
	}
	for offset := start; offset <= end; offset++ {
		hb.readOnly[loc][offset] = struct{}{}
	}
}

func (hb *HostBridge) isReadOnly(loc pciLocation, offset uint32) bool {
	entries, ok := hb.readOnly[loc]
	if !ok {
		return false
	}
	_, ro := entries[offset]
	return ro
}

var (
	_ hv.Device          = (*HostBridge)(nil)
	_ hv.X86IOPortDevice = (*HostBridge)(nil)
)
