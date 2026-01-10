package ui

import (
	"image/color"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/window"
)

// CheckboxStyle defines checkbox appearance.
type CheckboxStyle struct {
	BoxSize         float32
	BoxColor        color.Color
	BoxColorHovered color.Color
	CheckColor      color.Color
	LabelColor      color.Color
	LabelSize       float64
	Gap             float32
}

// DefaultCheckboxStyle returns the default checkbox styling.
func DefaultCheckboxStyle() CheckboxStyle {
	return CheckboxStyle{
		BoxSize:         18,
		BoxColor:        color.RGBA{R: 50, G: 50, B: 50, A: 255},
		BoxColorHovered: color.RGBA{R: 70, G: 70, B: 70, A: 255},
		CheckColor:      color.RGBA{R: 100, G: 180, B: 255, A: 255},
		LabelColor:      graphics.ColorWhite,
		LabelSize:       14,
		Gap:             8,
	}
}

// Checkbox is a toggleable checkbox with label.
type Checkbox struct {
	BaseWidget

	label    string
	style    CheckboxStyle
	checked  bool
	hovered  bool
	onChange func(checked bool)

	// Cached layout
	labelWidth float32
}

// NewCheckbox creates a new checkbox with label.
func NewCheckbox(label string) *Checkbox {
	c := &Checkbox{
		BaseWidget: NewBaseWidget(),
		label:      label,
		style:      DefaultCheckboxStyle(),
	}
	c.focusable = true
	return c
}

func (c *Checkbox) WithStyle(style CheckboxStyle) *Checkbox {
	c.style = style
	return c
}

func (c *Checkbox) OnChange(handler func(checked bool)) *Checkbox {
	c.onChange = handler
	return c
}

func (c *Checkbox) IsChecked() bool {
	return c.checked
}

func (c *Checkbox) SetChecked(v bool) {
	c.checked = v
}

func (c *Checkbox) SetLabel(label string) {
	c.label = label
	c.labelWidth = 0 // Force recalculation
}

func (c *Checkbox) Layout(ctx *LayoutContext, constraints Constraints) Size {
	// Measure label
	if ctx.TextRenderer != nil {
		c.labelWidth = ctx.TextRenderer.Advance(c.style.LabelSize, c.label)
	} else {
		c.labelWidth = float32(len(c.label)) * float32(c.style.LabelSize) * 0.6
	}

	w := c.style.BoxSize + c.style.Gap + c.labelWidth
	h := max(c.style.BoxSize, float32(c.style.LabelSize)*1.2)

	// Clamp to constraints
	w = clamp(w, constraints.MinW, constraints.MaxW)
	h = clamp(h, constraints.MinH, constraints.MaxH)

	return Size{W: w, H: h}
}

func (c *Checkbox) Draw(ctx *DrawContext) {
	if !c.visible {
		return
	}

	bounds := c.Bounds()

	// Draw box
	boxY := bounds.Y + (bounds.H-c.style.BoxSize)/2
	boxColor := c.style.BoxColor
	if c.hovered && c.enabled {
		boxColor = c.style.BoxColorHovered
	}
	ctx.Frame.RenderQuad(bounds.X, boxY, c.style.BoxSize, c.style.BoxSize, nil, boxColor)

	// Draw check mark if checked
	if c.checked {
		// Draw inner filled rect as check indicator
		inset := c.style.BoxSize * 0.25
		ctx.Frame.RenderQuad(
			bounds.X+inset,
			boxY+inset,
			c.style.BoxSize-inset*2,
			c.style.BoxSize-inset*2,
			nil,
			c.style.CheckColor,
		)
	}

	// Draw label
	if ctx.Text != nil && c.label != "" {
		labelX := bounds.X + c.style.BoxSize + c.style.Gap
		labelY := bounds.Y + bounds.H/2 + float32(c.style.LabelSize)/3
		ctx.Text.RenderText(c.label, labelX, labelY, c.style.LabelSize, c.style.LabelColor)
	}
}

func (c *Checkbox) HandleEvent(ctx *EventContext, event Event) bool {
	if !c.enabled || !c.visible {
		return false
	}

	bounds := c.Bounds()

	switch e := event.(type) {
	case *MouseMoveEvent:
		c.hovered = bounds.Contains(e.X, e.Y)
		return false // Don't consume move events

	case *MouseButtonEvent:
		if e.Button != window.ButtonLeft {
			return false
		}
		if !e.Pressed && bounds.Contains(e.X, e.Y) {
			c.checked = !c.checked
			if c.onChange != nil {
				c.onChange(c.checked)
			}
			return true
		}
	}

	return false
}
