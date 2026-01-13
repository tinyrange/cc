package main

import (
	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/ui"
)

// Dialog dimensions for URL confirmation
const (
	urlConfirmDialogWidth  float32 = 450
	urlConfirmDialogHeight float32 = 220
)

// URLConfirmScreen is the dialog for confirming a URL-initiated VM launch.
type URLConfirmScreen struct {
	root *ui.Root
	app  *Application

	// URL action data
	imageRef string
}

// NewURLConfirmScreen creates the URL confirmation dialog.
func NewURLConfirmScreen(app *Application, imageRef string) *URLConfirmScreen {
	screen := &URLConfirmScreen{
		root:     ui.NewRoot(app.text),
		app:      app,
		imageRef: imageRef,
	}
	screen.buildUI()
	return screen
}

func (s *URLConfirmScreen) buildUI() {
	// Main layout: Stack with centered dialog
	stack := ui.NewStack()

	// Semi-transparent overlay
	stack.AddChild(ui.NewBox(dialogOverlayColor(overlayAlphaLight)))

	// Dialog card
	dialogCard := ui.NewCard(nil).
		WithBackground(colorCardBg).
		WithCornerRadius(cornerRadiusLarge).
		WithGraphicsWindow(s.app.window).
		WithFixedSize(urlConfirmDialogWidth, urlConfirmDialogHeight).
		WithPadding(ui.All(0))

	// Content
	content := ui.Column().WithPadding(ui.All(24)).WithGap(16)

	// Title
	content.AddChild(
		ui.NewLabel("Run Container Image?").WithSize(textSizeHeading).WithColor(colorTextPrimary),
		ui.DefaultFlexParams(),
	)

	// Image name (truncated if too long)
	displayName := SanitizeImageNameForDisplay(s.imageRef)
	content.AddChild(
		ui.NewLabel(displayName).WithSize(textSizeMedium).WithColor(colorAccent),
		ui.DefaultFlexParams(),
	)

	// Security notice
	content.AddChild(
		ui.NewLabel("This will download and run the container image from its registry.").
			WithSize(textSizeLabel).
			WithColor(colorTextSecondary),
		ui.DefaultFlexParams(),
	)

	// Spacer
	content.AddChild(ui.NewSpacer(), ui.FlexParams(1))

	// Button row
	buttonRow := ui.Row().WithGap(12)
	buttonRow.AddChild(ui.NewSpacer(), ui.FlexParams(1))

	// Cancel button
	cancelStyle := secondaryButtonStyle()
	cancelStyle.MinWidth = 90
	cancelStyle.MinHeight = 36
	cancelStyle.TextSize = textSizeLabel
	cancelBtn := ui.NewButton("Cancel").
		WithStyle(cancelStyle).
		WithGraphicsWindow(s.app.window).
		OnClick(func() {
			s.app.clearBlurCapture()
			s.app.pendingURL = ""
			s.app.mode = modeLauncher
		})
	buttonRow.AddChild(cancelBtn, ui.DefaultFlexParams())

	// Run button (primary action)
	runStyle := primaryButtonStyle()
	runStyle.MinWidth = 90
	runStyle.MinHeight = 36
	runStyle.TextSize = textSizeLabel
	runBtn := ui.NewButton("Run").
		WithStyle(runStyle).
		WithGraphicsWindow(s.app.window).
		OnClick(func() {
			s.onConfirm()
		})
	buttonRow.AddChild(runBtn, ui.DefaultFlexParams())

	content.AddChild(buttonRow, ui.DefaultFlexParams())

	// Set content
	dialogCard.SetContent(content)
	stack.AddChild(ui.CenterCenter(dialogCard))

	s.root.SetChild(stack)
}

func (s *URLConfirmScreen) onConfirm() {
	// Clear blur capture
	s.app.clearBlurCapture()
	s.app.pendingURL = ""

	// Record to recent VMs
	bundleName := sanitizeImageName(s.imageRef)
	if s.app.recentVMs != nil {
		s.app.recentVMs.AddOrUpdate(RecentVM{
			Name:       bundleName,
			SourceType: VMSourceImageName,
			SourcePath: s.imageRef,
		})
		s.app.updateDockMenu()
	}

	// Launch the VM
	s.app.startCustomVM(VMSourceImageName, s.imageRef)
}

// Render renders the URL confirmation screen.
func (s *URLConfirmScreen) Render(f graphics.Frame) error {
	w, h := f.WindowSize()
	s.app.text.SetViewport(int32(w), int32(h))

	pw := s.app.window.PlatformWindow()
	s.root.Step(f, pw)
	return nil
}
