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

	// ARM64-specific: GIC state (nil for x86_64)
	Arm64GICState *arm64GICSnapshot
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
		// General purpose registers
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
		hv.RegisterARM64X29, // FP
		hv.RegisterARM64X30, // LR

		// Special registers
		hv.RegisterARM64Sp,
		hv.RegisterARM64Pc,
		hv.RegisterARM64Pstate,

		// System registers
		hv.RegisterARM64Vbar,
		hv.RegisterARM64SctlrEl1,
		hv.RegisterARM64TcrEl1,
		hv.RegisterARM64Ttbr0El1,
		hv.RegisterARM64Ttbr1El1,
		hv.RegisterARM64MairEl1,
		hv.RegisterARM64ElrEl1,
		hv.RegisterARM64SpsrEl1,
		hv.RegisterARM64EsrEl1,
		hv.RegisterARM64FarEl1,
		hv.RegisterARM64SpEl0,
		hv.RegisterARM64SpEl1,
		hv.RegisterARM64CntkctlEl1,
		hv.RegisterARM64CntvCtlEl0,
		hv.RegisterARM64CntvCvalEl0,
		hv.RegisterARM64CpacrEl1,
		hv.RegisterARM64ContextidrEl1,
		hv.RegisterARM64TpidrEl0,
		hv.RegisterARM64TpidrEl1,
		hv.RegisterARM64TpidrroEl0,
		hv.RegisterARM64ParEl1,
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

	v.memMu.Lock()
	if v.memory != nil {
		mem := v.memory.Slice()
		ret.Memory = make([]byte, len(mem))
		copy(ret.Memory, mem)
	}
	v.memMu.Unlock()

	// Capture architecture-specific state
	if err := v.captureArchSnapshot(ret); err != nil {
		return nil, err
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

	v.memMu.Lock()
	if len(snapshotData.Memory) != len(v.memory.Slice()) {
		v.memMu.Unlock()
		return fmt.Errorf("whp: snapshot memory size mismatch: got %d bytes, want %d bytes",
			len(snapshotData.Memory), len(v.memory.Slice()))
	}
	copy(v.memory.Slice(), snapshotData.Memory)
	v.memMu.Unlock()

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

	// Restore architecture-specific state
	if err := v.restoreArchSnapshot(snapshotData); err != nil {
		return err
	}

	return nil
}
