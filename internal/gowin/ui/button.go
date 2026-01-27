package ui

import (
	"image/color"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/window"
)

// ButtonState tracks button interaction state.
type ButtonState int

const (
	ButtonStateNormal ButtonState = iota
	ButtonStateHovered
	ButtonStatePressed
	ButtonStateDisabled
)

// ButtonStyle defines button appearance.
type ButtonStyle struct {
	BackgroundNormal   color.Color
	BackgroundHovered  color.Color
	BackgroundPressed  color.Color
	BackgroundDisabled color.Color
	TextColor          color.Color
	TextSize           float64
	Padding            EdgeInsets
	MinWidth           float32
	MinHeight          float32
	CornerRadius       float32 // 0 = square corners

	// Gradient background (optional - overrides solid colors when set)
	GradientStops     []graphics.ColorStop
	GradientDirection graphics.GradientDirection
}

// DefaultButtonStyle returns the default button styling.
func DefaultButtonStyle() ButtonStyle {
	return ButtonStyle{
		BackgroundNormal:   color.RGBA{R: 40, G: 40, B: 40, A: 255},
		BackgroundHovered:  color.RGBA{R: 56, G: 56, B: 56, A: 255},
		BackgroundPressed:  color.RGBA{R: 72, G: 72, B: 72, A: 255},
		BackgroundDisabled: color.RGBA{R: 30, G: 30, B: 30, A: 255},
		TextColor:          graphics.ColorWhite,
		TextSize:           14,
		Padding:            Symmetric(16, 8),
		MinWidth:           60,
		MinHeight:          32,
		CornerRadius:       6, // Subtle rounded corners by default
	}
}

// Button is a clickable button widget.
type Button struct {
	BaseWidget

	text    string
	style   ButtonStyle
	state   ButtonState
	onClick func()

	// Cached layout
	textWidth float32

	// Rounded corner rendering
	gfxWindow       graphics.Window
	shapeBuilder    *graphics.ShapeBuilder
	lastBounds      Rect
	lastBgColor     color.Color
	lastUseGradient bool
}

// NewButton creates a new button.
func NewButton(text string) *Button {
	b := &Button{
		BaseWidget: NewBaseWidget(),
		text:       text,
		style:      DefaultButtonStyle(),
		state:      ButtonStateNormal,
	}
	b.focusable = true
	return b
}

func (b *Button) WithStyle(style ButtonStyle) *Button {
	b.style = style
	return b
}

func (b *Button) WithMinSize(w, h float32) *Button {
	b.style.MinWidth = w
	b.style.MinHeight = h
	return b
}

func (b *Button) WithPadding(p EdgeInsets) *Button {
	b.style.Padding = p
	return b
}

func (b *Button) OnClick(handler func()) *Button {
	b.onClick = handler
	return b
}

func (b *Button) WithGraphicsWindow(w graphics.Window) *Button {
	b.gfxWindow = w
	return b
}

func (b *Button) WithCornerRadius(radius float32) *Button {
	b.style.CornerRadius = radius
	return b
}

func (b *Button) SetText(text string) {
	b.text = text
	b.textWidth = 0 // Force recalculation
}

func (b *Button) GetText() string {
	return b.text
}

func (b *Button) Layout(ctx *LayoutContext, constraints Constraints) Size {
	// Measure text
	if ctx.TextRenderer != nil {
		b.textWidth = ctx.TextRenderer.Advance(b.style.TextSize, b.text)
	} else {
		b.textWidth = float32(len(b.text)) * float32(b.style.TextSize) * 0.6
	}

	w := b.textWidth + b.style.Padding.Left + b.style.Padding.Right
	h := float32(b.style.TextSize) + b.style.Padding.Top + b.style.Padding.Bottom

	// Apply minimum size
	if w < b.style.MinWidth {
		w = b.style.MinWidth
	}
	if h < b.style.MinHeight {
		h = b.style.MinHeight
	}

	// Clamp to constraints
	w = clamp(w, constraints.MinW, constraints.MaxW)
	h = clamp(h, constraints.MinH, constraints.MaxH)

	return Size{W: w, H: h}
}

func (b *Button) Draw(ctx *DrawContext) {
	if !b.visible {
		return
	}

	bounds := b.Bounds()

	// Check if using gradient background
	useGradient := len(b.style.GradientStops) >= 2 && b.enabled && b.state == ButtonStateNormal

	// Determine background color (fallback or for hover/pressed states)
	var bg color.Color
	if !b.enabled {
		bg = b.style.BackgroundDisabled
	} else {
		switch b.state {
		case ButtonStateHovered:
			bg = b.style.BackgroundHovered
		case ButtonStatePressed:
			bg = b.style.BackgroundPressed
		default:
			bg = b.style.BackgroundNormal
		}
	}

	// Draw background
	// Note: gradient rendering requires the ShapeBuilder, so we use rounded background even
	// with zero corner radius for gradients. Without gfxWindow we fall back to solid color.
	if b.gfxWindow != nil && (b.style.CornerRadius > 0 || useGradient) {
		b.drawRoundedBackground(ctx, bounds, bg, useGradient)
	} else {
		ctx.Frame.RenderQuad(bounds.X, bounds.Y, bounds.W, bounds.H, nil, bg)
	}

	// Draw text centered using proper font metrics
	if ctx.Text != nil {
		textX := bounds.X + (bounds.W-b.textWidth)/2
		// Vertical centering: (bounds.H - lineHeight)/2 + ascender
		lineHeight := ctx.Text.LineHeight(b.style.TextSize)
		ascender := ctx.Text.Ascender(b.style.TextSize)
		textY := bounds.Y + (bounds.H-lineHeight)/2 + ascender
		ctx.Text.RenderText(b.text, textX, textY, b.style.TextSize, b.style.TextColor)
	}
}

func (b *Button) drawRoundedBackground(ctx *DrawContext, bounds Rect, bg color.Color, useGradient bool) {
	// Create shape builder if needed
	if b.shapeBuilder == nil {
		// Use appropriate segments for corner radius (minimum 8 for smooth corners)
		radius := b.style.CornerRadius
		if radius < 1 {
			radius = 1 // Minimum radius for shape builder
		}
		segments := graphics.SegmentsForRadius(radius)
		var err error
		b.shapeBuilder, err = graphics.NewShapeBuilder(b.gfxWindow, segments)
		if err != nil {
			// Fallback to solid quad (gradient not supported without shape builder)
			ctx.Frame.RenderQuad(bounds.X, bounds.Y, bounds.W, bounds.H, nil, bg)
			return
		}
	}

	// Update geometry if bounds, color, or gradient state changed
	if bounds != b.lastBounds || bg != b.lastBgColor || useGradient != b.lastUseGradient {
		var style graphics.ShapeStyle
		if useGradient {
			// For gradient-only buttons, use first gradient stop as fallback fill
			fillColor := bg
			if len(b.style.GradientStops) > 0 {
				fillColor = b.style.GradientStops[0].Color
			}
			style = graphics.ShapeStyle{
				FillColor:         fillColor,
				GradientStops:     b.style.GradientStops,
				GradientDirection: b.style.GradientDirection,
			}
		} else {
			style = graphics.ShapeStyle{FillColor: bg}
		}
		b.shapeBuilder.UpdateRoundedRect(
			bounds.X, bounds.Y, bounds.W, bounds.H,
			graphics.UniformRadius(b.style.CornerRadius),
			style,
		)
		b.lastBounds = bounds
		b.lastBgColor = bg
		b.lastUseGradient = useGradient
	}

	ctx.Frame.RenderMesh(b.shapeBuilder.Mesh(), graphics.DrawOptions{})
}

func (b *Button) HandleEvent(ctx *EventContext, event Event) bool {
	if !b.enabled || !b.visible {
		return false
	}

	bounds := b.Bounds()

	switch e := event.(type) {
	case *MouseMoveEvent:
		isHovered := bounds.Contains(e.X, e.Y)
		if isHovered {
			if b.state != ButtonStatePressed {
				b.state = ButtonStateHovered
			}
		} else if b.state == ButtonStateHovered {
			b.state = ButtonStateNormal
		}
		return false // Don't consume move events

	case *MouseButtonEvent:
		if e.Button != window.ButtonLeft {
			return false
		}
		isInside := bounds.Contains(e.X, e.Y)
		if e.Pressed && isInside {
			b.state = ButtonStatePressed
			return true
		} else if !e.Pressed && b.state == ButtonStatePressed {
			b.state = ButtonStateNormal
			if isInside {
				if b.onClick != nil {
					b.onClick()
				}
				return true
			}
			return false
		}
	}

	return false
}
