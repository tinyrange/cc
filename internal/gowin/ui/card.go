package ui

import (
	"image/color"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/window"
)

// CardStyle defines card appearance.
type CardStyle struct {
	BackgroundColor color.Color
	BorderColor     color.Color
	BorderWidth     float32
	Padding         EdgeInsets
	CornerRadius    float32 // 0 = square corners
}

// DefaultCardStyle returns default card styling.
func DefaultCardStyle() CardStyle {
	return CardStyle{
		BackgroundColor: color.RGBA{R: 20, G: 20, B: 20, A: 220},
		BorderColor:     color.RGBA{R: 80, G: 80, B: 80, A: 255},
		BorderWidth:     1,
		Padding:         All(12),
	}
}

// Card is a container with visual styling (background, border).
type Card struct {
	BaseWidget

	content Widget
	style   CardStyle

	// Hover state for interactive cards
	hovered    bool
	onClick    func()
	hoverStyle *CardStyle

	// Fixed size (0 = auto)
	fixedWidth  float32
	fixedHeight float32

	// Rounded corner rendering
	gfxWindow    graphics.Window
	shapeBuilder *graphics.ShapeBuilder
	lastBounds   Rect // Track bounds changes for mesh updates
}

// NewCard creates a new card.
func NewCard(content Widget) *Card {
	return &Card{
		BaseWidget: NewBaseWidget(),
		content:    content,
		style:      DefaultCardStyle(),
	}
}

func (c *Card) WithStyle(style CardStyle) *Card {
	c.style = style
	return c
}

func (c *Card) WithHoverStyle(style CardStyle) *Card {
	c.hoverStyle = &style
	return c
}

func (c *Card) WithFixedSize(w, h float32) *Card {
	c.fixedWidth = w
	c.fixedHeight = h
	return c
}

func (c *Card) WithPadding(p EdgeInsets) *Card {
	c.style.Padding = p
	return c
}

func (c *Card) WithBackground(col color.Color) *Card {
	c.style.BackgroundColor = col
	return c
}

func (c *Card) WithBorder(col color.Color, width float32) *Card {
	c.style.BorderColor = col
	c.style.BorderWidth = width
	return c
}

func (c *Card) WithCornerRadius(radius float32) *Card {
	c.style.CornerRadius = radius
	return c
}

func (c *Card) WithGraphicsWindow(w graphics.Window) *Card {
	c.gfxWindow = w
	return c
}

func (c *Card) OnClick(handler func()) *Card {
	c.onClick = handler
	c.focusable = true
	return c
}

func (c *Card) SetContent(content Widget) {
	c.content = content
}

func (c *Card) IsHovered() bool {
	return c.hovered
}

func (c *Card) Layout(ctx *LayoutContext, constraints Constraints) Size {
	p := c.style.Padding

	// Handle fixed size
	if c.fixedWidth > 0 && c.fixedHeight > 0 {
		// Layout content with fixed inner size
		if c.content != nil {
			contentConstraints := Constraints{
				MinW: c.fixedWidth - p.Horizontal(),
				MaxW: c.fixedWidth - p.Horizontal(),
				MinH: c.fixedHeight - p.Vertical(),
				MaxH: c.fixedHeight - p.Vertical(),
			}
			c.content.Layout(ctx, contentConstraints)
		}
		return Size{W: c.fixedWidth, H: c.fixedHeight}
	}

	// Subtract padding from constraints for content
	contentConstraints := Constraints{
		MinW: max(0, constraints.MinW-p.Horizontal()),
		MaxW: max(0, constraints.MaxW-p.Horizontal()),
		MinH: max(0, constraints.MinH-p.Vertical()),
		MaxH: max(0, constraints.MaxH-p.Vertical()),
	}

	var contentSize Size
	if c.content != nil {
		contentSize = c.content.Layout(ctx, contentConstraints)
	}

	w := contentSize.W + p.Horizontal()
	h := contentSize.H + p.Vertical()

	// Apply fixed dimensions if specified
	if c.fixedWidth > 0 {
		w = c.fixedWidth
	}
	if c.fixedHeight > 0 {
		h = c.fixedHeight
	}

	return Size{W: w, H: h}
}

func (c *Card) SetBounds(bounds Rect) {
	c.BaseWidget.SetBounds(bounds)

	if c.content != nil {
		p := c.style.Padding
		c.content.SetBounds(bounds.Inset(p.Left, p.Top, p.Right, p.Bottom))
	}
}

func (c *Card) Draw(ctx *DrawContext) {
	if !c.visible {
		return
	}

	bounds := c.Bounds()
	style := c.style
	if c.hovered && c.hoverStyle != nil {
		style = *c.hoverStyle
	}

	// Background
	if style.BackgroundColor != nil {
		if style.CornerRadius > 0 && c.gfxWindow != nil {
			// Use rounded rectangle rendering
			c.drawRoundedBackground(ctx, bounds, style)
		} else {
			// Fallback to simple quad
			ctx.Frame.RenderQuad(bounds.X, bounds.Y, bounds.W, bounds.H, nil, style.BackgroundColor)
		}
	}

	// Border (only for non-rounded cards)
	if style.CornerRadius == 0 && style.BorderWidth > 0 && style.BorderColor != nil {
		bw := style.BorderWidth
		bc := style.BorderColor
		// Top
		ctx.Frame.RenderQuad(bounds.X, bounds.Y, bounds.W, bw, nil, bc)
		// Bottom
		ctx.Frame.RenderQuad(bounds.X, bounds.Y+bounds.H-bw, bounds.W, bw, nil, bc)
		// Left
		ctx.Frame.RenderQuad(bounds.X, bounds.Y, bw, bounds.H, nil, bc)
		// Right
		ctx.Frame.RenderQuad(bounds.X+bounds.W-bw, bounds.Y, bw, bounds.H, nil, bc)
	}

	// Content
	if c.content != nil {
		c.content.Draw(ctx)
	}
}

func (c *Card) drawRoundedBackground(ctx *DrawContext, bounds Rect, style CardStyle) {
	// Create shape builder if needed
	if c.shapeBuilder == nil {
		segments := graphics.SegmentsForRadius(style.CornerRadius)
		var err error
		c.shapeBuilder, err = graphics.NewShapeBuilder(c.gfxWindow, segments)
		if err != nil {
			// Fallback to quad
			ctx.Frame.RenderQuad(bounds.X, bounds.Y, bounds.W, bounds.H, nil, style.BackgroundColor)
			return
		}
	}

	// Update geometry if bounds changed
	if bounds != c.lastBounds {
		shapeStyle := graphics.ShapeStyle{
			FillColor: style.BackgroundColor,
		}
		c.shapeBuilder.UpdateRoundedRect(
			bounds.X, bounds.Y, bounds.W, bounds.H,
			graphics.UniformRadius(style.CornerRadius),
			shapeStyle,
		)
		c.lastBounds = bounds
	}

	// Render the mesh
	ctx.Frame.RenderMesh(c.shapeBuilder.Mesh(), graphics.DrawOptions{})
}

func (c *Card) HandleEvent(ctx *EventContext, event Event) bool {
	bounds := c.Bounds()

	switch e := event.(type) {
	case *MouseMoveEvent:
		c.hovered = bounds.Contains(e.X, e.Y)

	case *MouseButtonEvent:
		if e.Button == window.ButtonLeft && e.Pressed && bounds.Contains(e.X, e.Y) {
			if c.onClick != nil {
				c.onClick()
				return true
			}
		}
	}

	// Dispatch to content
	if c.content != nil {
		return c.content.HandleEvent(ctx, event)
	}
	return false
}

func (c *Card) Children() []Widget {
	if c.content != nil {
		return []Widget{c.content}
	}
	return nil
}
