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
	termwin "github.com/tinyrange/cc/internal/term"
	"github.com/tinyrange/cc/internal/vfs"
)

// appMode tracks what the app is currently displaying.
type appMode int

const (
	modeLauncher appMode = iota
	modeTerminal
)

// discoveredBundle holds metadata and path for a discovered bundle.
type discoveredBundle struct {
	Dir  string
	Meta bundle.Metadata
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

	start time.Time

	// Logging
	logDir  string
	logFile string

	// UI state
	scrollX       float32
	selectedIndex int // -1 means list view

	prevLeftDown  bool
	draggingThumb bool
	thumbDragDX   float32

	// Discovered bundles
	bundlesDir string
	bundles    []discoveredBundle

	// Current mode
	mode appMode

	// Running VM (when in terminal mode)
	running *runningVM
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

func clamp(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
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
	app.window.SetClearColor(color.RGBA{R: 10, G: 10, B: 10, A: 255})

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

	return app.window.Loop(func(f graphics.Frame) error {
		switch app.mode {
		case modeLauncher:
			return app.renderLauncher(f)
		case modeTerminal:
			return app.renderTerminal(f)
		default:
			return nil
		}
	})
}

func (app *Application) renderLauncher(f graphics.Frame) error {
	w, h := f.WindowSize()
	app.text.SetViewport(int32(w), int32(h))

	// Pull raw input events (wheel deltas live here).
	var wheelX, wheelY float32
	for _, ev := range app.window.PlatformWindow().DrainInputEvents() {
		if ev.Type == window.InputEventScroll {
			wheelX += ev.ScrollX
			wheelY += ev.ScrollY
		}
	}

	mx, my := f.CursorPos()
	leftDown := f.GetButtonState(window.ButtonLeft).IsDown()
	justPressed := leftDown && !app.prevLeftDown
	justReleased := !leftDown && app.prevLeftDown
	app.prevLeftDown = leftDown

	// Layout uses the actual window bounds directly.
	winW := float32(w)
	winH := float32(h)
	padding := float32(20)

	// Top bar.
	topBarH := float32(32)
	f.RenderQuad(0, 0, winW, topBarH, nil, color.RGBA{R: 22, G: 22, B: 22, A: 255})

	// Logs button (top-right).
	logRect := rect{x: winW - 150, y: 6, w: 120, h: topBarH - 12}
	logHover := logRect.contains(mx, my)
	logColor := color.RGBA{R: 40, G: 40, B: 40, A: 255}
	if logHover {
		logColor = color.RGBA{R: 56, G: 56, B: 56, A: 255}
	}
	if logHover && leftDown {
		logColor = color.RGBA{R: 72, G: 72, B: 72, A: 255}
	}
	f.RenderQuad(logRect.x, logRect.y, logRect.w, logRect.h, nil, logColor)
	app.text.RenderText("Debug Logs", logRect.x+26, 20, 14, graphics.ColorWhite)
	if justPressed && logHover {
		slog.Info("open logs requested", "log_dir", app.logDir)
		if err := openDirectory(app.logDir); err != nil {
			slog.Error("failed to open logs directory", "log_dir", app.logDir, "error", err)
		}
	}

	// Title below top bar.
	titleY := topBarH + 50
	app.text.RenderText("CrumbleCracker", padding, titleY, 48, graphics.ColorWhite)

	if len(app.bundles) == 0 {
		app.text.RenderText("No bundles found. Create bundles with: cc -build <outDir> <image>", padding, titleY+30, 20, graphics.ColorWhite)
		app.text.RenderText("Searched for bundles in: "+app.bundlesDir, padding, titleY+50, 20, graphics.ColorWhite)
	} else {
		app.text.RenderText("Please select an environment to boot", padding, titleY+30, 20, graphics.ColorWhite)
	}

	// Logo in bottom-right corner, overlapping content area.
	if app.logo != nil {
		logoSize := winH * 0.75
		if logoSize > winW*0.75 {
			logoSize = winW * 0.75
		}
		if logoSize < 280 {
			logoSize = 280
		}

		logoX := winW - logoSize + logoSize*0.35
		logoY := winH - logoSize + logoSize*0.35

		t := float32(time.Since(app.start).Seconds())
		app.logo.DrawGroupRotated(f, "inner-circle", logoX, logoY, logoSize, logoSize, t*0.4)
		app.logo.DrawGroupRotated(f, "morse-circle", logoX, logoY, logoSize, logoSize, -t*0.9)
		app.logo.DrawGroupRotated(f, "outer-circle", logoX, logoY, logoSize, logoSize, t*1.6)
	}

	if len(app.bundles) == 0 {
		return nil
	}

	// List view - cards below title.
	listX := padding
	listY := titleY + 120
	viewW := winW - padding*2
	cardW := float32(180)
	cardH := float32(180)
	gap := float32(24)
	viewport := rect{x: listX, y: listY, w: viewW, h: cardH + 80}

	// draw a rectangle overlaying the viewport
	f.RenderQuad(0, listY-20, winW, cardH+160, nil, color.RGBA{R: 10, G: 10, B: 10, A: 200})

	contentWidth := float32(len(app.bundles))*(cardW+gap) - gap
	maxScroll := float32(0)
	if contentWidth > viewport.w {
		maxScroll = contentWidth - viewport.w
	}

	// Wheel scroll when hovering the list area.
	if (wheelX != 0 || wheelY != 0) && viewport.contains(mx, my) {
		app.scrollX -= wheelY * 40
		app.scrollX -= wheelX * 40
	}
	app.scrollX = clamp(app.scrollX, 0, maxScroll)

	// Scrollbar (bottom).
	barH := float32(8)
	barY := viewport.y + viewport.h + 16
	bar := rect{x: viewport.x, y: barY, w: viewport.w, h: barH}
	f.RenderQuad(bar.x, bar.y, bar.w, bar.h, nil, color.RGBA{R: 48, G: 48, B: 48, A: 255})

	thumbW := bar.w
	if contentWidth > 0 {
		thumbW = bar.w * (bar.w / contentWidth)
	}
	if thumbW < 30 {
		thumbW = 30
	}
	if thumbW > bar.w {
		thumbW = bar.w
	}
	thumbX := bar.x
	if maxScroll > 0 {
		thumbX = bar.x + (bar.w-thumbW)*(app.scrollX/maxScroll)
	}
	thumb := rect{x: thumbX, y: bar.y, w: thumbW, h: bar.h}
	f.RenderQuad(thumb.x, thumb.y, thumb.w, thumb.h, nil, color.RGBA{R: 100, G: 100, B: 100, A: 255})

	if justPressed && thumb.contains(mx, my) {
		app.draggingThumb = true
		app.thumbDragDX = mx - thumb.x
	}
	if app.draggingThumb && leftDown {
		newThumbX := clamp(mx-app.thumbDragDX, bar.x, bar.x+bar.w-thumbW)
		if bar.w-thumbW > 0 {
			app.scrollX = (newThumbX - bar.x) / (bar.w - thumbW) * maxScroll
		} else {
			app.scrollX = 0
		}
	}
	if justReleased {
		app.draggingThumb = false
	}

	// Draw cards.
	for i, b := range app.bundles {
		x := viewport.x + float32(i)*(cardW+gap) - app.scrollX
		card := rect{x: x, y: viewport.y, w: cardW, h: cardH + 60}

		// Simple clipping by skipping offscreen cards.
		if card.x+card.w < viewport.x-50 || card.x > viewport.x+viewport.w+50 {
			continue
		}

		hovered := viewport.contains(mx, my) && card.contains(mx, my)

		// Card background + border (hover state).
		bgColor := color.RGBA{R: 0, G: 0, B: 0, A: 0}
		borderColor := color.RGBA{R: 80, G: 80, B: 80, A: 255}
		if hovered {
			bgColor = color.RGBA{R: 20, G: 20, B: 20, A: 220}
			borderColor = color.RGBA{R: 140, G: 140, B: 140, A: 255}
		}
		if hovered && leftDown {
			bgColor = color.RGBA{R: 30, G: 30, B: 30, A: 235}
			borderColor = color.RGBA{R: 180, G: 180, B: 180, A: 255}
		}
		if bgColor.A != 0 {
			f.RenderQuad(card.x, card.y, card.w, card.h, nil, bgColor)
		}

		// Border.
		f.RenderQuad(card.x, card.y, card.w, 1, nil, borderColor)         // top
		f.RenderQuad(card.x, card.y+cardH, card.w, 1, nil, borderColor)   // bottom of image area
		f.RenderQuad(card.x, card.y, 1, cardH, nil, borderColor)          // left
		f.RenderQuad(card.x+card.w-1, card.y, 1, cardH, nil, borderColor) // right

		// Title + description below card.
		name := b.Meta.Name
		if name == "" || name == "{{name}}" {
			name = filepath.Base(b.Dir)
		}
		desc := b.Meta.Description
		if desc == "" || desc == "{{description}}" {
			desc = "VM Bundle"
		}
		app.text.RenderText(name, card.x, card.y+cardH+24, 18, graphics.ColorWhite)
		app.text.RenderText(desc, card.x, card.y+cardH+44, 14, graphics.ColorWhite)

		if justPressed && viewport.contains(mx, my) && card.contains(mx, my) {
			app.selectedIndex = i
			if err := app.bootBundle(i); err != nil {
				slog.Error("failed to boot bundle", "error", err)
				app.selectedIndex = -1
			}
		}
	}

	return nil
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

func (app *Application) bootBundle(index int) error {
	if index < 0 || index >= len(app.bundles) {
		return fmt.Errorf("invalid bundle index")
	}

	b := app.bundles[index]
	slog.Info("boot bundle requested", "index", index, "bundle_dir", b.Dir, "bundle_name", b.Meta.Name)

	// Determine architecture
	hvArch, err := parseArchitecture(runtime.GOARCH)
	if err != nil {
		return err
	}
	slog.Info("determined architecture", "goarch", runtime.GOARCH, "arch", hvArch)

	// Load image from bundle
	imageDir := filepath.Join(b.Dir, b.Meta.Boot.ImageDir)
	if b.Meta.Boot.ImageDir == "" {
		imageDir = filepath.Join(b.Dir, "image")
	}
	slog.Info("loading image from bundle", "image_dir", imageDir)

	img, err := oci.LoadFromDir(imageDir)
	if err != nil {
		return fmt.Errorf("load image: %w", err)
	}

	// Determine command
	cmd := b.Meta.Boot.Command
	execCmd := img.Command(cmd)
	if len(execCmd) == 0 {
		return fmt.Errorf("no command specified and image has no entrypoint/cmd")
	}
	slog.Info("resolved container command", "cmd", execCmd)

	// Create container filesystem
	containerFS, err := oci.NewContainerFS(img)
	if err != nil {
		return fmt.Errorf("create container filesystem: %w", err)
	}

	// Resolve command path
	pathEnv := extractInitialPath(img.Config.Env)
	workDir := containerWorkDir(img)
	execCmd, err = resolveCommandPath(containerFS, execCmd, pathEnv, workDir)
	if err != nil {
		containerFS.Close()
		return fmt.Errorf("resolve command: %w", err)
	}
	slog.Info("resolved command path", "exec", execCmd, "work_dir", workDir)

	// Create VirtioFS backend
	fsBackend := vfs.NewVirtioFsBackendWithAbstract()
	if err := fsBackend.SetAbstractRoot(containerFS); err != nil {
		containerFS.Close()
		return fmt.Errorf("set container filesystem as root: %w", err)
	}

	// Create hypervisor
	h, err := factory.OpenWithArchitecture(hvArch)
	if err != nil {
		containerFS.Close()
		return fmt.Errorf("create hypervisor: %w", err)
	}

	// Load kernel
	kernelLoader, err := kernel.LoadForArchitecture(hvArch)
	if err != nil {
		h.Close()
		containerFS.Close()
		return fmt.Errorf("load kernel: %w", err)
	}
	slog.Info("kernel loader ready")

	// Create terminal view
	termView, err := termwin.NewView(app.window)
	if err != nil {
		h.Close()
		containerFS.Close()
		return fmt.Errorf("create terminal view: %w", err)
	}
	// Reserve space for CCApp's top bar so terminal output doesn't overlap it.
	termView.SetInsets(0, terminalTopBarH, 0, 0)

	// VM options
	cpus := b.Meta.Boot.CPUs
	if cpus == 0 {
		cpus = 1
	}
	memoryMB := b.Meta.Boot.MemoryMB
	if memoryMB == 0 {
		memoryMB = 1024
	}
	slog.Info("vm config", "cpus", cpus, "memory_mb", memoryMB, "network", b.Meta.Boot.Network, "dmesg", b.Meta.Boot.Dmesg, "exec", b.Meta.Boot.Exec)

	opts := []initx.Option{
		initx.WithDeviceTemplate(virtio.FSTemplate{
			Tag:     "rootfs",
			Backend: fsBackend,
			Arch:    hvArch,
		}),
		initx.WithStdin(termView),
		initx.WithConsoleOutput(termView),
		initx.WithDmesgLogging(b.Meta.Boot.Dmesg),
	}

	var netBackend *netstack.NetStack
	if b.Meta.Boot.Network {
		slog.Info("network enabled; starting netstack DNS server")
		netBackend = netstack.New(slog.Default())
		if err := netBackend.StartDNSServer(); err != nil {
			termView.Close()
			h.Close()
			containerFS.Close()
			return fmt.Errorf("start DNS server: %w", err)
		}

		mac := net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
		virtioNet, err := virtio.NewNetstackBackend(netBackend, mac)
		if err != nil {
			netBackend.Close()
			termView.Close()
			h.Close()
			containerFS.Close()
			return fmt.Errorf("create netstack backend: %w", err)
		}

		opts = append(opts, initx.WithDeviceTemplate(virtio.NetTemplate{
			Backend: virtioNet,
			MAC:     mac,
			Arch:    hvArch,
		}))
	}

	vm, err := initx.NewVirtualMachine(h, cpus, uint64(memoryMB), kernelLoader, opts...)
	if err != nil {
		if netBackend != nil {
			netBackend.Close()
		}
		termView.Close()
		h.Close()
		containerFS.Close()
		return fmt.Errorf("create VM: %w", err)
	}

	// Build init program
	prog, err := initx.BuildContainerInitProgram(initx.ContainerInitConfig{
		Arch:          hvArch,
		Cmd:           execCmd,
		Env:           img.Config.Env,
		WorkDir:       workDir,
		EnableNetwork: b.Meta.Boot.Network,
		Exec:          b.Meta.Boot.Exec,
	})
	if err != nil {
		vm.Close()
		if netBackend != nil {
			netBackend.Close()
		}
		termView.Close()
		h.Close()
		containerFS.Close()
		return err
	}

	session := initx.StartSession(context.Background(), vm, prog, initx.SessionConfig{})
	slog.Info("VM session started")

	app.running = &runningVM{
		vm:          vm,
		session:     session,
		termView:    termView,
		containerFS: containerFS,
		netBackend:  netBackend,
	}
	app.mode = modeTerminal

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
