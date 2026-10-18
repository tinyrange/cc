//go:build linux && amd64

package kvm

import (
	"encoding/binary"
	"fmt"
	"j5.nz/cc/hypervisor/x86state"
	"runtime"
	"unsafe"
)

// SupportedHyperVCPUID queries the system interface; it does not enable any
// features or touch vCPU state. Nil means the kernel lacks discovery support.
func (v *VM) SupportedHyperVCPUID() ([]x86state.CPUIDEntry, error) {
	supported, err := checkExtension(v.kvm.fd, 191) // KVM_CAP_SYS_HYPERV_CPUID
	if err != nil || supported == 0 {
		return nil, err
	}
	size := unsafe.Sizeof(kvmCPUID2{}) + unsafe.Sizeof(kvmCPUIDEntry2{})*cpuidMaxEntries
	buf := make([]byte, size)
	table := (*kvmCPUID2)(unsafe.Pointer(&buf[0]))
	table.Nr = cpuidMaxEntries
	_, err = ioctlWithRetry(uintptr(v.kvm.fd), 0xc008aec1, uintptr(unsafe.Pointer(table)))
	runtime.KeepAlive(buf)
	if err != nil {
		return nil, fmt.Errorf("supported Hyper-V CPUID: %w", err)
	}
	if table.Nr > cpuidMaxEntries {
		return nil, fmt.Errorf("invalid Hyper-V CPUID count %d", table.Nr)
	}
	result := make([]x86state.CPUIDEntry, 0, table.Nr)
	for _, e := range cpuidEntries(table) {
		result = append(result, x86state.CPUIDEntry{Function: e.Function, Eax: e.Eax, Ebx: e.Ebx, Ecx: e.Ecx, Edx: e.Edx})
	}
	return result, nil
}

// CompleteHyperVExit rejects userspace hypercalls: this machine has no VMBus
// services. KVM implements clock hypercalls/MSRs itself. Unknown exit kinds
// remain errors rather than being silently accepted.
func (v *VM) CompleteHyperVExit(index int) error {
	if _, err := v.vcpuFD(index); err != nil {
		return err
	}
	cpu := v.vcpus[index]
	run := (*kvmRunData)(unsafe.Pointer(&cpu.run[0]))
	if run.exitReason != 27 {
		return fmt.Errorf("not a Hyper-V exit")
	}
	data := run.anon0[:40]
	kind := binary.LittleEndian.Uint32(data)
	// SynIC configuration notifications need no userspace routing: this
	// machine has no VMBus devices; synthetic timer delivery stays in KVM.
	if kind == 1 && cpu.hypervSynIC {
		return nil
	}
	if kind != 2 {
		return fmt.Errorf("unsupported Hyper-V exit type %d", kind)
	}
	cpu.hypervCalls++
	cpu.hypervLastCall = binary.LittleEndian.Uint64(data[8:])
	binary.LittleEndian.PutUint64(data[16:], 2) // HV_STATUS_INVALID_HYPERCALL_CODE
	return nil
}

// Called with all CPUs paused; ioctl access still runs on the lifetime owner.
func (v *VM) hyperVState(index int, result map[string]uint64) error {
	cpu := v.vcpus[index]
	if !cpu.hyperv {
		return nil
	}
	return v.onCPU(index, func() error {
		for name, msr := range map[string]uint32{"guest_os_id": 0x40000000, "hypercall": 0x40000001, "reference_tsc": 0x40000021, "time_ref_count": 0x40000020} {
			value, err := getVCPUMSR(cpu.fd, msr)
			if err != nil {
				return fmt.Errorf("Hyper-V %s: %w", name, err)
			}
			result[fmt.Sprintf("hyperv%d.%s", index, name)] = value
		}
		if cpu.hypervSynIC {
			for name, msr := range map[string]uint32{"scontrol": 0x40000080, "sint0": 0x40000090, "stimer0_config": 0x400000b0, "stimer0_count": 0x400000b1} {
				value, err := getVCPUMSR(cpu.fd, msr)
				if err != nil {
					return err
				}
				result[fmt.Sprintf("hyperv%d.%s", index, name)] = value
			}
		}
		result[fmt.Sprintf("hyperv%d.rejected_calls", index)] = cpu.hypervCalls
		result[fmt.Sprintf("hyperv%d.last_rejected_call", index)] = cpu.hypervLastCall
		return nil
	})
}

func (v *VM) EnableHyperVSynIC(index int) (bool, error) {
	fd, err := v.vcpuFD(index)
	if err != nil {
		return false, err
	}
	supported, err := checkExtension(v.kvm.fd, 123) // KVM_CAP_HYPERV_SYNIC
	if err != nil || supported == 0 {
		return false, err
	}
	var cap struct {
		Cap, Flags uint32
		Args       [4]uint64
		Pad        [64]byte
	}
	cap.Cap = 123
	_, err = ioctlWithRetry(uintptr(fd), 0x4068aea3, uintptr(unsafe.Pointer(&cap)))
	runtime.KeepAlive(&cap)
	if err != nil {
		return false, fmt.Errorf("enable Hyper-V SynIC: %w", err)
	}
	v.vcpus[index].hypervSynIC = true
	return true, nil
}
