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
