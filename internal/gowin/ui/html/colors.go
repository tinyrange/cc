package html

import "image/color"

// Tokyo Night color palette for CSS class class mapping.
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

// =============================================================================
// CRUMBLECRACKER FRUITY PALETTE
// Light theme with warm cream base and fruity accents
// =============================================================================

var (
	// Background colors - warm cream base
	colorCanvas    = hex("#FFFBF7")
	colorCanvasAlt = hex("#FFF8F2")
	colorSurface   = hex("#FFFFFF")

	// Bars
	colorBar       = hex("#FDFAF8")
	colorBarBorder = hex("#F0EAE4")

	// Tab colors
	colorTabInactive = hex("#F7F3F0")
	colorTabHover    = hex("#FFEDD5")
	colorTabActive   = hex("#FFFFFF")

	// Borders
	colorBorderLight  = hex("#F0EBE6")
	colorBorderMedium = hex("#E8E2DC")
)

// Mango (orange) color scale
var mangoColors = map[string]color.RGBA{
	"mango-50":  hex("#FFF8E7"),
	"mango-100": hex("#FFECCC"),
	"mango-200": hex("#FFD999"),
	"mango-300": hex("#FFC266"),
	"mango-400": hex("#FFAB33"),
	"mango-500": hex("#FF9500"),
	"mango-600": hex("#E88600"),
	"mango-700": hex("#CC7700"),
}

// Lime (green) color scale
var limeColors = map[string]color.RGBA{
	"lime-50":  hex("#F0FFF4"),
	"lime-100": hex("#DCFFE4"),
	"lime-200": hex("#B8FFCA"),
	"lime-300": hex("#7AE99A"),
	"lime-400": hex("#4ADE80"),
	"lime-500": hex("#22C55E"),
	"lime-600": hex("#16A34A"),
	"lime-700": hex("#15803D"),
}

// Berry (pink) color scale
var berryColors = map[string]color.RGBA{
	"berry-50":  hex("#FFF0F6"),
	"berry-100": hex("#FFE0EB"),
	"berry-200": hex("#FFC2D9"),
	"berry-300": hex("#FF8FBC"),
	"berry-400": hex("#FF5C9D"),
	"berry-500": hex("#F43F7A"),
	"berry-600": hex("#DB2777"),
	"berry-700": hex("#BE185D"),
}

// Grape (purple) color scale
var grapeColors = map[string]color.RGBA{
	"grape-50":  hex("#F5F3FF"),
	"grape-100": hex("#EDE9FE"),
	"grape-200": hex("#DDD6FE"),
	"grape-300": hex("#C4B5FD"),
	"grape-400": hex("#A78BFA"),
	"grape-500": hex("#8B5CF6"),
	"grape-600": hex("#7C3AED"),
	"grape-700": hex("#6D28D9"),
}

// Citrus (yellow) color scale
var citrusColors = map[string]color.RGBA{
	"citrus-50":  hex("#FEFCE8"),
	"citrus-100": hex("#FEF9C3"),
	"citrus-200": hex("#FEF08A"),
	"citrus-300": hex("#FDE047"),
	"citrus-400": hex("#FACC15"),
	"citrus-500": hex("#EAB308"),
	"citrus-600": hex("#CA8A04"),
	"citrus-700": hex("#A16207"),
}

// Ocean (cyan) color scale
var oceanColors = map[string]color.RGBA{
	"ocean-50":  hex("#ECFEFF"),
	"ocean-100": hex("#CFFAFE"),
	"ocean-200": hex("#A5F3FC"),
	"ocean-300": hex("#67E8F9"),
	"ocean-400": hex("#22D3EE"),
	"ocean-500": hex("#06B6D4"),
	"ocean-600": hex("#0891B2"),
	"ocean-700": hex("#0E7490"),
}

// Ink (neutral text) color scale
var inkColors = map[string]color.RGBA{
	"ink-200": hex("#E7E5E4"),
	"ink-300": hex("#D6D3D1"),
	"ink-400": hex("#A8A29E"),
	"ink-500": hex("#78716C"),
	"ink-600": hex("#57534E"),
	"ink-700": hex("#44403C"),
	"ink-800": hex("#292524"),
	"ink-900": hex("#1C1917"),
}

// colorMap maps CSS class color classes to actual colors.
var colorMap = map[string]color.RGBA{
	// Background colors (bg-*) - Tokyo Night
	"bg-background":      colorBackground,
	"bg-topbar":          colorTopBar,
	"bg-card":            colorCardBg,
	"bg-card-hover":      colorCardBgHover,
	"bg-btn":             colorBtnNormal,
	"bg-btn-hover":       colorBtnHover,
	"bg-accent":          colorAccent,
	"bg-accent-hover":    colorAccentHover,
	"bg-accent-pressed":  colorAccentPressed,
	"bg-success":         colorGreen,
	"bg-success-dark":    colorGreenDark,
	"bg-success-hover":   colorGreenHover,
	"bg-success-bright":  colorGreenBright,
	"bg-danger":          colorRed,
	"bg-danger-dark":     colorRedDark,
	"bg-danger-bright":   colorRedBright,
	"bg-warning":         colorYellow,
	"bg-notch":           colorNotchExpanded,
	"bg-notch-collapsed": colorNotchCollapsed,
	"bg-teal":            colorTeal,
	"bg-white":           colorWhite,

	// Background colors (bg-*) - CrumbleCracker Light
	"bg-canvas":        colorCanvas,
	"bg-canvas-alt":    colorCanvasAlt,
	"bg-surface":       colorSurface,
	"bg-surface-muted": hex("#FEFCFA"),
	"bg-bar":           colorBar,
	"bg-bar-border":    colorBarBorder,
	"bg-tab-inactive":  colorTabInactive,
	"bg-tab-hover":     colorTabHover,
	"bg-tab-active":    colorTabActive,
	"bg-border-light":  colorBorderLight,
	"bg-border-medium": colorBorderMedium,

	// Text colors (text-*) - Tokyo Night
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

// init adds all color scale entries to colorMap
func init() {
	// Add mango colors
	for name, c := range mangoColors {
		colorMap["bg-"+name] = c
		colorMap["text-"+name] = c
		colorMap["border-"+name] = c
		colorMap["from-"+name] = c
		colorMap["via-"+name] = c
		colorMap["to-"+name] = c
	}
	// Add lime colors
	for name, c := range limeColors {
		colorMap["bg-"+name] = c
		colorMap["text-"+name] = c
		colorMap["border-"+name] = c
		colorMap["from-"+name] = c
		colorMap["via-"+name] = c
		colorMap["to-"+name] = c
	}
	// Add berry colors
	for name, c := range berryColors {
		colorMap["bg-"+name] = c
		colorMap["text-"+name] = c
		colorMap["border-"+name] = c
		colorMap["from-"+name] = c
		colorMap["via-"+name] = c
		colorMap["to-"+name] = c
	}
	// Add grape colors
	for name, c := range grapeColors {
		colorMap["bg-"+name] = c
		colorMap["text-"+name] = c
		colorMap["border-"+name] = c
		colorMap["from-"+name] = c
		colorMap["via-"+name] = c
		colorMap["to-"+name] = c
	}
	// Add citrus colors
	for name, c := range citrusColors {
		colorMap["bg-"+name] = c
		colorMap["text-"+name] = c
		colorMap["border-"+name] = c
		colorMap["from-"+name] = c
		colorMap["via-"+name] = c
		colorMap["to-"+name] = c
	}
	// Add ocean colors
	for name, c := range oceanColors {
		colorMap["bg-"+name] = c
		colorMap["text-"+name] = c
		colorMap["border-"+name] = c
		colorMap["from-"+name] = c
		colorMap["via-"+name] = c
		colorMap["to-"+name] = c
	}
	// Add ink colors
	for name, c := range inkColors {
		colorMap["bg-"+name] = c
		colorMap["text-"+name] = c
		colorMap["border-"+name] = c
	}
}

// GetColor returns a color by name, or nil if not found.
func GetColor(name string) *color.RGBA {
	if c, ok := colorMap[name]; ok {
		return &c
	}
	return nil
}
