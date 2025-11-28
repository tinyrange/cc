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

func createIRQChip(vmFd int) error {
	_, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmCreateIrqchip), 0)
	return err
}

func createPIT(vmFd int) error {
	var cfg kvmPitConfig
	_, err := ioctlWithRetry(uintptr(vmFd), uint64(kvmCreatePit2), uintptr(unsafe.Pointer(&cfg)))
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

func pulseIRQ(vmFd int, irqLine uint32) error {
	// Set the IRQ line to high
	if err := irqLevel(vmFd, irqLine, true); err != nil {
		return fmt.Errorf("setting IRQ line high: %w", err)
	}

	// Set the IRQ line to low
	if err := irqLevel(vmFd, irqLine, false); err != nil {
		return fmt.Errorf("setting IRQ line low: %w", err)
	}

	return nil
}
