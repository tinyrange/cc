package vm

import (
	"fmt"
	"strings"
)

func unsupportedManagedFeature(runtimeName string, caps guestCapabilities, feature string) error {
	runtimeName = strings.TrimSpace(runtimeName)
	if runtimeName == "" {
		runtimeName = "managed guest"
	}
	feature = strings.TrimSpace(feature)
	if advertisedCapability(caps, feature) {
		return fmt.Errorf("%s runtime advertises %s but no implementation is wired", runtimeName, feature)
	}
	return fmt.Errorf("%s runtime does not support %s yet", runtimeName, feature)
}

func advertisedCapability(caps guestCapabilities, feature string) bool {
	switch strings.TrimSpace(feature) {
	case "filesystem shares":
		return caps.DynamicShares
	case "port forwards":
		return caps.PortForward
	case "alternate images":
		return caps.AlternateImageExec
	case "root snapshots":
		return caps.RootSnapshot
	case "image snapshots":
		return caps.ImageSnapshot
	case "copy into guest":
		return caps.CopyIn
	case "copy out of guest":
		return caps.CopyOut
	case "archive extraction":
		return caps.ArchiveExtract
	default:
		return false
	}
}

func checkAlternateImageExec(provider any) error {
	var caps guestCapabilities
	if provider, ok := provider.(managedCapabilityProvider); ok {
		caps = provider.ManagedCapabilities()
	}
	if caps.AlternateImageExec {
		return nil
	}
	return unsupportedManagedFeature("managed guest", caps, "alternate images")
}
