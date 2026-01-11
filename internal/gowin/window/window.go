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

// DockMenuItem represents an item in the dock menu (macOS only)
type DockMenuItem struct {
	Title     string
	Tag       int
	Enabled   bool
	Separator bool
}

// DockMenuCallback is called when a dock menu item is selected
type DockMenuCallback func(tag int)

// DockMenuSupport is an optional interface that windows can implement
// to provide dock menu functionality (macOS only)
type DockMenuSupport interface {
	// SetDockMenu configures the dock menu items and callback
	SetDockMenu(items []DockMenuItem, callback DockMenuCallback)
}

// FileDialogType specifies what a file dialog should select
type FileDialogType int

const (
	FileDialogTypeDirectory FileDialogType = iota
	FileDialogTypeFile
)

// FileDialogSupport is an optional interface that windows can implement
// to provide native file dialog functionality
type FileDialogSupport interface {
	// ShowOpenPanel shows a native open file/directory dialog
	// Returns the selected path or empty string if cancelled
	ShowOpenPanel(dialogType FileDialogType, allowedExtensions []string) string
}

// GetDisplayScale returns the display scale factor before creating a window.
// This can be used to calculate the physical window size needed to achieve
// a desired logical size on HiDPI displays.
// Returns 1.0 if scale detection is not available or fails.
func GetDisplayScale() float32 {
	return getDisplayScale()
}
