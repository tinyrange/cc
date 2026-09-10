//go:build darwin && arm64

package hvf

import (
	"fmt"
	"j5.nz/cc/internal/nvme"
	"j5.nz/cc/internal/virtio"
)

// NewNVMePCI builds a PCI controller over caller-owned random-access storage.
// The caller owns the returned MMIO device and dispatches its accesses while
// this VM is stopped. Interrupt is a GIC interrupt ID, not an SPI index.
func (v *VM) NewNVMePCI(configBase, configSize, windowBase, windowSize, bar uint64, device uint8, interrupt uint32, disk virtio.BlockBackend) (*hvfPCIHost, error) {
	if disk == nil || configBase&0xfffff != 0 || configSize == 0 || configSize > 256<<20 || configSize&0xfffff != 0 || configBase+configSize < configBase || windowSize == 0 || windowBase+windowSize < windowBase || windowBase+windowSize > 1<<32 || configBase < windowBase+windowSize && windowBase < configBase+configSize || device > 31 || interrupt < 32 || interrupt > 255 || bar&0x3fff != 0 || bar < windowBase || bar > windowBase+windowSize || nvme.MMIOSize > windowBase+windowSize-bar {
		return nil, fmt.Errorf("invalid NVMe PCI topology")
	}
	ctrl := nvme.NewController(disk)
	ctrl.Attach(v, v)
	dev := newHVFNVMePCIDevice(device, bar, uint8(interrupt-32), ctrl)
	dev.IRQLine = uint8(interrupt)
	h := newHVFPCIHost(dev)
	h.configBase, h.configSize, h.mmioBase, h.mmioSize = configBase, configSize, windowBase, windowSize
	return h, nil
}

// AddInputPCI attaches an input function to an existing PCI host before Run.
func (v *VM) AddInputPCI(bus interface{}, device uint8, bar uint64, interrupt uint32, pointer bool, width, height uint32) (*virtio.Input, error) {
	h, ok := bus.(*hvfPCIHost)
	if !ok || device > 31 || h.deviceAt(0, device, 0) != nil || interrupt < 32 || interrupt > 255 || bar&0x3fff != 0 || bar < h.mmioBase || bar+0x4000 > h.mmioBase+h.mmioSize {
		return nil, fmt.Errorf("invalid input PCI topology")
	}
	for _, d := range h.devices {
		if bar < d.MMIOBAR+d.MMIOSize && d.MMIOBAR < bar+0x4000 {
			return nil, fmt.Errorf("overlapping input BAR")
		}
	}
	i := virtio.NewKeyboardInput(0, 0, interrupt-32)
	if pointer {
		i = virtio.NewAbsolutePointerInput(0, 0, interrupt-32, width, height)
	}
	i.Attach(v, v)
	transport := &virtio.InputPCI{Input: i}
	h.devices = append(h.devices, &hvfPCIDevice{Device: device, VendorID: 0x1af4, DeviceID: 0x1052, SubsystemID: 0x1100, Class: 9, Subclass: 2, Revision: 1, IRQLine: uint8(interrupt), IRQPin: 1, MMIOBAR: bar, MMIOSize: 0x4000, barValue: uint32(bar), mmio: transport, configure: transport.PCIConfig})
	return i, nil
}
