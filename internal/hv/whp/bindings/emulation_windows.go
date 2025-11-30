//go:build windows && (amd64 || arm64)

package bindings

import (
	"fmt"
	"syscall"
	"unsafe"
)

var (
	modWinHvEmulation               = syscall.NewLazyDLL("winhvemulation.dll")
	procWHvEmulatorCreateEmulator   = modWinHvEmulation.NewProc("WHvEmulatorCreateEmulator")
	procWHvEmulatorDestroyEmulator  = modWinHvEmulation.NewProc("WHvEmulatorDestroyEmulator")
	procWHvEmulatorTryIoEmulation   = modWinHvEmulation.NewProc("WHvEmulatorTryIoEmulation")
	procWHvEmulatorTryMmioEmulation = modWinHvEmulation.NewProc("WHvEmulatorTryMmioEmulation")
)

// EmulatorHandle mirrors WHV_EMULATOR_HANDLE.
type EmulatorHandle uintptr

// EmulatorStatus mirrors WHV_EMULATOR_STATUS.
// This is a C Union containing a bitfield.
type EmulatorStatus uint32

const (
	EmulatorStatusSuccess                    EmulatorStatus = 1 << 0
	EmulatorStatusInternalFailure            EmulatorStatus = 1 << 1
	EmulatorStatusIoPortCallbackFailed       EmulatorStatus = 1 << 2
	EmulatorStatusMemoryCallbackFailed       EmulatorStatus = 1 << 3
	EmulatorStatusTranslateGvaCallbackFailed EmulatorStatus = 1 << 4
	EmulatorStatusTranslateGvaGpaNotAligned  EmulatorStatus = 1 << 5
	EmulatorStatusGetRegistersCallbackFailed EmulatorStatus = 1 << 6
	EmulatorStatusSetRegistersCallbackFailed EmulatorStatus = 1 << 7
	EmulatorStatusInterruptCausedIntercept   EmulatorStatus = 1 << 8
	EmulatorStatusGuestCannotBeFaulted       EmulatorStatus = 1 << 9
)

func (s EmulatorStatus) EmulationSuccessful() bool {
	// Check if Success bit is set AND no failure bits are set.
	failureMask := EmulatorStatusInternalFailure |
		EmulatorStatusIoPortCallbackFailed |
		EmulatorStatusMemoryCallbackFailed |
		EmulatorStatusTranslateGvaCallbackFailed |
		EmulatorStatusGetRegistersCallbackFailed |
		EmulatorStatusSetRegistersCallbackFailed

	return (s&EmulatorStatusSuccess != 0) && (s&failureMask == 0)
}

func (s EmulatorStatus) String() string {
	if s == 0 {
		return "None"
	}
	// Note: Simple output format, can be expanded if needed.
	return fmt.Sprintf("EmulatorStatus(0x%x)", uint32(s))
}

// EmulatorMemoryAccessDirection mirrors internal UINT8 usage in access info structs.
// Although 32-bit in standard enums, the struct explicitly uses UINT8.
type EmulatorMemoryAccessDirection uint8

const (
	EmulatorMemoryAccessDirectionRead  EmulatorMemoryAccessDirection = 0
	EmulatorMemoryAccessDirectionWrite EmulatorMemoryAccessDirection = 1
)

func (d EmulatorMemoryAccessDirection) String() string {
	switch d {
	case EmulatorMemoryAccessDirectionRead:
		return "Read"
	case EmulatorMemoryAccessDirectionWrite:
		return "Write"
	default:
		return fmt.Sprintf("Unknown(%d)", d)
	}
}

// EmulatorMemoryAccessInfo mirrors WHV_EMULATOR_MEMORY_ACCESS_INFO.
// Layout:
//
//	GpaAddress (8 bytes)
//	Direction  (1 byte)
//	AccessSize (1 byte)
//	Data       (8 bytes)
//
// Total aligned size: 24 bytes (Go pads end to align to 8 bytes).
type EmulatorMemoryAccessInfo struct {
	GpaAddress uint64
	Direction  EmulatorMemoryAccessDirection // UINT8
	AccessSize uint8                         // UINT8
	Data       [8]byte                       // UINT8 Data[8]
}

// EmulatorIOAccessDirection mirrors internal UINT8 usage.
type EmulatorIOAccessDirection uint8

const (
	EmulatorIOAccessDirectionIn  EmulatorIOAccessDirection = 0
	EmulatorIOAccessDirectionOut EmulatorIOAccessDirection = 1
)

func (d EmulatorIOAccessDirection) String() string {
	switch d {
	case EmulatorIOAccessDirectionIn:
		return "In"
	case EmulatorIOAccessDirectionOut:
		return "Out"
	default:
		return fmt.Sprintf("Unknown(%d)", d)
	}
}

// EmulatorIOAccessInfo mirrors WHV_EMULATOR_IO_ACCESS_INFO.
// Layout:
//
//	Direction  (1 byte)
//	(Pad 1 byte)
//	Port       (2 bytes)
//	AccessSize (2 bytes)
//	(Pad 2 bytes)
//	Data       (4 bytes)
type EmulatorIOAccessInfo struct {
	Direction  EmulatorIOAccessDirection // UINT8
	Port       uint16                    // UINT16
	AccessSize uint16                    // UINT16
	// FIXED: C header defines this as "UINT32 Data;" not an array.
	Data uint32 // UINT32
}

// --------------------------------------------------------------------------
// Callbacks
// --------------------------------------------------------------------------

// EmulatorIoPortFunc is the Go representation of WHV_EMULATOR_IO_PORT_CALLBACK.
// Returns HRESULT.
type EmulatorIoPortFunc func(ctx unsafe.Pointer, access *EmulatorIOAccessInfo) HRESULT

// EmulatorMemoryFunc is the Go representation of WHV_EMULATOR_MEMORY_CALLBACK.
// Returns HRESULT.
type EmulatorMemoryFunc func(ctx unsafe.Pointer, access *EmulatorMemoryAccessInfo) HRESULT

// EmulatorGetRegistersFunc mirrors WHV_EMULATOR_GET_VIRTUAL_PROCESSOR_REGISTERS_CALLBACK.
type EmulatorGetRegistersFunc func(ctx unsafe.Pointer, names []RegisterName, values []RegisterValue) HRESULT

// EmulatorSetRegistersFunc mirrors WHV_EMULATOR_SET_VIRTUAL_PROCESSOR_REGISTERS_CALLBACK.
type EmulatorSetRegistersFunc func(ctx unsafe.Pointer, names []RegisterName, values []RegisterValue) HRESULT

// EmulatorTranslateGvaPageFunc mirrors WHV_EMULATOR_TRANSLATE_GVA_PAGE_CALLBACK.
type EmulatorTranslateGvaPageFunc func(ctx unsafe.Pointer, gva GuestVirtualAddress, flags TranslateGVAFlags, result *TranslateGVAResultCode, gpa *GuestPhysicalAddress) HRESULT

// Helper to slice pointer arithmetic
func makeRegisterNameSlice(ptr uintptr, count uint32) []RegisterName {
	if ptr == 0 || count == 0 {
		return nil
	}
	return unsafe.Slice((*RegisterName)(unsafe.Pointer(ptr)), int(count))
}

func makeRegisterValueSlice(ptr uintptr, count uint32) []RegisterValue {
	if ptr == 0 || count == 0 {
		return nil
	}
	return unsafe.Slice((*RegisterValue)(unsafe.Pointer(ptr)), int(count))
}

// NewEmulatorIoPortCallback creates a trampoline for WHV_EMULATOR_IO_PORT_CALLBACK.
func NewEmulatorIoPortCallback(fn EmulatorIoPortFunc) uintptr {
	if fn == nil {
		return 0
	}
	return syscall.NewCallback(func(ctx, access uintptr) uintptr {
		return uintptr(fn(unsafe.Pointer(ctx), (*EmulatorIOAccessInfo)(unsafe.Pointer(access))))
	})
}

// NewEmulatorMemoryCallback creates a trampoline for WHV_EMULATOR_MEMORY_CALLBACK.
func NewEmulatorMemoryCallback(fn EmulatorMemoryFunc) uintptr {
	if fn == nil {
		return 0
	}
	return syscall.NewCallback(func(ctx, access uintptr) uintptr {
		return uintptr(fn(unsafe.Pointer(ctx), (*EmulatorMemoryAccessInfo)(unsafe.Pointer(access))))
	})
}

// NewEmulatorGetRegistersCallback creates a trampoline for WHV_EMULATOR_GET_VIRTUAL_PROCESSOR_REGISTERS_CALLBACK.
func NewEmulatorGetRegistersCallback(fn EmulatorGetRegistersFunc) uintptr {
	if fn == nil {
		return 0
	}
	return syscall.NewCallback(func(ctx, namesPtr, count, valuesPtr uintptr) uintptr {
		names := makeRegisterNameSlice(namesPtr, uint32(count))
		values := makeRegisterValueSlice(valuesPtr, uint32(count))
		return uintptr(fn(unsafe.Pointer(ctx), names, values))
	})
}

// NewEmulatorSetRegistersCallback creates a trampoline for WHV_EMULATOR_SET_VIRTUAL_PROCESSOR_REGISTERS_CALLBACK.
func NewEmulatorSetRegistersCallback(fn EmulatorSetRegistersFunc) uintptr {
	if fn == nil {
		return 0
	}
	return syscall.NewCallback(func(ctx, namesPtr, count, valuesPtr uintptr) uintptr {
		names := makeRegisterNameSlice(namesPtr, uint32(count))
		// C Header: _In_reads_(RegisterCount) const WHV_REGISTER_VALUE* RegisterValues
		values := makeRegisterValueSlice(valuesPtr, uint32(count))
		return uintptr(fn(unsafe.Pointer(ctx), names, values))
	})
}

// NewEmulatorTranslateGvaCallback creates a trampoline for WHV_EMULATOR_TRANSLATE_GVA_PAGE_CALLBACK.
func NewEmulatorTranslateGvaCallback(fn EmulatorTranslateGvaPageFunc) uintptr {
	if fn == nil {
		return 0
	}
	return syscall.NewCallback(func(ctx, gva, flags, result, gpa uintptr) uintptr {
		return uintptr(fn(
			unsafe.Pointer(ctx),
			GuestVirtualAddress(gva),
			TranslateGVAFlags(flags),
			(*TranslateGVAResultCode)(unsafe.Pointer(result)),
			(*GuestPhysicalAddress)(unsafe.Pointer(gpa)),
		))
	})
}

// EmulatorCallbacks mirrors WHV_EMULATOR_CALLBACKS.
type EmulatorCallbacks struct {
	Size                         uint32
	Reserved                     uint32
	IoPortCallback               uintptr
	MemoryCallback               uintptr
	GetVirtualProcessorRegisters uintptr
	SetVirtualProcessorRegisters uintptr
	TranslateGvaPage             uintptr
}

func (c *EmulatorCallbacks) ensureSize() {
	if c.Size == 0 {
		c.Size = uint32(unsafe.Sizeof(*c))
	}
}
