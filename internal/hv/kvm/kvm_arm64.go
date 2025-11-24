//go:build linux && arm64

package kvm

import (
	"context"
	"fmt"
	"unsafe"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	kvmRegArm64         uint64 = 0x6000000000000000
	kvmRegSizeU64       uint64 = 0x0030000000000000
	kvmRegArmCoproShift        = 16
	kvmRegArmCore       uint64 = 0x0010 << kvmRegArmCoproShift
)

func arm64CoreRegister(offsetBytes uintptr) uint64 {
	return kvmRegArm64 | kvmRegSizeU64 | kvmRegArmCore | uint64(offsetBytes/4)
}

var arm64RegisterIDs = func() map[hv.Register]uint64 {
	regs := make(map[hv.Register]uint64, 35)

	for i := 0; i <= 30; i++ {
		reg := hv.Register(int(hv.RegisterARM64X0) + i)
		regs[reg] = arm64CoreRegister(uintptr(i * 8))
	}

	regs[hv.RegisterARM64Sp] = arm64CoreRegister(uintptr(31 * 8))
	regs[hv.RegisterARM64Pc] = arm64CoreRegister(uintptr(32 * 8))
	regs[hv.RegisterARM64Pstate] = arm64CoreRegister(uintptr(33 * 8))

	return regs
}()

func (v *virtualCPU) SetRegisters(regs map[hv.Register]hv.RegisterValue) error {
	for reg, value := range regs {
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
	if _, err := ioctlWithRetry(uintptr(v.fd), uint64(kvmRun), 0); err != nil {
		return fmt.Errorf("kvm: run vCPU %d: %w", v.id, err)
	}

	run := (*kvmRunData)(unsafe.Pointer(&v.run[0]))

	reason := kvmExitReason(run.exit_reason)

	switch reason {
	case kvmExitInternalError:
		err := (*internalError)(unsafe.Pointer(&run.anon0[0]))
		return fmt.Errorf("kvm: vCPU %d exited with internal error: %s", v.id, err.Suberror)
	case kvmExitMmio:
		mmioData := (*kvmExitMMIOData)(unsafe.Pointer(&run.anon0[0]))

		return v.handleMMIO(mmioData)
	case kvmExitSystemEvent:
		system := (*kvmSystemEvent)(unsafe.Pointer(&run.anon0[0]))
		if system.typ == uint32(kvmSystemEventShutdown) {
			return hv.ErrVMHalted
		}
		return fmt.Errorf("kvm: vCPU %d exited with system event %d", v.id, system.typ)
	default:
		return fmt.Errorf("kvm: vCPU %d exited with reason %s", v.id, reason)
	}
}

func (v *virtualCPU) handleMMIO(mmioData *kvmExitMMIOData) error {
	for _, dev := range v.vm.devices {
		if kvmMmioDevice, ok := dev.(hv.MemoryMappedIODevice); ok {
			addr := mmioData.physAddr
			size := uint32(len(mmioData.data))
			regions := kvmMmioDevice.MMIORegions()
			for _, region := range regions {
				if addr >= region.Address && addr+uint64(size) <= region.Address+region.Size {
					data := mmioData.data[:size]

					if mmioData.isWrite == 0 {
						if err := kvmMmioDevice.ReadMMIO(addr, data); err != nil {
							return fmt.Errorf("MMIO read at 0x%016x: %w", addr, err)
						}
					} else {
						if err := kvmMmioDevice.WriteMMIO(addr, data); err != nil {
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

func (hv *hypervisor) archVMInit(vmFd int) error {
	return nil
}

func (hv *hypervisor) archVCPUInit(vm *virtualMachine, vcpuFd int) error {
	init, err := armPreferredTarget(hv.fd)
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
