//go:build windows

package window

import (
	"syscall"
	"unsafe"
)

// windowsClipboard implements Clipboard using Win32 APIs.
type windowsClipboard struct{}

var (
	procOpenClipboard    = user32.NewProc("OpenClipboard")
	procCloseClipboard   = user32.NewProc("CloseClipboard")
	procEmptyClipboard   = user32.NewProc("EmptyClipboard")
	procGetClipboardData = user32.NewProc("GetClipboardData")
	procSetClipboardData = user32.NewProc("SetClipboardData")

	procGlobalAlloc = kernel32.NewProc("GlobalAlloc")
	procGlobalFree  = kernel32.NewProc("GlobalFree")
	procGlobalLock  = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
)

const (
	cfUnicodeText = 13 // CF_UNICODETEXT

	gmemMoveable = 0x0002
)

func getClipboard() Clipboard {
	return &windowsClipboard{}
}

func (c *windowsClipboard) GetText() string {
	// Open clipboard.
	ret, _, _ := procOpenClipboard.Call(0)
	if ret == 0 {
		return ""
	}
	defer procCloseClipboard.Call()

	// Get clipboard data handle.
	hData, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if hData == 0 {
		return ""
	}

	// Lock the global memory.
	ptr, _, _ := procGlobalLock.Call(hData)
	if ptr == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(hData)

	// Convert wide string to Go string.
	return utf16PtrToString((*uint16)(unsafe.Pointer(ptr)))
}

func (c *windowsClipboard) SetText(text string) error {
	// Convert to UTF-16.
	utf16, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}

	// Calculate size in bytes (including null terminator).
	size := len(utf16) * 2

	// Allocate global memory.
	hMem, _, _ := procGlobalAlloc.Call(gmemMoveable, uintptr(size))
	if hMem == 0 {
		return nil
	}

	// Lock and copy data.
	ptr, _, _ := procGlobalLock.Call(hMem)
	if ptr == 0 {
		procGlobalFree.Call(hMem)
		return nil
	}

	// Copy UTF-16 data.
	dst := (*[1 << 20]uint16)(unsafe.Pointer(ptr))[:len(utf16):len(utf16)]
	copy(dst, utf16)

	procGlobalUnlock.Call(hMem)

	// Open clipboard.
	ret, _, _ := procOpenClipboard.Call(0)
	if ret == 0 {
		procGlobalFree.Call(hMem)
		return nil
	}

	// Empty clipboard.
	procEmptyClipboard.Call()

	// Set clipboard data.
	procSetClipboardData.Call(cfUnicodeText, hMem)

	// Close clipboard.
	procCloseClipboard.Call()

	return nil
}

// utf16PtrToString converts a null-terminated UTF-16 string to a Go string.
func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}

	// Find the null terminator.
	var length int
	for ptr := p; *ptr != 0; ptr = (*uint16)(unsafe.Pointer(uintptr(unsafe.Pointer(ptr)) + 2)) {
		length++
	}

	if length == 0 {
		return ""
	}

	// Create slice from the pointer.
	slice := (*[1 << 20]uint16)(unsafe.Pointer(p))[:length:length]
	return syscall.UTF16ToString(slice)
}
