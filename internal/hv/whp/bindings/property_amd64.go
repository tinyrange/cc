//go:build windows && amd64

package bindings

import (
	"unsafe"
)

// PartitionProperties wraps various properties that can be set on a WHP partition.
// It corresponds to the union WHV_PARTITION_PROPERTY in the C header.
type PartitionProperties struct {
	// WHV_EXTENDED_VM_EXITS ExtendedVmExits
	ExtendedVmExits *ExtendedVmExits
	// WHV_PROCESSOR_FEATURES ProcessorFeatures
	// Note: Modern usage usually prefers ProcessorFeaturesBanks
	ProcessorFeatures *ProcessorFeatures
	// WHV_PROCESSOR_FEATURES_BANKS ProcessorFeaturesBanks
	ProcessorFeaturesBanks *ProcessorFeaturesBanks
	// WHV_SYNTHETIC_PROCESSOR_FEATURES_BANKS SyntheticProcessorFeaturesBanks
	SyntheticProcessorFeaturesBanks *SyntheticProcessorFeaturesBanks
	// WHV_PROCESSOR_XSAVE_FEATURES ProcessorXsaveFeatures
	ProcessorXsaveFeatures *ProcessorXsaveFeatures
	// WHV_PROCESSOR_PERFMON_FEATURES ProcessorPerfmonFeatures
	ProcessorPerfmonFeatures *ProcessorPerfmonFeatures
	// UINT8 ProcessorClFlushSize
	ProcessorClFlushSize *uint8
	// UINT32 ProcessorCount
	ProcessorCount *uint32
	// UINT32 CpuidExitList[]
	CpuidExitList []uint32
	// WHV_X64_CPUID_RESULT CpuidResultList[]
	CpuidResultList []X64CpuidResult
	// WHV_X64_CPUID_RESULT2 CpuidResultList2[]
	CpuidResultList2 []X64CpuidResult2
	// WHV_MSR_ACTION_ENTRY MsrActionList[]
	MsrActionList []MsrActionEntry
	// WHV_MSR_ACTION UnimplementedMsrAction
	UnimplementedMsrAction *MsrAction
	// UINT64 ExceptionExitBitmap
	ExceptionExitBitmap *uint64
	// WHV_X64_LOCAL_APIC_EMULATION_MODE LocalApicEmulationMode
	LocalApicEmulationMode *LocalApicEmulationMode
	// BOOL SeparateSecurityDomain
	SeparateSecurityDomain *bool
	// BOOL NestedVirtualization
	NestedVirtualization *bool
	// WHV_X64_MSR_EXIT_BITMAP X64MsrExitBitmap
	X64MsrExitBitmap *X64MsrExitBitmap
	// UINT64 ProcessorClockFrequency
	ProcessorClockFrequency *uint64
	// UINT64 InterruptClockFrequency
	InterruptClockFrequency *uint64
	// BOOL ApicRemoteRead
	ApicRemoteRead *bool
	// UINT32 PhysicalAddressWidth
	PhysicalAddressWidth *uint32
	// USHORT PrimaryNumaNode
	PrimaryNumaNode *uint16
	// UINT32 CpuReserve
	CpuReserve *uint32
	// UINT32 CpuCap
	CpuCap *uint32
	// UINT32 CpuWeight
	CpuWeight *uint32
	// UINT64 CpuGroupId
	CpuGroupId *uint64
	// UINT32 ProcessorFrequencyCap
	ProcessorFrequencyCap *uint32
	// BOOL AllowDeviceAssignment
	AllowDeviceAssignment *bool
	// BOOL DisableSmt
	DisableSmt *bool
}

// SetPartitionProperties applies the properties defined in the struct to the partition.
func SetPartitionProperties(partition PartitionHandle, props PartitionProperties) error {
	// 1. Standard Structs and Scalars
	if props.ExtendedVmExits != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeExtendedVmExits, *props.ExtendedVmExits); err != nil {
			return err
		}
	}
	if props.ProcessorFeatures != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeProcessorFeatures, *props.ProcessorFeatures); err != nil {
			return err
		}
	}
	if props.ProcessorFeaturesBanks != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeProcessorFeaturesBanks, *props.ProcessorFeaturesBanks); err != nil {
			return err
		}
	}
	if props.SyntheticProcessorFeaturesBanks != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeSyntheticProcessorFeaturesBanks, *props.SyntheticProcessorFeaturesBanks); err != nil {
			return err
		}
	}
	if props.ProcessorXsaveFeatures != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeProcessorXsaveFeatures, *props.ProcessorXsaveFeatures); err != nil {
			return err
		}
	}
	if props.ProcessorPerfmonFeatures != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeProcessorPerfmonFeatures, *props.ProcessorPerfmonFeatures); err != nil {
			return err
		}
	}
	if props.ProcessorClFlushSize != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeProcessorClFlushSize, *props.ProcessorClFlushSize); err != nil {
			return err
		}
	}
	if props.ProcessorCount != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeProcessorCount, *props.ProcessorCount); err != nil {
			return err
		}
	}
	if props.ExceptionExitBitmap != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeExceptionExitBitmap, *props.ExceptionExitBitmap); err != nil {
			return err
		}
	}
	if props.LocalApicEmulationMode != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeLocalApicEmulationMode, *props.LocalApicEmulationMode); err != nil {
			return err
		}
	}
	if props.X64MsrExitBitmap != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeX64MsrExitBitmap, *props.X64MsrExitBitmap); err != nil {
			return err
		}
	}
	if props.ProcessorClockFrequency != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeProcessorClockFrequency, *props.ProcessorClockFrequency); err != nil {
			return err
		}
	}
	if props.InterruptClockFrequency != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeInterruptClockFrequency, *props.InterruptClockFrequency); err != nil {
			return err
		}
	}
	if props.PhysicalAddressWidth != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodePhysicalAddressWidth, *props.PhysicalAddressWidth); err != nil {
			return err
		}
	}
	if props.UnimplementedMsrAction != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeUnimplementedMsrAction, *props.UnimplementedMsrAction); err != nil {
			return err
		}
	}
	if props.PrimaryNumaNode != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodePrimaryNumaNode, *props.PrimaryNumaNode); err != nil {
			return err
		}
	}
	if props.CpuReserve != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeCpuReserve, *props.CpuReserve); err != nil {
			return err
		}
	}
	if props.CpuCap != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeCpuCap, *props.CpuCap); err != nil {
			return err
		}
	}
	if props.CpuWeight != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeCpuWeight, *props.CpuWeight); err != nil {
			return err
		}
	}
	if props.CpuGroupId != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeCpuGroupId, *props.CpuGroupId); err != nil {
			return err
		}
	}
	if props.ProcessorFrequencyCap != nil {
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeProcessorFrequencyCap, *props.ProcessorFrequencyCap); err != nil {
			return err
		}
	}

	// 2. BOOL properties
	// C BOOL is 4 bytes (int32). Go bool is 1 byte. We must convert to uint32 (TRUE=1, FALSE=0).
	if props.SeparateSecurityDomain != nil {
		val := uint32(0)
		if *props.SeparateSecurityDomain {
			val = 1
		}
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeSeparateSecurityDomain, val); err != nil {
			return err
		}
	}
	if props.NestedVirtualization != nil {
		val := uint32(0)
		if *props.NestedVirtualization {
			val = 1
		}
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeNestedVirtualization, val); err != nil {
			return err
		}
	}
	if props.ApicRemoteRead != nil {
		val := uint32(0)
		if *props.ApicRemoteRead {
			val = 1
		}
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeApicRemoteReadSupport, val); err != nil {
			return err
		}
	}
	if props.AllowDeviceAssignment != nil {
		val := uint32(0)
		if *props.AllowDeviceAssignment {
			val = 1
		}
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeAllowDeviceAssignment, val); err != nil {
			return err
		}
	}
	if props.DisableSmt != nil {
		val := uint32(0)
		if *props.DisableSmt {
			val = 1
		}
		if err := SetPartitionPropertyUnsafe(partition, PartitionPropertyCodeDisableSmt, val); err != nil {
			return err
		}
	}

	// 3. Slice/Array Properties
	// Generic Unsafe wrappers usually infer size from the value type. For slices, that would be the slice header size.
	// We must explicitly pass the pointer to data and the calculated byte size.
	if len(props.CpuidExitList) > 0 {
		size := uintptr(len(props.CpuidExitList)) * unsafe.Sizeof(props.CpuidExitList[0])
		if err := SetPartitionProperty(partition, PartitionPropertyCodeCpuidExitList, unsafe.Pointer(&props.CpuidExitList[0]), uint32(size)); err != nil {
			return err
		}
	}

	if len(props.CpuidResultList) > 0 {
		size := uintptr(len(props.CpuidResultList)) * unsafe.Sizeof(props.CpuidResultList[0])
		if err := SetPartitionProperty(partition, PartitionPropertyCodeCpuidResultList, unsafe.Pointer(&props.CpuidResultList[0]), uint32(size)); err != nil {
			return err
		}
	}

	if len(props.CpuidResultList2) > 0 {
		size := uintptr(len(props.CpuidResultList2)) * unsafe.Sizeof(props.CpuidResultList2[0])
		if err := SetPartitionProperty(partition, PartitionPropertyCodeCpuidResultList2, unsafe.Pointer(&props.CpuidResultList2[0]), uint32(size)); err != nil {
			return err
		}
	}

	if len(props.MsrActionList) > 0 {
		size := uintptr(len(props.MsrActionList)) * unsafe.Sizeof(props.MsrActionList[0])
		if err := SetPartitionProperty(partition, PartitionPropertyCodeMsrActionList, unsafe.Pointer(&props.MsrActionList[0]), uint32(size)); err != nil {
			return err
		}
	}

	return nil
}

// Getters for Capabilities.

func GetProcessorFeatures() (ProcessorFeatures, error) {
	return GetCapabilityUnsafe[ProcessorFeatures](CapabilityCodeProcessorFeatures)
}

func GetProcessorFeaturesBanks() (ProcessorFeaturesBanks, error) {
	return GetCapabilityUnsafe[ProcessorFeaturesBanks](CapabilityCodeProcessorFeaturesBanks)
}

func GetProcessorXsaveFeatures() (ProcessorXsaveFeatures, error) {
	return GetCapabilityUnsafe[ProcessorXsaveFeatures](CapabilityCodeProcessorXsaveFeatures)
}

func GetProcessorPerfmonFeatures() (ProcessorPerfmonFeatures, error) {
	return GetCapabilityUnsafe[ProcessorPerfmonFeatures](CapabilityCodeProcessorPerfmonFeatures)
}
