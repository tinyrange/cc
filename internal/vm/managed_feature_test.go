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

func TestAdvertisedCapabilityCoversManagedFeatureErrors(t *testing.T) {
	caps := guestCapabilities{
		DynamicShares:      true,
		PortForward:        true,
		AlternateImageExec: true,
		RootSnapshot:       true,
		ImageSnapshot:      true,
		CopyIn:             true,
		CopyOut:            true,
		ArchiveExtract:     true,
	}
	for _, feature := range []string{
		"filesystem shares",
		"port forwards",
		"alternate images",
		"root snapshots",
		"image snapshots",
		"copy into guest",
		"copy out of guest",
		"archive extraction",
	} {
		if !advertisedCapability(caps, feature) {
			t.Fatalf("advertisedCapability(%q) = false, want true", feature)
		}
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
