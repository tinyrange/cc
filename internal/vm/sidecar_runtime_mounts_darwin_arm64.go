//go:build darwin && arm64

package vm

import "j5.nz/cc/internal/oci"

func sidecarWithRuntimeMountDirs(image *oci.Image) *oci.Image {
	return withRuntimeMountDirs(image)
}
