//go:build linux && arm64

package kvm

import (
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	kvmCreateVM            = 0xae01
	kvmCheckExtension      = 0xae03
	kvmSetUserMemoryRegion = 0x4020ae46
	kvmCreateVCPU          = 0xae41
	kvmRun                 = 0xae80
	kvmGetOneReg           = 0x4010aeab
	kvmSetOneReg           = 0x4010aeac
	kvmArmVCPUInit         = 0x4020aeae
	kvmArmPrefTarget       = 0x8020aeaf
	kvmIRQLine             = 0x4008ae61
	kvmCreateDevice        = 0xc00caee0
	kvmSetDeviceAttr       = 0x4018aee1
)

const (
	kvmCapArmVmIpaSize = 165

	kvmDevTypeArmVgicV2 = 5
	kvmDevTypeArmVgicV3 = 7

	kvmDevArmVgicGrpAddr   = 0
	kvmDevArmVgicGrpNrIrqs = 3
	kvmDevArmVgicGrpCtrl   = 4

	kvmDevArmVgicCtrlInit = 0

	kvmVgicV2AddrTypeDist = 0
	kvmVgicV2AddrTypeCPU  = 1

	kvmVgicV3AddrTypeDist   = 2
	kvmVgicV3AddrTypeRedist = 3
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

func createVM(fd int, ipaBits uint32) (int, error) {
	v1, err := ioctlWithRetry(uintptr(fd), uint64(kvmCreateVM), uintptr(ipaBits))
	if err != nil {
		return 0, err
	}
	return int(v1), nil
}

func createVCPU(fd int, id int) (int, error) {
	v1, err := ioctlWithRetry(uintptr(fd), uint64(kvmCreateVCPU), uintptr(id))
	if err != nil {
		return 0, err
	}
	return int(v1), nil
}

func checkExtension(fd int, cap int) (uint64, error) {
	v1, _, err := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(kvmCheckExtension), uintptr(cap))
	if err != 0 {
		return 0, err
	}
	return uint64(v1), nil
}

func setUserMemoryRegion(fd int, region *kvmUserspaceMemoryRegion) error {
	_, err := ioctlWithRetry(uintptr(fd), uint64(kvmSetUserMemoryRegion), uintptr(unsafe.Pointer(region)))
	return err
}

func armPreferredTarget(fd int) (kvmVcpuInit, error) {
	var init kvmVcpuInit
	if _, err := ioctlWithRetry(uintptr(fd), uint64(kvmArmPrefTarget), uintptr(unsafe.Pointer(&init))); err != nil {
		return kvmVcpuInit{}, err
	}
	return init, nil
}

func armVCPUInit(vcpuFd int, init *kvmVcpuInit) error {
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmArmVCPUInit), uintptr(unsafe.Pointer(init)))
	return err
}

func createDevice(fd int, dev *kvmCreateDeviceArgs) error {
	_, err := ioctlWithRetry(uintptr(fd), uint64(kvmCreateDevice), uintptr(unsafe.Pointer(dev)))
	return err
}

func setDeviceAttr(fd int, attr *kvmDeviceAttr) error {
	_, err := ioctlWithRetry(uintptr(fd), uint64(kvmSetDeviceAttr), uintptr(unsafe.Pointer(attr)))
	return err
}

func irqLevel(vmFd int, irqLine uint32, level bool) error {
	line := kvmIRQLevel{IRQOrStatus: irqLine}
	if level {
		line.Level = 1
	}
	_, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmIRQLine), uintptr(unsafe.Pointer(&line)))
	return err
}

func getOneReg(vcpuFd int, id uint64, addr unsafe.Pointer) error {
	reg := kvmOneReg{id: id, addr: uint64(uintptr(addr))}
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmGetOneReg), uintptr(unsafe.Pointer(&reg)))
	return err
}

func setOneReg(vcpuFd int, id uint64, addr unsafe.Pointer) error {
	reg := kvmOneReg{id: id, addr: uint64(uintptr(addr))}
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmSetOneReg), uintptr(unsafe.Pointer(&reg)))
	return err
}
