//go:build linux && arm64

package kvm

import (
	"errors"
	"fmt"
	"unsafe"

	hvpkg "github.com/tinyrange/cc/internal/hv"
	"golang.org/x/sys/unix"
)

const (
	arm64VGICDistributorBase    = 0x08000000
	arm64VGICDistributorSize    = 0x00010000
	arm64VGICRedistributorBase  = 0x080a0000
	arm64VGICRedistributorSize  = 0x00020000
	arm64VGICv2DistributorSize  = 0x00001000
	arm64VGICv2CpuInterfaceBase = 0x08010000
	arm64VGICv2CpuInterfaceSize = 0x00002000
	arm64VGICNumIRQs            = 256
)

var (
	errArmVGICUnsupported         = errors.New("kvm: VGIC device unsupported")
	arm64VGICMaintenanceInterrupt = hvpkg.Arm64Interrupt{Type: 1, Num: 9, Flags: 0x4}
)

func (hv *hypervisor) initArm64VGIC(vm *virtualMachine) error {
	if err := hv.initArm64VGICv3(vm); err != nil {
		if errors.Is(err, errArmVGICUnsupported) {
			return hv.initArm64VGICv2(vm)
		}
		return err
	}

	return nil
}

func (hv *hypervisor) initArm64VGICv3(vm *virtualMachine) error {
	dev := kvmCreateDeviceArgs{
		Type:  kvmDevTypeArmVgicV3,
		Flags: 0,
	}

	if err := createDevice(vm.vmFd, &dev); err != nil {
		if errors.Is(err, unix.ENODEV) || errors.Is(err, unix.EOPNOTSUPP) {
			return errArmVGICUnsupported
		}
		return fmt.Errorf("kvm: create VGIC device: %w", err)
	}

	if err := setDeviceAttrU32(vm.vmFd, kvmDevArmVgicGrpNrIrqs, 0, arm64VGICNumIRQs); err != nil {
		return fmt.Errorf("kvm: set VGIC IRQ count: %w", err)
	}

	if err := setDeviceAttrU64(vm.vmFd, kvmDevArmVgicGrpAddr, kvmVgicV3AddrTypeDist, arm64VGICDistributorBase); err != nil {
		return fmt.Errorf("kvm: set VGIC distributor address: %w", err)
	}

	if err := setDeviceAttrU64(vm.vmFd, kvmDevArmVgicGrpAddr, kvmVgicV3AddrTypeRedist, arm64VGICRedistributorBase); err != nil {
		return fmt.Errorf("kvm: set VGIC redistributor address: %w", err)
	}

	if err := setDeviceAttr(vm.vmFd, &kvmDeviceAttr{Group: kvmDevArmVgicGrpCtrl, Attr: kvmDevArmVgicCtrlInit}); err != nil {
		return fmt.Errorf("kvm: initialize VGIC: %w", err)
	}

	vm.arm64GICInfo = hvpkg.Arm64GICInfo{
		Version:              hvpkg.Arm64GICVersion3,
		DistributorBase:      arm64VGICDistributorBase,
		DistributorSize:      arm64VGICDistributorSize,
		RedistributorBase:    arm64VGICRedistributorBase,
		RedistributorSize:    arm64VGICRedistributorSize,
		MaintenanceInterrupt: arm64VGICMaintenanceInterrupt,
	}

	return nil
}

func (hv *hypervisor) initArm64VGICv2(vm *virtualMachine) error {
	dev := kvmCreateDeviceArgs{
		Type:  kvmDevTypeArmVgicV2,
		Flags: 0,
	}

	if err := createDevice(vm.vmFd, &dev); err != nil {
		return fmt.Errorf("kvm: create VGIC device: %w", err)
	}

	if err := setVgicV2Addr(vm.vmFd, kvmVgicV2AddrTypeDist, arm64VGICDistributorBase); err != nil {
		return fmt.Errorf("kvm: set VGIC distributor address: %w", err)
	}

	if err := setVgicV2Addr(vm.vmFd, kvmVgicV2AddrTypeCpu, arm64VGICv2CpuInterfaceBase); err != nil {
		return fmt.Errorf("kvm: set VGIC CPU interface address: %w", err)
	}

	vm.arm64GICInfo = hvpkg.Arm64GICInfo{
		Version:              hvpkg.Arm64GICVersion2,
		DistributorBase:      arm64VGICDistributorBase,
		DistributorSize:      arm64VGICv2DistributorSize,
		CpuInterfaceBase:     arm64VGICv2CpuInterfaceBase,
		CpuInterfaceSize:     arm64VGICv2CpuInterfaceSize,
		MaintenanceInterrupt: arm64VGICMaintenanceInterrupt,
	}

	return nil
}

func setDeviceAttrU32(fd int, group uint32, attr uint64, value uint32) error {
	val := value
	devAttr := kvmDeviceAttr{
		Group: group,
		Attr:  attr,
		Addr:  uint64(uintptr(unsafe.Pointer(&val))),
	}
	return setDeviceAttr(fd, &devAttr)
}

func setDeviceAttrU64(fd int, group uint32, attr uint64, value uint64) error {
	val := value
	devAttr := kvmDeviceAttr{
		Group: group,
		Attr:  attr,
		Addr:  uint64(uintptr(unsafe.Pointer(&val))),
	}
	return setDeviceAttr(fd, &devAttr)
}

func setVgicV2Addr(fd int, addrType uint64, addr uint64) error {
	devAddr := kvmArmDeviceAddr{
		id:   (uint64(kvmArmDeviceVgicV2) << kvmArmDeviceIdShift) | addrType,
		addr: addr,
	}
	return setArmDeviceAddr(fd, &devAddr)
}
