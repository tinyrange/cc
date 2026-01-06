package ui

import (
	"image/color"

	"github.com/tinyrange/cc/internal/gowin/graphics"
)

// ImageScaleMode controls how images are scaled.
type ImageScaleMode int

const (
	ImageScaleFit     ImageScaleMode = iota // Maintain aspect, fit inside
	ImageScaleFill                          // Maintain aspect, fill container
	ImageScaleStretch                       // Stretch to fill
)

// Image displays a texture.
type Image struct {
	BaseWidget

	texture graphics.Texture

	// Sizing
	naturalWidth  float32
	naturalHeight float32
	scaleMode     ImageScaleMode

	tintColor color.Color
}

// NewImage creates an image widget from a texture.
func NewImage(tex graphics.Texture) *Image {
	w, h := tex.Size()
	return &Image{
		BaseWidget:    NewBaseWidget(),
		texture:       tex,
		naturalWidth:  float32(w),
		naturalHeight: float32(h),
		scaleMode:     ImageScaleFit,
		tintColor:     graphics.ColorWhite,
	}
}

func (i *Image) WithScaleMode(mode ImageScaleMode) *Image {
	i.scaleMode = mode
	return i
}

func (i *Image) WithTint(c color.Color) *Image {
	i.tintColor = c
	return i
}

func (i *Image) Layout(ctx *LayoutContext, constraints Constraints) Size {
	// Use natural size clamped to constraints
	w := clamp(i.naturalWidth, constraints.MinW, constraints.MaxW)
	h := clamp(i.naturalHeight, constraints.MinH, constraints.MaxH)
	return Size{W: w, H: h}
}

func (i *Image) Draw(ctx *DrawContext) {
	if !i.visible || i.texture == nil {
		return
	}
	bounds := i.Bounds()
	ctx.Frame.RenderQuad(bounds.X, bounds.Y, bounds.W, bounds.H, i.texture, i.tintColor)
}

func (i *Image) HandleEvent(ctx *EventContext, event Event) bool {
	return false
}

// SVGImage displays an SVG with optional rotation animation.
type SVGImage struct {
	BaseWidget

	svg *graphics.SVG

	// Sizing
	naturalWidth  float32
	naturalHeight float32

	// Animation
	rotation float32 // Current rotation in radians

	// For grouped SVG rendering
	groupName string
}

// NewSVGImage creates an SVG image widget.
func NewSVGImage(svg *graphics.SVG) *SVGImage {
	return &SVGImage{
		BaseWidget:    NewBaseWidget(),
		svg:           svg,
		naturalWidth:  100, // Default size
		naturalHeight: 100,
	}
}

func (s *SVGImage) WithSize(w, h float32) *SVGImage {
	s.naturalWidth = w
	s.naturalHeight = h
	return s
}

func (s *SVGImage) WithGroup(name string) *SVGImage {
	s.groupName = name
	return s
}

func (s *SVGImage) SetRotation(radians float32) {
	s.rotation = radians
}

func (s *SVGImage) Layout(ctx *LayoutContext, constraints Constraints) Size {
	w := clamp(s.naturalWidth, constraints.MinW, constraints.MaxW)
	h := clamp(s.naturalHeight, constraints.MinH, constraints.MaxH)
	return Size{W: w, H: h}
}

func (s *SVGImage) Draw(ctx *DrawContext) {
	if !s.visible || s.svg == nil {
		return
	}
	bounds := s.Bounds()

	if s.groupName != "" {
		s.svg.DrawGroupRotated(ctx.Frame, s.groupName, bounds.X, bounds.Y, bounds.W, bounds.H, s.rotation)
	} else {
		s.svg.Draw(ctx.Frame, bounds.X, bounds.Y, bounds.W, bounds.H)
	}
}

func (s *SVGImage) HandleEvent(ctx *EventContext, event Event) bool {
	return false
}

// AnimatedLogo is a specialized widget for the rotating logo with multiple groups.
type AnimatedLogo struct {
	BaseWidget

	svg    *graphics.SVG
	width  float32
	height float32

	// Per-group rotation speeds
	innerSpeed float32
	morseSpeed float32
	outerSpeed float32

	// Current time for animation
	time float32
}

// NewAnimatedLogo creates an animated logo widget.
func NewAnimatedLogo(svg *graphics.SVG) *AnimatedLogo {
	return &AnimatedLogo{
		BaseWidget: NewBaseWidget(),
		svg:        svg,
		width:      200,
		height:     200,
		innerSpeed: 0.4,
		morseSpeed: -0.9,
		outerSpeed: 1.6,
	}
}

func (a *AnimatedLogo) WithSize(w, h float32) *AnimatedLogo {
	a.width = w
	a.height = h
	return a
}

func (a *AnimatedLogo) WithSpeeds(inner, morse, outer float32) *AnimatedLogo {
	a.innerSpeed = inner
	a.morseSpeed = morse
	a.outerSpeed = outer
	return a
}

func (a *AnimatedLogo) SetTime(t float32) {
	a.time = t
}

func (a *AnimatedLogo) Layout(ctx *LayoutContext, constraints Constraints) Size {
	w := clamp(a.width, constraints.MinW, constraints.MaxW)
	h := clamp(a.height, constraints.MinH, constraints.MaxH)
	return Size{W: w, H: h}
}

func (a *AnimatedLogo) Draw(ctx *DrawContext) {
	if !a.visible || a.svg == nil {
		return
	}
	bounds := a.Bounds()

	a.svg.DrawGroupRotated(ctx.Frame, "inner-circle", bounds.X, bounds.Y, bounds.W, bounds.H, a.time*a.innerSpeed)
	a.svg.DrawGroupRotated(ctx.Frame, "morse-circle", bounds.X, bounds.Y, bounds.W, bounds.H, a.time*a.morseSpeed)
	a.svg.DrawGroupRotated(ctx.Frame, "outer-circle", bounds.X, bounds.Y, bounds.W, bounds.H, a.time*a.outerSpeed)
}

func (a *AnimatedLogo) HandleEvent(ctx *EventContext, event Event) bool {
	return false
}
