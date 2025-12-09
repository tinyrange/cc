//go:build darwin && arm64

package hvf

import (
	"fmt"

	"github.com/tinyrange/cc/internal/hv"
)

const (
	arm64GICDistributorBase   = 0x08000000
	arm64GICRedistributorBase = 0x080a0000
)

var arm64GICMaintenanceInterrupt = hv.Arm64Interrupt{Type: 1, Num: 9, Flags: 0xF04}

func (h *hypervisor) configureGIC(vm *virtualMachine, config hv.VMConfig) error {
	if vm == nil {
		return fmt.Errorf("hvf: configure GIC on nil VM")
	}

	if !config.NeedsInterruptSupport() {
		return nil
	}

	if hvGicCreate == nil || hvGicConfigCreate == nil {
		return fmt.Errorf("hvf: GIC not supported on this platform")
	}

	if hvGicGetDistributorSize == nil ||
		hvGicGetDistributorBaseAlignment == nil ||
		hvGicGetRedistributorSize == nil ||
		hvGicGetRedistributorBaseAlignment == nil ||
		hvGicGetSpiInterruptRange == nil ||
		hvGicConfigSetDistributorBase == nil ||
		hvGicConfigSetRedistributorBase == nil {
		return fmt.Errorf("hvf: incomplete GIC symbols on this platform")
	}

	var (
		distSize      uint64
		distAlign     uint64
		redistSize    uint64
		redistAlign   uint64
		spiBase       uint32
		spiCount      uint32
		distributor   = uint64(arm64GICDistributorBase)
		redistributor = uint64(arm64GICRedistributorBase)
	)

	if err := hvGicGetDistributorSize(&distSize).toError("hv_gic_get_distributor_size"); err != nil {
		return err
	}
	if err := hvGicGetDistributorBaseAlignment(&distAlign).toError("hv_gic_get_distributor_base_alignment"); err != nil {
		return err
	}
	if err := hvGicGetRedistributorSize(&redistSize).toError("hv_gic_get_redistributor_size"); err != nil {
		return err
	}
	if err := hvGicGetRedistributorBaseAlignment(&redistAlign).toError("hv_gic_get_redistributor_base_alignment"); err != nil {
		return err
	}
	if err := hvGicGetSpiInterruptRange(&spiBase, &spiCount).toError("hv_gic_get_spi_interrupt_range"); err != nil {
		return err
	}

	if distAlign > 0 && distributor%distAlign != 0 {
		return fmt.Errorf("hvf: GIC distributor base %#x not aligned to %#x", distributor, distAlign)
	}
	if redistAlign > 0 && redistributor%redistAlign != 0 {
		return fmt.Errorf("hvf: GIC redistributor base %#x not aligned to %#x", redistributor, redistAlign)
	}

	cfg := hvGicConfigCreate()
	if cfg == 0 {
		return fmt.Errorf("hvf: hv_gic_config_create returned nil")
	}
	if err := hvGicConfigSetDistributorBase(cfg, distributor).toError("hv_gic_config_set_distributor_base"); err != nil {
		return err
	}
	if err := hvGicConfigSetRedistributorBase(cfg, redistributor).toError("hv_gic_config_set_redistributor_base"); err != nil {
		return err
	}
	if err := hvGicCreate(cfg).toError("hv_gic_create"); err != nil {
		return err
	}

	vm.arm64GICInfo = hv.Arm64GICInfo{
		Version:              hv.Arm64GICVersion3,
		DistributorBase:      distributor,
		DistributorSize:      distSize,
		RedistributorBase:    redistributor,
		RedistributorSize:    redistSize,
		MaintenanceInterrupt: arm64GICMaintenanceInterrupt,
	}
	vm.gicSPIBase = spiBase
	vm.gicSPICount = spiCount
	vm.gicConfigured = true

	// Add GIC MMIO emulator - HVF handles GIC state internally but MMIO must be emulated
	if err := vm.addGICEmulator(); err != nil {
		return err
	}

	return nil
}
