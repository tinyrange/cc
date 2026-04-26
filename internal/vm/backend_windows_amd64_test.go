//go:build windows && amd64

package vm

import (
	"strings"
	"testing"

	"j5.nz/cc/internal/oci"
)

func TestEnsureWindowsAMD64ImageRejectsKnownForeignArchitecture(t *testing.T) {
	err := ensureWindowsAMD64Image(&oci.Image{Name: "tool", Architecture: "arm64"})
	if err == nil {
		t.Fatal("ensureWindowsAMD64Image() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "windows/amd64 runtime supports only amd64 images") {
		t.Fatalf("ensureWindowsAMD64Image() error = %q", err)
	}
}

func TestEnsureWindowsAMD64ImageAllowsAMD64AndUnknown(t *testing.T) {
	for _, image := range []*oci.Image{
		{Name: "tool", Architecture: "amd64"},
		{Name: "legacy"},
		nil,
	} {
		if err := ensureWindowsAMD64Image(image); err != nil {
			t.Fatalf("ensureWindowsAMD64Image(%#v) error = %v", image, err)
		}
	}
}

func TestWindowsImageMountPathUsesLinuxGuestSeparators(t *testing.T) {
	got := windowsImageMountPath("registry.example/niimath:latest")

	if got != "/.ccx3/images/registry.example_niimath_latest" {
		t.Fatalf("windowsImageMountPath() = %q", got)
	}
	if strings.Contains(got, `\`) {
		t.Fatalf("windowsImageMountPath() used host separators: %q", got)
	}
}
