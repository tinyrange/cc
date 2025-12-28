//go:build darwin && arm64

package bindings

import "unsafe"

// This file exposes the bound symbols as regular Go functions.
// All functions call MustLoad() before invoking the underlying symbol.

// ---- VM ----

func HvVmGetMaxVcpuCount(maxVCPUCount *uint32) Return {
	MustLoad()
	return hv_vm_get_max_vcpu_count(maxVCPUCount)
}

func HvVmCreate(config VMConfig) Return {
	MustLoad()
	return hv_vm_create(config)
}

func HvVmDestroy() Return {
	MustLoad()
	return hv_vm_destroy()
}

func HvVmMap(addr unsafe.Pointer, ipa IPA, size uintptr, flags MemoryFlags) Return {
	MustLoad()
	return hv_vm_map(addr, ipa, size, flags)
}

func HvVmUnmap(ipa IPA, size uintptr) Return {
	MustLoad()
	return hv_vm_unmap(ipa, size)
}

func HvVmProtect(ipa IPA, size uintptr, flags MemoryFlags) Return {
	MustLoad()
	return hv_vm_protect(ipa, size, flags)
}

func HvVmAllocate(uvap *unsafe.Pointer, size uintptr, flags AllocateFlags) Return {
	MustLoad()
	return hv_vm_allocate(uvap, size, flags)
}

func HvVmDeallocate(uva unsafe.Pointer, size uintptr) Return {
	MustLoad()
	return hv_vm_deallocate(uva, size)
}

// ---- VM config ----

func HvVmConfigCreate() VMConfig {
	MustLoad()
	return hv_vm_config_create()
}

func HvVmConfigGetMaxIpaSize(ipaBitLength *uint32) Return {
	MustLoad()
	return hv_vm_config_get_max_ipa_size(ipaBitLength)
}

func HvVmConfigGetDefaultIpaSize(ipaBitLength *uint32) Return {
	MustLoad()
	return hv_vm_config_get_default_ipa_size(ipaBitLength)
}

func HvVmConfigSetIpaSize(config VMConfig, ipaBitLength uint32) Return {
	MustLoad()
	return hv_vm_config_set_ipa_size(config, ipaBitLength)
}

func HvVmConfigGetIpaSize(config VMConfig, ipaBitLength *uint32) Return {
	MustLoad()
	return hv_vm_config_get_ipa_size(config, ipaBitLength)
}

func HvVmConfigGetEl2Supported(el2Supported *bool) Return {
	MustLoad()
	return hv_vm_config_get_el2_supported(el2Supported)
}

func HvVmConfigGetEl2Enabled(config VMConfig, el2Enabled *bool) Return {
	MustLoad()
	return hv_vm_config_get_el2_enabled(config, el2Enabled)
}

func HvVmConfigSetEl2Enabled(config VMConfig, el2Enabled bool) Return {
	MustLoad()
	return hv_vm_config_set_el2_enabled(config, el2Enabled)
}

// ---- vCPU config ----

func HvVcpuConfigCreate() VcpuConfig {
	MustLoad()
	return hv_vcpu_config_create()
}

func HvVcpuConfigGetFeatureReg(config VcpuConfig, featureReg FeatureReg, value *uint64) Return {
	MustLoad()
	return hv_vcpu_config_get_feature_reg(config, featureReg, value)
}

func HvVcpuConfigGetCcsidrEl1SysRegValues(config VcpuConfig, cacheType CacheType, values *[8]uint64) Return {
	MustLoad()
	return hv_vcpu_config_get_ccsidr_el1_sys_reg_values(config, cacheType, values)
}

// ---- vCPU ----

func HvVcpuCreate(vcpu *VCPU, exit **VcpuExit, config VcpuConfig) Return {
	MustLoad()
	return hv_vcpu_create(vcpu, exit, config)
}

func HvVcpuDestroy(vcpu VCPU) Return {
	MustLoad()
	return hv_vcpu_destroy(vcpu)
}

func HvVcpuGetReg(vcpu VCPU, reg Reg, value *uint64) Return {
	MustLoad()
	return hv_vcpu_get_reg(vcpu, reg, value)
}

func HvVcpuSetReg(vcpu VCPU, reg Reg, value uint64) Return {
	MustLoad()
	return hv_vcpu_set_reg(vcpu, reg, value)
}

func HvVcpuGetSimdFpReg(vcpu VCPU, reg SIMDReg, value *SimdFP) Return {
	MustLoad()
	return hv_vcpu_get_simd_fp_reg(vcpu, reg, value)
}

func HvVcpuSetSimdFpReg(vcpu VCPU, reg SIMDReg, value SimdFP) Return {
	MustLoad()
	return hv_vcpu_set_simd_fp_reg(vcpu, reg, value)
}

func HvVcpuGetSmeState(vcpu VCPU, state *VcpuSMEState) Return {
	MustLoad()
	return hv_vcpu_get_sme_state(vcpu, state)
}

func HvVcpuSetSmeState(vcpu VCPU, state *VcpuSMEState) Return {
	MustLoad()
	return hv_vcpu_set_sme_state(vcpu, state)
}

func HvVcpuGetSmeZReg(vcpu VCPU, reg SMEZReg, value *byte, length uintptr) Return {
	MustLoad()
	return hv_vcpu_get_sme_z_reg(vcpu, reg, value, length)
}

func HvVcpuSetSmeZReg(vcpu VCPU, reg SMEZReg, value *byte, length uintptr) Return {
	MustLoad()
	return hv_vcpu_set_sme_z_reg(vcpu, reg, value, length)
}

func HvVcpuGetSmePReg(vcpu VCPU, reg SMEPReg, value *byte, length uintptr) Return {
	MustLoad()
	return hv_vcpu_get_sme_p_reg(vcpu, reg, value, length)
}

func HvVcpuSetSmePReg(vcpu VCPU, reg SMEPReg, value *byte, length uintptr) Return {
	MustLoad()
	return hv_vcpu_set_sme_p_reg(vcpu, reg, value, length)
}

func HvVcpuGetSmeZaReg(vcpu VCPU, value *byte, length uintptr) Return {
	MustLoad()
	return hv_vcpu_get_sme_za_reg(vcpu, value, length)
}

func HvVcpuSetSmeZaReg(vcpu VCPU, value *byte, length uintptr) Return {
	MustLoad()
	return hv_vcpu_set_sme_za_reg(vcpu, value, length)
}

func HvVcpuGetSmeZt0Reg(vcpu VCPU, value *SMEZT0) Return {
	MustLoad()
	return hv_vcpu_get_sme_zt0_reg(vcpu, value)
}

func HvVcpuSetSmeZt0Reg(vcpu VCPU, value *SMEZT0) Return {
	MustLoad()
	return hv_vcpu_set_sme_zt0_reg(vcpu, value)
}

func HvVcpuGetSysReg(vcpu VCPU, reg SysReg, value *uint64) Return {
	MustLoad()
	return hv_vcpu_get_sys_reg(vcpu, reg, value)
}

func HvVcpuSetSysReg(vcpu VCPU, reg SysReg, value uint64) Return {
	MustLoad()
	return hv_vcpu_set_sys_reg(vcpu, reg, value)
}

func HvVcpuGetPendingInterrupt(vcpu VCPU, typ InterruptType, pending *bool) Return {
	MustLoad()
	return hv_vcpu_get_pending_interrupt(vcpu, typ, pending)
}

func HvVcpuSetPendingInterrupt(vcpu VCPU, typ InterruptType, pending bool) Return {
	MustLoad()
	return hv_vcpu_set_pending_interrupt(vcpu, typ, pending)
}

func HvVcpuGetTrapDebugExceptions(vcpu VCPU, value *bool) Return {
	MustLoad()
	return hv_vcpu_get_trap_debug_exceptions(vcpu, value)
}

func HvVcpuSetTrapDebugExceptions(vcpu VCPU, value bool) Return {
	MustLoad()
	return hv_vcpu_set_trap_debug_exceptions(vcpu, value)
}

func HvVcpuGetTrapDebugRegAccesses(vcpu VCPU, value *bool) Return {
	MustLoad()
	return hv_vcpu_get_trap_debug_reg_accesses(vcpu, value)
}

func HvVcpuSetTrapDebugRegAccesses(vcpu VCPU, value bool) Return {
	MustLoad()
	return hv_vcpu_set_trap_debug_reg_accesses(vcpu, value)
}

func HvVcpuRun(vcpu VCPU) Return {
	MustLoad()
	return hv_vcpu_run(vcpu)
}

func HvVcpusExit(vcpus *VCPU, vcpuCount uint32) Return {
	MustLoad()
	return hv_vcpus_exit(vcpus, vcpuCount)
}

func HvVcpuGetExecTime(vcpu VCPU, time *uint64) Return {
	MustLoad()
	return hv_vcpu_get_exec_time(vcpu, time)
}

func HvVcpuGetVtimerMask(vcpu VCPU, masked *bool) Return {
	MustLoad()
	return hv_vcpu_get_vtimer_mask(vcpu, masked)
}

func HvVcpuSetVtimerMask(vcpu VCPU, masked bool) Return {
	MustLoad()
	return hv_vcpu_set_vtimer_mask(vcpu, masked)
}

func HvVcpuGetVtimerOffset(vcpu VCPU, offset *uint64) Return {
	MustLoad()
	return hv_vcpu_get_vtimer_offset(vcpu, offset)
}

func HvVcpuSetVtimerOffset(vcpu VCPU, offset uint64) Return {
	MustLoad()
	return hv_vcpu_set_vtimer_offset(vcpu, offset)
}

// ---- GIC ----

func HvGicCreate(config GICConfig) Return {
	MustLoad()
	return hv_gic_create(config)
}

func HvGicSetSpi(intid uint32, level bool) Return {
	MustLoad()
	return hv_gic_set_spi(intid, level)
}

func HvGicSendMsi(address IPA, intid uint32) Return {
	MustLoad()
	return hv_gic_send_msi(address, intid)
}

func HvGicGetDistributorReg(reg GICDistributorReg, value *uint64) Return {
	MustLoad()
	return hv_gic_get_distributor_reg(reg, value)
}

func HvGicSetDistributorReg(reg GICDistributorReg, value uint64) Return {
	MustLoad()
	return hv_gic_set_distributor_reg(reg, value)
}

func HvGicGetRedistributorBase(vcpu VCPU, redistributorBaseAddress *IPA) Return {
	MustLoad()
	return hv_gic_get_redistributor_base(vcpu, redistributorBaseAddress)
}

func HvGicGetRedistributorReg(vcpu VCPU, reg GICRedistributorReg, value *uint64) Return {
	MustLoad()
	return hv_gic_get_redistributor_reg(vcpu, reg, value)
}

func HvGicSetRedistributorReg(vcpu VCPU, reg GICRedistributorReg, value uint64) Return {
	MustLoad()
	return hv_gic_set_redistributor_reg(vcpu, reg, value)
}

func HvGicGetIccReg(vcpu VCPU, reg GICICCReg, value *uint64) Return {
	MustLoad()
	return hv_gic_get_icc_reg(vcpu, reg, value)
}

func HvGicSetIccReg(vcpu VCPU, reg GICICCReg, value uint64) Return {
	MustLoad()
	return hv_gic_set_icc_reg(vcpu, reg, value)
}

func HvGicGetIchReg(vcpu VCPU, reg GICICHReg, value *uint64) Return {
	MustLoad()
	return hv_gic_get_ich_reg(vcpu, reg, value)
}

func HvGicSetIchReg(vcpu VCPU, reg GICICHReg, value uint64) Return {
	MustLoad()
	return hv_gic_set_ich_reg(vcpu, reg, value)
}

func HvGicGetIcvReg(vcpu VCPU, reg GICICVReg, value *uint64) Return {
	MustLoad()
	return hv_gic_get_icv_reg(vcpu, reg, value)
}

func HvGicSetIcvReg(vcpu VCPU, reg GICICVReg, value uint64) Return {
	MustLoad()
	return hv_gic_set_icv_reg(vcpu, reg, value)
}

func HvGicGetMsiReg(reg GICMSIReg, value *uint64) Return {
	MustLoad()
	return hv_gic_get_msi_reg(reg, value)
}

func HvGicSetMsiReg(reg GICMSIReg, value uint64) Return {
	MustLoad()
	return hv_gic_set_msi_reg(reg, value)
}

func HvGicSetState(gicStateData unsafe.Pointer, gicStateSize uintptr) Return {
	MustLoad()
	return hv_gic_set_state(gicStateData, gicStateSize)
}

func HvGicReset() Return {
	MustLoad()
	return hv_gic_reset()
}

// ---- GIC config + parameters ----

func HvGicConfigCreate() GICConfig {
	MustLoad()
	return hv_gic_config_create()
}

func HvGicConfigSetDistributorBase(config GICConfig, distributorBaseAddress IPA) Return {
	MustLoad()
	return hv_gic_config_set_distributor_base(config, distributorBaseAddress)
}

func HvGicConfigSetRedistributorBase(config GICConfig, redistributorBaseAddress IPA) Return {
	MustLoad()
	return hv_gic_config_set_redistributor_base(config, redistributorBaseAddress)
}

func HvGicConfigSetMsiRegionBase(config GICConfig, msiRegionBaseAddress IPA) Return {
	MustLoad()
	return hv_gic_config_set_msi_region_base(config, msiRegionBaseAddress)
}

func HvGicConfigSetMsiInterruptRange(config GICConfig, msiIntidBase uint32, msiIntidCount uint32) Return {
	MustLoad()
	return hv_gic_config_set_msi_interrupt_range(config, msiIntidBase, msiIntidCount)
}

func HvGicGetDistributorSize(distributorSize *uintptr) Return {
	MustLoad()
	return hv_gic_get_distributor_size(distributorSize)
}

func HvGicGetDistributorBaseAlignment(distributorBaseAlignment *uintptr) Return {
	MustLoad()
	return hv_gic_get_distributor_base_alignment(distributorBaseAlignment)
}

func HvGicGetRedistributorRegionSize(redistributorRegionSize *uintptr) Return {
	MustLoad()
	return hv_gic_get_redistributor_region_size(redistributorRegionSize)
}

func HvGicGetRedistributorSize(redistributorSize *uintptr) Return {
	MustLoad()
	return hv_gic_get_redistributor_size(redistributorSize)
}

func HvGicGetRedistributorBaseAlignment(redistributorBaseAlignment *uintptr) Return {
	MustLoad()
	return hv_gic_get_redistributor_base_alignment(redistributorBaseAlignment)
}

func HvGicGetMsiRegionSize(msiRegionSize *uintptr) Return {
	MustLoad()
	return hv_gic_get_msi_region_size(msiRegionSize)
}

func HvGicGetMsiRegionBaseAlignment(msiRegionBaseAlignment *uintptr) Return {
	MustLoad()
	return hv_gic_get_msi_region_base_alignment(msiRegionBaseAlignment)
}

func HvGicGetSpiInterruptRange(spiIntidBase *uint32, spiIntidCount *uint32) Return {
	MustLoad()
	return hv_gic_get_spi_interrupt_range(spiIntidBase, spiIntidCount)
}

func HvGicGetIntid(interrupt GICIntID, intid *uint32) Return {
	MustLoad()
	return hv_gic_get_intid(interrupt, intid)
}

// ---- GIC state capture ----

func HvGicStateCreate() GICState {
	MustLoad()
	return hv_gic_state_create()
}

func HvGicStateGetSize(state GICState, size *uintptr) Return {
	MustLoad()
	return hv_gic_state_get_size(state, size)
}

func HvGicStateGetData(state GICState, data unsafe.Pointer) Return {
	MustLoad()
	return hv_gic_state_get_data(state, data)
}

// ---- SME config ----

func HvSmeConfigGetMaxSvlBytes(value *uintptr) Return {
	MustLoad()
	return hv_sme_config_get_max_svl_bytes(value)
}
