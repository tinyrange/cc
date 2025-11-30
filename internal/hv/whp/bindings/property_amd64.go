//go:build windows && amd64

package bindings

// PartitionProperties wraps various properties that can be set on a WHP partition.

type PartitionProperties struct {
	// WHV_EXTENDED_VM_EXITS ExtendedVmExits;
	ExtendedVmExits *ExtendedVmExits
	// WHV_PROCESSOR_FEATURES ProcessorFeatures;
	ProcessorFeatures *ProcessorFeatures
	// WHV_PROCESSOR_XSAVE_FEATURES ProcessorXsaveFeatures;
	ProcessorXsaveFeatures *ProcessorXsaveFeatures
	// UINT8 ProcessorClFlushSize;
	ProcessorClFlushSize *uint8
	// UINT32 ProcessorCount;
	ProcessorCount *uint32
	// UINT32 CpuidExitList[1];
	CpuidExitList *[]uint32
	// WHV_X64_CPUID_RESULT CpuidResultList[1];
	CpuidResultList *[]X64CpuidResult
	// UINT64 ExceptionExitBitmap;
	ExceptionExitBitmap *uint64
	// WHV_X64_LOCAL_APIC_EMULATION_MODE LocalApicEmulationMode;
	LocalApicEmulationMode *LocalApicEmulationMode
	// BOOL SeparateSecurityDomain;
	SeparateSecurityDomain *bool
	// BOOL NestedVirtualization;
	NestedVirtualization *bool
	// WHV_X64_MSR_EXIT_BITMAP X64MsrExitBitmap;
	X64MsrExitBitmap *X64MsrExitBitmap
	// UINT64 ProcessorClockFrequency;
	ProcessorClockFrequency *uint64
	// UINT64 InterruptClockFrequency;
	InterruptClockFrequency *uint64
}

func SetPartitionProperties(partition PartitionHandle, props PartitionProperties) error {
	if props.ExtendedVmExits != nil {
		if err := SetPartitionPropertyUnsafe(
			partition,
			PartitionPropertyCodeExtendedVmExits,
			*props.ExtendedVmExits,
		); err != nil {
			return err
		}
	}
	if props.ProcessorFeatures != nil {
		if err := SetPartitionPropertyUnsafe(
			partition,
			PartitionPropertyCodeProcessorFeatures,
			*props.ProcessorFeatures,
		); err != nil {
			return err
		}
	}
	if props.ProcessorXsaveFeatures != nil {
		if err := SetPartitionPropertyUnsafe(
			partition,
			PartitionPropertyCodeProcessorXsaveFeatures,
			*props.ProcessorXsaveFeatures,
		); err != nil {
			return err
		}
	}
	if props.ProcessorClFlushSize != nil {
		if err := SetPartitionPropertyUnsafe(
			partition,
			PartitionPropertyCodeProcessorClFlushSize,
			*props.ProcessorClFlushSize,
		); err != nil {
			return err
		}
	}
	if props.ProcessorCount != nil {
		if err := SetPartitionPropertyUnsafe(
			partition,
			PartitionPropertyCodeProcessorCount,
			*props.ProcessorCount,
		); err != nil {
			return err
		}
	}
	if props.CpuidExitList != nil {
		if err := SetPartitionPropertyUnsafe(
			partition,
			PartitionPropertyCodeCpuidExitList,
			*props.CpuidExitList,
		); err != nil {
			return err
		}
	}
	if props.CpuidResultList != nil {
		if err := SetPartitionPropertyUnsafe(
			partition,
			PartitionPropertyCodeCpuidResultList,
			*props.CpuidResultList,
		); err != nil {
			return err
		}
	}
	if props.ExceptionExitBitmap != nil {
		if err := SetPartitionPropertyUnsafe(
			partition,
			PartitionPropertyCodeExceptionExitBitmap,
			*props.ExceptionExitBitmap,
		); err != nil {
			return err
		}
	}
	if props.LocalApicEmulationMode != nil {
		if err := SetPartitionPropertyUnsafe(
			partition,
			PartitionPropertyCodeLocalApicEmulationMode,
			*props.LocalApicEmulationMode,
		); err != nil {
			return err
		}
	}
	if props.SeparateSecurityDomain != nil {
		if err := SetPartitionPropertyUnsafe(
			partition,
			PartitionPropertyCodeSeparateSecurityDomain,
			*props.SeparateSecurityDomain,
		); err != nil {
			return err
		}
	}
	if props.NestedVirtualization != nil {
		if err := SetPartitionPropertyUnsafe(
			partition,
			PartitionPropertyCodeNestedVirtualization,
			*props.NestedVirtualization,
		); err != nil {
			return err
		}
	}
	if props.X64MsrExitBitmap != nil {
		if err := SetPartitionPropertyUnsafe(
			partition,
			PartitionPropertyCodeX64MsrExitBitmap,
			*props.X64MsrExitBitmap,
		); err != nil {
			return err
		}
	}
	if props.ProcessorClockFrequency != nil {
		if err := SetPartitionPropertyUnsafe(
			partition,
			PartitionPropertyCodeProcessorClockFrequency,
			*props.ProcessorClockFrequency,
		); err != nil {
			return err
		}
	}
	if props.InterruptClockFrequency != nil {
		if err := SetPartitionPropertyUnsafe(
			partition,
			PartitionPropertyCodeInterruptClockFrequency,
			*props.InterruptClockFrequency,
		); err != nil {
			return err
		}
	}
	return nil
}

// Getters for Capabilities.

func GetProcessorFeatures() (ProcessorFeatures, error) {
	return GetCapabilityUnsafe[ProcessorFeatures](CapabilityCodeProcessorFeatures)
}

func GetProcessorXsaveFeatures() (ProcessorXsaveFeatures, error) {
	return GetCapabilityUnsafe[ProcessorXsaveFeatures](CapabilityCodeProcessorXsaveFeatures)
}
