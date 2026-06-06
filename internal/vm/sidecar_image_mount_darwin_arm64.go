//go:build darwin && arm64

package vm

func sidecarImageMountPath(image string) string {
	return imageMountPath(image)
}
