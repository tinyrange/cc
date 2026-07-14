//go:build darwin && arm64

package vm

import (
	"testing"

	"j5.nz/cc/client"
)

func TestDarwinVirtualSwitchAllocatesDistinctLeases(t *testing.T) {
	defaultDarwinSidecarSwitch.Unregister("freebsd")
	defaultDarwinSidecarSwitch.Unregister("openbsd")
	leaseA := defaultDarwinSidecarSwitch.Register("freebsd")
	t.Cleanup(func() { defaultDarwinSidecarSwitch.Unregister(leaseA.id) })
	leaseB := defaultDarwinSidecarSwitch.Register("openbsd")
	t.Cleanup(func() { defaultDarwinSidecarSwitch.Unregister(leaseB.id) })

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

func TestDarwinNetworkRuntimeAllocatesDistinctAddresses(t *testing.T) {
	defaultDarwinSidecarSwitch.Unregister("freebsd")
	defaultDarwinSidecarSwitch.Unregister("openbsd")

	freebsdNet, err := newDarwinARM64NetworkRuntime("freebsd", &client.NetworkConfig{Enabled: true})
	if err != nil {
		t.Fatalf("create FreeBSD network runtime: %v", err)
	}
	openbsdNet, err := newDarwinARM64NetworkRuntime("openbsd", &client.NetworkConfig{Enabled: true})
	if err != nil {
		_ = freebsdNet.Close()
		t.Fatalf("create OpenBSD network runtime: %v", err)
	}
	t.Cleanup(func() { _ = freebsdNet.Close() })
	t.Cleanup(func() { _ = openbsdNet.Close() })

	if got := freebsdNet.GuestAddress(); got != "10.42.0.2" {
		t.Fatalf("FreeBSD runtime IP = %s, want 10.42.0.2", got)
	}
	if got := openbsdNet.GuestAddress(); got != "10.42.0.3" {
		t.Fatalf("OpenBSD runtime IP = %s, want 10.42.0.3", got)
	}

	if err := freebsdNet.Close(); err != nil {
		t.Fatalf("close FreeBSD runtime: %v", err)
	}
	nextNet, err := newDarwinARM64NetworkRuntime("netbsd", &client.NetworkConfig{Enabled: true})
	if err != nil {
		t.Fatalf("create NetBSD network runtime: %v", err)
	}
	t.Cleanup(func() { _ = nextNet.Close() })
	if got := nextNet.GuestAddress(); got != "10.42.0.2" {
		t.Fatalf("reused runtime IP = %s, want 10.42.0.2", got)
	}
}
