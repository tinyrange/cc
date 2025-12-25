package window

import "github.com/tinyrange/cc/internal/gowin/gl"

type Window interface {
	GL() (gl.OpenGL, error)
	Close()
	Poll() bool
	Swap()
	BackingSize() (width, height int)
	Cursor() (x, y float32)
	Scale() float32
	GetKeyState(key Key) KeyState
	GetButtonState(button Button) ButtonState
}

// GetDisplayScale returns the display scale factor before creating a window.
// This can be used to calculate the physical window size needed to achieve
// a desired logical size on HiDPI displays.
// Returns 1.0 if scale detection is not available or fails.
func GetDisplayScale() float32 {
	return getDisplayScale()
}
