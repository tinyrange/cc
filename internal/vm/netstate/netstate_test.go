package netstate

import (
	"context"
	"testing"

	"j5.nz/cc/client"
)

func TestNetworkInstanceHelpersRequireNetwork(t *testing.T) {
	if err := AddPortForwardToNetwork(nil, client.PortForward{}); err == nil {
		t.Fatalf("AddPortForwardToNetwork nil error = %v", err)
	}
	if err := AllowServiceProxyPortOnNetwork(nil, 8080); err == nil {
		t.Fatalf("AllowServiceProxyPortOnNetwork nil error = %v", err)
	}
}

func TestManagedNetworkStateRequiresNetwork(t *testing.T) {
	state := State{}
	if err := state.AddPortForward(client.PortForward{}); err == nil {
		t.Fatalf("AddPortForward nil error = %v", err)
	}
	if err := state.AllowServiceProxyPort(8080); err == nil {
		t.Fatalf("AllowServiceProxyPort nil error = %v", err)
	}
	if got := state.IPv4(); got != "" {
		t.Fatalf("nil network IPv4 = %q, want empty", got)
	}
}

func TestManagedNetworkStatePortForwardFallback(t *testing.T) {
	state := State{}
	forward := client.PortForward{HostPort: 1000, GuestPort: 2000}
	fallback := &recordingPortForwardFallback{}
	if err := state.AddPortForwardWithFallback(context.Background(), forward, fallback); err != nil {
		t.Fatalf("AddPortForwardWithFallback: %v", err)
	}
	if fallback.calls != 1 || fallback.forward != forward {
		t.Fatalf("fallback = (%d, %+v), want one call with %+v", fallback.calls, fallback.forward, forward)
	}
	if err := state.AddPortForwardWithFallback(context.Background(), forward, nil); err == nil {
		t.Fatalf("nil fallback error = %v", err)
	}
}

func TestManagedNetworkWrapperHelpersRequireNetwork(t *testing.T) {
	if err := AddManagedNetworkPortForward(context.Background(), nil, client.PortForward{}); err == nil {
		t.Fatalf("AddManagedNetworkPortForward nil error = %v", err)
	}
	if err := AllowManagedNetworkServiceProxyPort(context.Background(), nil, 8080); err == nil {
		t.Fatalf("AllowManagedNetworkServiceProxyPort nil error = %v", err)
	}
	if got := IPv4(nil, "10.42.0.99"); got != "10.42.0.99" {
		t.Fatalf("IPv4 explicit = %q, want explicit address", got)
	}
}

func TestManagedNetworkPortForwardWrapperUsesFallback(t *testing.T) {
	fallback := &recordingPortForwardFallback{}
	forward := client.PortForward{HostPort: 1000, GuestPort: 2000}
	if err := AddManagedNetworkPortForwardWithFallback(context.Background(), nil, forward, fallback); err != nil {
		t.Fatalf("AddManagedNetworkPortForwardWithFallback: %v", err)
	}
	if fallback.calls != 1 || fallback.forward != forward {
		t.Fatalf("fallback = (%d, %+v), want one call with %+v", fallback.calls, fallback.forward, forward)
	}
}

func TestManagedNetworkStateIPv4(t *testing.T) {
	if got := (State{IPv4Addr: "10.42.0.99"}).IPv4(); got != "10.42.0.99" {
		t.Fatalf("explicit IPv4 = %q", got)
	}
	if got := (State{Runtime: fakeRuntime{}}).IPv4(); got != "10.42.0.2" {
		t.Fatalf("runtime default IPv4 = %q", got)
	}
}

type recordingPortForwardFallback struct {
	calls   int
	forward client.PortForward
}

func (f *recordingPortForwardFallback) AddPortForward(_ context.Context, forward client.PortForward) error {
	f.calls++
	f.forward = forward
	return nil
}

type fakeRuntime struct{}

func (fakeRuntime) AddPortForward(client.PortForward) error { return nil }

func (fakeRuntime) AllowServiceProxyPort(int) error { return nil }

func (fakeRuntime) GuestAddress() string { return "10.42.0.2" }
