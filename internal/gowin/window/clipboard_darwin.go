//go:build darwin

package window

import (
	"runtime"
	"sync"

	"github.com/ebitengine/purego/objc"
)

// darwinClipboard implements Clipboard using NSPasteboard.
type darwinClipboard struct{}

var (
	clipboardOnce sync.Once

	// Cached selectors for NSPasteboard.
	selGeneralPasteboard  objc.SEL
	selClearContents      objc.SEL
	selSetStringForType   objc.SEL
	selStringForType      objc.SEL
	selPasteboardTypeStr  objc.ID // NSPasteboardTypeString constant
)

func initClipboardSelectors() {
	clipboardOnce.Do(func() {
		// Ensure runtime is loaded (reuse from window_darwin.go).
		if err := ensureRuntime(); err != nil {
			return
		}

		selGeneralPasteboard = objc.RegisterName("generalPasteboard")
		selClearContents = objc.RegisterName("clearContents")
		selSetStringForType = objc.RegisterName("setString:forType:")
		selStringForType = objc.RegisterName("stringForType:")

		// Get NSPasteboardTypeString constant - it's an NSString.
		// We create the string value directly since it's "public.utf8-plain-text".
		selPasteboardTypeStr = nsString("public.utf8-plain-text")
	})
}

func getClipboard() Clipboard {
	initClipboardSelectors()
	return &darwinClipboard{}
}

func (c *darwinClipboard) GetText() string {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	initClipboardSelectors()

	// Create autorelease pool for this operation.
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc)
	pool = pool.Send(selInit)
	if pool != 0 {
		defer pool.Send(selRelease)
	}

	// Get the general pasteboard.
	pbClass := objc.GetClass("NSPasteboard")
	if pbClass == 0 {
		return ""
	}
	pb := objc.ID(pbClass).Send(selGeneralPasteboard)
	if pb == 0 {
		return ""
	}

	// Get string content for plain text type.
	str := objc.Send[objc.ID](pb, selStringForType, selPasteboardTypeStr)
	if str == 0 {
		return ""
	}

	return nsStringToGo(str)
}

func (c *darwinClipboard) SetText(text string) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	initClipboardSelectors()

	// Create autorelease pool for this operation.
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selAlloc)
	pool = pool.Send(selInit)
	if pool != 0 {
		defer pool.Send(selRelease)
	}

	// Get the general pasteboard.
	pbClass := objc.GetClass("NSPasteboard")
	if pbClass == 0 {
		return nil
	}
	pb := objc.ID(pbClass).Send(selGeneralPasteboard)
	if pb == 0 {
		return nil
	}

	// Clear existing contents.
	pb.Send(selClearContents)

	// Set the new string content.
	nsStr := nsString(text)
	objc.Send[bool](pb, selSetStringForType, nsStr, selPasteboardTypeStr)

	return nil
}

