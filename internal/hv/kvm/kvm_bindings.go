//go:build linux

package kvm

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

func ioctl(fd uintptr, request uint64, arg uintptr) (uintptr, error) {
	v1, _, err := unix.Syscall(unix.SYS_IOCTL, fd, uintptr(request), arg)
	if err != 0 {
		return 0, err
	}
	return v1, nil
}

func ioctlWithRetry(fd uintptr, request uint64, arg uintptr) (uintptr, error) {
	for {
		v1, err := ioctl(fd, request, arg)
		if err == unix.EINTR {
			continue
		}
		return v1, err
	}
}

func ioctlInt(ioctl int) func(fd int) (int, error) {
	return func(fd int) (int, error) {
		v, err := ioctlWithRetry(uintptr(fd), uint64(ioctl), 0)
		if err != nil {
			return 0, err
		}
		return int(v), nil
	}
}

var (
	getApiVersion   = ioctlInt(kvmGetApiVersion)
	createVm        = ioctlInt(kvmCreateVm)
	getVcpuMmapSize = ioctlInt(kvmGetVcpuMmapSize)
)

func createVCPU(fd int, id int) (int, error) {
	v1, err := ioctlWithRetry(uintptr(fd), uint64(kvmCreateVcpu), uintptr(id))
	if err != nil {
		return 0, err
	}

	return int(v1), nil
}

func setUserMemoryRegion(fd int, region *kvmUserspaceMemoryRegion) error {
	_, err := ioctlWithRetry(uintptr(fd), uint64(kvmSetUserMemoryRegion), uintptr(unsafe.Pointer(region)))
	return err
}
