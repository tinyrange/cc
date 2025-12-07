//go:build windows && arm64

package whp

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"

	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/whp/bindings"
)

const (
	arm64InstructionSizeBytes = 4
	gic64KAlignment           = 64 * 1024
	whpGicDistributorBase     = 0x08000000
	whpGicDistributorSize     = 0x00010000
	whpGicRedistributorBase   = 0x080a0000
	whpGicRedistributorSize   = 0x00020000
)

type Arm64InterruptVector uint32

type Arm64GicV3Parameters struct {
	// WHV_GUEST_PHYSICAL_ADDRESS GicdBaseAddress;
	GicdBaseAddress bindings.GuestPhysicalAddress
	// WHV_GUEST_PHYSICAL_ADDRESS GitsTranslaterBaseAddress;
	GitsTranslatorBaseAddress bindings.GuestPhysicalAddress
	// UINT32 Reserved;
	Reserved uint32
	// UINT32 GicLpiIntIdBits;
	GicLpiIntIdBits uint32
	// WHV_ARM64_INTERRUPT_VECTOR GicPpiOverflowInterruptFromCntv;
	GicPpiOverflowInterruptFromCntv Arm64InterruptVector
	// WHV_ARM64_INTERRUPT_VECTOR GicPpiPerformanceMonitorsInterrupt;
	GicPpiPerformanceMonitorsInterrupt Arm64InterruptVector
	// UINT32 Reserved1[6];
	Reserved1 [6]uint32
}

type Arm64IcEmulationMode uint32

const (
	Arm64IcEmulationModeNone Arm64IcEmulationMode = iota
	Arm64IcEmulationModeGicV3
)

type Arm64IcParameters struct {
	EmulationMode   Arm64IcEmulationMode
	GicV3Parameters Arm64GicV3Parameters
}

type dataAbortInfo struct {
	sizeBytes int
	write     bool
	target    hv.Register
}

func arm64RegisterFromIndex(idx int) (hv.Register, bool) {
	switch {
	case idx >= 0 && idx <= 30:
		return hv.Register(int(hv.RegisterARM64X0) + idx), true
	case idx == 31:
		return hv.RegisterARM64Sp, true
	default:
		return hv.RegisterInvalid, false
	}
}

func decodeDataAbort(syndrome uint64) (dataAbortInfo, error) {
	const (
		dataAbortISSMask uint64 = (1 << 25) - 1
		isvBit                  = 24
		sasShift                = 22
		sasMask          uint64 = 0x3
		srtShift                = 16
		srtMask          uint64 = 0x1F
		wnrBit                  = 6
	)

	iss := syndrome & dataAbortISSMask
	if ((iss >> isvBit) & 0x1) == 0 {
		return dataAbortInfo{}, fmt.Errorf("hvf: data abort without ISV set (syndrome=0x%x)", syndrome)
	}

	sas := (iss >> sasShift) & sasMask
	size := 1 << sas
	if sas > 3 {
		return dataAbortInfo{}, fmt.Errorf("hvf: invalid SAS value %d", sas)
	}

	srt := int((iss >> srtShift) & srtMask)
	reg, ok := arm64RegisterFromIndex(srt)
	if !ok {
		return dataAbortInfo{}, fmt.Errorf("hvf: unsupported data abort target register index %d", srt)
	}

	write := ((iss >> wnrBit) & 0x1) == 1

	return dataAbortInfo{
		sizeBytes: int(size),
		write:     write,
		target:    reg,
	}, nil
}

func (v *virtualCPU) Run(ctx context.Context) error {
	var exit bindings.RunVPExitContext

	if ctx.Done() != nil {
		stop := context.AfterFunc(ctx, func() {
			_ = bindings.CancelRunVirtualProcessor(v.vm.part, uint32(v.id), 0)
		})
		defer stop()
	}

	if err := bindings.RunVirtualProcessorContext(v.vm.part, uint32(v.id), &exit); err != nil {
		return fmt.Errorf("whp: RunVirtualProcessorContext failed: %w", err)
	}

	switch exit.ExitReason {
	case bindings.WHvRunVpExitReasonArm64Reset:
		resetCtx := exit.Arm64Reset()

		switch resetCtx.ResetType {
		case bindings.Arm64ResetTypePowerOff:
			return hv.ErrVMHalted
		case bindings.Arm64ResetTypeReboot:
			return hv.ErrGuestRequestedReboot
		default:
			return fmt.Errorf("whp: unsupported ARM64 reset type %d", resetCtx.ResetType)
		}
	case bindings.WHvRunVpExitReasonUnmappedGpa:
		mem := exit.MemoryAccess()

		isWrite := mem.Header.InterceptAccessType == bindings.MemoryAccessWrite
		physAddr := mem.Gpa

		decoded, err := decodeDataAbort(mem.Syndrome)
		if err != nil {
			return fmt.Errorf("whp: failed to decode data abort syndrome 0x%X: %w", mem.Syndrome, err)
		}

		dev, err := v.findMMIODevice(physAddr, uint64(decoded.sizeBytes))
		if err != nil {
			return err
		}

		var pendingError error = nil

		data := make([]byte, decoded.sizeBytes)
		if isWrite {
			value, err := v.readRegister(decoded.target)
			if err != nil {
				return err
			}
			for i := 0; i < decoded.sizeBytes; i++ {
				data[i] = byte(value >> (8 * i))
			}

			if err := dev.WriteMMIO(physAddr, data); err != nil {
				pendingError = fmt.Errorf("whp: MMIO write 0x%x (%d bytes): %w", physAddr, decoded.sizeBytes, err)
			}
		} else {
			if err := dev.ReadMMIO(physAddr, data); err != nil {
				pendingError = fmt.Errorf("whp: MMIO read 0x%x (%d bytes): %w", physAddr, decoded.sizeBytes, err)
			}

			var tmp [8]byte
			copy(tmp[:], data)
			value := binary.LittleEndian.Uint64(tmp[:])
			if err := v.writeRegister(decoded.target, value); err != nil {
				return err
			}
		}

		if err := v.advanceProgramCounter(); err != nil {
			return fmt.Errorf("whp: advance PC after MMIO access: %w", err)
		}

		return pendingError
	case bindings.WHvRunVpExitReasonCanceled:
		if err := ctx.Err(); err != nil {
			return err
		}
		return fmt.Errorf("whp: virtual processor canceled without context error")
	default:
		return fmt.Errorf("whp: unsupported vCPU exit reason %s", exit.ExitReason)
	}
}

func (v *virtualCPU) advanceProgramCounter() error {
	pc, err := v.readRegister(hv.RegisterARM64Pc)
	if err != nil {
		return fmt.Errorf("hvf: read PC: %w", err)
	}
	return v.writeRegister(hv.RegisterARM64Pc, pc+arm64InstructionSizeBytes)
}

func (v *virtualCPU) readRegister(reg hv.Register) (uint64, error) {
	regs := map[hv.Register]hv.RegisterValue{
		reg: hv.Register64(0),
	}
	if err := v.GetRegisters(regs); err != nil {
		return 0, fmt.Errorf("whp: read register %v: %w", reg, err)
	}
	return uint64(regs[reg].(hv.Register64)), nil
}

func (v *virtualCPU) writeRegister(reg hv.Register, value uint64) error {
	regs := map[hv.Register]hv.RegisterValue{
		reg: hv.Register64(value),
	}
	return v.SetRegisters(regs)
}

func (v *virtualCPU) findMMIODevice(addr, size uint64) (hv.MemoryMappedIODevice, error) {
	for _, dev := range v.vm.devices {
		mmio, ok := dev.(hv.MemoryMappedIODevice)
		if !ok {
			continue
		}
		for _, region := range mmio.MMIORegions() {
			if addr >= region.Address && addr+size <= region.Address+region.Size {
				return mmio, nil
			}
		}
	}
	return nil, fmt.Errorf("hvf: no MMIO device handles address 0x%x (size=%d)", addr, size)
}

// Architecture implements hv.Hypervisor.
func (h *hypervisor) Architecture() hv.CpuArchitecture {
	return hv.ArchitectureARM64
}

func (h *hypervisor) archVMInit(vm *virtualMachine, config hv.VMConfig) error {
	if whpGicDistributorBase%gic64KAlignment != 0 {
		return fmt.Errorf("failed to set ARM64 IC parameters: GIC distributor base %#x must be 64KB aligned", whpGicDistributorBase)
	}
	if whpGicRedistributorBase%gic64KAlignment != 0 {
		return fmt.Errorf("failed to set ARM64 IC parameters: GIC redistributor base %#x must be 64KB aligned", whpGicRedistributorBase)
	}

	numCPUs := config.CPUCount()
	var gicrRegionSize uint64
	if numCPUs > 0 {
		gicrRegionSize = whpGicRedistributorSize * uint64(numCPUs)
	}

	if err := bindings.SetPartitionPropertyUnsafe(vm.part, bindings.PartitionPropertyCodeArm64IcParameters, Arm64IcParameters{
		EmulationMode: Arm64IcEmulationModeGicV3,
		GicV3Parameters: Arm64GicV3Parameters{
			GicdBaseAddress:                    whpGicDistributorBase,
			GitsTranslatorBaseAddress:          0,
			GicLpiIntIdBits:                    1,
			GicPpiOverflowInterruptFromCntv:    0x14,
			GicPpiPerformanceMonitorsInterrupt: 0x17,
		},
	}); err != nil {
		return fmt.Errorf("failed to set ARM64 IC parameters: %w", err)
	}

	vm.arm64GICInfo = hv.Arm64GICInfo{
		Version:           hv.Arm64GICVersion3,
		DistributorBase:   whpGicDistributorBase,
		DistributorSize:   whpGicDistributorSize,
		RedistributorBase: whpGicRedistributorBase,
		RedistributorSize: gicrRegionSize,
		ItsBase:           0,
		ItsSize:           0,
		MaintenanceInterrupt: hv.Arm64Interrupt{
			Type:  1,
			Num:   9,
			Flags: 0xF04,
		},
	}

	return nil
}

func (h *hypervisor) archVMInitWithMemory(vm *virtualMachine, config hv.VMConfig) error {
	// Currently, there are no architecture-specific initializations needed for AMD64.
	return nil
}

func (h *hypervisor) archVCPUInit(vm *virtualMachine, vcpu *virtualCPU) error {
	gicrBase := whpGicRedistributorBase + uint64(vcpu.id)*whpGicRedistributorSize
	if err := vcpu.SetRegisters(map[hv.Register]hv.RegisterValue{
		hv.RegisterARM64GicrBase: hv.Register64(gicrBase),
	}); err != nil {
		return fmt.Errorf("failed to set ARM64 GICR base: %w", err)
	}
	return nil
}

// SetIRQ asserts an interrupt line. WHP only supports edge-triggered delivery for
// our simple GIC setup; we emulate level semantics by tracking asserted state
// per INTID and only issuing a WHP request on rising edges. Deassert is recorded
// for bookkeeping even though WHP currently has no explicit deassert.
func (v *virtualMachine) SetIRQ(irqLine uint32, level bool) error {
	if v == nil {
		return fmt.Errorf("whp: virtual machine is nil")
	}

	const armIRQTypeShift = 24
	irqType := (irqLine >> armIRQTypeShift) & 0xff
	if irqType == 0 {
		return fmt.Errorf("whp: interrupt type missing in irqLine %#x", irqLine)
	}

	// INTID carried in irqLine low bits; WHP doesn’t take it directly when
	// asserting the line. We still decode it for future pending-state plumb.
	intid := irqLine & 0xffff

	if !v.arm64ShouldFire(intid, level) {
		return nil
	}

	ctrl := bindings.InterruptControl{
		InterruptControl: bindings.MakeInterruptControl2(
			bindings.InterruptTypeFixed,
			true,  // Asserted
			false, // Retarget
		),
		TargetPartition: 0,
		// WHP uses RequestedVector as the INTID when delivering to the vCPU.
		RequestedVector: uint32(intid),
		TargetVtl:       0,
	}

	// TODO: plumb a minimal GIC pending INTID model so the guest can observe
	// intid as pending when it samples the distributor/CPU interface.
	if irqType != 0 {
		_ = irqType
	}
	return bindings.RequestInterrupt(v.part, &ctrl)
}
