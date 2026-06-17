//go:build windows && amd64

package vm

import whphost "j5.nz/cc/internal/vm/host/whp"

func sidecarImageMountPath(image string) string {
	return whphost.ImageMountPath(image)
}
