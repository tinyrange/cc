//go:build linux && amd64

package kvm

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
	"j5.nz/cc/hypervisor/x86state"
)

// CompleteIO follows the KVM API's immediate_exit completion protocol. Guest
// registers at an IO exit are otherwise allowed to describe an instruction
// which is still pending inside KVM's emulator.
func (v *VM) CompleteIO() error {
	if len(v.vcpus) == 0 || v.vcpus[0] == nil || len(v.vcpus[0].run) == 0 {
		return fmt.Errorf("VCPU is closed")
	}
	cpu := v.vcpus[0]
	run := (*kvmRunData)(unsafe.Pointer(&cpu.run[0]))
	run.immediateExit = 1
	_, err := ioctlRunVCPUInterruptible(uintptr(cpu.fd))
	if errors.Is(err, unix.EINTR) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("KVM did not stop after IO completion")
}

func (v *VM) InterruptState() (map[string]uint64, error) {
	result := map[string]uint64{}
	for id := uint32(0); id < 2; id++ {
		chip, err := getIRQChip(v.vmfd, id)
		if err != nil {
			return nil, err
		}
		for i, name := range []string{"last_irr", "irr", "imr", "isr", "priority_add", "irq_base", "read_reg_select", "poll", "special_mask", "init_state", "auto_eoi", "rotate_on_auto_eoi", "special_fully_nested_mode", "init4", "elcr", "elcr_mask"} {
			result[fmt.Sprintf("pic%d.%s", id, name)] = uint64(chip.Chip[i])
		}
	}
	pit, err := getPIT2(v.vmfd)
	if err != nil {
		return nil, err
	}
	for i, c := range pit.Channels {
		result[fmt.Sprintf("pit%d.count", i)] = uint64(c.Count)
		result[fmt.Sprintf("pit%d.mode", i)] = uint64(c.Mode)
		result[fmt.Sprintf("pit%d.gate", i)] = uint64(c.Gate)
	}
	return result, nil
}

func (v *VM) Registers() (x86state.Registers, error)              { return getRegs(v.vcpufd) }
func (v *VM) SystemRegisters() (x86state.SystemRegisters, error)  { return getSRegs(v.vcpufd) }
func (v *VM) SetRegisters(r x86state.Registers) error             { return setRegs(v.vcpufd, &r) }
func (v *VM) SetSystemRegisters(r x86state.SystemRegisters) error { return setSRegs(v.vcpufd, &r) }
