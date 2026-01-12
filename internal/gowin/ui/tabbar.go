package ui

import (
	"image/color"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/window"
)

// TabBarStyle defines tab bar appearance.
type TabBarStyle struct {
	BackgroundColor color.Color

	// Tab text
	TextColor         color.Color
	TextColorSelected color.Color
	TextColorHovered  color.Color
	TextSize          float64

	// Tab sizing
	TabPadding EdgeInsets
	TabGap     float32 // gap between tabs

	// Underline indicator
	UnderlineColor     color.Color
	UnderlineThickness float32
	UnderlineInset     float32 // how far from bottom edge

	// Overall sizing
	Height float32
}

// DefaultTabBarStyle returns the default tab bar styling.
func DefaultTabBarStyle() TabBarStyle {
	return TabBarStyle{
		BackgroundColor:    color.RGBA{R: 0, G: 0, B: 0, A: 0}, // Transparent
		TextColor:          color.RGBA{R: 150, G: 150, B: 150, A: 255},
		TextColorSelected:  graphics.ColorWhite,
		TextColorHovered:   color.RGBA{R: 200, G: 200, B: 200, A: 255},
		TextSize:           14,
		TabPadding:         Symmetric(16, 12),
		TabGap:             0,
		UnderlineColor:     color.RGBA{R: 100, G: 140, B: 200, A: 255},
		UnderlineThickness: 2,
		UnderlineInset:     0,
		Height:             40,
	}
}

// TabBar is a horizontal row of selectable tabs with an underline indicator.
type TabBar struct {
	BaseWidget

	tabs          []string
	selectedIndex int
	hoveredIndex  int // -1 if none hovered
	style         TabBarStyle
	onSelect      func(index int)

	// Cached layout data
	tabWidths    []float32
	tabPositions []float32
}

// NewTabBar creates a new tab bar with the given tab labels.
func NewTabBar(tabs []string) *TabBar {
	return &TabBar{
		BaseWidget:    NewBaseWidget(),
		tabs:          tabs,
		selectedIndex: 0,
		hoveredIndex:  -1,
		style:         DefaultTabBarStyle(),
		tabWidths:     make([]float32, len(tabs)),
		tabPositions:  make([]float32, len(tabs)),
	}
}

// WithStyle sets the tab bar style.
func (tb *TabBar) WithStyle(style TabBarStyle) *TabBar {
	tb.style = style
	return tb
}

// OnSelect sets the callback for when a tab is selected.
func (tb *TabBar) OnSelect(handler func(index int)) *TabBar {
	tb.onSelect = handler
	return tb
}

// SelectedIndex returns the currently selected tab index.
func (tb *TabBar) SelectedIndex() int {
	return tb.selectedIndex
}

// SetSelectedIndex sets the selected tab index.
func (tb *TabBar) SetSelectedIndex(index int) {
	if index >= 0 && index < len(tb.tabs) {
		tb.selectedIndex = index
	}
}

// SetTabs updates the tab labels (for dynamic tab changes).
func (tb *TabBar) SetTabs(tabs []string) {
	tb.tabs = tabs
	tb.tabWidths = make([]float32, len(tabs))
	tb.tabPositions = make([]float32, len(tabs))
	if tb.selectedIndex >= len(tabs) {
		tb.selectedIndex = 0
	}
}

// Layout implements Widget.
func (tb *TabBar) Layout(ctx *LayoutContext, constraints Constraints) Size {
	// Measure each tab
	totalWidth := float32(0)
	for i, tab := range tb.tabs {
		var textWidth float32
		if ctx.TextRenderer != nil {
			textWidth = ctx.TextRenderer.Advance(tb.style.TextSize, tab)
		} else {
			textWidth = float32(len(tab)) * float32(tb.style.TextSize) * 0.6
		}

		tb.tabWidths[i] = textWidth + tb.style.TabPadding.Horizontal()
		tb.tabPositions[i] = totalWidth
		totalWidth += tb.tabWidths[i]

		if i < len(tb.tabs)-1 {
			totalWidth += tb.style.TabGap
		}
	}

	w := clamp(totalWidth, constraints.MinW, constraints.MaxW)
	h := clamp(tb.style.Height, constraints.MinH, constraints.MaxH)

	return Size{W: w, H: h}
}

// Draw implements Widget.
func (tb *TabBar) Draw(ctx *DrawContext) {
	if !tb.visible {
		return
	}

	bounds := tb.Bounds()

	// Draw background (if not transparent)
	if tb.style.BackgroundColor != nil {
		_, _, _, a := tb.style.BackgroundColor.RGBA()
		if a > 0 {
			ctx.Frame.RenderQuad(bounds.X, bounds.Y, bounds.W, bounds.H, nil, tb.style.BackgroundColor)
		}
	}

	// Draw each tab
	for i, tab := range tb.tabs {
		tabX := bounds.X + tb.tabPositions[i]
		tabW := tb.tabWidths[i]

		// Determine text color
		var textColor color.Color
		if i == tb.selectedIndex {
			textColor = tb.style.TextColorSelected
		} else if i == tb.hoveredIndex {
			textColor = tb.style.TextColorHovered
		} else {
			textColor = tb.style.TextColor
		}

		// Draw tab text (centered in tab area) using proper font metrics
		if ctx.Text != nil {
			textWidth := ctx.Text.Advance(tb.style.TextSize, tab)
			textX := tabX + (tabW-textWidth)/2
			// Vertical centering: (bounds.H - lineHeight)/2 + ascender
			lineHeight := ctx.Text.LineHeight(tb.style.TextSize)
			ascender := ctx.Text.Ascender(tb.style.TextSize)
			textY := bounds.Y + (bounds.H-lineHeight)/2 + ascender
			ctx.Text.RenderText(tab, textX, textY, tb.style.TextSize, textColor)
		}

		// Draw underline for selected tab
		if i == tb.selectedIndex {
			underlineY := bounds.Y + bounds.H - tb.style.UnderlineThickness - tb.style.UnderlineInset
			ctx.Frame.RenderQuad(
				tabX, underlineY,
				tabW, tb.style.UnderlineThickness,
				nil, tb.style.UnderlineColor,
			)
		}
	}
}

// HandleEvent implements Widget.
func (tb *TabBar) HandleEvent(ctx *EventContext, event Event) bool {
	if !tb.enabled || !tb.visible {
		return false
	}

	bounds := tb.Bounds()

	switch e := event.(type) {
	case *MouseMoveEvent:
		// Check if in bounds at all
		if !bounds.Contains(e.X, e.Y) {
			tb.hoveredIndex = -1
			return false
		}

		// Find which tab is hovered
		tb.hoveredIndex = -1
		for i := range tb.tabs {
			tabX := bounds.X + tb.tabPositions[i]
			tabW := tb.tabWidths[i]
			if e.X >= tabX && e.X < tabX+tabW {
				tb.hoveredIndex = i
				break
			}
		}
		return false // Don't consume move events

	case *MouseButtonEvent:
		if e.Button != window.ButtonLeft {
			return false
		}
		if !e.Pressed && bounds.Contains(e.X, e.Y) {
			// Find clicked tab
			for i := range tb.tabs {
				tabX := bounds.X + tb.tabPositions[i]
				tabW := tb.tabWidths[i]
				if e.X >= tabX && e.X < tabX+tabW {
					if i != tb.selectedIndex {
						tb.selectedIndex = i
						if tb.onSelect != nil {
							tb.onSelect(i)
						}
					}
					return true
				}
			}
		}
	}

	return false
}
