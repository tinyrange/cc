//go:build windows

package bindings

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	modWinHvPlatform = syscall.NewLazyDLL("winhvplatform.dll")

	procWHvGetCapability                                = modWinHvPlatform.NewProc("WHvGetCapability")
	procWHvCreatePartition                              = modWinHvPlatform.NewProc("WHvCreatePartition")
	procWHvSetupPartition                               = modWinHvPlatform.NewProc("WHvSetupPartition")
	procWHvDeletePartition                              = modWinHvPlatform.NewProc("WHvDeletePartition")
	procWHvGetPartitionProperty                         = modWinHvPlatform.NewProc("WHvGetPartitionProperty")
	procWHvSetPartitionProperty                         = modWinHvPlatform.NewProc("WHvSetPartitionProperty")
	procWHvSuspendPartitionTime                         = modWinHvPlatform.NewProc("WHvSuspendPartitionTime")
	procWHvResumePartitionTime                          = modWinHvPlatform.NewProc("WHvResumePartitionTime")
	procWHvMapGpaRange                                  = modWinHvPlatform.NewProc("WHvMapGpaRange")
	procWHvUnmapGpaRange                                = modWinHvPlatform.NewProc("WHvUnmapGpaRange")
	procWHvTranslateGva                                 = modWinHvPlatform.NewProc("WHvTranslateGva")
	procWHvCreateVirtualProcessor                       = modWinHvPlatform.NewProc("WHvCreateVirtualProcessor")
	procWHvDeleteVirtualProcessor                       = modWinHvPlatform.NewProc("WHvDeleteVirtualProcessor")
	procWHvRunVirtualProcessor                          = modWinHvPlatform.NewProc("WHvRunVirtualProcessor")
	procWHvCancelRunVirtualProcessor                    = modWinHvPlatform.NewProc("WHvCancelRunVirtualProcessor")
	procWHvGetVirtualProcessorRegisters                 = modWinHvPlatform.NewProc("WHvGetVirtualProcessorRegisters")
	procWHvSetVirtualProcessorRegisters                 = modWinHvPlatform.NewProc("WHvSetVirtualProcessorRegisters")
	procWHvGetVirtualProcessorInterruptControllerState  = modWinHvPlatform.NewProc("WHvGetVirtualProcessorInterruptControllerState")
	procWHvSetVirtualProcessorInterruptControllerState  = modWinHvPlatform.NewProc("WHvSetVirtualProcessorInterruptControllerState")
	procWHvRequestInterrupt                             = modWinHvPlatform.NewProc("WHvRequestInterrupt")
	procWHvGetVirtualProcessorXsaveState                = modWinHvPlatform.NewProc("WHvGetVirtualProcessorXsaveState")
	procWHvSetVirtualProcessorXsaveState                = modWinHvPlatform.NewProc("WHvSetVirtualProcessorXsaveState")
	procWHvQueryGpaRangeDirtyBitmap                     = modWinHvPlatform.NewProc("WHvQueryGpaRangeDirtyBitmap")
	procWHvGetPartitionCounters                         = modWinHvPlatform.NewProc("WHvGetPartitionCounters")
	procWHvGetVirtualProcessorCounters                  = modWinHvPlatform.NewProc("WHvGetVirtualProcessorCounters")
	procWHvGetVirtualProcessorInterruptControllerState2 = modWinHvPlatform.NewProc("WHvGetVirtualProcessorInterruptControllerState2")
	procWHvSetVirtualProcessorInterruptControllerState2 = modWinHvPlatform.NewProc("WHvSetVirtualProcessorInterruptControllerState2")
	procWHvRegisterPartitionDoorbellEvent               = modWinHvPlatform.NewProc("WHvRegisterPartitionDoorbellEvent")
	procWHvUnregisterPartitionDoorbellEvent             = modWinHvPlatform.NewProc("WHvUnregisterPartitionDoorbellEvent")
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

// CreateVirtualProcessor wraps WHvCreateVirtualProcessor.
func CreateVirtualProcessor(partition PartitionHandle, vpIndex uint32, flags uint32) error {
	_, err := callHRESULT(procWHvCreateVirtualProcessor,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(flags),
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

// GetVirtualProcessorInterruptControllerState wraps WHvGetVirtualProcessorInterruptControllerState.
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

// SetVirtualProcessorInterruptControllerState wraps WHvSetVirtualProcessorInterruptControllerState.
func SetVirtualProcessorInterruptControllerState(partition PartitionHandle, vpIndex uint32, buffer unsafe.Pointer, bufferSize uint32) error {
	_, err := callHRESULT(procWHvSetVirtualProcessorInterruptControllerState,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(buffer),
		uintptr(bufferSize),
	)
	return err
}

// RequestInterrupt wraps WHvRequestInterrupt.
func RequestInterrupt(partition PartitionHandle, control *InterruptControl, size uint32) error {
	_, err := callHRESULT(procWHvRequestInterrupt,
		uintptr(partition),
		uintptr(unsafe.Pointer(control)),
		uintptr(size),
	)
	return err
}

// GetVirtualProcessorXsaveState wraps WHvGetVirtualProcessorXsaveState.
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

// SetVirtualProcessorXsaveState wraps WHvSetVirtualProcessorXsaveState.
func SetVirtualProcessorXsaveState(partition PartitionHandle, vpIndex uint32, buffer unsafe.Pointer, bufferSize uint32) error {
	_, err := callHRESULT(procWHvSetVirtualProcessorXsaveState,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(buffer),
		uintptr(bufferSize),
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

// GetVirtualProcessorInterruptControllerState2 wraps WHvGetVirtualProcessorInterruptControllerState2.
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

// SetVirtualProcessorInterruptControllerState2 wraps WHvSetVirtualProcessorInterruptControllerState2.
func SetVirtualProcessorInterruptControllerState2(partition PartitionHandle, vpIndex uint32, buffer unsafe.Pointer, bufferSize uint32) error {
	_, err := callHRESULT(procWHvSetVirtualProcessorInterruptControllerState2,
		uintptr(partition),
		uintptr(vpIndex),
		uintptr(buffer),
		uintptr(bufferSize),
	)
	return err
}

// RegisterPartitionDoorbellEvent wraps WHvRegisterPartitionDoorbellEvent.
func RegisterPartitionDoorbellEvent(partition PartitionHandle, matchData *DoorbellMatchData, event syscall.Handle) error {
	_, err := callHRESULT(procWHvRegisterPartitionDoorbellEvent,
		uintptr(partition),
		uintptr(unsafe.Pointer(matchData)),
		uintptr(event),
	)
	return err
}

// UnregisterPartitionDoorbellEvent wraps WHvUnregisterPartitionDoorbellEvent.
func UnregisterPartitionDoorbellEvent(partition PartitionHandle, matchData *DoorbellMatchData) error {
	_, err := callHRESULT(procWHvUnregisterPartitionDoorbellEvent,
		uintptr(partition),
		uintptr(unsafe.Pointer(matchData)),
	)
	return err
}
