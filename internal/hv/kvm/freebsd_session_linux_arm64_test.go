//go:build linux && arm64

package kvm

import (
	"encoding/binary"
	"testing"

	"j5.nz/cc/internal/arm64vm"
	"j5.nz/cc/internal/nvme"
)

func TestArm64NVMePCIDeviceConfig(t *testing.T) {
	ctrl := nvme.NewController(nil)
	dev := NewArm64NVMePCIDevice(1, arm64vm.NVMeBase, arm64vm.NVMeIRQ, ctrl)
	host := NewArm64PCIHost(dev)

	id := host.readConfig(1<<15, 4)
	if vendor := uint16(id); vendor != pciVendorRedHat {
		t.Fatalf("vendor id = %#x, want %#x", vendor, uint16(pciVendorRedHat))
	}
	if device := uint16(id >> 16); device != pciNVMeDeviceID {
		t.Fatalf("device id = %#x, want %#x", device, uint16(pciNVMeDeviceID))
	}
	if ctrl.Base != arm64vm.NVMeBase {
		t.Fatalf("controller base = %#x, want %#x", ctrl.Base, uint64(arm64vm.NVMeBase))
	}
	if ctrl.IRQ != uint32(arm64vm.NVMeIRQ) {
		t.Fatalf("controller irq = %d, want %d", ctrl.IRQ, arm64vm.NVMeIRQ)
	}

	var cfg [64]byte
	dev.buildConfig(cfg[:])
	if class := cfg[0x0b]; class != 0x01 {
		t.Fatalf("class = %#x, want mass storage", class)
	}
	if subclass := cfg[0x0a]; subclass != 0x08 {
		t.Fatalf("subclass = %#x, want NVM", subclass)
	}
	if progIF := cfg[0x09]; progIF != 0x02 {
		t.Fatalf("prog-if = %#x, want NVMe", progIF)
	}
	if bar := binary.LittleEndian.Uint32(cfg[0x10:0x14]); bar != uint32(arm64vm.NVMeBase&0xfffffff0) {
		t.Fatalf("BAR0 = %#x, want %#x", bar, uint32(arm64vm.NVMeBase&0xfffffff0))
	}
}
