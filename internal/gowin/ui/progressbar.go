package ui

import (
	"fmt"
	"image/color"

	"github.com/tinyrange/cc/internal/gowin/graphics"
)

// ProgressBarStyle defines the appearance of a progress bar.
type ProgressBarStyle struct {
	BackgroundColor color.Color
	FillColor       color.Color
	TextColor       color.Color
	Height          float32
	CornerRadius    float32
	ShowPercentage  bool
	TextSize        float64
}

// DefaultProgressBarStyle returns a Tokyo Night themed progress bar style.
func DefaultProgressBarStyle() ProgressBarStyle {
	return ProgressBarStyle{
		BackgroundColor: color.RGBA{R: 0x24, G: 0x28, B: 0x3b, A: 255}, // Tokyo Night dark
		FillColor:       color.RGBA{R: 0x7a, G: 0xa2, B: 0xf7, A: 255}, // Tokyo Night blue
		TextColor:       color.RGBA{R: 0xc0, G: 0xca, B: 0xf5, A: 255}, // Tokyo Night text
		Height:          8,
		CornerRadius:    4,
		ShowPercentage:  false,
		TextSize:        12,
	}
}

// ProgressBar displays a visual progress indicator.
type ProgressBar struct {
	BaseWidget

	value    float64 // 0.0 to 1.0
	label    string
	style    ProgressBarStyle
	minWidth float32

	// Rounded corner rendering
	gfxWindow          graphics.Window
	bgShapeBuilder     *graphics.ShapeBuilder
	fillShapeBuilder   *graphics.ShapeBuilder
	lastBounds         Rect
	lastValue          float64
}

// NewProgressBar creates a new progress bar.
func NewProgressBar() *ProgressBar {
	return &ProgressBar{
		BaseWidget: NewBaseWidget(),
		style:      DefaultProgressBarStyle(),
		minWidth:   200,
		value:      0,
	}
}

func (p *ProgressBar) WithStyle(style ProgressBarStyle) *ProgressBar {
	p.style = style
	return p
}

func (p *ProgressBar) WithMinWidth(w float32) *ProgressBar {
	p.minWidth = w
	return p
}

func (p *ProgressBar) WithLabel(label string) *ProgressBar {
	p.label = label
	return p
}

func (p *ProgressBar) WithGraphicsWindow(w graphics.Window) *ProgressBar {
	p.gfxWindow = w
	return p
}

// SetValue sets the progress value (0.0 to 1.0).
func (p *ProgressBar) SetValue(v float64) {
	if v < 0 {
		v = 0
	}
	if v > 1 {
		v = 1
	}
	p.value = v
}

// Value returns the current progress value.
func (p *ProgressBar) Value() float64 {
	return p.value
}

// SetLabel sets the label text displayed above the progress bar.
func (p *ProgressBar) SetLabel(label string) {
	p.label = label
}

func (p *ProgressBar) Layout(ctx *LayoutContext, constraints Constraints) Size {
	w := p.minWidth
	h := p.style.Height

	// Add space for label if present
	if p.label != "" {
		h += float32(p.style.TextSize) + 4
	}

	// Add space for percentage text if shown
	if p.style.ShowPercentage {
		h += float32(p.style.TextSize) + 4
	}

	w = clamp(w, constraints.MinW, constraints.MaxW)
	h = clamp(h, constraints.MinH, constraints.MaxH)

	return Size{W: w, H: h}
}

func (p *ProgressBar) Draw(ctx *DrawContext) {
	if !p.visible {
		return
	}

	bounds := p.Bounds()
	barY := bounds.Y
	barH := p.style.Height

	// Draw label if present using proper font metrics
	if p.label != "" && ctx.Text != nil {
		ascender := ctx.Text.Ascender(p.style.TextSize)
		textY := bounds.Y + ascender
		ctx.Text.RenderText(p.label, bounds.X, textY, p.style.TextSize, p.style.TextColor)
		barY += float32(p.style.TextSize) + 4
	}

	// Draw background bar
	if p.style.CornerRadius > 0 && p.gfxWindow != nil {
		p.drawRoundedBar(ctx, bounds.X, barY, bounds.W, barH)
	} else {
		// Background
		ctx.Frame.RenderQuad(bounds.X, barY, bounds.W, barH, nil, p.style.BackgroundColor)
		// Fill
		fillW := bounds.W * float32(p.value)
		if fillW > 0 {
			ctx.Frame.RenderQuad(bounds.X, barY, fillW, barH, nil, p.style.FillColor)
		}
	}

	// Draw percentage if enabled using proper font metrics
	if p.style.ShowPercentage && ctx.Text != nil {
		ascender := ctx.Text.Ascender(p.style.TextSize)
		percentY := barY + barH + 4 + ascender
		percentText := formatPercent(p.value)
		ctx.Text.RenderText(percentText, bounds.X, percentY, p.style.TextSize, p.style.TextColor)
	}
}

func (p *ProgressBar) drawRoundedBar(ctx *DrawContext, x, y, w, h float32) {
	// Create background shape builder if needed
	if p.bgShapeBuilder == nil {
		segments := graphics.SegmentsForRadius(p.style.CornerRadius)
		var err error
		p.bgShapeBuilder, err = graphics.NewShapeBuilder(p.gfxWindow, segments)
		if err != nil {
			// Fallback to quads
			ctx.Frame.RenderQuad(x, y, w, h, nil, p.style.BackgroundColor)
			fillW := w * float32(p.value)
			if fillW > 0 {
				ctx.Frame.RenderQuad(x, y, fillW, h, nil, p.style.FillColor)
			}
			return
		}
	}

	// Create fill shape builder if needed
	if p.fillShapeBuilder == nil {
		segments := graphics.SegmentsForRadius(p.style.CornerRadius)
		var err error
		p.fillShapeBuilder, err = graphics.NewShapeBuilder(p.gfxWindow, segments)
		if err != nil {
			p.fillShapeBuilder = nil
		}
	}

	bounds := Rect{X: x, Y: y, W: w, H: h}

	// Always update background
	bgStyle := graphics.ShapeStyle{FillColor: p.style.BackgroundColor}
	p.bgShapeBuilder.UpdateRoundedRect(x, y, w, h, graphics.UniformRadius(p.style.CornerRadius), bgStyle)
	ctx.Frame.RenderMesh(p.bgShapeBuilder.Mesh(), graphics.DrawOptions{})

	// Update and render fill if value > 0
	if p.value > 0 && p.fillShapeBuilder != nil {
		fillW := w * float32(p.value)
		if fillW > p.style.CornerRadius*2 { // Only draw if wide enough
			fillStyle := graphics.ShapeStyle{FillColor: p.style.FillColor}
			p.fillShapeBuilder.UpdateRoundedRect(x, y, fillW, h, graphics.UniformRadius(p.style.CornerRadius), fillStyle)
			ctx.Frame.RenderMesh(p.fillShapeBuilder.Mesh(), graphics.DrawOptions{})
		} else if fillW > 0 {
			// For small values, draw a simple rect
			ctx.Frame.RenderQuad(x, y, fillW, h, nil, p.style.FillColor)
		}
	}

	p.lastBounds = bounds
	p.lastValue = p.value
}

func (p *ProgressBar) HandleEvent(ctx *EventContext, event Event) bool {
	return false
}

func formatPercent(v float64) string {
	percent := int(v * 100)
	if percent > 100 {
		percent = 100
	}
	if percent < 0 {
		percent = 0
	}
	return fmt.Sprintf("%d%%", percent)
}
