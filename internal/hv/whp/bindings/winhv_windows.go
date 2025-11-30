//go:build windows && (amd64 || arm64)

package bindings

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	modWinHvPlatform = syscall.NewLazyDLL("winhvplatform.dll")

	// Platform Capabilities
	procWHvGetCapability = modWinHvPlatform.NewProc("WHvGetCapability")

	// Partition Management
	procWHvCreatePartition      = modWinHvPlatform.NewProc("WHvCreatePartition")
	procWHvSetupPartition       = modWinHvPlatform.NewProc("WHvSetupPartition")
	procWHvResetPartition       = modWinHvPlatform.NewProc("WHvResetPartition")
	procWHvDeletePartition      = modWinHvPlatform.NewProc("WHvDeletePartition")
	procWHvGetPartitionProperty = modWinHvPlatform.NewProc("WHvGetPartitionProperty")
	procWHvSetPartitionProperty = modWinHvPlatform.NewProc("WHvSetPartitionProperty")
	procWHvSuspendPartitionTime = modWinHvPlatform.NewProc("WHvSuspendPartitionTime")
	procWHvResumePartitionTime  = modWinHvPlatform.NewProc("WHvResumePartitionTime")

	// Memory Management
	procWHvMapGpaRange              = modWinHvPlatform.NewProc("WHvMapGpaRange")
	procWHvMapGpaRange2             = modWinHvPlatform.NewProc("WHvMapGpaRange2")
	procWHvUnmapGpaRange            = modWinHvPlatform.NewProc("WHvUnmapGpaRange")
	procWHvTranslateGva             = modWinHvPlatform.NewProc("WHvTranslateGva")
	procWHvQueryGpaRangeDirtyBitmap = modWinHvPlatform.NewProc("WHvQueryGpaRangeDirtyBitmap")
	procWHvAdviseGpaRange           = modWinHvPlatform.NewProc("WHvAdviseGpaRange")
	procWHvReadGpaRange             = modWinHvPlatform.NewProc("WHvReadGpaRange")
	procWHvWriteGpaRange            = modWinHvPlatform.NewProc("WHvWriteGpaRange")

	// Virtual Processors
	procWHvCreateVirtualProcessor       = modWinHvPlatform.NewProc("WHvCreateVirtualProcessor")
	procWHvCreateVirtualProcessor2      = modWinHvPlatform.NewProc("WHvCreateVirtualProcessor2")
	procWHvDeleteVirtualProcessor       = modWinHvPlatform.NewProc("WHvDeleteVirtualProcessor")
	procWHvRunVirtualProcessor          = modWinHvPlatform.NewProc("WHvRunVirtualProcessor")
	procWHvCancelRunVirtualProcessor    = modWinHvPlatform.NewProc("WHvCancelRunVirtualProcessor")
	procWHvGetVirtualProcessorRegisters = modWinHvPlatform.NewProc("WHvGetVirtualProcessorRegisters")
	procWHvSetVirtualProcessorRegisters = modWinHvPlatform.NewProc("WHvSetVirtualProcessorRegisters")
	procWHvGetVirtualProcessorState     = modWinHvPlatform.NewProc("WHvGetVirtualProcessorState")
	procWHvSetVirtualProcessorState     = modWinHvPlatform.NewProc("WHvSetVirtualProcessorState")

	// Interrupts & Synic
	procWHvRequestInterrupt                 = modWinHvPlatform.NewProc("WHvRequestInterrupt")
	procWHvSignalVirtualProcessorSynicEvent = modWinHvPlatform.NewProc("WHvSignalVirtualProcessorSynicEvent")
	procWHvPostVirtualProcessorSynicMessage = modWinHvPlatform.NewProc("WHvPostVirtualProcessorSynicMessage")

	// Counters
	procWHvGetPartitionCounters        = modWinHvPlatform.NewProc("WHvGetPartitionCounters")
	procWHvGetVirtualProcessorCounters = modWinHvPlatform.NewProc("WHvGetVirtualProcessorCounters")

	// Virtual PCI (VPCI)
	procWHvAllocateVpciResource         = modWinHvPlatform.NewProc("WHvAllocateVpciResource")
	procWHvCreateVpciDevice             = modWinHvPlatform.NewProc("WHvCreateVpciDevice")
	procWHvDeleteVpciDevice             = modWinHvPlatform.NewProc("WHvDeleteVpciDevice")
	procWHvGetVpciDeviceProperty        = modWinHvPlatform.NewProc("WHvGetVpciDeviceProperty")
	procWHvGetVpciDeviceNotification    = modWinHvPlatform.NewProc("WHvGetVpciDeviceNotification")
	procWHvMapVpciDeviceMmioRanges      = modWinHvPlatform.NewProc("WHvMapVpciDeviceMmioRanges")
	procWHvUnmapVpciDeviceMmioRanges    = modWinHvPlatform.NewProc("WHvUnmapVpciDeviceMmioRanges")
	procWHvSetVpciDevicePowerState      = modWinHvPlatform.NewProc("WHvSetVpciDevicePowerState")
	procWHvReadVpciDeviceRegister       = modWinHvPlatform.NewProc("WHvReadVpciDeviceRegister")
	procWHvWriteVpciDeviceRegister      = modWinHvPlatform.NewProc("WHvWriteVpciDeviceRegister")
	procWHvMapVpciDeviceInterrupt       = modWinHvPlatform.NewProc("WHvMapVpciDeviceInterrupt")
	procWHvUnmapVpciDeviceInterrupt     = modWinHvPlatform.NewProc("WHvUnmapVpciDeviceInterrupt")
	procWHvRetargetVpciDeviceInterrupt  = modWinHvPlatform.NewProc("WHvRetargetVpciDeviceInterrupt")
	procWHvRequestVpciDeviceInterrupt   = modWinHvPlatform.NewProc("WHvRequestVpciDeviceInterrupt")
	procWHvGetVpciDeviceInterruptTarget = modWinHvPlatform.NewProc("WHvGetVpciDeviceInterruptTarget")

	// Triggers
	procWHvCreateTrigger           = modWinHvPlatform.NewProc("WHvCreateTrigger")
	procWHvUpdateTriggerParameters = modWinHvPlatform.NewProc("WHvUpdateTriggerParameters")
	procWHvDeleteTrigger           = modWinHvPlatform.NewProc("WHvDeleteTrigger")

	// Notification Ports
	procWHvCreateNotificationPort      = modWinHvPlatform.NewProc("WHvCreateNotificationPort")
	procWHvSetNotificationPortProperty = modWinHvPlatform.NewProc("WHvSetNotificationPortProperty")
	procWHvDeleteNotificationPort      = modWinHvPlatform.NewProc("WHvDeleteNotificationPort")

	// Migration
	procWHvStartPartitionMigration    = modWinHvPlatform.NewProc("WHvStartPartitionMigration")
	procWHvCancelPartitionMigration   = modWinHvPlatform.NewProc("WHvCancelPartitionMigration")
	procWHvCompletePartitionMigration = modWinHvPlatform.NewProc("WHvCompletePartitionMigration")
	procWHvAcceptPartitionMigration   = modWinHvPlatform.NewProc("WHvAcceptPartitionMigration")

	// Deprecated / Legacy Wrappers (Maintained for compatibility)
	procWHvGetVirtualProcessorInterruptControllerState  = modWinHvPlatform.NewProc("WHvGetVirtualProcessorInterruptControllerState")
	procWHvSetVirtualProcessorInterruptControllerState  = modWinHvPlatform.NewProc("WHvSetVirtualProcessorInterruptControllerState")
	procWHvGetVirtualProcessorXsaveState                = modWinHvPlatform.NewProc("WHvGetVirtualProcessorXsaveState")
	procWHvSetVirtualProcessorXsaveState                = modWinHvPlatform.NewProc("WHvSetVirtualProcessorXsaveState")
	procWHvGetVirtualProcessorInterruptControllerState2 = modWinHvPlatform.NewProc("WHvGetVirtualProcessorInterruptControllerState2")
	procWHvSetVirtualProcessorInterruptControllerState2 = modWinHvPlatform.NewProc("WHvSetVirtualProcessorInterruptControllerState2")
	procWHvRegisterPartitionDoorbellEvent               = modWinHvPlatform.NewProc("WHvRegisterPartitionDoorbellEvent")
	procWHvUnregisterPartitionDoorbellEvent             = modWinHvPlatform.NewProc("WHvUnregisterPartitionDoorbellEvent")
)

// AMD64 Specific Procs
var (
	procWHvGetVirtualProcessorCpuidOutput = modWinHvPlatform.NewProc("WHvGetVirtualProcessorCpuidOutput")
	procWHvGetInterruptTargetVpSet        = modWinHvPlatform.NewProc("WHvGetInterruptTargetVpSet")
)

func toHRESULT(r uintptr) HRESULT {
	return HRESULT(int32(r))
}

func callHRESULT(proc *syscall.LazyProc, args ...uintptr) (HRESULT, error) {
	r1, _, callErr := proc.Call(args...)
	if callErr != syscall.Errno(0) && r1 == 0 {
		return 0, callErr
	}
	hr := toHRESULT(r1)
	if err := hr.Err(); err != nil {
		return hr, err
	}
	return hr, nil
}

// GetCapability wraps WHvGetCapability.
func GetCapability(code CapabilityCode, buffer unsafe.Pointer, bufferSize uint32) (uint32, error) {
	var written uint32
	_, err := callHRESULT(procWHvGetCapability,
		uintptr(code),
		uintptr(buffer),
		uintptr(bufferSize),
		uintptr(unsafe.Pointer(&written)),
	)
	return written, err
}

func GetCapabilityUnsafe[T any](code CapabilityCode) (T, error) {
	var value T
	size := uint32(unsafe.Sizeof(value))
	_, err := callHRESULT(procWHvGetCapability,
		uintptr(code),
		uintptr(unsafe.Pointer(&value)),
		uintptr(size),
	)
	return value, err
}

func IsHypervisorPresent() (bool, error) {
	var present uint32 // Using uint32 for BOOL
	written, err := GetCapability(
		CapabilityCodeHypervisorPresent,
		unsafe.Pointer(&present),
		uint32(unsafe.Sizeof(present)),
	)
	if err != nil {
		return false, fmt.Errorf("WHvGetCapability failed: %w", err)
	}
	if written < uint32(unsafe.Sizeof(present)) {
		return false, fmt.Errorf("expected at least %d bytes, got %d", unsafe.Sizeof(present), written)
	}
	return present != 0, nil
}

// CreatePartition wraps WHvCreatePartition.
func CreatePartition() (PartitionHandle, error) {
	var handle PartitionHandle
	_, err := callHRESULT(procWHvCreatePartition, uintptr(unsafe.Pointer(&handle)))
	return handle, err
}

// SetupPartition wraps WHvSetupPartition.
func SetupPartition(partition PartitionHandle) error {
	_, err := callHRESULT(procWHvSetupPartition, uintptr(partition))
	return err
}

// ResetPartition wraps WHvResetPartition.
func ResetPartition(partition PartitionHandle) error {
	_, err := callHRESULT(procWHvResetPartition, uintptr(partition))
	return err
}

// DeletePartition wraps WHvDeletePartition.
func DeletePartition(partition PartitionHandle) error {
	_, err := callHRESULT(procWHvDeletePartition, uintptr(partition))
	return err
}

// GetPartitionProperty wraps WHvGetPartitionProperty.
func GetPartitionProperty(partition PartitionHandle, code PartitionPropertyCode, buffer unsafe.Pointer, bufferSize uint32) (uint32, error) {
	var written uint32
	_, err := callHRESULT(procWHvGetPartitionProperty,
		uintptr(partition),
		uintptr(code),
		uintptr(buffer),
		uintptr(bufferSize),
		uintptr(unsafe.Pointer(&written)),
	)
	return written, err
}

// SetPartitionProperty wraps WHvSetPartitionProperty.
func SetPartitionProperty(partition PartitionHandle, code PartitionPropertyCode, buffer unsafe.Pointer, bufferSize uint32) error {
	_, err := callHRESULT(procWHvSetPartitionProperty,
		uintptr(partition),
		uintptr(code),
		uintptr(buffer),
		uintptr(bufferSize),
	)
	return err
}

func SetPartitionPropertyUnsafe[T any](partition PartitionHandle, code PartitionPropertyCode, value T) error {
	size := uint32(unsafe.Sizeof(value))
	_, err := callHRESULT(procWHvSetPartitionProperty,
		uintptr(partition),
		uintptr(code),
		uintptr(unsafe.Pointer(&value)),
		uintptr(size),
	)
	return err
}

// SuspendPartitionTime wraps WHvSuspendPartitionTime.
func SuspendPartitionTime(partition PartitionHandle) error {
	_, err := callHRESULT(procWHvSuspendPartitionTime, uintptr(partition))
	return err
}

// ResumePartitionTime wraps WHvResumePartitionTime.
func ResumePartitionTime(partition PartitionHandle) error {
	_, err := callHRESULT(procWHvResumePartitionTime, uintptr(partition))
	return err
}

// MapGPARange wraps WHvMapGpaRange.
func MapGPARange(partition PartitionHandle, source unsafe.Pointer, guestAddress GuestPhysicalAddress, sizeInBytes uint64, flags MapGPARangeFlags) error {
	_, err := callHRESULT(procWHvMapGpaRange,
		uintptr(partition),
		uintptr(source),
		uintptr(guestAddress),
		uintptr(sizeInBytes),
		uintptr(flags),
	)
	return err
}

// MapGPARange2 wraps WHvMapGpaRange2.
func MapGPARange2(partition PartitionHandle, process syscall.Handle, source unsafe.Pointer, guestAddress GuestPhysicalAddress, sizeInBytes uint64, flags MapGPARangeFlags) error {
	_, err := callHRESULT(procWHvMapGpaRange2,
		uintptr(partition),
		uintptr(process),
		uintptr(source),
		uintptr(guestAddress),
		uintptr(sizeInBytes),
		uintptr(flags),
	)
	return err
}

// UnmapGPARange wraps WHvUnmapGpaRange.
func UnmapGPARange(partition PartitionHandle, guestAddress GuestPhysicalAddress, sizeInBytes uint64) error {
	_, err := callHRESULT(procWHvUnmapGpaRange,
		uintptr(partition),
		uintptr(guestAddress),
		uintptr(sizeInBytes),
	)
	return err
}

// TranslateGVA wraps WHvTranslateGva.
func TranslateGVA(partition PartitionHandle, vpIndex uint32, gva GuestVirtualAddress, flags TranslateGVAFlags, result *TranslateGVAResult, gpa *GuestPhysicalAddress) error {
	_, err := callHRESULT(procWHvTranslateGva,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(gva),
		uintptr(flags),
		uintptr(unsafe.Pointer(result)),
		uintptr(unsafe.Pointer(gpa)),
	)
	return err
}

// AdviseGpaRange wraps WHvAdviseGpaRange.
func AdviseGpaRange(partition PartitionHandle, ranges []MemoryRangeEntry, advice AdviseGpaRangeCode, adviceBuffer unsafe.Pointer, adviceBufferSize uint32) error {
	var rangesPtr uintptr
	if len(ranges) > 0 {
		rangesPtr = uintptr(unsafe.Pointer(&ranges[0]))
	}
	_, err := callHRESULT(procWHvAdviseGpaRange,
		uintptr(partition),
		rangesPtr,
		uintptr(len(ranges)),
		uintptr(advice),
		uintptr(adviceBuffer),
		uintptr(adviceBufferSize),
	)
	return err
}

// ReadGpaRange wraps WHvReadGpaRange.
func ReadGpaRange(partition PartitionHandle, vpIndex uint32, guestAddress GuestPhysicalAddress, controls AccessGpaControls, data unsafe.Pointer, sizeInBytes uint32) error {
	_, err := callHRESULT(procWHvReadGpaRange,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(guestAddress),
		uintptr(controls.AsUINT64()),
		uintptr(data),
		uintptr(sizeInBytes),
	)
	return err
}

// WriteGpaRange wraps WHvWriteGpaRange.
func WriteGpaRange(partition PartitionHandle, vpIndex uint32, guestAddress GuestPhysicalAddress, controls AccessGpaControls, data unsafe.Pointer, sizeInBytes uint32) error {
	_, err := callHRESULT(procWHvWriteGpaRange,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(guestAddress),
		uintptr(controls.AsUINT64()),
		uintptr(data),
		uintptr(sizeInBytes),
	)
	return err
}

// CreateVirtualProcessor wraps WHvCreateVirtualProcessor.
func CreateVirtualProcessor(partition PartitionHandle, vpIndex uint32, flags uint32) error {
	_, err := callHRESULT(procWHvCreateVirtualProcessor,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(flags),
	)
	return err
}

// CreateVirtualProcessor2 wraps WHvCreateVirtualProcessor2.
func CreateVirtualProcessor2(partition PartitionHandle, vpIndex uint32, properties []VirtualProcessorProperty) error {
	var propsPtr uintptr
	if len(properties) > 0 {
		propsPtr = uintptr(unsafe.Pointer(&properties[0]))
	}
	_, err := callHRESULT(procWHvCreateVirtualProcessor2,
		uintptr(partition),
		uintptr(vpIndex),
		propsPtr,
		uintptr(len(properties)),
	)
	return err
}

// DeleteVirtualProcessor wraps WHvDeleteVirtualProcessor.
func DeleteVirtualProcessor(partition PartitionHandle, vpIndex uint32) error {
	_, err := callHRESULT(procWHvDeleteVirtualProcessor,
		uintptr(partition),
		uintptr(vpIndex),
	)
	return err
}

// RunVirtualProcessorRaw wraps WHvRunVirtualProcessor.
func RunVirtualProcessorRaw(partition PartitionHandle, vpIndex uint32, exitContext unsafe.Pointer, exitContextSize uint32) error {
	_, err := callHRESULT(procWHvRunVirtualProcessor,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(exitContext),
		uintptr(exitContextSize),
	)
	return err
}

// RunVirtualProcessorContext is a typed helper for WHvRunVirtualProcessor.
func RunVirtualProcessorContext(partition PartitionHandle, vpIndex uint32, exitContext *RunVPExitContext) error {
	size := uint32(unsafe.Sizeof(*exitContext))
	return RunVirtualProcessorRaw(partition, vpIndex, unsafe.Pointer(exitContext), size)
}

// CancelRunVirtualProcessor wraps WHvCancelRunVirtualProcessor.
func CancelRunVirtualProcessor(partition PartitionHandle, vpIndex uint32, flags uint32) error {
	_, err := callHRESULT(procWHvCancelRunVirtualProcessor,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(flags),
	)
	return err
}

func checkRegisterLengths(names []RegisterName, values []RegisterValue) error {
	if len(values) < len(names) {
		return fmt.Errorf("whp: register value slice (%d) smaller than names (%d)", len(values), len(names))
	}
	return nil
}

// GetVirtualProcessorRegisters wraps WHvGetVirtualProcessorRegisters.
func GetVirtualProcessorRegisters(partition PartitionHandle, vpIndex uint32, names []RegisterName, values []RegisterValue) error {
	if err := checkRegisterLengths(names, values); err != nil {
		return err
	}
	var namesPtr uintptr
	if len(names) > 0 {
		namesPtr = uintptr(unsafe.Pointer(&names[0]))
	}
	var valuesPtr uintptr
	if len(values) > 0 {
		valuesPtr = uintptr(unsafe.Pointer(&values[0]))
	}
	_, err := callHRESULT(procWHvGetVirtualProcessorRegisters,
		uintptr(partition),
		uintptr(vpIndex),
		namesPtr,
		uintptr(len(names)),
		valuesPtr,
	)
	return err
}

// SetVirtualProcessorRegisters wraps WHvSetVirtualProcessorRegisters.
func SetVirtualProcessorRegisters(partition PartitionHandle, vpIndex uint32, names []RegisterName, values []RegisterValue) error {
	if err := checkRegisterLengths(names, values); err != nil {
		return err
	}
	if len(names) == 0 {
		return nil
	}
	_, err := callHRESULT(procWHvSetVirtualProcessorRegisters,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(unsafe.Pointer(&names[0])),
		uintptr(len(names)),
		uintptr(unsafe.Pointer(&values[0])),
	)
	return err
}

// GetVirtualProcessorState wraps WHvGetVirtualProcessorState.
func GetVirtualProcessorState(partition PartitionHandle, vpIndex uint32, stateType VirtualProcessorStateType, buffer unsafe.Pointer, bufferSize uint32) (uint32, error) {
	var written uint32
	_, err := callHRESULT(procWHvGetVirtualProcessorState,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(stateType),
		uintptr(buffer),
		uintptr(bufferSize),
		uintptr(unsafe.Pointer(&written)),
	)
	return written, err
}

// SetVirtualProcessorState wraps WHvSetVirtualProcessorState.
func SetVirtualProcessorState(partition PartitionHandle, vpIndex uint32, stateType VirtualProcessorStateType, buffer unsafe.Pointer, bufferSize uint32) error {
	_, err := callHRESULT(procWHvSetVirtualProcessorState,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(stateType),
		uintptr(buffer),
		uintptr(bufferSize),
	)
	return err
}

// SignalVirtualProcessorSynicEvent wraps WHvSignalVirtualProcessorSynicEvent.
func SignalVirtualProcessorSynicEvent(partition PartitionHandle, synicEvent SynicEventParameters) (bool, error) {
	var newlySignaled uint32 // BOOL
	_, err := callHRESULT(procWHvSignalVirtualProcessorSynicEvent,
		uintptr(partition),
		uintptr(unsafe.Pointer(&synicEvent)),
		uintptr(unsafe.Pointer(&newlySignaled)),
	)
	return newlySignaled != 0, err
}

// PostVirtualProcessorSynicMessage wraps WHvPostVirtualProcessorSynicMessage.
func PostVirtualProcessorSynicMessage(partition PartitionHandle, vpIndex uint32, sintIndex uint32, message unsafe.Pointer, messageSize uint32) error {
	_, err := callHRESULT(procWHvPostVirtualProcessorSynicMessage,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(sintIndex),
		uintptr(message),
		uintptr(messageSize),
	)
	return err
}

// RequestInterrupt wraps WHvRequestInterrupt.
func RequestInterrupt(partition PartitionHandle, control *InterruptControl) error {
	_, err := callHRESULT(procWHvRequestInterrupt,
		uintptr(partition),
		uintptr(unsafe.Pointer(control)),
		uintptr(unsafe.Sizeof(*control)),
	)
	return err
}

// QueryGpaRangeDirtyBitmap wraps WHvQueryGpaRangeDirtyBitmap.
func QueryGpaRangeDirtyBitmap(partition PartitionHandle, guestAddress GuestPhysicalAddress, rangeSize uint64, bitmap unsafe.Pointer, bitmapSize uint32) error {
	_, err := callHRESULT(procWHvQueryGpaRangeDirtyBitmap,
		uintptr(partition),
		uintptr(guestAddress),
		uintptr(rangeSize),
		uintptr(bitmap),
		uintptr(bitmapSize),
	)
	return err
}

// GetPartitionCounters wraps WHvGetPartitionCounters.
func GetPartitionCounters(partition PartitionHandle, counterSet PartitionCounterSet, buffer unsafe.Pointer, bufferSize uint32) (uint32, error) {
	var written uint32
	_, err := callHRESULT(procWHvGetPartitionCounters,
		uintptr(partition),
		uintptr(counterSet),
		uintptr(buffer),
		uintptr(bufferSize),
		uintptr(unsafe.Pointer(&written)),
	)
	return written, err
}

// GetVirtualProcessorCounters wraps WHvGetVirtualProcessorCounters.
func GetVirtualProcessorCounters(partition PartitionHandle, vpIndex uint32, counterSet ProcessorCounterSet, buffer unsafe.Pointer, bufferSize uint32) (uint32, error) {
	var written uint32
	_, err := callHRESULT(procWHvGetVirtualProcessorCounters,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(counterSet),
		uintptr(buffer),
		uintptr(bufferSize),
		uintptr(unsafe.Pointer(&written)),
	)
	return written, err
}

// --- Virtual PCI ---

// AllocateVpciResource wraps WHvAllocateVpciResource.
func AllocateVpciResource(providerID *syscall.GUID, flags AllocateVpciResourceFlags, resourceDescriptor unsafe.Pointer, resourceDescriptorSize uint32) (syscall.Handle, error) {
	var resource syscall.Handle
	_, err := callHRESULT(procWHvAllocateVpciResource,
		uintptr(unsafe.Pointer(providerID)),
		uintptr(flags),
		uintptr(resourceDescriptor),
		uintptr(resourceDescriptorSize),
		uintptr(unsafe.Pointer(&resource)),
	)
	return resource, err
}

// CreateVpciDevice wraps WHvCreateVpciDevice.
func CreateVpciDevice(partition PartitionHandle, logicalDeviceID uint64, vpciResource syscall.Handle, flags CreateVpciDeviceFlags, notificationEvent syscall.Handle) error {
	_, err := callHRESULT(procWHvCreateVpciDevice,
		uintptr(partition),
		uintptr(logicalDeviceID),
		uintptr(vpciResource),
		uintptr(flags),
		uintptr(notificationEvent),
	)
	return err
}

// DeleteVpciDevice wraps WHvDeleteVpciDevice.
func DeleteVpciDevice(partition PartitionHandle, logicalDeviceID uint64) error {
	_, err := callHRESULT(procWHvDeleteVpciDevice,
		uintptr(partition),
		uintptr(logicalDeviceID),
	)
	return err
}

// GetVpciDeviceProperty wraps WHvGetVpciDeviceProperty.
func GetVpciDeviceProperty(partition PartitionHandle, logicalDeviceID uint64, propertyCode VpciDevicePropertyCode, propertyBuffer unsafe.Pointer, propertyBufferSize uint32) (uint32, error) {
	var written uint32
	_, err := callHRESULT(procWHvGetVpciDeviceProperty,
		uintptr(partition),
		uintptr(logicalDeviceID),
		uintptr(propertyCode),
		uintptr(propertyBuffer),
		uintptr(propertyBufferSize),
		uintptr(unsafe.Pointer(&written)),
	)
	return written, err
}

// GetVpciDeviceNotification wraps WHvGetVpciDeviceNotification.
func GetVpciDeviceNotification(partition PartitionHandle, logicalDeviceID uint64, notification *VpciDeviceNotification, notificationSize uint32) error {
	_, err := callHRESULT(procWHvGetVpciDeviceNotification,
		uintptr(partition),
		uintptr(logicalDeviceID),
		uintptr(unsafe.Pointer(notification)),
		uintptr(notificationSize),
	)
	return err
}

// MapVpciDeviceMmioRanges wraps WHvMapVpciDeviceMmioRanges.
// Note: This API returns an array of pointers to WHV_VPCI_MMIO_MAPPING structures allocated by the system.
// Handling this correctly in Go usually requires reading the memory at the returned pointers.
func MapVpciDeviceMmioRanges(partition PartitionHandle, logicalDeviceID uint64) ([]*VpciMmioMapping, error) {
	var mappingCount uint32
	var mappingsPtr uintptr // WHV_VPCI_MMIO_MAPPING**

	_, err := callHRESULT(procWHvMapVpciDeviceMmioRanges,
		uintptr(partition),
		uintptr(logicalDeviceID),
		uintptr(unsafe.Pointer(&mappingCount)),
		uintptr(unsafe.Pointer(&mappingsPtr)),
	)
	if err != nil {
		return nil, err
	}

	if mappingCount == 0 || mappingsPtr == 0 {
		return nil, nil
	}

	// Iterate over the array of pointers returned
	result := make([]*VpciMmioMapping, mappingCount)
	// The mappingsPtr is a pointer to an array of pointers to VpciMmioMapping
	ptrSlice := (*[1 << 30]*VpciMmioMapping)(unsafe.Pointer(mappingsPtr))[:mappingCount:mappingCount]
	copy(result, ptrSlice)

	return result, nil
}

// UnmapVpciDeviceMmioRanges wraps WHvUnmapVpciDeviceMmioRanges.
func UnmapVpciDeviceMmioRanges(partition PartitionHandle, logicalDeviceID uint64) error {
	_, err := callHRESULT(procWHvUnmapVpciDeviceMmioRanges,
		uintptr(partition),
		uintptr(logicalDeviceID),
	)
	return err
}

// SetVpciDevicePowerState wraps WHvSetVpciDevicePowerState.
func SetVpciDevicePowerState(partition PartitionHandle, logicalDeviceID uint64, powerState DevicePowerState) error {
	_, err := callHRESULT(procWHvSetVpciDevicePowerState,
		uintptr(partition),
		uintptr(logicalDeviceID),
		uintptr(powerState),
	)
	return err
}

// ReadVpciDeviceRegister wraps WHvReadVpciDeviceRegister.
func ReadVpciDeviceRegister(partition PartitionHandle, logicalDeviceID uint64, register *VpciDeviceRegister, data unsafe.Pointer) error {
	_, err := callHRESULT(procWHvReadVpciDeviceRegister,
		uintptr(partition),
		uintptr(logicalDeviceID),
		uintptr(unsafe.Pointer(register)),
		uintptr(data),
	)
	return err
}

// WriteVpciDeviceRegister wraps WHvWriteVpciDeviceRegister.
func WriteVpciDeviceRegister(partition PartitionHandle, logicalDeviceID uint64, register *VpciDeviceRegister, data unsafe.Pointer) error {
	_, err := callHRESULT(procWHvWriteVpciDeviceRegister,
		uintptr(partition),
		uintptr(logicalDeviceID),
		uintptr(unsafe.Pointer(register)),
		uintptr(data),
	)
	return err
}

// MapVpciDeviceInterrupt wraps WHvMapVpciDeviceInterrupt.
func MapVpciDeviceInterrupt(partition PartitionHandle, logicalDeviceID uint64, index uint32, messageCount uint32, target *VpciInterruptTarget) (uint64, uint32, error) {
	var msiAddress uint64
	var msiData uint32
	_, err := callHRESULT(procWHvMapVpciDeviceInterrupt,
		uintptr(partition),
		uintptr(logicalDeviceID),
		uintptr(index),
		uintptr(messageCount),
		uintptr(unsafe.Pointer(target)),
		uintptr(unsafe.Pointer(&msiAddress)),
		uintptr(unsafe.Pointer(&msiData)),
	)
	return msiAddress, msiData, err
}

// UnmapVpciDeviceInterrupt wraps WHvUnmapVpciDeviceInterrupt.
func UnmapVpciDeviceInterrupt(partition PartitionHandle, logicalDeviceID uint64, index uint32) error {
	_, err := callHRESULT(procWHvUnmapVpciDeviceInterrupt,
		uintptr(partition),
		uintptr(logicalDeviceID),
		uintptr(index),
	)
	return err
}

// RetargetVpciDeviceInterrupt wraps WHvRetargetVpciDeviceInterrupt.
func RetargetVpciDeviceInterrupt(partition PartitionHandle, logicalDeviceID uint64, msiAddress uint64, msiData uint32, target *VpciInterruptTarget) error {
	_, err := callHRESULT(procWHvRetargetVpciDeviceInterrupt,
		uintptr(partition),
		uintptr(logicalDeviceID),
		uintptr(msiAddress),
		uintptr(msiData),
		uintptr(unsafe.Pointer(target)),
	)
	return err
}

// RequestVpciDeviceInterrupt wraps WHvRequestVpciDeviceInterrupt.
func RequestVpciDeviceInterrupt(partition PartitionHandle, logicalDeviceID uint64, msiAddress uint64, msiData uint32) error {
	_, err := callHRESULT(procWHvRequestVpciDeviceInterrupt,
		uintptr(partition),
		uintptr(logicalDeviceID),
		uintptr(msiAddress),
		uintptr(msiData),
	)
	return err
}

// GetVpciDeviceInterruptTarget wraps WHvGetVpciDeviceInterruptTarget.
func GetVpciDeviceInterruptTarget(partition PartitionHandle, logicalDeviceID uint64, index uint32, multiMessageNumber uint32, target unsafe.Pointer, targetSize uint32) (uint32, error) {
	var written uint32
	_, err := callHRESULT(procWHvGetVpciDeviceInterruptTarget,
		uintptr(partition),
		uintptr(logicalDeviceID),
		uintptr(index),
		uintptr(multiMessageNumber),
		uintptr(target),
		uintptr(targetSize),
		uintptr(unsafe.Pointer(&written)),
	)
	return written, err
}

// --- Triggers ---

// CreateTrigger wraps WHvCreateTrigger.
func CreateTrigger(partition PartitionHandle, parameters *TriggerParameters) (TriggerHandle, syscall.Handle, error) {
	var triggerHandle TriggerHandle
	var eventHandle syscall.Handle
	_, err := callHRESULT(procWHvCreateTrigger,
		uintptr(partition),
		uintptr(unsafe.Pointer(parameters)),
		uintptr(unsafe.Pointer(&triggerHandle)),
		uintptr(unsafe.Pointer(&eventHandle)),
	)
	return triggerHandle, eventHandle, err
}

// UpdateTriggerParameters wraps WHvUpdateTriggerParameters.
func UpdateTriggerParameters(partition PartitionHandle, parameters *TriggerParameters, triggerHandle TriggerHandle) error {
	_, err := callHRESULT(procWHvUpdateTriggerParameters,
		uintptr(partition),
		uintptr(unsafe.Pointer(parameters)),
		uintptr(triggerHandle),
	)
	return err
}

// DeleteTrigger wraps WHvDeleteTrigger.
func DeleteTrigger(partition PartitionHandle, triggerHandle TriggerHandle) error {
	_, err := callHRESULT(procWHvDeleteTrigger,
		uintptr(partition),
		uintptr(triggerHandle),
	)
	return err
}

// --- Notification Ports ---

// CreateNotificationPort wraps WHvCreateNotificationPort.
func CreateNotificationPort(partition PartitionHandle, parameters *NotificationPortParameters, event syscall.Handle) (NotificationPortHandle, error) {
	var portHandle NotificationPortHandle
	_, err := callHRESULT(procWHvCreateNotificationPort,
		uintptr(partition),
		uintptr(unsafe.Pointer(parameters)),
		uintptr(event),
		uintptr(unsafe.Pointer(&portHandle)),
	)
	return portHandle, err
}

// SetNotificationPortProperty wraps WHvSetNotificationPortProperty.
func SetNotificationPortProperty(partition PartitionHandle, portHandle NotificationPortHandle, propertyCode NotificationPortPropertyCode, propertyValue NotificationPortProperty) error {
	_, err := callHRESULT(procWHvSetNotificationPortProperty,
		uintptr(partition),
		uintptr(portHandle),
		uintptr(propertyCode),
		uintptr(propertyValue),
	)
	return err
}

// DeleteNotificationPort wraps WHvDeleteNotificationPort.
func DeleteNotificationPort(partition PartitionHandle, portHandle NotificationPortHandle) error {
	_, err := callHRESULT(procWHvDeleteNotificationPort,
		uintptr(partition),
		uintptr(portHandle),
	)
	return err
}

// --- Migration ---

// StartPartitionMigration wraps WHvStartPartitionMigration.
func StartPartitionMigration(partition PartitionHandle) (syscall.Handle, error) {
	var migrationHandle syscall.Handle
	_, err := callHRESULT(procWHvStartPartitionMigration,
		uintptr(partition),
		uintptr(unsafe.Pointer(&migrationHandle)),
	)
	return migrationHandle, err
}

// CancelPartitionMigration wraps WHvCancelPartitionMigration.
func CancelPartitionMigration(partition PartitionHandle) error {
	_, err := callHRESULT(procWHvCancelPartitionMigration, uintptr(partition))
	return err
}

// CompletePartitionMigration wraps WHvCompletePartitionMigration.
func CompletePartitionMigration(partition PartitionHandle) error {
	_, err := callHRESULT(procWHvCompletePartitionMigration, uintptr(partition))
	return err
}

// AcceptPartitionMigration wraps WHvAcceptPartitionMigration.
func AcceptPartitionMigration(migrationHandle syscall.Handle) (PartitionHandle, error) {
	var partition PartitionHandle
	_, err := callHRESULT(procWHvAcceptPartitionMigration,
		uintptr(migrationHandle),
		uintptr(unsafe.Pointer(&partition)),
	)
	return partition, err
}

// --- AMD64 Specific ---

// GetVirtualProcessorCpuidOutput wraps WHvGetVirtualProcessorCpuidOutput.
// Note: Only available on AMD64.
func GetVirtualProcessorCpuidOutput(partition PartitionHandle, vpIndex uint32, eax uint32, ecx uint32) (CpuidOutput, error) {
	var output CpuidOutput
	if procWHvGetVirtualProcessorCpuidOutput.Find() != nil {
		return output, syscall.Errno(ERROR_NOT_SUPPORTED)
	}
	_, err := callHRESULT(procWHvGetVirtualProcessorCpuidOutput,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(eax),
		uintptr(ecx),
		uintptr(unsafe.Pointer(&output)),
	)
	return output, err
}

// GetInterruptTargetVpSet wraps WHvGetInterruptTargetVpSet.
// Note: Only available on AMD64.
func GetInterruptTargetVpSet(partition PartitionHandle, destination uint64, destinationMode InterruptDestinationMode, targetVps []uint32) (uint32, error) {
	if procWHvGetInterruptTargetVpSet.Find() != nil {
		return 0, syscall.Errno(ERROR_NOT_SUPPORTED)
	}
	var targetVpCount uint32
	var vpsPtr uintptr
	if len(targetVps) > 0 {
		vpsPtr = uintptr(unsafe.Pointer(&targetVps[0]))
	}

	_, err := callHRESULT(procWHvGetInterruptTargetVpSet,
		uintptr(partition),
		uintptr(destination),
		uintptr(destinationMode),
		vpsPtr,
		uintptr(len(targetVps)),
		uintptr(unsafe.Pointer(&targetVpCount)),
	)
	return targetVpCount, err
}

// --- Legacy / Deprecated Functions ---

// GetVirtualProcessorInterruptControllerState wraps WHvGetVirtualProcessorInterruptControllerState (Deprecated).
func GetVirtualProcessorInterruptControllerState(partition PartitionHandle, vpIndex uint32, buffer unsafe.Pointer, bufferSize uint32) (uint32, error) {
	var written uint32
	_, err := callHRESULT(procWHvGetVirtualProcessorInterruptControllerState,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(buffer),
		uintptr(bufferSize),
		uintptr(unsafe.Pointer(&written)),
	)
	return written, err
}

// SetVirtualProcessorInterruptControllerState wraps WHvSetVirtualProcessorInterruptControllerState (Deprecated).
func SetVirtualProcessorInterruptControllerState(partition PartitionHandle, vpIndex uint32, buffer unsafe.Pointer, bufferSize uint32) error {
	_, err := callHRESULT(procWHvSetVirtualProcessorInterruptControllerState,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(buffer),
		uintptr(bufferSize),
	)
	return err
}

// GetVirtualProcessorXsaveState wraps WHvGetVirtualProcessorXsaveState (Deprecated).
func GetVirtualProcessorXsaveState(partition PartitionHandle, vpIndex uint32, buffer unsafe.Pointer, bufferSize uint32) (uint32, error) {
	var written uint32
	_, err := callHRESULT(procWHvGetVirtualProcessorXsaveState,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(buffer),
		uintptr(bufferSize),
		uintptr(unsafe.Pointer(&written)),
	)
	return written, err
}

// SetVirtualProcessorXsaveState wraps WHvSetVirtualProcessorXsaveState (Deprecated).
func SetVirtualProcessorXsaveState(partition PartitionHandle, vpIndex uint32, buffer unsafe.Pointer, bufferSize uint32) error {
	_, err := callHRESULT(procWHvSetVirtualProcessorXsaveState,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(buffer),
		uintptr(bufferSize),
	)
	return err
}

// GetVirtualProcessorInterruptControllerState2 wraps WHvGetVirtualProcessorInterruptControllerState2 (Deprecated).
func GetVirtualProcessorInterruptControllerState2(partition PartitionHandle, vpIndex uint32, buffer unsafe.Pointer, bufferSize uint32) (uint32, error) {
	var written uint32
	_, err := callHRESULT(procWHvGetVirtualProcessorInterruptControllerState2,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(buffer),
		uintptr(bufferSize),
		uintptr(unsafe.Pointer(&written)),
	)
	return written, err
}

// SetVirtualProcessorInterruptControllerState2 wraps WHvSetVirtualProcessorInterruptControllerState2 (Deprecated).
func SetVirtualProcessorInterruptControllerState2(partition PartitionHandle, vpIndex uint32, buffer unsafe.Pointer, bufferSize uint32) error {
	_, err := callHRESULT(procWHvSetVirtualProcessorInterruptControllerState2,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(buffer),
		uintptr(bufferSize),
	)
	return err
}

// RegisterPartitionDoorbellEvent wraps WHvRegisterPartitionDoorbellEvent (Deprecated).
func RegisterPartitionDoorbellEvent(partition PartitionHandle, matchData *DoorbellMatchData, event syscall.Handle) error {
	_, err := callHRESULT(procWHvRegisterPartitionDoorbellEvent,
		uintptr(partition),
		uintptr(unsafe.Pointer(matchData)),
		uintptr(event),
	)
	return err
}

// UnregisterPartitionDoorbellEvent wraps WHvUnregisterPartitionDoorbellEvent (Deprecated).
func UnregisterPartitionDoorbellEvent(partition PartitionHandle, matchData *DoorbellMatchData) error {
	_, err := callHRESULT(procWHvUnregisterPartitionDoorbellEvent,
		uintptr(partition),
		uintptr(unsafe.Pointer(matchData)),
	)
	return err
}
