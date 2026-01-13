package ui

import (
	"image/color"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/text"
	"github.com/tinyrange/cc/internal/gowin/window"
)

// ContextMenuItem represents a single item in a context menu.
type ContextMenuItem struct {
	Label     string
	Tag       int
	Enabled   bool
	Separator bool
}

// ContextMenuStyle defines the appearance of a context menu.
type ContextMenuStyle struct {
	Background     color.Color
	ItemHover      color.Color
	TextNormal     color.Color
	TextDisabled   color.Color
	Separator      color.Color
	CornerRadius   float32
	ItemHeight     float32
	SeparatorHeight float32
	PaddingX       float32
	PaddingY       float32
	TextSize       float64
	MinWidth       float32
}

// DefaultContextMenuStyle returns the default context menu styling (Tokyo Night theme).
func DefaultContextMenuStyle() ContextMenuStyle {
	return ContextMenuStyle{
		Background:     color.RGBA{R: 0x1f, G: 0x23, B: 0x35, A: 245},
		ItemHover:      color.RGBA{R: 0x3d, G: 0x59, B: 0xa1, A: 180},
		TextNormal:     color.RGBA{R: 0xa9, G: 0xb1, B: 0xd6, A: 255},
		TextDisabled:   color.RGBA{R: 0x56, G: 0x5f, B: 0x89, A: 255},
		Separator:      color.RGBA{R: 0x3b, G: 0x40, B: 0x54, A: 255},
		CornerRadius:   6,
		ItemHeight:     28,
		SeparatorHeight: 9,
		PaddingX:       12,
		PaddingY:       6,
		TextSize:       14,
		MinWidth:       120,
	}
}

// ContextMenu is a popup menu widget.
type ContextMenu struct {
	BaseWidget

	items       []ContextMenuItem
	style       ContextMenuStyle
	hoveredIdx  int // -1 = none hovered
	onSelect    func(tag int)
	onDismiss   func()

	// Positioning
	anchorX float32
	anchorY float32

	// Rendering
	gfxWindow    graphics.Window
	textRenderer *text.Renderer
	shapeBuilder *graphics.ShapeBuilder
	hoverBuilder *graphics.ShapeBuilder
	lastBounds   Rect

	// Layout cache (computed once on Show)
	layoutDone bool
	itemWidths []float32
	totalWidth float32
	totalHeight float32
}

// NewContextMenu creates a new context menu.
func NewContextMenu(items []ContextMenuItem) *ContextMenu {
	return &ContextMenu{
		BaseWidget: NewBaseWidget(),
		items:      items,
		style:      DefaultContextMenuStyle(),
		hoveredIdx: -1,
	}
}

// WithStyle sets a custom style.
func (m *ContextMenu) WithStyle(style ContextMenuStyle) *ContextMenu {
	m.style = style
	return m
}

// WithGraphicsWindow sets the graphics window for rounded corner rendering.
func (m *ContextMenu) WithGraphicsWindow(w graphics.Window) *ContextMenu {
	m.gfxWindow = w
	return m
}

// WithTextRenderer sets the text renderer for measuring text width.
func (m *ContextMenu) WithTextRenderer(r *text.Renderer) *ContextMenu {
	m.textRenderer = r
	return m
}

// OnSelect sets the callback for when an item is selected.
func (m *ContextMenu) OnSelect(handler func(tag int)) *ContextMenu {
	m.onSelect = handler
	return m
}

// OnDismiss sets the callback for when the menu is dismissed without selection.
func (m *ContextMenu) OnDismiss(handler func()) *ContextMenu {
	m.onDismiss = handler
	return m
}

// Show displays the menu at the given position.
func (m *ContextMenu) Show(x, y float32) {
	m.anchorX = x
	m.anchorY = y
	m.visible = true
	m.hoveredIdx = -1
	m.layoutDone = false // Force layout recalculation
	m.computeLayout()
}

// Hide dismisses the menu.
func (m *ContextMenu) Hide() {
	m.visible = false
}

// IsVisible returns whether the menu is currently shown.
func (m *ContextMenu) IsVisible() bool {
	return m.visible
}

// computeLayout calculates menu dimensions (called once on Show).
func (m *ContextMenu) computeLayout() {
	if m.layoutDone {
		return
	}

	// Calculate total height and max width
	m.totalHeight = m.style.PaddingY * 2
	m.itemWidths = make([]float32, len(m.items))
	m.totalWidth = m.style.MinWidth

	for i, item := range m.items {
		if item.Separator {
			m.totalHeight += m.style.SeparatorHeight
			m.itemWidths[i] = 0
		} else {
			m.totalHeight += m.style.ItemHeight
			// Measure text width
			if m.textRenderer != nil {
				w := m.textRenderer.Advance(m.style.TextSize, item.Label)
				m.itemWidths[i] = w
				totalW := w + m.style.PaddingX*2
				if totalW > m.totalWidth {
					m.totalWidth = totalW
				}
			}
		}
	}

	// Set bounds based on anchor and calculated size
	m.bounds = Rect{
		X: m.anchorX,
		Y: m.anchorY,
		W: m.totalWidth,
		H: m.totalHeight,
	}

	m.layoutDone = true
}

func (m *ContextMenu) Layout(ctx *LayoutContext, constraints Constraints) Size {
	// Use cached layout if available
	if !m.layoutDone {
		m.computeLayout()
	}
	return Size{W: m.totalWidth, H: m.totalHeight}
}

func (m *ContextMenu) SetBounds(bounds Rect) {
	// Position the menu at anchor, adjusting if needed
	bounds.X = m.anchorX
	bounds.Y = m.anchorY
	m.bounds = bounds
}

// PositionInWindow adjusts the menu position to stay within window bounds.
func (m *ContextMenu) PositionInWindow(windowW, windowH float32) {
	// Adjust X if menu would go off right edge
	if m.bounds.X+m.bounds.W > windowW {
		m.bounds.X = windowW - m.bounds.W - 4
	}
	// Adjust Y if menu would go off bottom edge
	if m.bounds.Y+m.bounds.H > windowH {
		m.bounds.Y = windowH - m.bounds.H - 4
	}
	// Ensure not off left/top edge
	if m.bounds.X < 4 {
		m.bounds.X = 4
	}
	if m.bounds.Y < 4 {
		m.bounds.Y = 4
	}
}

func (m *ContextMenu) Draw(ctx *DrawContext) {
	if !m.visible {
		return
	}

	bounds := m.Bounds()

	// Draw background
	if m.style.CornerRadius > 0 && m.gfxWindow != nil {
		m.drawRoundedBackground(ctx, bounds)
	} else {
		ctx.Frame.RenderQuad(bounds.X, bounds.Y, bounds.W, bounds.H, nil, m.style.Background)
	}

	// Draw items
	y := bounds.Y + m.style.PaddingY
	for i, item := range m.items {
		if item.Separator {
			// Draw separator line
			sepY := y + m.style.SeparatorHeight/2
			ctx.Frame.RenderQuad(
				bounds.X+m.style.PaddingX,
				sepY,
				bounds.W-m.style.PaddingX*2,
				1,
				nil,
				m.style.Separator,
			)
			y += m.style.SeparatorHeight
		} else {
			// Draw hover highlight
			if i == m.hoveredIdx && item.Enabled {
				ctx.Frame.RenderQuad(
					bounds.X+2,
					y,
					bounds.W-4,
					m.style.ItemHeight,
					nil,
					m.style.ItemHover,
				)
			}

			// Draw text using proper font metrics
			if ctx.Text != nil {
				textColor := m.style.TextNormal
				if !item.Enabled {
					textColor = m.style.TextDisabled
				}
				textX := bounds.X + m.style.PaddingX
				// Vertical centering: (ItemHeight - lineHeight)/2 + ascender
				lineHeight := ctx.Text.LineHeight(m.style.TextSize)
				ascender := ctx.Text.Ascender(m.style.TextSize)
				textY := y + (m.style.ItemHeight-lineHeight)/2 + ascender
				ctx.Text.RenderText(item.Label, textX, textY, m.style.TextSize, textColor)
			}

			y += m.style.ItemHeight
		}
	}
}

func (m *ContextMenu) drawRoundedBackground(ctx *DrawContext, bounds Rect) {
	// Create shape builder if needed
	if m.shapeBuilder == nil {
		segments := graphics.SegmentsForRadius(m.style.CornerRadius)
		var err error
		m.shapeBuilder, err = graphics.NewShapeBuilder(m.gfxWindow, segments)
		if err != nil {
			ctx.Frame.RenderQuad(bounds.X, bounds.Y, bounds.W, bounds.H, nil, m.style.Background)
			return
		}
	}

	// Update geometry if bounds changed
	if bounds != m.lastBounds {
		style := graphics.ShapeStyle{FillColor: m.style.Background}
		m.shapeBuilder.UpdateRoundedRect(
			bounds.X, bounds.Y, bounds.W, bounds.H,
			graphics.UniformRadius(m.style.CornerRadius),
			style,
		)
		m.lastBounds = bounds
	}

	ctx.Frame.RenderMesh(m.shapeBuilder.Mesh(), graphics.DrawOptions{})
}

func (m *ContextMenu) HandleEvent(ctx *EventContext, event Event) bool {
	if !m.visible {
		return false
	}

	bounds := m.Bounds()

	switch e := event.(type) {
	case *MouseMoveEvent:
		// Update hovered item
		m.hoveredIdx = m.itemAtPoint(e.X, e.Y)
		return bounds.Contains(e.X, e.Y) // Consume if inside menu

	case *MouseButtonEvent:
		if e.Button == window.ButtonLeft {
			if e.Pressed {
				// Check if click is inside menu
				if bounds.Contains(e.X, e.Y) {
					return true // Consume press
				}
				// Click outside - dismiss
				m.Hide()
				if m.onDismiss != nil {
					m.onDismiss()
				}
				return true // Consume to prevent pass-through
			} else {
				// Release - select item if inside
				idx := m.itemAtPoint(e.X, e.Y)
				if idx >= 0 && idx < len(m.items) && m.items[idx].Enabled && !m.items[idx].Separator {
					m.Hide()
					if m.onSelect != nil {
						m.onSelect(m.items[idx].Tag)
					}
					return true
				}
			}
		} else if e.Button == window.ButtonRight {
			// Right click dismisses
			if e.Pressed && !bounds.Contains(e.X, e.Y) {
				m.Hide()
				if m.onDismiss != nil {
					m.onDismiss()
				}
				return true
			}
		}

	case *KeyEvent:
		if e.Pressed {
			switch e.Key {
			case window.KeyEscape:
				m.Hide()
				if m.onDismiss != nil {
					m.onDismiss()
				}
				return true
			case window.KeyUp:
				m.moveSelection(-1)
				return true
			case window.KeyDown:
				m.moveSelection(1)
				return true
			case window.KeyEnter:
				if m.hoveredIdx >= 0 && m.hoveredIdx < len(m.items) {
					item := m.items[m.hoveredIdx]
					if item.Enabled && !item.Separator {
						m.Hide()
						if m.onSelect != nil {
							m.onSelect(item.Tag)
						}
					}
				}
				return true
			}
		}
	}

	return false
}

// itemAtPoint returns the index of the item at the given point, or -1 if none.
func (m *ContextMenu) itemAtPoint(x, y float32) int {
	bounds := m.Bounds()
	if !bounds.Contains(x, y) {
		return -1
	}

	itemY := bounds.Y + m.style.PaddingY
	for i, item := range m.items {
		var itemHeight float32
		if item.Separator {
			itemHeight = m.style.SeparatorHeight
		} else {
			itemHeight = m.style.ItemHeight
		}

		if y >= itemY && y < itemY+itemHeight {
			if item.Separator {
				return -1 // Can't select separators
			}
			return i
		}
		itemY += itemHeight
	}

	return -1
}

// moveSelection moves the keyboard selection up or down.
func (m *ContextMenu) moveSelection(delta int) {
	if len(m.items) == 0 {
		return
	}

	// Start from current or find first/last non-separator
	start := m.hoveredIdx
	if start < 0 {
		if delta > 0 {
			start = -1
		} else {
			start = len(m.items)
		}
	}

	// Move in direction, skipping separators and disabled items
	idx := start
	for {
		idx += delta
		if idx < 0 || idx >= len(m.items) {
			return // Hit boundary
		}
		if !m.items[idx].Separator && m.items[idx].Enabled {
			m.hoveredIdx = idx
			return
		}
	}
}
