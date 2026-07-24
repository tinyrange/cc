//go:build linux && (amd64 || arm64)

package kvm

import (
	"errors"

	"j5.nz/cc/internal/virtio"
)

func closeFSDevices(fsdevs []*virtio.FS) error {
	var errs []error
	for _, fsdev := range fsdevs {
		if fsdev != nil {
			errs = append(errs, fsdev.Close())
		}
	}
	return errors.Join(errs...)
}

func closeVMWithFS(vm *VM, fsdevs []*virtio.FS) error {
	fsErr := closeFSDevices(fsdevs)
	if vm != nil {
		return errors.Join(fsErr, vm.Close())
	}
	return fsErr
}
