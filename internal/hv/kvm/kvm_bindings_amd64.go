//go:build linux && amd64

package kvm

import (
	"fmt"
	"unsafe"
)

func getRegisters(vcpuFd int) (kvmRegs, error) {
	var regs kvmRegs

	if _, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmGetRegs), uintptr(unsafe.Pointer(&regs))); err != nil {
		return kvmRegs{}, err
	}

	return regs, nil
}

func setRegisters(vcpuFd int, regs *kvmRegs) error {
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmSetRegs), uintptr(unsafe.Pointer(regs)))
	return err
}

func setTSSAddr(vmFd int, addr uint64) error {
	_, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmSetTssAddr), uintptr(addr))
	return err
}

func getSupportedCpuId(hvFd int) (*kvmCPUID2, error) {
	// get the size of the cpuid structure
	size := unsafe.Sizeof(kvmCPUID2{}) + unsafe.Sizeof(kvmCPUIDEntry2{})*255
	cpuidData := make([]byte, size)
	cpuid := (*kvmCPUID2)(unsafe.Pointer(&cpuidData[0]))
	cpuid.Nr = 255

	if _, err := ioctlWithRetry(uintptr(hvFd), kvmGetSupportedCpuid, uintptr(unsafe.Pointer(cpuid))); err != nil {
		return nil, fmt.Errorf("KVM_GET_SUPPORTED_CPUID: %w", err)
	}

	return cpuid, nil
}

func setVCPUID(vcpuFd int, cpuId *kvmCPUID2) error {
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmSetCpuid2), uintptr(unsafe.Pointer(cpuId)))
	return err
}

func getSRegs(vcpuFd int) (kvmSRegs, error) {
	var sregs kvmSRegs

	if _, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmGetSregs), uintptr(unsafe.Pointer(&sregs))); err != nil {
		return kvmSRegs{}, err
	}

	return sregs, nil
}

func setSRegs(vcpuFd int, sregs *kvmSRegs) error {
	_, err := ioctlWithRetry(uintptr(vcpuFd), uint64(kvmSetSregs), uintptr(unsafe.Pointer(sregs)))
	return err
}
