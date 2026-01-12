package main

import (
	"image/color"

	"github.com/tinyrange/cc/internal/gowin/ui"
	"github.com/tinyrange/cc/internal/term"
)

// =============================================================================
// COLOR PARSING HELPER
// =============================================================================

// hex parses a CSS hex color code and returns a color.RGBA.
// Supports formats: "#rgb", "#rrggbb", "rgb", "rrggbb"
// Alpha is always set to 255 (fully opaque).
func hex(s string) color.RGBA {
	// Strip leading # if present
	if len(s) > 0 && s[0] == '#' {
		s = s[1:]
	}

	var r, g, b uint8
	switch len(s) {
	case 3: // #rgb -> #rrggbb
		r = hexDigit(s[0]) * 17
		g = hexDigit(s[1]) * 17
		b = hexDigit(s[2]) * 17
	case 6: // #rrggbb
		r = hexDigit(s[0])<<4 | hexDigit(s[1])
		g = hexDigit(s[2])<<4 | hexDigit(s[3])
		b = hexDigit(s[4])<<4 | hexDigit(s[5])
	}
	return color.RGBA{R: r, G: g, B: b, A: 255}
}

// hexAlpha parses a CSS hex color code with a custom alpha value.
func hexAlpha(s string, alpha uint8) color.RGBA {
	c := hex(s)
	c.A = alpha
	return c
}

// hexDigit converts a single hex character to its numeric value.
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

// =============================================================================
// TOKYO NIGHT COLOR PALETTE
// https://github.com/enkia/tokyo-night-vscode-theme
// =============================================================================

// Background colors
var (
	colorBackground  = hex("#1a1b26")
	colorTopBar      = hex("#16161e")
	colorCardBg      = hex("#1f2335")
	colorCardBgHover = hex("#28344a") // selection
	colorOverlay     = hexAlpha("#1a1b26", 220)
)

// Button colors
var (
	colorBtnNormal = hex("#24283b") // storm
	colorBtnHover  = hex("#3d59a1") // active border
)

// Accent colors
var (
	colorAccent        = hex("#7aa2f7") // blue
	colorAccentHover   = hex("#7dcfff") // cyan
	colorAccentPressed = hex("#3d59a1")
)

// Text colors
var (
	colorTextPrimary   = hex("#a9b1d6") // foreground
	colorTextSecondary = hex("#787c99")
	colorTextMuted     = hex("#565f89") // dimmer
	colorTextDark      = hex("#1a1b26") // dark text on light bg
)

// Semantic colors
var (
	colorGreen  = hex("#9ece6a")
	colorRed    = hex("#f7768e")
	colorYellow = hex("#e0af68")
)

// Contextual/variant colors
var (
	// Green variants
	colorGreenDark   = hex("#1e362a") // Dark green background
	colorGreenHover  = hex("#264634") // Green hover
	colorGreenBright = hex("#b9e08c") // Bright green
	colorGreenPress  = hex("#70a050") // Green pressed

	// Red variants
	colorRedDark   = hex("#402025") // Dark red background
	colorRedBright = hex("#ff90a0") // Bright red hover
	colorRedPress  = hex("#c05060") // Red pressed
	colorRedTint   = hex("#291b23") // Red tint background

	// Notch colors
	colorNotchExpanded  = hex("#2a2e45") // Expanded notch bg
	colorNotchCollapsed = hex("#414868") // Collapsed notch indicator
	colorTeal           = hex("#73daca") // Teal for network hover

	// Button variants (for terminal exit confirmation)
	colorBtnAlt        = hex("#363e5a")
	colorBtnAltPressed = hex("#28344a")
)

// Overlay alpha values
const (
	overlayAlphaLight  uint8 = 180
	overlayAlphaMedium uint8 = 200
	overlayAlphaHeavy  uint8 = 220
)

// =============================================================================
// UI DIMENSIONS
// =============================================================================

// Corner radii
const (
	cornerRadiusSmall  float32 = 6
	cornerRadiusMedium float32 = 10
	cornerRadiusLarge  float32 = 12
)

// Top bar dimensions
const (
	topBarButtonHeight float32 = 26
	topBarIconSize     float32 = 14
)

// Bundle card dimensions
const (
	cardWidth   float32 = 180
	cardHeight  float32 = 270
	cardPadding float32 = 12
	imageHeight float32 = 120
	buttonSize  float32 = 26
)

// Dialog dimensions - CustomVM
const (
	customVMDialogWidth  float32 = 500
	customVMDialogHeight float32 = 380
)

// Dialog dimensions - Settings
const (
	settingsDialogWidth  float32 = 600
	settingsDialogHeight float32 = 620
	settingsLabelWidth   float32 = 100
)

// Dialog dimensions - Delete confirmation
const (
	deleteDialogWidth  float32 = 400
	deleteDialogHeight float32 = 180
)

// Dialog dimensions - Exit confirmation (terminal)
const (
	exitDialogWidth  float32 = 280
	exitDialogHeight float32 = 120
)

// Notch dimensions
const (
	notchAnimSpeed float32 = 10.0
	notchHoverW    float32 = 180
	notchHoverH    float32 = 50
)

// =============================================================================
// TEXT SIZES
// =============================================================================

// Text sizes are untyped constants so they can be used with float64 fields
const (
	textSizeTitle   = 48 // Main title
	textSizeLarge   = 28 // Loading screen title
	textSizeHeading = 24 // Dialog titles
	textSizeSection = 20 // Section headers
	textSizeMedium  = 16 // Bundle names
	textSizeBody    = 15 // Button text
	textSizeLabel   = 14 // Labels
	textSizeSmall   = 13 // Top bar buttons
	textSizeCaption = 12 // Captions, small buttons
	textSizeTiny    = 11 // Version labels
)

// =============================================================================
// BUTTON STYLES
// =============================================================================

// topBarButtonStyle returns the standard style for top bar buttons (text only)
func topBarButtonStyle() ui.ButtonStyle {
	return ui.ButtonStyle{
		BackgroundNormal:   colorBtnNormal,
		BackgroundHovered:  colorBtnHover,
		BackgroundPressed:  colorAccentPressed,
		BackgroundDisabled: colorTopBar,
		TextColor:          colorTextPrimary,
		TextSize:           textSizeSmall,
		Padding:            ui.Symmetric(10, 6),
		MinWidth:           60,
		MinHeight:          topBarButtonHeight,
		CornerRadius:       cornerRadiusSmall,
	}
}

// primaryButtonStyle returns the style for primary action buttons (blue accent)
func primaryButtonStyle() ui.ButtonStyle {
	return ui.ButtonStyle{
		BackgroundNormal:   colorAccent,
		BackgroundHovered:  colorAccentHover,
		BackgroundPressed:  colorAccentPressed,
		BackgroundDisabled: colorNotchCollapsed, // #414868
		TextColor:          colorTextDark,
		TextSize:           textSizeBody,
		Padding:            ui.Symmetric(20, 12),
		MinWidth:           120,
		MinHeight:          44,
		CornerRadius:       cornerRadiusSmall,
	}
}

// secondaryButtonStyle returns the style for secondary action buttons
func secondaryButtonStyle() ui.ButtonStyle {
	return ui.ButtonStyle{
		BackgroundNormal:   colorBtnNormal,
		BackgroundHovered:  colorBtnHover,
		BackgroundPressed:  colorAccentPressed,
		BackgroundDisabled: colorTopBar,
		TextColor:          colorTextPrimary,
		TextSize:           textSizeBody,
		Padding:            ui.Symmetric(20, 12),
		MinWidth:           120,
		MinHeight:          44,
		CornerRadius:       cornerRadiusSmall,
	}
}

// notchButtonStyle returns style for notch buttons
func notchButtonStyle() ui.ButtonStyle {
	return ui.ButtonStyle{
		BackgroundNormal:   colorBtnNormal,
		BackgroundHovered:  colorBtnHover,
		BackgroundPressed:  colorAccentPressed,
		BackgroundDisabled: colorBtnNormal,
		TextColor:          color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 255}, // white text
		TextSize:           textSizeCaption,
		Padding:            ui.Symmetric(10, 5),
		MinWidth:           50,
		MinHeight:          24,
		CornerRadius:       cornerRadiusSmall,
	}
}

// notchExitButtonStyle returns style for the exit button (red on hover)
func notchExitButtonStyle() ui.ButtonStyle {
	style := notchButtonStyle()
	style.BackgroundHovered = colorRed
	return style
}

// notchNetButtonStyle returns style for the network button
func notchNetButtonStyle(connected bool) ui.ButtonStyle {
	style := notchButtonStyle()
	if connected {
		style.BackgroundNormal = colorGreen
		style.BackgroundHovered = colorTeal
	} else {
		style.BackgroundNormal = colorTextMuted
		style.BackgroundHovered = colorBtnHover
	}
	return style
}

// dangerButtonStyle returns style for dangerous/destructive actions
func dangerButtonStyle() ui.ButtonStyle {
	return ui.ButtonStyle{
		BackgroundNormal:   colorRed,
		BackgroundHovered:  colorRedBright,
		BackgroundPressed:  colorRedPress,
		BackgroundDisabled: colorRedDark,
		TextColor:          colorTextDark,
		TextSize:           textSizeLabel,
		Padding:            ui.Symmetric(20, 12),
		MinWidth:           90,
		MinHeight:          36,
		CornerRadius:       cornerRadiusSmall,
	}
}

// startButtonStyle returns style for the green Start button on bundle cards
func startButtonStyle() ui.ButtonStyle {
	return ui.ButtonStyle{
		BackgroundNormal:  colorGreen,
		BackgroundHovered: colorGreenBright,
		BackgroundPressed: colorGreenPress,
		TextColor:         colorBackground,
		TextSize:          textSizeCaption,
		Padding:           ui.Symmetric(12, 4),
		MinWidth:          60,
		MinHeight:         buttonSize,
		CornerRadius:      cornerRadiusSmall,
	}
}

// =============================================================================
// CARD STYLES
// =============================================================================

// iconButtonCardStyle returns a CardStyle matching the top bar button style
func iconButtonCardStyle() ui.CardStyle {
	return ui.CardStyle{
		BackgroundColor: colorBtnNormal,
		BorderColor:     color.Transparent,
		BorderWidth:     0,
		Padding:         ui.Symmetric(10, 6),
		CornerRadius:    cornerRadiusSmall,
	}
}

// iconButtonCardHoverStyle returns a hover CardStyle for icon buttons
func iconButtonCardHoverStyle() ui.CardStyle {
	return ui.CardStyle{
		BackgroundColor: colorBtnHover,
		BorderColor:     color.Transparent,
		BorderWidth:     0,
		Padding:         ui.Symmetric(10, 6),
		CornerRadius:    cornerRadiusSmall,
	}
}

// bundleCardStyle returns the style for bundle cards
func bundleCardStyle() ui.CardStyle {
	return ui.CardStyle{
		BackgroundColor: colorCardBg,
		BorderColor:     color.Transparent,
		BorderWidth:     0,
		Padding:         ui.All(cardPadding),
		CornerRadius:    cornerRadiusMedium,
	}
}

// bundleImageCardStyle returns the style for the image area in bundle cards
func bundleImageCardStyle() ui.CardStyle {
	return ui.CardStyle{
		BackgroundColor: colorTopBar,
		CornerRadius:    cornerRadiusSmall,
	}
}

// bundleImageCardHoverStyle returns the hover style for the image area
func bundleImageCardHoverStyle() ui.CardStyle {
	return ui.CardStyle{
		BackgroundColor: colorBtnHover,
		CornerRadius:    cornerRadiusSmall,
	}
}

// cogButtonStyle returns the style for the settings cog button
func cogButtonStyle() ui.CardStyle {
	return ui.CardStyle{
		BackgroundColor: colorBtnNormal,
		Padding:         ui.All(6),
		CornerRadius:    cornerRadiusSmall,
	}
}

// cogButtonHoverStyle returns the hover style for the settings cog button
func cogButtonHoverStyle() ui.CardStyle {
	return ui.CardStyle{
		BackgroundColor: colorBtnHover,
		Padding:         ui.All(6),
		CornerRadius:    cornerRadiusSmall,
	}
}

// addVMCardStyle returns the style for the Add VM button card
func addVMCardStyle() ui.CardStyle {
	return ui.CardStyle{
		BackgroundColor: colorGreen,
		Padding:         ui.Symmetric(24, 14),
		CornerRadius:    cornerRadiusMedium,
	}
}

// addVMCardHoverStyle returns the hover style for the Add VM button
func addVMCardHoverStyle() ui.CardStyle {
	return ui.CardStyle{
		BackgroundColor: colorGreenBright,
		Padding:         ui.Symmetric(24, 14),
		CornerRadius:    cornerRadiusMedium,
	}
}

// updateButtonCardStyle returns the style for the update notification button
func updateButtonCardStyle() ui.CardStyle {
	return ui.CardStyle{
		BackgroundColor: colorGreenDark,
		CornerRadius:    cornerRadiusSmall,
		Padding:         ui.Symmetric(10, 6),
	}
}

// updateButtonCardHoverStyle returns the hover style for the update button
func updateButtonCardHoverStyle() ui.CardStyle {
	return ui.CardStyle{
		BackgroundColor: colorGreenHover,
		CornerRadius:    cornerRadiusSmall,
		Padding:         ui.Symmetric(10, 6),
	}
}

// =============================================================================
// PROGRESS BAR STYLES
// =============================================================================

// progressBarStyle returns the standard progress bar style
func progressBarStyle(fillColor color.Color) ui.ProgressBarStyle {
	return ui.ProgressBarStyle{
		BackgroundColor: colorBtnNormal,
		FillColor:       fillColor,
		TextColor:       colorTextPrimary,
		Height:          8,
		CornerRadius:    4,
		ShowPercentage:  false,
		TextSize:        textSizeCaption,
	}
}

// progressBarSmallStyle returns a smaller progress bar style
func progressBarSmallStyle() ui.ProgressBarStyle {
	return ui.ProgressBarStyle{
		BackgroundColor: colorBtnNormal,
		FillColor:       colorAccent,
		TextColor:       colorTextPrimary,
		Height:          6,
		CornerRadius:    3,
		ShowPercentage:  false,
		TextSize:        textSizeCaption,
	}
}

// =============================================================================
// NOTCH STYLES
// =============================================================================

// notchExpandedCardStyle returns the style for the expanded notch
func notchExpandedCardStyle() ui.CardStyle {
	return ui.CardStyle{
		BackgroundColor: colorNotchExpanded,
		Padding:         ui.Symmetric(10, 8),
		CornerRadius:    20, // pill-like
	}
}

// notchCollapsedCardStyle returns the style for the collapsed notch
func notchCollapsedCardStyle() ui.CardStyle {
	return ui.CardStyle{
		BackgroundColor: colorNotchCollapsed,
		Padding:         ui.All(0),
		CornerRadius:    0, // square corners
	}
}

// =============================================================================
// DIALOG STYLES
// =============================================================================

// dialogOverlayColor returns the overlay color for dialogs
func dialogOverlayColor(alpha uint8) color.RGBA {
	return color.RGBA{R: 0x1a, G: 0x1b, B: 0x26, A: alpha}
}

// dialogCardStyle returns the style for dialog cards
func dialogCardStyle(width, height float32) ui.CardStyle {
	return ui.CardStyle{
		BackgroundColor: colorCardBg,
		CornerRadius:    cornerRadiusLarge,
	}
}

// =============================================================================
// TAB BAR STYLES
// =============================================================================

// tabBarStyle returns the standard tab bar style for dialogs
func tabBarStyle() ui.TabBarStyle {
	return ui.TabBarStyle{
		BackgroundColor:    color.RGBA{R: 0, G: 0, B: 0, A: 0}, // Transparent
		TextColor:          colorTextSecondary,
		TextColorSelected:  colorTextPrimary,
		TextColorHovered:   colorTextPrimary,
		TextSize:           textSizeLabel,
		TabPadding:         ui.Symmetric(20, 10),
		TabGap:             0,
		UnderlineColor:     colorAccent,
		UnderlineThickness: 2,
		UnderlineInset:     0,
		Height:             36,
	}
}

// =============================================================================
// TERMINAL COLOR SCHEME
// =============================================================================

// tokyoNightTerminalPalette is the 16-color ANSI palette for the terminal.
// Colors 0-7 are normal, 8-15 are bright variants.
var tokyoNightTerminalPalette = []color.RGBA{
	// Normal colors (0-7)
	{R: 0x15, G: 0x16, B: 0x1e, A: 255}, // 0: black
	{R: 0xf7, G: 0x76, B: 0x8e, A: 255}, // 1: red
	{R: 0x9e, G: 0xce, B: 0x6a, A: 255}, // 2: green
	{R: 0xe0, G: 0xaf, B: 0x68, A: 255}, // 3: yellow
	{R: 0x7a, G: 0xa2, B: 0xf7, A: 255}, // 4: blue
	{R: 0xbb, G: 0x9a, B: 0xf7, A: 255}, // 5: magenta
	{R: 0x7d, G: 0xcf, B: 0xff, A: 255}, // 6: cyan
	{R: 0xa9, G: 0xb1, B: 0xd6, A: 255}, // 7: white

	// Bright colors (8-15)
	{R: 0x41, G: 0x48, B: 0x68, A: 255}, // 8: bright black
	{R: 0xf7, G: 0x76, B: 0x8e, A: 255}, // 9: bright red
	{R: 0x9e, G: 0xce, B: 0x6a, A: 255}, // 10: bright green
	{R: 0xe0, G: 0xaf, B: 0x68, A: 255}, // 11: bright yellow
	{R: 0x7a, G: 0xa2, B: 0xf7, A: 255}, // 12: bright blue
	{R: 0xbb, G: 0x9a, B: 0xf7, A: 255}, // 13: bright magenta
	{R: 0x7d, G: 0xcf, B: 0xff, A: 255}, // 14: bright cyan
	{R: 0xc0, G: 0xca, B: 0xf5, A: 255}, // 15: bright white
}

// terminalColorScheme returns the Tokyo Night color scheme for the terminal.
func terminalColorScheme() term.ColorScheme {
	return term.ColorScheme{
		Foreground: hex("#a9b1d6"), // Default foreground
		Background: hex("#1a1b26"), // Default background
		Cursor:     hex("#c0caf5"), // Cursor color
		Selection:  hex("#41598b"), // Selection highlight
		Palette:    tokyoNightTerminalPalette,
	}
}
