//go:build darwin && arm64

package vm

import (
	"encoding/binary"
	"net"
	"testing"

	"j5.nz/cc/internal/netstack"
)

func TestDarwinSidecarSwitchAllocatesDistinctLeases(t *testing.T) {
	defaultDarwinSidecarSwitch.Unregister("freebsd")
	defaultDarwinSidecarSwitch.Unregister("openbsd")
	leaseA := defaultDarwinSidecarSwitch.Register("freebsd")
	t.Cleanup(func() { defaultDarwinSidecarSwitch.Unregister(leaseA.id) })
	leaseB := defaultDarwinSidecarSwitch.Register("openbsd")
	t.Cleanup(func() { defaultDarwinSidecarSwitch.Unregister(leaseB.id) })

	if got := leaseA.ip.String(); got != "10.42.0.2" {
		t.Fatalf("first lease IP = %s, want 10.42.0.2", got)
	}
	if got := leaseB.ip.String(); got != "10.42.0.3" {
		t.Fatalf("second lease IP = %s, want 10.42.0.3", got)
	}
}

func TestDarwinSidecarSwitchKeepsPendingLeaseForSameID(t *testing.T) {
	defaultDarwinSidecarSwitch.Unregister("freebsd")
	leaseA := defaultDarwinSidecarSwitch.Register("freebsd")
	t.Cleanup(func() { defaultDarwinSidecarSwitch.Unregister(leaseA.id) })
	leaseB := defaultDarwinSidecarSwitch.Register("freebsd")

	if !leaseA.ip.Equal(leaseB.ip) {
		t.Fatalf("pending lease changed from %s to %s", leaseA.ip, leaseB.ip)
	}
	if leaseA.mac.String() != leaseB.mac.String() {
		t.Fatalf("pending lease MAC changed from %s to %s", leaseA.mac, leaseB.mac)
	}
}

func TestDarwinSidecarSwitchDropsSpoofedSources(t *testing.T) {
	switcher := &darwinSidecarSwitch{
		leases:    make(map[string]darwinSidecarLease),
		endpoints: make(map[string]darwinSidecarEndpoint),
	}
	macA := net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0, 2}
	macB := net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0, 3}
	ipA := net.IPv4(10, 42, 0, 2)
	ipB := net.IPv4(10, 42, 0, 3)
	received := 0
	switcher.Attach(darwinSidecarEndpoint{id: "a", ip: ipA, mac: macA})
	switcher.Attach(darwinSidecarEndpoint{id: "b", ip: ipB, mac: macB, rx: func([]byte) { received++ }})

	switcher.Forward("a", darwinSidecarIPv4Frame(macB, macA, ipA))
	if received != 1 {
		t.Fatalf("valid frame deliveries = %d, want 1", received)
	}
	switcher.Forward("a", darwinSidecarIPv4Frame(macB, macA, ipB))
	if received != 1 {
		t.Fatalf("spoofed IPv4 frame reached target: deliveries = %d", received)
	}
	if got := switcher.sourceViolationCount(netstack.SourceIPv4Violation); got != 1 {
		t.Fatalf("IPv4 violation count = %d, want 1", got)
	}

	spoofedMAC := net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0, 4}
	switcher.Forward("a", darwinSidecarIPv4Frame(macB, spoofedMAC, ipA))
	if received != 1 {
		t.Fatalf("spoofed MAC frame reached target: deliveries = %d", received)
	}
	if got := switcher.sourceViolationCount(netstack.SourceMACViolation); got != 1 {
		t.Fatalf("MAC violation count = %d, want 1", got)
	}

	switcher.Forward("a", darwinSidecarEtherTypeFrame(macB, macA, 0x86dd))
	if received != 1 {
		t.Fatalf("unsupported IPv6 frame reached target: deliveries = %d", received)
	}
	if got := switcher.sourceViolationCount(netstack.SourceUnsupportedProtocol); got != 0 {
		t.Fatalf("ordinary unsupported frame violation count = %d, want 0", got)
	}
}

func darwinSidecarEtherTypeFrame(destinationMAC, sourceMAC net.HardwareAddr, etherType uint16) []byte {
	frame := make([]byte, 14)
	copy(frame[0:6], destinationMAC)
	copy(frame[6:12], sourceMAC)
	binary.BigEndian.PutUint16(frame[12:14], etherType)
	return frame
}

func darwinSidecarIPv4Frame(destinationMAC, sourceMAC net.HardwareAddr, sourceIP net.IP) []byte {
	const ethernetHeaderSize = 14
	const ipv4HeaderSize = 20
	frame := make([]byte, ethernetHeaderSize+ipv4HeaderSize)
	copy(frame[0:6], destinationMAC)
	copy(frame[6:12], sourceMAC)
	binary.BigEndian.PutUint16(frame[12:14], 0x0800)
	frame[14] = 0x45
	frame[23] = 1
	copy(frame[26:30], sourceIP.To4())
	copy(frame[30:34], net.IPv4(10, 42, 0, 1).To4())
	return frame
}
