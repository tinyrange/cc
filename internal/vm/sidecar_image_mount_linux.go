//go:build linux

package vm

import kvmhost "j5.nz/cc/internal/vm/host/kvm"

func sidecarImageMountPath(image string) string {
	return kvmhost.ImageMountPath(image)
}
