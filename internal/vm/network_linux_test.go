//go:build linux

package vm

import "testing"

func TestLinuxVirtualSwitchAllocatesDistinctLeases(t *testing.T) {
	defaultLinuxVirtualSwitch.Unregister("freebsd")
	defaultLinuxVirtualSwitch.Unregister("openbsd")
	leaseA := defaultLinuxVirtualSwitch.Register("freebsd")
	t.Cleanup(func() { defaultLinuxVirtualSwitch.Unregister(leaseA.id) })
	leaseB := defaultLinuxVirtualSwitch.Register("openbsd")
	t.Cleanup(func() { defaultLinuxVirtualSwitch.Unregister(leaseB.id) })

	if got := leaseA.ip.String(); got != "10.42.0.2" {
		t.Fatalf("first lease IP = %s, want 10.42.0.2", got)
	}
	if got := leaseA.mac.String(); got != "02:42:0a:2a:00:02" {
		t.Fatalf("first lease MAC = %s, want 02:42:0a:2a:00:02", got)
	}
	if got := leaseB.ip.String(); got != "10.42.0.3" {
		t.Fatalf("second lease IP = %s, want 10.42.0.3", got)
	}
	if got := leaseB.mac.String(); got != "02:42:0a:2a:00:03" {
		t.Fatalf("second lease MAC = %s, want 02:42:0a:2a:00:03", got)
	}
}
