package main

import (
	"context"
	"fmt"
	"image/color"
	"io"
	"log/slog"
	"net"
	"os"
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
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
	linux "github.com/tinyrange/cc/internal/linux/defs/amd64"
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
	cancel      context.CancelFunc
	done        chan error
	termView    *termwin.View
	containerFS *oci.ContainerFS
	netBackend  *netstack.NetStack
}

type Application struct {
	window graphics.Window
	text   *text.Renderer
	logo   *graphics.SVG

	start time.Time

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

	app.window, err = graphics.New("CrumbleCracker", 1024, 768)
	if err != nil {
		return fmt.Errorf("failed to create window: %w", err)
	}

	app.text, err = text.Load(app.window)
	if err != nil {
		return fmt.Errorf("failed to create text renderer: %w", err)
	}

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
	topBarH := float32(32)
	f.RenderQuad(0, 0, winW, topBarH, nil, color.RGBA{R: 22, G: 22, B: 22, A: 255})

	mx, my := f.CursorPos()
	leftDown := f.GetButtonState(window.ButtonLeft).IsDown()
	justPressed := leftDown && !app.prevLeftDown
	app.prevLeftDown = leftDown

	backRect := rect{x: 20, y: 6, w: 70, h: topBarH - 12}
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
		app.stopVM()
		return nil
	}

	// Check if VM has exited.
	select {
	case err := <-app.running.done:
		if err != nil && err != io.EOF {
			slog.Error("VM exited with error", "error", err)
		}
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

	// Determine architecture
	hvArch, err := parseArchitecture(runtime.GOARCH)
	if err != nil {
		return err
	}

	// Load image from bundle
	imageDir := filepath.Join(b.Dir, b.Meta.Boot.ImageDir)
	if b.Meta.Boot.ImageDir == "" {
		imageDir = filepath.Join(b.Dir, "image")
	}

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

	// Create terminal view
	termView, err := termwin.NewView(app.window)
	if err != nil {
		h.Close()
		containerFS.Close()
		return fmt.Errorf("create terminal view: %w", err)
	}

	// VM options
	cpus := b.Meta.Boot.CPUs
	if cpus == 0 {
		cpus = 1
	}
	memoryMB := b.Meta.Boot.MemoryMB
	if memoryMB == 0 {
		memoryMB = 1024
	}

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
	prog := buildContainerInit(hvArch, img, execCmd, b.Meta.Boot.Network, b.Meta.Boot.Exec)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	// Boot VM in background
	go func() {
		// First boot the VM
		bootCtx, bootCancel := context.WithTimeout(ctx, 10*time.Second)
		defer bootCancel()

		if err := vm.Run(bootCtx, &ir.Program{
			Entrypoint: "main",
			Methods: map[string]ir.Method{
				"main": {ir.Return(ir.Int64(0))},
			},
		}); err != nil {
			done <- err
			return
		}

		vm.StartStdinForwarding()

		// Run the actual program
		done <- vm.Run(ctx, prog)
	}()

	app.running = &runningVM{
		vm:          vm,
		cancel:      cancel,
		done:        done,
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

	app.running.cancel()

	// Wait briefly for VM to exit
	select {
	case <-app.running.done:
	case <-time.After(2 * time.Second):
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

func ipToUint32(ip net.IP) uint32 {
	ip = ip.To4()
	if ip == nil {
		return 0
	}
	return uint32(ip[0])<<24 | uint32(ip[1])<<16 | uint32(ip[2])<<8 | uint32(ip[3])
}

func buildContainerInit(arch hv.CpuArchitecture, img *oci.Image, cmd []string, enableNetwork bool, exec bool) *ir.Program {
	errLabel := ir.Label("__cc_error")
	errVar := ir.Var("__cc_errno")
	pivotResult := ir.Var("__cc_pivot_result")

	workDir := containerWorkDir(img)

	main := ir.Method{
		initx.LogKmsg("cc: running container init program\n"),

		// Create mount points
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt", ir.Int64(0o755)),
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/proc", ir.Int64(0o755)),
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/sys", ir.Int64(0o755)),

		// Mount virtiofs
		ir.Assign(errVar, ir.Syscall(
			defs.SYS_MOUNT,
			"rootfs",
			"/mnt",
			"virtiofs",
			ir.Int64(0),
			"",
		)),
		ir.If(ir.IsNegative(errVar), ir.Block{
			ir.Printf("cc: failed to mount virtiofs: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
			ir.Goto(errLabel),
		}),

		// Create necessary directories in container
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt/proc", ir.Int64(0o755)),
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt/sys", ir.Int64(0o755)),
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt/dev", ir.Int64(0o755)),
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt/tmp", ir.Int64(0o1777)),

		// Mount proc
		ir.Syscall(defs.SYS_MOUNT, "proc", "/mnt/proc", "proc", ir.Int64(0), ""),

		// Mount sysfs
		ir.Syscall(defs.SYS_MOUNT, "sysfs", "/mnt/sys", "sysfs", ir.Int64(0), ""),

		// Mount devtmpfs
		ir.Syscall(defs.SYS_MOUNT, "devtmpfs", "/mnt/dev", "devtmpfs", ir.Int64(0), ""),

		// Mount /dev/shm
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/mnt/dev/shm", ir.Int64(0o1777)),
		ir.Syscall(defs.SYS_MOUNT, "tmpfs", "/mnt/dev/shm", "tmpfs", ir.Int64(0), "mode=1777"),

		initx.LogKmsg("cc: mounted filesystems\n"),

		// Change root to container using pivot_root
		ir.Assign(errVar, ir.Syscall(defs.SYS_CHDIR, "/mnt")),
		ir.If(ir.IsNegative(errVar), ir.Block{
			ir.Printf("cc: failed to chdir to /mnt: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
			ir.Goto(errLabel),
		}),

		// pivot_root
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "oldroot", ir.Int64(0o755)),
		ir.Assign(pivotResult, ir.Syscall(defs.SYS_PIVOT_ROOT, ".", "oldroot")),
		ir.Assign(errVar, pivotResult),
		ir.If(ir.IsNegative(pivotResult), ir.Block{
			// Fall back to chroot if pivot_root fails
			ir.Assign(errVar, ir.Syscall(defs.SYS_CHROOT, ".")),
			ir.If(ir.IsNegative(errVar), ir.Block{
				ir.Printf("cc: failed to chroot: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
				ir.Goto(errLabel),
			}),
		}),
		ir.If(ir.IsGreaterOrEqual(pivotResult, ir.Int64(0)), ir.Block{
			ir.Assign(errVar, ir.Syscall(defs.SYS_CHDIR, "/")),
			ir.If(ir.IsNegative(errVar), ir.Block{
				ir.Printf("cc: failed to chdir to new root: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
				ir.Goto(errLabel),
			}),
			ir.Assign(errVar, ir.Syscall(defs.SYS_UMOUNT2, "/oldroot", ir.Int64(linux.MNT_DETACH))),
			ir.If(ir.IsNegative(errVar), ir.Block{
				ir.Printf("cc: failed to unmount oldroot: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
				ir.Goto(errLabel),
			}),
		}),

		initx.LogKmsg("cc: changed root to container\n"),

		// Always cleanup oldroot
		ir.Assign(errVar, ir.Syscall(defs.SYS_UNLINKAT, ir.Int64(linux.AT_FDCWD), "/oldroot", ir.Int64(linux.AT_REMOVEDIR))),
		ir.If(ir.IsNegative(errVar), ir.Block{
			ir.Printf("cc: failed to remove oldroot: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
			ir.Goto(errLabel),
		}),

		// Change to working directory
		ir.Syscall(defs.SYS_CHDIR, workDir),

		// mkdir /dev/pts
		ir.Syscall(defs.SYS_MKDIRAT, ir.Int64(linux.AT_FDCWD), "/dev/pts", ir.Int64(0o755)),

		// Mount devpts
		ir.Syscall(defs.SYS_MOUNT, "devpts", "/dev/pts", "devpts", ir.Int64(0), ""),

		initx.LogKmsg("cc: mounted devpts\n"),

		// Set hostname
		initx.SetHostname("tinyrange", errLabel, errVar),

		initx.LogKmsg("cc: set hostname to container name\n"),
	}

	// Configure network interface if networking is enabled
	if enableNetwork {
		ip := ipToUint32(net.ParseIP("10.42.0.2"))
		gateway := ipToUint32(net.ParseIP("10.42.0.1"))
		mask := ipToUint32(net.ParseIP("255.255.255.0"))
		main = append(main,
			initx.ConfigureInterface("eth0", ip, mask, errLabel, errVar),
			initx.AddDefaultRoute("eth0", gateway, errLabel, errVar),
			initx.SetResolvConf("10.42.0.1", errLabel, errVar),
			initx.LogKmsg("cc: configured network interface\n"),
		)
	}

	if exec {
		main = append(main, ir.Block{
			initx.LogKmsg(fmt.Sprintf("cc: executing command %s\n", cmd[0])),
			initx.Exec(cmd[0], cmd[1:], img.Config.Env, errLabel, errVar),
		})
	} else {
		main = append(main,
			initx.ForkExecWait(cmd[0], cmd[1:], img.Config.Env, errLabel, errVar),
		)
	}

	// Return child exit code to host
	main = append(main,
		ir.Return(errVar),

		// Error handler
		ir.DeclareLabel(errLabel, ir.Block{
			ir.Printf("cc: fatal error during boot: errno=0x%x\n", errVar),
			func() ir.Fragment {
				switch arch {
				case hv.ArchitectureX86_64:
					return ir.Syscall(defs.SYS_REBOOT,
						linux.LINUX_REBOOT_MAGIC1,
						linux.LINUX_REBOOT_MAGIC2,
						linux.LINUX_REBOOT_CMD_RESTART,
						ir.Int64(0),
					)
				case hv.ArchitectureARM64:
					return ir.Syscall(defs.SYS_REBOOT,
						linux.LINUX_REBOOT_MAGIC1,
						linux.LINUX_REBOOT_MAGIC2,
						linux.LINUX_REBOOT_CMD_POWER_OFF,
						ir.Int64(0),
					)
				default:
					panic(fmt.Sprintf("unsupported architecture for reboot: %s", arch))
				}
			}(),
		}),
	)

	return &ir.Program{
		Methods:    map[string]ir.Method{"main": main},
		Entrypoint: "main",
	}
}

func main() {
	if runtime.GOOS == "darwin" {
		runtime.LockOSThread()
	}

	app := Application{}

	if err := app.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "ccapp: %v\n", err)
		os.Exit(1)
	}
}
