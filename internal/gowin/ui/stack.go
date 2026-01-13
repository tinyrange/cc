package ui

// Stack layers children on top of each other.
// Children are drawn in order (first child at bottom, last at top).
type Stack struct {
	BaseWidget

	children []Widget
}

// NewStack creates a new stack.
func NewStack() *Stack {
	return &Stack{
		BaseWidget: NewBaseWidget(),
	}
}

// AddChild adds a child to the stack.
func (s *Stack) AddChild(child Widget) *Stack {
	s.children = append(s.children, child)
	return s
}

// AddChildren adds multiple children to the stack.
func (s *Stack) AddChildren(children ...Widget) *Stack {
	s.children = append(s.children, children...)
	return s
}

func (s *Stack) ClearChildren() {
	s.children = nil
}

func (s *Stack) Children() []Widget {
	return s.children
}

func (s *Stack) Layout(ctx *LayoutContext, constraints Constraints) Size {
	// Track maximum child size
	var maxW, maxH float32

	// Layout each child with the same constraints
	for _, child := range s.children {
		childSize := child.Layout(ctx, constraints)
		if childSize.W > maxW {
			maxW = childSize.W
		}
		if childSize.H > maxH {
			maxH = childSize.H
		}
	}

	// If constraints are bounded, fill available space
	// Otherwise, use the maximum child size
	const unboundedThreshold = 1e8
	w := maxW
	h := maxH
	if constraints.MaxW < unboundedThreshold {
		w = constraints.MaxW
	}
	if constraints.MaxH < unboundedThreshold {
		h = constraints.MaxH
	}

	return Size{W: w, H: h}
}

func (s *Stack) SetBounds(bounds Rect) {
	s.BaseWidget.SetBounds(bounds)
	// Each child gets the full bounds
	for _, child := range s.children {
		child.SetBounds(bounds)
	}
}

func (s *Stack) Draw(ctx *DrawContext) {
	if !s.visible {
		return
	}
	// Draw children in order (first at bottom)
	for _, child := range s.children {
		child.Draw(ctx)
	}
}

func (s *Stack) HandleEvent(ctx *EventContext, event Event) bool {
	// For MouseMoveEvent, dispatch to ALL children so they can update hover state
	if _, isMove := event.(*MouseMoveEvent); isMove {
		for i := len(s.children) - 1; i >= 0; i-- {
			s.children[i].HandleEvent(ctx, event)
		}
		return false
	}

	// For other events, dispatch in reverse order (top-most first) and stop when handled
	for i := len(s.children) - 1; i >= 0; i-- {
		if s.children[i].HandleEvent(ctx, event) {
			return true
		}
	}
	return false
}

// Align positions a child within the Stack bounds.
type Align struct {
	BaseWidget

	content    Widget
	horizontal float32 // 0 = left, 0.5 = center, 1 = right
	vertical   float32 // 0 = top, 0.5 = center, 1 = bottom

	// Cached content size from Layout
	contentSize Size
}

// NewAlign creates an alignment wrapper.
func NewAlign(content Widget, horizontal, vertical float32) *Align {
	return &Align{
		BaseWidget: NewBaseWidget(),
		content:    content,
		horizontal: horizontal,
		vertical:   vertical,
	}
}

// TopLeft aligns content to top-left.
func TopLeft(content Widget) *Align {
	return NewAlign(content, 0, 0)
}

// TopCenter aligns content to top-center.
func TopCenter(content Widget) *Align {
	return NewAlign(content, 0.5, 0)
}

// TopRight aligns content to top-right.
func TopRight(content Widget) *Align {
	return NewAlign(content, 1, 0)
}

// CenterLeft aligns content to center-left.
func CenterLeft(content Widget) *Align {
	return NewAlign(content, 0, 0.5)
}

// CenterCenter aligns content to center.
func CenterCenter(content Widget) *Align {
	return NewAlign(content, 0.5, 0.5)
}

// CenterRight aligns content to center-right.
func CenterRight(content Widget) *Align {
	return NewAlign(content, 1, 0.5)
}

// BottomLeft aligns content to bottom-left.
func BottomLeft(content Widget) *Align {
	return NewAlign(content, 0, 1)
}

// BottomCenter aligns content to bottom-center.
func BottomCenter(content Widget) *Align {
	return NewAlign(content, 0.5, 1)
}

// BottomRight aligns content to bottom-right.
func BottomRight(content Widget) *Align {
	return NewAlign(content, 1, 1)
}

func (a *Align) Layout(ctx *LayoutContext, constraints Constraints) Size {
	if a.content != nil {
		a.contentSize = a.content.Layout(ctx, Unconstrained())
	}

	const unboundedThreshold = 1e8

	w := constraints.MaxW
	h := constraints.MaxH

	// For unbounded constraints, use content size (intrinsic measurement)
	if constraints.MaxW >= unboundedThreshold {
		w = a.contentSize.W
	}
	if constraints.MaxH >= unboundedThreshold {
		h = a.contentSize.H
	}

	return Size{W: w, H: h}
}

func (a *Align) SetBounds(bounds Rect) {
	a.BaseWidget.SetBounds(bounds)
	if a.content != nil {
		// Use cached size from Layout, not Bounds (which may not be set yet)
		x := bounds.X + (bounds.W-a.contentSize.W)*a.horizontal
		y := bounds.Y + (bounds.H-a.contentSize.H)*a.vertical
		a.content.SetBounds(Rect{X: x, Y: y, W: a.contentSize.W, H: a.contentSize.H})
	}
}

func (a *Align) Draw(ctx *DrawContext) {
	if !a.visible {
		return
	}
	if a.content != nil {
		a.content.Draw(ctx)
	}
}

func (a *Align) HandleEvent(ctx *EventContext, event Event) bool {
	if a.content != nil {
		return a.content.HandleEvent(ctx, event)
	}
	return false
}

func (a *Align) Children() []Widget {
	if a.content != nil {
		return []Widget{a.content}
	}
	return nil
}
