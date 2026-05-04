//go:build linux

package vm

import (
	"net"
	"strings"
	"testing"

	"j5.nz/cc/client"
)

func TestLinuxNetworkRuntimeAddPortForwardValidation(t *testing.T) {
	runtime, err := newLinuxAMD64NetworkRuntime(&client.NetworkConfig{Enabled: true})
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
	runtime, err := newLinuxAMD64NetworkRuntime(&client.NetworkConfig{Enabled: true})
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

func reserveRuntimeForwardPort(t testing.TB) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve local tcp port: %v", err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}
