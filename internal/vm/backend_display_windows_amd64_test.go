//go:build windows && amd64

package vm

import (
	"slices"
	"testing"

	"j5.nz/cc/client"
)

func TestWindowsAMD64AdvertisesGraphicalVMs(t *testing.T) {
	if !HostCapabilities().SupportsDisplay {
		t.Fatal("windows/amd64 host does not advertise graphical VM support")
	}
	vars := windowsRuntimeConfigVars(&client.DisplayConfig{Width: 1440, Height: 900})
	for _, required := range []string{"CONFIG_DRM_VIRTIO_GPU", "CONFIG_VIRTIO_INPUT", "CONFIG_INPUT_EVDEV"} {
		if !slices.Contains(vars, required) {
			t.Fatalf("display kernel requirements omit %s", required)
		}
	}
}
