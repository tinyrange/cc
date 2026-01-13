package main

import (
	"fmt"
	"image/color"
	"log/slog"
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

// formatSpeed formats bytes per second as a human-readable string.
func formatSpeed(bytesPerSecond float64) string {
	const (
		KB = 1024.0
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytesPerSecond >= GB:
		return fmt.Sprintf("%.1f GB/s", bytesPerSecond/GB)
	case bytesPerSecond >= MB:
		return fmt.Sprintf("%.1f MB/s", bytesPerSecond/MB)
	case bytesPerSecond >= KB:
		return fmt.Sprintf("%.1f KB/s", bytesPerSecond/KB)
	default:
		return fmt.Sprintf("%.0f B/s", bytesPerSecond)
	}
}

// formatETA formats a duration as a human-readable ETA string.
func formatETA(d time.Duration) string {
	if d < 0 {
		return ""
	}
	if d == 0 {
		return "Done"
	}
	// Round to nearest second
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%ds remaining", int(d.Seconds()))
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		secs := int(d.Seconds()) % 60
		if secs > 0 {
			return fmt.Sprintf("%dm %ds remaining", mins, secs)
		}
		return fmt.Sprintf("%dm remaining", mins)
	}
	hours := int(d.Hours())
	mins := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm remaining", hours, mins)
}

// Colors, UI constants, and common styles are defined in design.go

// LauncherScreen manages the launcher UI state and widgets
type LauncherScreen struct {
	root *ui.Root
	app  *Application

	// Widgets that need updating
	bundleCards []*bundleCardWidget
	scrollView  *ui.ScrollView
	logo        *ui.AnimatedLogo
	titleLabel  *ui.GradientLabel

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

	// Version label on the left
	row.AddChild(
		ui.NewLabel(fmt.Sprintf("v%s", Version)).WithSize(11).WithColor(colorTextMuted),
		ui.DefaultFlexParams(),
	)

	row.AddChild(ui.NewSpacer(), ui.FlexParams(1))

	// Update available notification
	if s.app.updateStatus != nil && s.app.updateStatus.Available {
		row.AddChild(
			s.buildUpdateButton(),
			ui.FlexParamsWithMargin(0, ui.Only(0, 0, 10, 0)),
		)
	}

	// Settings button
	row.AddChild(
		s.buildIconButton("Settings", s.iconCog, func() {
			s.app.showAppSettings()
		}),
		ui.FlexParamsWithMargin(0, ui.Only(0, 0, 10, 0)),
	)

	// Debug Logs button with icon
	row.AddChild(
		s.buildIconButton("Logs", s.iconLogs, func() {
			s.app.openLogs()
		}),
		ui.DefaultFlexParams(),
	)

	return row
}

// buildUpdateButton creates a button to show when an update is available
func (s *LauncherScreen) buildUpdateButton() ui.Widget {
	status := s.app.updateStatus
	label := fmt.Sprintf("Update: %s", status.LatestVersion)

	content := ui.Row().
		WithGap(5).
		WithCrossAlignment(ui.CrossAxisCenter)

	content.AddChild(ui.NewLabel(label).WithSize(13).WithColor(colorGreen), ui.DefaultFlexParams())

	// Create card button with green accent
	card := ui.NewCard(content).
		WithStyle(updateButtonCardStyle()).
		WithHoverStyle(updateButtonCardHoverStyle()).
		WithGraphicsWindow(s.app.window).
		OnClick(func() {
			s.app.startUpdate()
		})

	return card
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

	// Rainbow gradient for the title (initial state - will be animated)
	s.titleLabel = ui.NewGradientLabel("CrumbleCracker").WithSize(48)
	col.AddChild(s.titleLabel, ui.DefaultFlexParams())

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
		WithStyle(addVMCardStyle()).
		WithHoverStyle(addVMCardHoverStyle()).
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

	// Card dimensions and padding (from design.go)
	const contentWidth = cardWidth - (cardPadding * 2)

	// Card content: vertical layout with image area and text
	content := ui.Column().WithGap(8)

	// Procedural icon area - clickable to start VM
	iconWidget := ui.NewProceduralIconWidget(name).
		WithSize(contentWidth, imageHeight).
		WithGraphicsWindow(s.app.window)

	imageCard := ui.NewCard(iconWidget).
		WithStyle(bundleImageCardStyle()).
		WithHoverStyle(bundleImageCardHoverStyle()).
		WithGraphicsWindow(s.app.window).
		WithFixedSize(contentWidth, imageHeight).
		OnClick(func() {
			s.app.selectedIndex = index
			s.app.startBootBundle(index)
		})
	content.AddChild(imageCard, ui.DefaultFlexParams())

	// Name and description with better spacing
	content.AddChild(ui.NewWrapLabel(name).WithSize(16), ui.DefaultFlexParams())
	content.AddChild(ui.NewWrapLabel(desc).WithSize(13).WithColor(colorTextSecondary), ui.DefaultFlexParams())

	// Spacer to push buttons to bottom
	content.AddChild(ui.NewSpacer(), ui.FlexParams(1))

	// Bottom row with Start button on left, Settings button on right
	bottomRow := ui.Row().WithGap(8)

	// Start button (green)
	startBtn := ui.NewButton("Start").
		WithStyle(startButtonStyle()).
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
			WithStyle(cogButtonStyle()).
			WithHoverStyle(cogButtonHoverStyle()).
			WithGraphicsWindow(s.app.window).
			WithFixedSize(buttonSize, buttonSize).
			OnClick(func() {
				s.app.showSettings(index)
			})
		bottomRow.AddChild(cogButton, ui.DefaultFlexParams())
	}
	content.AddChild(bottomRow, ui.DefaultFlexParams())

	// Outer card is just a visual container (NOT clickable)
	card := ui.NewCard(content).
		WithStyle(bundleCardStyle()).
		WithGraphicsWindow(s.app.window).
		WithFixedSize(cardWidth, cardHeight)

	return &bundleCardWidget{
		card:  card,
		index: index,
	}
}

func (s *LauncherScreen) Update(f graphics.Frame) {
	t := float32(time.Since(s.app.start).Seconds())

	// Update logo animation
	if s.logo != nil {
		s.logo.SetTime(t)
	}

	// Animate rainbow gradient on title
	if s.titleLabel != nil {
		// TokyoNight accent colors (desaturated rainbow)
		rainbowColors := []color.RGBA{
			hex("#f7768e"), // Red
			hex("#e0af68"), // Yellow
			hex("#9ece6a"), // Green
			hex("#7dcfff"), // Cyan
			hex("#7aa2f7"), // Blue
			hex("#bb9af7"), // Magenta
		}

		// Calculate phase offset (0.0 to 1.0, cycles every 12 seconds)
		phase := float32(t) / 12.0
		phase = phase - float32(int(phase)) // Keep in 0-1 range

		// Create stops with colors rotated by phase
		// We need n+1 stops to create a seamless gradient (first color repeated at end)
		n := len(rainbowColors)
		stops := make([]graphics.ColorStop, n+1)

		for i := 0; i <= n; i++ {
			// Position is fixed and evenly spaced
			pos := float32(i) / float32(n)

			// Color index is offset by phase and wraps around
			colorPhase := phase * float32(n)
			colorIdx := (i + int(colorPhase)) % n

			// Interpolate between two colors based on fractional phase
			frac := colorPhase - float32(int(colorPhase))
			nextColorIdx := (colorIdx + 1) % n

			c1 := rainbowColors[colorIdx]
			c2 := rainbowColors[nextColorIdx]

			// Lerp between colors
			r := uint8(float32(c1.R)*(1-frac) + float32(c2.R)*frac)
			g := uint8(float32(c1.G)*(1-frac) + float32(c2.G)*frac)
			b := uint8(float32(c1.B)*(1-frac) + float32(c2.B)*frac)

			stops[i] = graphics.ColorStop{Position: pos, Color: color.RGBA{r, g, b, 255}}
		}

		s.titleLabel.SetGradient(stops)
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

	// Determine title, subtitle, and accent color based on mode
	var title, subtitle string
	var accentColor = colorAccent

	// Read boot state under lock
	s.app.bootProgressMu.Lock()
	bootName := s.app.bootName
	s.app.bootProgressMu.Unlock()

	switch s.app.mode {
	case modeUpdating:
		title = "Downloading Update"
		subtitle = bootName // Version string
		accentColor = colorGreen
	case modeInstalling:
		title = "Downloading"
		subtitle = s.app.installName
		accentColor = colorAccent
	default: // modeLoading
		title = "Starting VM"
		subtitle = bootName
		accentColor = colorAccent
	}

	// Centered content column
	centerContent := ui.Column().
		WithCrossAlignment(ui.CrossAxisCenter).
		WithGap(0)

	// Smaller logo above the content
	if s.app.logo != nil {
		s.logo = ui.NewAnimatedLogo(s.app.logo).
			WithSize(180, 180).
			WithSpeeds(0.9, -1.4, 2.2)
		centerContent.AddChild(s.logo, ui.DefaultFlexParams())
	}

	// Spacer
	centerContent.AddChild(ui.NewBox(color.Transparent).WithSize(0, 24), ui.DefaultFlexParams())

	// Title (large)
	centerContent.AddChild(
		ui.NewLabel(title).WithSize(28).WithColor(colorTextPrimary),
		ui.DefaultFlexParams(),
	)

	// Subtitle (version/name)
	if subtitle != "" {
		centerContent.AddChild(
			ui.NewLabel(subtitle).WithSize(16).WithColor(colorTextSecondary),
			ui.FlexParamsWithMargin(0, ui.Only(0, 8, 0, 0)),
		)
	}

	// Spacer
	centerContent.AddChild(ui.NewBox(color.Transparent).WithSize(0, 32), ui.DefaultFlexParams())

	// Progress card
	cardContent := ui.Column().WithGap(12)

	// Blob count progress (overall progress)
	s.blobProgressLabel = ui.NewLabel("Preparing...").WithSize(13).WithColor(colorTextSecondary)
	cardContent.AddChild(s.blobProgressLabel, ui.DefaultFlexParams())

	s.blobProgressBar = ui.NewProgressBar().
		WithMinWidth(380).
		WithStyle(progressBarStyle(accentColor)).
		WithGraphicsWindow(s.app.window)
	cardContent.AddChild(s.blobProgressBar, ui.DefaultFlexParams())

	// Current blob progress (bytes downloaded)
	s.progressLabel = ui.NewLabel("").WithSize(12).WithColor(colorTextMuted)
	cardContent.AddChild(s.progressLabel, ui.DefaultFlexParams())

	s.progressBar = ui.NewProgressBar().
		WithMinWidth(380).
		WithStyle(progressBarSmallStyle()).
		WithGraphicsWindow(s.app.window)
	cardContent.AddChild(s.progressBar, ui.DefaultFlexParams())

	loadingCard := ui.NewCard(
		ui.NewPadding(cardContent, ui.Symmetric(24, 20)),
	).
		WithStyle(ui.CardStyle{
			BackgroundColor: colorCardBg,
			CornerRadius:    cornerRadiusMedium,
		}).
		WithGraphicsWindow(s.app.window)
	centerContent.AddChild(loadingCard, ui.DefaultFlexParams())

	stack.AddChild(ui.CenterCenter(centerContent))

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
		// modeLoading and modeUpdating both use bootProgress
		// Read all boot state under the same lock
		s.app.bootProgressMu.Lock()
		startTime = s.app.bootStarted
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

		// Format the label to show download status with speed and ETA
		label := formatBytes(progress.Current) + " / " + formatBytes(progress.Total)
		if progress.BytesPerSecond > 0 {
			label += " - " + formatSpeed(progress.BytesPerSecond)
		}
		if progress.ETA >= 0 {
			etaStr := formatETA(progress.ETA)
			if etaStr != "" {
				label += " - " + etaStr
			}
		}
		s.progressLabel.SetText(label)
	} else if progress.Current > 0 {
		// Unknown total, show bytes downloaded with speed
		s.progressBar.SetValue(0) // Indeterminate
		label := "Downloading: " + formatBytes(progress.Current)
		if progress.BytesPerSecond > 0 {
			label += " - " + formatSpeed(progress.BytesPerSecond)
		}
		s.progressLabel.SetText(label)
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
			BackgroundColor: colorRedTint,
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
					s.app.requestShutdown(1)
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

// NewTerminalScreen creates the terminal screen UI (notch bar)
func NewTerminalScreen(app *Application) *TerminalScreen {
	screen := &TerminalScreen{
		root: ui.NewRoot(app.text),
		app:  app,
	}
	screen.buildUI()
	return screen
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
	s.expandedCard = ui.NewCard(buttonRow).
		WithStyle(notchExpandedCardStyle()).
		WithGraphicsWindow(s.app.window)

	// Collapsed notch card (small indicator bar)
	s.collapsedCard = ui.NewCard(nil).
		WithStyle(notchCollapsedCardStyle()).
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
	hoverW := notchHoverW
	hoverH := notchHoverH
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

		if justClicked {
			// Use actual button bounds from the UI layout
			if s.exitBtn.Bounds().Contains(mx, my) {
				s.app.showExitConfirm = true
			}

			if s.netBtn.Bounds().Contains(mx, my) {
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
