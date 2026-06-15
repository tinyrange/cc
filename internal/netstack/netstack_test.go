package netstack

import (
	"net"
	"testing"
)

func TestAttachNetworkInterfaceGeneratesUnicastHostMAC(t *testing.T) {
	ns := New(nil)
	if err := ns.SetGuestMAC(net.HardwareAddr{0x02, 0x42, 0x0a, 0x2a, 0x00, 0x02}); err != nil {
		t.Fatal(err)
	}
	if _, err := ns.AttachNetworkInterface(); err != nil {
		t.Fatal(err)
	}

	host := macFromUint64(macAddr(ns.hostMAC.Load()))
	if len(host) != 6 {
		t.Fatalf("host mac length = %d", len(host))
	}
	if host[0]&1 != 0 {
		t.Fatalf("host mac is multicast: %s", host.String())
	}
	if host[0]&2 == 0 {
		t.Fatalf("host mac is not locally administered: %s", host.String())
	}
}
