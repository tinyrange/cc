//go:build windows && (amd64 || arm64)

package whp

import (
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
)

type whpVcpuSnapshot struct {
	Registers map[hv.Register]uint64
}

type whpSnapshot struct {
	Arch            hv.CpuArchitecture
	CpuStates       map[int]whpVcpuSnapshot
	DeviceSnapshots map[string]interface{}
	Memory          []byte
}

var whpSnapshotRegisters = map[hv.CpuArchitecture][]hv.Register{
	hv.ArchitectureX86_64: {
		hv.RegisterAMD64Rax,
		hv.RegisterAMD64Rbx,
		hv.RegisterAMD64Rcx,
		hv.RegisterAMD64Rdx,
		hv.RegisterAMD64Rsi,
		hv.RegisterAMD64Rdi,
		hv.RegisterAMD64Rsp,
		hv.RegisterAMD64Rbp,
		hv.RegisterAMD64R8,
		hv.RegisterAMD64R9,
		hv.RegisterAMD64R10,
		hv.RegisterAMD64R11,
		hv.RegisterAMD64R12,
		hv.RegisterAMD64R13,
		hv.RegisterAMD64R14,
		hv.RegisterAMD64R15,
		hv.RegisterAMD64Rip,
		hv.RegisterAMD64Rflags,
	},
	hv.ArchitectureARM64: {
		hv.RegisterARM64X0,
		hv.RegisterARM64X1,
		hv.RegisterARM64X2,
		hv.RegisterARM64X3,
		hv.RegisterARM64X4,
		hv.RegisterARM64X5,
		hv.RegisterARM64X6,
		hv.RegisterARM64X7,
		hv.RegisterARM64X8,
		hv.RegisterARM64X9,
		hv.RegisterARM64X10,
		hv.RegisterARM64X11,
		hv.RegisterARM64X12,
		hv.RegisterARM64X13,
		hv.RegisterARM64X14,
		hv.RegisterARM64X15,
		hv.RegisterARM64X16,
		hv.RegisterARM64X17,
		hv.RegisterARM64X18,
		hv.RegisterARM64X19,
		hv.RegisterARM64X20,
		hv.RegisterARM64X21,
		hv.RegisterARM64X22,
		hv.RegisterARM64X23,
		hv.RegisterARM64X24,
		hv.RegisterARM64X25,
		hv.RegisterARM64X26,
		hv.RegisterARM64X27,
		hv.RegisterARM64X28,
		hv.RegisterARM64Sp,
		hv.RegisterARM64Pc,
		hv.RegisterARM64Pstate,
		hv.RegisterARM64Vbar,
	},
}

func (v *virtualCPU) captureSnapshot(registers []hv.Register) (whpVcpuSnapshot, error) {
	ret := whpVcpuSnapshot{
		Registers: make(map[hv.Register]uint64, len(registers)),
	}

	regReq := make(map[hv.Register]hv.RegisterValue, len(registers))
	for _, reg := range registers {
		regReq[reg] = hv.Register64(0)
	}

	if err := v.GetRegisters(regReq); err != nil {
		return ret, fmt.Errorf("capture registers: %w", err)
	}

	for reg, value := range regReq {
		ret.Registers[reg] = uint64(value.(hv.Register64))
	}

	return ret, nil
}

func (v *virtualCPU) restoreSnapshot(snap whpVcpuSnapshot) error {
	regReq := make(map[hv.Register]hv.RegisterValue, len(snap.Registers))
	for reg, value := range snap.Registers {
		regReq[reg] = hv.Register64(value)
	}

	if err := v.SetRegisters(regReq); err != nil {
		return fmt.Errorf("restore registers: %w", err)
	}

	return nil
}

// CaptureSnapshot implements hv.VirtualMachine.
func (v *virtualMachine) CaptureSnapshot() (hv.Snapshot, error) {
	arch := v.hv.Architecture()
	regs, ok := whpSnapshotRegisters[arch]
	if !ok {
		return nil, fmt.Errorf("whp: snapshot unsupported for architecture %s", arch)
	}

	ret := &whpSnapshot{
		Arch:            arch,
		CpuStates:       make(map[int]whpVcpuSnapshot),
		DeviceSnapshots: make(map[string]interface{}),
	}

	for id := range v.vcpus {
		if err := v.VirtualCPUCall(id, func(vcpu hv.VirtualCPU) error {
			state, err := vcpu.(*virtualCPU).captureSnapshot(regs)
			if err != nil {
				return err
			}
			ret.CpuStates[id] = state
			return nil
		}); err != nil {
			return nil, fmt.Errorf("capture vCPU %d snapshot: %w", id, err)
		}
	}

	for _, dev := range v.devices {
		if snapshotter, ok := dev.(hv.DeviceSnapshotter); ok {
			id := snapshotter.DeviceId()
			snap, err := snapshotter.CaptureSnapshot()
			if err != nil {
				return nil, fmt.Errorf("capture device %s snapshot: %w", id, err)
			}
			ret.DeviceSnapshots[id] = snap
		}
	}

	if v.memory != nil {
		mem := v.memory.Slice()
		ret.Memory = make([]byte, len(mem))
		copy(ret.Memory, mem)
	}

	return ret, nil
}

// RestoreSnapshot implements hv.VirtualMachine.
func (v *virtualMachine) RestoreSnapshot(snap hv.Snapshot) error {
	snapshotData, ok := snap.(*whpSnapshot)
	if !ok {
		return fmt.Errorf("whp: invalid snapshot type")
	}

	if snapshotData.Arch != v.hv.Architecture() {
		return fmt.Errorf("whp: snapshot architecture %s does not match VM architecture %s",
			snapshotData.Arch, v.hv.Architecture())
	}

	if len(snapshotData.Memory) != len(v.memory.Slice()) {
		return fmt.Errorf("whp: snapshot memory size mismatch: got %d bytes, want %d bytes",
			len(snapshotData.Memory), len(v.memory.Slice()))
	}
	copy(v.memory.Slice(), snapshotData.Memory)

	for id := range v.vcpus {
		state, ok := snapshotData.CpuStates[id]
		if !ok {
			return fmt.Errorf("whp: missing vCPU %d state in snapshot", id)
		}
		if err := v.VirtualCPUCall(id, func(vcpu hv.VirtualCPU) error {
			return vcpu.(*virtualCPU).restoreSnapshot(state)
		}); err != nil {
			return fmt.Errorf("restore vCPU %d snapshot: %w", id, err)
		}
	}

	for _, dev := range v.devices {
		if snapshotter, ok := dev.(hv.DeviceSnapshotter); ok {
			id := snapshotter.DeviceId()
			data, ok := snapshotData.DeviceSnapshots[id]
			if !ok {
				return fmt.Errorf("whp: missing device %s snapshot", id)
			}
			if err := snapshotter.RestoreSnapshot(data); err != nil {
				return fmt.Errorf("restore device %s snapshot: %w", id, err)
			}
		}
	}

	return nil
}
