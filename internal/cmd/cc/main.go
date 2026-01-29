package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	cc "github.com/tinyrange/cc"
	"github.com/tinyrange/cc/internal/bundle"
	"github.com/tinyrange/cc/internal/gowin/window"
	"github.com/tinyrange/cc/internal/initx"
	"golang.org/x/term"
)

// Track which flags were explicitly set
var flagsSet = make(map[string]bool)

func recordSetFlags() {
	flag.Visit(func(f *flag.Flag) {
		flagsSet[f.Name] = true
	})
}

func isFlagSet(name string) bool {
	return flagsSet[name]
}

func main() {
	// On macOS, pin to main thread for potential future window support
	if runtime.GOOS == "darwin" {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
	}

	if err := cc.EnsureExecutableIsSigned(); err != nil {
		fmt.Fprintf(os.Stderr, "cc: ensure executable is signed: %v\n", err)
		os.Exit(1)
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

func run() error {
	memoryMB := flag.Uint64("memory", 256, "Memory in MB")
	cpus := flag.Int("cpus", 1, "Number of vCPUs")
	timeout := flag.Duration("timeout", 0, "Timeout for the container")
	workdir := flag.String("workdir", "", "Working directory inside the container")
	user := flag.String("user", "", "User to run as (uid or uid:gid)")
	dmesg := flag.Bool("dmesg", false, "Enable kernel dmesg output (loglevel=7)")
	execMode := flag.Bool("exec", false, "Run entrypoint as PID 1 (no fork)")
	packetdump := flag.String("packetdump", "", "Write packet capture to file (pcap format)")
	mountFlags := &mountSlice{}
	flag.Var(mountFlags, "mount", "Mount: tag (empty writable), tag:hostpath (read-only), or tag:hostpath:rw")
	gpuFlag := flag.Bool("gpu", false, "Enable GPU and open a window for display")
	cacheDir := flag.String("cache-dir", "", "OCI image cache directory (default: platform-specific)")
	envFlags := &stringSlice{}
	flag.Var(envFlags, "env", "Environment variables (KEY=value), can be specified multiple times")

	// New flags for bundle, arch, and dockerfile support
	buildOut := flag.String("build", "", "Build a prebaked bundle at this path and exit")
	archFlag := flag.String("arch", "", "Target architecture (amd64, arm64)")
	dockerfile := flag.String("dockerfile", "", "Path to Dockerfile to build and run")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <image|bundle> [command] [args...]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Run a command inside an OCI container image in a virtual machine.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  %s alpine:latest /bin/sh -c 'echo hello'\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s ubuntu:22.04 ls -la\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -memory 512 -timeout 30s alpine:latest sleep 10\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -arch arm64 alpine:latest uname -m\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -dockerfile ./Dockerfile\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -build ./mybundle alpine:latest\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s ./mybundle\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	recordSetFlags()

	args := flag.Args()

	// Validate conflicting options
	if *dockerfile != "" && *buildOut != "" {
		return fmt.Errorf("-dockerfile and -build cannot be used together")
	}

	// For -dockerfile mode, no positional args required
	// For other modes, need at least one arg (image or bundle)
	if *dockerfile == "" && len(args) < 1 {
		flag.Usage()
		return fmt.Errorf("image reference required")
	}

	var imageRef string
	var cmd []string
	if len(args) > 0 {
		imageRef = args[0]
		if len(args) > 1 {
			cmd = args[1:]
		}
	}

	// Create shared cache directory
	cache, err := cc.NewCacheDir(*cacheDir)
	if err != nil {
		return fmt.Errorf("create cache: %w", err)
	}

	// Create OCI client using shared cache
	client, err := cc.NewOCIClientWithCache(cache)
	if err != nil {
		return fmt.Errorf("create OCI client: %w", err)
	}

	// Create context with optional timeout
	ctx := context.Background()
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}

	// Source selection
	var source cc.InstanceSource
	var bundleMeta *bundle.Metadata
	var runtimeConfig *cc.DockerfileRuntimeConfig

	switch {
	case *dockerfile != "":
		// Build from Dockerfile
		content, err := os.ReadFile(*dockerfile)
		if err != nil {
			return fmt.Errorf("read dockerfile: %w", err)
		}
		contextDir := filepath.Dir(*dockerfile)

		source, err = cc.BuildDockerfileSource(ctx, content, client,
			cc.WithBuildContextDir(contextDir),
			cc.WithDockerfileCacheDir(cache.SnapshotPath()),
		)
		if err != nil {
			return fmt.Errorf("build dockerfile: %w", err)
		}
		runtimeConfig, _ = cc.BuildDockerfileRuntimeConfig(content)

	case bundle.IsBundleDir(imageRef):
		// Load from bundle
		meta, err := bundle.LoadMetadata(imageRef)
		if err != nil {
			return fmt.Errorf("load bundle metadata: %w", err)
		}
		bundleMeta = &meta

		imageDir := filepath.Join(imageRef, meta.Boot.ImageDir)
		pullOpts := buildPullOpts(*archFlag)
		source, err = client.LoadFromDir(imageDir, pullOpts...)
		if err != nil {
			return fmt.Errorf("load bundle image: %w", err)
		}

	default:
		// Pull OCI image
		pullOpts := buildPullOpts(*archFlag)
		source, err = client.Pull(ctx, imageRef, pullOpts...)
		if err != nil {
			return fmt.Errorf("pull image: %w", err)
		}
	}

	defer func() {
		if closer, ok := source.(io.Closer); ok {
			closer.Close()
		}
	}()

	// Apply bundle defaults (only if flag not explicitly set)
	if bundleMeta != nil {
		if !isFlagSet("cpus") && bundleMeta.Boot.CPUs != 0 {
			*cpus = bundleMeta.Boot.CPUs
		}
		if !isFlagSet("memory") && bundleMeta.Boot.MemoryMB != 0 {
			*memoryMB = bundleMeta.Boot.MemoryMB
		}
		if !isFlagSet("exec") && bundleMeta.Boot.Exec {
			*execMode = true
		}
		if !isFlagSet("dmesg") && bundleMeta.Boot.Dmesg {
			*dmesg = true
		}
		// Prepend bundle env to user env
		if len(bundleMeta.Boot.Env) > 0 {
			envFlags.values = append(bundleMeta.Boot.Env, envFlags.values...)
		}
	}

	// Handle bundle building (-build flag)
	if *buildOut != "" {
		// Export image to bundle directory
		imageDir := filepath.Join(*buildOut, bundle.DefaultImageDir)
		if err := client.ExportToDir(source, imageDir); err != nil {
			return fmt.Errorf("export image: %w", err)
		}

		// Create metadata
		meta := bundle.Metadata{
			Version:     1,
			Name:        "{{name}}",
			Description: "{{description}}",
			Boot: bundle.BootConfig{
				ImageDir: bundle.DefaultImageDir,
				Command:  cmd,
				CPUs:     *cpus,
				MemoryMB: *memoryMB,
				Exec:     *execMode,
				Dmesg:    *dmesg,
				Env:      envFlags.values,
			},
		}

		if err := bundle.WriteTemplate(*buildOut, meta); err != nil {
			return fmt.Errorf("write bundle metadata: %w", err)
		}

		fmt.Printf("Bundle created at %s\n", *buildOut)
		return nil
	}

	// Determine command (priority: CLI > bundle > Dockerfile > image default > /bin/sh)
	if len(cmd) == 0 {
		if bundleMeta != nil && len(bundleMeta.Boot.Command) > 0 {
			cmd = bundleMeta.Boot.Command
		} else if runtimeConfig != nil {
			if len(runtimeConfig.Entrypoint) > 0 {
				cmd = append(runtimeConfig.Entrypoint, runtimeConfig.Cmd...)
			} else if len(runtimeConfig.Cmd) > 0 {
				cmd = runtimeConfig.Cmd
			}
		}
		if len(cmd) == 0 {
			cmd = []string{"/bin/sh"}
		}
	}

	// Detect if stdin is a terminal for interactive mode
	isTerminal := term.IsTerminal(int(os.Stdin.Fd()))

	// Set up packet capture if requested
	var pcapFile *os.File
	if *packetdump != "" {
		var err error
		pcapFile, err = os.Create(*packetdump)
		if err != nil {
			return fmt.Errorf("create packet capture file: %w", err)
		}
		defer pcapFile.Close()
	}

	// Build instance options
	var opts []cc.Option
	opts = append(opts, cc.WithMemoryMB(*memoryMB))
	opts = append(opts, cc.WithCPUs(*cpus))
	if *dmesg {
		opts = append(opts, cc.WithDmesg())
	}
	// Note: execMode is handled after instance creation via inst.Exec()
	if pcapFile != nil {
		opts = append(opts, cc.WithPacketCapture(pcapFile))
	}
	for _, mount := range mountFlags.mounts {
		opts = append(opts, cc.WithMount(mount))
	}
	if *gpuFlag {
		opts = append(opts, cc.WithGPU())
	}

	// Note: env and workdir are set at command level via SetEnv/SetDir

	if *user != "" {
		opts = append(opts, cc.WithUser(*user))
	}

	if *timeout > 0 {
		opts = append(opts, cc.WithTimeout(*timeout))
	}

	// Use shared cache directory for QEMU emulation and other cached resources
	opts = append(opts, cc.WithCache(cache))

	// Enable interactive mode when connected to a terminal
	if isTerminal {
		opts = append(opts, cc.WithInteractiveIO(wrapStdinForVT(os.Stdin), os.Stdout))
	}

	// Create and start the instance
	inst, err := cc.New(source, opts...)
	if err != nil {
		if errors.Is(err, cc.ErrHypervisorUnavailable) {
			return fmt.Errorf("hypervisor unavailable: %w", err)
		}
		return fmt.Errorf("create instance: %w", err)
	}
	defer inst.Close()

	// Put stdin into raw mode if it's a terminal (for interactive mode)
	var oldState *term.State
	if isTerminal && !*gpuFlag {
		// Enable Windows VT processing if needed (no-op on Unix)
		restoreVT, err := enableVTProcessing()
		if err != nil {
			// Not fatal - some older Windows versions may not support VT processing
			fmt.Fprintf(os.Stderr, "warning: could not enable VT processing: %v\n", err)
		} else {
			defer restoreVT()
		}

		oldState, err = term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return fmt.Errorf("enable raw mode: %w", err)
		}
		defer term.Restore(int(os.Stdin.Fd()), oldState)
	}

	// Handle GPU mode with display loop
	if *gpuFlag && inst.GPU() != nil {
		return runWithGPU(inst, cmd)
	}

	// Handle exec mode - command replaces init as PID 1
	if *execMode {
		if err := inst.Exec(cmd[0], cmd[1:]...); err != nil {
			// Restore terminal before exiting
			if oldState != nil {
				term.Restore(int(os.Stdin.Fd()), oldState)
			}
			return fmt.Errorf("exec: %w", err)
		}
		return nil
	}

	// Run the command (fork/exec mode)
	cmdObj := inst.Command(cmd[0], cmd[1:]...)

	// Apply environment variables from -env flags
	for _, env := range envFlags.values {
		key, value, _ := strings.Cut(env, "=")
		cmdObj.SetEnv(key, value)
	}

	// Apply working directory from -workdir flag
	if *workdir != "" {
		cmdObj.SetDir(*workdir)
	}

	// Configure I/O
	if !isTerminal {
		// Capture mode - use vsock for stdin/stdout
		cmdObj.SetStdin(os.Stdin).
			SetStdout(os.Stdout).
			SetStderr(os.Stderr)
	}
	// Interactive mode - stdin/stdout are handled by virtio-console

	if err := cmdObj.Run(); err != nil {
		// Restore terminal before exiting
		if oldState != nil {
			term.Restore(int(os.Stdin.Fd()), oldState)
		}

		// Check if it's an exit error with a non-zero code
		exitCode := cmdObj.ExitCode()
		if exitCode != 0 {
			return &initx.ExitError{Code: exitCode}
		}
		return fmt.Errorf("run command: %w", err)
	}

	return nil
}

// buildPullOpts constructs pull options based on the arch flag.
func buildPullOpts(arch string) []cc.OCIPullOption {
	var opts []cc.OCIPullOption
	if arch != "" {
		opts = append(opts, cc.WithPlatform("linux", arch))
	}
	return opts
}

// runWithGPU runs a command with GPU display enabled.
// The display loop runs on the main thread while the command runs in a goroutine.
func runWithGPU(inst cc.Instance, cmd []string) error {
	gpu := inst.GPU()

	// Get display scale factor and calculate physical window dimensions
	scale := window.GetDisplayScale()
	physWidth := int(float32(1024) * scale)
	physHeight := int(float32(768) * scale)

	// Create window (useCoreProfile=true for modern OpenGL)
	win, err := window.New("cc", physWidth, physHeight, true)
	if err != nil {
		return fmt.Errorf("create window: %w", err)
	}
	defer win.Close()

	// Connect GPU to window
	gpu.SetWindow(win)

	// Run command in a goroutine
	cmdDone := make(chan error, 1)
	var cmdObj cc.Cmd
	go func() {
		cmdObj = inst.Command(cmd[0], cmd[1:]...).
			SetStdin(os.Stdin).
			SetStdout(os.Stdout).
			SetStderr(os.Stderr)
		cmdDone <- cmdObj.Run()
	}()

	// Run display loop on main thread at ~60 FPS
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case err := <-cmdDone:
			if err != nil {
				if cmdObj != nil {
					exitCode := cmdObj.ExitCode()
					if exitCode != 0 {
						return &initx.ExitError{Code: exitCode}
					}
				}
				return fmt.Errorf("run command: %w", err)
			}
			return nil

		case <-ticker.C:
			// Poll window events
			if !gpu.Poll() {
				// Window was closed
				return fmt.Errorf("window closed by user")
			}
			// Render and swap
			gpu.Render()
			gpu.Swap()
		}
	}
}

// stringSlice implements flag.Value for collecting multiple string flags.
type stringSlice struct {
	values []string
}

func (s *stringSlice) String() string {
	return strings.Join(s.values, ", ")
}

func (s *stringSlice) Set(value string) error {
	s.values = append(s.values, value)
	return nil
}

// mountSlice implements flag.Value for collecting multiple mount flags.
type mountSlice struct {
	mounts []cc.MountConfig
}

func (m *mountSlice) String() string {
	return ""
}

func (m *mountSlice) Set(value string) error {
	// Format: tag (empty writable), tag:hostpath (read-only), or tag:hostpath:rw
	parts := strings.Split(value, ":")
	if len(parts) == 0 || parts[0] == "" {
		return fmt.Errorf("mount format: tag, tag:hostpath, or tag:hostpath:rw")
	}

	config := cc.MountConfig{
		Tag:      parts[0],
		Writable: true, // default writable for empty mounts
	}

	if len(parts) >= 2 && parts[1] != "" {
		config.HostPath = parts[1]
		config.Writable = false // default read-only for host mounts
	}

	if len(parts) >= 3 && parts[2] == "rw" {
		config.Writable = true
	}

	m.mounts = append(m.mounts, config)
	return nil
}
