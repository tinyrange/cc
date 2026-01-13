package ui

import (
	"image/color"
	"time"
	"unicode/utf8"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/text"
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
	SelectionColor         color.Color // Background for selected text
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
		SelectionColor:         color.RGBA{R: 65, G: 89, B: 139, A: 255}, // Tokyo Night selection blue
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

	// Selection state
	selectionStart int  // start of selection in runes (-1 = no selection)
	selectionEnd   int  // end of selection in runes
	selecting      bool // currently dragging to select

	// Text measurement cache for click positioning
	textRenderer *text.Renderer

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
		BaseWidget:     NewBaseWidget(),
		style:          DefaultTextInputStyle(),
		cursorVisible:  true,
		lastBlink:      time.Now(),
		selectionStart: -1,
		selectionEnd:   -1,
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

	// Text area - use proper font metrics for vertical centering
	textX := bounds.X + t.style.Padding.Left
	var textY float32
	if ctx.Text != nil {
		lineHeight := ctx.Text.LineHeight(t.style.TextSize)
		ascender := ctx.Text.Ascender(t.style.TextSize)
		textY = bounds.Y + (bounds.H-lineHeight)/2 + ascender
	} else {
		textY = bounds.Y + bounds.H/2 + float32(t.style.TextSize)/3
	}

	// Cache text renderer for click positioning
	t.textRenderer = ctx.Text

	if ctx.Text != nil {
		if t.text == "" && t.placeholder != "" && !t.focused {
			// Draw placeholder
			ctx.Text.RenderText(t.placeholder, textX, textY, t.style.TextSize, t.style.PlaceholderColor)
		} else {
			// Draw selection highlight if there is a selection
			if t.hasSelection() {
				selStart, selEnd := t.normalizedSelection()
				runes := []rune(t.text)

				textBefore := string(runes[:selStart])
				textSelected := string(runes[selStart:selEnd])

				selStartX := textX + ctx.Text.Advance(t.style.TextSize, textBefore)
				selWidth := ctx.Text.Advance(t.style.TextSize, textSelected)
				selY := bounds.Y + t.style.Padding.Top
				selH := bounds.H - t.style.Padding.Top - t.style.Padding.Bottom

				ctx.Frame.RenderQuad(selStartX, selY, selWidth, selH, nil, t.style.SelectionColor)
			}

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
			if wasInside {
				t.focused = true
				t.cursorVisible = true
				t.lastBlink = time.Now()

				// Calculate cursor position based on click location
				clickPos := t.positionFromX(e.X, bounds)
				t.cursorPos = clickPos
				t.selecting = true
				t.selectionStart = clickPos
				t.selectionEnd = clickPos
				return true
			} else if t.focused {
				t.focused = false
				t.clearSelection()
				return false
			}
		} else {
			// Mouse up - stop selecting
			if t.selecting {
				t.selecting = false
				// If selection is empty (start == end), clear it
				if t.selectionStart == t.selectionEnd {
					t.clearSelection()
				}
			}
		}

	case *MouseMoveEvent:
		// Handle drag selection
		if t.selecting && t.focused {
			bounds := t.Bounds()
			if t.textRenderer != nil {
				clickPos := t.positionFromX(e.X, bounds)
				t.selectionEnd = clickPos
				t.cursorPos = clickPos
			}
			return true
		}

	case *KeyEvent:
		if !t.focused || !e.Pressed {
			return false
		}

		runes := []rune(t.text)
		hasShift := (e.Mods & window.ModShift) != 0
		hasCtrl := (e.Mods & window.ModCtrl) != 0
		hasSuper := (e.Mods & window.ModSuper) != 0
		hasMod := hasCtrl || hasSuper // Ctrl on Windows/Linux, Cmd on macOS

		switch e.Key {
		case window.KeyA:
			if hasMod {
				// Select all
				t.selectionStart = 0
				t.selectionEnd = len(runes)
				t.cursorPos = len(runes)
				return true
			}

		case window.KeyC:
			if hasMod && t.hasSelection() {
				// Copy
				t.copySelection()
				return true
			}

		case window.KeyX:
			if hasMod && t.hasSelection() {
				// Cut
				t.copySelection()
				t.deleteSelection()
				return true
			}

		case window.KeyV:
			if hasMod {
				// Paste
				t.paste()
				return true
			}

		case window.KeyBackspace:
			if t.hasSelection() {
				t.deleteSelection()
			} else if t.cursorPos > 0 {
				runes = append(runes[:t.cursorPos-1], runes[t.cursorPos:]...)
				t.text = string(runes)
				t.cursorPos--
				t.notifyChange()
			}
			return true

		case window.KeyDelete:
			if t.hasSelection() {
				t.deleteSelection()
			} else if t.cursorPos < len(runes) {
				runes = append(runes[:t.cursorPos], runes[t.cursorPos+1:]...)
				t.text = string(runes)
				t.notifyChange()
			}
			return true

		case window.KeyLeft:
			if hasShift {
				// Extend selection
				if t.selectionStart < 0 {
					t.selectionStart = t.cursorPos
					t.selectionEnd = t.cursorPos
				}
				if t.cursorPos > 0 {
					t.cursorPos--
					t.selectionEnd = t.cursorPos
				}
			} else {
				if t.hasSelection() {
					// Move to start of selection
					selStart, _ := t.normalizedSelection()
					t.cursorPos = selStart
					t.clearSelection()
				} else if t.cursorPos > 0 {
					t.cursorPos--
				}
			}
			return true

		case window.KeyRight:
			if hasShift {
				// Extend selection
				if t.selectionStart < 0 {
					t.selectionStart = t.cursorPos
					t.selectionEnd = t.cursorPos
				}
				if t.cursorPos < len(runes) {
					t.cursorPos++
					t.selectionEnd = t.cursorPos
				}
			} else {
				if t.hasSelection() {
					// Move to end of selection
					_, selEnd := t.normalizedSelection()
					t.cursorPos = selEnd
					t.clearSelection()
				} else if t.cursorPos < len(runes) {
					t.cursorPos++
				}
			}
			return true

		case window.KeyHome:
			if hasShift {
				if t.selectionStart < 0 {
					t.selectionStart = t.cursorPos
					t.selectionEnd = t.cursorPos
				}
				t.cursorPos = 0
				t.selectionEnd = 0
			} else {
				t.cursorPos = 0
				t.clearSelection()
			}
			return true

		case window.KeyEnd:
			if hasShift {
				if t.selectionStart < 0 {
					t.selectionStart = t.cursorPos
					t.selectionEnd = t.cursorPos
				}
				t.cursorPos = len(runes)
				t.selectionEnd = len(runes)
			} else {
				t.cursorPos = len(runes)
				t.clearSelection()
			}
			return true

		case window.KeyEnter:
			// Unfocus on enter
			t.focused = false
			t.clearSelection()
			return true

		case window.KeyEscape:
			t.focused = false
			t.clearSelection()
			return true
		}

	case *TextEvent:
		if !t.focused {
			return false
		}

		// Delete selection if there is one
		if t.hasSelection() {
			t.deleteSelection()
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

// Selection helpers

func (t *TextInput) hasSelection() bool {
	return t.selectionStart >= 0 && t.selectionStart != t.selectionEnd
}

func (t *TextInput) normalizedSelection() (start, end int) {
	if t.selectionStart < t.selectionEnd {
		return t.selectionStart, t.selectionEnd
	}
	return t.selectionEnd, t.selectionStart
}

func (t *TextInput) clearSelection() {
	t.selectionStart = -1
	t.selectionEnd = -1
	t.selecting = false
}

func (t *TextInput) selectedText() string {
	if !t.hasSelection() {
		return ""
	}
	start, end := t.normalizedSelection()
	runes := []rune(t.text)
	if start >= len(runes) || end > len(runes) {
		return ""
	}
	return string(runes[start:end])
}

func (t *TextInput) deleteSelection() {
	if !t.hasSelection() {
		return
	}
	start, end := t.normalizedSelection()
	runes := []rune(t.text)
	if start >= len(runes) || end > len(runes) {
		t.clearSelection()
		return
	}
	newRunes := append(runes[:start], runes[end:]...)
	t.text = string(newRunes)
	t.cursorPos = start
	t.clearSelection()
	t.notifyChange()
}

func (t *TextInput) copySelection() {
	text := t.selectedText()
	if text == "" {
		return
	}
	clipboard := window.GetClipboard()
	if clipboard != nil {
		clipboard.SetText(text)
	}
}

func (t *TextInput) paste() {
	clipboard := window.GetClipboard()
	if clipboard == nil {
		return
	}
	text := clipboard.GetText()
	if text == "" {
		return
	}

	// Delete selection if there is one
	if t.hasSelection() {
		t.deleteSelection()
	}

	// Filter out control characters and newlines
	var filtered []rune
	for _, r := range text {
		if r >= 32 && r != 127 && r != '\n' && r != '\r' {
			filtered = append(filtered, r)
		}
	}

	if len(filtered) > 0 {
		runes := []rune(t.text)
		newRunes := make([]rune, 0, len(runes)+len(filtered))
		newRunes = append(newRunes, runes[:t.cursorPos]...)
		newRunes = append(newRunes, filtered...)
		newRunes = append(newRunes, runes[t.cursorPos:]...)
		t.text = string(newRunes)
		t.cursorPos += len(filtered)
		t.notifyChange()
	}
}

// positionFromX calculates the character position from an X coordinate
func (t *TextInput) positionFromX(x float32, bounds Rect) int {
	if t.textRenderer == nil {
		return utf8.RuneCountInString(t.text)
	}

	textX := bounds.X + t.style.Padding.Left
	relativeX := x - textX

	if relativeX <= 0 {
		return 0
	}

	runes := []rune(t.text)
	if len(runes) == 0 {
		return 0
	}

	// Binary search for the closest character position
	for i := 0; i <= len(runes); i++ {
		textUpTo := string(runes[:i])
		advance := t.textRenderer.Advance(t.style.TextSize, textUpTo)
		if advance >= relativeX {
			// Check if we're closer to this position or the previous one
			if i > 0 {
				prevAdvance := t.textRenderer.Advance(t.style.TextSize, string(runes[:i-1]))
				if relativeX-prevAdvance < advance-relativeX {
					return i - 1
				}
			}
			return i
		}
	}

	return len(runes)
}
