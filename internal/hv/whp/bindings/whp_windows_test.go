//go:build windows && amd64

package bindings

import (
	"errors"
	"syscall"
	"testing"
	"unsafe"
)

var skipHRESULTCodes = map[uint32]struct{}{
	0x8007007E: {}, // ERROR_MOD_NOT_FOUND
	0x80070002: {}, // ERROR_FILE_NOT_FOUND
	0x80070032: {}, // ERROR_NOT_SUPPORTED
	0x80370102: {}, // WHV_E_VIRTUALIZATION_DISABLED
}

func shouldSkipError(err error) bool {
	if err == nil {
		return false
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		switch errno {
		case 2, 50, 126:
			return true
		}
	}
	if hr, ok := AsHRESULT(err); ok {
		if _, found := skipHRESULTCodes[uint32(hr)]; found {
			return true
		}
	}
	return false
}

func TestGetCapabilityHypervisorPresent(t *testing.T) {
	if err := modWinHvPlatform.Load(); err != nil {
		t.Skipf("winhvplatform.dll unavailable: %v", err)
	}
	var present uint32
	written, err := GetCapability(CapabilityCodeHypervisorPresent, unsafe.Pointer(&present), uint32(unsafe.Sizeof(present)))
	if shouldSkipError(err) {
		t.Skipf("hypervisor capability not available: %v", err)
	}
	if err != nil {
		t.Fatalf("WHvGetCapability failed: %v", err)
	}
	if written < uint32(unsafe.Sizeof(present)) {
		t.Fatalf("expected at least %d bytes, got %d", unsafe.Sizeof(present), written)
	}
}

func TestEmulatorLifecycle(t *testing.T) {
	if err := modWinHvEmulation.Load(); err != nil {
		t.Skipf("winhvemulation.dll unavailable: %v", err)
	}

	ioFn := func(ctx unsafe.Pointer, access *EmulatorIOAccessInfo) HRESULT {
		return 0
	}
	memFn := func(ctx unsafe.Pointer, access *EmulatorMemoryAccessInfo) HRESULT {
		return 0
	}
	getFn := func(ctx unsafe.Pointer, names []RegisterName, values []RegisterValue) HRESULT {
		return 0
	}
	setFn := func(ctx unsafe.Pointer, names []RegisterName, values []RegisterValue) HRESULT {
		return 0
	}
	translateFn := func(ctx unsafe.Pointer, gva GuestVirtualAddress, flags TranslateGVAFlags, result *TranslateGVAResultCode, gpa *GuestPhysicalAddress) HRESULT {
		if result != nil {
			*result = TranslateGVAResultSuccess
		}
		if gpa != nil {
			*gpa = 0
		}
		return 0
	}

	callbacks := EmulatorCallbacks{
		IoPortCallback:               NewEmulatorIoPortCallback(ioFn),
		MemoryCallback:               NewEmulatorMemoryCallback(memFn),
		GetVirtualProcessorRegisters: NewEmulatorGetRegistersCallback(getFn),
		SetVirtualProcessorRegisters: NewEmulatorSetRegistersCallback(setFn),
		TranslateGvaPage:             NewEmulatorTranslateGvaCallback(translateFn),
	}

	handle, err := EmulatorCreate(&callbacks)
	if shouldSkipError(err) {
		t.Skipf("emulator not available: %v", err)
	}
	if err != nil {
		t.Fatalf("WHvEmulatorCreateEmulator failed: %v", err)
	}
	t.Cleanup(func() {
		if destroyErr := EmulatorDestroy(handle); destroyErr != nil {
			t.Errorf("WHvEmulatorDestroyEmulator failed: %v", destroyErr)
		}
	})
}

func TestRegisterValueViews(t *testing.T) {
	var value RegisterValue
	*value.AsUint64() = 0xAABBCCDD33445566
	if got := value.AsUint128().Low64; got != 0xAABBCCDD33445566 {
		t.Fatalf("expected Low64 to match written value, got 0x%X", got)
	}
	seg := value.AsSegment()
	seg.Base = 0x1122334455667788
	if value.AsUint128().Low64 != 0x1122334455667788 {
		t.Fatalf("segment write not reflected in raw storage")
	}
}
