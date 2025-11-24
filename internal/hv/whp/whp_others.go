//go:build windows && !amd64

package whp

import (
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
)

// Architecture implements hv.Hypervisor.
func (h *hypervisor) Architecture() hv.CpuArchitecture {
	return hv.ArchitectureInvalid
}

func (h *hypervisor) archVMInit(vm *virtualMachine, config hv.VMConfig) error {
	return fmt.Errorf("WHPX is only supported on AMD64 architecture")
}

func (h *hypervisor) archVCPUInit(vm *virtualMachine, vcpu *virtualCPU) error {
	return fmt.Errorf("WHPX is only supported on AMD64 architecture")
}
