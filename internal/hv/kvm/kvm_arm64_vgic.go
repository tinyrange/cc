//go:build linux && arm64

package kvm

import (
	"errors"
	"fmt"
	"unsafe"

	"github.com/tinyrange/cc/internal/debug"
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
	arm64VGICMaintenanceInterrupt = hvpkg.Arm64Interrupt{Type: 1, Num: 9, Flags: 0xF04}
)

func (hv *hypervisor) initArm64VGIC(vm *virtualMachine) error {
	if err := hv.initArm64VGICv3(vm); err != nil {
		debug.Writef("kvm hypervisor initArm64VGIC v3 failed", "initArm64VGICv3 failed: %v", err)
		if errors.Is(err, errArmVGICUnsupported) {
			debug.Writef("kvm hypervisor initArm64VGIC v2 fallback", "initArm64VGICv3 unsupported, falling back to v2")
			return hv.initArm64VGICv2(vm)
		}
		return err
	}

	debug.Writef("kvm hypervisor initArm64VGIC v3 success", "")

	return nil
}

// finalizeArm64VGIC completes vGIC initialization after vCPUs are created.
// On ARM64, KVM requires at least one vCPU to exist before the vGIC can be finalized.
func (hv *hypervisor) finalizeArm64VGIC(vm *virtualMachine) error {
	if vm.arm64GICInfo.Version == hvpkg.Arm64GICVersionUnknown {
		// vGIC was not configured
		return nil
	}

	if vm.arm64VGICFd == 0 {
		return fmt.Errorf("kvm: vGIC device fd not set")
	}

	if err := setDeviceAttr(vm.arm64VGICFd, &kvmDeviceAttr{Group: kvmDevArmVgicGrpCtrl, Attr: kvmDevArmVgicCtrlInit}); err != nil {
		return fmt.Errorf("kvm: finalize VGIC (version=%d, fd=%d): %w", vm.arm64GICInfo.Version, vm.arm64VGICFd, err)
	}

	return nil
}

func (hv *hypervisor) initArm64VGICv3(vm *virtualMachine) error {
	dev := kvmCreateDeviceArgs{
		Type:  kvmDevTypeArmVgicV3,
		Flags: 0,
	}

	if err := createDevice(vm.vmFd, &dev); err != nil {
		debug.Writef("kvm hypervisor initArm64VGICv3 create device failed", "create device failed: %v", err)
		if errors.Is(err, unix.ENODEV) || errors.Is(err, unix.EOPNOTSUPP) {
			return errArmVGICUnsupported
		}
		return fmt.Errorf("kvm: create VGIC device: %w", err)
	}

	// Store the device fd for later use (finalization and attribute setting)
	vm.arm64VGICFd = int(dev.Fd)

	if err := setDeviceAttrU32(vm.arm64VGICFd, kvmDevArmVgicGrpNrIrqs, 0, arm64VGICNumIRQs); err != nil {
		return fmt.Errorf("kvm: set VGIC IRQ count: %w", err)
	}

	if err := setDeviceAttrU64(vm.arm64VGICFd, kvmDevArmVgicGrpAddr, kvmVgicV3AddrTypeDist, arm64VGICDistributorBase); err != nil {
		return fmt.Errorf("kvm: set VGIC distributor address: %w", err)
	}

	if err := setDeviceAttrU64(vm.arm64VGICFd, kvmDevArmVgicGrpAddr, kvmVgicV3AddrTypeRedist, arm64VGICRedistributorBase); err != nil {
		return fmt.Errorf("kvm: set VGIC redistributor address: %w", err)
	}

	// Note: KVM_DEV_ARM_VGIC_CTRL_INIT is called later in finalizeArm64VGIC
	// after vCPUs are created, as required by the Linux kernel.

	vm.arm64GICInfo = hvpkg.Arm64GICInfo{
		Version:              hvpkg.Arm64GICVersion3,
		DistributorBase:      arm64VGICDistributorBase,
		DistributorSize:      arm64VGICDistributorSize,
		RedistributorBase:    arm64VGICRedistributorBase,
		RedistributorSize:    arm64VGICRedistributorSize,
		MaintenanceInterrupt: arm64VGICMaintenanceInterrupt,
	}

	debug.Writef("kvm hypervisor initArm64VGICv3 success", "vm.arm64GICInfo: %+v", vm.arm64GICInfo)

	return nil
}

func (hv *hypervisor) initArm64VGICv2(vm *virtualMachine) error {
	dev := kvmCreateDeviceArgs{
		Type:  kvmDevTypeArmVgicV2,
		Flags: 0,
	}

	if err := createDevice(vm.vmFd, &dev); err != nil {
		debug.Writef("kvm hypervisor initArm64VGICv2 create device failed", "create device failed: %v", err)
		return fmt.Errorf("kvm: create VGIC device: %w", err)
	}

	debug.Writef("kvm hypervisor initArm64VGICv2 create device success", "dev: %+v", dev)

	// Store the device fd for later use (finalization)
	vm.arm64VGICFd = int(dev.Fd)

	// Set the number of IRQs
	if err := setDeviceAttrU32(vm.arm64VGICFd, kvmDevArmVgicGrpNrIrqs, 0, arm64VGICNumIRQs); err != nil {
		debug.Writef("kvm hypervisor initArm64VGICv2 set VGIC IRQ count failed", "set VGIC IRQ count failed: %v", err)
		return fmt.Errorf("kvm: set VGIC IRQ count: %w", err)
	}

	// Set VGICv2 addresses via device attributes (preferred over legacy KVM_ARM_SET_DEVICE_ADDR)
	if err := setDeviceAttrU64(vm.arm64VGICFd, kvmDevArmVgicGrpAddr, kvmVgicV2AddrTypeDist, arm64VGICDistributorBase); err != nil {
		debug.Writef("kvm hypervisor initArm64VGICv2 set VGIC distributor address failed", "set VGIC distributor address failed: %v", err)
		return fmt.Errorf("kvm: set VGIC distributor address: %w", err)
	}

	if err := setDeviceAttrU64(vm.arm64VGICFd, kvmDevArmVgicGrpAddr, kvmVgicV2AddrTypeCpu, arm64VGICv2CpuInterfaceBase); err != nil {
		debug.Writef("kvm hypervisor initArm64VGICv2 set VGIC CPU interface address failed", "set VGIC CPU interface address failed: %v", err)
		return fmt.Errorf("kvm: set VGIC CPU interface address: %w", err)
	}

	// Note: KVM_DEV_ARM_VGIC_CTRL_INIT is called later in finalizeArm64VGIC
	// after vCPUs are created, as required by the Linux kernel.

	vm.arm64GICInfo = hvpkg.Arm64GICInfo{
		Version:              hvpkg.Arm64GICVersion2,
		DistributorBase:      arm64VGICDistributorBase,
		DistributorSize:      arm64VGICv2DistributorSize,
		CpuInterfaceBase:     arm64VGICv2CpuInterfaceBase,
		CpuInterfaceSize:     arm64VGICv2CpuInterfaceSize,
		MaintenanceInterrupt: arm64VGICMaintenanceInterrupt,
	}

	debug.Writef("kvm hypervisor initArm64VGICv2 success", "vm.arm64GICInfo: %+v", vm.arm64GICInfo)

	return nil
}

func setDeviceAttrU32(fd int, group uint32, attr uint64, value uint32) error {
	debug.Writef("kvm hypervisor setDeviceAttrU32", "fd: %d, group: %d, attr: %d, value: %d", fd, group, attr, value)

	val := value
	devAttr := kvmDeviceAttr{
		Group: group,
		Attr:  attr,
		Addr:  uint64(uintptr(unsafe.Pointer(&val))),
	}
	return setDeviceAttr(fd, &devAttr)
}

func setDeviceAttrU64(fd int, group uint32, attr uint64, value uint64) error {
	debug.Writef("kvm hypervisor setDeviceAttrU64", "fd: %d, group: %d, attr: %d, value: %d", fd, group, attr, value)

	val := value
	devAttr := kvmDeviceAttr{
		Group: group,
		Attr:  attr,
		Addr:  uint64(uintptr(unsafe.Pointer(&val))),
	}
	return setDeviceAttr(fd, &devAttr)
}
