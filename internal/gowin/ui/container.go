package ui

import "image/color"

// FlexContainer is a container that lays out children using flexbox rules.
type FlexContainer struct {
	BaseWidget

	axis       Axis
	mainAlign  MainAxisAlignment
	crossAlign CrossAxisAlignment
	padding    EdgeInsets
	gap        float32

	children    []Widget
	childParams map[WidgetID]FlexLayoutParams
	childSizes  map[WidgetID]Size

	backgroundColor color.Color
}

// NewFlexContainer creates a new FlexContainer.
func NewFlexContainer(axis Axis) *FlexContainer {
	return &FlexContainer{
		BaseWidget:  NewBaseWidget(),
		axis:        axis,
		mainAlign:   MainAxisStart,
		crossAlign:  CrossAxisStart,
		childParams: make(map[WidgetID]FlexLayoutParams),
		childSizes:  make(map[WidgetID]Size),
	}
}

// Row creates a horizontal FlexContainer.
func Row() *FlexContainer {
	return NewFlexContainer(AxisHorizontal)
}

// Column creates a vertical FlexContainer.
func Column() *FlexContainer {
	return NewFlexContainer(AxisVertical)
}

// Builder pattern methods

func (c *FlexContainer) WithPadding(p EdgeInsets) *FlexContainer {
	c.padding = p
	return c
}

func (c *FlexContainer) WithGap(gap float32) *FlexContainer {
	c.gap = gap
	return c
}

func (c *FlexContainer) WithMainAlignment(align MainAxisAlignment) *FlexContainer {
	c.mainAlign = align
	return c
}

func (c *FlexContainer) WithCrossAlignment(align CrossAxisAlignment) *FlexContainer {
	c.crossAlign = align
	return c
}

func (c *FlexContainer) WithBackground(col color.Color) *FlexContainer {
	c.backgroundColor = col
	return c
}

// AddChild adds a child widget with layout parameters.
func (c *FlexContainer) AddChild(child Widget, params FlexLayoutParams) *FlexContainer {
	c.children = append(c.children, child)
	c.childParams[child.ID()] = params
	return c
}

// AddChildren adds multiple children with default parameters.
func (c *FlexContainer) AddChildren(children ...Widget) *FlexContainer {
	for _, child := range children {
		c.AddChild(child, DefaultFlexParams())
	}
	return c
}

func (c *FlexContainer) RemoveChild(child Widget) {
	for i, ch := range c.children {
		if ch.ID() == child.ID() {
			c.children = append(c.children[:i], c.children[i+1:]...)
			delete(c.childParams, child.ID())
			delete(c.childSizes, child.ID())
			return
		}
	}
}

func (c *FlexContainer) ClearChildren() {
	c.children = nil
	c.childParams = make(map[WidgetID]FlexLayoutParams)
	c.childSizes = make(map[WidgetID]Size)
}

func (c *FlexContainer) Children() []Widget {
	return c.children
}

// Layout implements the flexbox layout algorithm.
func (c *FlexContainer) Layout(ctx *LayoutContext, constraints Constraints) Size {
	if len(c.children) == 0 {
		w := clamp(c.padding.Horizontal(), constraints.MinW, constraints.MaxW)
		h := clamp(c.padding.Vertical(), constraints.MinH, constraints.MaxH)
		return Size{W: w, H: h}
	}

	// Calculate available space
	availableMain := c.mainSize(constraints.MaxW, constraints.MaxH) - c.mainPadding() - c.gap*float32(len(c.children)-1)
	availableCross := c.crossSize(constraints.MaxW, constraints.MaxH) - c.crossPadding()

	// First pass: measure non-flex children
	var totalFixed float32
	var totalFlex float32

	for _, child := range c.children {
		params := c.childParams[child.ID()]
		margin := params.Margin

		if params.Flex == 0 {
			// Fixed child - measure with unlimited main axis
			var childConstraints Constraints
			if c.axis == AxisHorizontal {
				childConstraints = Constraints{
					MinW: 0, MaxW: 1e9,
					MinH: 0, MaxH: availableCross - margin.Vertical(),
				}
			} else {
				childConstraints = Constraints{
					MinW: 0, MaxW: availableCross - margin.Horizontal(),
					MinH: 0, MaxH: 1e9,
				}
			}

			size := child.Layout(ctx, childConstraints)
			c.childSizes[child.ID()] = size
			totalFixed += c.mainOfSize(size) + c.mainMargin(margin)
		} else {
			totalFlex += params.Flex
		}
	}

	// Distribute remaining space to flex children
	remainingMain := availableMain - totalFixed
	if remainingMain < 0 {
		remainingMain = 0
	}

	for _, child := range c.children {
		params := c.childParams[child.ID()]
		if params.Flex > 0 {
			margin := params.Margin
			flexMain := remainingMain * (params.Flex / totalFlex)

			var childConstraints Constraints
			if c.axis == AxisHorizontal {
				childConstraints = Constraints{
					MinW: flexMain - margin.Horizontal(), MaxW: flexMain - margin.Horizontal(),
					MinH: 0, MaxH: availableCross - margin.Vertical(),
				}
			} else {
				childConstraints = Constraints{
					MinW: 0, MaxW: availableCross - margin.Horizontal(),
					MinH: flexMain - margin.Vertical(), MaxH: flexMain - margin.Vertical(),
				}
			}

			// Ensure non-negative constraints
			if childConstraints.MinW < 0 {
				childConstraints.MinW = 0
			}
			if childConstraints.MaxW < 0 {
				childConstraints.MaxW = 0
			}
			if childConstraints.MinH < 0 {
				childConstraints.MinH = 0
			}
			if childConstraints.MaxH < 0 {
				childConstraints.MaxH = 0
			}

			size := child.Layout(ctx, childConstraints)
			c.childSizes[child.ID()] = size
		}
	}

	// Calculate total size
	var totalMain float32
	var maxCross float32

	for _, child := range c.children {
		size := c.childSizes[child.ID()]
		params := c.childParams[child.ID()]
		margin := params.Margin

		totalMain += c.mainOfSize(size) + c.mainMargin(margin)
		cross := c.crossOfSize(size) + c.crossMargin(margin)
		if cross > maxCross {
			maxCross = cross
		}
	}
	totalMain += c.gap * float32(len(c.children)-1)

	var resultW, resultH float32
	if c.axis == AxisHorizontal {
		resultW = totalMain + c.padding.Horizontal()
		resultH = maxCross + c.padding.Vertical()
	} else {
		resultW = maxCross + c.padding.Horizontal()
		resultH = totalMain + c.padding.Vertical()
	}

	return Size{
		W: clamp(resultW, constraints.MinW, constraints.MaxW),
		H: clamp(resultH, constraints.MinH, constraints.MaxH),
	}
}

func (c *FlexContainer) SetBounds(bounds Rect) {
	c.BaseWidget.SetBounds(bounds)
	c.positionChildren()
}

func (c *FlexContainer) positionChildren() {
	if len(c.children) == 0 {
		return
	}

	bounds := c.Bounds()

	// Calculate total content size and spacing
	var totalMain float32
	for _, child := range c.children {
		size := c.childSizes[child.ID()]
		params := c.childParams[child.ID()]
		totalMain += c.mainOfSize(size) + c.mainMargin(params.Margin)
	}
	totalMain += c.gap * float32(len(c.children)-1)

	availableMain := c.mainSize(bounds.W, bounds.H) - c.mainPadding()
	availableCross := c.crossSize(bounds.W, bounds.H) - c.crossPadding()
	extraSpace := availableMain - totalMain
	if extraSpace < 0 {
		extraSpace = 0
	}

	// Determine starting position based on main axis alignment
	var mainPos float32
	var spacing float32
	switch c.mainAlign {
	case MainAxisStart:
		mainPos = c.mainStart(bounds) + c.mainStartPadding()
	case MainAxisCenter:
		mainPos = c.mainStart(bounds) + c.mainStartPadding() + extraSpace/2
	case MainAxisEnd:
		mainPos = c.mainStart(bounds) + c.mainStartPadding() + extraSpace
	case MainAxisSpaceBetween:
		mainPos = c.mainStart(bounds) + c.mainStartPadding()
		if len(c.children) > 1 {
			spacing = extraSpace / float32(len(c.children)-1)
		}
	case MainAxisSpaceAround:
		spacing = extraSpace / float32(len(c.children))
		mainPos = c.mainStart(bounds) + c.mainStartPadding() + spacing/2
	case MainAxisSpaceEvenly:
		spacing = extraSpace / float32(len(c.children)+1)
		mainPos = c.mainStart(bounds) + c.mainStartPadding() + spacing
	}

	crossStart := c.crossStart(bounds) + c.crossStartPadding()

	// Position each child
	for i, child := range c.children {
		size := c.childSizes[child.ID()]
		params := c.childParams[child.ID()]
		margin := params.Margin

		mainPos += c.mainStartMargin(margin)

		// Determine cross position based on alignment
		crossAlign := c.crossAlign
		if params.Alignment != nil {
			crossAlign = *params.Alignment
		}

		var crossPos float32
		childCross := c.crossOfSize(size)
		switch crossAlign {
		case CrossAxisStart:
			crossPos = crossStart + c.crossStartMargin(margin)
		case CrossAxisCenter:
			crossPos = crossStart + (availableCross-childCross)/2
		case CrossAxisEnd:
			crossPos = crossStart + availableCross - childCross - c.crossEndMargin(margin)
		case CrossAxisStretch:
			crossPos = crossStart + c.crossStartMargin(margin)
			childCross = availableCross - c.crossMargin(margin)
		}

		var childBounds Rect
		if c.axis == AxisHorizontal {
			childBounds = Rect{X: mainPos, Y: crossPos, W: size.W, H: childCross}
		} else {
			childBounds = Rect{X: crossPos, Y: mainPos, W: childCross, H: size.H}
		}
		child.SetBounds(childBounds)

		mainPos += c.mainOfSize(size) + c.mainEndMargin(margin) + c.gap
		if i < len(c.children)-1 && (c.mainAlign == MainAxisSpaceBetween || c.mainAlign == MainAxisSpaceAround || c.mainAlign == MainAxisSpaceEvenly) {
			mainPos += spacing
		}
	}
}

func (c *FlexContainer) Draw(ctx *DrawContext) {
	if !c.visible {
		return
	}

	if c.backgroundColor != nil {
		b := c.Bounds()
		ctx.Frame.RenderQuad(b.X, b.Y, b.W, b.H, nil, c.backgroundColor)
	}

	for _, child := range c.children {
		child.Draw(ctx)
	}
}

func (c *FlexContainer) HandleEvent(ctx *EventContext, event Event) bool {
	// For MouseMoveEvent, dispatch to ALL children so they can update hover state
	if _, isMove := event.(*MouseMoveEvent); isMove {
		for i := len(c.children) - 1; i >= 0; i-- {
			c.children[i].HandleEvent(ctx, event)
		}
		return false
	}

	// For other events, dispatch in reverse order (top-most first) and stop when handled
	for i := len(c.children) - 1; i >= 0; i-- {
		if c.children[i].HandleEvent(ctx, event) {
			return true
		}
	}
	return false
}

// Helper methods for axis-agnostic calculations

func (c *FlexContainer) mainSize(w, h float32) float32 {
	if c.axis == AxisHorizontal {
		return w
	}
	return h
}

func (c *FlexContainer) crossSize(w, h float32) float32 {
	if c.axis == AxisHorizontal {
		return h
	}
	return w
}

func (c *FlexContainer) mainOfSize(s Size) float32 {
	if c.axis == AxisHorizontal {
		return s.W
	}
	return s.H
}

func (c *FlexContainer) crossOfSize(s Size) float32 {
	if c.axis == AxisHorizontal {
		return s.H
	}
	return s.W
}

func (c *FlexContainer) mainPadding() float32 {
	if c.axis == AxisHorizontal {
		return c.padding.Horizontal()
	}
	return c.padding.Vertical()
}

func (c *FlexContainer) crossPadding() float32 {
	if c.axis == AxisHorizontal {
		return c.padding.Vertical()
	}
	return c.padding.Horizontal()
}

func (c *FlexContainer) mainStartPadding() float32 {
	if c.axis == AxisHorizontal {
		return c.padding.Left
	}
	return c.padding.Top
}

func (c *FlexContainer) crossStartPadding() float32 {
	if c.axis == AxisHorizontal {
		return c.padding.Top
	}
	return c.padding.Left
}

func (c *FlexContainer) mainMargin(m EdgeInsets) float32 {
	if c.axis == AxisHorizontal {
		return m.Horizontal()
	}
	return m.Vertical()
}

func (c *FlexContainer) crossMargin(m EdgeInsets) float32 {
	if c.axis == AxisHorizontal {
		return m.Vertical()
	}
	return m.Horizontal()
}

func (c *FlexContainer) mainStartMargin(m EdgeInsets) float32 {
	if c.axis == AxisHorizontal {
		return m.Left
	}
	return m.Top
}

func (c *FlexContainer) mainEndMargin(m EdgeInsets) float32 {
	if c.axis == AxisHorizontal {
		return m.Right
	}
	return m.Bottom
}

func (c *FlexContainer) crossStartMargin(m EdgeInsets) float32 {
	if c.axis == AxisHorizontal {
		return m.Top
	}
	return m.Left
}

func (c *FlexContainer) crossEndMargin(m EdgeInsets) float32 {
	if c.axis == AxisHorizontal {
		return m.Bottom
	}
	return m.Right
}

func (c *FlexContainer) mainStart(r Rect) float32 {
	if c.axis == AxisHorizontal {
		return r.X
	}
	return r.Y
}

func (c *FlexContainer) crossStart(r Rect) float32 {
	if c.axis == AxisHorizontal {
		return r.Y
	}
	return r.X
}
