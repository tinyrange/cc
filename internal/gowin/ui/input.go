package ui

import (
	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/window"
)

// Event is the base interface for all UI events.
type Event interface {
	isEvent()
}

// MouseMoveEvent represents cursor movement.
type MouseMoveEvent struct {
	X, Y float32
}

func (*MouseMoveEvent) isEvent() {}

// MouseButtonEvent represents a mouse button press/release.
type MouseButtonEvent struct {
	X, Y    float32
	Button  window.Button
	Pressed bool
}

func (*MouseButtonEvent) isEvent() {}

// ScrollEvent represents a scroll wheel event.
type ScrollEvent struct {
	X, Y           float32
	DeltaX, DeltaY float32
}

func (*ScrollEvent) isEvent() {}

// KeyEvent represents a keyboard event.
type KeyEvent struct {
	Key     window.Key
	Pressed bool
	Repeat  bool
	Mods    window.KeyMods
}

func (*KeyEvent) isEvent() {}

// TextEvent represents text input.
type TextEvent struct {
	Text string
}

func (*TextEvent) isEvent() {}

// InputProcessor converts platform events to UI events.
type InputProcessor struct {
	prevMouseX, prevMouseY float32
	prevButtons            map[window.Button]bool
	initialized            bool
}

// ProcessFrame extracts events from the frame and platform window.
func (p *InputProcessor) ProcessFrame(f graphics.Frame, pw window.Window) []Event {
	if !p.initialized {
		p.prevButtons = make(map[window.Button]bool)
		p.initialized = true
	}

	var events []Event

	// Cursor position - always generate move event for hover tracking
	mx, my := f.CursorPos()
	events = append(events, &MouseMoveEvent{X: mx, Y: my})
	p.prevMouseX = mx
	p.prevMouseY = my

	// Check mouse buttons
	buttons := []window.Button{window.ButtonLeft, window.ButtonRight, window.ButtonMiddle}
	for _, btn := range buttons {
		isDown := f.GetButtonState(btn).IsDown()
		wasDown := p.prevButtons[btn]
		if isDown != wasDown {
			events = append(events, &MouseButtonEvent{
				X:       mx,
				Y:       my,
				Button:  btn,
				Pressed: isDown,
			})
			p.prevButtons[btn] = isDown
		}
	}

	// Raw platform events
	for _, ev := range pw.DrainInputEvents() {
		switch ev.Type {
		case window.InputEventScroll:
			events = append(events, &ScrollEvent{
				X:      mx,
				Y:      my,
				DeltaX: ev.ScrollX,
				DeltaY: ev.ScrollY,
			})
		case window.InputEventKeyDown:
			events = append(events, &KeyEvent{
				Key:     ev.Key,
				Pressed: true,
				Repeat:  ev.Repeat,
				Mods:    ev.Mods,
			})
		case window.InputEventKeyUp:
			events = append(events, &KeyEvent{
				Key:     ev.Key,
				Pressed: false,
				Mods:    ev.Mods,
			})
		case window.InputEventText:
			if ev.Text != "" {
				events = append(events, &TextEvent{Text: ev.Text})
			}
		}
	}

	return events
}
