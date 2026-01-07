package ui

import "image/color"

// Spacer is an empty widget that takes up space in layouts.
type Spacer struct {
	BaseWidget

	// Fixed size (0 = flexible)
	fixedWidth  float32
	fixedHeight float32
}

// NewSpacer creates a flexible spacer that expands to fill available space.
func NewSpacer() *Spacer {
	return &Spacer{
		BaseWidget: NewBaseWidget(),
	}
}

// FixedSpacer creates a spacer with fixed dimensions.
func FixedSpacer(w, h float32) *Spacer {
	return &Spacer{
		BaseWidget:  NewBaseWidget(),
		fixedWidth:  w,
		fixedHeight: h,
	}
}

func (s *Spacer) WithSize(w, h float32) *Spacer {
	s.fixedWidth = w
	s.fixedHeight = h
	return s
}

func (s *Spacer) Layout(ctx *LayoutContext, constraints Constraints) Size {
	w := s.fixedWidth
	h := s.fixedHeight

	// Only expand to fill available space if constraints are bounded.
	// Unbounded constraints (1e9) indicate the parent is measuring intrinsic size,
	// so we should return 0 to not artificially inflate the layout.
	const unboundedThreshold = 1e8

	if w == 0 && constraints.MaxW < unboundedThreshold {
		w = constraints.MaxW
	}
	if h == 0 && constraints.MaxH < unboundedThreshold {
		h = constraints.MaxH
	}

	w = clamp(w, constraints.MinW, constraints.MaxW)
	h = clamp(h, constraints.MinH, constraints.MaxH)

	return Size{W: w, H: h}
}

func (s *Spacer) Draw(ctx *DrawContext) {
	// Spacers are invisible
}

func (s *Spacer) HandleEvent(ctx *EventContext, event Event) bool {
	return false
}

// Box is a simple colored rectangle widget.
type Box struct {
	BaseWidget

	color       color.Color
	fixedWidth  float32
	fixedHeight float32
}

// NewBox creates a colored box widget.
func NewBox(c color.Color) *Box {
	return &Box{
		BaseWidget: NewBaseWidget(),
		color:      c,
	}
}

func (b *Box) WithSize(w, h float32) *Box {
	b.fixedWidth = w
	b.fixedHeight = h
	return b
}

func (b *Box) WithColor(c color.Color) *Box {
	b.color = c
	return b
}

func (b *Box) Layout(ctx *LayoutContext, constraints Constraints) Size {
	w := b.fixedWidth
	h := b.fixedHeight

	// Only expand to fill available space if constraints are bounded.
	const unboundedThreshold = 1e8

	if w == 0 && constraints.MaxW < unboundedThreshold {
		w = constraints.MaxW
	}
	if h == 0 && constraints.MaxH < unboundedThreshold {
		h = constraints.MaxH
	}

	w = clamp(w, constraints.MinW, constraints.MaxW)
	h = clamp(h, constraints.MinH, constraints.MaxH)

	return Size{W: w, H: h}
}

func (b *Box) Draw(ctx *DrawContext) {
	if !b.visible || b.color == nil {
		return
	}
	bounds := b.Bounds()
	ctx.Frame.RenderQuad(bounds.X, bounds.Y, bounds.W, bounds.H, nil, b.color)
}

func (b *Box) HandleEvent(ctx *EventContext, event Event) bool {
	return false
}

// Padding wraps a widget with padding.
type Padding struct {
	BaseWidget

	content Widget
	padding EdgeInsets
}

// NewPadding creates a padding wrapper.
func NewPadding(content Widget, padding EdgeInsets) *Padding {
	return &Padding{
		BaseWidget: NewBaseWidget(),
		content:    content,
		padding:    padding,
	}
}

func (p *Padding) Layout(ctx *LayoutContext, constraints Constraints) Size {
	contentConstraints := Constraints{
		MinW: max(0, constraints.MinW-p.padding.Horizontal()),
		MaxW: max(0, constraints.MaxW-p.padding.Horizontal()),
		MinH: max(0, constraints.MinH-p.padding.Vertical()),
		MaxH: max(0, constraints.MaxH-p.padding.Vertical()),
	}

	var contentSize Size
	if p.content != nil {
		contentSize = p.content.Layout(ctx, contentConstraints)
	}

	return Size{
		W: contentSize.W + p.padding.Horizontal(),
		H: contentSize.H + p.padding.Vertical(),
	}
}

func (p *Padding) SetBounds(bounds Rect) {
	p.BaseWidget.SetBounds(bounds)
	if p.content != nil {
		p.content.SetBounds(bounds.Inset(p.padding.Left, p.padding.Top, p.padding.Right, p.padding.Bottom))
	}
}

func (p *Padding) Draw(ctx *DrawContext) {
	if !p.visible {
		return
	}
	if p.content != nil {
		p.content.Draw(ctx)
	}
}

func (p *Padding) HandleEvent(ctx *EventContext, event Event) bool {
	if p.content != nil {
		return p.content.HandleEvent(ctx, event)
	}
	return false
}

func (p *Padding) Children() []Widget {
	if p.content != nil {
		return []Widget{p.content}
	}
	return nil
}

// Center wraps a widget and centers it within available space.
type Center struct {
	BaseWidget

	content Widget

	// Cached content size from Layout
	contentSize Size
}

// NewCenter creates a centering wrapper.
func NewCenter(content Widget) *Center {
	return &Center{
		BaseWidget: NewBaseWidget(),
		content:    content,
	}
}

func (c *Center) Layout(ctx *LayoutContext, constraints Constraints) Size {
	if c.content != nil {
		c.contentSize = c.content.Layout(ctx, Unconstrained())
	}
	return Size{W: constraints.MaxW, H: constraints.MaxH}
}

func (c *Center) SetBounds(bounds Rect) {
	c.BaseWidget.SetBounds(bounds)
	if c.content != nil {
		// Use cached size from Layout, not Bounds (which may not be set yet)
		x := bounds.X + (bounds.W-c.contentSize.W)/2
		y := bounds.Y + (bounds.H-c.contentSize.H)/2
		c.content.SetBounds(Rect{X: x, Y: y, W: c.contentSize.W, H: c.contentSize.H})
	}
}

func (c *Center) Draw(ctx *DrawContext) {
	if !c.visible {
		return
	}
	if c.content != nil {
		c.content.Draw(ctx)
	}
}

func (c *Center) HandleEvent(ctx *EventContext, event Event) bool {
	if c.content != nil {
		return c.content.HandleEvent(ctx, event)
	}
	return false
}

func (c *Center) Children() []Widget {
	if c.content != nil {
		return []Widget{c.content}
	}
	return nil
}

// Positioned allows absolute positioning of a widget.
type Positioned struct {
	BaseWidget

	content Widget
	x, y    float32
	w, h    float32
}

// NewPositioned creates an absolutely positioned widget.
func NewPositioned(content Widget, x, y, w, h float32) *Positioned {
	return &Positioned{
		BaseWidget: NewBaseWidget(),
		content:    content,
		x:          x,
		y:          y,
		w:          w,
		h:          h,
	}
}

func (p *Positioned) Layout(ctx *LayoutContext, constraints Constraints) Size {
	if p.content != nil {
		p.content.Layout(ctx, Tight(p.w, p.h))
	}
	return Size{W: constraints.MaxW, H: constraints.MaxH}
}

func (p *Positioned) SetBounds(bounds Rect) {
	p.BaseWidget.SetBounds(bounds)
	if p.content != nil {
		p.content.SetBounds(Rect{X: bounds.X + p.x, Y: bounds.Y + p.y, W: p.w, H: p.h})
	}
}

func (p *Positioned) Draw(ctx *DrawContext) {
	if !p.visible {
		return
	}
	if p.content != nil {
		p.content.Draw(ctx)
	}
}

func (p *Positioned) HandleEvent(ctx *EventContext, event Event) bool {
	if p.content != nil {
		return p.content.HandleEvent(ctx, event)
	}
	return false
}

func (p *Positioned) Children() []Widget {
	if p.content != nil {
		return []Widget{p.content}
	}
	return nil
}
