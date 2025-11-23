//go:build linux && !amd64

package kvm

import (
	"context"
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
)

func (v *virtualCPU) SetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	return fmt.Errorf("kvm: SetRegisters not supported on this architecture")
}

func (v *virtualCPU) GetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	return fmt.Errorf("kvm: GetRegisters not supported on this architecture")
}

func (v *virtualCPU) Run(ctx context.Context) error {
	return fmt.Errorf("kvm: Run not supported on this architecture")
}

func (hv *hypervisor) archVMInit(vmFd int) error {
	return nil
}

func (hv *hypervisor) archVCPUInit(vm *virtualMachine, vcpuFd int) error {
	return nil
}

func (*hypervisor) Architecture() hv.CpuArchitecture {
	return hv.ArchitectureInvalid
}
