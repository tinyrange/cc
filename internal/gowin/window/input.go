package window

import "fmt"

// Key represents a keyboard key.
type Key int

const (
	KeyUnknown Key = iota

	// Letters
	KeyA
	KeyB
	KeyC
	KeyD
	KeyE
	KeyF
	KeyG
	KeyH
	KeyI
	KeyJ
	KeyK
	KeyL
	KeyM
	KeyN
	KeyO
	KeyP
	KeyQ
	KeyR
	KeyS
	KeyT
	KeyU
	KeyV
	KeyW
	KeyX
	KeyY
	KeyZ

	// Numbers
	Key0
	Key1
	Key2
	Key3
	Key4
	Key5
	Key6
	Key7
	Key8
	Key9

	// Function keys
	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12

	// Modifier keys
	KeyLeftShift
	KeyRightShift
	KeyLeftControl
	KeyRightControl
	KeyLeftAlt
	KeyRightAlt
	KeyLeftSuper  // Windows key on Windows, Command key on macOS
	KeyRightSuper // Windows key on Windows, Command key on macOS

	// Special keys
	KeySpace
	KeyEnter
	KeyEscape
	KeyBackspace
	KeyDelete
	KeyTab
	KeyCapsLock
	KeyScrollLock
	KeyNumLock
	KeyPrintScreen
	KeyPause

	// Arrow keys
	KeyUp
	KeyDown
	KeyLeft
	KeyRight

	// Navigation keys
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyInsert

	// Punctuation and symbols
	KeyGraveAccent  // `
	KeyMinus        // -
	KeyEqual        // =
	KeyLeftBracket  // [
	KeyRightBracket // ]
	KeyBackslash    // \
	KeySemicolon    // ;
	KeyApostrophe   // '
	KeyComma        // ,
	KeyPeriod       // .
	KeySlash        // /

	// Numpad keys
	KeyNumpad0
	KeyNumpad1
	KeyNumpad2
	KeyNumpad3
	KeyNumpad4
	KeyNumpad5
	KeyNumpad6
	KeyNumpad7
	KeyNumpad8
	KeyNumpad9
	KeyNumpadDecimal  // .
	KeyNumpadDivide   // /
	KeyNumpadMultiply // *
	KeyNumpadSubtract // -
	KeyNumpadAdd      // +
	KeyNumpadEnter
	KeyNumpadEqual // =
)

// Button represents a mouse button.
type Button int

const (
	ButtonLeft Button = iota
	ButtonRight
	ButtonMiddle
	Button4 // Additional mouse button (often back button)
	Button5 // Additional mouse button (often forward button)
)

// KeyState represents the state of a keyboard key.
type KeyState int

const (
	// KeyStatePressed indicates the key was pressed this frame
	KeyStatePressed KeyState = iota
	// KeyStateDown indicates the key is currently down
	KeyStateDown
	// KeyStateReleased indicates the key was released this frame
	KeyStateReleased
	// KeyStateUp indicates the key is currently up
	KeyStateUp
	// KeyStateRepeated indicates the key is being held down (repeated)
	KeyStateRepeated
)

// ButtonState represents the state of a mouse button.
type ButtonState int

const (
	// ButtonStatePressed indicates the button was pressed this frame
	ButtonStatePressed ButtonState = iota
	// ButtonStateDown indicates the button is currently down
	ButtonStateDown
	// ButtonStateReleased indicates the button was released this frame
	ButtonStateReleased
	// ButtonStateUp indicates the button is currently up
	ButtonStateUp
)

// IsDown returns true if the key state indicates the key is currently down.
func (ks KeyState) IsDown() bool {
	return ks == KeyStatePressed || ks == KeyStateDown || ks == KeyStateRepeated
}

// IsDown returns true if the button state indicates the button is currently down.
func (bs ButtonState) IsDown() bool {
	return bs == ButtonStatePressed || bs == ButtonStateDown
}

// KeyMods represents currently active keyboard modifiers.
type KeyMods uint8

const (
	ModShift KeyMods = 1 << iota
	ModCtrl
	ModAlt
	ModSuper
)

// InputEventType describes the kind of input event.
type InputEventType uint8

const (
	InputEventKeyDown InputEventType = iota
	InputEventKeyUp
	InputEventFlagsChanged
	InputEventText
	InputEventMouseDown
	InputEventMouseUp
	InputEventScroll
)

func (t InputEventType) String() string {
	switch t {
	case InputEventKeyDown:
		return "KeyDown"
	case InputEventKeyUp:
		return "KeyUp"
	case InputEventFlagsChanged:
		return "FlagsChanged"
	case InputEventText:
		return "Text"
	case InputEventMouseDown:
		return "MouseDown"
	case InputEventMouseUp:
		return "MouseUp"
	case InputEventScroll:
		return "Scroll"
	default:
		return fmt.Sprintf("InputEventType(%d)", uint8(t))
	}
}

// InputEvent is a raw input event emitted by a platform window backend.
//
// Contract:
// - Events are queued during Poll() and returned by DrainInputEvents().
// - DrainInputEvents() clears the internal queue.
type InputEvent struct {
	Type   InputEventType
	Key    Key
	Text   string
	Mods   KeyMods
	Repeat bool

	// Button is meaningful for MouseDown/MouseUp.
	Button Button

	// ScrollX/ScrollY are meaningful for Scroll events. Values are in "wheel ticks"
	// where 1.0 corresponds to a standard mouse wheel notch (platform-dependent).
	ScrollX float32
	ScrollY float32
}
