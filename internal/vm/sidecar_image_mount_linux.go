//go:build linux

package vm

func sidecarImageMountPath(image string) string {
	return linuxImageMountPath(image)
}
