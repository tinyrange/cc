//go:build windows && amd64

package bindings

import (
	"fmt"
	"unsafe"
)

// EmulatorCreate creates a new emulator using WHvEmulatorCreateEmulator.
func EmulatorCreate(callbacks *EmulatorCallbacks) (EmulatorHandle, error) {
	if callbacks == nil {
		return 0, fmt.Errorf("whp: callbacks must not be nil")
	}
	callbacks.ensureSize()
	var handle EmulatorHandle
	_, err := callHRESULT(procWHvEmulatorCreateEmulator,
		uintptr(unsafe.Pointer(callbacks)),
		uintptr(unsafe.Pointer(&handle)),
	)
	return handle, err
}

// EmulatorDestroy destroys a previously created emulator.
func EmulatorDestroy(handle EmulatorHandle) error {
	if handle == 0 {
		return nil
	}
	_, err := callHRESULT(procWHvEmulatorDestroyEmulator, uintptr(handle))
	return err
}

// EmulatorTryIoEmulation wraps WHvEmulatorTryIoEmulation.
func EmulatorTryIoEmulation(handle EmulatorHandle, context unsafe.Pointer, vpContext *VPExitContext, ioContext *X64IOPortAccessContext, status *EmulatorStatus) error {
	_, err := callHRESULT(procWHvEmulatorTryIoEmulation,
		uintptr(handle),
		uintptr(context),
		uintptr(unsafe.Pointer(vpContext)),
		uintptr(unsafe.Pointer(ioContext)),
		uintptr(unsafe.Pointer(status)),
	)
	return err
}

// EmulatorTryMmioEmulation wraps WHvEmulatorTryMmioEmulation.
func EmulatorTryMmioEmulation(handle EmulatorHandle, context unsafe.Pointer, vpContext *VPExitContext, mmioContext *MemoryAccessContext, status *EmulatorStatus) error {
	_, err := callHRESULT(procWHvEmulatorTryMmioEmulation,
		uintptr(handle),
		uintptr(context),
		uintptr(unsafe.Pointer(vpContext)),
		uintptr(unsafe.Pointer(mmioContext)),
		uintptr(unsafe.Pointer(status)),
	)
	return err
}
