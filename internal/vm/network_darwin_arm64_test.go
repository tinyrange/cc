//go:build darwin && arm64

package vm

import (
	"net"
	"testing"

	"j5.nz/cc/client"
)

func TestDarwinNetworkRuntimeAddPortForwardValidation(t *testing.T) {
	runtime, err := newDarwinARM64NetworkRuntime(&client.NetworkConfig{Enabled: true})
	if err != nil {
		t.Fatalf("new network runtime: %v", err)
	}
	defer runtime.Close()

	if err := runtime.AddPortForward(client.PortForward{HostPort: 0, GuestPort: 8080}); err == nil {
		t.Fatal("AddPortForward(host 0) error = nil, want error")
	}
	if err := runtime.AddPortForward(client.PortForward{HostPort: reserveDarwinRuntimeForwardPort(t), GuestPort: 0}); err == nil {
		t.Fatal("AddPortForward(guest 0) error = nil, want error")
	}
}

func TestDarwinNetworkRuntimeAddPortForwardIsIdempotent(t *testing.T) {
	runtime, err := newDarwinARM64NetworkRuntime(&client.NetworkConfig{Enabled: true})
	if err != nil {
		t.Fatalf("new network runtime: %v", err)
	}
	defer runtime.Close()

	forward := client.PortForward{
		Protocol:  "tcp",
		HostAddr:  "127.0.0.1",
		HostPort:  reserveDarwinRuntimeForwardPort(t),
		GuestPort: 8080,
	}
	if err := runtime.AddPortForward(forward); err != nil {
		t.Fatalf("first AddPortForward() error = %v", err)
	}
	if err := runtime.AddPortForward(forward); err != nil {
		t.Fatalf("duplicate AddPortForward() error = %v", err)
	}
}

func TestDarwinNetworkRuntimeProvidesGuestConfigAndDevice(t *testing.T) {
	runtime, err := newDarwinARM64NetworkRuntime(&client.NetworkConfig{Enabled: true})
	if err != nil {
		t.Fatalf("new network runtime: %v", err)
	}
	defer runtime.Close()

	if runtime.dev == nil {
		t.Fatal("network device is nil")
	}
	cfg := runtime.guestInitConfig()
	if cfg == nil || cfg.Interface != "eth0" || cfg.Address != "10.42.0.2/24" || cfg.Gateway != "10.42.0.1" {
		t.Fatalf("guest config = %#v, want eth0 10.42.0.2/24 via 10.42.0.1", cfg)
	}
}

func reserveDarwinRuntimeForwardPort(t testing.TB) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
