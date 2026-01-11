package main

import (
	"fmt"
	"image/color"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/tinyrange/cc/internal/bundle"
	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/ui"
	"github.com/tinyrange/cc/internal/oci"
)

// envVarRow represents a single environment variable row in the settings dialog
type envVarRow struct {
	keyInput   *ui.TextInput
	valueInput *ui.TextInput
	deleteBtn  *ui.Button
	container  *ui.FlexContainer
}

// SettingsScreen is the dialog for editing VM bundle settings
type SettingsScreen struct {
	root *ui.Root
	app  *Application

	// Bundle being edited
	bundleIndex int
	bundleDir   string
	metadata    bundle.Metadata

	// Image configuration (from OCI image)
	imageEntrypoint []string
	imageCmd        []string
	imageEnv        []string

	// Form fields
	nameInput    *ui.TextInput
	descInput    *ui.TextInput
	commandInput *ui.TextInput

	// Environment variables (structured editor)
	envRows      []*envVarRow
	envContainer *ui.FlexContainer

	// Buttons
	addEnvBtn *ui.Button
	deleteBtn *ui.Button
	cancelBtn *ui.Button
	saveBtn   *ui.Button
}

// NewSettingsScreen creates the settings screen UI for a bundle
func NewSettingsScreen(app *Application, bundleIndex int) *SettingsScreen {
	if bundleIndex < 0 || bundleIndex >= len(app.bundles) {
		return nil
	}
	b := app.bundles[bundleIndex]

	screen := &SettingsScreen{
		root:        ui.NewRoot(app.text),
		app:         app,
		bundleIndex: bundleIndex,
		bundleDir:   b.Dir,
		metadata:    b.Meta,
	}

	// Load OCI image to get entrypoint, cmd, and env
	imageDir := filepath.Join(b.Dir, b.Meta.Boot.ImageDir)
	if b.Meta.Boot.ImageDir == "" {
		imageDir = filepath.Join(b.Dir, "image")
	}
	if img, err := oci.LoadFromDir(imageDir); err == nil {
		screen.imageEntrypoint = img.Config.Entrypoint
		screen.imageCmd = img.Config.Cmd
		screen.imageEnv = img.Config.Env
	} else {
		slog.Warn("failed to load image config for settings", "error", err)
	}

	screen.buildUI()
	return screen
}

func (s *SettingsScreen) buildUI() {
	// Main layout: Stack with centered dialog
	stack := ui.NewStack()

	// Semi-transparent overlay
	stack.AddChild(ui.NewBox(color.RGBA{R: 0x1a, G: 0x1b, B: 0x26, A: 180}))

	// Dialog constants
	const dialogWidth float32 = 600
	const dialogHeight float32 = 620
	const contentWidth float32 = dialogWidth - 48
	const cornerRadius float32 = 12
	const labelWidth float32 = 100

	// Create content
	content := ui.Column().WithPadding(ui.All(24)).WithGap(12)

	// Title
	content.AddChild(
		ui.NewLabel("VM Settings").WithSize(24).WithColor(colorTextPrimary),
		ui.DefaultFlexParams(),
	)

	// Name field
	nameRow := ui.Row().WithGap(12).WithCrossAlignment(ui.CrossAxisCenter)
	nameRow.AddChild(
		ui.NewLabel("Name:").WithSize(14).WithColor(colorTextSecondary),
		ui.DefaultFlexParams(),
	)
	s.nameInput = ui.NewTextInput().
		WithPlaceholder("Bundle name").
		WithMinWidth(contentWidth - labelWidth).
		WithGraphicsWindow(s.app.window)
	s.nameInput.SetText(s.metadata.Name)
	nameRow.AddChild(s.nameInput, ui.FlexParams(1))
	content.AddChild(nameRow, ui.DefaultFlexParams())

	// Description field
	descRow := ui.Row().WithGap(12).WithCrossAlignment(ui.CrossAxisCenter)
	descRow.AddChild(
		ui.NewLabel("Description:").WithSize(14).WithColor(colorTextSecondary),
		ui.DefaultFlexParams(),
	)
	s.descInput = ui.NewTextInput().
		WithPlaceholder("Bundle description").
		WithMinWidth(contentWidth - labelWidth).
		WithGraphicsWindow(s.app.window)
	s.descInput.SetText(s.metadata.Description)
	descRow.AddChild(s.descInput, ui.FlexParams(1))
	content.AddChild(descRow, ui.DefaultFlexParams())

	// === Image Configuration Section (read-only) ===
	content.AddChild(
		ui.NewLabel("Image Configuration").WithSize(16).WithColor(colorTextPrimary),
		ui.FlexParamsWithMargin(0, ui.Only(0, 8, 0, 0)),
	)

	// Image Entrypoint (read-only)
	epRow := ui.Row().WithGap(12).WithCrossAlignment(ui.CrossAxisCenter)
	epRow.AddChild(
		ui.NewLabel("Entrypoint:").WithSize(13).WithColor(colorTextSecondary),
		ui.DefaultFlexParams(),
	)
	epText := "(none)"
	if len(s.imageEntrypoint) > 0 {
		epText = strings.Join(s.imageEntrypoint, " ")
	}
	epRow.AddChild(
		ui.NewLabel(epText).WithSize(13).WithColor(colorTextMuted),
		ui.FlexParams(1),
	)
	content.AddChild(epRow, ui.DefaultFlexParams())

	// Image Cmd (read-only)
	cmdInfoRow := ui.Row().WithGap(12).WithCrossAlignment(ui.CrossAxisCenter)
	cmdInfoRow.AddChild(
		ui.NewLabel("Cmd:").WithSize(13).WithColor(colorTextSecondary),
		ui.DefaultFlexParams(),
	)
	cmdText := "(none)"
	if len(s.imageCmd) > 0 {
		cmdText = strings.Join(s.imageCmd, " ")
	}
	cmdInfoRow.AddChild(
		ui.NewLabel(cmdText).WithSize(13).WithColor(colorTextMuted),
		ui.FlexParams(1),
	)
	content.AddChild(cmdInfoRow, ui.DefaultFlexParams())

	// Image Environment Variables (read-only, collapsible list)
	if len(s.imageEnv) > 0 {
		envInfoRow := ui.Row().WithGap(12)
		envInfoRow.AddChild(
			ui.NewLabel("Env:").WithSize(13).WithColor(colorTextSecondary),
			ui.DefaultFlexParams(),
		)
		// Show first few env vars, truncated
		envPreview := s.imageEnv[0]
		if len(s.imageEnv) > 1 {
			envPreview += fmt.Sprintf(" (+%d more)", len(s.imageEnv)-1)
		}
		if len(envPreview) > 50 {
			envPreview = envPreview[:47] + "..."
		}
		envInfoRow.AddChild(
			ui.NewLabel(envPreview).WithSize(13).WithColor(colorTextMuted),
			ui.FlexParams(1),
		)
		content.AddChild(envInfoRow, ui.DefaultFlexParams())
	}

	// === Override Section ===
	content.AddChild(
		ui.NewLabel("Overrides").WithSize(16).WithColor(colorTextPrimary),
		ui.FlexParamsWithMargin(0, ui.Only(0, 8, 0, 0)),
	)

	// Command override field
	cmdRow := ui.Row().WithGap(12).WithCrossAlignment(ui.CrossAxisCenter)
	cmdRow.AddChild(
		ui.NewLabel("Command:").WithSize(13).WithColor(colorTextSecondary),
		ui.DefaultFlexParams(),
	)
	s.commandInput = ui.NewTextInput().
		WithPlaceholder("Override command (replaces image cmd)").
		WithMinWidth(contentWidth - labelWidth).
		WithGraphicsWindow(s.app.window)
	if len(s.metadata.Boot.Command) > 0 {
		s.commandInput.SetText(strings.Join(s.metadata.Boot.Command, " "))
	}
	cmdRow.AddChild(s.commandInput, ui.FlexParams(1))
	content.AddChild(cmdRow, ui.DefaultFlexParams())

	// Custom Environment Variables section
	content.AddChild(
		ui.NewLabel("Custom Environment Variables:").WithSize(13).WithColor(colorTextSecondary),
		ui.DefaultFlexParams(),
	)

	// Env vars container
	s.envContainer = ui.Column().WithGap(6)
	s.envRows = nil

	// Parse existing custom env vars
	for _, envStr := range s.metadata.Boot.Env {
		parts := strings.SplitN(envStr, "=", 2)
		key := parts[0]
		value := ""
		if len(parts) > 1 {
			value = parts[1]
		}
		s.addEnvRow(key, value)
	}

	content.AddChild(s.envContainer, ui.DefaultFlexParams())

	// Add Variable button
	addEnvStyle := secondaryButtonStyle()
	addEnvStyle.MinWidth = 120
	addEnvStyle.MinHeight = 28
	addEnvStyle.TextSize = 12
	s.addEnvBtn = ui.NewButton("+ Add Variable").
		WithStyle(addEnvStyle).
		WithGraphicsWindow(s.app.window).
		OnClick(func() {
			// Collect current values before rebuilding
			s.collectCurrentEnvVars()
			// Add empty entry
			s.metadata.Boot.Env = append(s.metadata.Boot.Env, "=")
			s.buildUI()
		})
	content.AddChild(s.addEnvBtn, ui.DefaultFlexParams())

	// Spacer to push buttons to bottom
	content.AddChild(ui.NewSpacer(), ui.FlexParams(1))

	// Button row
	buttonRow := ui.Row().WithGap(12)

	// Delete button (red/danger style)
	deleteStyle := secondaryButtonStyle()
	deleteStyle.BackgroundNormal = color.RGBA{R: 0x40, G: 0x20, B: 0x25, A: 255}
	deleteStyle.BackgroundHovered = colorRed
	deleteStyle.TextColor = colorRed
	deleteStyle.MinWidth = 80
	deleteStyle.MinHeight = 36
	deleteStyle.TextSize = 14
	s.deleteBtn = ui.NewButton("Delete").
		WithStyle(deleteStyle).
		WithGraphicsWindow(s.app.window).
		OnClick(func() {
			s.app.showDeleteConfirm(s.bundleIndex)
		})
	buttonRow.AddChild(s.deleteBtn, ui.DefaultFlexParams())

	// Spacer
	buttonRow.AddChild(ui.NewSpacer(), ui.FlexParams(1))

	// Cancel button
	cancelStyle := secondaryButtonStyle()
	cancelStyle.MinWidth = 90
	cancelStyle.MinHeight = 36
	cancelStyle.TextSize = 14
	s.cancelBtn = ui.NewButton("Cancel").
		WithStyle(cancelStyle).
		WithGraphicsWindow(s.app.window).
		OnClick(func() {
			s.app.clearBlurCapture()
			s.app.mode = modeLauncher
		})
	buttonRow.AddChild(s.cancelBtn, ui.DefaultFlexParams())

	// Save button
	saveStyle := primaryButtonStyle()
	saveStyle.MinWidth = 90
	saveStyle.MinHeight = 36
	saveStyle.TextSize = 14
	s.saveBtn = ui.NewButton("Save").
		WithStyle(saveStyle).
		WithGraphicsWindow(s.app.window).
		OnClick(s.onSave)
	buttonRow.AddChild(s.saveBtn, ui.DefaultFlexParams())

	content.AddChild(buttonRow, ui.DefaultFlexParams())

	// Create dialog card
	dialogCard := ui.NewCard(nil).
		WithBackground(colorCardBg).
		WithCornerRadius(cornerRadius).
		WithGraphicsWindow(s.app.window).
		WithFixedSize(dialogWidth, dialogHeight).
		WithPadding(ui.All(0))
	dialogCard.SetContent(content)

	stack.AddChild(ui.CenterCenter(dialogCard))
	s.root.SetChild(stack)

	// Focus on name input
	if s.nameInput != nil {
		s.nameInput.SetFocused(true)
	}
}

func (s *SettingsScreen) addEnvRow(key, value string) {
	row := ui.Row().WithGap(8).WithCrossAlignment(ui.CrossAxisCenter)

	keyInput := ui.NewTextInput().
		WithPlaceholder("KEY").
		WithMinWidth(120).
		WithGraphicsWindow(s.app.window)
	keyInput.SetText(key)
	row.AddChild(keyInput, ui.DefaultFlexParams())

	row.AddChild(
		ui.NewLabel("=").WithSize(14).WithColor(colorTextSecondary),
		ui.DefaultFlexParams(),
	)

	valueInput := ui.NewTextInput().
		WithPlaceholder("value").
		WithMinWidth(200).
		WithGraphicsWindow(s.app.window)
	valueInput.SetText(value)
	row.AddChild(valueInput, ui.FlexParams(1))

	// Delete button for this row
	delStyle := secondaryButtonStyle()
	delStyle.MinWidth = 28
	delStyle.MinHeight = 28
	delStyle.TextSize = 14
	delStyle.Padding = ui.All(0)
	deleteBtn := ui.NewButton("X").
		WithStyle(delStyle).
		WithGraphicsWindow(s.app.window)

	envRow := &envVarRow{
		keyInput:   keyInput,
		valueInput: valueInput,
		deleteBtn:  deleteBtn,
		container:  row,
	}

	// Capture index for delete callback
	idx := len(s.envRows)
	deleteBtn.OnClick(func() {
		s.removeEnvRow(idx)
	})

	row.AddChild(deleteBtn, ui.DefaultFlexParams())

	s.envRows = append(s.envRows, envRow)
	s.envContainer.AddChild(row, ui.DefaultFlexParams())
}

func (s *SettingsScreen) collectCurrentEnvVars() {
	var envVars []string
	for _, row := range s.envRows {
		key := row.keyInput.Text()
		value := row.valueInput.Text()
		// Include all rows, even empty ones (they'll be filtered on save)
		envVars = append(envVars, key+"="+value)
	}
	s.metadata.Boot.Env = envVars
}

func (s *SettingsScreen) removeEnvRow(index int) {
	if index < 0 || index >= len(s.envRows) {
		return
	}
	// Store current values, excluding the removed row
	var envVars []string
	for i, row := range s.envRows {
		if i == index {
			continue
		}
		key := row.keyInput.Text()
		value := row.valueInput.Text()
		envVars = append(envVars, key+"="+value)
	}
	s.metadata.Boot.Env = envVars
	s.buildUI()
}

func (s *SettingsScreen) onSave() {
	// Collect form values
	s.metadata.Name = s.nameInput.Text()
	s.metadata.Description = s.descInput.Text()

	// Parse command
	cmdText := strings.TrimSpace(s.commandInput.Text())
	if cmdText != "" {
		s.metadata.Boot.Command = strings.Fields(cmdText)
	} else {
		s.metadata.Boot.Command = nil
	}

	// Collect env vars
	var envVars []string
	for _, row := range s.envRows {
		key := strings.TrimSpace(row.keyInput.Text())
		value := row.valueInput.Text()
		if key != "" {
			envVars = append(envVars, key+"="+value)
		}
	}
	s.metadata.Boot.Env = envVars

	// Save to file
	if err := bundle.WriteTemplate(s.bundleDir, s.metadata); err != nil {
		slog.Error("failed to save bundle metadata", "error", err)
		s.app.showError(err)
		return
	}

	slog.Info("saved bundle settings", "dir", s.bundleDir)

	// Refresh bundles list
	s.app.refreshBundles()

	// Close dialog
	s.app.clearBlurCapture()
	s.app.mode = modeLauncher
}

// Render renders the settings screen
func (s *SettingsScreen) Render(f graphics.Frame) error {
	w, h := f.WindowSize()
	s.app.text.SetViewport(int32(w), int32(h))

	pw := s.app.window.PlatformWindow()
	s.root.Step(f, pw)
	return nil
}
