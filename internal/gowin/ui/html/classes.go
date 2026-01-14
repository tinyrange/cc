// Based on Tailwind CSS classes.

package html

import (
	"image/color"
	"strconv"
	"strings"

	"github.com/tinyrange/cc/internal/gowin/ui"
)

// GradientDirection specifies the direction of a gradient.
type GradientDirection int

const (
	GradientNone GradientDirection = iota
	GradientToTop
	GradientToBottom
	GradientToLeft
	GradientToRight
	GradientToTopLeft
	GradientToTopRight
	GradientToBottomLeft
	GradientToBottomRight
)

// StyleSet holds computed styles from classes.
type StyleSet struct {
	// Layout
	Axis       ui.Axis
	MainAlign  ui.MainAxisAlignment
	CrossAlign ui.CrossAxisAlignment
	Flex       float32
	FlexShrink *float32
	FlexGrow   *float32
	IsFlex     bool // Explicitly set to flex
	IsGrid     bool // Grid layout (treated as flex-wrap)
	GridCols   int  // Number of grid columns

	// Spacing
	Padding ui.EdgeInsets
	Margin  ui.EdgeInsets
	Gap     float32
	SpaceX  float32 // space-x-* (horizontal spacing between children)
	SpaceY  float32 // space-y-* (vertical spacing between children)

	// Sizing
	Width        *float32
	Height       *float32
	WidthPercent float32 // 0-1, from style="width: X%"
	MinWidth     float32
	MinHeight    float32
	MaxWidth     float32
	MaxHeight    float32
	FullWidth    bool // w-full
	FullHeight   bool // h-full
	ScreenH      bool // h-screen

	// Colors
	BackgroundColor *color.RGBA
	TextColor       *color.RGBA

	// Gradients
	GradientDir  GradientDirection
	GradientFrom *color.RGBA
	GradientVia  *color.RGBA
	GradientTo   *color.RGBA

	// Typography
	TextSize   *float64
	FontWeight *int // 100-900

	// Borders
	CornerRadius *float32
	BorderWidth  float32
	BorderTop    float32
	BorderRight  float32
	BorderBottom float32
	BorderLeft   float32
	BorderColor  *color.RGBA

	// Effects
	Opacity *float32

	// Overflow
	OverflowHidden bool
	OverflowAuto   bool

	// Positioning
	Position string // relative, absolute, fixed
	Inset    *float32
	Top      *float32
	Right    *float32
	Bottom   *float32
	Left     *float32

	// Cursor
	CursorPointer bool

	// Transition (no-op but parsed)
	HasTransition bool

	// Gradient text (background-clip: text)
	GradientText bool
}

// ParseClasses converts a list of classes to a StyleSet.
func ParseClasses(classes []string) StyleSet {
	var s StyleSet
	s.Axis = ui.AxisVertical // Default to column
	for _, class := range classes {
		s.applyClass(class)
	}
	return s
}

func (s *StyleSet) applyClass(class string) {
	// Layout classes
	switch class {
	case "flex":
		s.IsFlex = true
		return
	case "flex-row":
		s.IsFlex = true
		s.Axis = ui.AxisHorizontal
		return
	case "flex-col":
		s.IsFlex = true
		s.Axis = ui.AxisVertical
		return
	case "flex-1", "flex-grow":
		s.Flex = 1
		v := float32(1)
		s.FlexGrow = &v
		return
	case "flex-shrink-0":
		v := float32(0)
		s.FlexShrink = &v
		return
	case "flex-shrink":
		v := float32(1)
		s.FlexShrink = &v
		return
	case "flex-grow-0":
		v := float32(0)
		s.FlexGrow = &v
		return
	case "grid":
		s.IsGrid = true
		s.IsFlex = true // Treat grid as flex-wrap for now
		return
	case "grid-cols-1":
		s.GridCols = 1
		return
	case "grid-cols-2":
		s.GridCols = 2
		return
	case "grid-cols-3":
		s.GridCols = 3
		return
	case "grid-cols-4":
		s.GridCols = 4
		return
	}

	// Justify content
	if strings.HasPrefix(class, "justify-") {
		switch class {
		case "justify-start":
			s.MainAlign = ui.MainAxisStart
		case "justify-center":
			s.MainAlign = ui.MainAxisCenter
		case "justify-end":
			s.MainAlign = ui.MainAxisEnd
		case "justify-between":
			s.MainAlign = ui.MainAxisSpaceBetween
		case "justify-around":
			s.MainAlign = ui.MainAxisSpaceAround
		case "justify-evenly":
			s.MainAlign = ui.MainAxisSpaceEvenly
		}
		return
	}

	// Align items
	if strings.HasPrefix(class, "items-") {
		switch class {
		case "items-start":
			s.CrossAlign = ui.CrossAxisStart
		case "items-center":
			s.CrossAlign = ui.CrossAxisCenter
		case "items-end":
			s.CrossAlign = ui.CrossAxisEnd
		case "items-stretch":
			s.CrossAlign = ui.CrossAxisStretch
		}
		return
	}

	// Gap
	if strings.HasPrefix(class, "gap-") {
		s.Gap = parseSpacing(class[4:])
		return
	}

	// Padding
	if strings.HasPrefix(class, "p-") {
		v := parseSpacing(class[2:])
		s.Padding = ui.All(v)
		return
	}
	if strings.HasPrefix(class, "px-") {
		v := parseSpacing(class[3:])
		s.Padding.Left = v
		s.Padding.Right = v
		return
	}
	if strings.HasPrefix(class, "py-") {
		v := parseSpacing(class[3:])
		s.Padding.Top = v
		s.Padding.Bottom = v
		return
	}
	if strings.HasPrefix(class, "pt-") {
		s.Padding.Top = parseSpacing(class[3:])
		return
	}
	if strings.HasPrefix(class, "pr-") {
		s.Padding.Right = parseSpacing(class[3:])
		return
	}
	if strings.HasPrefix(class, "pb-") {
		s.Padding.Bottom = parseSpacing(class[3:])
		return
	}
	if strings.HasPrefix(class, "pl-") {
		s.Padding.Left = parseSpacing(class[3:])
		return
	}

	// Margin
	if strings.HasPrefix(class, "m-") && !strings.HasPrefix(class, "max-") && !strings.HasPrefix(class, "min-") {
		v := parseSpacing(class[2:])
		s.Margin = ui.All(v)
		return
	}
	if strings.HasPrefix(class, "mx-") {
		v := parseSpacing(class[3:])
		s.Margin.Left = v
		s.Margin.Right = v
		return
	}
	if strings.HasPrefix(class, "my-") {
		v := parseSpacing(class[3:])
		s.Margin.Top = v
		s.Margin.Bottom = v
		return
	}
	if strings.HasPrefix(class, "mt-") {
		s.Margin.Top = parseSpacing(class[3:])
		return
	}
	if strings.HasPrefix(class, "mr-") {
		s.Margin.Right = parseSpacing(class[3:])
		return
	}
	if strings.HasPrefix(class, "mb-") {
		s.Margin.Bottom = parseSpacing(class[3:])
		return
	}
	if strings.HasPrefix(class, "ml-") {
		s.Margin.Left = parseSpacing(class[3:])
		return
	}

	// Width
	if strings.HasPrefix(class, "w-") {
		if class == "w-full" {
			s.FullWidth = true
		} else if class == "w-auto" {
			// Default
		} else if class == "w-screen" {
			s.FullWidth = true // Treat same as full for now
		} else {
			v := parseSpacing(class[2:])
			s.Width = &v
		}
		return
	}
	if strings.HasPrefix(class, "min-w-") {
		s.MinWidth = parseSpacing(class[6:])
		return
	}
	if strings.HasPrefix(class, "max-w-") {
		s.MaxWidth = parseSpacing(class[6:])
		return
	}

	// Height
	if strings.HasPrefix(class, "h-") {
		if class == "h-full" {
			s.FullHeight = true
		} else if class == "h-auto" {
			// Default
		} else if class == "h-screen" {
			s.ScreenH = true
		} else {
			v := parseSpacing(class[2:])
			s.Height = &v
		}
		return
	}
	if strings.HasPrefix(class, "min-h-") {
		if class == "min-h-screen" {
			s.ScreenH = true
		} else {
			s.MinHeight = parseSpacing(class[6:])
		}
		return
	}
	if strings.HasPrefix(class, "max-h-") {
		s.MaxHeight = parseSpacing(class[6:])
		return
	}

	// Space between children
	if strings.HasPrefix(class, "space-x-") {
		s.SpaceX = parseSpacing(class[8:])
		return
	}
	if strings.HasPrefix(class, "space-y-") {
		s.SpaceY = parseSpacing(class[8:])
		return
	}

	// Background colors and gradients
	if strings.HasPrefix(class, "bg-") {
		// Check for gradient direction
		switch class {
		case "bg-gradient-to-t":
			s.GradientDir = GradientToTop
			return
		case "bg-gradient-to-b":
			s.GradientDir = GradientToBottom
			return
		case "bg-gradient-to-l":
			s.GradientDir = GradientToLeft
			return
		case "bg-gradient-to-r":
			s.GradientDir = GradientToRight
			return
		case "bg-gradient-to-tl":
			s.GradientDir = GradientToTopLeft
			return
		case "bg-gradient-to-tr":
			s.GradientDir = GradientToTopRight
			return
		case "bg-gradient-to-bl":
			s.GradientDir = GradientToBottomLeft
			return
		case "bg-gradient-to-br":
			s.GradientDir = GradientToBottomRight
			return
		}
		// Regular background color
		if c, ok := colorMap[class]; ok {
			s.BackgroundColor = &c
		}
		return
	}

	// Gradient colors
	if strings.HasPrefix(class, "from-") {
		colorKey := class // from-mango-500 -> from-mango-500
		if c, ok := colorMap[colorKey]; ok {
			s.GradientFrom = &c
		}
		return
	}
	if strings.HasPrefix(class, "via-") {
		colorKey := class
		if c, ok := colorMap[colorKey]; ok {
			s.GradientVia = &c
		}
		return
	}
	if strings.HasPrefix(class, "to-") && !strings.HasPrefix(class, "top-") {
		colorKey := class
		if c, ok := colorMap[colorKey]; ok {
			s.GradientTo = &c
		}
		return
	}

	// Text colors and sizes
	if strings.HasPrefix(class, "text-") {
		suffix := class[5:]
		// Check if it's a size
		if size, ok := textSizeScale[suffix]; ok {
			s.TextSize = &size
			return
		}
		// Check if it's a color
		colorKey := "text-" + suffix
		if c, ok := colorMap[colorKey]; ok {
			s.TextColor = &c
		}
		return
	}

	// Border radius
	if class == "rounded" {
		v := float32(4)
		s.CornerRadius = &v
		return
	}
	if strings.HasPrefix(class, "rounded-") {
		suffix := class[8:]
		if v, ok := radiusScale[suffix]; ok {
			s.CornerRadius = &v
		}
		return
	}

	// Borders
	if class == "border" {
		s.BorderWidth = 1
		return
	}
	if class == "border-0" {
		s.BorderWidth = 0
		return
	}
	if class == "border-2" {
		s.BorderWidth = 2
		return
	}
	if class == "border-4" {
		s.BorderWidth = 4
		return
	}
	if class == "border-t" {
		s.BorderTop = 1
		return
	}
	if class == "border-r" {
		s.BorderRight = 1
		return
	}
	if class == "border-b" {
		s.BorderBottom = 1
		return
	}
	if class == "border-l" {
		s.BorderLeft = 1
		return
	}
	if strings.HasPrefix(class, "border-") && !strings.HasPrefix(class, "border-t-") &&
		!strings.HasPrefix(class, "border-r-") && !strings.HasPrefix(class, "border-b-") &&
		!strings.HasPrefix(class, "border-l-") {
		// Try as border color
		colorKey := "border-" + class[7:]
		if c, ok := colorMap[colorKey]; ok {
			s.BorderColor = &c
			return
		}
		// Also try the color directly (border-mango-200)
		if c, ok := colorMap[class]; ok {
			s.BorderColor = &c
			return
		}
	}

	// Font weight
	switch class {
	case "font-thin":
		v := 100
		s.FontWeight = &v
		return
	case "font-extralight":
		v := 200
		s.FontWeight = &v
		return
	case "font-light":
		v := 300
		s.FontWeight = &v
		return
	case "font-normal":
		v := 400
		s.FontWeight = &v
		return
	case "font-medium":
		v := 500
		s.FontWeight = &v
		return
	case "font-semibold":
		v := 600
		s.FontWeight = &v
		return
	case "font-bold":
		v := 700
		s.FontWeight = &v
		return
	case "font-extrabold":
		v := 800
		s.FontWeight = &v
		return
	case "font-black":
		v := 900
		s.FontWeight = &v
		return
	}

	// Opacity
	if strings.HasPrefix(class, "opacity-") {
		suffix := class[8:]
		if v, err := strconv.ParseFloat(suffix, 32); err == nil {
			opacity := float32(v / 100.0)
			s.Opacity = &opacity
		}
		return
	}

	// Overflow
	switch class {
	case "overflow-hidden":
		s.OverflowHidden = true
		return
	case "overflow-auto":
		s.OverflowAuto = true
		return
	case "overflow-scroll":
		s.OverflowAuto = true
		return
	case "overflow-visible":
		// Default
		return
	case "overflow-x-hidden", "overflow-y-hidden":
		s.OverflowHidden = true
		return
	case "overflow-x-auto", "overflow-y-auto":
		s.OverflowAuto = true
		return
	}

	// Positioning
	switch class {
	case "relative":
		s.Position = "relative"
		return
	case "absolute":
		s.Position = "absolute"
		return
	case "fixed":
		s.Position = "fixed"
		return
	case "static":
		s.Position = "static"
		return
	}

	// Inset
	if strings.HasPrefix(class, "inset-") {
		v := parseSpacing(class[6:])
		s.Inset = &v
		return
	}
	if strings.HasPrefix(class, "top-") {
		v := parseSpacing(class[4:])
		s.Top = &v
		return
	}
	if strings.HasPrefix(class, "right-") {
		v := parseSpacing(class[6:])
		s.Right = &v
		return
	}
	if strings.HasPrefix(class, "bottom-") {
		v := parseSpacing(class[7:])
		s.Bottom = &v
		return
	}
	if strings.HasPrefix(class, "left-") {
		v := parseSpacing(class[5:])
		s.Left = &v
		return
	}

	// Cursor
	if class == "cursor-pointer" {
		s.CursorPointer = true
		return
	}

	// Transition (no-op but parsed to avoid unknown class warnings)
	if strings.HasPrefix(class, "transition") {
		s.HasTransition = true
		return
	}

	// Duration (no-op)
	if strings.HasPrefix(class, "duration-") {
		return
	}

	// Shrink/truncate text (no-op for now)
	if class == "truncate" || class == "line-clamp-1" || class == "line-clamp-2" {
		return
	}

	// Whitespace (no-op)
	if strings.HasPrefix(class, "whitespace-") {
		return
	}

	// Text alignment (parse but limited support)
	if class == "text-center" || class == "text-left" || class == "text-right" {
		return
	}

	// Gradient text (background-clip: text effect)
	if class == "gradient-text" {
		s.GradientText = true
		return
	}

	// Shadow (parsed, will be handled in builder)
	if strings.HasPrefix(class, "shadow") {
		return
	}

	// Ring (no-op)
	if strings.HasPrefix(class, "ring") {
		return
	}

	// Focus states (no-op)
	if strings.HasPrefix(class, "focus:") || strings.HasPrefix(class, "hover:") ||
		strings.HasPrefix(class, "active:") || strings.HasPrefix(class, "group-hover:") {
		return
	}

	// Aspect ratio (no-op for now)
	if strings.HasPrefix(class, "aspect-") {
		return
	}

	// Object fit (no-op)
	if strings.HasPrefix(class, "object-") {
		return
	}

	// Z-index (no-op)
	if strings.HasPrefix(class, "z-") {
		return
	}

	// Pointer events (no-op)
	if strings.HasPrefix(class, "pointer-events-") {
		return
	}

	// Select (no-op)
	if strings.HasPrefix(class, "select-") {
		return
	}
}

// Spacing scale: Use 4px as the base unit
var spacingScale = map[string]float32{
	"0":   0,
	"0.5": 2,
	"1":   4,
	"1.5": 6,
	"2":   8,
	"2.5": 10,
	"3":   12,
	"3.5": 14,
	"4":   16,
	"5":   20,
	"6":   24,
	"7":   28,
	"8":   32,
	"9":   36,
	"10":  40,
	"11":  44,
	"12":  48,
	"14":  56,
	"16":  64,
	"20":  80,
	"24":  96,
	"28":  112,
	"32":  128,
	"36":  144,
	"40":  160,
	"44":  176,
	"48":  192,
	"52":  208,
	"56":  224,
	"60":  240,
	"64":  256,
	"72":  288,
	"80":  320,
	"96":  384,
}

func parseSpacing(s string) float32 {
	if v, ok := spacingScale[s]; ok {
		return v
	}
	// Try parsing as number (for arbitrary values)
	if n, err := strconv.ParseFloat(s, 32); err == nil {
		return float32(n) * 4
	}
	return 0
}

// Text size scale
var textSizeScale = map[string]float64{
	"xs":   12,
	"sm":   14,
	"base": 16,
	"lg":   18,
	"xl":   20,
	"2xl":  24,
	"3xl":  30,
	"4xl":  36,
	"5xl":  48,
	"6xl":  60,
}

// Heading sizes (used by builder)
var headingSizes = map[string]float64{
	"h1": 32,
	"h2": 24,
	"h3": 20,
	"h4": 18,
	"h5": 16,
	"h6": 14,
}

// Border radius scale
var radiusScale = map[string]float32{
	"none": 0,
	"sm":   4,
	"md":   6,
	"lg":   10,
	"xl":   12,
	"2xl":  16,
	"3xl":  24,
	"full": 9999,
}
