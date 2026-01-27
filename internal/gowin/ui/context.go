package ui

import (
	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/text"
	"github.com/tinyrange/cc/internal/gowin/window"
)

// LayoutContext provides context during the layout phase.
type LayoutContext struct {
	TextRenderer *text.Renderer
	WindowScale  float32
}

// NewLayoutContext creates a layout context.
func NewLayoutContext(txt *text.Renderer, scale float32) *LayoutContext {
	return &LayoutContext{
		TextRenderer: txt,
		WindowScale:  scale,
	}
}

// DrawContext provides context during the draw phase.
type DrawContext struct {
	Frame graphics.Frame
	Text  *text.Renderer

	// BlurEffect provides optional blur support.
	// May be nil if blur is not initialized.
	BlurEffect *graphics.BlurEffect

	// BlurredBackground is a cached blurred texture of the background.
	// Widgets can use this for glassmorphism effects.
	// May be nil if no blur has been applied.
	BlurredBackground graphics.Texture
}

// EventContext provides context during event handling.
type EventContext struct {
	FocusedWidget WidgetID
	HoveredWidget WidgetID
}

// Root is the top-level container that manages the widget tree.
type Root struct {
	child Widget

	layoutCtx *LayoutContext
	inputProc *InputProcessor
	eventCtx  *EventContext

	textRenderer *text.Renderer

	needsLayout bool
	lastWidth   int
	lastHeight  int

	// maxW and maxH constrain the layout size. 0 means use window size.
	maxW float32
	maxH float32

	// offsetX and offsetY shift where the child is positioned.
	offsetX float32
	offsetY float32
}

// NewRoot creates a new root container.
func NewRoot(txt *text.Renderer) *Root {
	return &Root{
		layoutCtx:    NewLayoutContext(txt, 1.0),
		inputProc:    &InputProcessor{},
		eventCtx:     &EventContext{},
		textRenderer: txt,
		needsLayout:  true,
	}
}

// SetChild sets the root widget.
func (r *Root) SetChild(child Widget) {
	r.child = child
	r.needsLayout = true
}

// InvalidateLayout marks the layout as needing recalculation.
func (r *Root) InvalidateLayout() {
	r.needsLayout = true
}

// SetMaxSize constrains the root layout to a maximum size.
// Pass 0 to use window size for that dimension.
func (r *Root) SetMaxSize(maxW, maxH float32) {
	if r.maxW != maxW || r.maxH != maxH {
		r.maxW = maxW
		r.maxH = maxH
		r.needsLayout = true
	}
}

// SetOffset sets the position offset for the root layout.
// The child will be positioned at (offsetX, offsetY) instead of (0, 0).
func (r *Root) SetOffset(offsetX, offsetY float32) {
	if r.offsetX != offsetX || r.offsetY != offsetY {
		r.offsetX = offsetX
		r.offsetY = offsetY
		r.needsLayout = true
	}
}

// Update processes input and updates widget state.
func (r *Root) Update(f graphics.Frame, pw window.Window) {
	// Update layout context scale
	r.layoutCtx.WindowScale = pw.Scale()

	// Process input events
	events := r.inputProc.ProcessFrame(f, pw)
	for _, event := range events {
		if r.child != nil {
			r.child.HandleEvent(r.eventCtx, event)
		}
	}

	// Check if window size changed
	w, h := f.WindowSize()
	if w != r.lastWidth || h != r.lastHeight {
		r.needsLayout = true
		r.lastWidth = w
		r.lastHeight = h
	}
}

// Layout performs layout if needed.
func (r *Root) Layout(f graphics.Frame) {
	if !r.needsLayout || r.child == nil {
		return
	}

	w, h := f.WindowSize()

	// Use maxW/maxH if set, otherwise use window size
	layoutW := float32(w)
	layoutH := float32(h)
	if r.maxW > 0 {
		layoutW = r.maxW
	}
	if r.maxH > 0 {
		layoutH = r.maxH
	}

	// Subtract offset from available height if using window size
	if r.maxH == 0 && r.offsetY > 0 {
		layoutH -= r.offsetY
	}

	constraints := Constraints{
		MinW: layoutW, MaxW: layoutW,
		MinH: layoutH, MaxH: layoutH,
	}

	size := r.child.Layout(r.layoutCtx, constraints)
	r.child.SetBounds(Rect{X: r.offsetX, Y: r.offsetY, W: size.W, H: size.H})

	r.needsLayout = false
}

// Draw renders the widget tree.
func (r *Root) Draw(f graphics.Frame) {
	if r.child == nil {
		return
	}

	w, h := f.WindowSize()
	if r.textRenderer != nil {
		r.textRenderer.SetViewport(int32(w), int32(h))
	}

	ctx := &DrawContext{
		Frame: f,
		Text:  r.textRenderer,
	}

	r.child.Draw(ctx)
}

// Step is a convenience method that performs update, layout, and draw.
func (r *Root) Step(f graphics.Frame, pw window.Window) {
	r.Update(f, pw)
	r.Layout(f)
	r.Draw(f)
}

// DrawOnly performs layout and draw without processing input events.
// Use this when rendering a screen as a background behind another screen.
func (r *Root) DrawOnly(f graphics.Frame) {
	r.Layout(f)
	r.Draw(f)
}

// DispatchEvent dispatches a single event to the widget tree.
// Use this to manually inject events without draining from the platform window.
func (r *Root) DispatchEvent(event Event) {
	if r.child != nil {
		r.child.HandleEvent(r.eventCtx, event)
	}
}
