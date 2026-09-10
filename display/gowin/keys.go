// Key mapping shared with the CrumbleCracker desktop frontend.
package gowin

import "github.com/tinyrange/gowin/window"

var keycodes = map[window.Key]uint16{
	window.KeyEscape: 1,
	window.Key1:      2, window.Key2: 3, window.Key3: 4, window.Key4: 5, window.Key5: 6,
	window.Key6: 7, window.Key7: 8, window.Key8: 9, window.Key9: 10, window.Key0: 11,
	window.KeyMinus: 12, window.KeyEqual: 13, window.KeyBackspace: 14, window.KeyTab: 15,
	window.KeyQ: 16, window.KeyW: 17, window.KeyE: 18, window.KeyR: 19, window.KeyT: 20,
	window.KeyY: 21, window.KeyU: 22, window.KeyI: 23, window.KeyO: 24, window.KeyP: 25,
	window.KeyLeftBracket: 26, window.KeyRightBracket: 27, window.KeyEnter: 28,
	window.KeyLeftControl: 29, window.KeyA: 30, window.KeyS: 31, window.KeyD: 32,
	window.KeyF: 33, window.KeyG: 34, window.KeyH: 35, window.KeyJ: 36, window.KeyK: 37,
	window.KeyL: 38, window.KeySemicolon: 39, window.KeyApostrophe: 40,
	window.KeyGraveAccent: 41, window.KeyLeftShift: 42, window.KeyBackslash: 43,
	window.KeyZ: 44, window.KeyX: 45, window.KeyC: 46, window.KeyV: 47, window.KeyB: 48,
	window.KeyN: 49, window.KeyM: 50, window.KeyComma: 51, window.KeyPeriod: 52,
	window.KeySlash: 53, window.KeyRightShift: 54, window.KeyNumpadMultiply: 55,
	window.KeyLeftAlt: 56, window.KeySpace: 57, window.KeyCapsLock: 58,
	window.KeyF1: 59, window.KeyF2: 60, window.KeyF3: 61, window.KeyF4: 62,
	window.KeyF5: 63, window.KeyF6: 64, window.KeyF7: 65, window.KeyF8: 66,
	window.KeyF9: 67, window.KeyF10: 68, window.KeyNumLock: 69, window.KeyScrollLock: 70,
	window.KeyNumpad7: 71, window.KeyNumpad8: 72, window.KeyNumpad9: 73,
	window.KeyNumpadSubtract: 74, window.KeyNumpad4: 75, window.KeyNumpad5: 76,
	window.KeyNumpad6: 77, window.KeyNumpadAdd: 78, window.KeyNumpad1: 79,
	window.KeyNumpad2: 80, window.KeyNumpad3: 81, window.KeyNumpad0: 82,
	window.KeyNumpadDecimal: 83, window.KeyF11: 87, window.KeyF12: 88,
	window.KeyNumpadEnter: 96, window.KeyRightControl: 97, window.KeyNumpadDivide: 98,
	window.KeyPrintScreen: 99, window.KeyRightAlt: 100, window.KeyHome: 102,
	window.KeyUp: 103, window.KeyPageUp: 104, window.KeyLeft: 105, window.KeyRight: 106,
	window.KeyEnd: 107, window.KeyDown: 108, window.KeyPageDown: 109, window.KeyInsert: 110,
	window.KeyDelete: 111, window.KeyPause: 119, window.KeyLeftSuper: 125,
	window.KeyRightSuper: 126, window.KeyNumpadEqual: 117,
}
