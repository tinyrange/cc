package virtio

import "github.com/tinyrange/cc/internal/gowin/window"

// Linux evdev event types
const (
	EV_SYN = 0x00
	EV_KEY = 0x01
	EV_REL = 0x02
	EV_ABS = 0x03
	EV_MSC = 0x04
	EV_SW  = 0x05
	EV_LED = 0x11
	EV_SND = 0x12
	EV_REP = 0x14
	EV_FF  = 0x15
)

// Linux evdev synchronization events
const (
	SYN_REPORT    = 0
	SYN_CONFIG    = 1
	SYN_MT_REPORT = 2
	SYN_DROPPED   = 3
)

// Linux evdev absolute axis codes
const (
	ABS_X              = 0x00
	ABS_Y              = 0x01
	ABS_Z              = 0x02
	ABS_RX             = 0x03
	ABS_RY             = 0x04
	ABS_RZ             = 0x05
	ABS_THROTTLE       = 0x06
	ABS_RUDDER         = 0x07
	ABS_WHEEL          = 0x08
	ABS_GAS            = 0x09
	ABS_BRAKE          = 0x0a
	ABS_HAT0X          = 0x10
	ABS_HAT0Y          = 0x11
	ABS_HAT1X          = 0x12
	ABS_HAT1Y          = 0x13
	ABS_HAT2X          = 0x14
	ABS_HAT2Y          = 0x15
	ABS_HAT3X          = 0x16
	ABS_HAT3Y          = 0x17
	ABS_PRESSURE       = 0x18
	ABS_DISTANCE       = 0x19
	ABS_TILT_X         = 0x1a
	ABS_TILT_Y         = 0x1b
	ABS_TOOL_WIDTH     = 0x1c
	ABS_VOLUME         = 0x20
	ABS_MISC           = 0x28
	ABS_MT_SLOT        = 0x2f
	ABS_MT_TOUCH_MAJOR = 0x30
	ABS_MT_TOUCH_MINOR = 0x31
	ABS_MT_WIDTH_MAJOR = 0x32
	ABS_MT_WIDTH_MINOR = 0x33
	ABS_MT_ORIENTATION = 0x34
	ABS_MT_POSITION_X  = 0x35
	ABS_MT_POSITION_Y  = 0x36
	ABS_MT_TOOL_TYPE   = 0x37
	ABS_MT_BLOB_ID     = 0x38
	ABS_MT_TRACKING_ID = 0x39
	ABS_MT_PRESSURE    = 0x3a
	ABS_MT_DISTANCE    = 0x3b
	ABS_MT_TOOL_X      = 0x3c
	ABS_MT_TOOL_Y      = 0x3d
	ABS_MAX            = 0x3f
	ABS_CNT            = ABS_MAX + 1
)

// Linux evdev button codes
const (
	BTN_MISC           = 0x100
	BTN_0              = 0x100
	BTN_1              = 0x101
	BTN_2              = 0x102
	BTN_3              = 0x103
	BTN_4              = 0x104
	BTN_5              = 0x105
	BTN_6              = 0x106
	BTN_7              = 0x107
	BTN_8              = 0x108
	BTN_9              = 0x109
	BTN_MOUSE          = 0x110
	BTN_LEFT           = 0x110
	BTN_RIGHT          = 0x111
	BTN_MIDDLE         = 0x112
	BTN_SIDE           = 0x113
	BTN_EXTRA          = 0x114
	BTN_FORWARD        = 0x115
	BTN_BACK           = 0x116
	BTN_TASK           = 0x117
	BTN_JOYSTICK       = 0x120
	BTN_TRIGGER        = 0x120
	BTN_THUMB          = 0x121
	BTN_THUMB2         = 0x122
	BTN_TOP            = 0x123
	BTN_TOP2           = 0x124
	BTN_PINKIE         = 0x125
	BTN_BASE           = 0x126
	BTN_BASE2          = 0x127
	BTN_BASE3          = 0x128
	BTN_BASE4          = 0x129
	BTN_BASE5          = 0x12a
	BTN_BASE6          = 0x12b
	BTN_DEAD           = 0x12f
	BTN_GAMEPAD        = 0x130
	BTN_SOUTH          = 0x130
	BTN_A              = 0x130
	BTN_EAST           = 0x131
	BTN_B              = 0x131
	BTN_C              = 0x132
	BTN_NORTH          = 0x133
	BTN_X              = 0x133
	BTN_WEST           = 0x134
	BTN_Y              = 0x134
	BTN_Z              = 0x135
	BTN_TL             = 0x136
	BTN_TR             = 0x137
	BTN_TL2            = 0x138
	BTN_TR2            = 0x139
	BTN_SELECT         = 0x13a
	BTN_START          = 0x13b
	BTN_MODE           = 0x13c
	BTN_THUMBL         = 0x13d
	BTN_THUMBR         = 0x13e
	BTN_DIGI           = 0x140
	BTN_TOOL_PEN       = 0x140
	BTN_TOOL_RUBBER    = 0x141
	BTN_TOOL_BRUSH     = 0x142
	BTN_TOOL_PENCIL    = 0x143
	BTN_TOOL_AIRBRUSH  = 0x144
	BTN_TOOL_FINGER    = 0x145
	BTN_TOOL_MOUSE     = 0x146
	BTN_TOOL_LENS      = 0x147
	BTN_TOOL_QUINTTAP  = 0x148
	BTN_STYLUS3        = 0x149
	BTN_TOUCH          = 0x14a
	BTN_STYLUS         = 0x14b
	BTN_STYLUS2        = 0x14c
	BTN_TOOL_DOUBLETAP = 0x14d
	BTN_TOOL_TRIPLETAP = 0x14e
	BTN_TOOL_QUADTAP   = 0x14f
	BTN_WHEEL          = 0x150
	BTN_GEAR_DOWN      = 0x150
	BTN_GEAR_UP        = 0x151
)

// Linux evdev key codes
const (
	KEY_RESERVED         = 0
	KEY_ESC              = 1
	KEY_1                = 2
	KEY_2                = 3
	KEY_3                = 4
	KEY_4                = 5
	KEY_5                = 6
	KEY_6                = 7
	KEY_7                = 8
	KEY_8                = 9
	KEY_9                = 10
	KEY_0                = 11
	KEY_MINUS            = 12
	KEY_EQUAL            = 13
	KEY_BACKSPACE        = 14
	KEY_TAB              = 15
	KEY_Q                = 16
	KEY_W                = 17
	KEY_E                = 18
	KEY_R                = 19
	KEY_T                = 20
	KEY_Y                = 21
	KEY_U                = 22
	KEY_I                = 23
	KEY_O                = 24
	KEY_P                = 25
	KEY_LEFTBRACE        = 26
	KEY_RIGHTBRACE       = 27
	KEY_ENTER            = 28
	KEY_LEFTCTRL         = 29
	KEY_A                = 30
	KEY_S                = 31
	KEY_D                = 32
	KEY_F                = 33
	KEY_G                = 34
	KEY_H                = 35
	KEY_J                = 36
	KEY_K                = 37
	KEY_L                = 38
	KEY_SEMICOLON        = 39
	KEY_APOSTROPHE       = 40
	KEY_GRAVE            = 41
	KEY_LEFTSHIFT        = 42
	KEY_BACKSLASH        = 43
	KEY_Z                = 44
	KEY_X                = 45
	KEY_C                = 46
	KEY_V                = 47
	KEY_B                = 48
	KEY_N                = 49
	KEY_M                = 50
	KEY_COMMA            = 51
	KEY_DOT              = 52
	KEY_SLASH            = 53
	KEY_RIGHTSHIFT       = 54
	KEY_KPASTERISK       = 55
	KEY_LEFTALT          = 56
	KEY_SPACE            = 57
	KEY_CAPSLOCK         = 58
	KEY_F1               = 59
	KEY_F2               = 60
	KEY_F3               = 61
	KEY_F4               = 62
	KEY_F5               = 63
	KEY_F6               = 64
	KEY_F7               = 65
	KEY_F8               = 66
	KEY_F9               = 67
	KEY_F10              = 68
	KEY_NUMLOCK          = 69
	KEY_SCROLLLOCK       = 70
	KEY_KP7              = 71
	KEY_KP8              = 72
	KEY_KP9              = 73
	KEY_KPMINUS          = 74
	KEY_KP4              = 75
	KEY_KP5              = 76
	KEY_KP6              = 77
	KEY_KPPLUS           = 78
	KEY_KP1              = 79
	KEY_KP2              = 80
	KEY_KP3              = 81
	KEY_KP0              = 82
	KEY_KPDOT            = 83
	KEY_ZENKAKUHANKAKU   = 85
	KEY_102ND            = 86
	KEY_F11              = 87
	KEY_F12              = 88
	KEY_RO               = 89
	KEY_KATAKANA         = 90
	KEY_HIRAGANA         = 91
	KEY_HENKAN           = 92
	KEY_KATAKANAHIRAGANA = 93
	KEY_MUHENKAN         = 94
	KEY_KPJPCOMMA        = 95
	KEY_KPENTER          = 96
	KEY_RIGHTCTRL        = 97
	KEY_KPSLASH          = 98
	KEY_SYSRQ            = 99
	KEY_RIGHTALT         = 100
	KEY_LINEFEED         = 101
	KEY_HOME             = 102
	KEY_UP               = 103
	KEY_PAGEUP           = 104
	KEY_LEFT             = 105
	KEY_RIGHT            = 106
	KEY_END              = 107
	KEY_DOWN             = 108
	KEY_PAGEDOWN         = 109
	KEY_INSERT           = 110
	KEY_DELETE           = 111
	KEY_MACRO            = 112
	KEY_MUTE             = 113
	KEY_VOLUMEDOWN       = 114
	KEY_VOLUMEUP         = 115
	KEY_POWER            = 116
	KEY_KPEQUAL          = 117
	KEY_KPPLUSMINUS      = 118
	KEY_PAUSE            = 119
	KEY_SCALE            = 120
	KEY_KPCOMMA          = 121
	KEY_HANGEUL          = 122
	KEY_HANGUEL          = 122
	KEY_HANJA            = 123
	KEY_YEN              = 124
	KEY_LEFTMETA         = 125
	KEY_RIGHTMETA        = 126
	KEY_COMPOSE          = 127
	KEY_MAX              = 0x2ff
)

// GowinKeyToLinux maps Gowin window keys to Linux evdev keycodes
var GowinKeyToLinux = map[window.Key]uint16{
	window.KeyUnknown:      KEY_RESERVED,
	window.KeySpace:        KEY_SPACE,
	window.KeyApostrophe:   KEY_APOSTROPHE,
	window.KeyComma:        KEY_COMMA,
	window.KeyMinus:        KEY_MINUS,
	window.KeyPeriod:       KEY_DOT,
	window.KeySlash:        KEY_SLASH,
	window.Key0:            KEY_0,
	window.Key1:            KEY_1,
	window.Key2:            KEY_2,
	window.Key3:            KEY_3,
	window.Key4:            KEY_4,
	window.Key5:            KEY_5,
	window.Key6:            KEY_6,
	window.Key7:            KEY_7,
	window.Key8:            KEY_8,
	window.Key9:            KEY_9,
	window.KeySemicolon:    KEY_SEMICOLON,
	window.KeyEqual:        KEY_EQUAL,
	window.KeyA:            KEY_A,
	window.KeyB:            KEY_B,
	window.KeyC:            KEY_C,
	window.KeyD:            KEY_D,
	window.KeyE:            KEY_E,
	window.KeyF:            KEY_F,
	window.KeyG:            KEY_G,
	window.KeyH:            KEY_H,
	window.KeyI:            KEY_I,
	window.KeyJ:            KEY_J,
	window.KeyK:            KEY_K,
	window.KeyL:            KEY_L,
	window.KeyM:            KEY_M,
	window.KeyN:            KEY_N,
	window.KeyO:            KEY_O,
	window.KeyP:            KEY_P,
	window.KeyQ:            KEY_Q,
	window.KeyR:            KEY_R,
	window.KeyS:            KEY_S,
	window.KeyT:            KEY_T,
	window.KeyU:            KEY_U,
	window.KeyV:            KEY_V,
	window.KeyW:            KEY_W,
	window.KeyX:            KEY_X,
	window.KeyY:            KEY_Y,
	window.KeyZ:            KEY_Z,
	window.KeyLeftBracket:  KEY_LEFTBRACE,
	window.KeyBackslash:    KEY_BACKSLASH,
	window.KeyRightBracket: KEY_RIGHTBRACE,
	window.KeyGraveAccent:  KEY_GRAVE,
	window.KeyEscape:       KEY_ESC,
	window.KeyEnter:        KEY_ENTER,
	window.KeyTab:          KEY_TAB,
	window.KeyBackspace:    KEY_BACKSPACE,
	window.KeyInsert:       KEY_INSERT,
	window.KeyDelete:       KEY_DELETE,
	window.KeyRight:        KEY_RIGHT,
	window.KeyLeft:         KEY_LEFT,
	window.KeyDown:         KEY_DOWN,
	window.KeyUp:           KEY_UP,
	window.KeyPageUp:       KEY_PAGEUP,
	window.KeyPageDown:     KEY_PAGEDOWN,
	window.KeyHome:         KEY_HOME,
	window.KeyEnd:          KEY_END,
	window.KeyCapsLock:     KEY_CAPSLOCK,
	window.KeyScrollLock:   KEY_SCROLLLOCK,
	window.KeyNumLock:      KEY_NUMLOCK,
	window.KeyPrintScreen:  KEY_SYSRQ,
	window.KeyPause:        KEY_PAUSE,
	window.KeyF1:           KEY_F1,
	window.KeyF2:           KEY_F2,
	window.KeyF3:           KEY_F3,
	window.KeyF4:           KEY_F4,
	window.KeyF5:           KEY_F5,
	window.KeyF6:           KEY_F6,
	window.KeyF7:           KEY_F7,
	window.KeyF8:           KEY_F8,
	window.KeyF9:           KEY_F9,
	window.KeyF10:          KEY_F10,
	window.KeyF11:          KEY_F11,
	window.KeyF12:          KEY_F12,
	window.KeyLeftShift:    KEY_LEFTSHIFT,
	window.KeyLeftControl:  KEY_LEFTCTRL,
	window.KeyLeftAlt:      KEY_LEFTALT,
	window.KeyLeftSuper:    KEY_LEFTMETA,
	window.KeyRightShift:   KEY_RIGHTSHIFT,
	window.KeyRightControl: KEY_RIGHTCTRL,
	window.KeyRightAlt:     KEY_RIGHTALT,
	window.KeyRightSuper:   KEY_RIGHTMETA,
}

// GowinButtonToLinux maps Gowin window buttons to Linux evdev button codes
var GowinButtonToLinux = map[window.Button]uint16{
	window.ButtonLeft:   BTN_LEFT,
	window.ButtonRight:  BTN_RIGHT,
	window.ButtonMiddle: BTN_MIDDLE,
	window.Button4:      BTN_SIDE,
	window.Button5:      BTN_EXTRA,
}

// TabletAxisMax is the maximum value for tablet absolute axes (per virtio-input spec)
const TabletAxisMax = 32767

// NormalizeTabletCoord normalizes a coordinate from window space to tablet space (0-32767)
func NormalizeTabletCoord(pos float32, windowSize int) int32 {
	if windowSize <= 0 {
		return 0
	}
	normalized := (pos / float32(windowSize)) * float32(TabletAxisMax)
	if normalized < 0 {
		return 0
	}
	if normalized > TabletAxisMax {
		return TabletAxisMax
	}
	return int32(normalized)
}

// AllKeyboardKeys returns a list of all keys that a keyboard device should support
func AllKeyboardKeys() []uint16 {
	keys := make([]uint16, 0, len(GowinKeyToLinux))
	for _, code := range GowinKeyToLinux {
		if code != KEY_RESERVED {
			keys = append(keys, code)
		}
	}
	return keys
}

// AllTabletButtons returns a list of all buttons that a tablet device should support
func AllTabletButtons() []uint16 {
	return []uint16{BTN_LEFT, BTN_RIGHT, BTN_MIDDLE, BTN_TOUCH}
}
