package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strconv"
	"strings"
	"time"

	"github.com/tinyrange/cc/internal/bundle"
	"github.com/tinyrange/cc/internal/debug"
	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/gowin/window"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/factory"
	"github.com/tinyrange/cc/internal/initx"
	"github.com/tinyrange/cc/internal/linux/kernel"
	"github.com/tinyrange/cc/internal/netstack"
	"github.com/tinyrange/cc/internal/oci"
	termwin "github.com/tinyrange/cc/internal/term"
	"github.com/tinyrange/cc/internal/timeslice"
	"github.com/tinyrange/cc/internal/vfs"
	"golang.org/x/term"
)

func main() {
	// Cocoa requires UI objects (e.g. NSWindow) to be created on the process main
	// thread. The Go scheduler can migrate the main goroutine across OS threads,
	// so we pin it early on darwin to keep later window creation safe.
	if runtime.GOOS == "darwin" {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
	}

	if err := run(); err != nil {
		var exitErr *initx.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		fmt.Fprintf(os.Stderr, "cc: %v\n", err)
		os.Exit(1)
	}
}

type fixCrlf struct {
	w io.Writer
}

func (f *fixCrlf) Write(p []byte) (n int, err error) {
	return f.w.Write(bytes.ReplaceAll(p, []byte{'\n'}, []byte{'\r', '\n'}))
}

type intFlag struct {
	v   int
	set bool
}

func (f *intFlag) String() string { return strconv.Itoa(f.v) }

func (f *intFlag) Set(s string) error {
	v, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	f.v = v
	f.set = true
	return nil
}

type uint64Flag struct {
	v   uint64
	set bool
}

func (f *uint64Flag) String() string { return strconv.FormatUint(f.v, 10) }

func (f *uint64Flag) Set(s string) error {
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return err
	}
	f.v = v
	f.set = true
	return nil
}

type boolFlag struct {
	v   bool
	set bool
}

func (f *boolFlag) String() string {
	if f.v {
		return "true"
	}
	return "false"
}

func (f *boolFlag) Set(s string) error {
	v, err := strconv.ParseBool(s)
	if err != nil {
		return err
	}
	f.v = v
	f.set = true
	return nil
}

func (f *boolFlag) IsBoolFlag() bool { return true }

func run() error {
	cacheDir := flag.String("cache-dir", "", "Cache directory (default: ~/.config/cc/)")
	buildOut := flag.String("build", "", "Build a prebaked bundle folder at this path, then exit")
	var cpusFlag intFlag
	cpusFlag.v = 1
	flag.Var(&cpusFlag, "cpus", "Number of vCPUs")
	var memoryFlag uint64Flag
	memoryFlag.v = 1024
	flag.Var(&memoryFlag, "memory", "Memory in MB")
	dbg := flag.Bool("debug", false, "Enable debug logging")
	debugFile := flag.String("debug-file", "", "Write debug stream to file")
	cpuprofile := flag.String("cpuprofile", "", "Write CPU profile to file")
	memprofile := flag.String("memprofile", "", "Write memory profile to file")
	var dmesgFlag boolFlag
	flag.Var(&dmesgFlag, "dmesg", "Print kernel dmesg during boot and runtime")
	var networkFlag boolFlag
	flag.Var(&networkFlag, "network", "Enable networking")
	timeout := flag.Duration("timeout", 0, "Timeout for the container")
	packetdump := flag.String("packetdump", "", "Write packet capture (pcap) to file (requires -network)")
	var execFlag boolFlag
	flag.Var(&execFlag, "exec", "Execute the entrypoint as PID 1 taking over init")
	gpu := flag.Bool("gpu", false, "Enable GPU and create a window")
	termWin := flag.Bool("term", false, "Open a terminal window and connect it to the VM console")
	addVirtioFs := flag.String("add-virtiofs", "", "Specify a comma-separated list of blank virtio-fs tags to create")
	timesliceFile := flag.String("timeslice-file", "", "Write timeslice data to file")
	var snapshotCacheFlag boolFlag
	snapshotCacheFlag.v = false // Disable by default
	flag.Var(&snapshotCacheFlag, "snapshot-cache", "Enable boot snapshot caching (default: false)")
	archFlag := flag.String("arch", "", "Target architecture (amd64, arm64). If different from host, enables QEMU emulation")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <image> [command] [args...]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Run a command inside an OCI container image in a virtual machine.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  %s alpine:latest /bin/sh -c 'echo hello'\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s ubuntu:22.04 ls -la\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	var virtioFsTags []string

	if *addVirtioFs != "" {
		virtioFsTags = strings.Split(*addVirtioFs, ",")
		for _, tag := range virtioFsTags {
			if tag == "" {
				return fmt.Errorf("empty virtio-fs tag")
			}
		}
	}

	// Check for debug file from flag or environment variable
	debugFilePath := *debugFile
	if debugFilePath == "" {
		debugFilePath = os.Getenv("CC_DEBUG_FILE")
	}
	if debugFilePath != "" {
		if err := debug.OpenFile(debugFilePath); err != nil {
			return fmt.Errorf("open debug file: %w", err)
		}
		defer debug.Close()

		debug.Writef("cc debug logging enabled", "filename=%s", debugFilePath)
	}

	if *timesliceFile != "" {
		f, err := os.Create(*timesliceFile)
		if err != nil {
			return fmt.Errorf("create timeslice file: %w", err)
		}
		defer f.Close()

		w, err := timeslice.StartRecording(f)
		if err != nil {
			return fmt.Errorf("open timeslice file: %w", err)
		}
		defer w.Close()
	}

	if *dbg {
		slog.SetDefault(slog.New(slog.NewTextHandler(
			&fixCrlf{w: os.Stderr},
			&slog.HandlerOptions{Level: slog.LevelDebug},
		)))
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(
			&fixCrlf{w: os.Stderr},
			&slog.HandlerOptions{Level: slog.LevelInfo},
		)))
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			return fmt.Errorf("create cpu profile file: %w", err)
		}
		defer f.Close()

		if err := pprof.StartCPUProfile(f); err != nil {
			return fmt.Errorf("start cpu profile: %w", err)
		}
		defer pprof.StopCPUProfile()
	}

	if *memprofile != "" {
		defer func() {
			f, err := os.Create(*memprofile)
			if err != nil {
				slog.Error("create memory profile file", "error", err)
				return
			}
			defer f.Close()

			if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
				slog.Error("write memory profile", "error", err)
			}
		}()
	}

	if *packetdump != "" && !networkFlag.v {
		return fmt.Errorf("-packetdump requires -network")
	}
	if *termWin && *gpu {
		return fmt.Errorf("-term and -gpu are mutually exclusive")
	}
	if *termWin && *dbg {
		return fmt.Errorf("-term and -debug are mutually exclusive")
	}

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		return fmt.Errorf("image reference required")
	}

	imageRef := args[0]
	var cmd []string
	if len(args) > 1 {
		cmd = args[1:]
	}

	// Determine host architecture (for hypervisor)
	hvArch, err := parseArchitecture(runtime.GOARCH)
	if err != nil {
		return err
	}

	// Determine target architecture for image
	imageArch := hvArch
	if *archFlag != "" {
		imageArch, err = parseArchitecture(*archFlag)
		if err != nil {
			return fmt.Errorf("invalid -arch value: %w", err)
		}
	}

	// Create OCI client
	client, err := oci.NewClient(*cacheDir)
	if err != nil {
		return fmt.Errorf("create OCI client: %w", err)
	}

	if *buildOut != "" {
		img, err := client.PullForArch(imageRef, hvArch)
		if err != nil {
			return fmt.Errorf("pull image: %w", err)
		}

		imageDir := filepath.Join(*buildOut, bundle.DefaultImageDir)
		if err := oci.ExportToDir(img, imageDir); err != nil {
			return fmt.Errorf("export prebaked image: %w", err)
		}

		meta := bundle.Metadata{
			Version:     1,
			Name:        "{{name}}",
			Description: "{{description}}",
			Boot: bundle.BootConfig{
				ImageDir: bundle.DefaultImageDir,
				Command:  img.Command(cmd),
				CPUs:     cpusFlag.v,
				MemoryMB: memoryFlag.v,
				Exec:     execFlag.v,
				Dmesg:    dmesgFlag.v,
			},
		}
		if err := bundle.WriteTemplate(*buildOut, meta); err != nil {
			return fmt.Errorf("write bundle metadata: %w", err)
		}

		slog.Info("bundle built", "dir", *buildOut)
		return nil
	}

	slog.Debug("Loading image", "ref", imageRef, "arch", imageArch)
	debug.Writef("cc.run load image", "loading image %s for architecture %s", imageRef, imageArch)

	var meta bundle.Metadata
	var img *oci.Image

	switch {
	case bundle.IsBundleDir(imageRef):
		m, loaded, err := bundle.Load(imageRef)
		if err != nil {
			return err
		}
		meta, img = m, loaded

		// Apply bundle boot defaults iff the user did not override via flags.
		if !cpusFlag.set && meta.Boot.CPUs != 0 {
			cpusFlag.v = meta.Boot.CPUs
		}
		if !memoryFlag.set && meta.Boot.MemoryMB != 0 {
			memoryFlag.v = meta.Boot.MemoryMB
		}
		if !execFlag.set {
			execFlag.v = meta.Boot.Exec
		}
		if !dmesgFlag.set {
			dmesgFlag.v = meta.Boot.Dmesg
		}
	case hasConfigJSON(imageRef):
		loaded, err := oci.LoadFromDir(imageRef)
		if err != nil {
			return fmt.Errorf("load prebaked image: %w", err)
		}
		img = loaded
	default:
		loaded, err := client.PullForArch(imageRef, imageArch)
		if err != nil {
			return fmt.Errorf("pull image: %w", err)
		}
		img = loaded
	}

	slog.Debug("Image pulled", "layers", len(img.Layers), "arch", img.Config.Architecture)
	debug.Writef("cc.run image pulled", "image pulled with %d layers, arch=%s", len(img.Layers), img.Config.Architecture)

	// Update imageArch based on actual pulled image architecture (may differ from requested)
	if img.Config.Architecture != "" {
		actualArch, err := parseArchitecture(img.Config.Architecture)
		if err == nil && actualArch != imageArch {
			slog.Info("Image architecture differs from requested", "requested", imageArch, "actual", actualArch)
			imageArch = actualArch
		}
	}

	// Determine command to run
	var execCmd []string
	if len(cmd) > 0 {
		execCmd = img.Command(cmd)
	} else if meta.Version != 0 && len(meta.Boot.Command) > 0 {
		execCmd = meta.Boot.Command
	} else {
		execCmd = img.Command(nil)
	}
	if len(execCmd) == 0 {
		return fmt.Errorf("no command specified and image has no entrypoint/cmd")
	}

	pathEnv := extractInitialPath(img.Config.Env)
	workDir := containerWorkDir(img)

	// Create container filesystem
	containerFS, err := oci.NewContainerFS(img)
	if err != nil {
		return fmt.Errorf("create container filesystem: %w", err)
	}
	defer containerFS.Close()

	debug.Writef("cc.run container filesystem created", "container filesystem created")

	execCmd, err = resolveCommandPath(containerFS, execCmd, pathEnv, workDir)
	if err != nil {
		return fmt.Errorf("resolve command: %w", err)
	}

	// If we're executing the entrypoint as PID 1, prefer exec'ing the resolved
	// target rather than a symlink path. This matters for binaries that rely on
	// $ORIGIN-based RUNPATH (e.g. Debian's systemd via /sbin/init symlink).
	if execFlag.v && strings.HasPrefix(execCmd[0], "/") {
		if resolved, err := containerFS.ResolvePath(execCmd[0]); err == nil {
			execCmd[0] = resolved
		}
	}

	slog.Debug("Running command", "cmd", execCmd)
	debug.Writef("cc.run running command", "running command %v", execCmd)

	// Create VirtioFS backend with container filesystem as root
	fsBackend := vfs.NewVirtioFsBackendWithAbstract()
	if err := fsBackend.SetAbstractRoot(containerFS); err != nil {
		return fmt.Errorf("set container filesystem as root: %w", err)
	}

	// Create hypervisor
	h, err := factory.OpenWithArchitecture(hvArch)
	if err != nil {
		return fmt.Errorf("create hypervisor: %w", err)
	}
	defer h.Close()

	debug.Writef("cc.run hypervisor created", "hypervisor created")

	// Load kernel
	kernelLoader, err := kernel.LoadForArchitecture(hvArch)
	if err != nil {
		return fmt.Errorf("load kernel: %w", err)
	}

	debug.Writef("cc.run kernel loaded", "kernel loaded for architecture %s", hvArch)

	// Add kernel modules to VFS for modprobe support
	if err := initx.AddKernelModulesToVFS(fsBackend, kernelLoader); err != nil {
		return fmt.Errorf("add kernel modules: %w", err)
	}
	debug.Writef("cc.run modules added", "added kernel modules to VFS")

	// Create VM with VirtioFS
	opts := []initx.Option{
		initx.WithDeviceTemplate(virtio.FSTemplate{
			Tag:     "rootfs",
			Backend: fsBackend,
			Arch:    hvArch,
		}),
		initx.WithDebugLogging(*dbg),
		initx.WithDmesgLogging(dmesgFlag.v),
	}

	base := uint64(0xd0006000)

	for _, tag := range virtioFsTags {
		opts = append(opts, initx.WithDeviceTemplate(virtio.FSTemplate{
			Tag:      tag,
			Backend:  vfs.NewVirtioFsBackendWithAbstract(),
			MMIOBase: base,
			Arch:     hvArch,
		}))
		base += 0x2000
	}

	var termWindow *termwin.Terminal
	if *termWin {
		scale := window.GetDisplayScale()
		physWidth := int(float32(1024) * scale)
		physHeight := int(float32(768) * scale)

		tw, err := termwin.New("cc", physWidth, physHeight)
		if err != nil {
			return fmt.Errorf("create terminal window: %w", err)
		}
		termWindow = tw
		defer termWindow.Close()

		opts = append(opts,
			initx.WithStdin(termWindow),
			initx.WithConsoleOutput(termWindow),
		)
	} else {
		// Wrap stdin with a filter to strip CPR responses on Windows.
		opts = append(opts, initx.WithStdin(wrapStdinForVT(os.Stdin)))
	}

	// Add network device if enabled
	if networkFlag.v {
		backend := netstack.New(slog.Default())
		var packetDumpFile *os.File
		defer func() {
			_ = backend.Close()
			if packetDumpFile != nil {
				_ = packetDumpFile.Close()
			}
		}()

		if *packetdump != "" {
			dir := filepath.Dir(*packetdump)
			if dir != "." {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return fmt.Errorf("create packet dump directory: %w", err)
				}
			}
			f, err := os.Create(*packetdump)
			if err != nil {
				return fmt.Errorf("create packet dump file: %w", err)
			}
			packetDumpFile = f
			if err := backend.OpenPacketCapture(packetDumpFile); err != nil {
				return fmt.Errorf("enable packet capture: %w", err)
			}
		}

		if err := backend.StartDNSServer(); err != nil {
			return fmt.Errorf("start DNS server: %w", err)
		}

		mac := net.HardwareAddr{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}

		netBackend, err := virtio.NewNetstackBackend(backend, mac)
		if err != nil {
			return fmt.Errorf("create netstack backend: %w", err)
		}

		opts = append(opts, initx.WithDeviceTemplate(virtio.NetTemplate{
			Backend: netBackend,
			MAC:     mac,
			Arch:    hvArch,
		}))

		debug.Writef("cc.run networking enabled", "networking enabled")
	}

	if *gpu {
		opts = append(opts, initx.WithGPUEnabled(true))
	}

	// Check if we need QEMU emulation for cross-architecture support
	// We need to detect this before creating the VM so binfmt_misc module is loaded
	needsQEMU := initx.NeedsQEMUEmulation(hvArch, imageArch)
	if needsQEMU {
		opts = append(opts, initx.WithQEMUEmulationEnabled(true))
	}

	vm, err := initx.NewVirtualMachine(
		h,
		cpusFlag.v,
		memoryFlag.v,
		kernelLoader,
		opts...,
	)
	if err != nil {
		return fmt.Errorf("create VM: %w", err)
	}
	defer vm.Close()

	// Ensure TERM is set for the container so terminal apps work correctly.
	env := img.Config.Env
	if !hasEnvVar(env, "TERM") {
		env = append(env, "TERM=xterm-256color")
	}

	// Prepare QEMU emulation config if needed (detection was done above before VM creation)
	var qemuConfig *initx.QEMUEmulationConfig
	if needsQEMU {
		slog.Info("Cross-architecture image detected, enabling QEMU emulation",
			"host", hvArch, "image", imageArch)
		debug.Writef("cc.run qemu", "enabling QEMU emulation for %s on %s host", imageArch, hvArch)

		cfg, err := initx.PrepareQEMUEmulation(hvArch, imageArch, client.CacheDir())
		if err != nil {
			return fmt.Errorf("prepare QEMU emulation: %w", err)
		}
		qemuConfig = cfg
	}

	// Build and run the container init program
	prog, err := initx.BuildContainerInitProgram(initx.ContainerInitConfig{
		Arch:          hvArch,
		Cmd:           execCmd,
		Env:           env,
		WorkDir:       workDir,
		EnableNetwork: networkFlag.v,
		Exec:          execFlag.v,
		UID:           img.Config.UID,
		GID:           img.Config.GID,
		QEMUEmulation: qemuConfig,
	})
	if err != nil {
		return err
	}

	slog.Debug("Booting VM")

	var ctx context.Context
	if *timeout > 0 {
		newCtx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		ctx = newCtx
	} else {
		ctx = context.Background()
	}

	// Put stdin into raw mode so we don't send cooked/echoed characters into the guest.
	// Do this after booting so that any Ctrl+C during boot still works to kill cc itself.

	debug.Writef("cc.run running command", "running command %v", execCmd)

	// Snapshot caching setup
	var sessionCfg initx.SessionConfig
	if snapshotCacheFlag.v && getSnapshotIO() != nil {
		// Get cache directory for snapshots
		snapshotCacheDir, err := getSnapshotCacheDir(*cacheDir)
		if err == nil {
			snapshotCache := initx.NewSnapshotCache(snapshotCacheDir, getSnapshotIO())

			// Compute config hash based on VM configuration
			configHash := hv.ComputeConfigHash(
				hvArch,
				memoryFlag.v<<20, // Convert MB to bytes
				vm.HVVirtualMachine().MemoryBase(),
				cpusFlag.v,
				nil, // Device configs - simplified for now
			)

			// Use a very old time as reference - snapshots are valid unless explicitly invalidated
			var referenceTime time.Time

			if snapshotCache.HasValidSnapshot(configHash, referenceTime) {
				// Try to load and restore cached snapshot
				snap, loadErr := snapshotCache.LoadSnapshot(configHash)
				if loadErr == nil {
					if restoreErr := vm.RestoreSnapshot(snap); restoreErr == nil {
						debug.Writef("cc.run snapshot", "restored from cache")
						sessionCfg.SkipBoot = true
					} else {
						slog.Debug("Failed to restore snapshot, falling back to boot", "error", restoreErr)
					}
				} else {
					slog.Debug("Failed to load snapshot, falling back to boot", "error", loadErr)
				}
			}

			if !sessionCfg.SkipBoot {
				// Set up callback to capture snapshot after boot
				sessionCfg.OnBootComplete = func() error {
					snap, captureErr := vm.CaptureSnapshot()
					if captureErr != nil {
						slog.Debug("Failed to capture boot snapshot", "error", captureErr)
						return nil // Don't fail the session
					}
					if saveErr := snapshotCache.SaveSnapshot(configHash, snap); saveErr != nil {
						slog.Debug("Failed to save boot snapshot", "error", saveErr)
					} else {
						debug.Writef("cc.run snapshot", "saved to cache")
					}
					return nil
				}
			}
		}
	}

	// Start the VM session now that we have the final execution context.
	// This handles boot → stdin forwarding → payload run.
	session := initx.StartSession(ctx, vm, prog, sessionCfg)

	// If GPU is enabled, set up the display manager and drive the window loop
	// on the main thread while the VM runs in the background.
	if *gpu && vm.GPU() != nil {
		// Get display scale factor and calculate physical window dimensions
		scale := window.GetDisplayScale()
		physWidth := int(float32(1024) * scale)
		physHeight := int(float32(768) * scale)

		// Create window for display with scaled dimensions
		win, err := window.New("cc", physWidth, physHeight, true)
		if err != nil {
			return fmt.Errorf("failed to create window: %w", err)
		} else {
			defer win.Close()

			// Create display manager and connect to GPU/Input devices
			displayMgr := virtio.NewDisplayManager(vm.GPU(), vm.Keyboard(), vm.Tablet())
			displayMgr.SetWindow(win)

			// Run display loop on main thread
			ticker := time.NewTicker(16 * time.Millisecond) // ~60 FPS
			defer ticker.Stop()

		displayLoop:
			for {
				select {
				case err := <-session.Done:
					if err != nil {
						return fmt.Errorf("run executable in initx virtual machine: %w", err)
					}
					break displayLoop
				case <-ctx.Done():
					return fmt.Errorf("context cancelled: %w", ctx.Err())
				case <-ticker.C:
					// Poll window events
					if !displayMgr.Poll() {
						// Window was closed
						return fmt.Errorf("window closed by user")
					}
					// Render and swap
					displayMgr.Render()
					displayMgr.Swap()
				}
			}

			slog.Info("cc: command exited")
			return nil
		}
	} else if *termWin && termWindow != nil {
		// If terminal window is enabled, run VM in background and drive the window loop
		// on the main thread.

		var lastResize struct {
			cols int
			rows int
		}

		err := termWindow.Run(ctx, termwin.Hooks{
			OnResize: func(cols, rows int) {
				if cols == lastResize.cols && rows == lastResize.rows {
					return
				}
				lastResize.cols, lastResize.rows = cols, rows
				vm.SetConsoleSize(cols, rows)
			},
			OnFrame: func() error {
				select {
				case err := <-session.Done:
					if err != nil {
						var exitErr *initx.ExitError
						if errors.As(err, &exitErr) {
							return exitErr
						}
						return fmt.Errorf("run VM: %w", err)
					}
					return io.EOF // stop loop cleanly
				default:
					return nil
				}
			},
		})
		if err != nil {
			if errors.Is(err, termwin.ErrWindowClosed) {
				// Best-effort: stop the VM when the user closes the window.
				_ = session.Stop(2 * time.Second)
				return fmt.Errorf("window closed by user")
			}
			if errors.Is(err, io.EOF) {
				// VM finished successfully and asked us to stop the loop.
				slog.Info("cc: command exited")
				return nil
			}
			var exitErr *initx.ExitError
			if errors.As(err, &exitErr) {
				return exitErr
			}
			return err
		}

		// If the loop returned nil, treat it as window closed.
		return fmt.Errorf("window closed by user")
	} else {
		if term.IsTerminal(int(os.Stdin.Fd())) {
			// On Windows, enable VT processing on stdout so ANSI sequences are interpreted.
			restoreVT, err := enableVTProcessing()
			if err != nil {
				return fmt.Errorf("enable VT processing: %w", err)
			}
			defer restoreVT()

			oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
			if err != nil {
				return fmt.Errorf("enable raw mode: %w", err)
			}
			defer term.Restore(int(os.Stdin.Fd()), oldState)
		}

		if err := session.Wait(); err != nil {
			var exitErr *initx.ExitError
			if errors.As(err, &exitErr) {
				return exitErr
			}
			return fmt.Errorf("run VM: %w", err)
		}

		debug.Writef("cc.run command exited", "command exited")

		return nil
	}
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
		if after, ok := strings.CutPrefix(entry, "PATH="); ok {
			return after
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

func hasConfigJSON(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, "config.json"))
	return err == nil
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

	if strings.Contains(resolved[0], "/") {
		return resolved, nil
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

	for dir := range strings.SplitSeq(pathEnv, ":") {
		switch {
		case dir == "":
			dir = workDir
		case !path.IsAbs(dir):
			dir = path.Join(workDir, dir)
		}

		candidate := path.Join(dir, file)
		entry, err := fs.Lookup(candidate)
		if err != nil {
			continue
		}

		// If it's a symlink, resolve it and check the target
		if entry.Symlink != nil {
			resolved, err := fs.ResolvePath(candidate)
			if err != nil {
				continue
			}
			entry, err = fs.Lookup(resolved)
			if err != nil {
				continue
			}
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

func getSnapshotCacheDir(cacheDir string) (string, error) {
	if cacheDir == "" {
		cfg, err := os.UserConfigDir()
		if err != nil {
			return "", err
		}
		cacheDir = filepath.Join(cfg, "cc")
	}
	return filepath.Join(cacheDir, "snapshots"), nil
}
