//go:build darwin && arm64

package vm

import "testing"

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
