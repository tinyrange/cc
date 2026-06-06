//go:build windows && amd64

package vm

func sidecarImageMountPath(image string) string {
	return windowsImageMountPath(image)
}
