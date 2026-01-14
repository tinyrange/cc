package ui

import (
	"image/color"
)

// LabelStyle defines label appearance.
type LabelStyle struct {
	TextColor color.Color
	TextSize  float64
}

// DefaultLabelStyle returns default label styling.
func DefaultLabelStyle() LabelStyle {
	return LabelStyle{
		TextColor: color.RGBA{R: 0x44, G: 0x40, B: 0x3c, A: 255}, // ink-700 dark text
		TextSize:  14,
	}
}

// Label displays text.
type Label struct {
	BaseWidget

	text  string
	style LabelStyle

	// Cached measurements
	textWidth  float32
	textHeight float32
}

// NewLabel creates a new label.
func NewLabel(text string) *Label {
	return &Label{
		BaseWidget: NewBaseWidget(),
		text:       text,
		style:      DefaultLabelStyle(),
	}
}

// Text creates a new label (alias for NewLabel).
func Text(text string) *Label {
	return NewLabel(text)
}

func (l *Label) WithStyle(style LabelStyle) *Label {
	l.style = style
	return l
}

func (l *Label) WithColor(c color.Color) *Label {
	l.style.TextColor = c
	return l
}

func (l *Label) WithSize(size float64) *Label {
	l.style.TextSize = size
	return l
}

func (l *Label) SetText(text string) {
	l.text = text
	l.textWidth = 0
}

func (l *Label) GetText() string {
	return l.text
}

func (l *Label) Layout(ctx *LayoutContext, constraints Constraints) Size {
	if ctx.TextRenderer != nil {
		l.textWidth = ctx.TextRenderer.Advance(l.style.TextSize, l.text)
		l.textHeight = ctx.TextRenderer.LineHeight(l.style.TextSize)
	} else {
		// Rough estimate if no renderer
		l.textWidth = float32(len(l.text)) * float32(l.style.TextSize) * 0.6
		l.textHeight = float32(l.style.TextSize) * 1.2
	}

	w := clamp(l.textWidth, constraints.MinW, constraints.MaxW)
	h := clamp(l.textHeight, constraints.MinH, constraints.MaxH)

	return Size{W: w, H: h}
}

func (l *Label) Draw(ctx *DrawContext) {
	if !l.visible || ctx.Text == nil {
		return
	}
	bounds := l.Bounds()
	// Position text using proper font metrics: baseline is at Y + ascender
	ascender := ctx.Text.Ascender(l.style.TextSize)
	ctx.Text.RenderText(l.text, bounds.X, bounds.Y+ascender, l.style.TextSize, l.style.TextColor)
}

func (l *Label) HandleEvent(ctx *EventContext, event Event) bool {
	return false // Labels don't handle events
}
