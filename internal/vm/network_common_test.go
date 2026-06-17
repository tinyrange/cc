package vm

import (
	"context"
	"strings"
	"testing"

	"j5.nz/cc/client"
)

func TestNetworkInstanceHelpersRequireNetwork(t *testing.T) {
	if err := addPortForwardToNetwork(nil, client.PortForward{}); err == nil || !strings.Contains(err.Error(), "network is not enabled") {
		t.Fatalf("addPortForwardToNetwork nil error = %v", err)
	}
	if err := allowServiceProxyPortOnNetwork(nil, 8080); err == nil || !strings.Contains(err.Error(), "network is not enabled") {
		t.Fatalf("allowServiceProxyPortOnNetwork nil error = %v", err)
	}
}

func TestManagedNetworkStateRequiresNetwork(t *testing.T) {
	state := managedNetworkState{}
	if err := state.AddPortForward(client.PortForward{}); err == nil || !strings.Contains(err.Error(), "network is not enabled") {
		t.Fatalf("AddPortForward nil error = %v", err)
	}
	if err := state.AllowServiceProxyPort(8080); err == nil || !strings.Contains(err.Error(), "network is not enabled") {
		t.Fatalf("AllowServiceProxyPort nil error = %v", err)
	}
	if got := state.IPv4(); got != "" {
		t.Fatalf("nil network IPv4 = %q, want empty", got)
	}
}

func TestManagedNetworkStatePortForwardFallback(t *testing.T) {
	state := managedNetworkState{}
	forward := client.PortForward{HostPort: 1000, GuestPort: 2000}
	fallback := &recordingPortForwardFallback{}
	if err := state.AddPortForwardWithFallback(context.Background(), forward, fallback); err != nil {
		t.Fatalf("AddPortForwardWithFallback: %v", err)
	}
	if fallback.calls != 1 || fallback.forward != forward {
		t.Fatalf("fallback = (%d, %+v), want one call with %+v", fallback.calls, fallback.forward, forward)
	}
	if err := state.AddPortForwardWithFallback(context.Background(), forward, nil); err == nil || !strings.Contains(err.Error(), "network is not enabled") {
		t.Fatalf("nil fallback error = %v", err)
	}
}

func TestManagedNetworkWrapperHelpersRequireNetwork(t *testing.T) {
	if err := addManagedNetworkPortForward(context.Background(), nil, client.PortForward{}); err == nil || !strings.Contains(err.Error(), "network is not enabled") {
		t.Fatalf("addManagedNetworkPortForward nil error = %v", err)
	}
	if err := allowManagedNetworkServiceProxyPort(context.Background(), nil, 8080); err == nil || !strings.Contains(err.Error(), "network is not enabled") {
		t.Fatalf("allowManagedNetworkServiceProxyPort nil error = %v", err)
	}
	if got := managedNetworkIPv4(nil, "10.42.0.99"); got != "10.42.0.99" {
		t.Fatalf("managedNetworkIPv4 explicit = %q, want explicit address", got)
	}
}

func TestManagedNetworkPortForwardWrapperUsesFallback(t *testing.T) {
	fallback := &recordingPortForwardFallback{}
	forward := client.PortForward{HostPort: 1000, GuestPort: 2000}
	if err := addManagedNetworkPortForwardWithFallback(context.Background(), nil, forward, fallback); err != nil {
		t.Fatalf("addManagedNetworkPortForwardWithFallback: %v", err)
	}
	if fallback.calls != 1 || fallback.forward != forward {
		t.Fatalf("fallback = (%d, %+v), want one call with %+v", fallback.calls, fallback.forward, forward)
	}
}

func TestManagedNetworkStateIPv4(t *testing.T) {
	if got := (managedNetworkState{ipv4: "10.42.0.99"}).IPv4(); got != "10.42.0.99" {
		t.Fatalf("explicit IPv4 = %q", got)
	}
	if got := (managedNetworkState{runtime: &networkRuntime{ip: nil}}).IPv4(); got != "10.42.0.2" {
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
