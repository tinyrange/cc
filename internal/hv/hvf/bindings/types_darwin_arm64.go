//go:build darwin && arm64

package bindings

import "fmt"

// This file defines the core arm64 Hypervisor.framework types, closely mirroring
// the C headers. Most of the “variable” surface area (enums/constants) is
// generated into `consts_generated_darwin_arm64.go`.

// Return is Hypervisor.framework's return type (hv_return_t / mach_error_t).
// On Darwin, this is a 32-bit integer value.
type Return int32

func (r Return) Error() string {
	switch r {
	case HV_SUCCESS:
		return ""
	case HV_ERROR:
		return "error"
	case HV_BUSY:
		return "busy"
	case HV_BAD_ARGUMENT:
		return "bad argument"
	case HV_ILLEGAL_GUEST_STATE:
		return "illegal guest state"
	case HV_NO_RESOURCES:
		return "no resources"
	case HV_NO_DEVICE:
		return "no device"
	case HV_DENIED:
		return "denied"
	case HV_UNSUPPORTED:
		return "unsupported"
	default:
		return fmt.Sprintf("unknown error: %d", r)
	}
}

// VMConfig is an opaque configuration object used by hv_vm_create().
// It is an os_object and must be released with os_release (not part of Hypervisor.framework).
type VMConfig uintptr

// VcpuConfig is an opaque configuration object used by hv_vcpu_create().
// It is an os_object and must be released with os_release (not part of Hypervisor.framework).
type VcpuConfig uintptr

// GICConfig is an opaque configuration object used by hv_gic_create().
// It is an os_object and must be released with os_release (not part of Hypervisor.framework).
type GICConfig uintptr

// GICState is an opaque state object returned by hv_gic_state_create().
// It is an os_object and must be released with os_release (not part of Hypervisor.framework).
type GICState uintptr

// IPA is a guest Intermediate Physical Address (hv_ipa_t).
type IPA uint64

// VCPU is a vCPU instance ID (hv_vcpu_t).
type VCPU uint64

// MemoryFlags is a guest memory permission bitmask (hv_memory_flags_t).
type MemoryFlags uint64

// AllocateFlags are flags for hv_vm_allocate (hv_allocate_flags_t).
type AllocateFlags uint64

// ExitReason is an exit reason from hv_vcpu_run (hv_exit_reason_t).
type ExitReason uint32

func (r ExitReason) String() string {
	switch r {
	case HV_EXIT_REASON_CANCELED:
		return "canceled"
	case HV_EXIT_REASON_EXCEPTION:
		return "exception"
	case HV_EXIT_REASON_VTIMER_ACTIVATED:
		return "vtimer activated"
	case HV_EXIT_REASON_UNKNOWN:
		return "unknown"
	default:
		return fmt.Sprintf("unknown exit reason: %d", r)
	}
}

// ExceptionSyndrome corresponds to ESR_ELx.
type ExceptionSyndrome uint64

// ExceptionAddress corresponds to FAR_ELx.
type ExceptionAddress uint64

// VcpuExitException corresponds to hv_vcpu_exit_exception_t.
type VcpuExitException struct {
	Syndrome        ExceptionSyndrome
	VirtualAddress  ExceptionAddress
	PhysicalAddress IPA
}

// VcpuExit corresponds to hv_vcpu_exit_t.
//
// Note: This mirrors the C layout, including padding after the 32-bit reason.
type VcpuExit struct {
	Reason    ExitReason
	_         uint32
	Exception VcpuExitException
}

// Reg is an ARM general purpose register selector (hv_reg_t).
type Reg uint32

// SIMDReg is an ARM SIMD&FP register selector (hv_simd_fp_reg_t).
type SIMDReg uint32

// SimdFP is the value of a SIMD&FP register (hv_simd_fp_uchar16_t).
type SimdFP struct {
	low  uint64
	high uint64
}

// NewSimdFP creates a new SimdFP value from low and high parts.
func NewSimdFP(low, high uint64) SimdFP {
	return SimdFP{low: low, high: high}
}

// Low returns the low 64 bits of the SIMD register.
func (s SimdFP) Low() uint64 { return s.low }

// High returns the high 64 bits of the SIMD register.
func (s SimdFP) High() uint64 { return s.high }

// SysReg is an ARM system register selector (hv_sys_reg_t).
type SysReg uint16

// InterruptType is an injected interrupt type (hv_interrupt_type_t).
type InterruptType uint32

// CacheType is a cache selector (hv_cache_type_t).
type CacheType uint32

// FeatureReg is an ARM feature register selector (hv_feature_reg_t).
type FeatureReg uint32

// VcpuSMEState corresponds to hv_vcpu_sme_state_t.
type VcpuSMEState struct {
	StreamingSVEModeEnabled bool
	ZAStorageEnabled        bool
}

// SMEZReg is an ARM SME Z vector register selector (hv_sme_z_reg_t).
type SMEZReg uint32

// SMEPReg is an ARM SME P predicate register selector (hv_sme_p_reg_t).
type SMEPReg uint32

// SMEZT0 is the SME2 ZT0 register value (hv_sme_zt0_uchar64_t).
type SMEZT0 [64]byte

// GICIntID is an ARM GIC interrupt id selector (hv_gic_intid_t).
type GICIntID uint16

// GICDistributorReg is an ARM GIC distributor register selector (hv_gic_distributor_reg_t).
type GICDistributorReg uint16

// GICRedistributorReg is an ARM GIC redistributor register selector (hv_gic_redistributor_reg_t).
type GICRedistributorReg uint32

// GICICCReg is an ARM GIC ICC system control register selector (hv_gic_icc_reg_t).
type GICICCReg uint16

// GICICHReg is an ARM GIC virtualization control system register selector (hv_gic_ich_reg_t).
type GICICHReg uint16

// GICICVReg is an ARM GIC ICV system control register selector (hv_gic_icv_reg_t).
type GICICVReg uint16

// GICMSIReg is an ARM GIC MSI register selector (hv_gic_msi_reg_t).
type GICMSIReg uint16
