//go:build linux

package vm

import (
	"net"
	"strings"
	"testing"

	"j5.nz/cc/client"
)

func TestLinuxNetworkRuntimeAddPortForwardValidation(t *testing.T) {
	runtime, err := newLinuxAMD64NetworkRuntime("test-validation", &client.NetworkConfig{Enabled: true})
	if err != nil {
		t.Fatalf("new network runtime: %v", err)
	}
	defer runtime.Close()

	err = runtime.AddPortForward(client.PortForward{HostPort: 0, GuestPort: 8080})
	if err == nil || !strings.Contains(err.Error(), "host port 0 out of range") {
		t.Fatalf("expected host port validation error, got %v", err)
	}

	err = runtime.AddPortForward(client.PortForward{HostPort: reserveRuntimeForwardPort(t), GuestPort: 0})
	if err == nil || !strings.Contains(err.Error(), "guest port 0 out of range") {
		t.Fatalf("expected guest port validation error, got %v", err)
	}
}

func TestLinuxNetworkRuntimeAddPortForwardIsIdempotent(t *testing.T) {
	runtime, err := newLinuxAMD64NetworkRuntime("test-idempotent", &client.NetworkConfig{Enabled: true})
	if err != nil {
		t.Fatalf("new network runtime: %v", err)
	}
	defer runtime.Close()

	forward := client.PortForward{
		Protocol:  "tcp",
		HostAddr:  "127.0.0.1",
		HostPort:  reserveRuntimeForwardPort(t),
		GuestAddr: "10.42.0.2",
		GuestPort: 8080,
	}
	if err := runtime.AddPortForward(forward); err != nil {
		t.Fatalf("first AddPortForward() error = %v", err)
	}
	if err := runtime.AddPortForward(forward); err != nil {
		t.Fatalf("duplicate AddPortForward() error = %v", err)
	}
}

func TestLinuxVirtualSwitchReusesLowestFreeAddress(t *testing.T) {
	sw := &linuxVirtualSwitch{
		leases:    make(map[string]linuxNetworkLease),
		endpoints: make(map[string]*linuxNetworkRuntime),
	}
	one := sw.Register("one")
	sw.Attach(&linuxNetworkRuntime{networkRuntime: &networkRuntime{id: one.id, ip: one.ip}})
	two := sw.Register("two")
	sw.Attach(&linuxNetworkRuntime{networkRuntime: &networkRuntime{id: two.id, ip: two.ip}})
	three := sw.Register("three")
	sw.Attach(&linuxNetworkRuntime{networkRuntime: &networkRuntime{id: three.id, ip: three.ip}})

	if one.ip.String() != "10.42.0.2" || two.ip.String() != "10.42.0.3" || three.ip.String() != "10.42.0.4" {
		t.Fatalf("initial leases = %s %s %s, want .2 .3 .4", one.ip, two.ip, three.ip)
	}

	sw.Unregister("two")
	four := sw.Register("four")
	if four.ip.String() != "10.42.0.3" {
		t.Fatalf("reused lease = %s, want 10.42.0.3", four.ip)
	}
}

func reserveRuntimeForwardPort(t testing.TB) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local tcp port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
