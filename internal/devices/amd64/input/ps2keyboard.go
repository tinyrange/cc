package input

import (
	"sync"

	"github.com/tinyrange/cc/internal/chipset"
)

const (
	// Keyboard commands
	ps2CmdReset        = 0xff
	ps2CmdResend       = 0xfe
	ps2CmdSetDefaults  = 0xf6
	ps2CmdDisable      = 0xf5
	ps2CmdEnable       = 0xf4
	ps2CmdSetTypematic = 0xf3
	ps2CmdSetLEDs      = 0xed
	ps2CmdEcho         = 0xee
	ps2CmdSetScancode  = 0xf0
	ps2CmdIdentify     = 0xf2
	ps2CmdSetRate      = 0xf3

	// Keyboard responses
	ps2ResponseAck      = 0xfa
	ps2ResponseResend   = 0xfe
	ps2ResponseError    = 0xfc
	ps2ResponseTestPass = 0xaa
	ps2ResponseEcho     = 0xee

	// Scancode sets
	scancodeSet1 = 1
	scancodeSet2 = 2
	scancodeSet3 = 3
)

// PS2Keyboard implements a PS/2 keyboard device.
type PS2Keyboard struct {
	mu sync.Mutex

	controller *I8042
	irq        chipset.LineInterrupt

	// Keyboard state
	enabled        bool
	scancodeSet    int
	typematicRate  byte
	typematicDelay byte
	leds           byte // Caps Lock, Num Lock, Scroll Lock

	// Command state
	expectingTypematic byte
	expectingLEDs      bool
	expectingScancode  bool
}

// NewPS2Keyboard creates a new PS/2 keyboard device.
func NewPS2Keyboard() *PS2Keyboard {
	return &PS2Keyboard{
		enabled:     true,
		scancodeSet: scancodeSet2, // Default to set 2
		leds:        0,
	}
}

// SetIRQ sets the interrupt line for this keyboard.
func (k *PS2Keyboard) SetIRQ(line chipset.LineInterrupt) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if line == nil {
		k.irq = chipset.LineInterruptDetached()
	} else {
		k.irq = line
	}
}

// SetController sets the i8042 controller this keyboard belongs to.
func (k *PS2Keyboard) SetController(ctrl *I8042) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.controller = ctrl
}

// Reset resets the keyboard to default state.
func (k *PS2Keyboard) Reset() {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.resetLocked()
}

// resetLocked resets the keyboard state (must be called with mutex held).
func (k *PS2Keyboard) resetLocked() {
	k.enabled = true
	k.scancodeSet = scancodeSet2
	k.typematicRate = 0x20 // Default rate
	k.typematicDelay = 0x00
	k.leds = 0
	k.expectingTypematic = 0
	k.expectingLEDs = false
	k.expectingScancode = false
}

// HandleCommand processes a command sent to the keyboard.
func (k *PS2Keyboard) HandleCommand(cmd byte) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	if k.expectingLEDs {
		k.leds = cmd
		k.expectingLEDs = false
		k.sendResponse(ps2ResponseAck)
		return nil
	}

	if k.expectingTypematic != 0 {
		if k.expectingTypematic == ps2CmdSetTypematic {
			k.typematicRate = cmd
			k.expectingTypematic = 0
			k.sendResponse(ps2ResponseAck)
			return nil
		}
	}

	if k.expectingScancode {
		// Set scancode set (0x01, 0x02, or 0x03)
		if cmd >= 1 && cmd <= 3 {
			k.scancodeSet = int(cmd)
			k.expectingScancode = false
			k.sendResponse(ps2ResponseAck)
		} else {
			k.expectingScancode = false
			k.sendResponse(ps2ResponseError)
		}
		return nil
	}

	switch cmd {
	case ps2CmdReset:
		k.resetLocked()
		k.sendResponse(ps2ResponseAck)
		k.sendResponse(ps2ResponseTestPass)

	case ps2CmdResend:
		// Resend last byte - not implemented
		k.sendResponse(ps2ResponseResend)

	case ps2CmdSetDefaults:
		k.typematicRate = 0x20
		k.typematicDelay = 0x00
		k.sendResponse(ps2ResponseAck)

	case ps2CmdDisable:
		k.enabled = false
		k.sendResponse(ps2ResponseAck)

	case ps2CmdEnable:
		k.enabled = true
		k.sendResponse(ps2ResponseAck)

	case ps2CmdSetTypematic:
		k.expectingTypematic = ps2CmdSetTypematic
		k.sendResponse(ps2ResponseAck)

	case ps2CmdSetLEDs:
		k.expectingLEDs = true
		k.sendResponse(ps2ResponseAck)

	case ps2CmdEcho:
		k.sendResponse(ps2ResponseEcho)

	case ps2CmdSetScancode:
		k.expectingScancode = true
		k.sendResponse(ps2ResponseAck)

	case ps2CmdIdentify:
		k.sendResponse(ps2ResponseAck)
		// Send keyboard ID (0xAB 0x83 for MF2 keyboard)
		k.sendData(0xab)
		k.sendData(0x83)

	default:
		// Unknown command
		k.sendResponse(ps2ResponseError)
	}

	return nil
}

// SendKey sends a key press/release event to the keyboard.
func (k *PS2Keyboard) SendKey(scancode byte, pressed bool) {
	k.mu.Lock()
	defer k.mu.Unlock()

	if !k.enabled {
		return
	}

	// Translate scancode set 1 to set 2 if needed
	scancodeToSend := scancode
	if k.scancodeSet == scancodeSet2 {
		scancodeToSend = translateScancodeSet1ToSet2(scancode)
	}

	if pressed {
		k.sendData(scancodeToSend)
	} else {
		// Release: send 0xF0 prefix for set 2, or 0x80+scancode for set 1
		if k.scancodeSet == scancodeSet2 {
			k.sendData(0xf0)
			k.sendData(scancodeToSend)
		} else {
			k.sendData(0x80 | scancodeToSend)
		}
	}
}

func (k *PS2Keyboard) sendResponse(resp byte) {
	if k.controller != nil {
		// We're called from HandleCommand which holds k.mu, but we need to
		// call into the controller. The controller's QueueKeyboardDataLocked
		// expects the controller's mutex to be held, but we don't have it.
		// So we need to use the public QueueKeyboardData which acquires the lock.
		// However, this can cause deadlocks if called from within a locked context.
		// The solution is to ensure the controller's methods can be called safely.
		// For now, we'll use QueueKeyboardData which is safe for external callers.
		k.controller.QueueKeyboardData(resp)
	}
}

func (k *PS2Keyboard) sendData(data byte) {
	if k.controller != nil {
		k.controller.QueueKeyboardData(data)
	}
}

// translateScancodeSet1ToSet2 translates a scancode from set 1 to set 2.
// This is a simplified translation - full table would be more comprehensive.
func translateScancodeSet1ToSet2(set1 byte) byte {
	// Basic translation table for common keys
	// This is a simplified version - a full implementation would have a complete table
	translation := map[byte]byte{
		0x01: 0x76, // ESC
		0x02: 0x05, // 1
		0x03: 0x06, // 2
		0x04: 0x04, // 3
		0x05: 0x0c, // 4
		0x06: 0x03, // 5
		0x07: 0x0b, // 6
		0x08: 0x83, // 7
		0x09: 0x0a, // 8
		0x0a: 0x01, // 9
		0x0b: 0x09, // 0
		0x0c: 0x78, // -
		0x0d: 0x07, // =
		0x0e: 0x0e, // Backspace
		0x0f: 0x0f, // Tab
		0x10: 0x0d, // Q
		0x11: 0x19, // W
		0x12: 0x1e, // E
		0x13: 0x1f, // R
		0x14: 0x20, // T
		0x15: 0x21, // Y
		0x16: 0x22, // U
		0x17: 0x23, // I
		0x18: 0x24, // O
		0x19: 0x25, // P
		0x1a: 0x26, // [
		0x1b: 0x27, // ]
		0x1c: 0x28, // Enter
		0x1d: 0x29, // Left Ctrl
		0x1e: 0x2e, // A
		0x1f: 0x2f, // S
		0x20: 0x30, // D
		0x21: 0x31, // F
		0x22: 0x32, // G
		0x23: 0x33, // H
		0x24: 0x34, // J
		0x25: 0x35, // K
		0x26: 0x36, // L
		0x27: 0x37, // ;
		0x28: 0x38, // '
		0x29: 0x39, // `
		0x2a: 0x2a, // Left Shift
		0x2b: 0x56, // \
		0x2c: 0x2c, // Z
		0x2d: 0x2d, // X
		0x2e: 0x2e, // C
		0x2f: 0x2f, // V
		0x30: 0x30, // B
		0x31: 0x31, // N
		0x32: 0x32, // M
		0x33: 0x33, // ,
		0x34: 0x34, // .
		0x35: 0x35, // /
		0x36: 0x36, // Right Shift
		0x37: 0x73, // Print Screen
		0x38: 0x1d, // Left Alt
		0x39: 0x39, // Space
		0x3a: 0x58, // Caps Lock
		0x3b: 0x07, // F1
		0x3c: 0x0f, // F2
		0x3d: 0x17, // F3
		0x3e: 0x1f, // F4
		0x3f: 0x27, // F5
		0x40: 0x2f, // F6
		0x41: 0x37, // F7
		0x42: 0x3f, // F8
		0x43: 0x47, // F9
		0x44: 0x4f, // F10
		0x45: 0x56, // Num Lock
		0x46: 0x57, // Scroll Lock
	}

	if translated, ok := translation[set1]; ok {
		return translated
	}
	// If no translation found, return as-is (simplified)
	return set1
}
