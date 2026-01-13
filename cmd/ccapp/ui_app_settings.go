package main

import (
	"fmt"
	"image/color"
	"log/slog"
	"runtime"
	"time"

	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/ui"
	"github.com/tinyrange/cc/internal/update"
)

// AppSettingsScreen is the app-level settings dialog
type AppSettingsScreen struct {
	root *ui.Root
	app  *Application

	// State
	autoUpdateEnabled     bool
	createDesktopShortcut bool
	needsInstall          bool
	deleteOriginal        bool

	// Widgets
	autoUpdateCheckbox     *ui.Checkbox
	desktopShortcutCheckbox *ui.Checkbox
	installButton          *ui.Button
	deleteCheckbox         *ui.Checkbox
	updateButton           *ui.Button
	logsButton             *ui.Button
	closeButton            *ui.Button
	logo                   *ui.AnimatedLogo
}

// NewAppSettingsScreen creates the app settings screen
func NewAppSettingsScreen(app *Application) *AppSettingsScreen {
	needsInstall := !update.IsInStandardLocation()

	// Load current settings
	autoUpdateEnabled := false
	createDesktopShortcut := false
	if app.settings != nil {
		settings := app.settings.Get()
		autoUpdateEnabled = settings.AutoUpdateEnabled
		createDesktopShortcut = settings.CreateDesktopShortcut
	}

	screen := &AppSettingsScreen{
		root:                  ui.NewRoot(app.text),
		app:                   app,
		autoUpdateEnabled:     autoUpdateEnabled,
		createDesktopShortcut: createDesktopShortcut,
		needsInstall:          needsInstall,
		deleteOriginal:        needsInstall, // Default: delete original if installing
	}
	screen.buildUI()
	return screen
}

func (s *AppSettingsScreen) buildUI() {
	// Main layout: Stack with background and centered content
	stack := ui.NewStack()

	// Semi-transparent overlay
	stack.AddChild(ui.NewBox(dialogOverlayColor(overlayAlphaLight)))

	// Centered content column (following OnboardingScreen pattern)
	centerContent := ui.Column().
		WithCrossAlignment(ui.CrossAxisCenter).
		WithGap(0)

	// Logo at top (smaller animated version)
	if s.app.logo != nil {
		s.logo = ui.NewAnimatedLogo(s.app.logo).
			WithSize(100, 100).
			WithSpeeds(0.9, -1.4, 2.2)
		centerContent.AddChild(s.logo, ui.DefaultFlexParams())
	}

	// Spacer
	centerContent.AddChild(ui.NewBox(color.Transparent).WithSize(0, 16), ui.DefaultFlexParams())

	// Settings heading
	centerContent.AddChild(
		ui.NewLabel("Settings").WithSize(textSizeHeading).WithColor(colorTextPrimary),
		ui.DefaultFlexParams(),
	)

	// Spacer
	centerContent.AddChild(ui.NewBox(color.Transparent).WithSize(0, 16), ui.DefaultFlexParams())

	// Card for options
	cardContent := ui.Column().WithGap(12)

	// Auto-update checkbox
	s.autoUpdateCheckbox = ui.NewCheckbox("Automatically check for updates").
		WithStyle(s.checkboxStyle())
	s.autoUpdateCheckbox.SetChecked(s.autoUpdateEnabled)
	s.autoUpdateCheckbox.OnChange(func(checked bool) {
		s.autoUpdateEnabled = checked
		s.saveSettings()
	})
	cardContent.AddChild(s.autoUpdateCheckbox, ui.DefaultFlexParams())

	// Desktop shortcut checkbox (Windows/Linux only)
	if runtime.GOOS == "windows" || runtime.GOOS == "linux" {
		s.desktopShortcutCheckbox = ui.NewCheckbox("Create desktop shortcut").
			WithStyle(s.checkboxStyle())
		s.desktopShortcutCheckbox.SetChecked(s.createDesktopShortcut)
		s.desktopShortcutCheckbox.OnChange(func(checked bool) {
			s.createDesktopShortcut = checked
			s.saveSettings()
			// If enabling and already installed, create the shortcut now
			if checked && !s.needsInstall {
				if targetPath, err := update.GetTargetPath(); err == nil {
					if err := update.CreateDesktopShortcut(targetPath); err != nil {
						slog.Warn("failed to create desktop shortcut", "error", err)
					} else {
						slog.Info("created desktop shortcut")
					}
				}
			} else if !checked {
				// Remove shortcut when unchecked
				if err := update.RemoveDesktopShortcut(); err != nil {
					slog.Warn("failed to remove desktop shortcut", "error", err)
				}
			}
		})

		shortcutLabel := "Add to Start Menu"
		if runtime.GOOS == "linux" {
			shortcutLabel = "Add to Applications menu"
		}
		s.desktopShortcutCheckbox = ui.NewCheckbox(shortcutLabel).
			WithStyle(s.checkboxStyle())
		s.desktopShortcutCheckbox.SetChecked(s.createDesktopShortcut)
		s.desktopShortcutCheckbox.OnChange(func(checked bool) {
			s.createDesktopShortcut = checked
			s.saveSettings()
			// Handle shortcut creation/removal
			if checked && !s.needsInstall {
				if targetPath, err := update.GetTargetPath(); err == nil {
					if err := update.CreateDesktopShortcut(targetPath); err != nil {
						slog.Warn("failed to create desktop shortcut", "error", err)
					} else {
						slog.Info("created desktop shortcut")
					}
				}
			} else if !checked {
				if err := update.RemoveDesktopShortcut(); err != nil {
					slog.Warn("failed to remove desktop shortcut", "error", err)
				}
			}
		})
		cardContent.AddChild(s.desktopShortcutCheckbox, ui.DefaultFlexParams())
	}

	// Spacer before install section
	cardContent.AddChild(ui.NewBox(color.Transparent).WithSize(0, 8), ui.DefaultFlexParams())

	// Install to Applications section (only if not installed)
	if s.needsInstall {
		// Delete original checkbox
		s.deleteCheckbox = ui.NewCheckbox("Delete original after installation").
			WithStyle(s.checkboxStyle())
		s.deleteCheckbox.SetChecked(s.deleteOriginal)
		s.deleteCheckbox.OnChange(func(checked bool) {
			s.deleteOriginal = checked
		})
		cardContent.AddChild(s.deleteCheckbox, ui.DefaultFlexParams())

		// Install button
		installStyle := primaryButtonStyle()
		installStyle.MinWidth = 220
		installStyle.MinHeight = 40
		installStyle.TextSize = textSizeBody
		s.installButton = ui.NewButton("Install to Applications").
			WithStyle(installStyle).
			WithGraphicsWindow(s.app.window).
			OnClick(s.onInstall)
		cardContent.AddChild(ui.CenterCenter(s.installButton), ui.DefaultFlexParams())
	} else {
		// Show installation status
		cardContent.AddChild(
			ui.NewLabel("Installed").WithSize(textSizeLabel).WithColor(colorGreen),
			ui.DefaultFlexParams(),
		)
	}

	// Spacer
	cardContent.AddChild(ui.NewBox(color.Transparent).WithSize(0, 8), ui.DefaultFlexParams())

	// Check for Updates button
	updateStyle := secondaryButtonStyle()
	updateStyle.MinWidth = 180
	updateStyle.MinHeight = 36
	updateStyle.TextSize = textSizeLabel
	s.updateButton = ui.NewButton("Check for Updates").
		WithStyle(updateStyle).
		WithGraphicsWindow(s.app.window).
		OnClick(s.onCheckForUpdates)
	cardContent.AddChild(ui.CenterCenter(s.updateButton), ui.DefaultFlexParams())

	// Bottom button row
	buttonRow := ui.Row().WithGap(12)

	// Open Logs button
	logsStyle := secondaryButtonStyle()
	logsStyle.MinWidth = 100
	logsStyle.MinHeight = 36
	logsStyle.TextSize = textSizeLabel
	s.logsButton = ui.NewButton("Open Logs").
		WithStyle(logsStyle).
		WithGraphicsWindow(s.app.window).
		OnClick(func() {
			s.app.openLogs()
		})
	buttonRow.AddChild(s.logsButton, ui.DefaultFlexParams())

	// Spacer
	buttonRow.AddChild(ui.NewSpacer(), ui.FlexParams(1))

	// Close button
	closeStyle := primaryButtonStyle()
	closeStyle.MinWidth = 100
	closeStyle.MinHeight = 36
	closeStyle.TextSize = textSizeLabel
	s.closeButton = ui.NewButton("Close").
		WithStyle(closeStyle).
		WithGraphicsWindow(s.app.window).
		OnClick(func() {
			s.app.mode = modeLauncher
		})
	buttonRow.AddChild(s.closeButton, ui.DefaultFlexParams())

	cardContent.AddChild(buttonRow, ui.DefaultFlexParams())

	// Version info
	cardContent.AddChild(
		ui.NewLabel(fmt.Sprintf("Version %s", Version)).WithSize(11).WithColor(colorTextMuted),
		ui.FlexParamsWithMargin(0, ui.Only(0, 8, 0, 0)),
	)

	// Create options card
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

func (s *AppSettingsScreen) checkboxStyle() ui.CheckboxStyle {
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

func (s *AppSettingsScreen) saveSettings() {
	if s.app.settings == nil {
		return
	}
	settings := s.app.settings.Get()
	settings.AutoUpdateEnabled = s.autoUpdateEnabled
	settings.CreateDesktopShortcut = s.createDesktopShortcut
	if err := s.app.settings.Set(settings); err != nil {
		slog.Warn("failed to save settings", "error", err)
	}
}

func (s *AppSettingsScreen) onInstall() {
	if s.app.settings == nil {
		return
	}

	// Get target directory
	targetDir, err := update.GetUserApplicationsDir()
	if err != nil {
		s.app.showError(fmt.Errorf("failed to get Applications directory: %w", err))
		return
	}

	slog.Info("installing app from settings", "targetDir", targetDir)

	// Copy app to target directory
	newPath, err := update.CopyAppToLocation(targetDir)
	if err != nil {
		s.app.showError(fmt.Errorf("failed to install app: %w", err))
		return
	}

	slog.Info("app installed", "newPath", newPath)

	// Create desktop shortcut if enabled
	if s.createDesktopShortcut {
		if err := update.CreateDesktopShortcut(newPath); err != nil {
			slog.Warn("failed to create desktop shortcut", "error", err)
		} else {
			slog.Info("created desktop shortcut")
		}
	}

	// Update settings
	settings := s.app.settings.Get()
	settings.InstallPath = newPath
	settings.OnboardingCompleted = true
	settings.AutoUpdateEnabled = s.autoUpdateEnabled
	settings.CreateDesktopShortcut = s.createDesktopShortcut

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
		s.app.showError(fmt.Errorf("failed to launch installed app: %w", launchErr))
	}
}

func (s *AppSettingsScreen) onCheckForUpdates() {
	slog.Info("check for updates requested from settings")

	// Check for updates (force fetch from API)
	status := s.app.updateChecker.ForceCheck()
	if status.Error != nil {
		slog.Error("update check failed", "error", status.Error)
		s.app.showError(fmt.Errorf("update check failed: %w", status.Error))
		return
	}

	// Check if an update is available
	if !status.Available {
		slog.Info("no update available", "current", status.CurrentVersion, "latest", status.LatestVersion)
		s.app.showError(fmt.Errorf("you're already on the latest version (%s)", status.CurrentVersion))
		return
	}

	// Check if we have a download URL
	if status.DownloadURL == "" {
		slog.Error("no download URL found for this platform")
		s.app.showError(fmt.Errorf("no update available for %s/%s", runtime.GOOS, runtime.GOARCH))
		return
	}

	slog.Info("update available", "current", status.CurrentVersion, "latest", status.LatestVersion)

	// Show the update confirmation dialog
	s.app.showUpdateConfirm(&status)
}

// Update updates any dynamic state (called before rendering)
func (s *AppSettingsScreen) Update(f graphics.Frame) {
	// Update logo animation if present
	if s.logo != nil {
		t := float32(time.Since(s.app.start).Seconds())
		s.logo.SetTime(t)
	}
}

// Render renders the app settings screen
func (s *AppSettingsScreen) Render(f graphics.Frame) error {
	s.Update(f)
	s.root.Step(f, s.app.window.PlatformWindow())
	return nil
}
