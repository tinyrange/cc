//go:build linux && amd64

package kvm

import (
	"encoding/binary"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
	"j5.nz/cc/hypervisor/x86state"
)

// CompleteIO follows the KVM API's immediate_exit completion protocol. Guest
// registers at an IO exit are otherwise allowed to describe an instruction
// which is still pending inside KVM's emulator.
func (v *VM) CompleteIO() error { return v.CompleteVCPUIO(0) }

func (v *VM) CompleteVCPUIO(index int) error {
	if index < 0 || index >= len(v.vcpus) || v.vcpus[index] == nil || len(v.vcpus[index].run) == 0 {
		return fmt.Errorf("VCPU is closed")
	}
	cpu := v.vcpus[index]
	run := (*kvmRunData)(unsafe.Pointer(&cpu.run[0]))
	cpu.setImmediateExit(1)
	_, err := ioctlRunVCPUInterruptible(uintptr(cpu.fd))
	if errors.Is(err, unix.EINTR) {
		return nil
	}
	if err != nil {
		return err
	}
	// Completing an OUT under debugger single-step may surface its #DB
	// before immediate_exit. This is still the completed instruction's
	// stop, not permission to execute the following instruction.
	if index == 0 && v.debugSingleStep && run.exitReason == 4 {
		return nil
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
	chip, err := getIRQChip(v.vmfd, 2)
	if err != nil {
		return nil, err
	}
	result["ioapic.irr"] = uint64(binary.LittleEndian.Uint32(chip.Chip[16:]))
	for i := 0; i < 24; i++ {
		result[fmt.Sprintf("ioapic.route%d", i)] = binary.LittleEndian.Uint64(chip.Chip[24+i*8:])
	}
	for id, cpu := range v.vcpus {
		if err := v.hyperVState(id, result); err != nil {
			return nil, err
		}
		var lapic kvmLAPICState
		err := v.onCPU(id, func() error { var e error; lapic, e = getLAPIC(cpu.fd); return e })
		if err != nil {
			return nil, err
		}
		for name, reg := range map[string]int{"id": 0x20, "tpr": 0x80, "ppr": 0xa0, "ldr": 0xd0, "dfr": 0xe0, "svr": 0xf0, "lvt_timer": 0x320, "timer_initial": 0x380, "timer_current": 0x390, "timer_divide": 0x3e0} {
			result[fmt.Sprintf("lapic%d.%s", id, name)] = uint64(binary.LittleEndian.Uint32(lapic.Regs[reg:]))
		}
		for i := 0; i < 8; i++ {
			result[fmt.Sprintf("lapic%d.isr%d", id, i)] = uint64(binary.LittleEndian.Uint32(lapic.Regs[0x100+i*16:]))
			result[fmt.Sprintf("lapic%d.irr%d", id, i)] = uint64(binary.LittleEndian.Uint32(lapic.Regs[0x200+i*16:]))
		}
	}
	pit, err := getPIT2(v.vmfd)
	if err != nil {
		return nil, err
	}
	result["pit.flags"] = uint64(pit.Flags)
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

// Architectural operations are serialized with execution of the selected CPU.
func (v *VM) VCPURegistersState(index int) (x86state.Registers, error) {
	fd, err := v.vcpuFD(index)
	if err != nil {
		return x86state.Registers{}, err
	}
	return getRegs(fd)
}
func (v *VM) VCPUSystemRegisters(index int) (x86state.SystemRegisters, error) {
	fd, err := v.vcpuFD(index)
	if err != nil {
		return x86state.SystemRegisters{}, err
	}
	return getSRegs(fd)
}
func (v *VM) SetVCPURegisters(index int, r x86state.Registers) error {
	fd, err := v.vcpuFD(index)
	if err != nil {
		return err
	}
	return setRegs(fd, &r)
}
func (v *VM) SetVCPUSystemRegisters(index int, r x86state.SystemRegisters) error {
	fd, err := v.vcpuFD(index)
	if err != nil {
		return err
	}
	return setSRegs(fd, &r)
}
func (v *VM) vcpuFD(index int) (int, error) {
	if index < 0 || index >= len(v.vcpus) || v.vcpus[index] == nil || v.vcpus[index].fd < 0 {
		return -1, fmt.Errorf("vcpu %d unavailable", index)
	}
	return v.vcpus[index].fd, nil
}
