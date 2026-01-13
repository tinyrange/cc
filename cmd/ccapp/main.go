package main

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"log"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/tinyrange/cc/internal/assets"
	"github.com/tinyrange/cc/internal/bundle"
	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/text"
	"github.com/tinyrange/cc/internal/gowin/window"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/factory"
	"github.com/tinyrange/cc/internal/initx"
	"github.com/tinyrange/cc/internal/linux/kernel"
	"github.com/tinyrange/cc/internal/netstack"
	"github.com/tinyrange/cc/internal/oci"
	termwin "github.com/tinyrange/cc/internal/term"
	"github.com/tinyrange/cc/internal/update"
	"github.com/tinyrange/cc/internal/vfs"
)

// Version is the application version, injected at build time via -ldflags.
var Version = "dev"

// appMode tracks what the app is currently displaying.
type appMode int

const (
	modeLauncher appMode = iota
	modeOnboarding
	modeLoading
	modeError
	modeTerminal
	modeCustomVM
	modeInstalling
	modeSettings
	modeDeleteConfirm
	modeUpdating
	modeAppSettings
	modeUpdateConfirm
	modeURLConfirm
)

// discoveredBundle holds metadata and path for a discovered bundle.
type discoveredBundle struct {
	Dir  string
	Meta bundle.Metadata
}

type bootPrep struct {
	hvArch hv.CpuArchitecture

	// bundle-derived config (with defaults applied)
	cpus     int
	memoryMB uint64
	dmesg    bool
	exec     bool

	// container execution config
	execCmd []string
	env     []string
	workDir string

	// resources created during prep (must be cleaned up on error)
	containerFS  *oci.ContainerFS
	fsBackend    vfs.VirtioFsBackend
	hypervisor   hv.Hypervisor
	kernelLoader kernel.Kernel
	netBackend   *netstack.NetStack
	virtioNet    *virtio.NetstackBackend
}

type bootResult struct {
	prep *bootPrep
	err  error
}

type installResult struct {
	bundlePath string
	err        error
}

// runningVM holds state for a booted VM.
type runningVM struct {
	vm          *initx.VirtualMachine
	session     *initx.Session
	termView    *termwin.View
	containerFS *oci.ContainerFS
	netBackend  *netstack.NetStack
}

type Application struct {
	window graphics.Window
	text   *text.Renderer
	logo   *graphics.SVG

	// Blur effect for dialog backgrounds
	blurEffect        *graphics.BlurEffect
	blurredBackground graphics.Texture
	blurCaptured      bool

	start time.Time

	// Logging
	logDir  string
	logFile string

	// UI screens (widget-based)
	launcherScreen      *LauncherScreen
	loadingScreen       *LoadingScreen
	errorScreen         *ErrorScreen
	terminalScreen      *TerminalScreen
	customVMScreen      *CustomVMScreen
	settingsScreen      *SettingsScreen
	deleteConfirmScreen *DeleteConfirmScreen
	appSettingsScreen   *AppSettingsScreen
	updateConfirmScreen *UpdateConfirmScreen

	// Settings dialog state
	selectedSettingsIndex int

	// Legacy UI state (for terminal screen which uses termview directly)
	scrollX       float32
	selectedIndex int // -1 means list view

	prevLeftDown  bool
	draggingThumb bool
	thumbDragDX   float32

	// Boot loading state
	bootCh         chan bootResult
	bootStarted    time.Time            // Protected by bootProgressMu
	bootName       string               // Protected by bootProgressMu
	bootProgress   oci.DownloadProgress // Protected by bootProgressMu
	bootProgressMu sync.Mutex           // Protects bootStarted, bootName, bootProgress

	// Install state (for installing bundles from images/tarballs)
	installCh         chan installResult
	installStarted    time.Time
	installName       string
	installProgress   oci.DownloadProgress
	installProgressMu sync.Mutex

	// Error state (full-screen)
	errMsg     string
	fatalError bool // true if error is unrecoverable (e.g., no hypervisor)

	// Discovered bundles
	bundlesDir string
	bundles    []discoveredBundle

	// Current mode
	mode appMode

	// Pre-opened hypervisor (opened at startup, transferred to VM on boot)
	hypervisor hv.Hypervisor

	// Running VM (when in terminal mode)
	running *runningVM

	// Recent VMs storage
	recentVMs *RecentVMsStore

	// Settings storage
	settings *SettingsStore

	// Onboarding screen
	onboardingScreen *OnboardingScreen

	// Update checker
	updateChecker *update.Checker
	updateStatus  *update.UpdateStatus

	// Notch UI state (for VM header)
	networkDisabled bool // is internet access cut
	showExitConfirm bool // show shutdown confirmation dialog

	// Exit confirmation dialog shape builders (for rounded corners)
	dialogBgShape   *graphics.ShapeBuilder
	confirmBtnShape *graphics.ShapeBuilder
	cancelBtnShape  *graphics.ShapeBuilder
	dialogLastBgR   rect // track last dialog background rect for geometry updates

	// URL handler state
	pendingURL       string
	pendingURLMu     sync.Mutex // Protects pendingURL
	urlConfirmScreen *URLConfirmScreen

	// Shutdown state
	shutdownRequested bool
	shutdownCode      int
}

type rect struct {
	x float32
	y float32
	w float32
	h float32
}

func (r rect) contains(px, py float32) bool {
	return px >= r.x && px <= r.x+r.w && py >= r.y && py <= r.y+r.h
}

// discoverBundles finds all bundle directories in the given path.
func discoverBundles(bundlesDir string) ([]discoveredBundle, error) {
	entries, err := os.ReadDir(bundlesDir)
	if os.IsNotExist(err) {
		return nil, nil // No bundles directory is fine
	}
	if err != nil {
		return nil, err
	}

	var result []discoveredBundle
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		dir := filepath.Join(bundlesDir, entry.Name())
		if !bundle.IsBundleDir(dir) {
			continue
		}
		meta, err := bundle.LoadMetadata(dir)
		if err != nil {
			slog.Warn("failed to load bundle metadata", "dir", dir, "error", err)
			continue
		}
		result = append(result, discoveredBundle{Dir: dir, Meta: meta})
	}
	return result, nil
}

func setupLogging() (logDir string, logFile string, closeFn func() error) {
	closeFn = func() error { return nil }

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		// Fallback: keep default stderr logging.
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo, AddSource: true})))
		slog.Warn("failed to determine user cache dir; logging to stderr only", "error", err)
		return "", "", closeFn
	}

	logDir = filepath.Join(cacheDir, "ccapp")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo, AddSource: true})))
		slog.Warn("failed to create log dir; logging to stderr only", "dir", logDir, "error", err)
		return "", "", closeFn
	}

	ts := time.Now().Format("20060102-150405")
	logFile = filepath.Join(logDir, fmt.Sprintf("ccapp-%s.log", ts))

	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo, AddSource: true})))
		slog.Warn("failed to open log file; logging to stderr only", "file", logFile, "error", err)
		return logDir, "", closeFn
	}
	closeFn = f.Close

	// Best-effort: redirect process stdout/stderr to the log file so non-slog output
	// (including native libs) lands in the same place.
	dupOK := redirectStdoutStderrToFile(f)

	var w io.Writer = f
	if dupOK {
		// stderr now points at the log, keep slog output consistent.
		w = os.Stderr
	} else {
		// If redirection failed, at least tee the slog + standard log package.
		w = io.MultiWriter(os.Stderr, f)
	}
	log.SetOutput(w)
	slog.SetDefault(slog.New(slog.NewTextHandler(w, &slog.HandlerOptions{
		Level:     slog.LevelDebug,
		AddSource: true,
	})))
	slog.Info("ccapp logging initialized", "log_dir", logDir, "log_file", logFile, "stdout_stderr_redirected", dupOK)

	return logDir, logFile, closeFn
}

func openDirectory(path string) error {
	if path == "" {
		return fmt.Errorf("empty path")
	}
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path).Start()
	case "windows":
		return exec.Command("explorer.exe", path).Start()
	default:
		return exec.Command("xdg-open", path).Start()
	}
}

// openLogs opens the log directory in the system file manager.
func (app *Application) clearBlurCapture() {
	app.blurCaptured = false
	app.blurredBackground = nil
}

func (app *Application) openLogs() {
	slog.Info("open logs requested", "log_dir", app.logDir)
	if err := openDirectory(app.logDir); err != nil {
		slog.Error("failed to open logs directory", "log_dir", app.logDir, "error", err)
	}
}

func (app *Application) openBundlesDir() {
	slog.Info("open bundles directory requested", "bundles_dir", app.bundlesDir)
	if err := openDirectory(app.bundlesDir); err != nil {
		slog.Error("failed to open bundles directory", "bundles_dir", app.bundlesDir, "error", err)
	}
}

// forceUpdate triggers a forced update check and install (for testing)
func (app *Application) forceUpdate() {
	slog.Info("force update requested")

	// Force fetch latest release info
	status := app.updateChecker.ForceUpdate()
	if status.Error != nil {
		slog.Error("force update check failed", "error", status.Error)
		app.showError(fmt.Errorf("update check failed: %w", status.Error))
		return
	}

	// Check if we have a download URL
	if status.DownloadURL == "" {
		slog.Error("no download URL found for this platform")
		app.showError(fmt.Errorf("no update available for %s/%s", runtime.GOOS, runtime.GOARCH))
		return
	}

	app.updateStatus = &status
	slog.Info("force update: got release", "version", status.LatestVersion, "url", status.DownloadURL)

	// Start the update process
	app.startUpdate()
}

// startUpdate initiates the update process
func (app *Application) startUpdate() {
	if app.mode == modeUpdating {
		slog.Warn("update already in progress")
		return
	}
	if app.updateStatus == nil {
		slog.Warn("startUpdate called but no update status")
		return
	}

	slog.Info("starting update", "version", app.updateStatus.LatestVersion)

	// Show updating screen
	app.bootProgressMu.Lock()
	app.bootName = app.updateStatus.LatestVersion
	app.bootStarted = time.Now()
	app.bootProgressMu.Unlock()
	app.loadingScreen = nil // Force rebuild
	app.mode = modeUpdating

	// Download and install in background
	go func() {
		downloader := update.NewDownloader()
		downloader.SetProgressCallback(func(p update.DownloadProgress) {
			slog.Debug("update download progress", "current", p.Current, "total", p.Total, "status", p.Status)
			// Update progress for loading screen
			app.bootProgressMu.Lock()
			app.bootProgress = oci.DownloadProgress{
				Current: p.Current,
				Total:   p.Total,
			}
			app.bootProgressMu.Unlock()
		})

		// Fatal error if checksum is not available
		if app.updateStatus.Checksum == "" {
			slog.Error("update checksum not available, cannot download")
			app.showError(fmt.Errorf("update checksum not available, cannot download"))
			return
		}
		slog.Info("update checksum available, will verify after download", "checksum", app.updateStatus.Checksum[:16]+"...")

		stagingDir, err := downloader.DownloadToStaging(app.updateStatus.DownloadURL, runtime.GOOS, app.updateStatus.Checksum)
		if err != nil {
			slog.Error("failed to download update", "error", err)
			app.showError(fmt.Errorf("failed to download update: %w", err))
			return
		}

		targetPath, err := update.GetTargetPath()
		if err != nil {
			slog.Error("failed to get target path", "error", err)
			os.RemoveAll(stagingDir)
			app.showError(fmt.Errorf("failed to get target path: %w", err))
			return
		}

		slog.Info("launching installer", "staging", stagingDir, "target", targetPath)
		err = update.LaunchInstaller(stagingDir, targetPath)
		if err == update.ErrInstallerLaunched {
			// Installer launched successfully - request graceful shutdown
			slog.Info("installer launched, requesting shutdown")
			app.requestShutdown(0)
			return
		}
		if err != nil {
			slog.Error("failed to launch installer", "error", err)
			os.RemoveAll(stagingDir)
			app.showError(fmt.Errorf("failed to launch installer: %w", err))
			return
		}
	}()
}

// updateDockMenu updates the dock menu with recent VMs
func (app *Application) updateDockMenu() {
	if app.window == nil {
		return
	}

	dm, ok := app.window.PlatformWindow().(window.DockMenuSupport)
	if !ok {
		return
	}

	var items []window.DockMenuItem

	// Add recent VMs
	if app.recentVMs != nil {
		for i, vm := range app.recentVMs.GetRecent() {
			items = append(items, window.DockMenuItem{
				Title:   vm.Name,
				Tag:     100 + i, // Tags 100-104 for recent VMs
				Enabled: true,
			})
		}
	}

	// Separator
	if len(items) > 0 {
		items = append(items, window.DockMenuItem{Separator: true})
	}

	// New Custom VM option
	items = append(items, window.DockMenuItem{
		Title:   "New Custom VM...",
		Tag:     1,
		Enabled: true,
	})

	dm.SetDockMenu(items, app.handleDockMenuClick)
}

// handleDockMenuClick handles dock menu item clicks
func (app *Application) handleDockMenuClick(tag int) {
	slog.Info("dock menu click", "tag", tag)

	if tag == 1 {
		// New Custom VM
		app.customVMScreen = NewCustomVMScreen(app)
		app.mode = modeCustomVM
		return
	}

	if tag >= 100 && tag < 105 && app.recentVMs != nil {
		// Recent VM
		index := tag - 100
		recent := app.recentVMs.GetRecent()
		if index >= 0 && index < len(recent) {
			vm := recent[index]
			app.startCustomVM(vm.SourceType, vm.SourcePath)
		}
	}
}

// getBundlesDir returns the path to the bundles directory in the user's config directory.
func getBundlesDir() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(configDir, "ccapp", "bundles")
}

func (app *Application) Run() error {
	var err error

	slog.Info("creating window")
	app.window, err = graphics.New("CrumbleCracker", 1024, 768)
	if err != nil {
		return fmt.Errorf("failed to create window: %w", err)
	}

	slog.Info("loading text renderer")
	app.text, err = text.Load(app.window)
	if err != nil {
		return fmt.Errorf("failed to create text renderer: %w", err)
	}

	slog.Info("loading logo")
	app.logo, err = graphics.LoadSVG(app.window, assets.LogoWhite)
	if err != nil {
		return fmt.Errorf("failed to load logo svg: %w", err)
	}

	app.window.SetClear(true)
	app.window.SetClearColor(colorBackground) // Tokyo Night background

	// Initialize blur effect for dialog overlays
	blurEffect, err := app.window.NewBlurEffect()
	if err != nil {
		slog.Warn("failed to create blur effect", "error", err)
	} else {
		app.blurEffect = blurEffect
	}

	app.start = time.Now()
	app.selectedIndex = -1
	app.mode = modeLauncher

	// Discover bundles
	bundlesDir := getBundlesDir()
	app.bundlesDir = bundlesDir

	// Ensure bundles directory exists
	if bundlesDir != "" {
		if err := os.MkdirAll(bundlesDir, 0o755); err != nil {
			slog.Warn("failed to create bundles directory", "dir", bundlesDir, "error", err)
		}
	}

	app.bundles, err = discoverBundles(bundlesDir)
	if err != nil {
		slog.Warn("failed to discover bundles", "error", err)
	}
	slog.Info("bundle discovery complete", "bundles_dir", bundlesDir, "bundle_count", len(app.bundles))

	// Initialize recent VMs store
	recentStore, err := NewRecentVMsStore()
	if err != nil {
		slog.Warn("failed to initialize recent VMs store", "error", err)
	} else {
		app.recentVMs = recentStore
		slog.Info("loaded recent VMs store", "count", len(recentStore.GetRecent()))
	}

	// Initialize settings store
	settingsStore, err := NewSettingsStore()
	if err != nil {
		slog.Warn("failed to initialize settings store", "error", err)
	} else {
		app.settings = settingsStore
		slog.Info("loaded settings store", "onboarding_completed", settingsStore.Get().OnboardingCompleted)

		// Handle pending cleanup from previous installation
		if cleanup := settingsStore.Get().CleanupPending; cleanup != "" {
			// Validate cleanup path before deletion to prevent TOCTOU attacks
			if err := update.ValidateCleanupPath(cleanup); err != nil {
				slog.Warn("cleanup path validation failed, skipping cleanup", "path", cleanup, "error", err)
				settingsStore.ClearCleanupPending()
			} else {
				slog.Info("performing pending cleanup", "path", cleanup)
				if err := update.DeleteApp(cleanup); err != nil {
					slog.Warn("failed to clean up old app", "path", cleanup, "error", err)
				} else {
					slog.Info("cleaned up old app", "path", cleanup)
				}
				settingsStore.ClearCleanupPending()
			}
		}
	}

	// Load site config (deployment-wide settings from file next to app)
	siteConfig := LoadSiteConfig()

	// Initialize update checker
	cacheDir, _ := os.UserCacheDir()
	app.updateChecker = update.NewChecker(Version, filepath.Join(cacheDir, "ccapp"))
	app.updateChecker.SetLogger(slog.Default())
	slog.Info("initialized update checker", "version", Version)

	// Check for updates in background (site config overrides user settings)
	autoUpdateEnabled := app.settings == nil || app.settings.Get().AutoUpdateEnabled
	if siteConfig.AutoUpdateEnabled != nil {
		autoUpdateEnabled = *siteConfig.AutoUpdateEnabled
		slog.Info("site config: auto-update override", "enabled", autoUpdateEnabled)
	}
	if autoUpdateEnabled {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			status := app.updateChecker.CheckWithContext(ctx)
			if status.Error != nil {
				slog.Warn("update check failed", "error", status.Error)
				return
			}
			app.updateStatus = &status
			if status.Available {
				slog.Info("update available", "current", status.CurrentVersion, "latest", status.LatestVersion)
			} else {
				slog.Debug("no update available", "current", status.CurrentVersion)
			}
		}()
	} else {
		slog.Info("auto-update check disabled")
	}

	// Set up dock menu (macOS only)
	app.updateDockMenu()

	// Set up URL handler for Apple Events (macOS only)
	if urlSupport, ok := app.window.PlatformWindow().(window.URLEventSupport); ok {
		urlSupport.SetURLHandler(func(url string) {
			// Validate length before storing (handler may be called from another thread)
			if len(url) > 1024 {
				slog.Warn("received URL via Apple Event exceeds max length, ignoring")
				return
			}
			slog.Info("received URL via Apple Event")

			app.pendingURLMu.Lock()
			app.pendingURL = url
			app.pendingURLMu.Unlock()

			// If we're in a state that can handle URLs, process it
			// Note: mode is only modified on main thread, safe to read here
			if app.mode == modeLauncher || app.mode == modeOnboarding {
				app.handlePendingURL()
			}
		})
		slog.Info("URL event handler registered")
	}

	// Initialize built-in UI screens
	app.launcherScreen = NewLauncherScreen(app)
	app.terminalScreen = NewTerminalScreen(app)

	// Open hypervisor at startup to check availability
	slog.Info("opening hypervisor")
	h, hvErr := factory.Open()
	if hvErr != nil {
		slog.Error("hypervisor unavailable at startup", "error", hvErr)
		app.errMsg = formatHypervisorError(hvErr)
		app.fatalError = true
		app.mode = modeError
	} else {
		app.hypervisor = h
		slog.Info("hypervisor opened successfully")
	}

	// Determine if onboarding should be shown (only if not in error state)
	if app.mode != modeError {
		showOnboarding := false

		// Site config can skip onboarding entirely
		if siteConfig.SkipOnboarding {
			slog.Info("site config: skipping onboarding")
			showOnboarding = false
		} else if app.settings != nil {
			settings := app.settings.Get()
			// Show if onboarding not completed
			if !settings.OnboardingCompleted {
				showOnboarding = true
			}
			// Also show if not in standard location (even if previously completed elsewhere)
			if !update.IsInStandardLocation() {
				currentPath, _ := update.GetTargetPath()
				if settings.InstallPath == "" || settings.InstallPath != currentPath {
					showOnboarding = true
				}
			}
		} else {
			// No settings file exists - first run
			showOnboarding = true
		}

		if showOnboarding {
			app.mode = modeOnboarding
			slog.Info("showing onboarding screen")
		}
	}

	// Handle pending URL if present (from command line argument)
	app.pendingURLMu.Lock()
	hasPendingURL := app.pendingURL != ""
	app.pendingURLMu.Unlock()
	if hasPendingURL && app.mode != modeError {
		app.handlePendingURL()
	}

	// Close hypervisor on exit if it wasn't transferred to a VM
	defer func() {
		if app.hypervisor != nil {
			app.hypervisor.Close()
			app.hypervisor = nil
		}
	}()

	// Stop running VM on exit
	defer func() {
		if app.running != nil {
			slog.Info("stopping VM on shutdown")
			app.stopVM()
		}
	}()

	err = app.window.Loop(func(f graphics.Frame) error {
		// Check for shutdown request
		if app.shutdownRequested {
			slog.Info("shutdown requested, exiting main loop", "code", app.shutdownCode)
			return fmt.Errorf("shutdown requested")
		}

		switch app.mode {
		case modeLauncher:
			return app.renderLauncher(f)
		case modeOnboarding:
			return app.renderOnboarding(f)
		case modeLoading:
			return app.renderLoading(f)
		case modeError:
			return app.renderError(f)
		case modeTerminal:
			return app.renderTerminal(f)
		case modeCustomVM:
			return app.renderCustomVM(f)
		case modeInstalling:
			return app.renderInstalling(f)
		case modeSettings:
			return app.renderSettings(f)
		case modeDeleteConfirm:
			return app.renderDeleteConfirm(f)
		case modeUpdating:
			return app.renderLoading(f)
		case modeAppSettings:
			return app.renderAppSettings(f)
		case modeUpdateConfirm:
			return app.renderUpdateConfirm(f)
		case modeURLConfirm:
			return app.renderURLConfirm(f)
		default:
			return nil
		}
	})

	// If shutdown was requested, exit with the requested code (after defers run)
	if app.shutdownRequested {
		slog.Info("exiting after shutdown cleanup", "code", app.shutdownCode)
		os.Exit(app.shutdownCode)
	}

	return err
}

func (app *Application) showError(err error) {
	if err == nil {
		err = fmt.Errorf("unknown error")
	}
	app.errMsg = err.Error()
	app.mode = modeError
}

// requestShutdown requests a graceful shutdown of the application.
// The main loop will check this and perform cleanup before exiting.
func (app *Application) requestShutdown(code int) {
	app.shutdownRequested = true
	app.shutdownCode = code
}

// formatHypervisorError returns a user-friendly error message for hypervisor failures.
func formatHypervisorError(err error) string {
	errStr := err.Error()

	// Linux KVM errors
	if strings.Contains(errStr, "/dev/kvm") {
		if strings.Contains(errStr, "permission denied") {
			return "Cannot access /dev/kvm: permission denied.\n\n" +
				"Add your user to the 'kvm' group:\n" +
				"  sudo usermod -aG kvm $USER\n\n" +
				"Then log out and log back in."
		}
		if strings.Contains(errStr, "no such file or directory") {
			return "KVM is not available (/dev/kvm not found).\n\n" +
				"Possible solutions:\n" +
				"  1. Enable virtualization (VT-x/AMD-V) in your BIOS/UEFI settings\n" +
				"  2. Install KVM: sudo apt install qemu-kvm (Debian/Ubuntu)\n" +
				"  3. Load the KVM module: sudo modprobe kvm"
		}
	}

	// macOS HVF errors
	if strings.Contains(errStr, "Hypervisor.framework") {
		return "Cannot load macOS Hypervisor.framework.\n\n" +
			"This may happen if:\n" +
			"  1. Virtualization is disabled in your Mac's security settings\n" +
			"  2. The app is not properly signed\n" +
			"  3. Your Mac doesn't support the Hypervisor framework"
	}

	// Windows WHP errors
	if strings.Contains(errStr, "hypervisor not present") || strings.Contains(errStr, "whp:") {
		return "Windows Hypervisor Platform is not available.\n\n" +
			"To enable it:\n" +
			"  1. Open 'Turn Windows features on or off'\n" +
			"  2. Enable 'Windows Hypervisor Platform'\n" +
			"  3. Restart your computer"
	}

	// Unsupported platform
	if strings.Contains(errStr, "unsupported") {
		return "Hypervisor is not supported on this platform.\n\n" +
			"Virtualization may not be available on your hardware or operating system."
	}

	// Generic fallback
	return fmt.Sprintf("Failed to initialize hypervisor: %s\n\nVMs cannot be started.", errStr)
}

func (app *Application) renderLauncher(f graphics.Frame) error {
	return app.launcherScreen.Render(f)
}

func (app *Application) renderOnboarding(f graphics.Frame) error {
	if app.onboardingScreen == nil {
		app.onboardingScreen = NewOnboardingScreen(app)
	}
	return app.onboardingScreen.Render(f)
}

func (app *Application) renderLoading(f graphics.Frame) error {
	// Check for background prep completion.
	if app.bootCh != nil {
		select {
		case res := <-app.bootCh:
			app.bootCh = nil
			if res.err != nil {
				slog.Error("failed to prepare VM boot", "error", res.err)
				app.selectedIndex = -1
				app.showError(res.err)
				return nil
			}
			if err := app.finalizeBoot(res.prep); err != nil {
				slog.Error("failed to finalize VM boot", "error", err)
				app.selectedIndex = -1
				app.showError(err)
				return nil
			}
			app.mode = modeTerminal
			return nil
		default:
		}
	}

	// Create or use cached loading screen
	if app.loadingScreen == nil {
		app.loadingScreen = NewLoadingScreen(app)
	}
	return app.loadingScreen.Render(f)
}

func (app *Application) renderError(f graphics.Frame) error {
	// Create or use cached error screen (rebuild if error message changed)
	if app.errorScreen == nil {
		app.errorScreen = NewErrorScreen(app)
	}
	return app.errorScreen.Render(f)
}

func (app *Application) renderCustomVM(f graphics.Frame) error {
	if app.customVMScreen == nil {
		app.customVMScreen = NewCustomVMScreen(app)
	}

	// Render launcher as background without processing events
	// (events should go to the custom VM dialog instead)
	app.launcherScreen.RenderBackground(f)

	// Render the custom VM dialog on top
	return app.customVMScreen.Render(f)
}

func (app *Application) renderSettings(f graphics.Frame) error {
	if app.settingsScreen == nil {
		app.settingsScreen = NewSettingsScreen(app, app.selectedSettingsIndex)
	}

	// Render launcher as background
	app.launcherScreen.RenderBackground(f)

	// Render the settings dialog on top
	return app.settingsScreen.Render(f)
}

func (app *Application) renderAppSettings(f graphics.Frame) error {
	if app.appSettingsScreen == nil {
		app.appSettingsScreen = NewAppSettingsScreen(app)
	}

	// Render launcher as background
	app.launcherScreen.RenderBackground(f)

	// Render the app settings dialog on top
	return app.appSettingsScreen.Render(f)
}

func (app *Application) showAppSettings() {
	app.appSettingsScreen = nil // Force rebuild to get fresh settings
	app.mode = modeAppSettings
}

func (app *Application) renderDeleteConfirm(f graphics.Frame) error {
	if app.deleteConfirmScreen == nil {
		app.deleteConfirmScreen = NewDeleteConfirmScreen(app, app.selectedSettingsIndex)
	}

	// Render launcher as background
	app.launcherScreen.RenderBackground(f)

	// Render the delete confirmation dialog on top
	return app.deleteConfirmScreen.Render(f)
}

func (app *Application) renderUpdateConfirm(f graphics.Frame) error {
	if app.updateConfirmScreen == nil {
		// Should not happen - screen must be created with status
		return nil
	}

	// Render launcher as background
	app.launcherScreen.RenderBackground(f)

	// Render the update confirmation dialog on top
	return app.updateConfirmScreen.Render(f)
}

func (app *Application) showUpdateConfirm(status *update.UpdateStatus) {
	app.updateConfirmScreen = NewUpdateConfirmScreen(app, status)
	app.mode = modeUpdateConfirm
}

func (app *Application) showSettings(bundleIndex int) {
	app.selectedSettingsIndex = bundleIndex
	app.settingsScreen = nil // Force rebuild with new bundle
	app.mode = modeSettings
}

func (app *Application) showDeleteConfirm(bundleIndex int) {
	app.selectedSettingsIndex = bundleIndex
	app.deleteConfirmScreen = nil // Force rebuild
	app.mode = modeDeleteConfirm
}

func (app *Application) renderURLConfirm(f graphics.Frame) error {
	if app.urlConfirmScreen == nil {
		return nil
	}

	// Render launcher as background
	app.launcherScreen.RenderBackground(f)

	// Render the URL confirmation dialog on top
	return app.urlConfirmScreen.Render(f)
}

func (app *Application) handlePendingURL() {
	// Take URL under lock and clear it
	app.pendingURLMu.Lock()
	url := app.pendingURL
	app.pendingURL = ""
	app.pendingURLMu.Unlock()

	if url == "" {
		return
	}

	action, err := ParseCrumbleCrackerURL(url)
	if err != nil {
		slog.Error("invalid URL", "url", url, "error", err)
		app.showError(fmt.Errorf("invalid URL: %w", err))
		return
	}

	if err := ValidateURLAction(action); err != nil {
		slog.Error("invalid URL action", "url", url, "error", err)
		app.showError(err)
		return
	}

	slog.Info("handling URL", "action", action.Action, "image", action.ImageRef)

	switch action.Action {
	case "run":
		// Show confirmation dialog
		app.urlConfirmScreen = NewURLConfirmScreen(app, action.ImageRef)
		app.mode = modeURLConfirm
	default:
		app.showError(fmt.Errorf("unknown action: %s", action.Action))
	}
}

func (app *Application) refreshBundles() {
	bundles, err := discoverBundles(app.bundlesDir)
	if err != nil {
		slog.Warn("failed to refresh bundles", "error", err)
	} else {
		app.bundles = bundles
	}
	// Rebuild launcher screen to show updated bundles
	app.launcherScreen = NewLauncherScreen(app)
}

func (app *Application) renderInstalling(f graphics.Frame) error {
	// Check for installation completion
	if app.installCh != nil {
		select {
		case res := <-app.installCh:
			app.installCh = nil
			if res.err != nil {
				slog.Error("failed to install bundle", "error", res.err)
				app.showError(res.err)
				return nil
			}
			// Refresh bundles list
			bundles, err := discoverBundles(app.bundlesDir)
			if err != nil {
				slog.Warn("failed to refresh bundles", "error", err)
			} else {
				app.bundles = bundles
			}
			// Rebuild launcher screen to show new bundle
			app.launcherScreen = NewLauncherScreen(app)
			app.mode = modeLauncher
			slog.Info("bundle installed successfully", "path", res.bundlePath)
			return nil
		default:
		}
	}

	// Reuse loading screen UI for installation progress
	if app.loadingScreen == nil {
		app.loadingScreen = NewLoadingScreen(app)
	}
	return app.loadingScreen.Render(f)
}

func (app *Application) renderTerminal(f graphics.Frame) error {
	if app.running == nil || app.running.termView == nil {
		app.mode = modeLauncher
		return nil
	}

	w, h := f.WindowSize()
	app.text.SetViewport(int32(w), int32(h))
	winW := float32(w)

	// Render the widget-based notch bar
	app.terminalScreen.RenderNotch(f)

	// Track mouse state for confirmation dialog
	mx, my := f.CursorPos()
	leftDown := f.GetButtonState(window.ButtonLeft).IsDown()
	justPressed := leftDown && !app.prevLeftDown
	app.prevLeftDown = leftDown

	// Colors for confirmation dialog (from design.go)
	btnNormal := colorBtnAlt
	btnHover := colorBtnHover
	btnPressed := colorBtnAltPressed
	exitBtnHover := colorRed
	textColorDialogDark := colorTextDark
	textColorDialogLight := colorTextPrimary

	// Render exit confirmation dialog
	if app.showExitConfirm {
		// Darken background
		overlay := color.RGBA{R: 0, G: 0, B: 0, A: overlayAlphaLight}
		f.RenderQuad(0, 0, winW, float32(h), nil, overlay)

		// Dialog box dimensions (from design.go)
		dialogW := exitDialogWidth
		dialogH := exitDialogHeight
		dialogX := (winW - dialogW) / 2
		dialogY := (float32(h) - dialogH) / 2
		dialogBg := colorBackground
		dialogCornerRadius := cornerRadiusLarge
		btnCornerRadius := cornerRadiusSmall

		// Initialize or update dialog background shape
		dialogBgRect := rect{x: dialogX, y: dialogY, w: dialogW, h: dialogH}
		if app.dialogBgShape == nil {
			segments := graphics.SegmentsForRadius(dialogCornerRadius)
			app.dialogBgShape, _ = graphics.NewShapeBuilder(app.window, segments)
		}
		if dialogBgRect != app.dialogLastBgR {
			app.dialogBgShape.UpdateRoundedRect(dialogX, dialogY, dialogW, dialogH,
				graphics.UniformRadius(dialogCornerRadius),
				graphics.ShapeStyle{FillColor: dialogBg})
			app.dialogLastBgR = dialogBgRect
		}
		f.RenderMesh(app.dialogBgShape.Mesh(), graphics.DrawOptions{})

		// Title (use lighter text for title on dark background)
		app.text.RenderText("Shut down VM?", dialogX+70, dialogY+35, 16, colorTextPrimary)

		// Confirm button
		confirmRect := rect{x: dialogX + 30, y: dialogY + 60, w: 100, h: 30}
		confirmHover := confirmRect.contains(mx, my)
		confirmColor := exitBtnHover
		if confirmHover && leftDown {
			confirmColor = btnPressed
		}

		// Initialize or update confirm button shape (always update for hover color changes)
		if app.confirmBtnShape == nil {
			segments := graphics.SegmentsForRadius(btnCornerRadius)
			app.confirmBtnShape, _ = graphics.NewShapeBuilder(app.window, segments)
		}
		app.confirmBtnShape.UpdateRoundedRect(confirmRect.x, confirmRect.y, confirmRect.w, confirmRect.h,
			graphics.UniformRadius(btnCornerRadius),
			graphics.ShapeStyle{FillColor: confirmColor})
		f.RenderMesh(app.confirmBtnShape.Mesh(), graphics.DrawOptions{})
		app.text.RenderText("Shut Down", confirmRect.x+12, confirmRect.y+20, 14, textColorDialogDark)

		// Cancel button
		cancelRect := rect{x: dialogX + 150, y: dialogY + 60, w: 100, h: 30}
		cancelHover := cancelRect.contains(mx, my)
		cancelColor := btnNormal
		if cancelHover {
			cancelColor = btnHover
		}
		if cancelHover && leftDown {
			cancelColor = btnPressed
		}

		// Initialize or update cancel button shape (always update for hover color changes)
		if app.cancelBtnShape == nil {
			segments := graphics.SegmentsForRadius(btnCornerRadius)
			app.cancelBtnShape, _ = graphics.NewShapeBuilder(app.window, segments)
		}
		app.cancelBtnShape.UpdateRoundedRect(cancelRect.x, cancelRect.y, cancelRect.w, cancelRect.h,
			graphics.UniformRadius(btnCornerRadius),
			graphics.ShapeStyle{FillColor: cancelColor})
		f.RenderMesh(app.cancelBtnShape.Mesh(), graphics.DrawOptions{})
		app.text.RenderText("Cancel", cancelRect.x+28, cancelRect.y+20, 14, textColorDialogLight)

		// Handle dialog button clicks
		if justPressed && confirmRect.contains(mx, my) {
			slog.Info("shutdown confirmed; stopping VM")
			app.showExitConfirm = false
			app.stopVM()
			return nil
		}
		if justPressed && cancelRect.contains(mx, my) {
			slog.Info("shutdown cancelled")
			app.showExitConfirm = false
		}
	}

	// Check if VM has exited.
	select {
	case err := <-app.running.session.Done:
		if err != nil && err != io.EOF {
			slog.Error("VM exited with error", "error", err)
		}
		slog.Info("VM session ended; cleaning up")
		app.stopVM()
		return nil
	default:
	}

	// Render terminal view.
	return app.running.termView.Step(f, termwin.Hooks{
		OnResize: func(cols, rows int) {
			if app.running != nil && app.running.vm != nil {
				app.running.vm.SetConsoleSize(cols, rows)
			}
		},
	})
}

func (app *Application) startBootBundle(index int) {
	if index < 0 || index >= len(app.bundles) {
		return
	}
	// Prevent overlapping boot attempts.
	if app.bootCh != nil {
		return
	}

	b := app.bundles[index]
	name := b.Meta.Name
	if name == "" || name == "{{name}}" {
		name = filepath.Base(b.Dir)
	}

	// Record to recent VMs
	if app.recentVMs != nil {
		app.recentVMs.AddOrUpdate(RecentVM{
			Name:       name,
			SourceType: VMSourceBundle,
			SourcePath: b.Dir,
		})
		app.updateDockMenu()
	}

	hvArch, err := parseArchitecture(runtime.GOARCH)
	if err != nil {
		slog.Error("failed to determine architecture", "goarch", runtime.GOARCH, "error", err)
		app.selectedIndex = -1
		app.showError(err)
		return
	}

	app.bootProgressMu.Lock()
	app.bootStarted = time.Now()
	app.bootName = name
	app.bootProgressMu.Unlock()
	app.mode = modeLoading

	ch := make(chan bootResult, 1)
	app.bootCh = ch

	// Transfer ownership of pre-opened hypervisor to the boot prep
	preOpenedHV := app.hypervisor
	app.hypervisor = nil

	// Background prep: do anything slow/off-GPU here (disk IO, kernel fetch, etc).
	go func(b discoveredBundle, arch hv.CpuArchitecture, hvArg hv.Hypervisor, out chan<- bootResult) {
		prep, err := prepareBootBundle(b, arch, hvArg)
		out <- bootResult{prep: prep, err: err}
	}(b, hvArch, preOpenedHV, ch)
}

func prepareBootBundle(b discoveredBundle, hvArch hv.CpuArchitecture, preOpenedHV hv.Hypervisor) (_ *bootPrep, retErr error) {
	if b.Dir == "" {
		return nil, fmt.Errorf("invalid bundle: empty dir")
	}
	slog.Info("boot bundle prep started", "bundle_dir", b.Dir, "bundle_name", b.Meta.Name, "arch", hvArch)

	prep := &bootPrep{hvArch: hvArch}
	defer func() {
		if retErr != nil {
			// Best-effort cleanup on failure.
			if prep.hypervisor != nil {
				_ = prep.hypervisor.Close()
			}
			if prep.netBackend != nil {
				prep.netBackend.Close()
			}
			if prep.containerFS != nil {
				prep.containerFS.Close()
			}
		}
	}()

	// Load image from bundle
	imageDir := filepath.Join(b.Dir, b.Meta.Boot.ImageDir)
	if b.Meta.Boot.ImageDir == "" {
		imageDir = filepath.Join(b.Dir, "image")
	}
	slog.Info("loading image from bundle", "image_dir", imageDir)

	img, err := oci.LoadFromDir(imageDir)
	if err != nil {
		return nil, fmt.Errorf("load image: %w", err)
	}

	// Determine command
	cmd := b.Meta.Boot.Command
	execCmd := img.Command(cmd)
	if len(execCmd) == 0 {
		return nil, fmt.Errorf("no command specified and image has no entrypoint/cmd")
	}
	slog.Info("resolved container command", "cmd", execCmd)

	// Create container filesystem
	containerFS, err := oci.NewContainerFS(img)
	if err != nil {
		return nil, fmt.Errorf("create container filesystem: %w", err)
	}
	prep.containerFS = containerFS

	// Resolve command path
	pathEnv := extractInitialPath(img.Config.Env)
	workDir := containerWorkDir(img)
	execCmd, err = resolveCommandPath(containerFS, execCmd, pathEnv, workDir)
	if err != nil {
		return nil, fmt.Errorf("resolve command: %w", err)
	}
	slog.Info("resolved command path", "exec", execCmd, "work_dir", workDir)
	prep.execCmd = execCmd
	prep.env = img.Config.Env
	// Append custom environment variables from bundle metadata
	if len(b.Meta.Boot.Env) > 0 {
		prep.env = append(prep.env, b.Meta.Boot.Env...)
		slog.Info("added custom env vars from bundle", "count", len(b.Meta.Boot.Env))
	}
	// Ensure TERM is set for the container so terminal apps work correctly.
	if !hasEnvVar(prep.env, "TERM") {
		prep.env = append(prep.env, "TERM=xterm-256color")
	}
	prep.workDir = workDir

	// Create VirtioFS backend
	fsBackend := vfs.NewVirtioFsBackendWithAbstract()
	if err := fsBackend.SetAbstractRoot(containerFS); err != nil {
		return nil, fmt.Errorf("set container filesystem as root: %w", err)
	}
	prep.fsBackend = fsBackend

	// Use pre-opened hypervisor or create a new one
	if preOpenedHV != nil {
		slog.Info("using pre-opened hypervisor")
		prep.hypervisor = preOpenedHV
	} else {
		slog.Info("opening new hypervisor")
		h, err := factory.OpenWithArchitecture(hvArch)
		if err != nil {
			return nil, fmt.Errorf("create hypervisor: %w", err)
		}
		prep.hypervisor = h
	}

	// Load kernel
	kernelLoader, err := kernel.LoadForArchitecture(hvArch)
	if err != nil {
		return nil, fmt.Errorf("load kernel: %w", err)
	}
	slog.Info("kernel loader ready")
	prep.kernelLoader = kernelLoader

	// VM options
	prep.cpus = b.Meta.Boot.CPUs
	if prep.cpus == 0 {
		prep.cpus = 1
	}
	prep.memoryMB = uint64(b.Meta.Boot.MemoryMB)
	if prep.memoryMB == 0 {
		prep.memoryMB = 1024
	}
	prep.dmesg = b.Meta.Boot.Dmesg
	prep.exec = b.Meta.Boot.Exec
	slog.Info("vm config (prep)", "cpus", prep.cpus, "memory_mb", prep.memoryMB, "dmesg", prep.dmesg, "exec", prep.exec)

	// Always create netstack and attach network device with internet enabled.
	slog.Info("starting netstack DNS server")
	netBackend := netstack.New(slog.Default())
	if err := netBackend.StartDNSServer(); err != nil {
		return nil, fmt.Errorf("start DNS server: %w", err)
	}
	prep.netBackend = netBackend

	mac := net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	virtioNet, err := virtio.NewNetstackBackend(netBackend, mac)
	if err != nil {
		return nil, fmt.Errorf("create netstack backend: %w", err)
	}
	prep.virtioNet = virtioNet

	slog.Info("boot bundle prep complete", "bundle_dir", b.Dir, "bundle_name", b.Meta.Name)
	return prep, nil
}

func (app *Application) finalizeBoot(prep *bootPrep) (retErr error) {
	if prep == nil {
		return fmt.Errorf("nil boot prep")
	}
	if prep.hypervisor == nil || prep.kernelLoader == nil || prep.containerFS == nil || prep.fsBackend == nil {
		return fmt.Errorf("incomplete boot prep")
	}

	// If we fail after this point, ensure cleanup.
	defer func() {
		if retErr != nil {
			if app.running != nil {
				app.stopVM()
			} else {
				if prep.netBackend != nil {
					prep.netBackend.Close()
				}
				if prep.containerFS != nil {
					prep.containerFS.Close()
				}
				if prep.hypervisor != nil {
					_ = prep.hypervisor.Close()
				}
			}
		}
	}()

	// Create terminal view (must be on the main thread for GPU resources).
	termView, err := termwin.NewView(app.window)
	if err != nil {
		return fmt.Errorf("create terminal view: %w", err)
	}
	// Drain any text input that accumulated while user was typing in UI fields
	// (e.g., image name). Otherwise, the terminal view would receive this text
	// as keyboard input when it starts processing.
	_ = app.window.PlatformWindow().TextInput()
	// Terminal fills the full window - the notch overlays on top
	termView.SetInsets(0, 0, 0, 0)
	// Apply the app's color scheme to the terminal.
	termView.SetColorScheme(terminalColorScheme())

	opts := []initx.Option{
		initx.WithDeviceTemplate(virtio.FSTemplate{
			Tag:     "rootfs",
			Backend: prep.fsBackend,
			Arch:    prep.hvArch,
		}),
		initx.WithStdin(termView),
		initx.WithConsoleOutput(termView),
		initx.WithDmesgLogging(prep.dmesg),
	}

	// Always attach network device with internet access enabled.
	if prep.netBackend == nil || prep.virtioNet == nil {
		termView.Close()
		return fmt.Errorf("netstack was not prepared")
	}
	mac := net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	opts = append(opts, initx.WithDeviceTemplate(virtio.NetTemplate{
		Backend: prep.virtioNet,
		MAC:     mac,
		Arch:    prep.hvArch,
	}))

	vm, err := initx.NewVirtualMachine(prep.hypervisor, prep.cpus, prep.memoryMB, prep.kernelLoader, opts...)
	if err != nil {
		termView.Close()
		return fmt.Errorf("create VM: %w", err)
	}

	// Build init program with network always enabled.
	prog, err := initx.BuildContainerInitProgram(initx.ContainerInitConfig{
		Arch:          prep.hvArch,
		Cmd:           prep.execCmd,
		Env:           prep.env,
		WorkDir:       prep.workDir,
		EnableNetwork: true,
		Exec:          prep.exec,
	})
	if err != nil {
		vm.Close()
		termView.Close()
		return err
	}

	session := initx.StartSession(context.Background(), vm, prog, initx.SessionConfig{})
	slog.Info("VM session started")

	app.running = &runningVM{
		vm:          vm,
		session:     session,
		termView:    termView,
		containerFS: prep.containerFS,
		netBackend:  prep.netBackend,
	}
	return nil
}

func (app *Application) stopVM() {
	if app.running == nil {
		app.mode = modeLauncher
		return
	}

	slog.Info("stopping VM")
	// Wait briefly for VM to exit
	if app.running.session != nil {
		if err := app.running.session.Stop(2 * time.Second); err != nil {
			slog.Warn("session stop returned error", "error", err)
		}
	}

	if app.running.termView != nil {
		app.running.termView.Close()
	}
	if app.running.vm != nil {
		app.running.vm.Close()
	}
	if app.running.containerFS != nil {
		app.running.containerFS.Close()
	}
	if app.running.netBackend != nil {
		app.running.netBackend.Close()
	}

	app.running = nil
	app.mode = modeLauncher
	app.selectedIndex = -1

	// Reset notch UI state for next VM
	app.networkDisabled = false
	app.showExitConfirm = false

	slog.Info("VM stopped; returned to launcher")
}

// startCustomVM starts a VM from a custom source (tarball, image name, or bundle directory)
func (app *Application) startCustomVM(sourceType VMSourceType, sourcePath string) {
	if app.bootCh != nil {
		return // Already booting
	}

	hvArch, err := parseArchitecture(runtime.GOARCH)
	if err != nil {
		slog.Error("failed to determine architecture", "goarch", runtime.GOARCH, "error", err)
		app.showError(err)
		return
	}

	displayName := filepath.Base(sourcePath)
	if sourceType == VMSourceImageName {
		displayName = sourcePath
	}

	app.bootProgressMu.Lock()
	app.bootStarted = time.Now()
	app.bootName = displayName
	app.bootProgressMu.Unlock()
	app.mode = modeLoading
	app.loadingScreen = nil // Force rebuild to show correct name

	ch := make(chan bootResult, 1)
	app.bootCh = ch

	// Transfer ownership of pre-opened hypervisor to the boot prep
	preOpenedHV := app.hypervisor
	app.hypervisor = nil

	// Create progress callback for image downloads
	progressCallback := func(progress oci.DownloadProgress) {
		app.bootProgressMu.Lock()
		app.bootProgress = progress
		app.bootProgressMu.Unlock()
	}

	go func(srcType VMSourceType, srcPath string, arch hv.CpuArchitecture, hvArg hv.Hypervisor, out chan<- bootResult, progressCb oci.ProgressCallback) {
		var prep *bootPrep
		var err error

		switch srcType {
		case VMSourceBundle:
			// Load bundle and use existing logic
			meta, metaErr := bundle.LoadMetadata(srcPath)
			if metaErr != nil {
				out <- bootResult{err: metaErr}
				return
			}
			prep, err = prepareBootBundle(discoveredBundle{Dir: srcPath, Meta: meta}, arch, hvArg)

		case VMSourceTarball:
			prep, err = prepareFromTarball(srcPath, arch, hvArg)

		case VMSourceImageName:
			prep, err = prepareFromImageName(srcPath, arch, hvArg, progressCb)

		default:
			err = fmt.Errorf("unknown source type: %s", srcType)
		}

		out <- bootResult{prep: prep, err: err}
	}(sourceType, sourcePath, hvArch, preOpenedHV, ch, progressCallback)
}

// installBundle installs a VM source as a bundle in the bundles directory.
// For image names and tarballs, it creates a new bundle. For bundle dirs, it validates only.
func (app *Application) installBundle(sourceType VMSourceType, sourcePath string, bundleName string) {
	if app.installCh != nil {
		return // Already installing
	}

	hvArch, err := parseArchitecture(runtime.GOARCH)
	if err != nil {
		slog.Error("failed to determine architecture", "goarch", runtime.GOARCH, "error", err)
		app.showError(err)
		return
	}

	app.installStarted = time.Now()
	app.installName = bundleName
	app.mode = modeInstalling
	app.loadingScreen = nil // Force rebuild to show correct name

	ch := make(chan installResult, 1)
	app.installCh = ch

	progressCallback := func(progress oci.DownloadProgress) {
		app.installProgressMu.Lock()
		app.installProgress = progress
		app.installProgressMu.Unlock()
	}

	go func() {
		var err error
		bundlePath := filepath.Join(app.bundlesDir, bundleName)

		switch sourceType {
		case VMSourceImageName:
			err = app.doInstallFromImageName(sourcePath, bundlePath, bundleName, hvArch, progressCallback)
		case VMSourceTarball:
			err = app.doInstallFromTarball(sourcePath, bundlePath, bundleName, hvArch)
		case VMSourceBundle:
			// For bundle dirs, just validate - don't copy
			err = bundle.ValidateBundleDir(sourcePath)
			if err == nil {
				bundlePath = sourcePath // Use original path
			}
		}

		ch <- installResult{bundlePath: bundlePath, err: err}
	}()
}

func (app *Application) doInstallFromImageName(imageName, bundlePath, name string, hvArch hv.CpuArchitecture, progress oci.ProgressCallback) error {
	slog.Info("installing image as bundle", "image", imageName, "dest", bundlePath)

	client, err := oci.NewClient("")
	if err != nil {
		return fmt.Errorf("create OCI client: %w", err)
	}
	client.SetProgressCallback(progress)

	img, err := client.PullForArch(imageName, hvArch)
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}

	return installImageAsBundle(img, bundlePath, name, imageName)
}

func (app *Application) doInstallFromTarball(tarPath, bundlePath, name string, hvArch hv.CpuArchitecture) error {
	slog.Info("installing tarball as bundle", "path", tarPath, "dest", bundlePath)

	client, err := oci.NewClient("")
	if err != nil {
		return fmt.Errorf("create OCI client: %w", err)
	}

	img, err := client.LoadFromTar(tarPath, hvArch)
	if err != nil {
		return fmt.Errorf("load tar: %w", err)
	}

	return installImageAsBundle(img, bundlePath, name, tarPath)
}

func installImageAsBundle(img *oci.Image, bundlePath, name, source string) error {
	// Create image directory
	imageDir := filepath.Join(bundlePath, "image")
	if err := oci.ExportToDir(img, imageDir); err != nil {
		return fmt.Errorf("export image: %w", err)
	}

	// Create bundle metadata
	meta := bundle.Metadata{
		Version:     1,
		Name:        name,
		Description: fmt.Sprintf("Installed from %s", filepath.Base(source)),
		Boot: bundle.BootConfig{
			ImageDir: "image",
			Exec:     true,
		},
	}

	if err := bundle.WriteTemplate(bundlePath, meta); err != nil {
		return fmt.Errorf("write bundle metadata: %w", err)
	}

	slog.Info("bundle installed successfully", "path", bundlePath)
	return nil
}

// prepareFromTarball loads a VM from an OCI tarball
func prepareFromTarball(tarPath string, hvArch hv.CpuArchitecture, preOpenedHV hv.Hypervisor) (*bootPrep, error) {
	slog.Info("loading VM from tarball", "path", tarPath, "arch", hvArch)

	client, err := oci.NewClient("")
	if err != nil {
		return nil, fmt.Errorf("create OCI client: %w", err)
	}

	img, err := client.LoadFromTar(tarPath, hvArch)
	if err != nil {
		return nil, fmt.Errorf("load tarball: %w", err)
	}

	return prepareFromImage(img, hvArch, preOpenedHV)
}

// prepareFromImageName pulls a container image and prepares it for boot
func prepareFromImageName(imageName string, hvArch hv.CpuArchitecture, preOpenedHV hv.Hypervisor, progressCallback oci.ProgressCallback) (*bootPrep, error) {
	slog.Info("pulling container image", "image", imageName, "arch", hvArch)

	client, err := oci.NewClient("")
	if err != nil {
		return nil, fmt.Errorf("create OCI client: %w", err)
	}

	// Set progress callback if provided
	if progressCallback != nil {
		client.SetProgressCallback(progressCallback)
	}

	img, err := client.PullForArch(imageName, hvArch)
	if err != nil {
		return nil, fmt.Errorf("pull image: %w", err)
	}

	return prepareFromImage(img, hvArch, preOpenedHV)
}

// prepareFromImage prepares a VM from an already-loaded OCI image
func prepareFromImage(img *oci.Image, hvArch hv.CpuArchitecture, preOpenedHV hv.Hypervisor) (_ *bootPrep, retErr error) {
	prep := &bootPrep{hvArch: hvArch}
	defer func() {
		if retErr != nil {
			if prep.hypervisor != nil {
				_ = prep.hypervisor.Close()
			}
			if prep.netBackend != nil {
				prep.netBackend.Close()
			}
			if prep.containerFS != nil {
				prep.containerFS.Close()
			}
		}
	}()

	// Determine command from image
	execCmd := img.Command(nil)
	if len(execCmd) == 0 {
		return nil, fmt.Errorf("image has no entrypoint/cmd")
	}
	slog.Info("container command", "cmd", execCmd)

	// Create container filesystem
	containerFS, err := oci.NewContainerFS(img)
	if err != nil {
		return nil, fmt.Errorf("create container filesystem: %w", err)
	}
	prep.containerFS = containerFS

	// Resolve command path
	pathEnv := extractInitialPath(img.Config.Env)
	workDir := containerWorkDir(img)
	execCmd, err = resolveCommandPath(containerFS, execCmd, pathEnv, workDir)
	if err != nil {
		return nil, fmt.Errorf("resolve command: %w", err)
	}
	slog.Info("resolved command path", "exec", execCmd, "work_dir", workDir)
	prep.execCmd = execCmd
	prep.env = img.Config.Env
	// Ensure TERM is set for the container so terminal apps work correctly.
	if !hasEnvVar(prep.env, "TERM") {
		prep.env = append(prep.env, "TERM=xterm-256color")
	}
	prep.workDir = workDir

	// Create VirtioFS backend
	fsBackend := vfs.NewVirtioFsBackendWithAbstract()
	if err := fsBackend.SetAbstractRoot(containerFS); err != nil {
		return nil, fmt.Errorf("set container filesystem as root: %w", err)
	}
	prep.fsBackend = fsBackend

	// Use pre-opened hypervisor or create a new one
	if preOpenedHV != nil {
		slog.Info("using pre-opened hypervisor")
		prep.hypervisor = preOpenedHV
	} else {
		slog.Info("opening new hypervisor")
		h, err := factory.OpenWithArchitecture(hvArch)
		if err != nil {
			return nil, fmt.Errorf("create hypervisor: %w", err)
		}
		prep.hypervisor = h
	}

	// Load kernel
	kernelLoader, err := kernel.LoadForArchitecture(hvArch)
	if err != nil {
		return nil, fmt.Errorf("load kernel: %w", err)
	}
	slog.Info("kernel loader ready")
	prep.kernelLoader = kernelLoader

	// VM options - use defaults
	prep.cpus = 1
	prep.memoryMB = 1024
	prep.dmesg = false
	prep.exec = true
	slog.Info("vm config", "cpus", prep.cpus, "memory_mb", prep.memoryMB)

	// Always create netstack and attach network device with internet enabled.
	slog.Info("starting netstack DNS server")
	netBackend := netstack.New(slog.Default())
	if err := netBackend.StartDNSServer(); err != nil {
		return nil, fmt.Errorf("start DNS server: %w", err)
	}
	prep.netBackend = netBackend

	mac := net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	virtioNet, err := virtio.NewNetstackBackend(netBackend, mac)
	if err != nil {
		return nil, fmt.Errorf("create netstack backend: %w", err)
	}
	prep.virtioNet = virtioNet

	slog.Info("VM prep from image complete")
	return prep, nil
}

func parseArchitecture(arch string) (hv.CpuArchitecture, error) {
	switch arch {
	case "amd64", "x86_64":
		return hv.ArchitectureX86_64, nil
	case "arm64", "aarch64":
		return hv.ArchitectureARM64, nil
	default:
		return "", fmt.Errorf("unsupported architecture: %s", arch)
	}
}

const defaultPathEnv = "/bin:/usr/bin"

func extractInitialPath(env []string) string {
	for _, entry := range env {
		if len(entry) > 5 && entry[:5] == "PATH=" {
			return entry[5:]
		}
	}
	return defaultPathEnv
}

func hasEnvVar(env []string, name string) bool {
	prefix := name + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

func containerWorkDir(img *oci.Image) string {
	if img.Config.WorkingDir == "" {
		return "/"
	}
	return img.Config.WorkingDir
}

func resolveCommandPath(fs *oci.ContainerFS, cmd []string, pathEnv string, workDir string) ([]string, error) {
	if len(cmd) == 0 {
		return nil, fmt.Errorf("empty command")
	}

	resolved := make([]string, len(cmd))
	copy(resolved, cmd)

	if len(resolved[0]) > 0 && resolved[0][0] == '/' {
		return resolved, nil
	}
	for _, c := range resolved[0] {
		if c == '/' {
			return resolved, nil
		}
	}

	resolvedPath, err := lookPath(fs, pathEnv, workDir, resolved[0])
	if err != nil {
		return nil, err
	}
	resolved[0] = resolvedPath
	return resolved, nil
}

func lookPath(fs *oci.ContainerFS, pathEnv string, workDir string, file string) (string, error) {
	if file == "" {
		return "", fmt.Errorf("executable name is empty")
	}
	if pathEnv == "" {
		pathEnv = defaultPathEnv
	}
	if workDir == "" {
		workDir = "/"
	}

	dirs := splitPath(pathEnv)
	for _, dir := range dirs {
		if dir == "" {
			dir = workDir
		} else if dir[0] != '/' {
			dir = workDir + "/" + dir
		}

		candidate := dir + "/" + file
		entry, err := fs.Lookup(candidate)
		if err != nil {
			continue
		}

		if entry.File == nil {
			continue
		}
		_, mode := entry.File.Stat()
		if mode.IsDir() || mode&0o111 == 0 {
			continue
		}

		return candidate, nil
	}

	return "", fmt.Errorf("executable %q not found in PATH", file)
}

func splitPath(pathEnv string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(pathEnv); i++ {
		if i == len(pathEnv) || pathEnv[i] == ':' {
			result = append(result, pathEnv[start:i])
			start = i + 1
		}
	}
	return result
}

func main() {
	if runtime.GOOS == "darwin" {
		runtime.LockOSThread()
	}

	logDir, logFile, closeLog := setupLogging()
	defer func() {
		if err := closeLog(); err != nil {
			// Best-effort; at this point logging may already be torn down.
			fmt.Fprintf(os.Stderr, "ccapp: failed to close log file: %v\n", err)
		}
	}()

	exe, _ := os.Executable()
	wd, _ := os.Getwd()
	slog.Info("ccapp starting",
		"exe", exe,
		"cwd", wd,
		"goos", runtime.GOOS,
		"goarch", runtime.GOARCH,
		"pid", os.Getpid(),
		"log_dir", logDir,
		"log_file", logFile,
	)

	app := Application{}
	app.logDir = logDir
	app.logFile = logFile

	// Check for URL argument (passed by OS when handling crumblecracker:// URLs)
	if len(os.Args) > 1 && strings.HasPrefix(os.Args[1], "crumblecracker://") {
		rawURL := os.Args[1]
		if len(rawURL) > 1024 {
			slog.Warn("URL from command line exceeds maximum length, ignoring")
		} else {
			app.pendingURL = rawURL
			// Don't log full URL as it may contain sensitive data
			slog.Info("pending URL from command line received")
		}
	}

	if err := app.Run(); err != nil {
		slog.Error("ccapp exited with error", "error", err)
		fmt.Fprintf(os.Stderr, "ccapp: %v\n", err)
		os.Exit(1)
	}
}
