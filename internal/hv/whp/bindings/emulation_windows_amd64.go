//go:build windows && (amd64 || arm64)

package bindings

import (
	"fmt"
	"unsafe"
)

const S_OK = HRESULT(0)

// EmulatorCreate creates a new emulator using WHvEmulatorCreateEmulator.
func EmulatorCreate(callbacks *EmulatorCallbacks) (EmulatorHandle, error) {
	if callbacks == nil {
		return 0, fmt.Errorf("whp: callbacks must not be nil")
	}
	callbacks.ensureSize()
	var handle EmulatorHandle

	// C Signature:
	// HRESULT WHvEmulatorCreateEmulator(
	//   _In_ const WHV_EMULATOR_CALLBACKS* Callbacks,
	//   _Out_ WHV_EMULATOR_HANDLE* Emulator
	// );
	hr, err := callHRESULT(procWHvEmulatorCreateEmulator,
		uintptr(unsafe.Pointer(callbacks)),
		uintptr(unsafe.Pointer(&handle)),
	)
	if hr != S_OK {
		return 0, err
	}
	return handle, nil
}

// EmulatorDestroy destroys a previously created emulator using WHvEmulatorDestroyEmulator.
func EmulatorDestroy(handle EmulatorHandle) error {
	if handle == 0 {
		return nil
	}

	// C Signature:
	// HRESULT WHvEmulatorDestroyEmulator(_In_ WHV_EMULATOR_HANDLE Emulator);
	hr, err := callHRESULT(procWHvEmulatorDestroyEmulator, uintptr(handle))
	if hr != S_OK {
		return err
	}
	return nil
}

// EmulatorTryIoEmulation wraps WHvEmulatorTryIoEmulation.
func EmulatorTryIoEmulation(
	handle EmulatorHandle,
	context unsafe.Pointer,
	vpContext *VPExitContext,
	ioContext *X64IOPortAccessContext,
	status *EmulatorStatus,
) error {
	// C Signature:
	// HRESULT WHvEmulatorTryIoEmulation(
	//   _In_ WHV_EMULATOR_HANDLE Emulator,
	//   _In_ VOID* Context,
	//   _In_ const WHV_VP_EXIT_CONTEXT* VpContext,
	//   _In_ const WHV_X64_IO_PORT_ACCESS_CONTEXT* IoInstructionContext,
	//   _Out_ WHV_EMULATOR_STATUS* EmulatorReturnStatus
	// );
	hr, err := callHRESULT(procWHvEmulatorTryIoEmulation,
		uintptr(handle),
		uintptr(context),
		uintptr(unsafe.Pointer(vpContext)),
		uintptr(unsafe.Pointer(ioContext)),
		uintptr(unsafe.Pointer(status)),
	)
	if hr != S_OK {
		return err
	}
	return nil
}

// EmulatorTryMmioEmulation wraps WHvEmulatorTryMmioEmulation.
func EmulatorTryMmioEmulation(
	handle EmulatorHandle,
	context unsafe.Pointer,
	vpContext *VPExitContext,
	mmioContext *MemoryAccessContext,
	status *EmulatorStatus,
) error {
	// C Signature:
	// HRESULT WHvEmulatorTryMmioEmulation(
	//   _In_ WHV_EMULATOR_HANDLE Emulator,
	//   _In_ VOID* Context,
	//   _In_ const WHV_VP_EXIT_CONTEXT* VpContext,
	//   _In_ const WHV_MEMORY_ACCESS_CONTEXT* MmioInstructionContext,
	//   _Out_ WHV_EMULATOR_STATUS* EmulatorReturnStatus
	// );
	hr, err := callHRESULT(procWHvEmulatorTryMmioEmulation,
		uintptr(handle),
		uintptr(context),
		uintptr(unsafe.Pointer(vpContext)),
		uintptr(unsafe.Pointer(mmioContext)),
		uintptr(unsafe.Pointer(status)),
	)
	if hr != S_OK {
		return err
	}
	return nil
}
