// Package display defines the host-facing interface for a graphical VM.
package display

import (
	"image"
)

// FramebufferUpdate is an XRGB8888 framebuffer snapshot. Pixels contain only
// Rect, tightly packed by row, with each pixel in blue, green, red, unused
// byte order.
type FramebufferUpdate struct {
	Width      int
	Height     int
	Generation uint64
	Rect       image.Rectangle
	Pixels     []byte
}

// Session provides direct access to a running VM's graphical desktop. Key
// accepts Linux input-event key codes. Pointer uses absolute guest coordinates
// and the conventional VNC button mask: left=1, middle=2, right=4, wheel=8/16.
type Session interface {
	Size() (width, height int)
	Snapshot(request image.Rectangle, since uint64, incremental bool) FramebufferUpdate
	Changed() <-chan struct{}
	Resize(width, height int) error
	Key(code uint16, down bool) error
	Pointer(x, y uint32, buttons, previousButtons uint8) error
	SetClipboard(text string)
	GuestClipboard() (text string, generation uint64)
}

// HighResolutionScroller is implemented by display sessions that accept
// horizontal and vertical wheel movement in v120 units, where 120 is one
// conventional wheel detent.
type HighResolutionScroller interface {
	Scroll(deltaX120, deltaY120 int32) error
}
