package ui

import (
	"image/color"
	"time"
	"unicode/utf8"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/window"
)

// TextInputStyle defines text input appearance.
type TextInputStyle struct {
	BackgroundColor        color.Color
	BackgroundColorFocused color.Color
	BorderColor            color.Color
	BorderColorFocused     color.Color
	TextColor              color.Color
	PlaceholderColor       color.Color
	CursorColor            color.Color
	TextSize               float64
	Padding                EdgeInsets
	MinWidth               float32
	Height                 float32
	CornerRadius           float32 // 0 = square corners
}

// DefaultTextInputStyle returns the default text input styling.
func DefaultTextInputStyle() TextInputStyle {
	return TextInputStyle{
		BackgroundColor:        color.RGBA{R: 30, G: 30, B: 30, A: 255},
		BackgroundColorFocused: color.RGBA{R: 35, G: 35, B: 35, A: 255},
		BorderColor:            color.RGBA{R: 60, G: 60, B: 60, A: 255},
		BorderColorFocused:     color.RGBA{R: 100, G: 140, B: 200, A: 255},
		TextColor:              graphics.ColorWhite,
		PlaceholderColor:       color.RGBA{R: 100, G: 100, B: 100, A: 255},
		CursorColor:            color.RGBA{R: 200, G: 200, B: 200, A: 255},
		TextSize:               14,
		Padding:                Symmetric(12, 8),
		MinWidth:               200,
		Height:                 36,
		CornerRadius:           6, // Subtle rounded corners
	}
}

// TextInput is a single-line text input field.
type TextInput struct {
	BaseWidget

	text        string
	placeholder string
	style       TextInputStyle
	focused     bool
	cursorPos   int // cursor position in runes
	onChange    func(text string)

	// Blink state
	cursorVisible bool
	lastBlink     time.Time

	// Rounded corner rendering
	gfxWindow    graphics.Window
	shapeBuilder *graphics.ShapeBuilder
	lastBounds   Rect
	lastBgColor  color.Color
}

// NewTextInput creates a new text input.
func NewTextInput() *TextInput {
	t := &TextInput{
		BaseWidget:    NewBaseWidget(),
		style:         DefaultTextInputStyle(),
		cursorVisible: true,
		lastBlink:     time.Now(),
	}
	t.focusable = true
	return t
}

func (t *TextInput) WithStyle(style TextInputStyle) *TextInput {
	t.style = style
	return t
}

func (t *TextInput) WithPlaceholder(p string) *TextInput {
	t.placeholder = p
	return t
}

func (t *TextInput) WithMinWidth(w float32) *TextInput {
	t.style.MinWidth = w
	return t
}

func (t *TextInput) OnChange(handler func(text string)) *TextInput {
	t.onChange = handler
	return t
}

func (t *TextInput) WithGraphicsWindow(w graphics.Window) *TextInput {
	t.gfxWindow = w
	return t
}

func (t *TextInput) Text() string {
	return t.text
}

func (t *TextInput) SetText(s string) {
	t.text = s
	t.cursorPos = utf8.RuneCountInString(s)
}

func (t *TextInput) IsFocused() bool {
	return t.focused
}

func (t *TextInput) SetFocused(v bool) {
	t.focused = v
	if v {
		t.cursorVisible = true
		t.lastBlink = time.Now()
	}
}

func (t *TextInput) Layout(ctx *LayoutContext, constraints Constraints) Size {
	w := t.style.MinWidth
	h := t.style.Height

	// Clamp to constraints
	w = clamp(w, constraints.MinW, constraints.MaxW)
	h = clamp(h, constraints.MinH, constraints.MaxH)

	return Size{W: w, H: h}
}

func (t *TextInput) Draw(ctx *DrawContext) {
	if !t.visible {
		return
	}

	bounds := t.Bounds()

	// Background
	bgColor := t.style.BackgroundColor
	borderColor := t.style.BorderColor
	if t.focused {
		bgColor = t.style.BackgroundColorFocused
		borderColor = t.style.BorderColorFocused
	}

	// Draw background with optional rounded corners
	if t.style.CornerRadius > 0 && t.gfxWindow != nil {
		t.drawRoundedBackground(ctx, bounds, bgColor, borderColor)
	} else {
		// Draw border (1px)
		ctx.Frame.RenderQuad(bounds.X, bounds.Y, bounds.W, bounds.H, nil, borderColor)
		// Draw inner background
		ctx.Frame.RenderQuad(bounds.X+1, bounds.Y+1, bounds.W-2, bounds.H-2, nil, bgColor)
	}

	// Text area
	textX := bounds.X + t.style.Padding.Left
	textY := bounds.Y + bounds.H/2 + float32(t.style.TextSize)/3

	if ctx.Text != nil {
		if t.text == "" && t.placeholder != "" && !t.focused {
			// Draw placeholder
			ctx.Text.RenderText(t.placeholder, textX, textY, t.style.TextSize, t.style.PlaceholderColor)
		} else {
			// Draw text
			ctx.Text.RenderText(t.text, textX, textY, t.style.TextSize, t.style.TextColor)

			// Draw cursor if focused
			if t.focused {
				// Update blink
				if time.Since(t.lastBlink) > 530*time.Millisecond {
					t.cursorVisible = !t.cursorVisible
					t.lastBlink = time.Now()
				}

				if t.cursorVisible {
					// Calculate cursor position
					textBeforeCursor := string([]rune(t.text)[:t.cursorPos])
					cursorX := textX + ctx.Text.Advance(t.style.TextSize, textBeforeCursor)
					cursorY := bounds.Y + t.style.Padding.Top
					cursorH := bounds.H - t.style.Padding.Top - t.style.Padding.Bottom

					ctx.Frame.RenderQuad(cursorX, cursorY, 2, cursorH, nil, t.style.CursorColor)
				}
			}
		}
	}
}

func (t *TextInput) drawRoundedBackground(ctx *DrawContext, bounds Rect, bgColor, borderColor color.Color) {
	// Create shape builder if needed
	if t.shapeBuilder == nil {
		segments := graphics.SegmentsForRadius(t.style.CornerRadius)
		var err error
		t.shapeBuilder, err = graphics.NewShapeBuilder(t.gfxWindow, segments)
		if err != nil {
			// Fallback to quads
			ctx.Frame.RenderQuad(bounds.X, bounds.Y, bounds.W, bounds.H, nil, borderColor)
			ctx.Frame.RenderQuad(bounds.X+1, bounds.Y+1, bounds.W-2, bounds.H-2, nil, bgColor)
			return
		}
	}

	// For text input, we draw a single rounded rect with the background color
	// The border effect comes from the slight darkness at the edges
	// For simplicity, just draw the background with rounded corners
	if bounds != t.lastBounds || bgColor != t.lastBgColor {
		style := graphics.ShapeStyle{FillColor: bgColor}
		t.shapeBuilder.UpdateRoundedRect(
			bounds.X, bounds.Y, bounds.W, bounds.H,
			graphics.UniformRadius(t.style.CornerRadius),
			style,
		)
		t.lastBounds = bounds
		t.lastBgColor = bgColor
	}

	ctx.Frame.RenderMesh(t.shapeBuilder.Mesh(), graphics.DrawOptions{})
}

func (t *TextInput) HandleEvent(ctx *EventContext, event Event) bool {
	if !t.enabled || !t.visible {
		return false
	}

	bounds := t.Bounds()

	switch e := event.(type) {
	case *MouseButtonEvent:
		if e.Button != window.ButtonLeft {
			return false
		}
		if e.Pressed {
			wasInside := bounds.Contains(e.X, e.Y)
			if wasInside && !t.focused {
				t.focused = true
				t.cursorVisible = true
				t.lastBlink = time.Now()
				// Move cursor to end
				t.cursorPos = utf8.RuneCountInString(t.text)
				return true
			} else if !wasInside && t.focused {
				t.focused = false
				return false
			}
		}

	case *KeyEvent:
		if !t.focused || !e.Pressed {
			return false
		}

		runes := []rune(t.text)

		switch e.Key {
		case window.KeyBackspace:
			if t.cursorPos > 0 {
				runes = append(runes[:t.cursorPos-1], runes[t.cursorPos:]...)
				t.text = string(runes)
				t.cursorPos--
				t.notifyChange()
			}
			return true

		case window.KeyDelete:
			if t.cursorPos < len(runes) {
				runes = append(runes[:t.cursorPos], runes[t.cursorPos+1:]...)
				t.text = string(runes)
				t.notifyChange()
			}
			return true

		case window.KeyLeft:
			if t.cursorPos > 0 {
				t.cursorPos--
			}
			return true

		case window.KeyRight:
			if t.cursorPos < len(runes) {
				t.cursorPos++
			}
			return true

		case window.KeyHome:
			t.cursorPos = 0
			return true

		case window.KeyEnd:
			t.cursorPos = len(runes)
			return true

		case window.KeyEnter:
			// Unfocus on enter
			t.focused = false
			return true

		case window.KeyEscape:
			t.focused = false
			return true
		}

	case *TextEvent:
		if !t.focused {
			return false
		}

		// Insert text at cursor
		runes := []rune(t.text)
		insertRunes := []rune(e.Text)

		// Filter out control characters
		var filtered []rune
		for _, r := range insertRunes {
			if r >= 32 && r != 127 { // Printable characters
				filtered = append(filtered, r)
			}
		}

		if len(filtered) > 0 {
			newRunes := make([]rune, 0, len(runes)+len(filtered))
			newRunes = append(newRunes, runes[:t.cursorPos]...)
			newRunes = append(newRunes, filtered...)
			newRunes = append(newRunes, runes[t.cursorPos:]...)
			t.text = string(newRunes)
			t.cursorPos += len(filtered)
			t.notifyChange()
		}
		return true
	}

	return false
}

func (t *TextInput) notifyChange() {
	if t.onChange != nil {
		t.onChange(t.text)
	}
}
