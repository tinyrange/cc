package main

import (
	"image/color"
	"path/filepath"
	"time"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/ui"
)

// UI color constants matching the original design
var (
	colorBackground    = color.RGBA{R: 10, G: 10, B: 10, A: 255}
	colorTopBar        = color.RGBA{R: 22, G: 22, B: 22, A: 255}
	colorBtnNormal     = color.RGBA{R: 40, G: 40, B: 40, A: 255}
	colorBtnHover      = color.RGBA{R: 56, G: 56, B: 56, A: 255}
	colorBtnPressed    = color.RGBA{R: 72, G: 72, B: 72, A: 255}
	colorBorderNormal  = color.RGBA{R: 80, G: 80, B: 80, A: 255}
	colorBorderHover   = color.RGBA{R: 140, G: 140, B: 140, A: 255}
	colorBorderPressed = color.RGBA{R: 180, G: 180, B: 180, A: 255}
	colorCardBg        = color.RGBA{R: 20, G: 20, B: 20, A: 220}
	colorCardBgHover   = color.RGBA{R: 30, G: 30, B: 30, A: 235}
	colorOverlay       = color.RGBA{R: 10, G: 10, B: 10, A: 200}
	colorScrollTrack   = color.RGBA{R: 48, G: 48, B: 48, A: 255}
	colorScrollThumb   = color.RGBA{R: 100, G: 100, B: 100, A: 255}
)

// LauncherScreen manages the launcher UI state and widgets
type LauncherScreen struct {
	root *ui.Root
	app  *Application

	// Widgets that need updating
	bundleCards []*bundleCardWidget
	scrollView  *ui.ScrollView
	logo        *ui.AnimatedLogo

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

	stack.AddChild(contentCol)
	s.root.SetChild(stack)
}

func (s *LauncherScreen) buildTopBar() *ui.FlexContainer {
	return ui.Row().
		WithBackground(colorTopBar).
		WithPadding(ui.Symmetric(20, 6)).
		AddChild(ui.NewSpacer(), ui.FlexParams(1)).
		AddChild(
			ui.NewButton("Debug Logs").
				WithMinSize(120, 20).
				OnClick(func() {
					s.app.openLogs()
				}),
			ui.DefaultFlexParams(),
		)
}

func (s *LauncherScreen) buildTitleSection() *ui.FlexContainer {
	col := ui.Column().
		WithPadding(ui.Only(20, 50, 20, 0))

	col.AddChild(ui.NewLabel("CrumbleCracker").WithSize(48), ui.DefaultFlexParams())

	if len(s.app.bundles) == 0 {
		col.AddChild(
			ui.NewLabel("No bundles found. Create bundles with: cc -build <outDir> <image>").WithSize(20),
			ui.FlexParamsWithMargin(0, ui.Only(0, 10, 0, 0)),
		)
		col.AddChild(
			ui.NewLabel("Searched for bundles in: "+s.app.bundlesDir).WithSize(20),
			ui.FlexParamsWithMargin(0, ui.Only(0, 10, 0, 0)),
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
	const cardWidth = 180
	const cardHeight = 230
	const contentWidth = cardWidth - 10

	// Card content: vertical layout with image area and text
	content := ui.Column().WithGap(8)

	// Placeholder image area (sized to fit within padded content area)
	content.AddChild(ui.NewSpacer().WithSize(contentWidth, contentWidth), ui.DefaultFlexParams())

	// Name and description
	content.AddChild(ui.NewLabel(name).WithSize(18), ui.DefaultFlexParams())
	content.AddChild(ui.NewLabel(desc).WithSize(14).WithColor(graphics.ColorLightGray), ui.DefaultFlexParams())

	cardStyle := ui.CardStyle{
		BackgroundColor: color.RGBA{A: 0}, // Transparent by default
		BorderColor:     colorBorderNormal,
		BorderWidth:     1,
		Padding:         ui.Only(5, 0, 5, 0),
	}

	hoverStyle := ui.CardStyle{
		BackgroundColor: colorCardBg,
		BorderColor:     colorBorderHover,
		BorderWidth:     1,
		Padding:         ui.Only(5, 0, 5, 0),
	}

	card := ui.NewCard(content).
		WithStyle(cardStyle).
		WithHoverStyle(hoverStyle).
		WithFixedSize(cardWidth, cardHeight).
		OnClick(func() {
			s.app.selectedIndex = index
			s.app.startBootBundle(index)
		})

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

// LoadingScreen manages the loading UI state
type LoadingScreen struct {
	root *ui.Root
	app  *Application
	logo *ui.AnimatedLogo
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

	// Centered logo
	if s.app.logo != nil {
		s.logo = ui.NewAnimatedLogo(s.app.logo).
			WithSize(300, 300).
			WithSpeeds(0.9, -1.4, 2.2)
		stack.AddChild(ui.CenterCenter(s.logo))
	}

	// Loading text at top-left
	msg := "Booting VM…"
	if s.app.bootName != "" {
		msg = "Booting " + s.app.bootName + "…"
	}
	loadingLabel := ui.NewLabel(msg).WithSize(20)
	stack.AddChild(ui.TopLeft(ui.NewPadding(loadingLabel, ui.All(20))))

	s.root.SetChild(stack)
}

func (s *LoadingScreen) Update(f graphics.Frame) {
	if s.logo != nil {
		t := float32(time.Since(s.app.bootStarted).Seconds())
		s.logo.SetTime(t)
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

	// Subtle rotating logo in center
	if s.app.logo != nil {
		s.logo = ui.NewAnimatedLogo(s.app.logo).
			WithSize(250, 250).
			WithSpeeds(0.4, 0, 0) // Only outer circle slowly rotates
		stack.AddChild(ui.CenterCenter(s.logo))
	}

	// Content column
	content := ui.Column().WithPadding(ui.All(30))

	// Error header
	content.AddChild(ui.NewLabel("Error").WithSize(56), ui.DefaultFlexParams())

	// Error message
	msg := s.app.errMsg
	if msg == "" {
		msg = "unknown error"
	}
	content.AddChild(
		ui.NewLabel(msg).WithSize(18),
		ui.FlexParamsWithMargin(0, ui.Only(0, 30, 0, 0)),
	)

	// Buttons
	buttonCol := ui.Column().WithGap(14)

	buttonCol.AddChild(
		ui.NewButton("Back to carousel").
			WithMinSize(320, 44).
			OnClick(func() {
				s.app.errMsg = ""
				s.app.selectedIndex = -1
				s.app.mode = modeLauncher
			}),
		ui.DefaultFlexParams(),
	)

	buttonCol.AddChild(
		ui.NewButton("Open logs directory").
			WithMinSize(320, 44).
			OnClick(func() {
				s.app.openLogs()
			}),
		ui.DefaultFlexParams(),
	)

	content.AddChild(buttonCol, ui.FlexParamsWithMargin(0, ui.Only(0, 40, 0, 0)))

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
	root *ui.Root
	app  *Application
}

// NewTerminalScreen creates the terminal screen UI (top bar only)
func NewTerminalScreen(app *Application) *TerminalScreen {
	screen := &TerminalScreen{
		root: ui.NewRoot(app.text),
		app:  app,
	}
	screen.buildUI()
	return screen
}

func (s *TerminalScreen) buildUI() {
	// Just the top bar - terminal view is handled separately
	topBar := ui.Row().
		WithBackground(colorTopBar).
		WithPadding(ui.Symmetric(20, 6)).
		AddChild(
			ui.NewButton("Exit").
				WithMinSize(70, 20).
				OnClick(func() {
					s.app.stopVM()
				}),
			ui.DefaultFlexParams(),
		).
		AddChild(ui.NewSpacer(), ui.FlexParams(1)).
		AddChild(
			ui.NewButton("Debug Logs").
				WithMinSize(120, 20).
				OnClick(func() {
					s.app.openLogs()
				}),
			ui.DefaultFlexParams(),
		)

	// Wrap in column to set height
	col := ui.Column()
	col.AddChild(topBar, ui.DefaultFlexParams())
	col.AddChild(ui.NewSpacer(), ui.FlexParams(1)) // Rest of space for terminal

	s.root.SetChild(col)
}

func (s *TerminalScreen) RenderTopBar(f graphics.Frame) {
	s.root.Step(f, s.app.window.PlatformWindow())
}
