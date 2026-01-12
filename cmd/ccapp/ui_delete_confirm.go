package main

import (
	"log/slog"
	"os"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/ui"
)

// DeleteConfirmScreen is the confirmation dialog for deleting a bundle
type DeleteConfirmScreen struct {
	root *ui.Root
	app  *Application

	// Bundle being deleted
	bundleIndex int
	bundleName  string
	bundleDir   string
}

// NewDeleteConfirmScreen creates the delete confirmation dialog
func NewDeleteConfirmScreen(app *Application, bundleIndex int) *DeleteConfirmScreen {
	if bundleIndex < 0 || bundleIndex >= len(app.bundles) {
		return nil
	}
	b := app.bundles[bundleIndex]

	screen := &DeleteConfirmScreen{
		root:        ui.NewRoot(app.text),
		app:         app,
		bundleIndex: bundleIndex,
		bundleName:  b.Meta.Name,
		bundleDir:   b.Dir,
	}
	screen.buildUI()
	return screen
}

func (s *DeleteConfirmScreen) buildUI() {
	// Main layout: Stack with centered dialog
	stack := ui.NewStack()

	// Semi-transparent overlay
	stack.AddChild(ui.NewBox(dialogOverlayColor(overlayAlphaMedium)))

	// Dialog dimensions (from design.go)
	const dialogWidth = deleteDialogWidth
	const dialogHeight = deleteDialogHeight

	// Create content
	content := ui.Column().WithPadding(ui.All(24)).WithGap(20)

	// Title
	content.AddChild(
		ui.NewLabel("Delete Bundle?").WithSize(20).WithColor(colorTextPrimary),
		ui.DefaultFlexParams(),
	)

	// Message
	content.AddChild(
		ui.NewWrapLabel("Are you sure you want to delete \""+s.bundleName+"\"? This action cannot be undone.").
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
			// Go back to settings dialog
			s.app.mode = modeSettings
		})
	buttonRow.AddChild(cancelBtn, ui.DefaultFlexParams())

	// Delete button (danger style)
	deleteStyle := dangerButtonStyle()
	deleteStyle.MinWidth = 90
	deleteStyle.MinHeight = 36
	deleteStyle.TextSize = 14
	deleteBtn := ui.NewButton("Delete").
		WithStyle(deleteStyle).
		WithGraphicsWindow(s.app.window).
		OnClick(s.onDelete)
	buttonRow.AddChild(deleteBtn, ui.DefaultFlexParams())

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

func (s *DeleteConfirmScreen) onDelete() {
	slog.Info("deleting bundle", "name", s.bundleName, "dir", s.bundleDir)

	// Remove the bundle directory
	if err := os.RemoveAll(s.bundleDir); err != nil {
		slog.Error("failed to delete bundle", "error", err)
		s.app.showError(err)
		return
	}

	slog.Info("bundle deleted successfully", "name", s.bundleName)

	// Refresh bundles list
	s.app.refreshBundles()

	// Close dialogs and return to launcher
	s.app.clearBlurCapture()
	s.app.mode = modeLauncher
}

// Render renders the delete confirmation screen
func (s *DeleteConfirmScreen) Render(f graphics.Frame) error {
	w, h := f.WindowSize()
	s.app.text.SetViewport(int32(w), int32(h))

	pw := s.app.window.PlatformWindow()
	s.root.Step(f, pw)
	return nil
}
