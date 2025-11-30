//go:build windows

package bindings

import (
	"fmt"
	"syscall"
	"unsafe"
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
	CapabilityCodeHypervisorPresent               CapabilityCode = 0x00000000
	CapabilityCodeFeatures                        CapabilityCode = 0x00000001
	CapabilityCodeExtendedVmExits                 CapabilityCode = 0x00000002
	CapabilityCodeExceptionExitBitmap             CapabilityCode = 0x00000003
	CapabilityCodeX64MsrExitBitmap                CapabilityCode = 0x00000004
	CapabilityCodeGpaRangePopulateFlags           CapabilityCode = 0x00000005
	CapabilityCodeSchedulerFeatures               CapabilityCode = 0x00000006
	CapabilityCodeProcessorVendor                 CapabilityCode = 0x00001000
	CapabilityCodeProcessorFeatures               CapabilityCode = 0x00001001
	CapabilityCodeProcessorClFlushSize            CapabilityCode = 0x00001002
	CapabilityCodeProcessorXsaveFeatures          CapabilityCode = 0x00001003
	CapabilityCodeProcessorClockFrequency         CapabilityCode = 0x00001004
	CapabilityCodeInterruptClockFrequency         CapabilityCode = 0x00001005
	CapabilityCodeProcessorFeaturesBanks          CapabilityCode = 0x00001006
	CapabilityCodeProcessorFrequencyCap           CapabilityCode = 0x00001007
	CapabilityCodeSyntheticProcessorFeaturesBanks CapabilityCode = 0x00001008
	CapabilityCodeProcessorPerfmonFeatures        CapabilityCode = 0x00001009
	CapabilityCodePhysicalAddressWidth            CapabilityCode = 0x0000100A
	CapabilityCodeVmxBasic                        CapabilityCode = 0x00002000
	CapabilityCodeVmxPinbasedCtls                 CapabilityCode = 0x00002001
	CapabilityCodeVmxProcbasedCtls                CapabilityCode = 0x00002002
	CapabilityCodeVmxExitCtls                     CapabilityCode = 0x00002003
	CapabilityCodeVmxEntryCtls                    CapabilityCode = 0x00002004
	CapabilityCodeVmxMisc                         CapabilityCode = 0x00002005
	CapabilityCodeVmxCr0Fixed0                    CapabilityCode = 0x00002006
	CapabilityCodeVmxCr0Fixed1                    CapabilityCode = 0x00002007
	CapabilityCodeVmxCr4Fixed0                    CapabilityCode = 0x00002008
	CapabilityCodeVmxCr4Fixed1                    CapabilityCode = 0x00002009
	CapabilityCodeVmxVmcsEnum                     CapabilityCode = 0x0000200A
	CapabilityCodeVmxProcbasedCtls2               CapabilityCode = 0x0000200B
	CapabilityCodeVmxEptVpidCap                   CapabilityCode = 0x0000200C
	CapabilityCodeVmxTruePinbasedCtls             CapabilityCode = 0x0000200D
	CapabilityCodeVmxTrueProcbasedCtls            CapabilityCode = 0x0000200E
	CapabilityCodeVmxTrueExitCtls                 CapabilityCode = 0x0000200F
	CapabilityCodeVmxTrueEntryCtls                CapabilityCode = 0x00002010
)

// CapabilityFeatures mirrors WHV_CAPABILITY_FEATURES.
type CapabilityFeatures uint64

const (
	CapabilityFeaturePartialUnmap         CapabilityFeatures = 1 << 0
	CapabilityFeatureLocalApicEmulation   CapabilityFeatures = 1 << 1
	CapabilityFeatureXsave                CapabilityFeatures = 1 << 2
	CapabilityFeatureDirtyPageTracking    CapabilityFeatures = 1 << 3
	CapabilityFeatureSpeculationControl   CapabilityFeatures = 1 << 4
	CapabilityFeatureApicRemoteRead       CapabilityFeatures = 1 << 5
	CapabilityFeatureIdleSuspend          CapabilityFeatures = 1 << 6
	CapabilityFeatureVirtualPciDevice     CapabilityFeatures = 1 << 7
	CapabilityFeatureIommuSupport         CapabilityFeatures = 1 << 8
	CapabilityFeatureVpHotAddRemove       CapabilityFeatures = 1 << 9
	CapabilityFeatureDeviceAccessTracking CapabilityFeatures = 1 << 10
)

// ExtendedVmExits mirrors WHV_EXTENDED_VM_EXITS.
type ExtendedVmExits uint64

const (
	ExtendedVmExitX64Cpuid               ExtendedVmExits = 1 << 0
	ExtendedVmExitX64Msr                 ExtendedVmExits = 1 << 1
	ExtendedVmExitException              ExtendedVmExits = 1 << 2
	ExtendedVmExitX64Rdtsc               ExtendedVmExits = 1 << 3
	ExtendedVmExitX64ApicSmiTrap         ExtendedVmExits = 1 << 4
	ExtendedVmExitHypercall              ExtendedVmExits = 1 << 5
	ExtendedVmExitX64ApicInitSipi        ExtendedVmExits = 1 << 6
	ExtendedVmExitX64ApicWriteLint0Trap  ExtendedVmExits = 1 << 7
	ExtendedVmExitX64ApicWriteLint1Trap  ExtendedVmExits = 1 << 8
	ExtendedVmExitX64ApicWriteSvrTrap    ExtendedVmExits = 1 << 9
	ExtendedVmExitUnknownSynicConnection ExtendedVmExits = 1 << 10
	ExtendedVmExitRetargetUnknownVpci    ExtendedVmExits = 1 << 11
	ExtendedVmExitX64ApicWriteLdrTrap    ExtendedVmExits = 1 << 12
	ExtendedVmExitX64ApicWriteDfrTrap    ExtendedVmExits = 1 << 13
	ExtendedVmExitGpaAccessFault         ExtendedVmExits = 1 << 14
)

// ProcessorVendor mirrors WHV_PROCESSOR_VENDOR.
type ProcessorVendor uint32

const (
	ProcessorVendorAmd   ProcessorVendor = 0x0000
	ProcessorVendorIntel ProcessorVendor = 0x0001
	ProcessorVendorHygon ProcessorVendor = 0x0002
	ProcessorVendorArm   ProcessorVendor = 0x0010
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
	ProcessorFeatureUnrestrictedGuestSupport  ProcessorFeatures = 1 << 47
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
	ProcessorFeatureTaaNoSupport              ProcessorFeatures = 1 << 61
	ProcessorFeatureTsxCtrlSupport            ProcessorFeatures = 1 << 62
)

// ProcessorFeatures1 mirrors WHV_PROCESSOR_FEATURES1.
type ProcessorFeatures1 uint64

const (
	ProcessorFeature1ACountMCountSupport       ProcessorFeatures1 = 1 << 0
	ProcessorFeature1TscInvariantSupport       ProcessorFeatures1 = 1 << 1
	ProcessorFeature1ClZeroSupport             ProcessorFeatures1 = 1 << 2
	ProcessorFeature1RdpruSupport              ProcessorFeatures1 = 1 << 3
	ProcessorFeature1La57Support               ProcessorFeatures1 = 1 << 4
	ProcessorFeature1MbecSupport               ProcessorFeatures1 = 1 << 5
	ProcessorFeature1NestedVirtSupport         ProcessorFeatures1 = 1 << 6
	ProcessorFeature1PsfdSupport               ProcessorFeatures1 = 1 << 7
	ProcessorFeature1CetSsSupport              ProcessorFeatures1 = 1 << 8
	ProcessorFeature1CetIbtSupport             ProcessorFeatures1 = 1 << 9
	ProcessorFeature1VmxExceptionInjectSupport ProcessorFeatures1 = 1 << 10
	ProcessorFeature1UmwaitTpauseSupport       ProcessorFeatures1 = 1 << 12
	ProcessorFeature1MovdiriSupport            ProcessorFeatures1 = 1 << 13
	ProcessorFeature1Movdir64bSupport          ProcessorFeatures1 = 1 << 14
	ProcessorFeature1CldemoteSupport           ProcessorFeatures1 = 1 << 15
	ProcessorFeature1SerializeSupport          ProcessorFeatures1 = 1 << 16
	ProcessorFeature1TscDeadlineTmrSupport     ProcessorFeatures1 = 1 << 17
	ProcessorFeature1TscAdjustSupport          ProcessorFeatures1 = 1 << 18
	ProcessorFeature1FZLRepMovsb               ProcessorFeatures1 = 1 << 19
	ProcessorFeature1FSRepStosb                ProcessorFeatures1 = 1 << 20
	ProcessorFeature1FSRepCmpsb                ProcessorFeatures1 = 1 << 21
	ProcessorFeature1TsxLdTrkSupport           ProcessorFeatures1 = 1 << 22
	ProcessorFeature1VmxInsOutsExitInfoSupport ProcessorFeatures1 = 1 << 23
	ProcessorFeature1SbdrSsdpNoSupport         ProcessorFeatures1 = 1 << 25
	ProcessorFeature1FbsdpNoSupport            ProcessorFeatures1 = 1 << 26
	ProcessorFeature1PsdpNoSupport             ProcessorFeatures1 = 1 << 27
	ProcessorFeature1FbClearSupport            ProcessorFeatures1 = 1 << 28
	ProcessorFeature1BtcNoSupport              ProcessorFeatures1 = 1 << 29
	ProcessorFeature1IbpbRsbFlushSupport       ProcessorFeatures1 = 1 << 30
	ProcessorFeature1StibpAlwaysOnSupport      ProcessorFeatures1 = 1 << 31
	ProcessorFeature1PerfGlobalCtrlSupport     ProcessorFeatures1 = 1 << 32
	ProcessorFeature1NptExecuteOnlySupport     ProcessorFeatures1 = 1 << 33
	ProcessorFeature1NptADFlagsSupport         ProcessorFeatures1 = 1 << 34
	ProcessorFeature1Npt1GbPageSupport         ProcessorFeatures1 = 1 << 35
	ProcessorFeature1CmpccxaddSupport          ProcessorFeatures1 = 1 << 40
	ProcessorFeature1PrefetchISupport          ProcessorFeatures1 = 1 << 45
	ProcessorFeature1Sha512Support             ProcessorFeatures1 = 1 << 46
	ProcessorFeature1SM3Support                ProcessorFeatures1 = 1 << 50
	ProcessorFeature1SM4Support                ProcessorFeatures1 = 1 << 51
	ProcessorFeature1SbpbSupported             ProcessorFeatures1 = 1 << 54
	ProcessorFeature1IbpbBrTypeSupported       ProcessorFeatures1 = 1 << 55
	ProcessorFeature1SrsoNoSupported           ProcessorFeatures1 = 1 << 56
	ProcessorFeature1SrsoUserKernelNoSupported ProcessorFeatures1 = 1 << 57
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
	ProcessorXsaveAvx512Fp16Support   ProcessorXsaveFeatures = 1 << 25
	ProcessorXsaveXfdSupport          ProcessorXsaveFeatures = 1 << 26
	ProcessorXsaveAmxTileSupport      ProcessorXsaveFeatures = 1 << 27
	ProcessorXsaveAmxBf16Support      ProcessorXsaveFeatures = 1 << 28
	ProcessorXsaveAmxInt8Support      ProcessorXsaveFeatures = 1 << 29
	ProcessorXsaveAvxVnniSupport      ProcessorXsaveFeatures = 1 << 30
	ProcessorXsaveAvxIfmaSupport      ProcessorXsaveFeatures = 1 << 31
	ProcessorXsaveAvxNeConvertSupport ProcessorXsaveFeatures = 1 << 32
	ProcessorXsaveAvxVnniInt8Support  ProcessorXsaveFeatures = 1 << 33
	ProcessorXsaveAvxVnniInt16Support ProcessorXsaveFeatures = 1 << 34
	ProcessorXsaveAvx10_1_256Support  ProcessorXsaveFeatures = 1 << 35
	ProcessorXsaveAvx10_1_512Support  ProcessorXsaveFeatures = 1 << 36
	ProcessorXsaveAmxFp16Support      ProcessorXsaveFeatures = 1 << 37
)

// X64MsrExitBitmap mirrors WHV_X64_MSR_EXIT_BITMAP.
type X64MsrExitBitmap uint64

const (
	X64MsrExitUnhandledMsrs          X64MsrExitBitmap = 1 << 0
	X64MsrExitTscMsrWrite            X64MsrExitBitmap = 1 << 1
	X64MsrExitTscMsrRead             X64MsrExitBitmap = 1 << 2
	X64MsrExitApicBaseMsrWrite       X64MsrExitBitmap = 1 << 3
	X64MsrExitMiscEnableMsrRead      X64MsrExitBitmap = 1 << 4
	X64MsrExitMcUpdatePatchLevelRead X64MsrExitBitmap = 1 << 5
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
	TranslateGVAFlagEnforceSmap      TranslateGVAFlags = 0x00000100
	TranslateGVAFlagOverrideSmap     TranslateGVAFlags = 0x00000200
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
	PartitionPropertyCodeExtendedVmExits                 PartitionPropertyCode = 0x00000001
	PartitionPropertyCodeExceptionExitBitmap             PartitionPropertyCode = 0x00000002
	PartitionPropertyCodeSeparateSecurityDomain          PartitionPropertyCode = 0x00000003
	PartitionPropertyCodeNestedVirtualization            PartitionPropertyCode = 0x00000004
	PartitionPropertyCodeX64MsrExitBitmap                PartitionPropertyCode = 0x00000005
	PartitionPropertyCodePrimaryNumaNode                 PartitionPropertyCode = 0x00000006
	PartitionPropertyCodeCpuReserve                      PartitionPropertyCode = 0x00000007
	PartitionPropertyCodeCpuCap                          PartitionPropertyCode = 0x00000008
	PartitionPropertyCodeCpuWeight                       PartitionPropertyCode = 0x00000009
	PartitionPropertyCodeCpuGroupId                      PartitionPropertyCode = 0x0000000a
	PartitionPropertyCodeProcessorFrequencyCap           PartitionPropertyCode = 0x0000000b
	PartitionPropertyCodeAllowDeviceAssignment           PartitionPropertyCode = 0x0000000c
	PartitionPropertyCodeDisableSmt                      PartitionPropertyCode = 0x0000000d
	PartitionPropertyCodeProcessorFeatures               PartitionPropertyCode = 0x00001001
	PartitionPropertyCodeProcessorClFlushSize            PartitionPropertyCode = 0x00001002
	PartitionPropertyCodeCpuidExitList                   PartitionPropertyCode = 0x00001003
	PartitionPropertyCodeCpuidResultList                 PartitionPropertyCode = 0x00001004
	PartitionPropertyCodeLocalApicEmulationMode          PartitionPropertyCode = 0x00001005
	PartitionPropertyCodeProcessorXsaveFeatures          PartitionPropertyCode = 0x00001006
	PartitionPropertyCodeProcessorClockFrequency         PartitionPropertyCode = 0x00001007
	PartitionPropertyCodeInterruptClockFrequency         PartitionPropertyCode = 0x00001008
	PartitionPropertyCodeApicRemoteReadSupport           PartitionPropertyCode = 0x00001009
	PartitionPropertyCodeProcessorFeaturesBanks          PartitionPropertyCode = 0x0000100A
	PartitionPropertyCodeReferenceTime                   PartitionPropertyCode = 0x0000100B
	PartitionPropertyCodeSyntheticProcessorFeaturesBanks PartitionPropertyCode = 0x0000100C
	PartitionPropertyCodeCpuidResultList2                PartitionPropertyCode = 0x0000100D
	PartitionPropertyCodeProcessorPerfmonFeatures        PartitionPropertyCode = 0x0000100E
	PartitionPropertyCodeMsrActionList                   PartitionPropertyCode = 0x0000100F
	PartitionPropertyCodeUnimplementedMsrAction          PartitionPropertyCode = 0x00001010
	PartitionPropertyCodePhysicalAddressWidth            PartitionPropertyCode = 0x00001011
	PartitionPropertyCodeProcessorCount                  PartitionPropertyCode = 0x00001fff

	// ARM64 specific
	PartitionPropertyCodeArm64IcParameters PartitionPropertyCode = 0x00001012
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
	ExceptionTypeControlProtectionFault       ExceptionType = 0x15
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

// TriggerHandle mirrors WHV_TRIGGER_HANDLE.
type TriggerHandle uintptr

const (
	InterruptDestinationModePhysical InterruptDestinationMode = 0
	InterruptDestinationModeLogical  InterruptDestinationMode = 1
)

const (
	InterruptTriggerModeEdge  InterruptTriggerMode = 0
	InterruptTriggerModeLevel InterruptTriggerMode = 1
)

// DeviceInterruptParameters represents the struct inside the WHV_TRIGGER_PARAMETERS union.
type DeviceInterruptParameters struct {
	LogicalDeviceId uint64
	MsiAddress      uint64
	MsiData         uint32
	Reserved        uint32
}

// SetDeviceInterrupt helper to write DeviceInterruptParameters into the union.
func (tp *TriggerParameters) SetDeviceInterrupt(dip DeviceInterruptParameters) {
	*(*DeviceInterruptParameters)(unsafe.Pointer(&tp.Data[0])) = dip
}

// NotificationPortType mirrors WHV_NOTIFICATION_PORT_TYPE.
type NotificationPortType uint32

const (
	NotificationPortTypeEvent    NotificationPortType = 2
	NotificationPortTypeDoorbell NotificationPortType = 4
)

// NotificationPortHandle mirrors WHV_NOTIFICATION_PORT_HANDLE.
type NotificationPortHandle uintptr

// NotificationPortParameters mirrors WHV_NOTIFICATION_PORT_PARAMETERS.
type NotificationPortParameters struct {
	NotificationPortType NotificationPortType
	Reserved             uint16
	Reserved1            uint8
	ConnectionVtl        uint8
	Data                 [24]byte // Union of DoorbellMatchData (24) or Event struct (4)
}

// SetDoorbell helper to write DoorbellMatchData into the union.
func (npp *NotificationPortParameters) SetDoorbell(dmd DoorbellMatchData) {
	*(*DoorbellMatchData)(unsafe.Pointer(&npp.Data[0])) = dmd
}

// SetEventConnectionId helper to write ConnectionId into the union.
func (npp *NotificationPortParameters) SetEventConnectionId(id uint32) {
	*(*uint32)(unsafe.Pointer(&npp.Data[0])) = id
}

// NotificationPortPropertyCode mirrors WHV_NOTIFICATION_PORT_PROPERTY_CODE.
type NotificationPortPropertyCode uint32

const (
	NotificationPortPropertyPreferredTargetVp       NotificationPortPropertyCode = 1
	NotificationPortPropertyPreferredTargetDuration NotificationPortPropertyCode = 5
)

// AdviseGpaRangeCode mirrors WHV_ADVISE_GPA_RANGE_CODE.
type AdviseGpaRangeCode uint32

const (
	AdviseGpaRangeCodePopulate AdviseGpaRangeCode = 0
	AdviseGpaRangeCodePin      AdviseGpaRangeCode = 1
	AdviseGpaRangeCodeUnpin    AdviseGpaRangeCode = 2
)

// AdviseGpaRangePopulateFlags mirrors WHV_ADVISE_GPA_RANGE_POPULATE_FLAGS.
type AdviseGpaRangePopulateFlags uint32

const (
	AdviseGpaRangePopulateFlagPrefetch        AdviseGpaRangePopulateFlags = 1 << 0
	AdviseGpaRangePopulateFlagAvoidHardFaults AdviseGpaRangePopulateFlags = 1 << 1
)

// AdviseGpaRangePopulate mirrors WHV_ADVISE_GPA_RANGE_POPULATE.
type AdviseGpaRangePopulate struct {
	Flags      AdviseGpaRangePopulateFlags
	AccessType MemoryAccessType
}

// CacheType mirrors WHV_CACHE_TYPE.
type CacheType uint32

const (
	CacheTypeUncached       CacheType = 0
	CacheTypeWriteCombining CacheType = 1
	CacheTypeWriteThrough   CacheType = 4
	CacheTypeWriteProtected CacheType = 5
	CacheTypeWriteBack      CacheType = 6
)

// MemoryRangeEntry mirrors WHV_MEMORY_RANGE_ENTRY.
type MemoryRangeEntry struct {
	GuestAddress uint64
	SizeInBytes  uint64
}

// Standard Windows Error if missing from syscall package context
const ERROR_NOT_SUPPORTED = syscall.Errno(50)

// -------------------------------------------------------------------------
// Enumerations and Flags
// -------------------------------------------------------------------------

type MsrAction uint8
type VirtualProcessorStateType uint32
type AllocateVpciResourceFlags uint32
type CreateVpciDeviceFlags uint32
type VpciDevicePropertyCode uint32
type DevicePowerState uint32
type NotificationPortProperty uint64

// -------------------------------------------------------------------------
// Structs representing Bitfields/Unions in C
// -------------------------------------------------------------------------

type ProcessorPerfmonFeatures struct {
	// Represents WHV_PROCESSOR_PERFMON_FEATURES (Union/Bitfield)
	AsUINT64 uint64
}

type SyntheticProcessorFeaturesBanks struct {
	// Represents WHV_SYNTHETIC_PROCESSOR_FEATURES_BANKS
	BanksCount uint32
	Reserved0  uint32
	Bank0      uint64 // WHV_SYNTHETIC_PROCESSOR_FEATURES
	// Bank1 would go here if BanksCount > 1, but C definition currently uses a union/array approach.
	// We map the static size (16 bytes)
}

// -------------------------------------------------------------------------
// CPUID and MSR Types
// -------------------------------------------------------------------------

type CpuidOutput struct {
	Eax uint32
	Ebx uint32
	Ecx uint32
	Edx uint32
}

type X64CpuidResult2 struct {
	Function uint32
	Index    uint32
	VpIndex  uint32
	Flags    uint32 // WHV_X64_CPUID_RESULT2_FLAGS
	Output   CpuidOutput
	Mask     CpuidOutput
}

type MsrActionEntry struct {
	Index       uint32
	ReadAction  uint8 // MsrAction
	WriteAction uint8 // MsrAction
	Reserved    uint16
}

// -------------------------------------------------------------------------
// Memory Management Types
// -------------------------------------------------------------------------

type AccessGpaControls struct {
	// Represents WHV_ACCESS_GPA_CONTROLS (Union)
	// Lower 64 bits contain CacheType (enum), InputVtl (union), and padding.
	// Since Go doesn't support unions, we hold the raw value and use helper methods.
	Raw uint64
}

func (c AccessGpaControls) AsUINT64() uint64 {
	return c.Raw
}

// -------------------------------------------------------------------------
// Virtual Processor Properties
// -------------------------------------------------------------------------

type VirtualProcessorProperty struct {
	PropertyCode uint32 // WHV_VIRTUAL_PROCESSOR_PROPERTY_CODE
	Reserved     uint32
	// Union: NumaNode (USHORT) or Padding (UINT64)
	Padding uint64
}

// -------------------------------------------------------------------------
// Virtual PCI (VPCI) Types
// -------------------------------------------------------------------------

type VpciDeviceNotification struct {
	NotificationType uint32 // WHV_VPCI_DEVICE_NOTIFICATION_TYPE
	Reserved1        uint32
	Reserved2        uint64
}

type VpciMmioMapping struct {
	Location       uint32 // WHV_VPCI_DEVICE_REGISTER_SPACE
	Flags          uint32 // WHV_VPCI_MMIO_RANGE_FLAGS
	SizeInBytes    uint64
	OffsetInBytes  uint64
	VirtualAddress unsafe.Pointer // PVOID
}

type VpciDeviceRegister struct {
	Location      uint32 // WHV_VPCI_DEVICE_REGISTER_SPACE
	SizeInBytes   uint32
	OffsetInBytes uint64
}

type VpciInterruptTarget struct {
	Vector         uint32
	Flags          uint32 // WHV_VPCI_INTERRUPT_TARGET_FLAGS
	ProcessorCount uint32
	// C header uses ANYSIZE_ARRAY. We define it as 1 for the base struct.
	// Use unsafe arithmetic if accessing more than one processor.
	Processors [1]uint32
}
