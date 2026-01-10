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
	"github.com/tinyrange/cc/internal/starui"
	termwin "github.com/tinyrange/cc/internal/term"
	"github.com/tinyrange/cc/internal/vfs"
)

// appMode tracks what the app is currently displaying.
type appMode int

const (
	modeLauncher appMode = iota
	modeLoading
	modeError
	modeTerminal
	modeCustomVM
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
	network  bool
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
	launcherScreen *LauncherScreen
	loadingScreen  *LoadingScreen
	errorScreen    *ErrorScreen
	terminalScreen *TerminalScreen
	customVMScreen *CustomVMScreen

	// Starlark UI (optional override)
	starEngine         *starui.Engine
	starLauncherScreen *starui.StarlarkScreen
	starLoadingScreen  *starui.StarlarkScreen
	starErrorScreen    *starui.StarlarkScreen
	starTerminalScreen *starui.StarlarkScreen

	// Legacy UI state (for terminal screen which uses termview directly)
	scrollX       float32
	selectedIndex int // -1 means list view

	prevLeftDown  bool
	draggingThumb bool
	thumbDragDX   float32

	// Boot loading state
	bootCh      chan bootResult
	bootStarted time.Time
	bootName    string

	// Error state (full-screen)
	errMsg string

	// Discovered bundles
	bundlesDir string
	bundles    []discoveredBundle

	// Current mode
	mode appMode

	// Running VM (when in terminal mode)
	running *runningVM

	// Recent VMs storage
	recentVMs *RecentVMsStore
}

const terminalTopBarH = float32(32)

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
		if index < len(recent) {
			vm := recent[index]
			app.startCustomVM(vm.SourceType, vm.SourcePath, vm.NetworkEnabled)
		}
	}
}

// getBundlesDir returns the path to the bundles directory next to the app.
func getBundlesDir() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return ""
	}
	dir := filepath.Dir(exe)

	// On macOS, if we're inside a .app bundle, go up to the bundle's parent.
	if runtime.GOOS == "darwin" {
		// e.g. /path/to/Foo.app/Contents/MacOS/ccapp → /path/to/bundles
		for range 3 {
			parent := filepath.Dir(dir)
			if filepath.Ext(parent) == ".app" || filepath.Base(parent) == "Contents" || filepath.Base(parent) == "MacOS" {
				dir = filepath.Dir(parent)
			} else {
				break
			}
		}

		// if we're still in a .app bundle, go up to the parent
		if filepath.Ext(dir) == ".app" {
			dir = filepath.Dir(dir)
		}
	}

	return filepath.Join(dir, "bundles")
}

// loadStarlarkUI attempts to load custom UI from app.star in the bundles directory.
func (app *Application) loadStarlarkUI() error {
	appStarPath := starui.FindAppStar(app.bundlesDir)
	if appStarPath == "" {
		return fmt.Errorf("app.star not found in %s", app.bundlesDir)
	}

	engine := starui.NewEngine()
	if err := engine.LoadScript(appStarPath); err != nil {
		return fmt.Errorf("failed to load app.star: %w", err)
	}

	app.starEngine = engine
	return nil
}

// updateStarlarkContext updates the Starlark context with current app state.
func (app *Application) updateStarlarkContext() {
	if app.starEngine == nil {
		return
	}

	bundles := make([]starui.BundleInfo, len(app.bundles))
	for i, b := range app.bundles {
		name := b.Meta.Name
		if name == "" || name == "{{name}}" {
			name = filepath.Base(b.Dir)
		}
		desc := b.Meta.Description
		if desc == "" || desc == "{{description}}" {
			desc = "VM Bundle"
		}
		bundles[i] = starui.BundleInfo{
			Index:       i,
			Name:        name,
			Description: desc,
			Dir:         b.Dir,
		}
	}

	ctx := &starui.AppContext{
		AppTitle:   "CrumbleCracker",
		BundlesDir: app.bundlesDir,
		Bundles:    bundles,
		Logo:       app.logo,
		Text:       app.text,
		ErrorMessage: app.errMsg,
		BootName:     app.bootName,
		OnOpenLogs: func() {
			app.openLogs()
		},
		OnSelectBundle: func(index int) {
			app.selectedIndex = index
			app.startBootBundle(index)
		},
		OnStopVM: func() {
			app.stopVM()
		},
		OnBack: func() {
			app.errMsg = ""
			app.selectedIndex = -1
			app.mode = modeLauncher
		},
	}

	app.starEngine.SetContext(ctx)
}

// checkStarlarkReload checks if the Starlark script has been modified and reloads if needed.
func (app *Application) checkStarlarkReload() {
	if app.starEngine == nil {
		return
	}

	if app.starEngine.CheckReload() {
		// Script was reloaded, clear all cached screens so they get rebuilt
		app.starLauncherScreen = nil
		app.starLoadingScreen = nil
		app.starErrorScreen = nil
		app.starTerminalScreen = nil
	}
}

// renderStarlarkLauncher renders the launcher screen using Starlark.
func (app *Application) renderStarlarkLauncher(f graphics.Frame) error {
	app.checkStarlarkReload()
	app.updateStarlarkContext()

	// Create or rebuild the screen
	if app.starLauncherScreen == nil {
		screen, err := starui.NewStarlarkScreen(app.starEngine, "launcher", app.text)
		if err != nil {
			slog.Warn("failed to render Starlark launcher screen, falling back to built-in", "error", err)
			return app.launcherScreen.Render(f)
		}
		screen.SetStartTime(app.start)
		app.starLauncherScreen = screen
	}

	return app.starLauncherScreen.Render(f, app.window.PlatformWindow())
}

// renderStarlarkLoading renders the loading screen using Starlark.
func (app *Application) renderStarlarkLoading(f graphics.Frame) error {
	app.checkStarlarkReload()
	app.updateStarlarkContext()

	// Create or rebuild the screen
	if app.starLoadingScreen == nil {
		screen, err := starui.NewStarlarkScreen(app.starEngine, "loading", app.text)
		if err != nil {
			slog.Warn("failed to render Starlark loading screen, falling back to built-in", "error", err)
			if app.loadingScreen == nil {
				app.loadingScreen = NewLoadingScreen(app)
			}
			return app.loadingScreen.Render(f)
		}
		screen.SetStartTime(app.bootStarted)
		app.starLoadingScreen = screen
	}

	return app.starLoadingScreen.Render(f, app.window.PlatformWindow())
}

// renderStarlarkError renders the error screen using Starlark.
func (app *Application) renderStarlarkError(f graphics.Frame) error {
	app.checkStarlarkReload()
	app.updateStarlarkContext()

	// Always rebuild error screen since error message may have changed
	screen, err := starui.NewStarlarkScreen(app.starEngine, "error", app.text)
	if err != nil {
		slog.Warn("failed to render Starlark error screen, falling back to built-in", "error", err)
		if app.errorScreen == nil {
			app.errorScreen = NewErrorScreen(app)
		}
		return app.errorScreen.Render(f)
	}
	screen.SetStartTime(app.start)
	app.starErrorScreen = screen

	return app.starErrorScreen.Render(f, app.window.PlatformWindow())
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
	app.window.SetClearColor(color.RGBA{R: 30, G: 30, B: 32, A: 255}) // Darker background

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

	// Set up dock menu (macOS only)
	app.updateDockMenu()

	// Try to load app.star for custom UI
	if err := app.loadStarlarkUI(); err != nil {
		slog.Info("using built-in UI", "reason", err)
	} else {
		slog.Info("loaded custom Starlark UI from app.star")
	}

	// Initialize built-in UI screens (used as fallback or if no Starlark UI)
	app.launcherScreen = NewLauncherScreen(app)
	app.terminalScreen = NewTerminalScreen(app)

	return app.window.Loop(func(f graphics.Frame) error {
		switch app.mode {
		case modeLauncher:
			return app.renderLauncher(f)
		case modeLoading:
			return app.renderLoading(f)
		case modeError:
			return app.renderError(f)
		case modeTerminal:
			return app.renderTerminal(f)
		case modeCustomVM:
			return app.renderCustomVM(f)
		default:
			return nil
		}
	})
}

func (app *Application) showError(err error) {
	if err == nil {
		err = fmt.Errorf("unknown error")
	}
	app.errMsg = err.Error()
	app.mode = modeError
}

func (app *Application) renderLauncher(f graphics.Frame) error {
	// Use Starlark UI if available
	if app.starEngine != nil && app.starEngine.HasScreen("launcher") {
		return app.renderStarlarkLauncher(f)
	}
	return app.launcherScreen.Render(f)
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

	// Use Starlark UI if available
	if app.starEngine != nil && app.starEngine.HasScreen("loading") {
		return app.renderStarlarkLoading(f)
	}

	// Create or use cached loading screen
	if app.loadingScreen == nil {
		app.loadingScreen = NewLoadingScreen(app)
	}
	return app.loadingScreen.Render(f)
}

func (app *Application) renderError(f graphics.Frame) error {
	// Use Starlark UI if available
	if app.starEngine != nil && app.starEngine.HasScreen("error") {
		return app.renderStarlarkError(f)
	}

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

	// Render launcher as background
	app.launcherScreen.Render(f)

	// Render the custom VM dialog on top
	return app.customVMScreen.Render(f)
}

func (app *Application) renderTerminal(f graphics.Frame) error {
	if app.running == nil || app.running.termView == nil {
		app.mode = modeLauncher
		return nil
	}

	w, h := f.WindowSize()
	app.text.SetViewport(int32(w), int32(h))
	winW := float32(w)

	// Top bar with Exit button.
	f.RenderQuad(0, 0, winW, terminalTopBarH, nil, color.RGBA{R: 22, G: 22, B: 22, A: 255})

	mx, my := f.CursorPos()
	leftDown := f.GetButtonState(window.ButtonLeft).IsDown()
	justPressed := leftDown && !app.prevLeftDown
	app.prevLeftDown = leftDown

	backRect := rect{x: 20, y: 6, w: 70, h: terminalTopBarH - 12}
	backHover := backRect.contains(mx, my)
	backColor := color.RGBA{R: 40, G: 40, B: 40, A: 255}
	if backHover {
		backColor = color.RGBA{R: 56, G: 56, B: 56, A: 255}
	}
	if backHover && leftDown {
		backColor = color.RGBA{R: 72, G: 72, B: 72, A: 255}
	}
	f.RenderQuad(backRect.x, backRect.y, backRect.w, backRect.h, nil, backColor)
	app.text.RenderText("Exit", backRect.x+14, 22, 14, graphics.ColorWhite)

	if justPressed && backRect.contains(mx, my) {
		slog.Info("exit requested; stopping VM")
		app.stopVM()
		return nil
	}

	// Logs button (top-right).
	logRect := rect{x: winW - 150, y: 6, w: 120, h: terminalTopBarH - 12}
	logHover := logRect.contains(mx, my)
	logColor := color.RGBA{R: 40, G: 40, B: 40, A: 255}
	if logHover {
		logColor = color.RGBA{R: 56, G: 56, B: 56, A: 255}
	}
	if logHover && leftDown {
		logColor = color.RGBA{R: 72, G: 72, B: 72, A: 255}
	}
	f.RenderQuad(logRect.x, logRect.y, logRect.w, logRect.h, nil, logColor)
	app.text.RenderText("Debug Logs", logRect.x+26, 22, 14, graphics.ColorWhite)
	if justPressed && logHover {
		slog.Info("open logs requested", "log_dir", app.logDir)
		if err := openDirectory(app.logDir); err != nil {
			slog.Error("failed to open logs directory", "log_dir", app.logDir, "error", err)
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
			Name:           name,
			SourceType:     VMSourceBundle,
			SourcePath:     b.Dir,
			NetworkEnabled: b.Meta.Boot.Network,
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

	app.bootStarted = time.Now()
	app.bootName = name
	app.mode = modeLoading

	ch := make(chan bootResult, 1)
	app.bootCh = ch

	// Background prep: do anything slow/off-GPU here (disk IO, kernel fetch, etc).
	go func(b discoveredBundle, arch hv.CpuArchitecture, out chan<- bootResult) {
		prep, err := prepareBootBundle(b, arch)
		out <- bootResult{prep: prep, err: err}
	}(b, hvArch, ch)
}

func prepareBootBundle(b discoveredBundle, hvArch hv.CpuArchitecture) (_ *bootPrep, retErr error) {
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
	prep.workDir = workDir

	// Create VirtioFS backend
	fsBackend := vfs.NewVirtioFsBackendWithAbstract()
	if err := fsBackend.SetAbstractRoot(containerFS); err != nil {
		return nil, fmt.Errorf("set container filesystem as root: %w", err)
	}
	prep.fsBackend = fsBackend

	// Create hypervisor
	h, err := factory.OpenWithArchitecture(hvArch)
	if err != nil {
		return nil, fmt.Errorf("create hypervisor: %w", err)
	}
	prep.hypervisor = h

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
	prep.network = b.Meta.Boot.Network
	prep.dmesg = b.Meta.Boot.Dmesg
	prep.exec = b.Meta.Boot.Exec
	slog.Info("vm config (prep)", "cpus", prep.cpus, "memory_mb", prep.memoryMB, "network", prep.network, "dmesg", prep.dmesg, "exec", prep.exec)

	var netBackend *netstack.NetStack
	if prep.network {
		slog.Info("network enabled; starting netstack DNS server")
		netBackend = netstack.New(slog.Default())
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
	}

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
	// Reserve space for CCApp's top bar so terminal output doesn't overlap it.
	termView.SetInsets(0, terminalTopBarH, 0, 0)

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

	if prep.network {
		if prep.netBackend == nil || prep.virtioNet == nil {
			termView.Close()
			return fmt.Errorf("network enabled but netstack was not prepared")
		}
		mac := net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
		opts = append(opts, initx.WithDeviceTemplate(virtio.NetTemplate{
			Backend: prep.virtioNet,
			MAC:     mac,
			Arch:    prep.hvArch,
		}))
	}

	vm, err := initx.NewVirtualMachine(prep.hypervisor, prep.cpus, prep.memoryMB, prep.kernelLoader, opts...)
	if err != nil {
		termView.Close()
		return fmt.Errorf("create VM: %w", err)
	}

	// Build init program
	prog, err := initx.BuildContainerInitProgram(initx.ContainerInitConfig{
		Arch:          prep.hvArch,
		Cmd:           prep.execCmd,
		Env:           prep.env,
		WorkDir:       prep.workDir,
		EnableNetwork: prep.network,
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
	slog.Info("VM stopped; returned to launcher")
}

// startCustomVM starts a VM from a custom source (tarball, image name, or bundle directory)
func (app *Application) startCustomVM(sourceType VMSourceType, sourcePath string, network bool) {
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

	app.bootStarted = time.Now()
	app.bootName = displayName
	app.mode = modeLoading

	ch := make(chan bootResult, 1)
	app.bootCh = ch

	go func(srcType VMSourceType, srcPath string, net bool, arch hv.CpuArchitecture, out chan<- bootResult) {
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
			// Override network setting
			meta.Boot.Network = net
			prep, err = prepareBootBundle(discoveredBundle{Dir: srcPath, Meta: meta}, arch)

		case VMSourceTarball:
			prep, err = prepareFromTarball(srcPath, net, arch)

		case VMSourceImageName:
			prep, err = prepareFromImageName(srcPath, net, arch)

		default:
			err = fmt.Errorf("unknown source type: %s", srcType)
		}

		out <- bootResult{prep: prep, err: err}
	}(sourceType, sourcePath, network, hvArch, ch)
}

// prepareFromTarball loads a VM from an OCI tarball
func prepareFromTarball(tarPath string, network bool, hvArch hv.CpuArchitecture) (*bootPrep, error) {
	slog.Info("loading VM from tarball", "path", tarPath, "arch", hvArch)

	client, err := oci.NewClient("")
	if err != nil {
		return nil, fmt.Errorf("create OCI client: %w", err)
	}

	img, err := client.LoadFromTar(tarPath, hvArch)
	if err != nil {
		return nil, fmt.Errorf("load tarball: %w", err)
	}

	return prepareFromImage(img, network, hvArch)
}

// prepareFromImageName pulls a container image and prepares it for boot
func prepareFromImageName(imageName string, network bool, hvArch hv.CpuArchitecture) (*bootPrep, error) {
	slog.Info("pulling container image", "image", imageName, "arch", hvArch)

	client, err := oci.NewClient("")
	if err != nil {
		return nil, fmt.Errorf("create OCI client: %w", err)
	}

	img, err := client.PullForArch(imageName, hvArch)
	if err != nil {
		return nil, fmt.Errorf("pull image: %w", err)
	}

	return prepareFromImage(img, network, hvArch)
}

// prepareFromImage prepares a VM from an already-loaded OCI image
func prepareFromImage(img *oci.Image, network bool, hvArch hv.CpuArchitecture) (_ *bootPrep, retErr error) {
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
	prep.workDir = workDir

	// Create VirtioFS backend
	fsBackend := vfs.NewVirtioFsBackendWithAbstract()
	if err := fsBackend.SetAbstractRoot(containerFS); err != nil {
		return nil, fmt.Errorf("set container filesystem as root: %w", err)
	}
	prep.fsBackend = fsBackend

	// Create hypervisor
	h, err := factory.OpenWithArchitecture(hvArch)
	if err != nil {
		return nil, fmt.Errorf("create hypervisor: %w", err)
	}
	prep.hypervisor = h

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
	prep.network = network
	prep.dmesg = false
	prep.exec = true
	slog.Info("vm config", "cpus", prep.cpus, "memory_mb", prep.memoryMB, "network", prep.network)

	if prep.network {
		slog.Info("network enabled; starting netstack DNS server")
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
	}

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

	if err := app.Run(); err != nil {
		slog.Error("ccapp exited with error", "error", err)
		fmt.Fprintf(os.Stderr, "ccapp: %v\n", err)
		os.Exit(1)
	}
}
