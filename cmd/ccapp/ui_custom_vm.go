package main

import (
	"image/color"
	"path/filepath"
	"time"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/ui"
	"github.com/tinyrange/cc/internal/gowin/window"
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
	mode           CustomVMMode
	selectedPath   string
	imageName      string
	networkEnabled bool

	// Widgets that need updating
	tabBar         *ui.TabBar
	pathInput      *ui.TextInput
	browseButton   *ui.Button
	imageInput     *ui.TextInput
	networkToggle  *ui.Toggle
	launchButton   *ui.Button
	inputContainer *ui.FlexContainer
}

// NewCustomVMScreen creates the custom VM selection screen
func NewCustomVMScreen(app *Application) *CustomVMScreen {
	screen := &CustomVMScreen{
		root:           ui.NewRoot(app.text),
		app:            app,
		mode:           CustomVMModeBundleDir,
		networkEnabled: false,
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
			s.mode = CustomVMMode(index)
			s.buildUI() // Rebuild to reflect mode change
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

	// Toggle for network access with mesh rendering for smooth shapes
	networkRow := ui.Row().WithGap(12)
	s.networkToggle = ui.NewToggle().
		WithStyle(ui.ToggleStyle{
			TrackWidth:        44,
			TrackHeight:       24,
			TrackColorOn:      colorAccent,   // Tokyo Night blue
			TrackColorOff:     colorBtnNormal, // Tokyo Night storm
			ThumbSize:         20,
			ThumbColor:        colorTextPrimary,
			ThumbPadding:      2,
			AnimationDuration: 200 * time.Millisecond,
		}).
		WithGraphicsWindow(s.app.window). // Enable mesh rendering for smooth pill/circle shapes
		OnChange(func(on bool) {
			s.networkEnabled = on
		})
	s.networkToggle.SetOn(s.networkEnabled)
	networkRow.AddChild(s.networkToggle, ui.DefaultFlexParams())
	networkRow.AddChild(ui.NewLabel("Enable Internet Access").WithSize(14).WithColor(textColorPrimary), ui.DefaultFlexParams())
	content.AddChild(networkRow, ui.DefaultFlexParams())

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
	s.launchButton = ui.NewButton("Launch").
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
	var displayName string

	switch s.mode {
	case CustomVMModeBundleDir:
		if s.selectedPath == "" {
			return // No path selected
		}
		sourceType = VMSourceBundle
		sourcePath = s.selectedPath
		displayName = filepath.Base(sourcePath)

	case CustomVMModeTarball:
		if s.selectedPath == "" {
			return // No path selected
		}
		sourceType = VMSourceTarball
		sourcePath = s.selectedPath
		displayName = filepath.Base(sourcePath)

	case CustomVMModeImageName:
		if s.imageName == "" {
			return // No image name entered
		}
		sourceType = VMSourceImageName
		sourcePath = s.imageName
		displayName = s.imageName
	}

	// Record to recent VMs
	if s.app.recentVMs != nil {
		s.app.recentVMs.AddOrUpdate(RecentVM{
			Name:           displayName,
			SourceType:     sourceType,
			SourcePath:     sourcePath,
			NetworkEnabled: s.networkEnabled,
		})
		// Update dock menu
		s.app.updateDockMenu()
	}

	// Clear blur capture before changing mode
	s.app.clearBlurCapture()

	// Start boot process
	s.app.startCustomVM(sourceType, sourcePath, s.networkEnabled)
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
