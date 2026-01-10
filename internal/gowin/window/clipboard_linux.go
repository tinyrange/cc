//go:build linux

package window

import (
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// linuxClipboard implements Clipboard using X11 selections.
type linuxClipboard struct{}

var (
	clipboardOnce sync.Once
	clipboardDpy  uintptr

	// X11 atoms for clipboard.
	atomClipboard uintptr
	atomUTF8      uintptr
	atomTargets   uintptr

	// X11 functions for clipboard.
	xOpenDisplayClip  func(*byte) uintptr
	xCloseDisplayClip func(uintptr) int32
	xInternAtomClip   func(uintptr, *byte, int32) uintptr
	xGetSelectionOwner func(uintptr, uintptr) uintptr
	xConvertSelection  func(uintptr, uintptr, uintptr, uintptr, uintptr, uintptr)
	xGetWindowProperty func(uintptr, uintptr, uintptr, int64, int64, int32, uintptr, *uintptr, *int32, *uint64, *uint64, *uintptr) int32
	xFree              func(uintptr) int32
	xDefaultRootWindow func(uintptr) uintptr
	xNextEventClip     func(uintptr, unsafe.Pointer)
	xPendingClip       func(uintptr) int32
	xSetSelectionOwner func(uintptr, uintptr, uintptr, uintptr) int32
	xChangeProperty    func(uintptr, uintptr, uintptr, uintptr, int32, int32, *byte, int32) int32
	xCreateSimpleWindow func(uintptr, uintptr, int32, int32, uint32, uint32, uint32, uint64, uint64) uintptr
	xDestroyWindowClip func(uintptr, uintptr) int32
	xSelectInputClip   func(uintptr, uintptr, int64)
	xFlush             func(uintptr)

	// Store clipboard data for owner events.
	clipboardData     string
	clipboardDataLock sync.Mutex
	clipboardWindow   uintptr
)

func initClipboardX11() {
	clipboardOnce.Do(func() {
		lib, err := purego.Dlopen("libX11.so.6", purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}

		purego.RegisterLibFunc(&xOpenDisplayClip, lib, "XOpenDisplay")
		purego.RegisterLibFunc(&xCloseDisplayClip, lib, "XCloseDisplay")
		purego.RegisterLibFunc(&xInternAtomClip, lib, "XInternAtom")
		purego.RegisterLibFunc(&xGetSelectionOwner, lib, "XGetSelectionOwner")
		purego.RegisterLibFunc(&xConvertSelection, lib, "XConvertSelection")
		purego.RegisterLibFunc(&xGetWindowProperty, lib, "XGetWindowProperty")
		purego.RegisterLibFunc(&xFree, lib, "XFree")
		purego.RegisterLibFunc(&xDefaultRootWindow, lib, "XDefaultRootWindow")
		purego.RegisterLibFunc(&xNextEventClip, lib, "XNextEvent")
		purego.RegisterLibFunc(&xPendingClip, lib, "XPending")
		purego.RegisterLibFunc(&xSetSelectionOwner, lib, "XSetSelectionOwner")
		purego.RegisterLibFunc(&xChangeProperty, lib, "XChangeProperty")
		purego.RegisterLibFunc(&xCreateSimpleWindow, lib, "XCreateSimpleWindow")
		purego.RegisterLibFunc(&xDestroyWindowClip, lib, "XDestroyWindow")
		purego.RegisterLibFunc(&xSelectInputClip, lib, "XSelectInput")
		purego.RegisterLibFunc(&xFlush, lib, "XFlush")

		// Open a display connection for clipboard operations.
		clipboardDpy = xOpenDisplayClip(nil)
		if clipboardDpy == 0 {
			return
		}

		// Get atoms.
		atomClipboard = xInternAtomClip(clipboardDpy, cStringClip("CLIPBOARD"), 0)
		atomUTF8 = xInternAtomClip(clipboardDpy, cStringClip("UTF8_STRING"), 0)
		atomTargets = xInternAtomClip(clipboardDpy, cStringClip("TARGETS"), 0)

		// Create a hidden window for clipboard operations.
		root := xDefaultRootWindow(clipboardDpy)
		clipboardWindow = xCreateSimpleWindow(clipboardDpy, root, 0, 0, 1, 1, 0, 0, 0)
	})
}

func getClipboard() Clipboard {
	initClipboardX11()
	return &linuxClipboard{}
}

func (c *linuxClipboard) GetText() string {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if clipboardDpy == 0 {
		return ""
	}

	// Check if there's a clipboard owner.
	owner := xGetSelectionOwner(clipboardDpy, atomClipboard)
	if owner == 0 {
		return ""
	}

	// If we own the clipboard, return our stored data.
	if owner == clipboardWindow {
		clipboardDataLock.Lock()
		data := clipboardData
		clipboardDataLock.Unlock()
		return data
	}

	// Request the clipboard content.
	propAtom := xInternAtomClip(clipboardDpy, cStringClip("CC_CLIPBOARD_PROP"), 0)
	xConvertSelection(clipboardDpy, atomClipboard, atomUTF8, propAtom, clipboardWindow, 0)
	xFlush(clipboardDpy)

	// Wait for SelectionNotify event (with timeout via polling).
	const selectionNotify = 31
	var ev [24]uint64 // xEvent buffer

	// Simple polling loop - wait for the selection event.
	for i := 0; i < 100; i++ { // ~100ms timeout
		if xPendingClip(clipboardDpy) > 0 {
			xNextEventClip(clipboardDpy, unsafe.Pointer(&ev[0]))
			etype := *(*int32)(unsafe.Pointer(&ev[0]))
			if etype == selectionNotify {
				break
			}
		}
		// Small sleep would be ideal but we'll just spin briefly.
	}

	// Read the property.
	var actualType uintptr
	var actualFormat int32
	var nItems, bytesAfter uint64
	var propData uintptr

	result := xGetWindowProperty(
		clipboardDpy,
		clipboardWindow,
		propAtom,
		0,
		1024*1024, // Up to 1MB
		1,         // Delete after read
		atomUTF8,
		&actualType,
		&actualFormat,
		&nItems,
		&bytesAfter,
		&propData,
	)

	if result != 0 || propData == 0 || nItems == 0 {
		return ""
	}
	defer xFree(propData)

	// Convert to Go string.
	return goStringN(propData, int(nItems))
}

func (c *linuxClipboard) SetText(text string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if clipboardDpy == 0 || clipboardWindow == 0 {
		return nil
	}

	// Store the data.
	clipboardDataLock.Lock()
	clipboardData = text
	clipboardDataLock.Unlock()

	// Take ownership of the clipboard.
	xSetSelectionOwner(clipboardDpy, atomClipboard, clipboardWindow, 0)
	xFlush(clipboardDpy)

	return nil
}

func cStringClip(s string) *byte {
	b := append([]byte(s), 0)
	return &b[0]
}

func goStringN(ptr uintptr, n int) string {
	if ptr == 0 || n <= 0 {
		return ""
	}
	bytes := make([]byte, n)
	for i := 0; i < n; i++ {
		bytes[i] = *(*byte)(unsafe.Pointer(ptr + uintptr(i)))
	}
	return string(bytes)
}
