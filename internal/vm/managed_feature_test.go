package vm

import (
	"strings"
	"testing"
)

func TestUnsupportedManagedFeatureUsesCapabilitySignal(t *testing.T) {
	err := unsupportedManagedFeature("TestOS", guestCapabilities{RootSnapshot: true}, "root snapshots")
	if err == nil || !strings.Contains(err.Error(), "advertises root snapshots") {
		t.Fatalf("advertised capability error = %v", err)
	}
	err = unsupportedManagedFeature("TestOS", guestCapabilities{}, "root snapshots")
	if err == nil || !strings.Contains(err.Error(), "does not support root snapshots yet") {
		t.Fatalf("unsupported capability error = %v", err)
	}
}

func TestCheckAlternateImageExecUsesCapabilities(t *testing.T) {
	if err := checkAlternateImageExec(nil); err == nil || !strings.Contains(err.Error(), "alternate images") {
		t.Fatalf("nil provider error = %v", err)
	}
	if err := checkAlternateImageExec(staticCapabilityProvider{}); err == nil || !strings.Contains(err.Error(), "alternate images") {
		t.Fatalf("denied provider error = %v", err)
	}
	if err := checkAlternateImageExec(staticCapabilityProvider{caps: guestCapabilities{AlternateImageExec: true}}); err != nil {
		t.Fatalf("allowed provider: %v", err)
	}
}

type staticCapabilityProvider struct {
	caps guestCapabilities
}

func (p staticCapabilityProvider) ManagedCapabilities() guestCapabilities {
	return p.caps
}
