//go:build !linux && !(darwin && arm64) && !(windows && amd64)

package vm

import "path"

func sidecarImageMountPath(image string) string {
	return path.Join("/.ccx3/images", image)
}
