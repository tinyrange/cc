package ui

// FlowContainer arranges children horizontally and wraps to new lines when needed.
// Similar to CSS flexbox with flex-wrap: wrap.
type FlowContainer struct {
	BaseWidget
	children   []Widget
	gap        float32 // Horizontal gap between items
	lineGap    float32 // Vertical gap between lines

	// Cached layout data
	childSizes  map[WidgetID]Size
	lines       [][]int   // Indices of children per line
	lineHeights []float32 // Height of each line
}

// NewFlowContainer creates a new flow container.
func NewFlowContainer() *FlowContainer {
	return &FlowContainer{
		BaseWidget: NewBaseWidget(),
		childSizes: make(map[WidgetID]Size),
		gap:        4,
		lineGap:    4,
	}
}

// Flow creates a new flow container (alias).
func Flow() *FlowContainer {
	return NewFlowContainer()
}

// WithGap sets the horizontal gap between items.
func (f *FlowContainer) WithGap(gap float32) *FlowContainer {
	f.gap = gap
	return f
}

// WithLineGap sets the vertical gap between lines.
func (f *FlowContainer) WithLineGap(gap float32) *FlowContainer {
	f.lineGap = gap
	return f
}

// AddChild adds a widget to the container.
func (f *FlowContainer) AddChild(child Widget) *FlowContainer {
	f.children = append(f.children, child)
	return f
}

// Children returns all child widgets.
func (f *FlowContainer) Children() []Widget {
	return f.children
}

// Layout measures the container and arranges children into lines.
func (f *FlowContainer) Layout(ctx *LayoutContext, constraints Constraints) Size {
	if len(f.children) == 0 {
		return Size{W: 0, H: 0}
	}

	availableWidth := constraints.MaxW

	// First pass: measure all children with unbounded width
	f.childSizes = make(map[WidgetID]Size)
	for _, child := range f.children {
		childConstraints := Constraints{
			MinW: 0,
			MinH: 0,
			MaxW: availableWidth, // Constrain to available width for WrapLabels
			MaxH: 1e9,
		}
		size := child.Layout(ctx, childConstraints)
		f.childSizes[child.ID()] = size
	}

	// Second pass: arrange into lines
	f.lines = nil
	f.lineHeights = nil

	var currentLine []int
	var currentLineWidth float32
	var currentLineHeight float32

	for i, child := range f.children {
		size := f.childSizes[child.ID()]

		// Check if this child fits on current line
		widthNeeded := size.W
		if len(currentLine) > 0 {
			widthNeeded += f.gap
		}

		if len(currentLine) > 0 && currentLineWidth+widthNeeded > availableWidth {
			// Start new line
			f.lines = append(f.lines, currentLine)
			f.lineHeights = append(f.lineHeights, currentLineHeight)
			currentLine = nil
			currentLineWidth = 0
			currentLineHeight = 0
		}

		// Add to current line
		currentLine = append(currentLine, i)
		if len(currentLine) > 1 {
			currentLineWidth += f.gap
		}
		currentLineWidth += size.W
		if size.H > currentLineHeight {
			currentLineHeight = size.H
		}
	}

	// Don't forget the last line
	if len(currentLine) > 0 {
		f.lines = append(f.lines, currentLine)
		f.lineHeights = append(f.lineHeights, currentLineHeight)
	}

	// Calculate total size
	var totalHeight float32
	var maxWidth float32

	for i, lineHeight := range f.lineHeights {
		if i > 0 {
			totalHeight += f.lineGap
		}
		totalHeight += lineHeight

		// Calculate line width
		var lineWidth float32
		for j, childIdx := range f.lines[i] {
			if j > 0 {
				lineWidth += f.gap
			}
			lineWidth += f.childSizes[f.children[childIdx].ID()].W
		}
		if lineWidth > maxWidth {
			maxWidth = lineWidth
		}
	}

	return Size{W: maxWidth, H: totalHeight}
}

// SetBounds positions all children within the container bounds.
func (f *FlowContainer) SetBounds(bounds Rect) {
	f.BaseWidget.SetBounds(bounds)

	if len(f.lines) == 0 {
		return
	}

	y := bounds.Y

	for lineIdx, line := range f.lines {
		if lineIdx > 0 {
			y += f.lineGap
		}

		lineHeight := f.lineHeights[lineIdx]
		x := bounds.X

		for j, childIdx := range line {
			if j > 0 {
				x += f.gap
			}

			child := f.children[childIdx]
			size := f.childSizes[child.ID()]

			// Center vertically within line
			childY := y + (lineHeight-size.H)/2

			child.SetBounds(Rect{
				X: x,
				Y: childY,
				W: size.W,
				H: size.H,
			})

			x += size.W
		}

		y += lineHeight
	}
}

// Draw renders all children.
func (f *FlowContainer) Draw(ctx *DrawContext) {
	if !f.visible {
		return
	}

	for _, child := range f.children {
		child.Draw(ctx)
	}
}

// HandleEvent passes events to children.
func (f *FlowContainer) HandleEvent(ctx *EventContext, event Event) bool {
	// Pass to children in reverse order (topmost first)
	for i := len(f.children) - 1; i >= 0; i-- {
		if f.children[i].HandleEvent(ctx, event) {
			return true
		}
	}
	return false
}
