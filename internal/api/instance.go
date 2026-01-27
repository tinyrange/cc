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
	"github.com/tinyrange/cc/internal/timeslice"
	"github.com/tinyrange/cc/internal/vfs"
)

var (
	envInitOnce       sync.Once
	envInitErr        error
	timesliceCloser   io.Closer
	timesliceCloserMu sync.Mutex
)

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
	src     *ociSource

	ctx    context.Context
	cancel context.CancelFunc

	mu     sync.Mutex
	closed bool

	// Config from options
	memoryMB       uint64
	cpus           int
	env            []string
	workdir        string
	user           string
	timeout        time.Duration
	skipEntrypoint bool

	// Filesystem backend for direct FS operations
	fsBackend vfs.VirtioFsBackend
}

// New creates and starts a new Instance from the given source.
func New(source InstanceSource, opts ...Option) (Instance, error) {
	// Initialize debug/timeslice/verbose from environment variables (once per process)
	if err := initFromEnv(); err != nil {
		return nil, &Error{Op: "new", Err: err}
	}

	src, ok := source.(*ociSource)
	if !ok {
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
		id:             generateID(),
		src:            src,
		ctx:            ctx,
		cancel:         cancel,
		memoryMB:       cfg.memoryMB,
		cpus:           cfg.cpus,
		env:            cfg.env,
		workdir:        cfg.workdir,
		user:           cfg.user,
		timeout:        cfg.timeout,
		skipEntrypoint: cfg.skipEntrypoint,
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

	arch := inst.src.arch
	if arch == "" || arch == hv.ArchitectureInvalid {
		arch = inst.h.Architecture()
	}

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
	if err := fsBackend.SetAbstractRoot(inst.src.cfs); err != nil {
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

	// Determine workdir
	workdir := inst.workdir
	if workdir == "" {
		workdir = inst.src.image.Config.WorkingDir
		if workdir == "" {
			workdir = "/"
		}
	}

	// Merge environment
	env := append([]string{}, inst.src.image.Config.Env...)
	env = append(env, inst.env...)

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

	// Get command from image config
	cmd := inst.src.image.Config.Entrypoint
	if len(inst.src.image.Config.Cmd) > 0 {
		cmd = append(cmd, inst.src.image.Config.Cmd...)
	}
	if len(cmd) == 0 {
		cmd = []string{"/bin/sh"}
	}

	// Build container init program
	// Always skip entrypoint - commands are run via inst.Command()
	initProg, err := initx.BuildContainerInitProgram(initx.ContainerInitConfig{
		Arch:                  arch,
		Cmd:                   cmd,
		Env:                   env,
		WorkDir:               workdir,
		EnableNetwork:         true,
		UID:                   uid,
		GID:                   gid,
		SkipEntrypoint:        true,
		TimesliceMMIOPhysAddr: inst.vm.TimesliceMMIOPhysAddr(),
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

// Exec interface methods

func (inst *instance) Command(name string, args ...string) Cmd {
	return inst.CommandContext(inst.ctx, name, args...)
}

func (inst *instance) CommandContext(ctx context.Context, name string, args ...string) Cmd {
	// Merge environment: image config env + instance options env
	env := append([]string{}, inst.src.image.Config.Env...)
	env = append(env, inst.env...)

	return &instanceCmd{
		inst: inst,
		ctx:  ctx,
		name: name,
		args: args,
		env:  env,
		dir:  inst.workdir,
	}
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

var _ Instance = (*instance)(nil)
