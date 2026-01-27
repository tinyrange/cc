package ui

import (
	"image/color"
	"time"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/window"
)

// ToggleRenderMode controls how the toggle is rendered.
type ToggleRenderMode int

const (
	ToggleRenderQuads ToggleRenderMode = iota // Use simple quads (default)
	ToggleRenderMesh                          // Use mesh for smooth shapes
)

// ToggleStyle defines toggle switch appearance.
type ToggleStyle struct {
	// Track (background pill)
	TrackWidth    float32
	TrackHeight   float32
	TrackColorOn  color.Color
	TrackColorOff color.Color

	// Thumb (circular knob)
	ThumbSize    float32
	ThumbColor   color.Color
	ThumbPadding float32 // gap between thumb edge and track edge

	// Animation
	AnimationDuration time.Duration
}

// DefaultToggleStyle returns the default toggle styling.
func DefaultToggleStyle() ToggleStyle {
	return ToggleStyle{
		TrackWidth:        50,
		TrackHeight:       28,
		TrackColorOn:      color.RGBA{R: 52, G: 120, B: 246, A: 255},  // Blue
		TrackColorOff:     color.RGBA{R: 120, G: 120, B: 120, A: 255}, // Gray
		ThumbSize:         22,
		ThumbColor:        graphics.ColorWhite,
		ThumbPadding:      3,
		AnimationDuration: 200 * time.Millisecond,
	}
}

// Toggle is an iOS-style toggle switch widget.
type Toggle struct {
	BaseWidget

	style    ToggleStyle
	on       bool
	hovered  bool
	onChange func(on bool)

	// Animation state
	animProgress  float32 // 0.0 = off position, 1.0 = on position
	animating     bool
	animStart     time.Time
	animStartPos  float32
	animTargetPos float32

	// Mesh rendering (optional)
	renderMode    ToggleRenderMode
	gfxWindow     graphics.Window
	trackBuilder  *graphics.ShapeBuilder
	thumbBuilder  *graphics.ShapeBuilder
	lastTrackRect Rect
	lastThumbRect Rect
}

// NewToggle creates a new toggle switch.
func NewToggle() *Toggle {
	t := &Toggle{
		BaseWidget:   NewBaseWidget(),
		style:        DefaultToggleStyle(),
		animProgress: 0,
	}
	t.focusable = true
	return t
}

// WithStyle sets the toggle style.
func (t *Toggle) WithStyle(style ToggleStyle) *Toggle {
	t.style = style
	return t
}

// OnChange sets the callback for when the toggle state changes.
func (t *Toggle) OnChange(handler func(on bool)) *Toggle {
	t.onChange = handler
	return t
}

// WithGraphicsWindow enables mesh rendering with the given graphics window.
func (t *Toggle) WithGraphicsWindow(w graphics.Window) *Toggle {
	t.gfxWindow = w
	t.renderMode = ToggleRenderMesh
	return t
}

// IsOn returns the current state of the toggle.
func (t *Toggle) IsOn() bool {
	return t.on
}

// SetOn sets the toggle state immediately (no animation).
func (t *Toggle) SetOn(v bool) {
	t.on = v
	if v {
		t.animProgress = 1.0
	} else {
		t.animProgress = 0.0
	}
	t.animating = false
}

// SetOnAnimated sets the toggle state with animation.
func (t *Toggle) SetOnAnimated(v bool) {
	if t.on == v {
		return
	}
	t.on = v
	t.animating = true
	t.animStart = time.Now()
	t.animStartPos = t.animProgress
	if v {
		t.animTargetPos = 1.0
	} else {
		t.animTargetPos = 0.0
	}
}

func (t *Toggle) updateAnimation() {
	if !t.animating {
		return
	}

	elapsed := time.Since(t.animStart)
	progress := float32(elapsed) / float32(t.style.AnimationDuration)

	if progress >= 1.0 {
		t.animProgress = t.animTargetPos
		t.animating = false
	} else {
		// Ease-out interpolation: 1 - (1-t)^2
		eased := 1.0 - (1.0-progress)*(1.0-progress)
		t.animProgress = t.animStartPos + (t.animTargetPos-t.animStartPos)*eased
	}
}

// Layout implements Widget.
func (t *Toggle) Layout(ctx *LayoutContext, constraints Constraints) Size {
	w := clamp(t.style.TrackWidth, constraints.MinW, constraints.MaxW)
	h := clamp(t.style.TrackHeight, constraints.MinH, constraints.MaxH)
	return Size{W: w, H: h}
}

// Draw implements Widget.
func (t *Toggle) Draw(ctx *DrawContext) {
	if !t.visible {
		return
	}

	t.updateAnimation()
	bounds := t.Bounds()

	// Center track vertically within bounds
	trackY := bounds.Y + (bounds.H-t.style.TrackHeight)/2

	// Interpolate track color based on animation progress
	trackColor := LerpColor(t.style.TrackColorOff, t.style.TrackColorOn, t.animProgress)

	// Calculate thumb position
	thumbTravel := t.style.TrackWidth - t.style.ThumbSize - t.style.ThumbPadding*2
	thumbX := bounds.X + t.style.ThumbPadding + thumbTravel*t.animProgress
	thumbY := trackY + (t.style.TrackHeight-t.style.ThumbSize)/2

	if t.renderMode == ToggleRenderMesh && t.gfxWindow != nil {
		// Use mesh rendering for smooth shapes
		t.drawMesh(ctx, bounds.X, trackY, thumbX, thumbY, trackColor)
	} else {
		// Fallback to quad rendering
		t.drawQuads(ctx, bounds.X, trackY, thumbX, thumbY, trackColor)
	}
}

func (t *Toggle) drawQuads(ctx *DrawContext, trackX, trackY, thumbX, thumbY float32, trackColor color.Color) {
	// Draw track as 3 parts: left cap, center rect, right cap
	// This approximates a pill shape using rectangles
	radius := t.style.TrackHeight / 2

	// Left cap (semicircle approximation - just use same color rect)
	ctx.Frame.RenderQuad(trackX, trackY, radius, t.style.TrackHeight, nil, trackColor)
	// Center rectangle
	ctx.Frame.RenderQuad(trackX+radius, trackY, t.style.TrackWidth-t.style.TrackHeight, t.style.TrackHeight, nil, trackColor)
	// Right cap
	ctx.Frame.RenderQuad(trackX+t.style.TrackWidth-radius, trackY, radius, t.style.TrackHeight, nil, trackColor)

	// Thumb (approximation - square for now)
	ctx.Frame.RenderQuad(thumbX, thumbY, t.style.ThumbSize, t.style.ThumbSize, nil, t.style.ThumbColor)
}

func (t *Toggle) drawMesh(ctx *DrawContext, trackX, trackY, thumbX, thumbY float32, trackColor color.Color) {
	// Initialize track builder if needed
	if t.trackBuilder == nil {
		segments := graphics.SegmentsForRadius(t.style.TrackHeight / 2)
		var err error
		t.trackBuilder, err = graphics.NewShapeBuilder(t.gfxWindow, segments)
		if err != nil {
			t.drawQuads(ctx, trackX, trackY, thumbX, thumbY, trackColor)
			return
		}
	}

	// Initialize thumb builder if needed
	if t.thumbBuilder == nil {
		segments := graphics.SegmentsForRadius(t.style.ThumbSize / 2)
		var err error
		t.thumbBuilder, err = graphics.NewShapeBuilder(t.gfxWindow, segments)
		if err != nil {
			t.drawQuads(ctx, trackX, trackY, thumbX, thumbY, trackColor)
			return
		}
	}

	// Update track geometry (pill shape)
	trackRect := Rect{X: trackX, Y: trackY, W: t.style.TrackWidth, H: t.style.TrackHeight}
	// Always update track since color changes with animation
	trackStyle := graphics.ShapeStyle{FillColor: trackColor}
	t.trackBuilder.UpdatePill(trackRect.X, trackRect.Y, trackRect.W, trackRect.H, trackStyle)
	t.lastTrackRect = trackRect

	// Update thumb geometry (circle)
	thumbRect := Rect{X: thumbX, Y: thumbY, W: t.style.ThumbSize, H: t.style.ThumbSize}
	if thumbRect != t.lastThumbRect {
		thumbStyle := graphics.ShapeStyle{FillColor: t.style.ThumbColor}
		thumbRadius := t.style.ThumbSize / 2
		t.thumbBuilder.UpdateCircle(thumbX+thumbRadius, thumbY+thumbRadius, thumbRadius, thumbStyle)
		t.lastThumbRect = thumbRect
	}

	// Render track then thumb
	ctx.Frame.RenderMesh(t.trackBuilder.Mesh(), graphics.DrawOptions{})
	ctx.Frame.RenderMesh(t.thumbBuilder.Mesh(), graphics.DrawOptions{})
}

// HandleEvent implements Widget.
func (t *Toggle) HandleEvent(ctx *EventContext, event Event) bool {
	if !t.enabled || !t.visible {
		return false
	}

	bounds := t.Bounds()

	switch e := event.(type) {
	case *MouseMoveEvent:
		t.hovered = bounds.Contains(e.X, e.Y)
		return false // Don't consume move events

	case *MouseButtonEvent:
		if e.Button != window.ButtonLeft {
			return false
		}
		if !e.Pressed && bounds.Contains(e.X, e.Y) {
			// Toggle state with animation
			t.on = !t.on
			t.animating = true
			t.animStart = time.Now()
			t.animStartPos = t.animProgress
			if t.on {
				t.animTargetPos = 1.0
			} else {
				t.animTargetPos = 0.0
			}
			if t.onChange != nil {
				t.onChange(t.on)
			}
			return true
		}
	}

	return false
}
