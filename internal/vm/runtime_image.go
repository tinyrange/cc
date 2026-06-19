package vm

import (
	"strings"

	"j5.nz/cc/internal/vm/builtin"
)

func sameRuntimeImage(targetImage, runningImage string) bool {
	targetImage = strings.TrimSpace(targetImage)
	runningImage = strings.TrimSpace(runningImage)
	if targetImage == "" || targetImage == runningImage {
		return true
	}
	if builtin.IsGuestImage(targetImage) || builtin.IsGuestImage(runningImage) {
		return canonicalBuiltinRuntimeImage(targetImage) == canonicalBuiltinRuntimeImage(runningImage)
	}
	return false
}

func canonicalBuiltinRuntimeImage(image string) string {
	if profile, ok := builtin.GuestForImage(image); ok {
		return profile.Canonical
	}
	return strings.TrimSpace(image)
}
