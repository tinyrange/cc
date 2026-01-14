// Command miniplayer displays markdown files with an embedded VM terminal.
//
// Usage:
//
//	go run ./internal/cmd/miniplayer
//	go run ./internal/cmd/miniplayer -file /path/to/file.md
//	go run ./internal/cmd/miniplayer -bundle /path/to/bundle
//	go run ./internal/cmd/miniplayer -no-vm
//	go run ./internal/cmd/miniplayer -screenshot -o output.png
package main

import (
	"context"
	"flag"
	"fmt"
	"image/color"
	"image/png"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/tinyrange/cc/internal/bundle"
	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/gowin/graphics"
	"github.com/tinyrange/cc/internal/gowin/text"
	"github.com/tinyrange/cc/internal/gowin/ui"
	"github.com/tinyrange/cc/internal/gowin/ui/html"
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

// vmState holds all the resources for a running VM.
type vmState struct {
	hypervisor  hv.Hypervisor
	vm          *initx.VirtualMachine
	session     *initx.Session
	termView    *termwin.View
	containerFS *oci.ContainerFS
	netBackend  *netstack.NetStack
}

// close cleans up all VM resources.
func (v *vmState) close() {
	if v == nil {
		return
	}

	slog.Info("stopping VM")

	// Stop session with timeout
	if v.session != nil {
		if err := v.session.Stop(2 * time.Second); err != nil {
			slog.Warn("session stop returned error", "error", err)
		}
	}

	// Close resources in reverse order
	if v.termView != nil {
		v.termView.Close()
	}
	if v.vm != nil {
		v.vm.Close()
	}
	if v.netBackend != nil {
		v.netBackend.Close()
	}
	if v.containerFS != nil {
		v.containerFS.Close()
	}
	if v.hypervisor != nil {
		_ = v.hypervisor.Close()
	}
}

func main() {
	mdPath := flag.String("file", "lesson/cheatsheet.md", "markdown file to display")
	bundleDir := flag.String("bundle", "lesson", "bundle directory for VM")
	width := flag.Int("w", 1400, "window width")
	height := flag.Int("h", 800, "window height")
	screenshot := flag.Bool("screenshot", false, "take a screenshot and exit")
	output := flag.String("o", "screenshot.png", "output file for screenshot")
	noVM := flag.Bool("no-vm", false, "disable VM, show markdown only")
	exportResources := flag.Bool("export-resources", false, "export kernel resources for offline use and exit")
	flag.Parse()

	// Handle export-resources flag
	if *exportResources {
		if err := exportKernelResources(); err != nil {
			slog.Error("export resources failed", "error", err)
			os.Exit(1)
		}
		slog.Info("resources exported successfully")
		os.Exit(0)
	}

	if err := run(*mdPath, *bundleDir, *width, *height, *screenshot, *output, *noVM); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

// exportKernelResources exports the kernel files for offline distribution.
func exportKernelResources() error {
	exePath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("get executable path: %w", err)
	}
	resourcesDir := filepath.Join(filepath.Dir(exePath), "resources")

	// Export for current architecture
	arch := hvArchFromRuntime()
	slog.Info("exporting kernel resources", "arch", arch, "dest", resourcesDir)
	return kernel.ExportResources(arch, resourcesDir)
}

// hvArchFromRuntime returns the hypervisor architecture for the current runtime.
func hvArchFromRuntime() hv.CpuArchitecture {
	switch runtime.GOARCH {
	case "amd64":
		return hv.ArchitectureX86_64
	case "arm64":
		return hv.ArchitectureARM64
	default:
		return hv.ArchitectureInvalid
	}
}

func run(mdPath, bundleDir string, width, height int, takeScreenshot bool, outputPath string, noVM bool) error {
	// Read markdown file
	content, err := os.ReadFile(mdPath)
	if err != nil {
		return fmt.Errorf("failed to read file: %w", err)
	}

	// Create window
	win, err := graphics.New("Miniplayer", width, height)
	if err != nil {
		return fmt.Errorf("failed to create window: %w", err)
	}

	// Load text renderer
	textRenderer, err := text.Load(win)
	if err != nil {
		return fmt.Errorf("failed to create text renderer: %w", err)
	}

	// Calculate max width for markdown (half of window width)
	maxWidth := float32(width) / 2

	// Parse markdown to document with max width
	doc, err := html.ParseMarkdownWithMaxWidth(string(content), maxWidth)
	if err != nil {
		return fmt.Errorf("failed to parse markdown: %w", err)
	}

	// Render to widget tree
	ctx := &html.RenderContext{
		Window:       win,
		TextRenderer: textRenderer,
	}
	widget := doc.Render(ctx)

	// Wrap in ScrollView for scrolling through content
	scrollView := ui.NewScrollView(widget)

	// Create header label (will be rendered separately for full-width)
	headerLabel := ui.NewGradientLabel("CrumbleCracker").WithSize(32)
	headerRoot := ui.NewRoot(textRenderer)
	headerRoot.SetChild(ui.Row().
		WithPadding(ui.Symmetric(16, 12)).
		AddChild(headerLabel, ui.DefaultFlexParams()))

	// Create root for markdown content (below header)
	root := ui.NewRoot(textRenderer)
	root.SetChild(scrollView)

	// Setup window with light cream background
	bgColor := color.RGBA{R: 0xff, G: 0xfb, B: 0xf7, A: 255} // #FFFBF7 warm cream
	win.SetClear(true)
	win.SetClearColor(bgColor)

	// Boot VM if not disabled and not in screenshot mode
	var vm *vmState
	if !noVM && !takeScreenshot {
		var err error
		vm, err = bootVM(win, bundleDir)
		if err != nil {
			return fmt.Errorf("failed to boot VM: %w", err)
		}
		defer vm.close()
	}

	if takeScreenshot {
		// Screenshot mode: render 2 frames then capture and exit
		frameCount := 0
		var screenshotErr error

		err = win.Loop(func(f graphics.Frame) error {
			root.DrawOnly(f)
			frameCount++

			if frameCount == 2 {
				img, err := f.Screenshot()
				if err != nil {
					screenshotErr = fmt.Errorf("failed to take screenshot: %w", err)
					return screenshotErr
				}

				file, err := os.Create(outputPath)
				if err != nil {
					screenshotErr = fmt.Errorf("failed to create output file: %w", err)
					return screenshotErr
				}
				defer file.Close()

				if err := png.Encode(file, img); err != nil {
					screenshotErr = fmt.Errorf("failed to encode PNG: %w", err)
					return screenshotErr
				}

				fmt.Printf("Screenshot saved to %s\n", outputPath)
				return fmt.Errorf("done")
			}

			return nil
		})

		if err != nil && err.Error() == "done" {
			return screenshotErr
		}
		return err
	}

	// Interactive mode: run with input handling
	platformWin := win.PlatformWindow()
	// Header bar height (text size + padding)
	const headerHeight float32 = 56

	// Purple color for header background
	headerBgColor := color.RGBA{R: 88, G: 28, B: 135, A: 255}

	return win.Loop(func(f graphics.Frame) error {
		windowWidth, _ := f.WindowSize()
		splitX := windowWidth / 2

		// Update animated header gradient (3 second cycle)
		phase := float32(time.Now().UnixMilli()%3000) / 3000.0
		headerLabel.SetGradient(createScrollingWhiteGradient(phase))

		// Drain events once at the start of the frame
		rawEvents := platformWin.DrainInputEvents()

		// Route scroll events to the markdown UI (adjust Y for header offset)
		mx, my := f.CursorPos()
		for _, ev := range rawEvents {
			if ev.Type == window.InputEventScroll {
				root.DispatchEvent(&ui.ScrollEvent{
					X:      mx,
					Y:      my - headerHeight,
					DeltaX: ev.ScrollX,
					DeltaY: ev.ScrollY,
				})
			}
		}

		// Constrain markdown to left half, below header
		root.SetMaxSize(float32(splitX), 0)
		root.SetOffset(0, headerHeight)
		root.DrawOnly(f)

		// Render terminal on right half if VM is running (with top inset for header)
		if vm != nil {
			vm.termView.SetInsets(float32(splitX), headerHeight, 0, 0)
			// Pass events to terminal (it will use these instead of draining)
			vm.termView.SetPendingEvents(rawEvents)
			if err := vm.termView.Step(f, termwin.Hooks{
				OnResize: func(cols, rows int) {
					if vm.vm != nil {
						vm.vm.SetConsoleSize(cols, rows)
					}
				},
			}); err != nil {
				// Terminal closed or error
				slog.Info("terminal step returned error", "error", err)
				return err
			}

			// Check if VM exited
			select {
			case err := <-vm.session.Done:
				slog.Info("VM session ended", "error", err)
				vm.close()
				vm = nil
			default:
			}
		}

		// Draw header last so it clips over markdown/terminal content
		f.RenderQuad(0, 0, float32(windowWidth), headerHeight, nil, headerBgColor)
		headerRoot.SetMaxSize(float32(windowWidth), headerHeight)
		headerRoot.DrawOnly(f)

		return nil
	})
}

// bootVM boots a VM from the given bundle directory.
func bootVM(win graphics.Window, bundleDir string) (*vmState, error) {
	slog.Info("booting VM from bundle", "dir", bundleDir)

	// Load bundle metadata
	meta, err := bundle.LoadMetadata(bundleDir)
	if err != nil {
		return nil, fmt.Errorf("load bundle metadata: %w", err)
	}

	// Determine image directory
	imageDir := filepath.Join(bundleDir, meta.Boot.ImageDir)
	if meta.Boot.ImageDir == "" {
		imageDir = filepath.Join(bundleDir, "image")
	}

	// Determine architecture early so we can validate the image
	hvArch, err := parseArchitecture(runtime.GOARCH)
	if err != nil {
		return nil, fmt.Errorf("determine architecture: %w", err)
	}

	// Load OCI image with architecture validation
	img, err := oci.LoadFromDirForArch(imageDir, hvArch)
	if err != nil {
		return nil, fmt.Errorf("load image: %w", err)
	}

	// Determine command
	cmd := meta.Boot.Command
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

	// Resolve command path
	pathEnv := extractInitialPath(img.Config.Env)
	workDir := containerWorkDir(img)
	execCmd, err = resolveCommandPath(containerFS, execCmd, pathEnv, workDir)
	if err != nil {
		containerFS.Close()
		return nil, fmt.Errorf("resolve command: %w", err)
	}

	// Prepare environment
	env := img.Config.Env
	if len(meta.Boot.Env) > 0 {
		env = append(env, meta.Boot.Env...)
	}
	if !hasEnvVar(env, "TERM") {
		env = append(env, "TERM=xterm-256color")
	}

	// Create VirtioFS backend
	fsBackend := vfs.NewVirtioFsBackendWithAbstract()
	if err := fsBackend.SetAbstractRoot(containerFS); err != nil {
		containerFS.Close()
		return nil, fmt.Errorf("set container filesystem as root: %w", err)
	}

	// Open hypervisor
	h, err := factory.OpenWithArchitecture(hvArch)
	if err != nil {
		containerFS.Close()
		return nil, fmt.Errorf("create hypervisor: %w", err)
	}

	// Determine resources directory (next to executable) for offline mode
	var resourcesDir string
	if exePath, err := os.Executable(); err == nil {
		resourcesDir = filepath.Join(filepath.Dir(exePath), "resources")
	}

	// Load kernel (tries local resources first, then downloads)
	kernelLoader, err := kernel.LoadForArchitectureWithFallback(hvArch, resourcesDir)
	if err != nil {
		h.Close()
		containerFS.Close()
		return nil, fmt.Errorf("load kernel: %w", err)
	}

	// VM configuration
	cpus := meta.Boot.CPUs
	if cpus == 0 {
		cpus = 1
	}
	memoryMB := uint64(meta.Boot.MemoryMB)
	if memoryMB == 0 {
		memoryMB = 1024
	}

	// Create netstack for networking
	netBackend := netstack.New(slog.Default())
	if err := netBackend.StartDNSServer(); err != nil {
		h.Close()
		containerFS.Close()
		return nil, fmt.Errorf("start DNS server: %w", err)
	}
	netBackend.SetInternetAccessEnabled(true)

	mac := net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	virtioNet, err := virtio.NewNetstackBackend(netBackend, mac)
	if err != nil {
		netBackend.Close()
		h.Close()
		containerFS.Close()
		return nil, fmt.Errorf("create netstack backend: %w", err)
	}

	// Create terminal view
	termView, err := termwin.NewView(win)
	if err != nil {
		netBackend.Close()
		h.Close()
		containerFS.Close()
		return nil, fmt.Errorf("create terminal view: %w", err)
	}
	termView.SetColorScheme(termwin.LightColorScheme())

	// Drain any accumulated text input from the window
	_ = win.PlatformWindow().TextInput()

	// Create VM with devices
	opts := []initx.Option{
		initx.WithDeviceTemplate(virtio.FSTemplate{
			Tag:     "rootfs",
			Backend: fsBackend,
			Arch:    hvArch,
		}),
		initx.WithDeviceTemplate(virtio.NetTemplate{
			Backend: virtioNet,
			MAC:     mac,
			Arch:    hvArch,
		}),
		initx.WithStdin(termView),
		initx.WithConsoleOutput(termView),
		initx.WithDmesgLogging(false),
	}

	vm, err := initx.NewVirtualMachine(h, cpus, memoryMB, kernelLoader, opts...)
	if err != nil {
		termView.Close()
		netBackend.Close()
		h.Close()
		containerFS.Close()
		return nil, fmt.Errorf("create VM: %w", err)
	}

	// Build init program
	prog, err := initx.BuildContainerInitProgram(initx.ContainerInitConfig{
		Arch:          hvArch,
		Cmd:           execCmd,
		Env:           env,
		WorkDir:       workDir,
		EnableNetwork: true,
		Exec:          meta.Boot.Exec,
		UID:           img.Config.UID,
		GID:           img.Config.GID,
	})
	if err != nil {
		vm.Close()
		termView.Close()
		netBackend.Close()
		h.Close()
		containerFS.Close()
		return nil, fmt.Errorf("build init program: %w", err)
	}

	// Start session
	session := initx.StartSession(context.Background(), vm, prog, initx.SessionConfig{})
	slog.Info("VM session started")

	return &vmState{
		hypervisor:  h,
		vm:          vm,
		session:     session,
		termView:    termView,
		containerFS: containerFS,
		netBackend:  netBackend,
	}, nil
}

// Helper functions copied from ccapp

const defaultPathEnv = "/bin:/usr/bin"

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

	// If already absolute, return as-is
	if len(resolved[0]) > 0 && resolved[0][0] == '/' {
		return resolved, nil
	}
	// Also check for embedded slashes (relative path with directory)
	for _, c := range resolved[0] {
		if c == '/' {
			return resolved, nil
		}
	}

	// Search in PATH
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
	return strings.Split(pathEnv, ":")
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

// createScrollingWhiteGradient creates a white/grey gradient that scrolls based on the phase.
// Phase should be 0.0 to 1.0 and represents position in the animation cycle.
func createScrollingWhiteGradient(phase float32) []graphics.ColorStop {
	// White and light grey shades
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}     // Pure white
	lightGrey := color.RGBA{R: 220, G: 220, B: 225, A: 255} // Very light grey
	offWhite := color.RGBA{R: 245, G: 245, B: 250, A: 255}  // Off-white

	// Create scrolling effect by offsetting positions
	return []graphics.ColorStop{
		{Position: wrapPosition(0.0 + phase), Color: lightGrey},
		{Position: wrapPosition(0.33 + phase), Color: white},
		{Position: wrapPosition(0.66 + phase), Color: offWhite},
		{Position: wrapPosition(1.0 + phase), Color: lightGrey},
	}
}

// wrapPosition wraps a position value to the 0.0-1.0 range.
func wrapPosition(p float32) float32 {
	for p > 1.0 {
		p -= 1.0
	}
	for p < 0.0 {
		p += 1.0
	}
	return p
}
