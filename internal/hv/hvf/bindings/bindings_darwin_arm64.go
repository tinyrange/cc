//go:build darwin && arm64

package bindings

import (
	"fmt"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

var (
	loadOnce sync.Once
	loadErr  error

	hypervisorLib uintptr
	libSystemLib  uintptr
)

// Load loads Hypervisor.framework and binds all arm64-exported Hypervisor APIs.
//
// This package intentionally provides very low-level bindings; higher-level safety
// and ergonomics belong in `internal/hv/hvf`.
func Load() error {
	loadOnce.Do(func() {
		// Prefer the system framework path. This is consistent with other purego-based
		// framework loads in this repo (e.g. AppKit.framework).
		var err error
		hypervisorLib, err = purego.Dlopen(
			"/System/Library/Frameworks/Hypervisor.framework/Hypervisor",
			purego.RTLD_GLOBAL|purego.RTLD_LAZY,
		)
		if err != nil {
			loadErr = fmt.Errorf("purego dlopen Hypervisor.framework: %w", err)
			return
		}

		// VM
		purego.RegisterLibFunc(&hv_vm_get_max_vcpu_count, hypervisorLib, "hv_vm_get_max_vcpu_count")
		purego.RegisterLibFunc(&hv_vm_create, hypervisorLib, "hv_vm_create")
		purego.RegisterLibFunc(&hv_vm_destroy, hypervisorLib, "hv_vm_destroy")
		purego.RegisterLibFunc(&hv_vm_map, hypervisorLib, "hv_vm_map")
		purego.RegisterLibFunc(&hv_vm_unmap, hypervisorLib, "hv_vm_unmap")
		purego.RegisterLibFunc(&hv_vm_protect, hypervisorLib, "hv_vm_protect")
		purego.RegisterLibFunc(&hv_vm_allocate, hypervisorLib, "hv_vm_allocate")
		purego.RegisterLibFunc(&hv_vm_deallocate, hypervisorLib, "hv_vm_deallocate")

		// VM config
		purego.RegisterLibFunc(&hv_vm_config_create, hypervisorLib, "hv_vm_config_create")
		purego.RegisterLibFunc(&hv_vm_config_get_max_ipa_size, hypervisorLib, "hv_vm_config_get_max_ipa_size")
		purego.RegisterLibFunc(&hv_vm_config_get_default_ipa_size, hypervisorLib, "hv_vm_config_get_default_ipa_size")
		purego.RegisterLibFunc(&hv_vm_config_set_ipa_size, hypervisorLib, "hv_vm_config_set_ipa_size")
		purego.RegisterLibFunc(&hv_vm_config_get_ipa_size, hypervisorLib, "hv_vm_config_get_ipa_size")
		purego.RegisterLibFunc(&hv_vm_config_get_el2_supported, hypervisorLib, "hv_vm_config_get_el2_supported")
		purego.RegisterLibFunc(&hv_vm_config_get_el2_enabled, hypervisorLib, "hv_vm_config_get_el2_enabled")
		purego.RegisterLibFunc(&hv_vm_config_set_el2_enabled, hypervisorLib, "hv_vm_config_set_el2_enabled")

		// vCPU config
		purego.RegisterLibFunc(&hv_vcpu_config_create, hypervisorLib, "hv_vcpu_config_create")
		purego.RegisterLibFunc(&hv_vcpu_config_get_feature_reg, hypervisorLib, "hv_vcpu_config_get_feature_reg")
		purego.RegisterLibFunc(&hv_vcpu_config_get_ccsidr_el1_sys_reg_values, hypervisorLib, "hv_vcpu_config_get_ccsidr_el1_sys_reg_values")

		// vCPU
		purego.RegisterLibFunc(&hv_vcpu_create, hypervisorLib, "hv_vcpu_create")
		purego.RegisterLibFunc(&hv_vcpu_destroy, hypervisorLib, "hv_vcpu_destroy")
		purego.RegisterLibFunc(&hv_vcpu_get_reg, hypervisorLib, "hv_vcpu_get_reg")
		purego.RegisterLibFunc(&hv_vcpu_set_reg, hypervisorLib, "hv_vcpu_set_reg")
		purego.RegisterLibFunc(&hv_vcpu_get_simd_fp_reg, hypervisorLib, "hv_vcpu_get_simd_fp_reg")
		purego.RegisterLibFunc(&hv_vcpu_set_simd_fp_reg, hypervisorLib, "hv_vcpu_set_simd_fp_reg")
		purego.RegisterLibFunc(&hv_vcpu_get_sme_state, hypervisorLib, "hv_vcpu_get_sme_state")
		purego.RegisterLibFunc(&hv_vcpu_set_sme_state, hypervisorLib, "hv_vcpu_set_sme_state")
		purego.RegisterLibFunc(&hv_vcpu_get_sme_z_reg, hypervisorLib, "hv_vcpu_get_sme_z_reg")
		purego.RegisterLibFunc(&hv_vcpu_set_sme_z_reg, hypervisorLib, "hv_vcpu_set_sme_z_reg")
		purego.RegisterLibFunc(&hv_vcpu_get_sme_p_reg, hypervisorLib, "hv_vcpu_get_sme_p_reg")
		purego.RegisterLibFunc(&hv_vcpu_set_sme_p_reg, hypervisorLib, "hv_vcpu_set_sme_p_reg")
		purego.RegisterLibFunc(&hv_vcpu_get_sme_za_reg, hypervisorLib, "hv_vcpu_get_sme_za_reg")
		purego.RegisterLibFunc(&hv_vcpu_set_sme_za_reg, hypervisorLib, "hv_vcpu_set_sme_za_reg")
		purego.RegisterLibFunc(&hv_vcpu_get_sme_zt0_reg, hypervisorLib, "hv_vcpu_get_sme_zt0_reg")
		purego.RegisterLibFunc(&hv_vcpu_set_sme_zt0_reg, hypervisorLib, "hv_vcpu_set_sme_zt0_reg")
		purego.RegisterLibFunc(&hv_vcpu_get_sys_reg, hypervisorLib, "hv_vcpu_get_sys_reg")
		purego.RegisterLibFunc(&hv_vcpu_set_sys_reg, hypervisorLib, "hv_vcpu_set_sys_reg")
		purego.RegisterLibFunc(&hv_vcpu_get_pending_interrupt, hypervisorLib, "hv_vcpu_get_pending_interrupt")
		purego.RegisterLibFunc(&hv_vcpu_set_pending_interrupt, hypervisorLib, "hv_vcpu_set_pending_interrupt")
		purego.RegisterLibFunc(&hv_vcpu_get_trap_debug_exceptions, hypervisorLib, "hv_vcpu_get_trap_debug_exceptions")
		purego.RegisterLibFunc(&hv_vcpu_set_trap_debug_exceptions, hypervisorLib, "hv_vcpu_set_trap_debug_exceptions")
		purego.RegisterLibFunc(&hv_vcpu_get_trap_debug_reg_accesses, hypervisorLib, "hv_vcpu_get_trap_debug_reg_accesses")
		purego.RegisterLibFunc(&hv_vcpu_set_trap_debug_reg_accesses, hypervisorLib, "hv_vcpu_set_trap_debug_reg_accesses")
		purego.RegisterLibFunc(&hv_vcpu_run, hypervisorLib, "hv_vcpu_run")
		purego.RegisterLibFunc(&hv_vcpus_exit, hypervisorLib, "hv_vcpus_exit")
		purego.RegisterLibFunc(&hv_vcpu_get_exec_time, hypervisorLib, "hv_vcpu_get_exec_time")
		purego.RegisterLibFunc(&hv_vcpu_get_vtimer_mask, hypervisorLib, "hv_vcpu_get_vtimer_mask")
		purego.RegisterLibFunc(&hv_vcpu_set_vtimer_mask, hypervisorLib, "hv_vcpu_set_vtimer_mask")
		purego.RegisterLibFunc(&hv_vcpu_get_vtimer_offset, hypervisorLib, "hv_vcpu_get_vtimer_offset")
		purego.RegisterLibFunc(&hv_vcpu_set_vtimer_offset, hypervisorLib, "hv_vcpu_set_vtimer_offset")

		// GIC
		purego.RegisterLibFunc(&hv_gic_create, hypervisorLib, "hv_gic_create")
		purego.RegisterLibFunc(&hv_gic_set_spi, hypervisorLib, "hv_gic_set_spi")
		purego.RegisterLibFunc(&hv_gic_send_msi, hypervisorLib, "hv_gic_send_msi")
		purego.RegisterLibFunc(&hv_gic_get_distributor_reg, hypervisorLib, "hv_gic_get_distributor_reg")
		purego.RegisterLibFunc(&hv_gic_set_distributor_reg, hypervisorLib, "hv_gic_set_distributor_reg")
		purego.RegisterLibFunc(&hv_gic_get_redistributor_base, hypervisorLib, "hv_gic_get_redistributor_base")
		purego.RegisterLibFunc(&hv_gic_get_redistributor_reg, hypervisorLib, "hv_gic_get_redistributor_reg")
		purego.RegisterLibFunc(&hv_gic_set_redistributor_reg, hypervisorLib, "hv_gic_set_redistributor_reg")
		purego.RegisterLibFunc(&hv_gic_get_icc_reg, hypervisorLib, "hv_gic_get_icc_reg")
		purego.RegisterLibFunc(&hv_gic_set_icc_reg, hypervisorLib, "hv_gic_set_icc_reg")
		purego.RegisterLibFunc(&hv_gic_get_ich_reg, hypervisorLib, "hv_gic_get_ich_reg")
		purego.RegisterLibFunc(&hv_gic_set_ich_reg, hypervisorLib, "hv_gic_set_ich_reg")
		purego.RegisterLibFunc(&hv_gic_get_icv_reg, hypervisorLib, "hv_gic_get_icv_reg")
		purego.RegisterLibFunc(&hv_gic_set_icv_reg, hypervisorLib, "hv_gic_set_icv_reg")
		purego.RegisterLibFunc(&hv_gic_get_msi_reg, hypervisorLib, "hv_gic_get_msi_reg")
		purego.RegisterLibFunc(&hv_gic_set_msi_reg, hypervisorLib, "hv_gic_set_msi_reg")
		purego.RegisterLibFunc(&hv_gic_set_state, hypervisorLib, "hv_gic_set_state")
		purego.RegisterLibFunc(&hv_gic_reset, hypervisorLib, "hv_gic_reset")

		// GIC config + parameters
		purego.RegisterLibFunc(&hv_gic_config_create, hypervisorLib, "hv_gic_config_create")
		purego.RegisterLibFunc(&hv_gic_config_set_distributor_base, hypervisorLib, "hv_gic_config_set_distributor_base")
		purego.RegisterLibFunc(&hv_gic_config_set_redistributor_base, hypervisorLib, "hv_gic_config_set_redistributor_base")
		purego.RegisterLibFunc(&hv_gic_config_set_msi_region_base, hypervisorLib, "hv_gic_config_set_msi_region_base")
		purego.RegisterLibFunc(&hv_gic_config_set_msi_interrupt_range, hypervisorLib, "hv_gic_config_set_msi_interrupt_range")
		purego.RegisterLibFunc(&hv_gic_get_distributor_size, hypervisorLib, "hv_gic_get_distributor_size")
		purego.RegisterLibFunc(&hv_gic_get_distributor_base_alignment, hypervisorLib, "hv_gic_get_distributor_base_alignment")
		purego.RegisterLibFunc(&hv_gic_get_redistributor_region_size, hypervisorLib, "hv_gic_get_redistributor_region_size")
		purego.RegisterLibFunc(&hv_gic_get_redistributor_size, hypervisorLib, "hv_gic_get_redistributor_size")
		purego.RegisterLibFunc(&hv_gic_get_redistributor_base_alignment, hypervisorLib, "hv_gic_get_redistributor_base_alignment")
		purego.RegisterLibFunc(&hv_gic_get_msi_region_size, hypervisorLib, "hv_gic_get_msi_region_size")
		purego.RegisterLibFunc(&hv_gic_get_msi_region_base_alignment, hypervisorLib, "hv_gic_get_msi_region_base_alignment")
		purego.RegisterLibFunc(&hv_gic_get_spi_interrupt_range, hypervisorLib, "hv_gic_get_spi_interrupt_range")
		purego.RegisterLibFunc(&hv_gic_get_intid, hypervisorLib, "hv_gic_get_intid")

		// GIC state capture
		purego.RegisterLibFunc(&hv_gic_state_create, hypervisorLib, "hv_gic_state_create")
		purego.RegisterLibFunc(&hv_gic_state_get_size, hypervisorLib, "hv_gic_state_get_size")
		purego.RegisterLibFunc(&hv_gic_state_get_data, hypervisorLib, "hv_gic_state_get_data")

		// SME config
		purego.RegisterLibFunc(&hv_sme_config_get_max_svl_bytes, hypervisorLib, "hv_sme_config_get_max_svl_bytes")

		// Load libSystem for Mach VM functions (vm_copy for COW)
		libSystemLib, err = purego.Dlopen(
			"/usr/lib/libSystem.B.dylib",
			purego.RTLD_GLOBAL|purego.RTLD_LAZY,
		)
		if err != nil {
			loadErr = fmt.Errorf("purego dlopen libSystem: %w", err)
			return
		}

		purego.RegisterLibFunc(&mach_task_self, libSystemLib, "mach_task_self")
		purego.RegisterLibFunc(&vm_copy, libSystemLib, "vm_copy")
	})
	return loadErr
}

func MustLoad() {
	if err := Load(); err != nil {
		panic(err)
	}
}

// ---- Function variables (populated by Load) ----

// VM
var (
	hv_vm_get_max_vcpu_count func(maxVCPUCount *uint32) Return
	hv_vm_create             func(config VMConfig) Return
	hv_vm_destroy            func() Return
	hv_vm_map                func(addr unsafe.Pointer, ipa IPA, size uintptr, flags MemoryFlags) Return
	hv_vm_unmap              func(ipa IPA, size uintptr) Return
	hv_vm_protect            func(ipa IPA, size uintptr, flags MemoryFlags) Return
	hv_vm_allocate           func(uvap *unsafe.Pointer, size uintptr, flags AllocateFlags) Return
	hv_vm_deallocate         func(uva unsafe.Pointer, size uintptr) Return
)

// VM config
var (
	hv_vm_config_create               func() VMConfig
	hv_vm_config_get_max_ipa_size     func(ipaBitLength *uint32) Return
	hv_vm_config_get_default_ipa_size func(ipaBitLength *uint32) Return
	hv_vm_config_set_ipa_size         func(config VMConfig, ipaBitLength uint32) Return
	hv_vm_config_get_ipa_size         func(config VMConfig, ipaBitLength *uint32) Return
	hv_vm_config_get_el2_supported    func(el2Supported *bool) Return
	hv_vm_config_get_el2_enabled      func(config VMConfig, el2Enabled *bool) Return
	hv_vm_config_set_el2_enabled      func(config VMConfig, el2Enabled bool) Return
)

// vCPU config
var (
	hv_vcpu_config_create                        func() VcpuConfig
	hv_vcpu_config_get_feature_reg               func(config VcpuConfig, featureReg FeatureReg, value *uint64) Return
	hv_vcpu_config_get_ccsidr_el1_sys_reg_values func(config VcpuConfig, cacheType CacheType, values *[8]uint64) Return
)

// vCPU
var (
	hv_vcpu_create                      func(vcpu *VCPU, exit **VcpuExit, config VcpuConfig) Return
	hv_vcpu_destroy                     func(vcpu VCPU) Return
	hv_vcpu_get_reg                     func(vcpu VCPU, reg Reg, value *uint64) Return
	hv_vcpu_set_reg                     func(vcpu VCPU, reg Reg, value uint64) Return
	hv_vcpu_get_simd_fp_reg             func(vcpu VCPU, reg SIMDReg, value *SimdFP) Return
	hv_vcpu_set_simd_fp_reg             func(vcpu VCPU, reg SIMDReg, value SimdFP) Return
	hv_vcpu_get_sme_state               func(vcpu VCPU, state *VcpuSMEState) Return
	hv_vcpu_set_sme_state               func(vcpu VCPU, state *VcpuSMEState) Return
	hv_vcpu_get_sme_z_reg               func(vcpu VCPU, reg SMEZReg, value *byte, length uintptr) Return
	hv_vcpu_set_sme_z_reg               func(vcpu VCPU, reg SMEZReg, value *byte, length uintptr) Return
	hv_vcpu_get_sme_p_reg               func(vcpu VCPU, reg SMEPReg, value *byte, length uintptr) Return
	hv_vcpu_set_sme_p_reg               func(vcpu VCPU, reg SMEPReg, value *byte, length uintptr) Return
	hv_vcpu_get_sme_za_reg              func(vcpu VCPU, value *byte, length uintptr) Return
	hv_vcpu_set_sme_za_reg              func(vcpu VCPU, value *byte, length uintptr) Return
	hv_vcpu_get_sme_zt0_reg             func(vcpu VCPU, value *SMEZT0) Return
	hv_vcpu_set_sme_zt0_reg             func(vcpu VCPU, value *SMEZT0) Return
	hv_vcpu_get_sys_reg                 func(vcpu VCPU, reg SysReg, value *uint64) Return
	hv_vcpu_set_sys_reg                 func(vcpu VCPU, reg SysReg, value uint64) Return
	hv_vcpu_get_pending_interrupt       func(vcpu VCPU, typ InterruptType, pending *bool) Return
	hv_vcpu_set_pending_interrupt       func(vcpu VCPU, typ InterruptType, pending bool) Return
	hv_vcpu_get_trap_debug_exceptions   func(vcpu VCPU, value *bool) Return
	hv_vcpu_set_trap_debug_exceptions   func(vcpu VCPU, value bool) Return
	hv_vcpu_get_trap_debug_reg_accesses func(vcpu VCPU, value *bool) Return
	hv_vcpu_set_trap_debug_reg_accesses func(vcpu VCPU, value bool) Return
	hv_vcpu_run                         func(vcpu VCPU) Return
	hv_vcpus_exit                       func(vcpus *VCPU, vcpuCount uint32) Return
	hv_vcpu_get_exec_time               func(vcpu VCPU, time *uint64) Return
	hv_vcpu_get_vtimer_mask             func(vcpu VCPU, masked *bool) Return
	hv_vcpu_set_vtimer_mask             func(vcpu VCPU, masked bool) Return
	hv_vcpu_get_vtimer_offset           func(vcpu VCPU, offset *uint64) Return
	hv_vcpu_set_vtimer_offset           func(vcpu VCPU, offset uint64) Return
)

// GIC
var (
	hv_gic_create                 func(config GICConfig) Return
	hv_gic_set_spi                func(intid uint32, level bool) Return
	hv_gic_send_msi               func(address IPA, intid uint32) Return
	hv_gic_get_distributor_reg    func(reg GICDistributorReg, value *uint64) Return
	hv_gic_set_distributor_reg    func(reg GICDistributorReg, value uint64) Return
	hv_gic_get_redistributor_base func(vcpu VCPU, redistributorBaseAddress *IPA) Return
	hv_gic_get_redistributor_reg  func(vcpu VCPU, reg GICRedistributorReg, value *uint64) Return
	hv_gic_set_redistributor_reg  func(vcpu VCPU, reg GICRedistributorReg, value uint64) Return
	hv_gic_get_icc_reg            func(vcpu VCPU, reg GICICCReg, value *uint64) Return
	hv_gic_set_icc_reg            func(vcpu VCPU, reg GICICCReg, value uint64) Return
	hv_gic_get_ich_reg            func(vcpu VCPU, reg GICICHReg, value *uint64) Return
	hv_gic_set_ich_reg            func(vcpu VCPU, reg GICICHReg, value uint64) Return
	hv_gic_get_icv_reg            func(vcpu VCPU, reg GICICVReg, value *uint64) Return
	hv_gic_set_icv_reg            func(vcpu VCPU, reg GICICVReg, value uint64) Return
	hv_gic_get_msi_reg            func(reg GICMSIReg, value *uint64) Return
	hv_gic_set_msi_reg            func(reg GICMSIReg, value uint64) Return
	hv_gic_set_state              func(gicStateData unsafe.Pointer, gicStateSize uintptr) Return
	hv_gic_reset                  func() Return
)

// GIC config + parameters
var (
	hv_gic_config_create                    func() GICConfig
	hv_gic_config_set_distributor_base      func(config GICConfig, distributorBaseAddress IPA) Return
	hv_gic_config_set_redistributor_base    func(config GICConfig, redistributorBaseAddress IPA) Return
	hv_gic_config_set_msi_region_base       func(config GICConfig, msiRegionBaseAddress IPA) Return
	hv_gic_config_set_msi_interrupt_range   func(config GICConfig, msiIntidBase uint32, msiIntidCount uint32) Return
	hv_gic_get_distributor_size             func(distributorSize *uintptr) Return
	hv_gic_get_distributor_base_alignment   func(distributorBaseAlignment *uintptr) Return
	hv_gic_get_redistributor_region_size    func(redistributorRegionSize *uintptr) Return
	hv_gic_get_redistributor_size           func(redistributorSize *uintptr) Return
	hv_gic_get_redistributor_base_alignment func(redistributorBaseAlignment *uintptr) Return
	hv_gic_get_msi_region_size              func(msiRegionSize *uintptr) Return
	hv_gic_get_msi_region_base_alignment    func(msiRegionBaseAlignment *uintptr) Return
	hv_gic_get_spi_interrupt_range          func(spiIntidBase *uint32, spiIntidCount *uint32) Return
	hv_gic_get_intid                        func(interrupt GICIntID, intid *uint32) Return
)

// GIC state capture
var (
	hv_gic_state_create   func() GICState
	hv_gic_state_get_size func(state GICState, size *uintptr) Return
	hv_gic_state_get_data func(state GICState, data unsafe.Pointer) Return
)

// SME config
var (
	hv_sme_config_get_max_svl_bytes func(value *uintptr) Return
)

// Mach VM functions (from libSystem)
var (
	mach_task_self func() uint32
	vm_copy        func(targetTask uint32, sourceAddr uintptr, size uintptr, destAddr uintptr) int32
)

// VmCopy performs a copy-on-write memory copy using the Mach vm_copy syscall.
// This is an O(1) operation that sets up COW page table entries - the actual
// page copying is deferred until either source or destination is written to.
func VmCopy(source, dest uintptr, size uintptr) error {
	ret := vm_copy(mach_task_self(), source, size, dest)
	if ret != 0 {
		return fmt.Errorf("vm_copy failed with kern_return_t: %d", ret)
	}
	return nil
}
