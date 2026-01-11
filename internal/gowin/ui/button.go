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
	gfxWindow    graphics.Window
	shapeBuilder *graphics.ShapeBuilder
	lastBounds   Rect
	lastBgColor  color.Color
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

	// Determine background color
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
	if b.style.CornerRadius > 0 && b.gfxWindow != nil {
		b.drawRoundedBackground(ctx, bounds, bg)
	} else {
		ctx.Frame.RenderQuad(bounds.X, bounds.Y, bounds.W, bounds.H, nil, bg)
	}

	// Draw text centered
	if ctx.Text != nil {
		textX := bounds.X + (bounds.W-b.textWidth)/2
		textY := bounds.Y + bounds.H/2 + float32(b.style.TextSize)/3
		ctx.Text.RenderText(b.text, textX, textY, b.style.TextSize, b.style.TextColor)
	}
}

func (b *Button) drawRoundedBackground(ctx *DrawContext, bounds Rect, bg color.Color) {
	// Create shape builder if needed
	if b.shapeBuilder == nil {
		segments := graphics.SegmentsForRadius(b.style.CornerRadius)
		var err error
		b.shapeBuilder, err = graphics.NewShapeBuilder(b.gfxWindow, segments)
		if err != nil {
			ctx.Frame.RenderQuad(bounds.X, bounds.Y, bounds.W, bounds.H, nil, bg)
			return
		}
	}

	// Update geometry if bounds or color changed
	if bounds != b.lastBounds || bg != b.lastBgColor {
		style := graphics.ShapeStyle{FillColor: bg}
		b.shapeBuilder.UpdateRoundedRect(
			bounds.X, bounds.Y, bounds.W, bounds.H,
			graphics.UniformRadius(b.style.CornerRadius),
			style,
		)
		b.lastBounds = bounds
		b.lastBgColor = bg
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
