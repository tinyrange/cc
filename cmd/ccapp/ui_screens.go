package main

import (
	"fmt"
	"image/color"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/tinyrange/cc/internal/assets"
	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/ui"
	"github.com/tinyrange/cc/internal/gowin/window"
	"github.com/tinyrange/cc/internal/oci"
)

// formatBytes formats a byte count as a human-readable string.
func formatBytes(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/float64(GB))
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/float64(MB))
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/float64(KB))
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

// Tokyo Night color theme
// Based on https://github.com/enkia/tokyo-night-vscode-theme
var (
	colorBackground    = color.RGBA{R: 0x1a, G: 0x1b, B: 0x26, A: 255} // #1a1b26
	colorTopBar        = color.RGBA{R: 0x16, G: 0x16, B: 0x1e, A: 255} // #16161e
	colorBtnNormal     = color.RGBA{R: 0x24, G: 0x28, B: 0x3b, A: 255} // #24283b (storm)
	colorBtnHover      = color.RGBA{R: 0x3d, G: 0x59, B: 0xa1, A: 255} // #3d59a1 (active border)
	colorCardBg        = color.RGBA{R: 0x1f, G: 0x23, B: 0x35, A: 255} // #1f2335
	colorCardBgHover   = color.RGBA{R: 0x28, G: 0x34, B: 0x4a, A: 255} // #28344a (selection)
	colorOverlay       = color.RGBA{R: 0x1a, G: 0x1b, B: 0x26, A: 220} // #1a1b26 with alpha
	colorAccent        = color.RGBA{R: 0x7a, G: 0xa2, B: 0xf7, A: 255} // #7aa2f7 (blue)
	colorAccentHover   = color.RGBA{R: 0x7d, G: 0xcf, B: 0xff, A: 255} // #7dcfff (cyan)
	colorAccentPressed = color.RGBA{R: 0x3d, G: 0x59, B: 0xa1, A: 255} // #3d59a1
	colorTextPrimary   = color.RGBA{R: 0xa9, G: 0xb1, B: 0xd6, A: 255} // #a9b1d6 (foreground)
	colorTextSecondary = color.RGBA{R: 0x78, G: 0x7c, B: 0x99, A: 255} // #787c99
	colorTextMuted     = color.RGBA{R: 0x56, G: 0x5f, B: 0x89, A: 255} // #565f89 (dimmer)
	colorGreen         = color.RGBA{R: 0x9e, G: 0xce, B: 0x6a, A: 255} // #9ece6a
	colorRed           = color.RGBA{R: 0xf7, G: 0x76, B: 0x8e, A: 255} // #f7768e
	colorYellow        = color.RGBA{R: 0xe0, G: 0xaf, B: 0x68, A: 255} // #e0af68
)

// UI constants
const (
	cornerRadiusSmall  float32 = 6
	cornerRadiusMedium float32 = 10
	topBarButtonHeight float32 = 26
	topBarIconSize     float32 = 14
)

// topBarButtonStyle returns the standard style for top bar buttons (text only)
func topBarButtonStyle() ui.ButtonStyle {
	return ui.ButtonStyle{
		BackgroundNormal:   colorBtnNormal,
		BackgroundHovered:  colorBtnHover,
		BackgroundPressed:  colorAccentPressed,
		BackgroundDisabled: color.RGBA{R: 0x16, G: 0x16, B: 0x1e, A: 255},
		TextColor:          colorTextPrimary,
		TextSize:           13,
		Padding:            ui.Symmetric(10, 6),
		MinWidth:           60,
		MinHeight:          topBarButtonHeight,
		CornerRadius:       cornerRadiusSmall,
	}
}

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

// primaryButtonStyle returns the style for primary action buttons (blue accent)
func primaryButtonStyle() ui.ButtonStyle {
	return ui.ButtonStyle{
		BackgroundNormal:   colorAccent,
		BackgroundHovered:  colorAccentHover,
		BackgroundPressed:  colorAccentPressed,
		BackgroundDisabled: color.RGBA{R: 0x41, G: 0x48, B: 0x68, A: 255}, // #414868
		TextColor:          color.RGBA{R: 0x1a, G: 0x1b, B: 0x26, A: 255}, // Dark text on light bg
		TextSize:           15,
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
		BackgroundDisabled: color.RGBA{R: 0x16, G: 0x16, B: 0x1e, A: 255},
		TextColor:          colorTextPrimary,
		TextSize:           15,
		Padding:            ui.Symmetric(20, 12),
		MinWidth:           120,
		MinHeight:          44,
		CornerRadius:       cornerRadiusSmall,
	}
}

// LauncherScreen manages the launcher UI state and widgets
type LauncherScreen struct {
	root *ui.Root
	app  *Application

	// Widgets that need updating
	bundleCards []*bundleCardWidget
	scrollView  *ui.ScrollView
	logo        *ui.AnimatedLogo

	// Icons
	iconPlus *graphics.SVG
	iconLogs *graphics.SVG
	iconCog  *graphics.SVG

	// State
	scrollX float32
}

// bundleCardWidget represents a single bundle card
type bundleCardWidget struct {
	card  *ui.Card
	index int
}

// NewLauncherScreen creates the launcher screen UI
func NewLauncherScreen(app *Application) *LauncherScreen {
	screen := &LauncherScreen{
		root: ui.NewRoot(app.text),
		app:  app,
	}

	// Load icons
	if icon, err := graphics.LoadSVG(app.window, assets.IconPlus); err != nil {
		slog.Warn("failed to load plus icon", "error", err)
	} else {
		screen.iconPlus = icon
	}
	if icon, err := graphics.LoadSVG(app.window, assets.IconLogs); err != nil {
		slog.Warn("failed to load logs icon", "error", err)
	} else {
		screen.iconLogs = icon
	}
	if icon, err := graphics.LoadSVG(app.window, assets.IconCog); err != nil {
		slog.Warn("failed to load cog icon", "error", err)
	} else {
		screen.iconCog = icon
	}

	screen.buildUI()
	return screen
}

func (s *LauncherScreen) buildUI() {
	// Create the logo widget
	if s.app.logo != nil {
		s.logo = ui.NewAnimatedLogo(s.app.logo).WithSize(400, 400)
	}

	// Main layout: Stack with background, logo, and content
	stack := ui.NewStack()

	// Background
	stack.AddChild(ui.NewBox(colorBackground))

	// Logo in bottom-right (only if we have bundles, otherwise it's too prominent)
	if s.logo != nil {
		stack.AddChild(ui.BottomRight(
			ui.NewPadding(s.logo, ui.Only(0, 0, -140, -140)), // Offset to position partly off-screen
		))
	}

	// Main content column
	contentCol := ui.Column()

	// Top bar with Debug Logs button
	topBar := s.buildTopBar()
	contentCol.AddChild(topBar, ui.DefaultFlexParams())

	// Title section
	titleSection := s.buildTitleSection()
	contentCol.AddChild(titleSection, ui.DefaultFlexParams())

	// Bundle cards section (only if bundles exist)
	if len(s.app.bundles) > 0 {
		bundleSection := s.buildBundleSection()
		contentCol.AddChild(bundleSection, ui.FlexParams(1))
	}

	// Add VM button section - prominent and centered below bundles
	addVMSection := s.buildAddVMSection()
	contentCol.AddChild(addVMSection, ui.DefaultFlexParams())

	stack.AddChild(contentCol)
	s.root.SetChild(stack)
}

func (s *LauncherScreen) buildTopBar() *ui.FlexContainer {
	row := ui.Row().
		WithBackground(colorTopBar).
		WithPadding(ui.Symmetric(16, 6)).
		WithCrossAlignment(ui.CrossAxisCenter)

	row.AddChild(ui.NewSpacer(), ui.FlexParams(1))

	// Debug Logs button with icon
	row.AddChild(
		s.buildIconButton("Logs", s.iconLogs, func() {
			s.app.openLogs()
		}),
		ui.DefaultFlexParams(),
	)

	return row
}

// buildIconButton creates a compact button with an icon and label
func (s *LauncherScreen) buildIconButton(label string, icon *graphics.SVG, onClick func()) ui.Widget {
	content := ui.Row().
		WithGap(5).
		WithCrossAlignment(ui.CrossAxisCenter)

	// Add icon if available
	if icon != nil {
		iconWidget := ui.NewSVGImage(icon).WithSize(topBarIconSize, topBarIconSize)
		content.AddChild(iconWidget, ui.DefaultFlexParams())
	}

	// Add label
	content.AddChild(ui.NewLabel(label).WithSize(13), ui.DefaultFlexParams())

	// Create card button with fixed height for alignment
	card := ui.NewCard(content).
		WithStyle(iconButtonCardStyle()).
		WithHoverStyle(iconButtonCardHoverStyle()).
		WithGraphicsWindow(s.app.window).
		OnClick(onClick)

	return card
}

func (s *LauncherScreen) buildTitleSection() *ui.FlexContainer {
	col := ui.Column().
		WithPadding(ui.Only(20, 50, 20, 0))

	col.AddChild(ui.NewLabel("CrumbleCracker").WithSize(48), ui.DefaultFlexParams())

	if len(s.app.bundles) == 0 {
		col.AddChild(
			ui.NewWrapLabel("No bundles found. Use the New VM button to add one.").WithSize(20),
			ui.FlexParamsWithMargin(0, ui.Only(0, 10, 0, 0)),
		)
		col.AddChild(
			ui.NewWrapLabel("Place bundles in the CrumbleCracker bundles directory to use them here.").WithSize(16),
			ui.FlexParamsWithMargin(0, ui.Only(0, 10, 0, 0)),
		)
		openBundlesStyle := secondaryButtonStyle()
		openBundlesStyle.MinWidth = 200
		col.AddChild(
			ui.NewButton("Open Bundles Folder").
				WithStyle(openBundlesStyle).
				WithGraphicsWindow(s.app.window).
				OnClick(func() {
					s.app.openBundlesDir()
				}),
			ui.FlexParamsWithMargin(0, ui.Only(0, 16, 0, 0)),
		)
	} else {
		col.AddChild(
			ui.NewLabel("Please select an environment to boot").WithSize(20),
			ui.FlexParamsWithMargin(0, ui.Only(0, 10, 0, 10)),
		)
	}

	return col
}

func (s *LauncherScreen) buildBundleSection() ui.Widget {
	// Stack with overlay and content
	stack := ui.NewStack()

	// Semi-transparent overlay
	stack.AddChild(ui.NewBox(colorOverlay).WithSize(0, 320)) // Height will be constrained

	// Horizontal scrollable card container
	cardContainer := s.buildCardContainer()
	s.scrollView = ui.NewScrollView(cardContainer).
		WithHorizontalOnly().
		WithScrollbarWidth(8)

	// Wrap in a column with fixed height and scrollbar below
	bundleCol := ui.Column().
		WithPadding(ui.Only(20, 0, 20, 20))

	bundleCol.AddChild(s.scrollView, ui.FlexParams(1))

	stack.AddChild(bundleCol)

	return stack
}

func (s *LauncherScreen) buildAddVMSection() *ui.FlexContainer {
	row := ui.Row().
		WithPadding(ui.Only(20, 20, 20, 30)).
		WithMainAlignment(ui.MainAxisCenter)

	// Button content with icon and text
	content := ui.Row().
		WithGap(10).
		WithCrossAlignment(ui.CrossAxisCenter)

	if s.iconPlus != nil {
		iconWidget := ui.NewSVGImage(s.iconPlus).WithSize(20, 20)
		content.AddChild(iconWidget, ui.DefaultFlexParams())
	}
	content.AddChild(ui.NewLabel("Add VM").WithSize(16).WithColor(colorBackground), ui.DefaultFlexParams())

	// Use Card for clickable content with icon - prominent green style
	card := ui.NewCard(content).
		WithStyle(ui.CardStyle{
			BackgroundColor: colorGreen,
			Padding:         ui.Symmetric(24, 14),
			CornerRadius:    cornerRadiusMedium,
		}).
		WithHoverStyle(ui.CardStyle{
			BackgroundColor: color.RGBA{R: 0xb9, G: 0xe0, B: 0x8c, A: 255}, // Lighter green on hover
			Padding:         ui.Symmetric(24, 14),
			CornerRadius:    cornerRadiusMedium,
		}).
		WithGraphicsWindow(s.app.window).
		OnClick(func() {
			s.app.customVMScreen = NewCustomVMScreen(s.app)
			s.app.mode = modeCustomVM
		})

	row.AddChild(card, ui.DefaultFlexParams())
	return row
}

func (s *LauncherScreen) buildCardContainer() *ui.FlexContainer {
	row := ui.Row().WithGap(24)

	s.bundleCards = nil
	for i, b := range s.app.bundles {
		card := s.buildBundleCard(i, b)
		s.bundleCards = append(s.bundleCards, card)
		row.AddChild(card.card, ui.DefaultFlexParams())
	}

	return row
}

func (s *LauncherScreen) buildBundleCard(index int, b discoveredBundle) *bundleCardWidget {
	name := b.Meta.Name
	if name == "" || name == "{{name}}" {
		name = filepath.Base(b.Dir)
	}
	desc := b.Meta.Description
	if desc == "" || desc == "{{description}}" {
		desc = "VM Bundle"
	}

	// Card dimensions and padding
	const cardWidth float32 = 180
	const cardHeight float32 = 270
	const cardPadding float32 = 12
	const contentWidth = cardWidth - (cardPadding * 2)
	const imageHeight float32 = 120
	const buttonSize float32 = 26

	// Card content: vertical layout with image area and text
	content := ui.Column().WithGap(8)

	// Image placeholder area - clickable to start VM
	imagePlaceholder := ui.NewCard(nil).
		WithStyle(ui.CardStyle{
			BackgroundColor: colorTopBar,
			CornerRadius:    cornerRadiusSmall,
		}).
		WithHoverStyle(ui.CardStyle{
			BackgroundColor: colorBtnHover,
			CornerRadius:    cornerRadiusSmall,
		}).
		WithGraphicsWindow(s.app.window).
		WithFixedSize(contentWidth, imageHeight).
		OnClick(func() {
			s.app.selectedIndex = index
			s.app.startBootBundle(index)
		})
	content.AddChild(imagePlaceholder, ui.DefaultFlexParams())

	// Name and description with better spacing
	content.AddChild(ui.NewWrapLabel(name).WithSize(16), ui.DefaultFlexParams())
	content.AddChild(ui.NewWrapLabel(desc).WithSize(13).WithColor(colorTextSecondary), ui.DefaultFlexParams())

	// Spacer to push buttons to bottom
	content.AddChild(ui.NewSpacer(), ui.FlexParams(1))

	// Bottom row with Start button on left, Settings button on right
	bottomRow := ui.Row().WithGap(8)

	// Start button (green)
	startBtn := ui.NewButton("Start").
		WithStyle(ui.ButtonStyle{
			BackgroundNormal:  colorGreen,
			BackgroundHovered: color.RGBA{R: 0xb9, G: 0xe0, B: 0x8c, A: 255},
			BackgroundPressed: color.RGBA{R: 0x70, G: 0xa0, B: 0x50, A: 255},
			TextColor:         colorBackground,
			TextSize:          12,
			Padding:           ui.Symmetric(12, 4),
			MinWidth:          60,
			MinHeight:         buttonSize,
			CornerRadius:      cornerRadiusSmall,
		}).
		WithGraphicsWindow(s.app.window).
		OnClick(func() {
			s.app.selectedIndex = index
			s.app.startBootBundle(index)
		})
	bottomRow.AddChild(startBtn, ui.DefaultFlexParams())

	// Spacer between buttons
	bottomRow.AddChild(ui.NewSpacer(), ui.FlexParams(1))

	// Settings cog button (icon only, fixed size)
	if s.iconCog != nil {
		cogContent := ui.NewSVGImage(s.iconCog).WithSize(14, 14)
		cogButton := ui.NewCard(cogContent).
			WithStyle(ui.CardStyle{
				BackgroundColor: colorBtnNormal,
				Padding:         ui.All(6),
				CornerRadius:    cornerRadiusSmall,
			}).
			WithHoverStyle(ui.CardStyle{
				BackgroundColor: colorBtnHover,
				Padding:         ui.All(6),
				CornerRadius:    cornerRadiusSmall,
			}).
			WithGraphicsWindow(s.app.window).
			WithFixedSize(buttonSize, buttonSize).
			OnClick(func() {
				s.app.showSettings(index)
			})
		bottomRow.AddChild(cogButton, ui.DefaultFlexParams())
	}
	content.AddChild(bottomRow, ui.DefaultFlexParams())

	// Outer card is just a visual container (NOT clickable)
	cardStyle := ui.CardStyle{
		BackgroundColor: colorCardBg,
		BorderColor:     color.Transparent,
		BorderWidth:     0,
		Padding:         ui.All(cardPadding),
		CornerRadius:    cornerRadiusMedium,
	}

	card := ui.NewCard(content).
		WithStyle(cardStyle).
		WithGraphicsWindow(s.app.window).
		WithFixedSize(cardWidth, cardHeight)

	return &bundleCardWidget{
		card:  card,
		index: index,
	}
}

func (s *LauncherScreen) Update(f graphics.Frame) {
	// Update logo animation
	if s.logo != nil {
		t := float32(time.Since(s.app.start).Seconds())
		s.logo.SetTime(t)
	}
	s.root.InvalidateLayout()
}

func (s *LauncherScreen) Render(f graphics.Frame) error {
	s.Update(f)
	s.root.Step(f, s.app.window.PlatformWindow())
	return nil
}

// RenderBackground renders the launcher without processing input events.
// Used when rendering the launcher as a background behind a dialog.
func (s *LauncherScreen) RenderBackground(f graphics.Frame) error {
	s.Update(f)
	s.root.DrawOnly(f)
	return nil
}

// LoadingScreen manages the loading UI state
type LoadingScreen struct {
	root          *ui.Root
	app           *Application
	logo          *ui.AnimatedLogo
	progressBar   *ui.ProgressBar
	progressLabel *ui.Label

	// Blob count progress
	blobProgressBar   *ui.ProgressBar
	blobProgressLabel *ui.Label
}

// NewLoadingScreen creates the loading screen UI
func NewLoadingScreen(app *Application) *LoadingScreen {
	screen := &LoadingScreen{
		root: ui.NewRoot(app.text),
		app:  app,
	}
	screen.buildUI()
	return screen
}

func (s *LoadingScreen) buildUI() {
	// Stack layout
	stack := ui.NewStack()

	// Background
	stack.AddChild(ui.NewBox(colorBackground))

	// Centered logo with larger size
	if s.app.logo != nil {
		s.logo = ui.NewAnimatedLogo(s.app.logo).
			WithSize(320, 320).
			WithSpeeds(0.9, -1.4, 2.2)
		stack.AddChild(ui.CenterCenter(s.logo))
	}

	// Loading status in a rounded card at top-left
	var msg string
	if s.app.mode == modeInstalling {
		msg = "Installing…"
		if s.app.installName != "" {
			msg = "Installing " + s.app.installName + "…"
		}
	} else {
		msg = "Booting VM…"
		if s.app.bootName != "" {
			msg = "Booting " + s.app.bootName + "…"
		}
	}

	// Create a column for the status card content
	cardContent := ui.Column().WithGap(8)
	cardContent.AddChild(ui.NewLabel(msg).WithSize(16).WithColor(colorTextPrimary), ui.DefaultFlexParams())

	// Blob count progress (overall progress)
	s.blobProgressLabel = ui.NewLabel("").WithSize(12).WithColor(colorTextSecondary)
	cardContent.AddChild(s.blobProgressLabel, ui.DefaultFlexParams())

	s.blobProgressBar = ui.NewProgressBar().
		WithMinWidth(280).
		WithStyle(ui.ProgressBarStyle{
			BackgroundColor: color.RGBA{R: 0x24, G: 0x28, B: 0x3b, A: 255},
			FillColor:       colorGreen,
			TextColor:       colorTextPrimary,
			Height:          6,
			CornerRadius:    3,
			ShowPercentage:  false,
			TextSize:        12,
		}).
		WithGraphicsWindow(s.app.window)
	cardContent.AddChild(s.blobProgressBar, ui.DefaultFlexParams())

	// Current blob progress (bytes downloaded)
	s.progressLabel = ui.NewLabel("").WithSize(12).WithColor(colorTextSecondary)
	cardContent.AddChild(s.progressLabel, ui.DefaultFlexParams())

	s.progressBar = ui.NewProgressBar().
		WithMinWidth(280).
		WithStyle(ui.ProgressBarStyle{
			BackgroundColor: color.RGBA{R: 0x24, G: 0x28, B: 0x3b, A: 255},
			FillColor:       colorAccent,
			TextColor:       colorTextPrimary,
			Height:          6,
			CornerRadius:    3,
			ShowPercentage:  false,
			TextSize:        12,
		}).
		WithGraphicsWindow(s.app.window)
	cardContent.AddChild(s.progressBar, ui.DefaultFlexParams())

	loadingCard := ui.NewCard(
		ui.NewPadding(cardContent, ui.Symmetric(16, 12)),
	).
		WithStyle(ui.CardStyle{
			BackgroundColor: colorTopBar,
			CornerRadius:    cornerRadiusSmall,
		}).
		WithGraphicsWindow(s.app.window)
	stack.AddChild(ui.TopLeft(ui.NewPadding(loadingCard, ui.All(24))))

	s.root.SetChild(stack)
}

func (s *LoadingScreen) Update(f graphics.Frame) {
	// Get the appropriate start time and progress based on mode
	var startTime time.Time
	var progress oci.DownloadProgress

	if s.app.mode == modeInstalling {
		startTime = s.app.installStarted
		s.app.installProgressMu.Lock()
		progress = s.app.installProgress
		s.app.installProgressMu.Unlock()
	} else {
		startTime = s.app.bootStarted
		s.app.bootProgressMu.Lock()
		progress = s.app.bootProgress
		s.app.bootProgressMu.Unlock()
	}

	if s.logo != nil {
		t := float32(time.Since(startTime).Seconds())
		s.logo.SetTime(t)
	}

	// Update blob count progress (overall progress)
	if progress.BlobCount > 0 {
		blobPercent := float64(progress.BlobIndex) / float64(progress.BlobCount)
		s.blobProgressBar.SetValue(blobPercent)
		s.blobProgressLabel.SetText(fmt.Sprintf("Downloading blob %d of %d", progress.BlobIndex+1, progress.BlobCount))
	}

	// Update current blob progress (bytes downloaded)
	if progress.Total > 0 {
		// Calculate progress percentage
		percent := float64(progress.Current) / float64(progress.Total)
		s.progressBar.SetValue(percent)

		// Format the label to show download status
		label := formatBytes(progress.Current) + " / " + formatBytes(progress.Total)
		s.progressLabel.SetText(label)
	} else if progress.Current > 0 {
		// Unknown total, show bytes downloaded
		s.progressBar.SetValue(0) // Indeterminate
		s.progressLabel.SetText("Downloading: " + formatBytes(progress.Current))
	}

	s.root.InvalidateLayout()
}

func (s *LoadingScreen) Render(f graphics.Frame) error {
	s.Update(f)
	s.root.Step(f, s.app.window.PlatformWindow())
	return nil
}

// ErrorScreen manages the error UI state
type ErrorScreen struct {
	root *ui.Root
	app  *Application
	logo *ui.AnimatedLogo
}

// NewErrorScreen creates the error screen UI
func NewErrorScreen(app *Application) *ErrorScreen {
	screen := &ErrorScreen{
		root: ui.NewRoot(app.text),
		app:  app,
	}
	screen.buildUI()
	return screen
}

func (s *ErrorScreen) buildUI() {
	stack := ui.NewStack()

	// Background
	stack.AddChild(ui.NewBox(colorBackground))

	// Subtle rotating logo in center (behind content)
	if s.app.logo != nil {
		s.logo = ui.NewAnimatedLogo(s.app.logo).
			WithSize(250, 250).
			WithSpeeds(0.4, 0, 0) // Only outer circle slowly rotates
		stack.AddChild(ui.CenterCenter(s.logo))
	}

	// Content column with better spacing
	content := ui.Column().WithPadding(ui.All(40))

	// Error header with color accent
	content.AddChild(ui.NewLabel("Error").WithSize(48).WithColor(colorRed), ui.DefaultFlexParams())

	// Error message in a rounded card
	msg := s.app.errMsg
	if msg == "" {
		msg = "unknown error"
	}
	errorCard := ui.NewCard(
		ui.NewPadding(
			ui.NewWrapLabel(msg).WithSize(16).WithColor(colorTextPrimary),
			ui.All(16),
		),
	).
		WithStyle(ui.CardStyle{
			BackgroundColor: color.RGBA{R: 0x29, G: 0x1b, B: 0x23, A: 255}, // Tokyo Night red tint
			CornerRadius:    cornerRadiusSmall,
		}).
		WithGraphicsWindow(s.app.window)
	content.AddChild(errorCard, ui.FlexParamsWithMargin(0, ui.Only(0, 24, 0, 0)))

	// Buttons with consistent styles
	buttonCol := ui.Column().WithGap(12)

	// Primary action button (blue accent)
	primaryStyle := primaryButtonStyle()
	primaryStyle.MinWidth = 280

	if s.app.fatalError {
		// Fatal error - show Quit button
		buttonCol.AddChild(
			ui.NewButton("Quit").
				WithStyle(primaryStyle).
				WithGraphicsWindow(s.app.window).
				OnClick(func() {
					os.Exit(1)
				}),
			ui.DefaultFlexParams(),
		)
	} else {
		// Recoverable error - show Back to Launcher button
		buttonCol.AddChild(
			ui.NewButton("Back to Launcher").
				WithStyle(primaryStyle).
				WithGraphicsWindow(s.app.window).
				OnClick(func() {
					s.app.errMsg = ""
					s.app.selectedIndex = -1
					s.app.mode = modeLauncher
				}),
			ui.DefaultFlexParams(),
		)
	}

	// Secondary action button (muted)
	secondaryStyle := secondaryButtonStyle()
	secondaryStyle.MinWidth = 280
	buttonCol.AddChild(
		ui.NewButton("Open Logs Directory").
			WithStyle(secondaryStyle).
			WithGraphicsWindow(s.app.window).
			OnClick(func() {
				s.app.openLogs()
			}),
		ui.DefaultFlexParams(),
	)

	content.AddChild(buttonCol, ui.FlexParamsWithMargin(0, ui.Only(0, 32, 0, 0)))

	stack.AddChild(content)

	s.root.SetChild(stack)
}

func (s *ErrorScreen) Update(f graphics.Frame) {
	if s.logo != nil {
		t := float32(time.Since(s.app.start).Seconds())
		s.logo.SetTime(t)
	}
	s.root.InvalidateLayout()
}

func (s *ErrorScreen) Render(f graphics.Frame) error {
	s.Update(f)
	s.root.Step(f, s.app.window.PlatformWindow())
	return nil
}

// TerminalScreen manages the terminal UI state
type TerminalScreen struct {
	root           *ui.Root
	app            *Application
	expandedCard   *ui.Card
	collapsedCard  *ui.Card
	exitBtn        *ui.Button
	netBtn         *ui.Button
	expanded       bool
	expandProgress float32
	lastUpdate     time.Time
	prevLeftDown   bool // For manual click detection
}

// Notch animation constants
const (
	notchAnimSpeed = float32(10.0) // speed of expand/collapse
)

// NewTerminalScreen creates the terminal screen UI (notch bar)
func NewTerminalScreen(app *Application) *TerminalScreen {
	screen := &TerminalScreen{
		root: ui.NewRoot(app.text),
		app:  app,
	}
	screen.buildUI()
	return screen
}

// notchButtonStyle returns style for notch buttons
func notchButtonStyle() ui.ButtonStyle {
	return ui.ButtonStyle{
		BackgroundNormal:   colorBtnNormal,
		BackgroundHovered:  colorBtnHover,
		BackgroundPressed:  colorAccentPressed,
		BackgroundDisabled: colorBtnNormal,
		TextColor:          color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 255}, // white text
		TextSize:           12,
		Padding:            ui.Symmetric(10, 5),
		MinWidth:           50,
		MinHeight:          24,
		CornerRadius:       6,
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
		style.BackgroundHovered = color.RGBA{R: 0x73, G: 0xda, B: 0xca, A: 255} // teal hover
	} else {
		style.BackgroundNormal = color.RGBA{R: 0x56, G: 0x5f, B: 0x89, A: 255} // gray
		style.BackgroundHovered = colorBtnHover
	}
	return style
}

func (s *TerminalScreen) buildUI() {
	// Create buttons for expanded state
	s.exitBtn = ui.NewButton("Exit").
		WithStyle(notchExitButtonStyle()).
		WithGraphicsWindow(s.app.window).
		OnClick(func() {
			s.app.showExitConfirm = true
		})

	s.netBtn = ui.NewButton("Net").
		WithStyle(notchNetButtonStyle(true)).
		WithGraphicsWindow(s.app.window).
		OnClick(func() {
			s.app.networkDisabled = !s.app.networkDisabled
			if s.app.running != nil && s.app.running.netBackend != nil {
				s.app.running.netBackend.SetInternetAccessEnabled(!s.app.networkDisabled)
			}
			// Update button style
			if s.app.networkDisabled {
				s.netBtn.WithStyle(notchNetButtonStyle(false))
				s.netBtn.SetText("Off")
			} else {
				s.netBtn.WithStyle(notchNetButtonStyle(true))
				s.netBtn.SetText("Net")
			}
			slog.Info("internet access toggled", "disabled", s.app.networkDisabled)
		})

	// Button row inside the expanded notch
	buttonRow := ui.Row().
		WithCrossAlignment(ui.CrossAxisCenter).
		WithGap(8).
		AddChild(s.exitBtn, ui.DefaultFlexParams()).
		AddChild(s.netBtn, ui.DefaultFlexParams())

	// Expanded notch card with buttons
	expandedStyle := ui.CardStyle{
		BackgroundColor: color.RGBA{R: 0x2a, G: 0x2e, B: 0x45, A: 255}, // slightly lighter when expanded
		Padding:         ui.Symmetric(10, 8),
		CornerRadius:    20, // pill-like rounded corners
	}

	s.expandedCard = ui.NewCard(buttonRow).
		WithStyle(expandedStyle).
		WithGraphicsWindow(s.app.window)

	// Collapsed notch card (small indicator bar)
	collapsedStyle := ui.CardStyle{
		BackgroundColor: color.RGBA{R: 0x41, G: 0x48, B: 0x68, A: 255}, // visible purple-gray
		Padding:         ui.All(0),
		CornerRadius:    0, // square corners
	}

	s.collapsedCard = ui.NewCard(nil).
		WithStyle(collapsedStyle).
		WithGraphicsWindow(s.app.window).
		WithFixedSize(60, 10) // small pill

	s.rebuildLayout()
}

func (s *TerminalScreen) rebuildLayout() {
	// Choose which card to show based on expanded state
	var notchWidget ui.Widget
	if s.expanded {
		notchWidget = s.expandedCard
	} else {
		notchWidget = s.collapsedCard
	}

	// Center the notch at the top
	topRow := ui.Row().
		WithCrossAlignment(ui.CrossAxisCenter).
		AddChild(ui.NewSpacer(), ui.FlexParams(1)).
		AddChild(notchWidget, ui.DefaultFlexParams()).
		AddChild(ui.NewSpacer(), ui.FlexParams(1))

	// Wrap in column
	col := ui.Column()
	col.AddChild(topRow, ui.DefaultFlexParams())
	col.AddChild(ui.NewSpacer(), ui.FlexParams(1)) // Rest of space for terminal

	s.root.SetChild(col)
}

func (s *TerminalScreen) RenderNotch(f graphics.Frame) {
	// Get mouse position and window size
	mx, my := f.CursorPos()
	w, _ := f.WindowSize()
	winW := float32(w)

	// Define hover zone (expanded notch area at top center)
	hoverW := float32(180)
	hoverH := float32(50)
	hoverX := (winW - hoverW) / 2
	hoverRect := rect{x: hoverX, y: 0, w: hoverW, h: hoverH}
	isHovered := hoverRect.contains(mx, my)

	// Animate expand/collapse
	now := time.Now()
	if !s.lastUpdate.IsZero() {
		dt := float32(now.Sub(s.lastUpdate).Seconds())
		if isHovered {
			s.expandProgress += notchAnimSpeed * dt
			if s.expandProgress > 1.0 {
				s.expandProgress = 1.0
			}
		} else {
			s.expandProgress -= notchAnimSpeed * dt
			if s.expandProgress < 0.0 {
				s.expandProgress = 0.0
			}
		}
	}
	s.lastUpdate = now

	// Update expanded state and rebuild if changed
	newExpanded := s.expandProgress > 0.5
	if newExpanded != s.expanded {
		s.expanded = newExpanded
		s.rebuildLayout()
	}

	// Use DrawOnly to avoid consuming keyboard events that the terminal needs.
	// Handle button clicks manually below.
	s.root.DrawOnly(f)

	// Manual button click handling when expanded
	if s.expanded {
		leftDown := f.GetButtonState(window.ButtonLeft).IsDown()
		justClicked := leftDown && !s.prevLeftDown
		s.prevLeftDown = leftDown

		if justClicked && isHovered {
			// Check if click is on Exit button (left side of notch)
			exitBtnRect := rect{x: hoverX + 10, y: 10, w: 50, h: 28}
			if exitBtnRect.contains(mx, my) {
				s.app.showExitConfirm = true
			}

			// Check if click is on Net button (right side of notch)
			netBtnRect := rect{x: hoverX + hoverW - 60, y: 10, w: 50, h: 28}
			if netBtnRect.contains(mx, my) {
				s.app.networkDisabled = !s.app.networkDisabled
				if s.app.running != nil && s.app.running.netBackend != nil {
					s.app.running.netBackend.SetInternetAccessEnabled(!s.app.networkDisabled)
				}
				if s.app.networkDisabled {
					s.netBtn.WithStyle(notchNetButtonStyle(false))
					s.netBtn.SetText("Off")
				} else {
					s.netBtn.WithStyle(notchNetButtonStyle(true))
					s.netBtn.SetText("Net")
				}
				slog.Info("internet access toggled", "disabled", s.app.networkDisabled)
			}
		}
	} else {
		s.prevLeftDown = f.GetButtonState(window.ButtonLeft).IsDown()
	}
}
