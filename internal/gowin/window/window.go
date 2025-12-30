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
	// DrainInputEvents returns queued raw input events since the last call and
	// clears the internal queue.
	DrainInputEvents() []InputEvent
	// TextInput returns the UTF-8 text entered since the last call to TextInput.
	// Implementations should buffer text during Poll() and clear the buffer when
	// TextInput is called.
	TextInput() string
}

// GetDisplayScale returns the display scale factor before creating a window.
// This can be used to calculate the physical window size needed to achieve
// a desired logical size on HiDPI displays.
// Returns 1.0 if scale detection is not available or fails.
func GetDisplayScale() float32 {
	return getDisplayScale()
}
