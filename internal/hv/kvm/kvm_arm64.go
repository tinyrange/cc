//go:build linux && arm64

package kvm

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"github.com/tinyrange/cc/internal/debug"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/timeslice"
	"golang.org/x/sys/unix"
)

const (
	kvmRegArm64         uint64 = 0x6000000000000000
	kvmRegSizeU64       uint64 = 0x0030000000000000
	kvmRegArmCoproShift        = 16
	kvmRegArmCore       uint64 = 0x0010 << kvmRegArmCoproShift
	kvmRegArm64SysReg   uint64 = 0x0013 << kvmRegArmCoproShift

	kvmRegArm64SysRegOp0Mask  uint64 = 0x000000000000c000
	kvmRegArm64SysRegOp0Shift        = 14
	kvmRegArm64SysRegOp1Mask  uint64 = 0x0000000000003800
	kvmRegArm64SysRegOp1Shift        = 11
	kvmRegArm64SysRegCrnMask  uint64 = 0x0000000000000780
	kvmRegArm64SysRegCrnShift        = 7
	kvmRegArm64SysRegCrmMask  uint64 = 0x0000000000000078
	kvmRegArm64SysRegCrmShift        = 3
	kvmRegArm64SysRegOp2Mask  uint64 = 0x0000000000000007
	kvmRegArm64SysRegOp2Shift        = 0
)

func arm64SysReg(op0, op1, crn, crm, op2 uint64) uint64 {
	return kvmRegArm64 | kvmRegSizeU64 | kvmRegArm64SysReg |
		((op0 << kvmRegArm64SysRegOp0Shift) & kvmRegArm64SysRegOp0Mask) |
		((op1 << kvmRegArm64SysRegOp1Shift) & kvmRegArm64SysRegOp1Mask) |
		((crn << kvmRegArm64SysRegCrnShift) & kvmRegArm64SysRegCrnMask) |
		((crm << kvmRegArm64SysRegCrmShift) & kvmRegArm64SysRegCrmMask) |
		((op2 << kvmRegArm64SysRegOp2Shift) & kvmRegArm64SysRegOp2Mask)
}

func arm64CoreRegister(offsetBytes uintptr) uint64 {
	return kvmRegArm64 | kvmRegSizeU64 | kvmRegArmCore | uint64(offsetBytes/4)
}

var arm64SysRegVbarEl1 = arm64SysReg(3, 0, 12, 0, 0)

var arm64RegisterIDs = func() map[hv.Register]uint64 {
	regs := make(map[hv.Register]uint64, 36)

	for i := 0; i <= 30; i++ {
		reg := hv.Register(int(hv.RegisterARM64X0) + i)
		regs[reg] = arm64CoreRegister(uintptr(i * 8))
	}

	regs[hv.RegisterARM64Sp] = arm64CoreRegister(uintptr(31 * 8))
	regs[hv.RegisterARM64Pc] = arm64CoreRegister(uintptr(32 * 8))
	regs[hv.RegisterARM64Pstate] = arm64CoreRegister(uintptr(33 * 8))
	regs[hv.RegisterARM64Vbar] = arm64SysRegVbarEl1

	return regs
}()

func (v *virtualCPU) SetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	for reg, value := range regs {
		if reg == hv.RegisterARM64GicrBase {
			return fmt.Errorf("kvm: register %v is read-only for architecture arm64", reg)
		}

		kvmReg, ok := arm64RegisterIDs[reg]
		if !ok {
			return fmt.Errorf("kvm: unsupported register %v for architecture arm64", reg)
		}

		raw, ok := value.(hv.Register64)
		if !ok {
			return fmt.Errorf("kvm: invalid register value type %T for %v", value, reg)
		}

		val := uint64(raw)
		if err := setOneReg(v.fd, kvmReg, unsafe.Pointer(&val)); err != nil {
			return fmt.Errorf("kvm: set register %v: %w", reg, err)
		}
	}

	return nil
}

func (v *virtualCPU) GetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	for reg := range regs {
		if reg == hv.RegisterARM64GicrBase {
			info := v.vm.arm64GICInfo
			if info.Version != hv.Arm64GICVersion3 || info.RedistributorBase == 0 {
				return fmt.Errorf("kvm: register %v not available", reg)
			}
			regs[reg] = hv.Register64(info.RedistributorBase)
			continue
		}

		kvmReg, ok := arm64RegisterIDs[reg]
		if !ok {
			return fmt.Errorf("kvm: unsupported register %v for architecture arm64", reg)
		}

		var val uint64
		if err := getOneReg(v.fd, kvmReg, unsafe.Pointer(&val)); err != nil {
			return fmt.Errorf("kvm: get register %v: %w", reg, err)
		}

		regs[reg] = hv.Register64(val)
	}

	return nil
}

func (v *virtualCPU) Run(ctx context.Context) error {
	usingContext := false
	var stopNotify func() bool
	if done := ctx.Done(); done != nil {
		usingContext = true
		tid := unix.Gettid()
		stopNotify = context.AfterFunc(ctx, func() {
			_ = v.RequestImmediateExit(tid)
		})
	}
	if stopNotify != nil {
		defer stopNotify()
	}

	run := (*kvmRunData)(unsafe.Pointer(&v.run[0]))

	// clear immediate_exit in case it was set
	run.immediate_exit = 0

	debug.Writef("kvm-arm64.Run run", "vCPU %d running", v.id)

	timeslice.Record(tsKvmHostTime)

	// keep trying to run the vCPU until it exits or an error occurs
	for {
		_, err := ioctl(uintptr(v.fd), uint64(kvmRun), 0)
		// slog.Info("kvm: vCPU run ioctl returned", "err", err)
		if errors.Is(err, unix.EINTR) {
			if usingContext && (errors.Is(ctx.Err(), context.Canceled) ||
				errors.Is(ctx.Err(), context.DeadlineExceeded)) {
				return ctx.Err()
			}

			continue
		} else if err != nil {
			return fmt.Errorf("kvm: run vCPU %d: %w", v.id, err)
		}

		break
	}

	reason := kvmExitReason(run.exit_reason)

	exitCtx := &exitContext{
		timeslice: timeslice.InvalidTimesliceID,
	}

	switch reason {
	case kvmExitInternalError:
		err := (*internalError)(unsafe.Pointer(&run.anon0[0]))
		return fmt.Errorf("kvm: vCPU %d exited with internal error: %s", v.id, err.Suberror)
	case kvmExitMmio:
		mmioData := (*kvmExitMMIOData)(unsafe.Pointer(&run.anon0[0]))

		if err := v.handleMMIO(exitCtx, mmioData); err != nil {
			return fmt.Errorf("handle MMIO: %w", err)
		}
	case kvmExitSystemEvent:
		system := (*kvmSystemEvent)(unsafe.Pointer(&run.anon0[0]))
		if system.typ == uint32(kvmSystemEventShutdown) {
			return hv.ErrVMHalted
		} else if system.typ == uint32(kvmSystemEventReset) {
			return hv.ErrGuestRequestedReboot
		}
		return fmt.Errorf("kvm: vCPU %d exited with system event %d", v.id, system.typ)
	default:
		return fmt.Errorf("kvm: vCPU %d exited with reason %s", v.id, reason)
	}

	if exitCtx.timeslice != timeslice.InvalidTimesliceID {
		timeslice.Record(exitCtx.timeslice)
	}

	return nil
}

func (v *virtualCPU) handleMMIO(exitCtx *exitContext, mmioData *kvmExitMMIOData) error {
	for _, dev := range v.vm.devices {
		if kvmMmioDevice, ok := dev.(hv.MemoryMappedIODevice); ok {
			addr := mmioData.physAddr
			size := uint32(len(mmioData.data))
			regions := kvmMmioDevice.MMIORegions()
			for _, region := range regions {
				if addr >= region.Address && addr+uint64(size) <= region.Address+region.Size {
					data := mmioData.data[:size]

					if mmioData.isWrite == 0 {
						if err := kvmMmioDevice.ReadMMIO(exitCtx, addr, data); err != nil {
							return fmt.Errorf("MMIO read at 0x%016x: %w", addr, err)
						}
					} else {
						if err := kvmMmioDevice.WriteMMIO(exitCtx, addr, data); err != nil {
							return fmt.Errorf("MMIO write at 0x%016x: %w", addr, err)
						}
					}

					return nil
				}
			}
		}
	}

	return fmt.Errorf("no device handles MMIO at 0x%016x", mmioData.physAddr)
}

func (hv *hypervisor) archVMInit(vm *virtualMachine, config hv.VMConfig) error {
	debug.Writef("kvm hypervisor archVMInit", "")

	if !config.NeedsInterruptSupport() {
		return nil
	}

	if err := hv.initArm64VGIC(vm); err != nil {
		return fmt.Errorf("configure VGIC: %w", err)
	}

	return nil
}

// archPostVCPUInit is called after all vCPUs are created.
// On ARM64, we need to finalize the vGIC after vCPUs exist.
func (hv *hypervisor) archPostVCPUInit(vm *virtualMachine, config hv.VMConfig) error {
	debug.Writef("kvm hypervisor archPostVCPUInit", "")

	if !config.NeedsInterruptSupport() {
		return nil
	}

	if err := hv.finalizeArm64VGIC(vm); err != nil {
		return fmt.Errorf("finalize VGIC: %w", err)
	}

	return nil
}

func (hv *hypervisor) archVCPUInit(vm *virtualMachine, vcpuFd int) error {
	debug.Writef("kvm hypervisor archVCPUInit", "vcpuFd: %d", vcpuFd)

	init, err := armPreferredTarget(vm.vmFd)
	if err != nil {
		return fmt.Errorf("getting preferred target: %w", err)
	}

	enableArmVcpuFeature(&init, kvmArmVcpuFeaturePsci02)

	if err := armVcpuInit(vcpuFd, &init); err != nil {
		return fmt.Errorf("initializing vCPU: %w", err)
	}

	return nil
}

func enableArmVcpuFeature(init *kvmVcpuInit, feature uint32) {
	debug.Writef("kvm hypervisor enableArmVcpuFeature", "feature: %d", feature)

	word := feature / 32
	bit := feature % 32

	if word >= kvmArmVcpuInitFeatureWords {
		return
	}

	init.Features[word] |= 1 << bit
}

func (*hypervisor) Architecture() hv.CpuArchitecture {
	return hv.ArchitectureARM64
}

// Snapshot Support

type arm64VcpuSnapshot struct {
	Registers map[hv.Register]uint64
}

type arm64Snapshot struct {
	cpuStates       map[int]arm64VcpuSnapshot
	deviceSnapshots map[string]interface{}
	memory          []byte
	clockData       *kvmClockData
}

func (v *virtualCPU) captureSnapshot() (arm64VcpuSnapshot, error) {
	ret := arm64VcpuSnapshot{
		Registers: make(map[hv.Register]uint64, len(arm64RegisterIDs)),
	}

	regRequest := make(map[hv.Register]hv.RegisterValue, len(arm64RegisterIDs))
	for reg := range arm64RegisterIDs {
		regRequest[reg] = hv.Register64(0)
	}

	if err := v.GetRegisters(regRequest); err != nil {
		return ret, fmt.Errorf("capture registers: %w", err)
	}

	for reg, value := range regRequest {
		ret.Registers[reg] = uint64(value.(hv.Register64))
	}

	return ret, nil
}

func (v *virtualCPU) restoreSnapshot(snap arm64VcpuSnapshot) error {
	regs := make(map[hv.Register]hv.RegisterValue, len(snap.Registers))
	for reg, value := range snap.Registers {
		regs[reg] = hv.Register64(value)
	}

	if err := v.SetRegisters(regs); err != nil {
		return fmt.Errorf("restore registers: %w", err)
	}

	return nil
}

// CaptureSnapshot implements hv.VirtualMachine.
func (v *virtualMachine) CaptureSnapshot() (hv.Snapshot, error) {
	ret := &arm64Snapshot{
		cpuStates:       make(map[int]arm64VcpuSnapshot),
		deviceSnapshots: make(map[string]interface{}),
	}

	for i := range v.vcpus {
		if err := v.VirtualCPUCall(i, func(vcpu hv.VirtualCPU) error {
			state, err := vcpu.(*virtualCPU).captureSnapshot()
			if err != nil {
				return err
			}

			ret.cpuStates[i] = state
			return nil
		}); err != nil {
			return nil, fmt.Errorf("capture vCPU %d snapshot: %w", i, err)
		}
	}

	if clock, err := getClock(v.vmFd); err != nil {
		if !errors.Is(err, unix.ENOTTY) && !errors.Is(err, unix.EINVAL) {
			return nil, fmt.Errorf("capture clock: %w", err)
		}
	} else {
		ret.clockData = &clock
	}

	for _, dev := range v.devices {
		if snapshotter, ok := dev.(hv.DeviceSnapshotter); ok {
			id := snapshotter.DeviceId()
			snap, err := snapshotter.CaptureSnapshot()
			if err != nil {
				return nil, fmt.Errorf("capture device %s snapshot: %w", id, err)
			}
			ret.deviceSnapshots[id] = snap
		}
	}

	if len(v.memory) > 0 {
		ret.memory = make([]byte, len(v.memory))
		copy(ret.memory, v.memory)
	}

	return ret, nil
}

// RestoreSnapshot implements hv.VirtualMachine.
func (v *virtualMachine) RestoreSnapshot(snap hv.Snapshot) error {
	snapshotData, ok := snap.(*arm64Snapshot)
	if !ok {
		return fmt.Errorf("invalid snapshot type")
	}

	if len(v.memory) != len(snapshotData.memory) {
		return fmt.Errorf("snapshot memory size mismatch: got %d bytes, want %d bytes",
			len(snapshotData.memory), len(v.memory))
	}
	if len(v.memory) > 0 {
		copy(v.memory, snapshotData.memory)
	}

	for i := range v.vcpus {
		state, ok := snapshotData.cpuStates[i]
		if !ok {
			return fmt.Errorf("missing vCPU %d state in snapshot", i)
		}

		if err := v.VirtualCPUCall(i, func(vcpu hv.VirtualCPU) error {
			return vcpu.(*virtualCPU).restoreSnapshot(state)
		}); err != nil {
			return fmt.Errorf("restore vCPU %d snapshot: %w", i, err)
		}
	}

	if snapshotData.clockData != nil {
		if err := setClock(v.vmFd, snapshotData.clockData); err != nil {
			return fmt.Errorf("restore clock: %w", err)
		}
	}

	for _, dev := range v.devices {
		if snapshotter, ok := dev.(hv.DeviceSnapshotter); ok {
			id := snapshotter.DeviceId()
			snapData, ok := snapshotData.deviceSnapshots[id]
			if !ok {
				return fmt.Errorf("missing device %s snapshot", id)
			}
			if err := snapshotter.RestoreSnapshot(snapData); err != nil {
				return fmt.Errorf("restore device %s snapshot: %w", id, err)
			}
		}
	}

	return nil
}
