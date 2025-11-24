//go:build windows

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

// EmulatorStatus mirrors WHV_EMULATOR_STATUS.
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
	return s&EmulatorStatusSuccess != 0 && s&(EmulatorStatusInternalFailure|EmulatorStatusIoPortCallbackFailed|EmulatorStatusMemoryCallbackFailed|EmulatorStatusTranslateGvaCallbackFailed|EmulatorStatusGetRegistersCallbackFailed|EmulatorStatusSetRegistersCallbackFailed) == 0
}

func (s EmulatorStatus) String() string {
	var flags []string
	if s&EmulatorStatusSuccess != 0 {
		flags = append(flags, "Success")
	}
	if s&EmulatorStatusInternalFailure != 0 {
		flags = append(flags, "InternalFailure")
	}
	if s&EmulatorStatusIoPortCallbackFailed != 0 {
		flags = append(flags, "IoPortCallbackFailed")
	}
	if s&EmulatorStatusMemoryCallbackFailed != 0 {
		flags = append(flags, "MemoryCallbackFailed")
	}
	if s&EmulatorStatusTranslateGvaCallbackFailed != 0 {
		flags = append(flags, "TranslateGvaCallbackFailed")
	}
	if s&EmulatorStatusTranslateGvaGpaNotAligned != 0 {
		flags = append(flags, "TranslateGvaGpaNotAligned")
	}
	if s&EmulatorStatusGetRegistersCallbackFailed != 0 {
		flags = append(flags, "GetRegistersCallbackFailed")
	}
	if s&EmulatorStatusSetRegistersCallbackFailed != 0 {
		flags = append(flags, "SetRegistersCallbackFailed")
	}
	if s&EmulatorStatusInterruptCausedIntercept != 0 {
		flags = append(flags, "InterruptCausedIntercept")
	}
	if s&EmulatorStatusGuestCannotBeFaulted != 0 {
		flags = append(flags, "GuestCannotBeFaulted")
	}
	if len(flags) == 0 {
		return "None"
	}
	return fmt.Sprintf("%v", flags)
}

// EmulatorMemoryAccessDirection mirrors WHV_EMULATOR_MEMORY_ACCESS_DIRECTION.
// NOTE: Enums in Windows C ABI are 32-bit ints by default.
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
// C Layout:
//
//	GpaAddress (8 bytes)
//	Direction  (1 byte)
//	AccessSize (1 byte)
//	ByteData   (8 bytes)
type EmulatorMemoryAccessInfo struct {
	// typedef struct _WHV_EMULATOR_MEMORY_ACCESS_INFO {
	//     UINT64 GpaAddress;
	//     UINT8 Direction; // 0 for read, 1 for write
	//     UINT8 AccessSize; // 1 thru 8
	//     UINT8 Data[8]; // Raw byte contents to be read from/written to memory
	// } WHV_EMULATOR_MEMORY_ACCESS_INFO;
	GpaAddress uint64                        // Offset 0
	Direction  EmulatorMemoryAccessDirection // Offset 8
	AccessSize uint8                         // Offset 9
	Data       [8]byte                       // Offset 10
}

// EmulatorIOAccessDirection mirrors WHV_EMULATOR_IO_ACCESS_DIRECTION.
// NOTE: Enums in Windows C ABI are 32-bit ints by default.
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
// C Layout:
//
//	Direction  (1 byte)
//	Port       (2 bytes)
//	AccessSize (2 bytes)
//	Data       (4 bytes)
type EmulatorIOAccessInfo struct {
	// typedef struct _WHV_EMULATOR_IO_ACCESS_INFO {
	//     UINT8 Direction; // 0 for in, 1 for out
	//     UINT16 Port;
	//     UINT16 AccessSize; // only 1, 2, 4
	//     UINT32 Data[4];
	// } WHV_EMULATOR_IO_ACCESS_INFO;\
	Direction  EmulatorIOAccessDirection // Offset 0
	Port       uint16                    // Offset 1
	AccessSize uint16                    // Offset 3
	Data       [4]uint32                 // Offset 5
}

// EmulatorIoPortFunc is the Go representation of WHV_EMULATOR_IO_PORT_CALLBACK.
type EmulatorIoPortFunc func(ctx unsafe.Pointer, access *EmulatorIOAccessInfo) HRESULT

// EmulatorMemoryFunc is the Go representation of WHV_EMULATOR_MEMORY_CALLBACK.
type EmulatorMemoryFunc func(ctx unsafe.Pointer, access *EmulatorMemoryAccessInfo) HRESULT

// EmulatorGetRegistersFunc mirrors WHV_EMULATOR_GET_VIRTUAL_PROCESSOR_REGISTERS_CALLBACK.
type EmulatorGetRegistersFunc func(ctx unsafe.Pointer, names []RegisterName, values []RegisterValue) HRESULT

// EmulatorSetRegistersFunc mirrors WHV_EMULATOR_SET_VIRTUAL_PROCESSOR_REGISTERS_CALLBACK.
type EmulatorSetRegistersFunc func(ctx unsafe.Pointer, names []RegisterName, values []RegisterValue) HRESULT

// EmulatorTranslateGvaPageFunc mirrors WHV_EMULATOR_TRANSLATE_GVA_PAGE_CALLBACK.
type EmulatorTranslateGvaPageFunc func(ctx unsafe.Pointer, gva GuestVirtualAddress, flags TranslateGVAFlags, result *TranslateGVAResultCode, gpa *GuestPhysicalAddress) HRESULT

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

// NewEmulatorIoPortCallback converts a Go function into a CALLBACK-compatible pointer.
func NewEmulatorIoPortCallback(fn EmulatorIoPortFunc) uintptr {
	if fn == nil {
		return 0
	}
	return syscall.NewCallback(func(ctx, access uintptr) uintptr {
		return uintptr(fn(unsafe.Pointer(ctx), (*EmulatorIOAccessInfo)(unsafe.Pointer(access))))
	})
}

// NewEmulatorMemoryCallback converts a Go function into a CALLBACK-compatible pointer.
func NewEmulatorMemoryCallback(fn EmulatorMemoryFunc) uintptr {
	if fn == nil {
		return 0
	}
	return syscall.NewCallback(func(ctx, access uintptr) uintptr {
		return uintptr(fn(unsafe.Pointer(ctx), (*EmulatorMemoryAccessInfo)(unsafe.Pointer(access))))
	})
}

// NewEmulatorGetRegistersCallback converts a Go function into a CALLBACK-compatible pointer.
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

// NewEmulatorSetRegistersCallback converts a Go function into a CALLBACK-compatible pointer.
func NewEmulatorSetRegistersCallback(fn EmulatorSetRegistersFunc) uintptr {
	if fn == nil {
		return 0
	}
	return syscall.NewCallback(func(ctx, namesPtr, count, valuesPtr uintptr) uintptr {
		names := makeRegisterNameSlice(namesPtr, uint32(count))
		values := makeRegisterValueSlice(valuesPtr, uint32(count))
		return uintptr(fn(unsafe.Pointer(ctx), names, values))
	})
}

// NewEmulatorTranslateGvaCallback converts a Go function into a CALLBACK-compatible pointer.
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

// EmulatorHandle mirrors WHV_EMULATOR_HANDLE.
type EmulatorHandle uintptr

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
