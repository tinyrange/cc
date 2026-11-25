package kvm

import (
	"fmt"

	"j5.nz/cc/hypervisor/x86state"
)

// SetSyscallRegisters sets the long-mode syscall MSRs on stopped vCPU 0.
func (v *VM) SetSyscallRegisters(registers x86state.SyscallRegisters) error {
	for _, msr := range []struct {
		index uint32
		value uint64
	}{
		{0xc0000081, registers.STAR},
		{0xc0000082, registers.LSTAR},
		{0xc0000084, registers.SFMASK},
		{0xc0000102, registers.KernelGSBase},
	} {
		if err := setVCPUMSR(v.vcpufd, msr.index, msr.value); err != nil {
			return fmt.Errorf("set syscall MSR %#x: %w", msr.index, err)
		}
	}
	return nil
}
