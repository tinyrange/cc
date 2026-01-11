package main

import (
	"image/color"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/tinyrange/cc/internal/bundle"
	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/ui"
	"github.com/tinyrange/cc/internal/gowin/window"
	"github.com/tinyrange/cc/internal/oci"
)

// CustomVMMode represents the input mode for custom VM
type CustomVMMode int

const (
	CustomVMModeBundleDir CustomVMMode = iota
	CustomVMModeTarball
	CustomVMModeImageName
)

// CustomVMScreen is the dialog for selecting a custom VM
type CustomVMScreen struct {
	root *ui.Root
	app  *Application

	// State
	mode         CustomVMMode
	selectedPath string
	imageName    string

	// Widgets that need updating
	tabBar         *ui.TabBar
	pathInput      *ui.TextInput
	browseButton   *ui.Button
	imageInput     *ui.TextInput
	launchButton   *ui.Button
	inputContainer *ui.FlexContainer
}

// NewCustomVMScreen creates the custom VM selection screen
func NewCustomVMScreen(app *Application) *CustomVMScreen {
	screen := &CustomVMScreen{
		root: ui.NewRoot(app.text),
		app:  app,
		mode: CustomVMModeBundleDir,
	}
	screen.buildUI()
	return screen
}

func (s *CustomVMScreen) buildUI() {
	// Main layout: Stack with centered dialog
	// Background blur is handled by the parent renderer
	stack := ui.NewStack()

	// Semi-transparent overlay to darken the blurred background slightly
	stack.AddChild(ui.NewBox(color.RGBA{R: 0x1a, G: 0x1b, B: 0x26, A: 180})) // Tokyo Night with alpha

	// Dialog constants
	const dialogWidth float32 = 500
	const dialogHeight float32 = 380
	const contentWidth float32 = dialogWidth - 48 // minus padding
	const cornerRadius float32 = 12

	// Dialog colors (Tokyo Night theme)
	dialogBg := colorCardBg
	textColorPrimary := colorTextPrimary
	textColorSecondary := colorTextSecondary

	// Create content first
	content := ui.Column().WithPadding(ui.All(24)).WithGap(20)
	content.AddChild(ui.NewLabel("New Custom VM").WithSize(24).WithColor(textColorPrimary), ui.DefaultFlexParams())

	// Use Card with rounded corners for dialog background
	dialogCard := ui.NewCard(nil).
		WithBackground(dialogBg).
		WithCornerRadius(cornerRadius).
		WithGraphicsWindow(s.app.window).
		WithFixedSize(dialogWidth, dialogHeight).
		WithPadding(ui.All(0))

	// Tab bar for mode selection
	s.tabBar = ui.NewTabBar([]string{"Bundle Dir", "Tarball", "Image Name"}).
		WithStyle(ui.TabBarStyle{
			BackgroundColor:    color.RGBA{R: 0, G: 0, B: 0, A: 0}, // Transparent
			TextColor:          textColorSecondary,
			TextColorSelected:  textColorPrimary,
			TextColorHovered:   colorTextPrimary,
			TextSize:           14,
			TabPadding:         ui.Symmetric(20, 10),
			TabGap:             0,
			UnderlineColor:     colorAccent, // Tokyo Night blue
			UnderlineThickness: 2,
			UnderlineInset:     0,
			Height:             36,
		}).
		OnSelect(func(index int) {
			// Commit current input text before rebuilding
			if s.pathInput != nil {
				s.selectedPath = s.pathInput.Text()
			}
			if s.imageInput != nil {
				s.imageName = s.imageInput.Text()
			}
			s.mode = CustomVMMode(index)
			s.buildUI()
		})
	s.tabBar.SetSelectedIndex(int(s.mode))
	content.AddChild(s.tabBar, ui.DefaultFlexParams())

	// Input area (changes based on mode)
	inputCol := ui.Column().WithGap(8)
	switch s.mode {
	case CustomVMModeBundleDir:
		inputCol.AddChild(ui.NewLabel("Select a bundle directory:").WithSize(14).WithColor(textColorSecondary), ui.DefaultFlexParams())
		inputRow := ui.Row().WithGap(8)
		s.pathInput = ui.NewTextInput().
			WithPlaceholder("").
			WithMinWidth(contentWidth - 90 - 8).
			WithGraphicsWindow(s.app.window)
		if s.selectedPath != "" {
			s.pathInput.SetText(s.selectedPath)
		}
		s.pathInput.OnChange(func(text string) {
			s.selectedPath = text
		})
		inputRow.AddChild(s.pathInput, ui.DefaultFlexParams())
		browseStyle := secondaryButtonStyle()
		browseStyle.MinWidth = 90
		browseStyle.MinHeight = 32
		browseStyle.TextSize = 14
		s.browseButton = ui.NewButton("Browse...").
			WithStyle(browseStyle).
			WithGraphicsWindow(s.app.window).
			OnClick(func() {
				s.browseDirectory()
			})
		inputRow.AddChild(s.browseButton, ui.DefaultFlexParams())
		inputCol.AddChild(inputRow, ui.DefaultFlexParams())

	case CustomVMModeTarball:
		inputCol.AddChild(ui.NewLabel("Select an OCI tarball (.tar):").WithSize(14).WithColor(textColorSecondary), ui.DefaultFlexParams())
		inputRow := ui.Row().WithGap(8)
		s.pathInput = ui.NewTextInput().
			WithPlaceholder("").
			WithMinWidth(contentWidth - 90 - 8).
			WithGraphicsWindow(s.app.window)
		if s.selectedPath != "" {
			s.pathInput.SetText(s.selectedPath)
		}
		s.pathInput.OnChange(func(text string) {
			s.selectedPath = text
		})
		inputRow.AddChild(s.pathInput, ui.DefaultFlexParams())
		browseStyle := secondaryButtonStyle()
		browseStyle.MinWidth = 90
		browseStyle.MinHeight = 32
		browseStyle.TextSize = 14
		s.browseButton = ui.NewButton("Browse...").
			WithStyle(browseStyle).
			WithGraphicsWindow(s.app.window).
			OnClick(func() {
				s.browseTarball()
			})
		inputRow.AddChild(s.browseButton, ui.DefaultFlexParams())
		inputCol.AddChild(inputRow, ui.DefaultFlexParams())

	case CustomVMModeImageName:
		inputCol.AddChild(ui.NewLabel("Enter container image name:").WithSize(14).WithColor(textColorSecondary), ui.DefaultFlexParams())
		s.imageInput = ui.NewTextInput().
			WithPlaceholder("e.g., alpine:latest").
			WithMinWidth(contentWidth).
			WithGraphicsWindow(s.app.window). // Enable rounded corners
			OnChange(func(text string) {
				s.imageName = text
			})
		if s.imageName != "" {
			s.imageInput.SetText(s.imageName)
		}
		inputCol.AddChild(s.imageInput, ui.DefaultFlexParams())
	}
	content.AddChild(inputCol, ui.DefaultFlexParams())

	// Spacer to push buttons to bottom
	content.AddChild(ui.NewSpacer(), ui.FlexParams(1))

	// Button row with consistent dialog button styles
	buttonRow := ui.Row().WithGap(12)
	buttonRow.AddChild(ui.NewSpacer(), ui.FlexParams(1))

	// Cancel button (secondary style, sized for dialog)
	cancelStyle := secondaryButtonStyle()
	cancelStyle.MinWidth = 90
	cancelStyle.MinHeight = 36
	cancelStyle.TextSize = 14
	cancelBtn := ui.NewButton("Cancel").
		WithStyle(cancelStyle).
		WithGraphicsWindow(s.app.window).
		OnClick(func() {
			s.app.clearBlurCapture()
			s.app.mode = modeLauncher
		})
	buttonRow.AddChild(cancelBtn, ui.DefaultFlexParams())

	// Launch button (primary style, sized for dialog)
	launchStyle := primaryButtonStyle()
	launchStyle.MinWidth = 90
	launchStyle.MinHeight = 36
	launchStyle.TextSize = 14
	s.launchButton = ui.NewButton("Add").
		WithStyle(launchStyle).
		WithGraphicsWindow(s.app.window).
		OnClick(s.onLaunch)
	buttonRow.AddChild(s.launchButton, ui.DefaultFlexParams())
	content.AddChild(buttonRow, ui.DefaultFlexParams())

	// Set content as the card's child
	dialogCard.SetContent(content)
	stack.AddChild(ui.CenterCenter(dialogCard))

	s.root.SetChild(stack)

	// Set focus on the appropriate input field based on mode
	switch s.mode {
	case CustomVMModeBundleDir, CustomVMModeTarball:
		if s.pathInput != nil {
			s.pathInput.SetFocused(true)
		}
	case CustomVMModeImageName:
		if s.imageInput != nil {
			s.imageInput.SetFocused(true)
		}
	}
}

func (s *CustomVMScreen) browseDirectory() {
	if dm, ok := s.app.window.PlatformWindow().(window.FileDialogSupport); ok {
		path := dm.ShowOpenPanel(window.FileDialogTypeDirectory, nil)
		if path != "" {
			s.selectedPath = path
			if s.pathInput != nil {
				s.pathInput.SetText(path)
			}
		}
	}
}

func (s *CustomVMScreen) browseTarball() {
	if dm, ok := s.app.window.PlatformWindow().(window.FileDialogSupport); ok {
		path := dm.ShowOpenPanel(window.FileDialogTypeFile, []string{"tar"})
		if path != "" {
			s.selectedPath = path
			if s.pathInput != nil {
				s.pathInput.SetText(path)
			}
		}
	}
}

func (s *CustomVMScreen) onLaunch() {
	var sourceType VMSourceType
	var sourcePath string
	var bundleName string

	switch s.mode {
	case CustomVMModeBundleDir:
		if s.selectedPath == "" {
			return // No path selected
		}
		// Validate bundle directory
		if err := bundle.ValidateBundleDir(s.selectedPath); err != nil {
			slog.Error("invalid bundle directory", "path", s.selectedPath, "error", err)
			s.app.showError(err)
			return
		}
		sourceType = VMSourceBundle
		sourcePath = s.selectedPath
		bundleName = filepath.Base(sourcePath)

	case CustomVMModeTarball:
		if s.selectedPath == "" {
			return // No path selected
		}
		// Validate tarball
		if err := oci.ValidateTar(s.selectedPath); err != nil {
			slog.Error("invalid tarball", "path", s.selectedPath, "error", err)
			s.app.showError(err)
			return
		}
		sourceType = VMSourceTarball
		sourcePath = s.selectedPath
		bundleName = strings.TrimSuffix(filepath.Base(sourcePath), ".tar")

	case CustomVMModeImageName:
		if s.imageName == "" {
			return // No image name entered
		}
		sourceType = VMSourceImageName
		sourcePath = s.imageName
		bundleName = sanitizeImageName(s.imageName)
	}

	// Clear blur capture before changing mode
	s.app.clearBlurCapture()

	// For bundle dir, validate and launch directly (don't copy)
	if sourceType == VMSourceBundle {
		// Record to recent VMs
		if s.app.recentVMs != nil {
			s.app.recentVMs.AddOrUpdate(RecentVM{
				Name:       bundleName,
				SourceType: sourceType,
				SourcePath: sourcePath,
			})
			s.app.updateDockMenu()
		}
		s.app.startCustomVM(sourceType, sourcePath)
		return
	}

	// For image/tarball, install as bundle
	s.app.installBundle(sourceType, sourcePath, bundleName)
}

// sanitizeImageName converts an image name to a valid directory name
func sanitizeImageName(imageName string) string {
	// Remove registry prefix if present
	if idx := strings.LastIndex(imageName, "/"); idx != -1 {
		imageName = imageName[idx+1:]
	}
	// Replace : with _
	imageName = strings.ReplaceAll(imageName, ":", "_")
	// Replace other invalid chars
	var b strings.Builder
	for _, r := range imageName {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	result := b.String()
	if result == "" {
		result = "bundle"
	}
	return result
}

// Render renders the custom VM screen
func (s *CustomVMScreen) Render(f graphics.Frame) error {
	w, h := f.WindowSize()
	s.app.text.SetViewport(int32(w), int32(h))

	// Get platform window for events
	pw := s.app.window.PlatformWindow()

	s.root.Step(f, pw)
	return nil
}
