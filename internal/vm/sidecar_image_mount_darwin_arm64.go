//go:build darwin && arm64

package vm

import hvfhost "j5.nz/cc/internal/vm/host/hvf"

func sidecarImageMountPath(image string) string {
	return hvfhost.ImageMountPath(image)
}
