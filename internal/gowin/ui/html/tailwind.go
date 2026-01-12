package html

import (
	"image/color"
	"strconv"
	"strings"

	"github.com/tinyrange/cc/internal/gowin/ui"
)

// StyleSet holds computed styles from Tailwind classes.
type StyleSet struct {
	// Layout
	Axis       ui.Axis
	MainAlign  ui.MainAxisAlignment
	CrossAlign ui.CrossAxisAlignment
	Flex       float32
	IsFlex     bool // Explicitly set to flex

	// Spacing
	Padding ui.EdgeInsets
	Margin  ui.EdgeInsets
	Gap     float32

	// Sizing
	Width     *float32
	Height    *float32
	MinWidth  float32
	MinHeight float32
	MaxWidth  float32
	MaxHeight float32

	// Colors
	BackgroundColor *color.RGBA
	TextColor       *color.RGBA

	// Typography
	TextSize *float64

	// Borders
	CornerRadius *float32
}

// ParseClasses converts a list of Tailwind classes to a StyleSet.
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
		return
	case "flex-shrink-0":
		// No-op for now
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
			// Handled specially in builder
		} else if class == "w-auto" {
			// Default
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
			// Handled specially in builder
		} else if class == "h-auto" {
			// Default
		} else {
			v := parseSpacing(class[2:])
			s.Height = &v
		}
		return
	}
	if strings.HasPrefix(class, "min-h-") {
		s.MinHeight = parseSpacing(class[6:])
		return
	}
	if strings.HasPrefix(class, "max-h-") {
		s.MaxHeight = parseSpacing(class[6:])
		return
	}

	// Background colors
	if strings.HasPrefix(class, "bg-") {
		if c, ok := colorMap[class]; ok {
			s.BackgroundColor = &c
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
}

// Spacing scale: Tailwind uses 4px as base unit
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
	"h1": 48,
	"h2": 36,
	"h3": 30,
	"h4": 24,
	"h5": 20,
	"h6": 18,
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
