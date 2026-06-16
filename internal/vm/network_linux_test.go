//go:build linux

package vm

import (
	"testing"

	"j5.nz/cc/client"
)

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

func TestLinuxPCINetworkRuntimeAttachesToVirtualSwitch(t *testing.T) {
	defaultLinuxVirtualSwitch.Unregister("openbsd")
	defaultLinuxVirtualSwitch.Unregister("freebsd")

	openbsdNet, err := newLinuxPCINetworkRuntime("openbsd", &client.NetworkConfig{Enabled: true})
	if err != nil {
		t.Fatalf("create OpenBSD PCI network runtime: %v", err)
	}
	freebsdNet, err := newLinuxPCINetworkRuntime("freebsd", &client.NetworkConfig{Enabled: true})
	if err != nil {
		_ = openbsdNet.Close()
		t.Fatalf("create FreeBSD PCI network runtime: %v", err)
	}
	t.Cleanup(func() { _ = openbsdNet.Close() })
	t.Cleanup(func() { _ = freebsdNet.Close() })

	if got := openbsdNet.GuestAddress(); got != "10.42.0.2" {
		t.Fatalf("OpenBSD runtime IP = %s, want 10.42.0.2", got)
	}
	if got := freebsdNet.GuestAddress(); got != "10.42.0.3" {
		t.Fatalf("FreeBSD runtime IP = %s, want 10.42.0.3", got)
	}
	if target := defaultLinuxVirtualSwitch.endpointByIP("", openbsdNet.ip); target != openbsdNet {
		t.Fatalf("OpenBSD runtime was not attached to virtual switch")
	}
	if target := defaultLinuxVirtualSwitch.endpointByIP("", freebsdNet.ip); target != freebsdNet {
		t.Fatalf("FreeBSD runtime was not attached to virtual switch")
	}

	if err := openbsdNet.Close(); err != nil {
		t.Fatalf("close OpenBSD runtime: %v", err)
	}
	if target := defaultLinuxVirtualSwitch.endpointByIP("", openbsdNet.ip); target != nil {
		t.Fatalf("OpenBSD runtime remained attached after close")
	}
}
