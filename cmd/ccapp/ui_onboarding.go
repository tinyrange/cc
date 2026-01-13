package main

import (
	"fmt"
	"image/color"
	"log/slog"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/ui"
	"github.com/tinyrange/cc/internal/update"
)

// OnboardingScreen is the welcome/setup dialog shown on first run
type OnboardingScreen struct {
	root *ui.Root
	app  *Application

	// State
	autoUpdateEnabled bool
	deleteOriginal    bool
	needsInstall      bool // True if not in standard location

	// Widgets
	autoUpdateCheckbox *ui.Checkbox
	deleteCheckbox     *ui.Checkbox
	installButton      *ui.Button
	skipButton         *ui.Button
	logo               *ui.AnimatedLogo
}

// NewOnboardingScreen creates the onboarding screen
func NewOnboardingScreen(app *Application) *OnboardingScreen {
	needsInstall := !update.IsInStandardLocation()

	screen := &OnboardingScreen{
		root:              ui.NewRoot(app.text),
		app:               app,
		autoUpdateEnabled: false,        // Default: opt-in (unchecked)
		deleteOriginal:    needsInstall, // Default: true if needs install
		needsInstall:      needsInstall,
	}
	screen.buildUI()
	return screen
}

func (s *OnboardingScreen) buildUI() {
	// Main layout: Stack with background and centered content
	stack := ui.NewStack()

	// Background color
	stack.AddChild(ui.NewBox(colorBackground))

	// Centered content column (following LoadingScreen pattern)
	centerContent := ui.Column().
		WithCrossAlignment(ui.CrossAxisCenter).
		WithGap(0)

	// Logo at top (smaller animated version)
	if s.app.logo != nil {
		s.logo = ui.NewAnimatedLogo(s.app.logo).
			WithSize(120, 120).
			WithSpeeds(0.9, -1.4, 2.2)
		centerContent.AddChild(s.logo, ui.DefaultFlexParams())
	}

	// Spacer
	centerContent.AddChild(ui.NewBox(color.Transparent).WithSize(0, 24), ui.DefaultFlexParams())

	// Welcome heading
	centerContent.AddChild(
		ui.NewLabel("Welcome to CrumbleCracker").WithSize(textSizeHeading).WithColor(colorTextPrimary),
		ui.DefaultFlexParams(),
	)

	// Spacer
	centerContent.AddChild(ui.NewBox(color.Transparent).WithSize(0, 16), ui.DefaultFlexParams())

	// Info text about internet requirement
	centerContent.AddChild(
		ui.NewLabel("This app needs internet access to download a Linux kernel.").
			WithSize(textSizeLabel).
			WithColor(colorTextSecondary),
		ui.DefaultFlexParams(),
	)

	// Spacer
	centerContent.AddChild(ui.NewBox(color.Transparent).WithSize(0, 24), ui.DefaultFlexParams())

	// Card for options
	cardContent := ui.Column().WithGap(16)

	// Auto-update checkbox
	s.autoUpdateCheckbox = ui.NewCheckbox("Automatically check for updates").
		WithStyle(s.checkboxStyle())
	s.autoUpdateCheckbox.SetChecked(s.autoUpdateEnabled)
	s.autoUpdateCheckbox.OnChange(func(checked bool) {
		s.autoUpdateEnabled = checked
	})
	cardContent.AddChild(s.autoUpdateCheckbox, ui.DefaultFlexParams())

	// Delete original checkbox (only shown if needs install)
	if s.needsInstall {
		s.deleteCheckbox = ui.NewCheckbox("Delete original after installation").
			WithStyle(s.checkboxStyle())
		s.deleteCheckbox.SetChecked(s.deleteOriginal)
		s.deleteCheckbox.OnChange(func(checked bool) {
			s.deleteOriginal = checked
		})
		cardContent.AddChild(s.deleteCheckbox, ui.DefaultFlexParams())
	}

	// Button row
	buttonRow := ui.Row().WithGap(12)

	// Skip button (secondary style)
	skipStyle := secondaryButtonStyle()
	skipStyle.MinWidth = 100
	skipStyle.MinHeight = 40
	skipStyle.TextSize = textSizeBody
	s.skipButton = ui.NewButton("Skip").
		WithStyle(skipStyle).
		WithGraphicsWindow(s.app.window).
		OnClick(s.onSkip)
	buttonRow.AddChild(s.skipButton, ui.DefaultFlexParams())

	// Install/Continue button (primary style)
	installStyle := primaryButtonStyle()
	installStyle.MinWidth = 180
	installStyle.MinHeight = 40
	installStyle.TextSize = textSizeBody

	buttonText := "Continue"
	if s.needsInstall {
		buttonText = "Install to Applications"
	}
	s.installButton = ui.NewButton(buttonText).
		WithStyle(installStyle).
		WithGraphicsWindow(s.app.window).
		OnClick(s.onInstall)
	buttonRow.AddChild(s.installButton, ui.DefaultFlexParams())

	cardContent.AddChild(buttonRow, ui.DefaultFlexParams())

	// Create options card (following LoadingScreen pattern - no fixed size)
	optionsCard := ui.NewCard(
		ui.NewPadding(cardContent, ui.Symmetric(32, 24)),
	).
		WithStyle(ui.CardStyle{
			BackgroundColor: colorCardBg,
			CornerRadius:    cornerRadiusMedium,
		}).
		WithGraphicsWindow(s.app.window)
	centerContent.AddChild(optionsCard, ui.DefaultFlexParams())

	stack.AddChild(ui.CenterCenter(centerContent))
	s.root.SetChild(stack)
}

func (s *OnboardingScreen) checkboxStyle() ui.CheckboxStyle {
	return ui.CheckboxStyle{
		BoxSize:         18,
		BoxColor:        colorBtnNormal,
		BoxColorHovered: colorBtnHover,
		CheckColor:      colorAccent,
		LabelColor:      colorTextPrimary,
		LabelSize:       textSizeLabel,
		Gap:             10,
	}
}

func (s *OnboardingScreen) onSkip() {
	// Save preferences
	if s.app.settings != nil {
		settings := s.app.settings.Get()
		settings.AutoUpdateEnabled = s.autoUpdateEnabled
		settings.OnboardingCompleted = true
		if err := s.app.settings.Set(settings); err != nil {
			slog.Warn("failed to save settings", "error", err)
		}
	}

	// Transition to launcher
	s.app.mode = modeLauncher
}

func (s *OnboardingScreen) onInstall() {
	// Save auto-update preference first
	if s.app.settings != nil {
		settings := s.app.settings.Get()
		settings.AutoUpdateEnabled = s.autoUpdateEnabled

		if s.needsInstall {
			// Copy app to ~/Applications
			targetDir, err := update.GetUserApplicationsDir()
			if err != nil {
				s.app.showError(fmt.Errorf("failed to get Applications directory: %w", err))
				return
			}

			slog.Info("installing app", "targetDir", targetDir)

			newPath, err := update.CopyAppToLocation(targetDir)
			if err != nil {
				s.app.showError(fmt.Errorf("failed to install app: %w", err))
				return
			}

			slog.Info("app installed", "newPath", newPath)

			// Update settings with new location
			settings.InstallPath = newPath
			settings.OnboardingCompleted = true

			// Schedule cleanup if requested
			if s.deleteOriginal {
				originalPath, _ := update.GetTargetPath()
				settings.CleanupPending = originalPath
			}

			if err := s.app.settings.Set(settings); err != nil {
				slog.Warn("failed to save settings", "error", err)
			}

			// Launch new instance and exit
			launchErr := update.LaunchAppAndExit(newPath)
			if launchErr == update.ErrAppLaunched {
				// App launched successfully - request graceful shutdown
				s.app.requestShutdown(0)
				return
			}
			if launchErr != nil {
				// Launch failed - clear the cleanup pending so we don't delete working app
				settings.CleanupPending = ""
				if err := s.app.settings.Set(settings); err != nil {
					slog.Warn("failed to clear cleanup pending after launch failure", "error", err)
				}
				s.app.showError(fmt.Errorf("installed successfully to %s, but failed to launch\n\nPlease launch manually from the new location", newPath))
				return
			}
			return
		}

		// Already in standard location - just mark complete
		settings.OnboardingCompleted = true
		if err := s.app.settings.Set(settings); err != nil {
			slog.Warn("failed to save settings", "error", err)
		}
	}

	// Transition to launcher
	s.app.mode = modeLauncher
}

// Update updates any dynamic state (called before rendering)
func (s *OnboardingScreen) Update(f graphics.Frame) {
	// Nothing dynamic to update currently
}

// Render renders the onboarding screen
func (s *OnboardingScreen) Render(f graphics.Frame) error {
	s.Update(f)
	s.root.Step(f, s.app.window.PlatformWindow())
	return nil
}
