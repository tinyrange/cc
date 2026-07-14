//go:build linux && amd64

package kvm

import (
	"testing"

	"j5.nz/cc/internal/managed/machine"
)

func TestNormalizeBSDPCSessionConfigUsesMachineSpec(t *testing.T) {
	cfg := normalizeBSDPCSessionConfig(bsdPCSessionConfig{
		Spec: machine.Spec{
			Guest:    "TestBSD",
			MemoryMB: 768,
			Dmesg:    true,
		},
	})
	if cfg.GuestName != "TestBSD" {
		t.Fatalf("guest name = %q", cfg.GuestName)
	}
	if cfg.MemoryMB != 768 {
		t.Fatalf("memory = %d", cfg.MemoryMB)
	}
	if !cfg.Dmesg {
		t.Fatalf("dmesg was not copied")
	}
}

func TestNormalizeBSDPCSessionConfigUsesSpecDevicePlacement(t *testing.T) {
	cfg := normalizeBSDPCSessionConfig(bsdPCSessionConfig{
		Spec: machine.Spec{
			Devices: []machine.DeviceSpec{
				{Kind: "nvme", Name: "root", Bus: "pci", Slot: 1, IRQ: 10},
				{Kind: "virtio-net", Name: "net0", Bus: "pci", Slot: 4, IOBase: 0x1400, IRQ: 14},
			},
		},
	})
	if cfg.NetPCIDev != 4 || cfg.NetIOBase != 0x1400 || cfg.NetIRQ != 14 {
		t.Fatalf("net placement = dev %d io %#x irq %d", cfg.NetPCIDev, cfg.NetIOBase, cfg.NetIRQ)
	}
}

func TestBSDSessionControlPortUsesMachineSpec(t *testing.T) {
	cfg := bsdPCSessionConfig{Spec: machine.Spec{Control: machine.ControlSpec{Kind: "tcp", Port: 12345}}}
	if got := bsdSessionControlPort(cfg); got != 12345 {
		t.Fatalf("control port = %d, want 12345", got)
	}
	if got := bsdSessionControlPort(bsdPCSessionConfig{}); got != bsdControlPort {
		t.Fatalf("default control port = %d, want %d", got, bsdControlPort)
	}
}
