//go:build windows

package bindings

import (
	"runtime"
	"syscall"
	"unsafe"
)

const (
	PAGE_SIZE = 0x1000

	MEM_COMMIT  = 0x1000
	MEM_RESERVE = 0x2000
	MEM_RELEASE = 0x8000

	PAGE_READWRITE         = 0x04
	PAGE_EXECUTE_READWRITE = 0x40
)

var (
	kernel32DLL      = syscall.NewLazyDLL("kernel32.dll")
	procVirtualAlloc = kernel32DLL.NewProc("VirtualAlloc")
	procVirtualFree  = kernel32DLL.NewProc("VirtualFree")
)

type Allocation struct {
	addr    uintptr
	size    uintptr
	cleanup runtime.Cleanup // Store handle to prevent double-free
}

// Pointer returns the unsafe.Pointer to the memory.
// Note: We keep addr as uintptr in the struct to avoid GC scanning of
// non-Go memory, only converting to unsafe.Pointer when needed.
func (a *Allocation) Pointer() unsafe.Pointer {
	return unsafe.Pointer(a.addr)
}

// Slice returns a byte slice backing the memory.
func (a *Allocation) Slice() []byte {
	// unsafe.Slice requires a pointer to the element type (*byte)
	return unsafe.Slice((*byte)(unsafe.Pointer(a.addr)), int(a.size))
}

func (a *Allocation) Size() uint64 {
	return uint64(a.size)
}

// releaseMem is the standalone cleanup function.
// It must NOT take *Allocation as an argument to avoid object resurrection.
func releaseMem(addr uintptr) {
	// For MEM_RELEASE, dwSize must be 0.
	procVirtualFree.Call(addr, 0, MEM_RELEASE)
}

// VirtualAlloc allocates memory via WinAPI.
func VirtualAlloc(addr uintptr, size uintptr, allocType uint32, protect uint32) (*Allocation, error) {
	ptr, _, err := procVirtualAlloc.Call(addr, size, uintptr(allocType), uintptr(protect))
	if ptr == 0 {
		if err == syscall.Errno(0) {
			err = syscall.GetLastError()
		}
		return nil, err
	}

	alloc := &Allocation{
		addr: ptr,
		size: size,
	}

	// Register cleanup.
	// 1. Target: alloc
	// 2. Function: releaseMem (func(uintptr))
	// 3. Arg: ptr (uintptr) -- We pass the raw address, NOT the alloc struct.
	alloc.cleanup = runtime.AddCleanup(alloc, releaseMem, ptr)

	return alloc, nil
}

// VirtualFree frees memory allocated with VirtualAlloc.
// It stops the automatic cleanup to prevent double-freeing.
func VirtualFree(alloc *Allocation, freeType uint32) error {
	// If we are manually freeing, we must stop the GC cleanup to prevent
	// it from trying to free this address again later.
	if freeType == MEM_RELEASE {
		alloc.cleanup.Stop()
	}

	sizeArg := uintptr(0)
	if freeType != MEM_RELEASE {
		sizeArg = alloc.size
	}

	r1, _, err := procVirtualFree.Call(alloc.addr, sizeArg, uintptr(freeType))
	if r1 == 0 {
		if err == syscall.Errno(0) {
			err = syscall.GetLastError()
		}
		return err
	}
	return nil
}
