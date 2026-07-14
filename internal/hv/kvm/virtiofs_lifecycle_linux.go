//go:build linux && (amd64 || arm64)

package kvm

import "j5.nz/cc/internal/virtio"

func closeFSDevices(fsdevs []*virtio.FS) {
	for _, fsdev := range fsdevs {
		if fsdev != nil {
			_ = fsdev.Close()
		}
	}
}

func closeVMWithFS(vm *VM, fsdevs []*virtio.FS) {
	closeFSDevices(fsdevs)
	if vm != nil {
		_ = vm.Close()
	}
}
