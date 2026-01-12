package ui

import (
	"github.com/tinyrange/cc/internal/gowin/graphics"
)

// GradientLabel displays text with a gradient color.
type GradientLabel struct {
	BaseWidget

	text     string
	textSize float64
	stops    []graphics.ColorStop

	// Cached measurements
	textWidth  float32
	textHeight float32
}

// NewGradientLabel creates a new gradient text label.
func NewGradientLabel(text string) *GradientLabel {
	return &GradientLabel{
		BaseWidget: NewBaseWidget(),
		text:       text,
		textSize:   24, // Default to larger size for titles
	}
}

// WithSize sets the text size.
func (l *GradientLabel) WithSize(size float64) *GradientLabel {
	l.textSize = size
	return l
}

// WithGradient sets the gradient stops.
func (l *GradientLabel) WithGradient(stops []graphics.ColorStop) *GradientLabel {
	l.stops = stops
	return l
}

// SetText updates the text content.
func (l *GradientLabel) SetText(text string) {
	l.text = text
	l.textWidth = 0
}

// GetText returns the current text.
func (l *GradientLabel) GetText() string {
	return l.text
}

func (l *GradientLabel) Layout(ctx *LayoutContext, constraints Constraints) Size {
	if ctx.TextRenderer != nil {
		l.textWidth = ctx.TextRenderer.Advance(l.textSize, l.text)
		l.textHeight = ctx.TextRenderer.LineHeight(l.textSize)
	} else {
		// Rough estimate if no renderer
		l.textWidth = float32(len(l.text)) * float32(l.textSize) * 0.6
		l.textHeight = float32(l.textSize) * 1.2
	}

	w := clamp(l.textWidth, constraints.MinW, constraints.MaxW)
	h := clamp(l.textHeight, constraints.MinH, constraints.MaxH)

	return Size{W: w, H: h}
}

func (l *GradientLabel) Draw(ctx *DrawContext) {
	if !l.visible || ctx.Text == nil || len(l.stops) < 2 {
		return
	}

	bounds := l.Bounds()
	// Position text using proper font metrics: baseline is at Y + ascender
	ascender := ctx.Text.Ascender(l.textSize)
	ctx.Text.RenderGradientText(l.text, bounds.X, bounds.Y+ascender, l.textSize, l.stops)
}

func (l *GradientLabel) HandleEvent(ctx *EventContext, event Event) bool {
	return false // Labels don't handle events
}
