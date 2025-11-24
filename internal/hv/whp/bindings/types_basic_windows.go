//go:build windows

package bindings

import (
	"fmt"
	"syscall"
)

// HRESULT represents a Windows error/success code returned from WinHv APIs.
type HRESULT int32

// Failed reports whether the HRESULT indicates failure.
func (hr HRESULT) Failed() bool { return hr < 0 }

// Succeeded reports whether the HRESULT indicates success.
func (hr HRESULT) Succeeded() bool { return hr >= 0 }

// Err converts the HRESULT into a Go error. It returns nil when the code
// represents success.
func (hr HRESULT) Err() error {
	if hr.Succeeded() {
		return nil
	}
	return HRESULTError(hr)
}

var (
	// HRESULTS
	HRESULTSuccess = HRESULT(0x00000000)
	HRESULTFail    = HRESULT(-0x7FFFBFFB)
)

// HRESULTError wraps a failing HRESULT value and implements the error interface.
type HRESULTError HRESULT

func (e HRESULTError) Error() string {
	return fmt.Sprintf("ERRNO %s", syscall.Errno(e).Error())
}

// CapabilityCode mirrors WHV_CAPABILITY_CODE.
type CapabilityCode uint32

const (
	CapabilityCodeHypervisorPresent       CapabilityCode = 0x00000000
	CapabilityCodeFeatures                CapabilityCode = 0x00000001
	CapabilityCodeExtendedVmExits         CapabilityCode = 0x00000002
	CapabilityCodeExceptionExitBitmap     CapabilityCode = 0x00000003
	CapabilityCodeX64MsrExitBitmap        CapabilityCode = 0x00000004
	CapabilityCodeProcessorVendor         CapabilityCode = 0x00001000
	CapabilityCodeProcessorFeatures       CapabilityCode = 0x00001001
	CapabilityCodeProcessorClFlushSize    CapabilityCode = 0x00001002
	CapabilityCodeProcessorXsaveFeatures  CapabilityCode = 0x00001003
	CapabilityCodeProcessorClockFrequency CapabilityCode = 0x00001004
	CapabilityCodeInterruptClockFrequency CapabilityCode = 0x00001005
	CapabilityCodeProcessorFeaturesBanks  CapabilityCode = 0x00001006
)

// CapabilityFeatures mirrors WHV_CAPABILITY_FEATURES.
type CapabilityFeatures uint64

const (
	CapabilityFeaturePartialUnmap       CapabilityFeatures = 1 << 0
	CapabilityFeatureLocalApicEmulation CapabilityFeatures = 1 << 1
	CapabilityFeatureXsave              CapabilityFeatures = 1 << 2
	CapabilityFeatureDirtyPageTracking  CapabilityFeatures = 1 << 3
	CapabilityFeatureSpeculationControl CapabilityFeatures = 1 << 4
	CapabilityFeatureApicRemoteRead     CapabilityFeatures = 1 << 5
	CapabilityFeatureIdleSuspend        CapabilityFeatures = 1 << 6
)

// ExtendedVmExits mirrors WHV_EXTENDED_VM_EXITS.
type ExtendedVmExits uint64

const (
	ExtendedVmExitX64Cpuid        ExtendedVmExits = 1 << 0
	ExtendedVmExitX64Msr          ExtendedVmExits = 1 << 1
	ExtendedVmExitException       ExtendedVmExits = 1 << 2
	ExtendedVmExitX64Rdtsc        ExtendedVmExits = 1 << 3
	ExtendedVmExitX64ApicSmiTrap  ExtendedVmExits = 1 << 4
	ExtendedVmExitHypercall       ExtendedVmExits = 1 << 5
	ExtendedVmExitX64ApicInitSipi ExtendedVmExits = 1 << 6
)

// ProcessorVendor mirrors WHV_PROCESSOR_VENDOR.
type ProcessorVendor uint32

const (
	ProcessorVendorAmd   ProcessorVendor = 0x0000
	ProcessorVendorIntel ProcessorVendor = 0x0001
	ProcessorVendorHygon ProcessorVendor = 0x0002
)

// ProcessorFeatures mirrors WHV_PROCESSOR_FEATURES.
type ProcessorFeatures uint64

const (
	ProcessorFeatureSse3Support               ProcessorFeatures = 1 << 0
	ProcessorFeatureLahfSahfSupport           ProcessorFeatures = 1 << 1
	ProcessorFeatureSsse3Support              ProcessorFeatures = 1 << 2
	ProcessorFeatureSse41Support              ProcessorFeatures = 1 << 3
	ProcessorFeatureSse42Support              ProcessorFeatures = 1 << 4
	ProcessorFeatureSse4aSupport              ProcessorFeatures = 1 << 5
	ProcessorFeatureXopSupport                ProcessorFeatures = 1 << 6
	ProcessorFeaturePopCntSupport             ProcessorFeatures = 1 << 7
	ProcessorFeatureCmpxchg16bSupport         ProcessorFeatures = 1 << 8
	ProcessorFeatureAltmovcr8Support          ProcessorFeatures = 1 << 9
	ProcessorFeatureLzcntSupport              ProcessorFeatures = 1 << 10
	ProcessorFeatureMisAlignSseSupport        ProcessorFeatures = 1 << 11
	ProcessorFeatureMmxExtSupport             ProcessorFeatures = 1 << 12
	ProcessorFeatureAmd3DNowSupport           ProcessorFeatures = 1 << 13
	ProcessorFeatureExtendedAmd3DNowSupport   ProcessorFeatures = 1 << 14
	ProcessorFeaturePage1GbSupport            ProcessorFeatures = 1 << 15
	ProcessorFeatureAesSupport                ProcessorFeatures = 1 << 16
	ProcessorFeaturePclmulqdqSupport          ProcessorFeatures = 1 << 17
	ProcessorFeaturePcidSupport               ProcessorFeatures = 1 << 18
	ProcessorFeatureFma4Support               ProcessorFeatures = 1 << 19
	ProcessorFeatureF16CSupport               ProcessorFeatures = 1 << 20
	ProcessorFeatureRdRandSupport             ProcessorFeatures = 1 << 21
	ProcessorFeatureRdWrFsGsSupport           ProcessorFeatures = 1 << 22
	ProcessorFeatureSmepSupport               ProcessorFeatures = 1 << 23
	ProcessorFeatureEnhancedFastStringSupport ProcessorFeatures = 1 << 24
	ProcessorFeatureBmi1Support               ProcessorFeatures = 1 << 25
	ProcessorFeatureBmi2Support               ProcessorFeatures = 1 << 26
	ProcessorFeatureMovbeSupport              ProcessorFeatures = 1 << 29
	ProcessorFeatureNpiep1Support             ProcessorFeatures = 1 << 30
	ProcessorFeatureDepX87FPUSaveSupport      ProcessorFeatures = 1 << 31
	ProcessorFeatureRdSeedSupport             ProcessorFeatures = 1 << 32
	ProcessorFeatureAdxSupport                ProcessorFeatures = 1 << 33
	ProcessorFeatureIntelPrefetchSupport      ProcessorFeatures = 1 << 34
	ProcessorFeatureSmapSupport               ProcessorFeatures = 1 << 35
	ProcessorFeatureHleSupport                ProcessorFeatures = 1 << 36
	ProcessorFeatureRtmSupport                ProcessorFeatures = 1 << 37
	ProcessorFeatureRdtscpSupport             ProcessorFeatures = 1 << 38
	ProcessorFeatureClflushoptSupport         ProcessorFeatures = 1 << 39
	ProcessorFeatureClwbSupport               ProcessorFeatures = 1 << 40
	ProcessorFeatureShaSupport                ProcessorFeatures = 1 << 41
	ProcessorFeatureX87PointersSavedSupport   ProcessorFeatures = 1 << 42
	ProcessorFeatureInvpcidSupport            ProcessorFeatures = 1 << 43
	ProcessorFeatureIbrsSupport               ProcessorFeatures = 1 << 44
	ProcessorFeatureStibpSupport              ProcessorFeatures = 1 << 45
	ProcessorFeatureIbpbSupport               ProcessorFeatures = 1 << 46
	ProcessorFeatureSsbdSupport               ProcessorFeatures = 1 << 48
	ProcessorFeatureFastShortRepMovSupport    ProcessorFeatures = 1 << 49
	ProcessorFeatureRdclNo                    ProcessorFeatures = 1 << 51
	ProcessorFeatureIbrsAllSupport            ProcessorFeatures = 1 << 52
	ProcessorFeatureSsbNo                     ProcessorFeatures = 1 << 54
	ProcessorFeatureRsbANo                    ProcessorFeatures = 1 << 55
	ProcessorFeatureRdPidSupport              ProcessorFeatures = 1 << 57
	ProcessorFeatureUmipSupport               ProcessorFeatures = 1 << 58
	ProcessorFeatureMdsNoSupport              ProcessorFeatures = 1 << 59
	ProcessorFeatureMdClearSupport            ProcessorFeatures = 1 << 60
)

// ProcessorFeatures1 mirrors WHV_PROCESSOR_FEATURES1.
type ProcessorFeatures1 uint64

const (
	ProcessorFeature1ClZeroSupport ProcessorFeatures1 = 1 << 2
)

// ProcessorFeaturesBanks mirrors WHV_PROCESSOR_FEATURES_BANKS.
type ProcessorFeaturesBanks struct {
	BanksCount uint32
	Reserved0  uint32
	Bank0      ProcessorFeatures
	Bank1      ProcessorFeatures1
}

// ProcessorXsaveFeatures mirrors WHV_PROCESSOR_XSAVE_FEATURES.
type ProcessorXsaveFeatures uint64

const (
	ProcessorXsaveXsaveSupport        ProcessorXsaveFeatures = 1 << 0
	ProcessorXsaveXsaveoptSupport     ProcessorXsaveFeatures = 1 << 1
	ProcessorXsaveAvxSupport          ProcessorXsaveFeatures = 1 << 2
	ProcessorXsaveAvx2Support         ProcessorXsaveFeatures = 1 << 3
	ProcessorXsaveFmaSupport          ProcessorXsaveFeatures = 1 << 4
	ProcessorXsaveMpxSupport          ProcessorXsaveFeatures = 1 << 5
	ProcessorXsaveAvx512Support       ProcessorXsaveFeatures = 1 << 6
	ProcessorXsaveAvx512DQSupport     ProcessorXsaveFeatures = 1 << 7
	ProcessorXsaveAvx512CDSupport     ProcessorXsaveFeatures = 1 << 8
	ProcessorXsaveAvx512BWSupport     ProcessorXsaveFeatures = 1 << 9
	ProcessorXsaveAvx512VLSupport     ProcessorXsaveFeatures = 1 << 10
	ProcessorXsaveXsaveCompSupport    ProcessorXsaveFeatures = 1 << 11
	ProcessorXsaveXsaveSupervisor     ProcessorXsaveFeatures = 1 << 12
	ProcessorXsaveXcr1Support         ProcessorXsaveFeatures = 1 << 13
	ProcessorXsaveAvx512BitalgSupport ProcessorXsaveFeatures = 1 << 14
	ProcessorXsaveAvx512IfmaSupport   ProcessorXsaveFeatures = 1 << 15
	ProcessorXsaveAvx512VBmiSupport   ProcessorXsaveFeatures = 1 << 16
	ProcessorXsaveAvx512VBmi2Support  ProcessorXsaveFeatures = 1 << 17
	ProcessorXsaveAvx512VnniSupport   ProcessorXsaveFeatures = 1 << 18
	ProcessorXsaveGfniSupport         ProcessorXsaveFeatures = 1 << 19
	ProcessorXsaveVaesSupport         ProcessorXsaveFeatures = 1 << 20
	ProcessorXsaveAvx512VPopcntdq     ProcessorXsaveFeatures = 1 << 21
	ProcessorXsaveVpclmulqdqSupport   ProcessorXsaveFeatures = 1 << 22
	ProcessorXsaveAvx512Bf16Support   ProcessorXsaveFeatures = 1 << 23
	ProcessorXsaveAvx512Vp2Intersect  ProcessorXsaveFeatures = 1 << 24
)

// X64MsrExitBitmap mirrors WHV_X64_MSR_EXIT_BITMAP.
type X64MsrExitBitmap uint64

const (
	X64MsrExitUnhandledMsrs    X64MsrExitBitmap = 1 << 0
	X64MsrExitTscMsrWrite      X64MsrExitBitmap = 1 << 1
	X64MsrExitTscMsrRead       X64MsrExitBitmap = 1 << 2
	X64MsrExitApicBaseMsrWrite X64MsrExitBitmap = 1 << 3
)

// PartitionHandle mirrors WHV_PARTITION_HANDLE.
type PartitionHandle syscall.Handle

// GuestPhysicalAddress mirrors WHV_GUEST_PHYSICAL_ADDRESS.
type GuestPhysicalAddress uint64

// GuestVirtualAddress mirrors WHV_GUEST_VIRTUAL_ADDRESS.
type GuestVirtualAddress uint64

// MapGPARangeFlags mirrors WHV_MAP_GPA_RANGE_FLAGS.
type MapGPARangeFlags uint32

const (
	MapGPARangeFlagNone       MapGPARangeFlags = 0
	MapGPARangeFlagRead       MapGPARangeFlags = 0x00000001
	MapGPARangeFlagWrite      MapGPARangeFlags = 0x00000002
	MapGPARangeFlagExecute    MapGPARangeFlags = 0x00000004
	MapGPARangeFlagTrackDirty MapGPARangeFlags = 0x00000008
)

// TranslateGVAFlags mirrors WHV_TRANSLATE_GVA_FLAGS.
type TranslateGVAFlags uint32

const (
	TranslateGVAFlagNone             TranslateGVAFlags = 0
	TranslateGVAFlagValidateRead     TranslateGVAFlags = 0x00000001
	TranslateGVAFlagValidateWrite    TranslateGVAFlags = 0x00000002
	TranslateGVAFlagValidateExec     TranslateGVAFlags = 0x00000004
	TranslateGVAFlagPrivilegeExempt  TranslateGVAFlags = 0x00000008
	TranslateGVAFlagSetPageTableBits TranslateGVAFlags = 0x00000010
)

// TranslateGVAResultCode mirrors WHV_TRANSLATE_GVA_RESULT_CODE.
type TranslateGVAResultCode uint32

const (
	TranslateGVAResultSuccess               TranslateGVAResultCode = 0
	TranslateGVAResultPageNotPresent        TranslateGVAResultCode = 1
	TranslateGVAResultPrivilegeViolation    TranslateGVAResultCode = 2
	TranslateGVAResultInvalidPageTableFlags TranslateGVAResultCode = 3
	TranslateGVAResultGpaUnmapped           TranslateGVAResultCode = 4
	TranslateGVAResultGpaNoReadAccess       TranslateGVAResultCode = 5
	TranslateGVAResultGpaNoWriteAccess      TranslateGVAResultCode = 6
	TranslateGVAResultGpaIllegalOverlay     TranslateGVAResultCode = 7
	TranslateGVAResultIntercept             TranslateGVAResultCode = 8
)

// TranslateGVAResult mirrors WHV_TRANSLATE_GVA_RESULT.
type TranslateGVAResult struct {
	ResultCode TranslateGVAResultCode
	Reserved   uint32
}

// PartitionPropertyCode mirrors WHV_PARTITION_PROPERTY_CODE.
type PartitionPropertyCode uint32

const (
	PartitionPropertyCodeExtendedVmExits         PartitionPropertyCode = 0x00000001
	PartitionPropertyCodeExceptionExitBitmap     PartitionPropertyCode = 0x00000002
	PartitionPropertyCodeSeparateSecurityDomain  PartitionPropertyCode = 0x00000003
	PartitionPropertyCodeNestedVirtualization    PartitionPropertyCode = 0x00000004
	PartitionPropertyCodeX64MsrExitBitmap        PartitionPropertyCode = 0x00000005
	PartitionPropertyCodeProcessorFeatures       PartitionPropertyCode = 0x00001001
	PartitionPropertyCodeProcessorClFlushSize    PartitionPropertyCode = 0x00001002
	PartitionPropertyCodeCpuidExitList           PartitionPropertyCode = 0x00001003
	PartitionPropertyCodeCpuidResultList         PartitionPropertyCode = 0x00001004
	PartitionPropertyCodeLocalApicEmulationMode  PartitionPropertyCode = 0x00001005
	PartitionPropertyCodeProcessorXsaveFeatures  PartitionPropertyCode = 0x00001006
	PartitionPropertyCodeProcessorClockFrequency PartitionPropertyCode = 0x00001007
	PartitionPropertyCodeInterruptClockFrequency PartitionPropertyCode = 0x00001008
	PartitionPropertyCodeApicRemoteReadSupport   PartitionPropertyCode = 0x00001009
	PartitionPropertyCodeProcessorFeaturesBanks  PartitionPropertyCode = 0x0000100A
	PartitionPropertyCodeReferenceTime           PartitionPropertyCode = 0x0000100B
	PartitionPropertyCodeProcessorCount          PartitionPropertyCode = 0x00001fff
)

// ExceptionType mirrors WHV_EXCEPTION_TYPE.
type ExceptionType uint32

const (
	ExceptionTypeDivideErrorFault             ExceptionType = 0x0
	ExceptionTypeDebugTrapOrFault             ExceptionType = 0x1
	ExceptionTypeBreakpointTrap               ExceptionType = 0x3
	ExceptionTypeOverflowTrap                 ExceptionType = 0x4
	ExceptionTypeBoundRangeFault              ExceptionType = 0x5
	ExceptionTypeInvalidOpcodeFault           ExceptionType = 0x6
	ExceptionTypeDeviceNotAvailableFault      ExceptionType = 0x7
	ExceptionTypeDoubleFaultAbort             ExceptionType = 0x8
	ExceptionTypeInvalidTaskStateSegmentFault ExceptionType = 0x0A
	ExceptionTypeSegmentNotPresentFault       ExceptionType = 0x0B
	ExceptionTypeStackFault                   ExceptionType = 0x0C
	ExceptionTypeGeneralProtectionFault       ExceptionType = 0x0D
	ExceptionTypePageFault                    ExceptionType = 0x0E
	ExceptionTypeFloatingPointErrorFault      ExceptionType = 0x10
	ExceptionTypeAlignmentCheckFault          ExceptionType = 0x11
	ExceptionTypeMachineCheckAbort            ExceptionType = 0x12
	ExceptionTypeSimdFloatingPointFault       ExceptionType = 0x13
)

// LocalApicEmulationMode mirrors WHV_X64_LOCAL_APIC_EMULATION_MODE.
type LocalApicEmulationMode uint32

const (
	LocalApicEmulationModeNone   LocalApicEmulationMode = 0
	LocalApicEmulationModeXApic  LocalApicEmulationMode = 1
	LocalApicEmulationModeX2Apic LocalApicEmulationMode = 2
)

// Map flag/test helper bit masks.
const (
	ProcessorFeaturesBanksCount = 2
)
