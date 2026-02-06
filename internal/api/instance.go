package api

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tinyrange/cc/internal/debug"
	"github.com/tinyrange/cc/internal/devices/virtio"
	"github.com/tinyrange/cc/internal/hv"
	"github.com/tinyrange/cc/internal/hv/factory"
	"github.com/tinyrange/cc/internal/initx"
	"github.com/tinyrange/cc/internal/linux/kernel"
	"github.com/tinyrange/cc/internal/netstack"
	"github.com/tinyrange/cc/internal/oci"
	"github.com/tinyrange/cc/internal/timeslice"
	"github.com/tinyrange/cc/internal/vfs"
)

var (
	envInitOnce       sync.Once
	envInitErr        error
	timesliceCloser   io.Closer
	timesliceCloserMu sync.Mutex
)

// SupportsHypervisor checks if the hypervisor is available on this system.
// Returns nil if available, or an error describing why not.
// Use this for early startup checks to show a friendly error message.
//
// Example:
//
//	if err := cc.SupportsHypervisor(); err != nil {
//	    log.Fatal("Hypervisor unavailable:", err)
//	}
func SupportsHypervisor() error {
	h, err := factory.Open()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrHypervisorUnavailable, err)
	}
	h.Close()
	return nil
}

// initFromEnv initializes debug, timeslice, and verbose logging from environment variables.
// This is called once per process, before the first instance is created.
func initFromEnv() error {
	envInitOnce.Do(func() {
		// CC_VERBOSE: enable verbose slog logging
		if os.Getenv("CC_VERBOSE") != "" {
			slog.SetDefault(slog.New(slog.NewTextHandler(
				os.Stderr,
				&slog.HandlerOptions{Level: slog.LevelDebug},
			)))
		}

		// CC_DEBUG_FILE: enable binary debug logging
		if debugFile := os.Getenv("CC_DEBUG_FILE"); debugFile != "" {
			if err := debug.OpenFile(debugFile); err != nil {
				envInitErr = fmt.Errorf("open debug file: %w", err)
				return
			}
			debug.Writef("api debug logging enabled", "filename=%s", debugFile)
		}

		// CC_TIMESLICE_FILE: enable timeslice recording
		if tsFile := os.Getenv("CC_TIMESLICE_FILE"); tsFile != "" {
			f, err := os.Create(tsFile)
			if err != nil {
				envInitErr = fmt.Errorf("create timeslice file: %w", err)
				return
			}

			w, err := timeslice.StartRecording(f)
			if err != nil {
				f.Close()
				envInitErr = fmt.Errorf("start timeslice recording: %w", err)
				return
			}

			timesliceCloserMu.Lock()
			timesliceCloser = w
			timesliceCloserMu.Unlock()
		}
	})
	return envInitErr
}

// instance implements Instance.
type instance struct {
	id string

	vm      *initx.VirtualMachine
	h       hv.Hypervisor
	session *initx.Session
	ns      *netstack.NetStack

	// Source components (extracted from ociSource or fsSnapshotSource)
	abstractRoot vfs.AbstractDir
	imageConfig  *oci.RuntimeConfig
	arch         hv.CpuArchitecture

	// Original source (for snapshotting - stores *ociSource or *fsSnapshotSource)
	source InstanceSource

	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	closed bool

	// Config from options
	memoryMB uint64
	cpus     int
	user     string
	timeout  time.Duration

	// Interactive mode - uses virtio-console for live I/O instead of vsock capture
	interactive       bool
	interactiveStdin  io.Reader
	interactiveStdout io.Writer

	// VM configuration
	dmesg bool

	// Networking
	packetCapture io.Writer

	// Mounts
	mounts []mountConfig

	// GPU
	gpu            bool
	displayManager *virtio.DisplayManager

	// Filesystem backend for direct FS operations
	fsBackend vfs.VirtioFsBackend

	// Cache directory configuration
	cache CacheDir
}

// New creates and starts a new Instance from the given source.
func New(source InstanceSource, opts ...Option) (Instance, error) {
	// Initialize debug/timeslice/verbose from environment variables (once per process)
	if err := initFromEnv(); err != nil {
		return nil, &Error{Op: "new", Err: err}
	}

	// Extract components from source (supports ociSource and fsSnapshotSource)
	var abstractRoot vfs.AbstractDir
	var imageConfig *oci.RuntimeConfig
	var arch hv.CpuArchitecture

	switch src := source.(type) {
	case *ociSource:
		abstractRoot = src.cfs
		imageConfig = &src.image.Config
		arch = src.arch
	case *fsSnapshotSource:
		abstractRoot = src.lcfs
		imageConfig = &src.baseImage.Config
		arch = src.arch
	default:
		return nil, &Error{Op: "new", Err: fmt.Errorf("unsupported source type: %T", source)}
	}

	cfg := parseInstanceOptions(opts)

	// Create context with optional timeout
	ctx := context.Background()
	var cancel context.CancelFunc
	if cfg.timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, cfg.timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}

	inst := &instance{
		id:                generateID(),
		abstractRoot:      abstractRoot,
		imageConfig:       imageConfig,
		arch:              arch,
		source:            source,
		ctx:               ctx,
		cancel:            cancel,
		memoryMB:          cfg.memoryMB,
		cpus:              cfg.cpus,
		user:              cfg.user,
		timeout:           cfg.timeout,
		interactive:       cfg.interactive,
		interactiveStdin:  cfg.interactiveStdin,
		interactiveStdout: cfg.interactiveStdout,
		dmesg:             cfg.dmesg,
		packetCapture:     cfg.packetCapture,
		mounts:            cfg.mounts,
		gpu:               cfg.gpu,
		cache:             cfg.cache,
	}

	if err := inst.start(); err != nil {
		cancel()
		return nil, err
	}

	return inst, nil
}

// start initializes and boots the VM.
func (inst *instance) start() error {
	var err error

	// Open hypervisor
	inst.h, err = factory.Open()
	if err != nil {
		return &Error{Op: "new", Err: fmt.Errorf("%w: %v", ErrHypervisorUnavailable, err)}
	}

	// Determine target architecture from source (container arch)
	containerArch := inst.arch
	if containerArch == "" || containerArch == hv.ArchitectureInvalid {
		containerArch = inst.h.Architecture()
	}

	// Host (hypervisor) architecture is always native
	hostArch := inst.h.Architecture()

	// Check if QEMU emulation is needed for cross-architecture
	var qemuConfig *initx.QEMUEmulationConfig
	if initx.NeedsQEMUEmulation(hostArch, containerArch) {
		var cacheDir string
		if inst.cache != nil {
			// Use the shared cache directory
			cacheDir = inst.cache.QEMUPath()
		}
		if cacheDir == "" {
			// Use a default cache directory
			if userCacheDir, err := os.UserCacheDir(); err == nil {
				cacheDir = userCacheDir + "/cc/qemu"
			}
		}
		cfg, err := initx.PrepareQEMUEmulation(hostArch, containerArch, cacheDir)
		if err != nil {
			inst.h.Close()
			return &Error{Op: "new", Err: fmt.Errorf("prepare QEMU emulation: %w", err)}
		}
		qemuConfig = cfg
	}

	// Kernel runs on host architecture, container binaries are emulated if needed
	arch := hostArch

	// Load kernel
	kernelLoader, err := kernel.LoadForArchitecture(arch)
	if err != nil {
		inst.h.Close()
		return &Error{Op: "new", Err: fmt.Errorf("load kernel: %w", err)}
	}

	// Create VirtioFS backend
	fsBackend := vfs.NewVirtioFsBackendWithAbstract()

	// Store reference for direct FS operations
	inst.fsBackend = fsBackend

	// Set the container filesystem as the root
	if err := fsBackend.SetAbstractRoot(inst.abstractRoot); err != nil {
		inst.h.Close()
		return &Error{Op: "new", Err: fmt.Errorf("set filesystem root: %w", err)}
	}

	// Add kernel modules to VFS
	if err := initx.AddKernelModulesToVFS(fsBackend, kernelLoader); err != nil {
		inst.h.Close()
		return &Error{Op: "new", Err: fmt.Errorf("add kernel modules: %w", err)}
	}

	// Create netstack
	inst.ns = netstack.New(slog.Default())
	if err := inst.ns.StartDNSServer(); err != nil {
		inst.h.Close()
		return &Error{Op: "new", Err: fmt.Errorf("start DNS server: %w", err)}
	}

	// Enable packet capture if requested
	if inst.packetCapture != nil {
		if err := inst.ns.OpenPacketCapture(inst.packetCapture); err != nil {
			inst.ns.Close()
			inst.h.Close()
			return &Error{Op: "new", Err: fmt.Errorf("enable packet capture: %w", err)}
		}
	}

	// Generate MAC address for guest
	guestMAC := make(net.HardwareAddr, 6)
	if _, err := rand.Read(guestMAC); err != nil {
		inst.ns.Close()
		inst.h.Close()
		return &Error{Op: "new", Err: fmt.Errorf("generate MAC: %w", err)}
	}
	// Set locally administered and unicast bits
	guestMAC[0] = (guestMAC[0] & 0xfe) | 0x02

	// Create netstack backend
	netBackend, err := virtio.NewNetstackBackend(inst.ns, guestMAC)
	if err != nil {
		inst.ns.Close()
		inst.h.Close()
		return &Error{Op: "new", Err: fmt.Errorf("create net backend: %w", err)}
	}

	// Create vsock backend for program loading
	vsockBackend := virtio.NewSimpleVsockBackend()

	// Parse user option
	var uid, gid *int
	if inst.user != "" {
		u, g, err := parseUser(inst.user)
		if err != nil {
			inst.h.Close()
			return &Error{Op: "new", Err: fmt.Errorf("parse user: %w", err)}
		}
		uid = &u
		gid = &g
	}

	// Determine workdir from image config
	workdir := inst.imageConfig.WorkingDir
	if workdir == "" {
		workdir = "/"
	}

	// Use environment from image config with defaults
	env := envWithDefaults(inst.imageConfig.Env)

	// Create VM options
	vmOpts := []initx.Option{
		initx.WithDeviceTemplate(virtio.FSTemplate{
			Tag:     "rootfs",
			Backend: fsBackend,
			Arch:    arch,
		}),
		initx.WithDeviceTemplate(virtio.NetTemplate{
			Backend: netBackend,
			MAC:     guestMAC,
			Arch:    arch,
		}),
		initx.WithDeviceTemplate(virtio.NewVsockTemplate(3, vsockBackend)),
		initx.WithVsockProgramLoader(vsockBackend, initx.VsockProgramPort),
	}

	// Configure interactive mode - route stdin/stdout to virtio-console
	if inst.interactive {
		if inst.interactiveStdin != nil {
			vmOpts = append(vmOpts, initx.WithStdin(inst.interactiveStdin))
		}
		if inst.interactiveStdout != nil {
			vmOpts = append(vmOpts, initx.WithConsoleOutput(inst.interactiveStdout))
		}
	}

	// Configure dmesg logging (loglevel=7)
	if inst.dmesg {
		vmOpts = append(vmOpts, initx.WithDmesgLogging(true))
	}

	// Enable GPU if requested
	if inst.gpu {
		vmOpts = append(vmOpts, initx.WithGPUEnabled(true))
	}

	// Add additional VirtioFS mounts
	mmioBase := uint64(0xd0006000)
	for _, mount := range inst.mounts {
		backend := vfs.NewVirtioFsBackendWithAbstract()
		if mount.hostPath != "" {
			// Create OS filesystem backend for host directory
			// Default is read-only unless Writable is set
			readOnly := !mount.writable
			osDir, err := vfs.NewOSDirBackend(mount.hostPath, readOnly)
			if err != nil {
				inst.ns.Close()
				inst.h.Close()
				return &Error{Op: "new", Err: fmt.Errorf("mount %s: %w", mount.tag, err)}
			}
			backend.SetAbstractRoot(osDir)
		}
		vmOpts = append(vmOpts, initx.WithDeviceTemplate(virtio.FSTemplate{
			Tag:      mount.tag,
			Backend:  backend,
			MMIOBase: mmioBase,
			Arch:     arch,
		}))
		mmioBase += 0x2000
	}

	// Create VM
	inst.vm, err = initx.NewVirtualMachine(
		inst.h,
		inst.cpus,
		inst.memoryMB,
		kernelLoader,
		vmOpts...,
	)
	if err != nil {
		inst.ns.Close()
		inst.h.Close()
		// Check if the error indicates hypervisor unavailability (e.g., CI, no entitlements)
		if errors.Is(err, hv.ErrHypervisorUnsupported) {
			return &Error{Op: "new", Err: fmt.Errorf("%w: %v", ErrHypervisorUnavailable, err)}
		}
		return &Error{Op: "new", Err: fmt.Errorf("create VM: %w", err)}
	}

	// Create display manager if GPU is enabled
	if inst.gpu && inst.vm.GPU() != nil {
		inst.displayManager = virtio.NewDisplayManager(
			inst.vm.GPU(),
			inst.vm.Keyboard(),
			inst.vm.Tablet(),
		)
	}

	// Get command from image config
	cmd := inst.imageConfig.Entrypoint
	if len(inst.imageConfig.Cmd) > 0 {
		cmd = append(cmd, inst.imageConfig.Cmd...)
	}
	if len(cmd) == 0 {
		cmd = []string{"/bin/sh"}
	}

	// Build container init program
	// Always skip entrypoint - commands are run via inst.Command() or inst.Exec()
	// Exec mode is handled by inst.Exec() method after instance creation
	initProg, err := initx.BuildContainerInitProgram(initx.ContainerInitConfig{
		Arch:                  arch,
		Cmd:                   cmd,
		Env:                   env,
		WorkDir:               workdir,
		EnableNetwork:         true,
		Exec:                  false, // Exec mode is now handled by inst.Exec()
		UID:                   uid,
		GID:                   gid,
		SkipEntrypoint:        true,
		TimesliceMMIOPhysAddr: inst.vm.TimesliceMMIOPhysAddr(),
		QEMUEmulation:         qemuConfig,
	})
	if err != nil {
		inst.vm.Close()
		inst.ns.Close()
		inst.h.Close()
		return &Error{Op: "new", Err: fmt.Errorf("build init program: %w", err)}
	}

	// Start session (boots VM only, skip the init program for now)
	// We'll run the container init program synchronously after boot to ensure
	// it completes before returning from New(). This prevents race conditions
	// between container init and user commands.
	bootDone := make(chan error, 1)
	inst.session = initx.StartSession(inst.ctx, inst.vm, nil, initx.SessionConfig{
		OnBootComplete: func() error {
			bootDone <- nil
			return nil
		},
	})

	// Wait for boot to complete
	select {
	case err := <-bootDone:
		if err != nil {
			inst.vm.Close()
			inst.ns.Close()
			inst.h.Close()
			return &Error{Op: "new", Err: fmt.Errorf("boot failed: %w", err)}
		}
	case err := <-inst.session.Done:
		// Session ended before boot completed - something went wrong
		inst.vm.Close()
		inst.ns.Close()
		inst.h.Close()
		if err != nil {
			return &Error{Op: "new", Err: fmt.Errorf("session failed during boot: %w", err)}
		}
		return &Error{Op: "new", Err: fmt.Errorf("session ended unexpectedly during boot")}
	case <-inst.ctx.Done():
		inst.vm.Close()
		inst.ns.Close()
		inst.h.Close()
		return &Error{Op: "new", Err: inst.ctx.Err()}
	}

	// Now run the container init program synchronously.
	// This sets up the container (chroot, mounts, etc.) and must complete
	// before user commands can run.
	if err := inst.vm.Run(inst.ctx, initProg); err != nil {
		inst.vm.Close()
		inst.ns.Close()
		inst.h.Close()
		return &Error{Op: "new", Err: fmt.Errorf("container init failed: %w", err)}
	}

	return nil
}

// hasEnvKey checks if an environment variable list contains a given key.
func hasEnvKey(env []string, key string) bool {
	prefix := key + "="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return true
		}
	}
	return false
}

// envWithDefaults returns a copy of env with default PATH and HOME if not set.
// This provides Docker-compatible defaults when the image doesn't specify them.
func envWithDefaults(env []string) []string {
	result := append([]string{}, env...)
	if !hasEnvKey(result, "PATH") {
		result = append(result, "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin")
	}
	if !hasEnvKey(result, "HOME") {
		result = append(result, "HOME=/root")
	}
	return result
}

// parseUser parses a user string into uid and gid.
func parseUser(user string) (int, int, error) {
	parts := strings.SplitN(user, ":", 2)
	uid, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid uid: %s", parts[0])
	}
	gid := uid
	if len(parts) > 1 {
		gid, err = strconv.Atoi(parts[1])
		if err != nil {
			return 0, 0, fmt.Errorf("invalid gid: %s", parts[1])
		}
	}
	return uid, gid, nil
}

// Close shuts down the instance and releases all resources.
func (inst *instance) Close() error {
	inst.mu.Lock()
	defer inst.mu.Unlock()

	if inst.closed {
		return nil
	}
	inst.closed = true

	// Cancel context to stop session
	inst.cancel()

	// Stop session with timeout
	if inst.session != nil {
		inst.session.Stop(5 * time.Second)
	}

	// Close VM
	if inst.vm != nil {
		inst.vm.Close()
	}

	// Close netstack
	if inst.ns != nil {
		inst.ns.Close()
	}

	// Close hypervisor
	if inst.h != nil {
		inst.h.Close()
	}

	// Note: source is not closed here - it's owned by the caller
	// and may be reused to create additional instances

	return nil
}

// Wait blocks until the instance terminates.
func (inst *instance) Wait() error {
	if inst.session == nil {
		return ErrNotRunning
	}
	return inst.session.Wait()
}

// ID returns a unique identifier for this instance.
func (inst *instance) ID() string {
	return inst.id
}

// WithContext returns an FS that uses the given context for all operations.
func (inst *instance) WithContext(ctx context.Context) FS {
	return &instanceFS{
		inst: inst,
		ctx:  ctx,
	}
}

// FS interface methods - delegate to instanceFS with background context

func (inst *instance) Open(name string) (File, error) {
	return inst.WithContext(inst.ctx).Open(name)
}

func (inst *instance) Create(name string) (File, error) {
	return inst.WithContext(inst.ctx).Create(name)
}

func (inst *instance) OpenFile(name string, flag int, perm fs.FileMode) (File, error) {
	return inst.WithContext(inst.ctx).OpenFile(name, flag, perm)
}

func (inst *instance) ReadFile(name string) ([]byte, error) {
	return inst.WithContext(inst.ctx).ReadFile(name)
}

func (inst *instance) WriteFile(name string, data []byte, perm fs.FileMode) error {
	return inst.WithContext(inst.ctx).WriteFile(name, data, perm)
}

func (inst *instance) Stat(name string) (fs.FileInfo, error) {
	return inst.WithContext(inst.ctx).Stat(name)
}

func (inst *instance) Lstat(name string) (fs.FileInfo, error) {
	return inst.WithContext(inst.ctx).Lstat(name)
}

func (inst *instance) Remove(name string) error {
	return inst.WithContext(inst.ctx).Remove(name)
}

func (inst *instance) RemoveAll(path string) error {
	return inst.WithContext(inst.ctx).RemoveAll(path)
}

func (inst *instance) Mkdir(name string, perm fs.FileMode) error {
	return inst.WithContext(inst.ctx).Mkdir(name, perm)
}

func (inst *instance) MkdirAll(path string, perm fs.FileMode) error {
	return inst.WithContext(inst.ctx).MkdirAll(path, perm)
}

func (inst *instance) Rename(oldpath, newpath string) error {
	return inst.WithContext(inst.ctx).Rename(oldpath, newpath)
}

func (inst *instance) Symlink(oldname, newname string) error {
	return inst.WithContext(inst.ctx).Symlink(oldname, newname)
}

func (inst *instance) Readlink(name string) (string, error) {
	return inst.WithContext(inst.ctx).Readlink(name)
}

func (inst *instance) ReadDir(name string) ([]fs.DirEntry, error) {
	return inst.WithContext(inst.ctx).ReadDir(name)
}

func (inst *instance) Chmod(name string, mode fs.FileMode) error {
	return inst.WithContext(inst.ctx).Chmod(name, mode)
}

func (inst *instance) Chown(name string, uid, gid int) error {
	return inst.WithContext(inst.ctx).Chown(name, uid, gid)
}

func (inst *instance) Chtimes(name string, atime, mtime time.Time) error {
	return inst.WithContext(inst.ctx).Chtimes(name, atime, mtime)
}

func (inst *instance) SnapshotFilesystem(opts ...FilesystemSnapshotOption) (FilesystemSnapshot, error) {
	return inst.WithContext(inst.ctx).SnapshotFilesystem(opts...)
}

// Exec interface methods

func (inst *instance) Command(name string, args ...string) Cmd {
	return inst.CommandContext(inst.ctx, name, args...)
}

func (inst *instance) CommandContext(ctx context.Context, name string, args ...string) Cmd {
	// Use environment from image config with defaults (commands can override via SetEnv)
	env := envWithDefaults(inst.imageConfig.Env)

	// Use workdir from image config (commands can override via SetDir)
	workdir := inst.imageConfig.WorkingDir
	if workdir == "" {
		workdir = "/"
	}

	return &instanceCmd{
		inst: inst,
		ctx:  ctx,
		name: name,
		args: args,
		env:  env,
		dir:  workdir,
	}
}

func (inst *instance) EntrypointCommand(args ...string) Cmd {
	return inst.EntrypointCommandContext(inst.ctx, args...)
}

func (inst *instance) EntrypointCommandContext(ctx context.Context, args ...string) Cmd {
	// Build entrypoint command from image config
	cmd := append([]string{}, inst.imageConfig.Entrypoint...)
	if len(args) > 0 {
		cmd = append(cmd, args...)
	} else if len(inst.imageConfig.Cmd) > 0 {
		cmd = append(cmd, inst.imageConfig.Cmd...)
	}
	if len(cmd) == 0 {
		cmd = []string{"/bin/sh"}
	}

	return inst.CommandContext(ctx, cmd[0], cmd[1:]...)
}

func (inst *instance) Exec(name string, args ...string) error {
	return inst.ExecContext(inst.ctx, name, args...)
}

func (inst *instance) ExecContext(ctx context.Context, name string, args ...string) error {
	return inst.execCommand(ctx, name, args)
}

// Net interface methods

func (inst *instance) Dial(network, address string) (net.Conn, error) {
	return inst.DialContext(inst.ctx, network, address)
}

func (inst *instance) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	// Dial to guest is complex - requires setting up reverse proxy through netstack
	// For now, return an error indicating this is not yet implemented
	return nil, &Error{Op: "dial", Err: fmt.Errorf("dial to guest not yet implemented")}
}

func (inst *instance) Listen(network, address string) (net.Listener, error) {
	// Listen within the netstack for guest connections
	if network != "tcp" && network != "tcp4" {
		return nil, &Error{Op: "listen", Err: fmt.Errorf("unsupported network: %s", network)}
	}

	ln, err := inst.ns.ListenInternal(network, address)
	if err != nil {
		return nil, &Error{Op: "listen", Err: err}
	}
	return ln, nil
}

func (inst *instance) ListenPacket(network, address string) (net.PacketConn, error) {
	return nil, &Error{Op: "listen_packet", Err: fmt.Errorf("not yet implemented")}
}

func (inst *instance) Expose(guestNetwork, guestAddress string, host net.Listener) (io.Closer, error) {
	return nil, &Error{Op: "expose", Err: fmt.Errorf("not yet implemented")}
}

func (inst *instance) Forward(guest net.Listener, hostNetwork, hostAddress string) (io.Closer, error) {
	return nil, &Error{Op: "forward", Err: fmt.Errorf("not yet implemented")}
}

// GPU returns the GPU interface if GPU is enabled, nil otherwise.
func (inst *instance) GPU() GPU {
	if inst.displayManager == nil {
		return nil
	}
	return &gpuAdapter{dm: inst.displayManager}
}

// gpuAdapter wraps DisplayManager to implement the GPU interface.
// This allows accepting any window type in the public API.
type gpuAdapter struct {
	dm *virtio.DisplayManager
}

func (g *gpuAdapter) SetWindow(w any) {
	g.dm.SetWindowAny(w)
}

func (g *gpuAdapter) Poll() bool {
	return g.dm.Poll()
}

func (g *gpuAdapter) Render() {
	g.dm.Render()
}

func (g *gpuAdapter) Swap() {
	g.dm.Swap()
}

func (g *gpuAdapter) GetFramebuffer() (pixels []byte, width, height uint32, ok bool) {
	return g.dm.GetFramebuffer()
}

var _ GPU = (*gpuAdapter)(nil)

// Done returns a channel that receives an error when the VM exits.
func (inst *instance) Done() <-chan error {
	if inst.session == nil {
		// Return a closed channel with nil error if no session
		ch := make(chan error, 1)
		ch <- nil
		close(ch)
		return ch
	}
	return inst.session.Done
}

// SetConsoleSize updates the virtio-console size.
func (inst *instance) SetConsoleSize(cols, rows int) {
	if inst.vm != nil {
		inst.vm.SetConsoleSize(cols, rows)
	}
}

// SetNetworkEnabled enables or disables internet access for the VM.
func (inst *instance) SetNetworkEnabled(enabled bool) {
	if inst.ns != nil {
		inst.ns.SetInternetAccessEnabled(enabled)
	}
}

var _ Instance = (*instance)(nil)
