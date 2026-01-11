package ui

import (
	"github.com/tinyrange/cc/internal/gowin/graphics"
)

// ProceduralIconWidget displays a procedurally generated geometric icon.
type ProceduralIconWidget struct {
	BaseWidget

	icon      *graphics.ProceduralIcon
	gfxWindow graphics.Window

	// Sizing
	width  float32
	height float32

	// State
	initialized bool
}

// NewProceduralIconWidget creates a new procedural icon widget from a name.
// The icon is deterministically generated based on a hash of the name.
func NewProceduralIconWidget(name string) *ProceduralIconWidget {
	return &ProceduralIconWidget{
		BaseWidget: NewBaseWidget(),
		icon:       graphics.NewProceduralIcon(name),
		width:      156, // Default to bundle card placeholder size
		height:     120,
	}
}

// WithSize sets the widget's preferred size.
func (p *ProceduralIconWidget) WithSize(w, h float32) *ProceduralIconWidget {
	p.width = w
	p.height = h
	return p
}

// WithGraphicsWindow sets the graphics window for GPU initialization.
func (p *ProceduralIconWidget) WithGraphicsWindow(w graphics.Window) *ProceduralIconWidget {
	p.gfxWindow = w
	return p
}

// Layout implements the Widget interface.
func (p *ProceduralIconWidget) Layout(ctx *LayoutContext, constraints Constraints) Size {
	w := clamp(p.width, constraints.MinW, constraints.MaxW)
	h := clamp(p.height, constraints.MinH, constraints.MaxH)
	return Size{W: w, H: h}
}

// Draw implements the Widget interface.
func (p *ProceduralIconWidget) Draw(ctx *DrawContext) {
	if !p.visible {
		return
	}

	bounds := p.Bounds()

	// Lazy initialization of GPU resources
	if !p.initialized && p.gfxWindow != nil {
		if err := p.icon.Initialize(p.gfxWindow, bounds.W, bounds.H); err == nil {
			p.initialized = true
		}
	}

	if p.initialized {
		p.icon.Draw(ctx.Frame, bounds.X, bounds.Y)
	}
}

// HandleEvent implements the Widget interface.
func (p *ProceduralIconWidget) HandleEvent(ctx *EventContext, event Event) bool {
	return false
}
