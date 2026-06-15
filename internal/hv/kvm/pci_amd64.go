//go:build linux && amd64

package kvm

import (
	"encoding/binary"

	"j5.nz/cc/internal/virtio"
)

const (
	pciConfigAddressPort = 0xcf8
	pciConfigDataPort    = 0xcfc

	pciVendorQumranet = 0x1af4

	pciVirtioNetDeviceID   = 0x1000
	pciVirtioBlockDeviceID = 0x1001
)

type pciIOHandler interface {
	ReadLegacy(offset uint16, size int) (uint64, error)
	WriteLegacy(offset uint16, size int, value uint64) error
}

type PCIBus struct {
	configAddress uint32
	devices       []*PCIDevice
}

type PCIDevice struct {
	Bus           uint8
	Device        uint8
	Function      uint8
	VendorID      uint16
	DeviceID      uint16
	SubsystemID   uint16
	Class         uint8
	Subclass      uint8
	ProgIF        uint8
	Revision      uint8
	IRQLine       uint8
	IRQPin        uint8
	IOBAR         uint32
	IOSize        uint32
	Command       uint16
	legacyIO      pciIOHandler
	barProbeValue bool
}

func NewPCIBus(devices ...*PCIDevice) *PCIBus {
	return &PCIBus{devices: devices}
}

func NewVirtioBlockPCIDevice(dev uint8, ioBase uint16, irq uint8, block *virtio.Block) *PCIDevice {
	if block != nil {
		block.IRQ = uint32(irq)
	}
	return &PCIDevice{
		Device:      dev,
		VendorID:    pciVendorQumranet,
		DeviceID:    pciVirtioBlockDeviceID,
		SubsystemID: 2,
		Class:       0x01,
		Subclass:    0x80,
		IRQLine:     irq,
		IRQPin:      1,
		IOBAR:       uint32(ioBase),
		IOSize:      0x100,
		legacyIO:    block,
	}
}

func NewVirtioNetPCIDevice(dev uint8, ioBase uint16, irq uint8, netdev *virtio.Net) *PCIDevice {
	if netdev != nil {
		netdev.IRQ = uint32(irq)
	}
	return &PCIDevice{
		Device:      dev,
		VendorID:    pciVendorQumranet,
		DeviceID:    pciVirtioNetDeviceID,
		SubsystemID: 1,
		Class:       0x02,
		Subclass:    0x00,
		IRQLine:     irq,
		IRQPin:      1,
		IOBAR:       uint32(ioBase),
		IOSize:      0x100,
		legacyIO:    netdev,
	}
}

func (b *PCIBus) HandleIO(ioExit IOExit) (bool, error) {
	if b == nil {
		return false, nil
	}
	if ioExit.Port >= pciConfigAddressPort && ioExit.Port < pciConfigAddressPort+4 {
		var cfgAddr [4]byte
		binary.LittleEndian.PutUint32(cfgAddr[:], b.configAddress)
		if ioExit.Write {
			writePortBytes(cfgAddr[:], int(ioExit.Port-pciConfigAddressPort), ioExit.Data)
			b.configAddress = binary.LittleEndian.Uint32(cfgAddr[:])
		} else {
			readPortBytes(ioExit.Data, cfgAddr[:], int(ioExit.Port-pciConfigAddressPort))
		}
		return true, nil
	}
	if ioExit.Port >= pciConfigDataPort && ioExit.Port < pciConfigDataPort+4 {
		offset := uint8((b.configAddress & 0xfc) + uint32(ioExit.Port-pciConfigDataPort))
		if ioExit.Write {
			b.writeConfig(offset, ioExit.Data)
		} else {
			b.readConfig(offset, ioExit.Data)
		}
		return true, nil
	}
	for _, dev := range b.devices {
		if dev == nil || dev.legacyIO == nil || dev.IOSize == 0 {
			continue
		}
		if uint32(ioExit.Port) < dev.IOBAR || uint32(ioExit.Port)+uint32(len(ioExit.Data)) > dev.IOBAR+dev.IOSize {
			continue
		}
		offset := uint16(uint32(ioExit.Port) - dev.IOBAR)
		for i := uint32(0); i < ioExit.Count; i++ {
			dataOff := i * uint32(ioExit.Size)
			if ioExit.Write {
				value := readLittleValue(ioExit.Data[dataOff : dataOff+uint32(ioExit.Size)])
				if err := dev.legacyIO.WriteLegacy(offset+uint16(dataOff), int(ioExit.Size), value); err != nil {
					return true, err
				}
				continue
			}
			value, err := dev.legacyIO.ReadLegacy(offset+uint16(dataOff), int(ioExit.Size))
			if err != nil {
				return true, err
			}
			writeLittleValue(ioExit.Data[dataOff:dataOff+uint32(ioExit.Size)], value)
		}
		return true, nil
	}
	return false, nil
}

func (b *PCIBus) readConfig(offset uint8, dst []byte) {
	for i := range dst {
		dst[i] = 0xff
	}
	dev := b.selectedDevice()
	if dev == nil {
		return
	}
	var cfg [256]byte
	dev.buildConfig(cfg[:])
	copy(dst, cfg[offset:])
}

func (b *PCIBus) writeConfig(offset uint8, src []byte) {
	dev := b.selectedDevice()
	if dev == nil {
		return
	}
	for i, value := range src {
		dev.writeConfigByte(uint8(int(offset)+i), value)
	}
}

func (b *PCIBus) selectedDevice() *PCIDevice {
	if b.configAddress&(1<<31) == 0 {
		return nil
	}
	bus := uint8((b.configAddress >> 16) & 0xff)
	device := uint8((b.configAddress >> 11) & 0x1f)
	function := uint8((b.configAddress >> 8) & 0x07)
	for _, dev := range b.devices {
		if dev != nil && dev.Bus == bus && dev.Device == device && dev.Function == function {
			return dev
		}
	}
	return nil
}

func (d *PCIDevice) buildConfig(cfg []byte) {
	binary.LittleEndian.PutUint16(cfg[0x00:0x02], d.VendorID)
	binary.LittleEndian.PutUint16(cfg[0x02:0x04], d.DeviceID)
	binary.LittleEndian.PutUint16(cfg[0x04:0x06], d.Command)
	cfg[0x08] = d.Revision
	cfg[0x09] = d.ProgIF
	cfg[0x0a] = d.Subclass
	cfg[0x0b] = d.Class
	cfg[0x0e] = 0
	if d.barProbeValue {
		binary.LittleEndian.PutUint32(cfg[0x10:0x14], ^(d.IOSize-1)|0x1)
	} else {
		binary.LittleEndian.PutUint32(cfg[0x10:0x14], d.IOBAR|0x1)
	}
	binary.LittleEndian.PutUint16(cfg[0x2c:0x2e], d.VendorID)
	binary.LittleEndian.PutUint16(cfg[0x2e:0x30], d.SubsystemID)
	cfg[0x3c] = d.IRQLine
	cfg[0x3d] = d.IRQPin
}

func (d *PCIDevice) writeConfigByte(offset uint8, value byte) {
	switch offset {
	case 0x04:
		d.Command = (d.Command & 0xff00) | uint16(value)
	case 0x05:
		d.Command = (d.Command & 0x00ff) | uint16(value)<<8
	case 0x10, 0x11, 0x12, 0x13:
		if value == 0xff {
			d.barProbeValue = true
			return
		}
		d.barProbeValue = false
		if offset == 0x10 {
			d.IOBAR = (d.IOBAR & 0xffffff00) | uint32(value&0xfc)
		} else if offset == 0x11 {
			d.IOBAR = (d.IOBAR & 0xffff00ff) | uint32(value)<<8
		} else if offset == 0x12 {
			d.IOBAR = (d.IOBAR & 0xff00ffff) | uint32(value)<<16
		} else if offset == 0x13 {
			d.IOBAR = (d.IOBAR & 0x00ffffff) | uint32(value)<<24
		}
	case 0x3c:
		d.IRQLine = value
	}
}

func handleBootIOWithPCI(uartIO func(IOExit) error, pci *PCIBus, ioExit IOExit) error {
	if handled, err := pci.HandleIO(ioExit); handled || err != nil {
		return err
	}
	return uartIO(ioExit)
}

func readLittleValue(data []byte) uint64 {
	var value uint64
	for i, b := range data {
		value |= uint64(b) << (8 * i)
	}
	return value
}

func writeLittleValue(data []byte, value uint64) {
	for i := range data {
		data[i] = byte(value >> (8 * i))
	}
}

func readPortBytes(dst, src []byte, off int) {
	if off < 0 || off >= len(src) {
		for i := range dst {
			dst[i] = 0xff
		}
		return
	}
	copy(dst, src[off:])
}

func writePortBytes(dst []byte, off int, src []byte) {
	if off < 0 || off >= len(dst) {
		return
	}
	copy(dst[off:], src)
}
