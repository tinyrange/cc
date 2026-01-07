// Package ui provides a widget-based UI framework for graphics rendering.
package ui

import "image/color"

// Rect represents a rectangle with position and size.
type Rect struct {
	X, Y float32
	W, H float32
}

// Contains returns true if the point (px, py) is inside the rectangle.
func (r Rect) Contains(px, py float32) bool {
	return px >= r.X && px <= r.X+r.W && py >= r.Y && py <= r.Y+r.H
}

// Inset returns a new Rect inset by the given amounts.
func (r Rect) Inset(left, top, right, bottom float32) Rect {
	return Rect{
		X: r.X + left,
		Y: r.Y + top,
		W: r.W - left - right,
		H: r.H - top - bottom,
	}
}

// Size represents dimensions.
type Size struct {
	W, H float32
}

// Constraints define min/max sizing constraints for layout.
type Constraints struct {
	MinW, MaxW float32
	MinH, MaxH float32
}

// Unconstrained returns constraints with no limits.
func Unconstrained() Constraints {
	return Constraints{
		MinW: 0, MaxW: 1e9,
		MinH: 0, MaxH: 1e9,
	}
}

// Tight returns constraints that force a specific size.
func Tight(w, h float32) Constraints {
	return Constraints{
		MinW: w, MaxW: w,
		MinH: h, MaxH: h,
	}
}

// WidgetID is a unique identifier for a widget (for event targeting).
type WidgetID uint64

// Widget is the core interface for all UI elements.
type Widget interface {
	// ID returns a unique identifier for this widget.
	ID() WidgetID

	// Layout computes the desired size given constraints.
	// Returns the actual size the widget wants.
	Layout(ctx *LayoutContext, constraints Constraints) Size

	// SetBounds sets the final position and size after layout.
	SetBounds(bounds Rect)

	// Bounds returns the current bounds.
	Bounds() Rect

	// Draw renders the widget to the frame.
	Draw(ctx *DrawContext)

	// HandleEvent processes an input event. Returns true if consumed.
	HandleEvent(ctx *EventContext, event Event) bool

	// Children returns child widgets (empty for leaf widgets).
	Children() []Widget
}

// BaseWidget provides common functionality for all widgets.
type BaseWidget struct {
	id        WidgetID
	bounds    Rect
	visible   bool
	enabled   bool
	focusable bool
}

var nextWidgetID WidgetID = 1

// NewBaseWidget creates a new BaseWidget with a unique ID.
func NewBaseWidget() BaseWidget {
	id := nextWidgetID
	nextWidgetID++
	return BaseWidget{
		id:      id,
		visible: true,
		enabled: true,
	}
}

func (w *BaseWidget) ID() WidgetID       { return w.id }
func (w *BaseWidget) Bounds() Rect       { return w.bounds }
func (w *BaseWidget) SetBounds(b Rect)   { w.bounds = b }
func (w *BaseWidget) IsVisible() bool    { return w.visible }
func (w *BaseWidget) SetVisible(v bool)  { w.visible = v }
func (w *BaseWidget) IsEnabled() bool    { return w.enabled }
func (w *BaseWidget) SetEnabled(e bool)  { w.enabled = e }
func (w *BaseWidget) IsFocusable() bool  { return w.focusable }
func (w *BaseWidget) Children() []Widget { return nil }

// Helper functions
func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func max(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

func min(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

// ColorWithAlpha returns a color with modified alpha.
func ColorWithAlpha(c color.Color, alpha uint8) color.Color {
	r, g, b, _ := c.RGBA()
	return color.RGBA{
		R: uint8(r >> 8),
		G: uint8(g >> 8),
		B: uint8(b >> 8),
		A: alpha,
	}
}
