package main

import (
	"fmt"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/ui"
	"github.com/tinyrange/cc/internal/update"
)

// UpdateConfirmScreen is the confirmation dialog for installing an update
type UpdateConfirmScreen struct {
	root *ui.Root
	app  *Application

	// Update info
	status *update.UpdateStatus
}

// NewUpdateConfirmScreen creates the update confirmation dialog
func NewUpdateConfirmScreen(app *Application, status *update.UpdateStatus) *UpdateConfirmScreen {
	screen := &UpdateConfirmScreen{
		root:   ui.NewRoot(app.text),
		app:    app,
		status: status,
	}
	screen.buildUI()
	return screen
}

func (s *UpdateConfirmScreen) buildUI() {
	// Main layout: Stack with centered dialog
	stack := ui.NewStack()

	// Semi-transparent overlay
	stack.AddChild(ui.NewBox(dialogOverlayColor(overlayAlphaMedium)))

	// Dialog dimensions
	const dialogWidth float32 = 420
	const dialogHeight float32 = 220

	// Create content
	content := ui.Column().WithPadding(ui.All(24)).WithGap(16)

	// Title
	content.AddChild(
		ui.NewLabel("Update Available").WithSize(20).WithColor(colorTextPrimary),
		ui.DefaultFlexParams(),
	)

	// Version change info
	versionText := fmt.Sprintf("%s  →  %s", s.status.CurrentVersion, s.status.LatestVersion)
	content.AddChild(
		ui.NewLabel(versionText).WithSize(16).WithColor(colorTextSecondary),
		ui.DefaultFlexParams(),
	)

	// Description
	content.AddChild(
		ui.NewWrapLabel("A new version is available. Would you like to install the update now?").
			WithSize(14).
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
	cancelStyle.TextSize = 14
	cancelBtn := ui.NewButton("Cancel").
		WithStyle(cancelStyle).
		WithGraphicsWindow(s.app.window).
		OnClick(func() {
			// Go back to app settings
			s.app.mode = modeAppSettings
		})
	buttonRow.AddChild(cancelBtn, ui.DefaultFlexParams())

	// Install button (primary style)
	installStyle := primaryButtonStyle()
	installStyle.MinWidth = 110
	installStyle.MinHeight = 36
	installStyle.TextSize = 14
	installBtn := ui.NewButton("Install Now").
		WithStyle(installStyle).
		WithGraphicsWindow(s.app.window).
		OnClick(s.onInstall)
	buttonRow.AddChild(installBtn, ui.DefaultFlexParams())

	content.AddChild(buttonRow, ui.DefaultFlexParams())

	// Create dialog card
	dialogCard := ui.NewCard(nil).
		WithBackground(colorCardBg).
		WithCornerRadius(cornerRadiusLarge).
		WithGraphicsWindow(s.app.window).
		WithFixedSize(dialogWidth, dialogHeight).
		WithPadding(ui.All(0))
	dialogCard.SetContent(content)

	stack.AddChild(ui.CenterCenter(dialogCard))
	s.root.SetChild(stack)
}

func (s *UpdateConfirmScreen) onInstall() {
	// Store the status and start the update
	s.app.updateStatus = s.status
	s.app.startUpdate()
}

// Render renders the update confirmation screen
func (s *UpdateConfirmScreen) Render(f graphics.Frame) error {
	w, h := f.WindowSize()
	s.app.text.SetViewport(int32(w), int32(h))

	pw := s.app.window.PlatformWindow()
	s.root.Step(f, pw)
	return nil
}
