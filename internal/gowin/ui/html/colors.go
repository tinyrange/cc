package html

import "image/color"

// Tokyo Night color palette for Tailwind class mapping.
// Based on https://github.com/enkia/tokyo-night-vscode-theme

// hex parses a CSS hex color code and returns a color.RGBA.
func hex(s string) color.RGBA {
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}

	var r, g, b uint8
	switch len(s) {
	case 3:
		r = hexDigit(s[0]) * 17
		g = hexDigit(s[1]) * 17
		b = hexDigit(s[2]) * 17
	case 6:
		r = hexDigit(s[0])<<4 | hexDigit(s[1])
		g = hexDigit(s[2])<<4 | hexDigit(s[3])
		b = hexDigit(s[4])<<4 | hexDigit(s[5])
	}
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

func hexDigit(c byte) uint8 {
	switch {
	case c >= '0' && c <= '9':
		return c - '0'
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10
	default:
		return 0
	}
}

// Color definitions matching design.go
var (
	// Backgrounds
	colorBackground  = hex("#1a1b26")
	colorTopBar      = hex("#16161e")
	colorCardBg      = hex("#1f2335")
	colorCardBgHover = hex("#28344a")

	// Buttons
	colorBtnNormal = hex("#24283b")
	colorBtnHover  = hex("#3d59a1")

	// Accents
	colorAccent        = hex("#7aa2f7")
	colorAccentHover   = hex("#7dcfff")
	colorAccentPressed = hex("#3d59a1")

	// Text
	colorTextPrimary   = hex("#a9b1d6")
	colorTextSecondary = hex("#787c99")
	colorTextMuted     = hex("#565f89")
	colorTextDark      = hex("#1a1b26")

	// Semantic
	colorGreen  = hex("#9ece6a")
	colorRed    = hex("#f7768e")
	colorYellow = hex("#e0af68")

	// Variants
	colorGreenDark   = hex("#1e362a")
	colorGreenHover  = hex("#264634")
	colorGreenBright = hex("#b9e08c")
	colorGreenPress  = hex("#70a050")

	colorRedDark   = hex("#402025")
	colorRedBright = hex("#ff90a0")
	colorRedPress  = hex("#c05060")

	colorNotchExpanded  = hex("#2a2e45")
	colorNotchCollapsed = hex("#414868")
	colorTeal           = hex("#73daca")

	// White
	colorWhite = hex("#ffffff")
)

// colorMap maps Tailwind color classes to actual colors.
var colorMap = map[string]color.RGBA{
	// Background colors (bg-*)
	"bg-background":     colorBackground,
	"bg-topbar":         colorTopBar,
	"bg-card":           colorCardBg,
	"bg-card-hover":     colorCardBgHover,
	"bg-btn":            colorBtnNormal,
	"bg-btn-hover":      colorBtnHover,
	"bg-accent":         colorAccent,
	"bg-accent-hover":   colorAccentHover,
	"bg-accent-pressed": colorAccentPressed,
	"bg-success":        colorGreen,
	"bg-success-dark":   colorGreenDark,
	"bg-success-hover":  colorGreenHover,
	"bg-success-bright": colorGreenBright,
	"bg-danger":         colorRed,
	"bg-danger-dark":    colorRedDark,
	"bg-danger-bright":  colorRedBright,
	"bg-warning":        colorYellow,
	"bg-notch":          colorNotchExpanded,
	"bg-notch-collapsed": colorNotchCollapsed,
	"bg-teal":           colorTeal,
	"bg-white":          colorWhite,

	// Text colors (text-*)
	"text-primary":   colorTextPrimary,
	"text-secondary": colorTextSecondary,
	"text-muted":     colorTextMuted,
	"text-dark":      colorTextDark,
	"text-accent":    colorAccent,
	"text-success":   colorGreen,
	"text-danger":    colorRed,
	"text-warning":   colorYellow,
	"text-teal":      colorTeal,
	"text-white":     colorWhite,
}

// GetColor returns a color by name, or nil if not found.
func GetColor(name string) *color.RGBA {
	if c, ok := colorMap[name]; ok {
		return &c
	}
	return nil
}
