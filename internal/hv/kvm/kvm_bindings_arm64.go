//go:build linux && arm64

package kvm

import "unsafe"

func getOneReg(vcpuFd int, id uint64, addr unsafe.Pointer) error {
	reg := kvmOneReg{
		id:   id,
		addr: uint64(uintptr(addr)),
	}

	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmGetOneReg), uintptr(unsafe.Pointer(&reg)))
	return err
}

func setOneReg(vcpuFd int, id uint64, addr unsafe.Pointer) error {
	reg := kvmOneReg{
		id:   id,
		addr: uint64(uintptr(addr)),
	}

	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmSetOneReg), uintptr(unsafe.Pointer(&reg)))
	return err
}

func armPreferredTarget(fd int) (kvmVcpuInit, error) {
	var init kvmVcpuInit

	if _, err := ioctlWithRetry(uintptr(fd), uint64(kvmArmPreferredTarget), uintptr(unsafe.Pointer(&init))); err != nil {
		return kvmVcpuInit{}, err
	}

	return init, nil
}

func armVcpuInit(vcpuFd int, init *kvmVcpuInit) error {
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmArmVcpuInitIoctl), uintptr(unsafe.Pointer(init)))
	return err
}

func irqLevel(vmFd int, irqLine uint32, level bool) error {
	var line kvmIRQLevel

	line.IRQOrStatus = irqLine
	if level {
		line.Level = 1
	} else {
		line.Level = 0
	}

	_, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmIrqLine), uintptr(unsafe.Pointer(&line)))
	return err
}
