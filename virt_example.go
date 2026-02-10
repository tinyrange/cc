//go:build ignore

// This file demonstrates every public API in the cc package.
// It is excluded from the build and serves as a reference and compile-time check.

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"time"

	cc "github.com/tinyrange/cc"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	// =========================================================================
	// EnsureExecutableIsSigned - macOS hypervisor entitlement check
	// =========================================================================
	if err := cc.EnsureExecutableIsSigned(); err != nil {
		return fmt.Errorf("ensure signed: %w", err)
	}

	// =========================================================================
	// CacheDir - unified cache directory configuration (interface)
	// =========================================================================
	cache, err := cc.NewCacheDir("") // empty = platform default
	if err != nil {
		return fmt.Errorf("new cache dir: %w", err)
	}

	// CacheDir interface methods
	_ = cache.Path()         // base path
	_ = cache.OCIPath()      // OCI image cache
	_ = cache.QEMUPath()     // QEMU emulation binaries
	_ = cache.SnapshotPath() // filesystem snapshots

	// =========================================================================
	// SupportsHypervisor - early startup check
	// =========================================================================
	if err := cc.SupportsHypervisor(); err != nil {
		// Show friendly error to user instead of proceeding
		return fmt.Errorf("hypervisor unavailable: %w", err)
	}

	// =========================================================================
	// OCIClient - image pulling and management
	// =========================================================================

	// Create OCI client (various constructors)
	client, err := cc.NewOCIClient()
	if err != nil {
		return fmt.Errorf("new oci client: %w", err)
	}
	_ = client.CacheDir() // get cache directory

	// Create OCI client with shared cache
	_, _ = cc.NewOCIClientWithCache(cache)

	// =========================================================================
	// OCIPullOption - image pull configuration
	// =========================================================================
	pullOpts := []cc.OCIPullOption{
		cc.WithPlatform("linux", "amd64"),
		cc.WithAuth("username", "password"),
		cc.WithPullPolicy(cc.PullIfNotPresent),
	}

	// PullPolicy constants
	_ = cc.PullIfNotPresent
	_ = cc.PullAlways
	_ = cc.PullNever

	// WithProgressCallback - download progress reporting
	_ = cc.WithProgressCallback(func(p cc.DownloadProgress) {
		// DownloadProgress fields
		_ = p.Current   // Bytes downloaded so far
		_ = p.Total     // Total bytes to download (-1 if unknown)
		_ = p.Filename  // Name/path being downloaded
		_ = p.BlobIndex // Index of current blob (0-based)
		_ = p.BlobCount // Total number of blobs to download
	})

	// Pull an image
	source, err := client.Pull(ctx, "alpine:latest", pullOpts...)
	if err != nil {
		return fmt.Errorf("pull: %w", err)
	}

	// OCIClient.LoadFromDir - load prebaked image
	source, _ = client.LoadFromDir("/path/to/image", pullOpts...)

	// OCIClient.LoadFromTar - load from tarball (docker save format)
	source, _ = client.LoadFromTar("/path/to/image.tar", pullOpts...)

	// OCIClient.ExportToDir - export image to directory
	_ = client.ExportToDir(source, "/path/to/export")

	// =========================================================================
	// ImageConfig and OCISource - access container metadata
	// =========================================================================

	// SourceConfig helper - get ImageConfig from any InstanceSource
	cfg := cc.SourceConfig(source)
	if cfg != nil {
		_ = cfg.Architecture // "amd64", "arm64", etc.
		_ = cfg.Env          // []string{"PATH=/bin:/usr/bin", ...}
		_ = cfg.WorkingDir   // "/app"
		_ = cfg.Entrypoint   // []string{"/entrypoint.sh"}
		_ = cfg.Cmd          // []string{"--config", "/etc/app.conf"}
		_ = cfg.User         // "1000:1000"
		_ = cfg.Labels       // map[string]string{"version": "1.0"}

		// Get combined command (entrypoint + cmd)
		fullCmd := cfg.Command(nil)
		_ = fullCmd

		// Override cmd while keeping entrypoint
		overrideCmd := cfg.Command([]string{"custom", "args"})
		_ = overrideCmd
	}

	// Type assertion for OCISource interface
	if ociSrc, ok := source.(cc.OCISource); ok {
		cfg := ociSrc.Config()
		_ = cfg.Architecture
	}

	// =========================================================================
	// Option - instance configuration
	// =========================================================================
	opts := []cc.Option{
		cc.WithMemoryMB(256),
		cc.WithCPUs(2),
		cc.WithTimeout(5 * time.Minute),
		cc.WithUser("1000:1000"),
		cc.WithDmesg(),
		cc.WithCache(cache),
		cc.WithMount(cc.MountConfig{
			Tag:      "shared",
			HostPath: "/host/path",
			Writable: true, // default is read-only
		}),
	}

	// Interactive I/O (for terminal sessions)
	_ = cc.WithInteractiveIO(os.Stdin, os.Stdout)

	// Packet capture (for network debugging)
	var pcapBuf bytes.Buffer
	_ = cc.WithPacketCapture(&pcapBuf)

	// GPU support (requires display loop on main thread)
	_ = cc.WithGPU()

	// =========================================================================
	// New - create and start an instance
	// =========================================================================
	inst, err := cc.New(source, opts...)
	if err != nil {
		// Check for common errors
		if errors.Is(err, cc.ErrHypervisorUnavailable) {
			return fmt.Errorf("hypervisor unavailable: %w", err)
		}
		return fmt.Errorf("new instance: %w", err)
	}
	defer inst.Close()

	// Instance.ID - unique identifier
	_ = inst.ID()

	// =========================================================================
	// FS interface - filesystem operations (mirrors os package)
	// =========================================================================

	// WithContext - create FS with custom context
	fsWithCtx := inst.WithContext(ctx)
	_ = fsWithCtx

	// Open/Create/OpenFile
	file, _ := inst.Open("/etc/os-release")
	if file != nil {
		file.Close()
	}

	file, _ = inst.Create("/opt/test.txt")
	if file != nil {
		file.Close()
	}

	file, _ = inst.OpenFile("/opt/test2.txt", os.O_RDWR|os.O_CREATE, 0644)
	if file != nil {
		// File interface methods
		var buf [1024]byte
		_, _ = file.Read(buf[:])
		_, _ = file.Write([]byte("data"))
		_, _ = file.Seek(0, io.SeekStart)
		_, _ = file.ReadAt(buf[:], 0)
		_, _ = file.WriteAt([]byte("data"), 0)
		_, _ = file.Stat()
		_ = file.Sync()
		_ = file.Truncate(100)
		_ = file.Name()
		file.Close()
	}

	// ReadFile/WriteFile
	data, _ := inst.ReadFile("/etc/os-release")
	_ = data
	_ = inst.WriteFile("/opt/output.txt", []byte("content"), 0644)

	// Stat/Lstat
	info, _ := inst.Stat("/etc")
	_ = info
	info, _ = inst.Lstat("/etc")
	_ = info

	// Directory operations
	_ = inst.Mkdir("/opt/newdir", 0755)
	_ = inst.MkdirAll("/opt/nested/dirs", 0755)
	entries, _ := inst.ReadDir("/opt")
	_ = entries

	// File manipulation
	_ = inst.Rename("/opt/old", "/opt/new")
	_ = inst.Remove("/opt/file")
	_ = inst.RemoveAll("/opt/dir")

	// Symlinks
	_ = inst.Symlink("/target", "/opt/link")
	target, _ := inst.Readlink("/opt/link")
	_ = target

	// Permissions and times
	_ = inst.Chmod("/opt/file", fs.FileMode(0755))
	_ = inst.Chown("/opt/file", 1000, 1000)
	_ = inst.Chtimes("/opt/file", time.Now(), time.Now())

	// =========================================================================
	// FilesystemSnapshotOption - snapshot configuration
	// =========================================================================
	snapshotOpts := []cc.FilesystemSnapshotOption{
		cc.WithSnapshotExcludes("/opt/*", "/var/cache/*"),
		cc.WithSnapshotCacheDir(cache.SnapshotPath()),
	}

	// SnapshotFilesystem - create COW snapshot
	snapshot, _ := inst.SnapshotFilesystem(snapshotOpts...)
	if snapshot != nil {
		// FilesystemSnapshot methods
		_ = snapshot.CacheKey()
		_ = snapshot.Parent()
		snapshot.Close()
	}

	// =========================================================================
	// Exec interface - process execution (mirrors os/exec)
	// =========================================================================

	// Command/CommandContext
	cmd := inst.Command("ls", "-la", "/")
	cmd = inst.CommandContext(ctx, "ls", "-la", "/")

	// EntrypointCommand - run container's configured entrypoint
	cmd = inst.EntrypointCommand()               // uses image's CMD
	cmd = inst.EntrypointCommand("arg1", "arg2") // override CMD
	cmd = inst.EntrypointCommandContext(ctx, "arg1")

	// Cmd configuration (chainable)
	cmd = inst.Command("cat").
		SetStdin(bytes.NewReader([]byte("input"))).
		SetStdout(os.Stdout).
		SetStderr(os.Stderr).
		SetDir("/tmp").
		SetEnv("KEY", "value")

	// Cmd environment inspection
	_ = cmd.GetEnv("PATH")
	_ = cmd.Environ()

	// Cmd execution methods
	_ = cmd.Run()
	_ = cmd.Start()
	_ = cmd.Wait()
	_ = cmd.ExitCode()

	output, _ := inst.Command("echo", "hello").Output()
	_ = output

	combined, _ := inst.Command("sh", "-c", "echo out; echo err >&2").CombinedOutput()
	_ = combined

	// Cmd pipes
	cmd = inst.Command("cat")
	stdinPipe, _ := cmd.StdinPipe()
	stdoutPipe, _ := cmd.StdoutPipe()
	stderrPipe, _ := cmd.StderrPipe()
	_ = stdinPipe
	_ = stdoutPipe
	_ = stderrPipe

	// Exec/ExecContext - replace init as PID 1
	// (terminal operation - only use when you want the command to be PID 1)
	_ = inst.Exec("/bin/sh")
	_ = inst.ExecContext(ctx, "/bin/sh")

	// =========================================================================
	// Net interface - network operations (mirrors net package)
	// =========================================================================

	// Dial/DialContext - connect to guest'
	{
		conn, _ := inst.Dial("tcp", "127.0.0.1:8080")
		_ = conn
	}

	{
		conn, _ := inst.DialContext(ctx, "tcp", "127.0.0.1:8080")
		_ = conn
	}

	// Listen - listen for guest connections
	listener, _ := inst.Listen("tcp", ":8080")
	if listener != nil {
		listener.Close()
	}

	// ListenPacket - UDP listener
	{
		packetConn, _ := inst.ListenPacket("udp", ":8080")
		_ = packetConn
	}

	// Expose - expose guest port on host listener
	{
		hostListener, _ := net.Listen("tcp", ":9000")
		closer, _ := inst.Expose("tcp", ":8080", hostListener)
		_ = closer
	}

	// Forward - forward host to guest
	{
		guestListener, _ := inst.Listen("tcp", ":8080")
		closer, _ := inst.Forward(guestListener, "tcp", "localhost:9000")
		_ = closer
	}

	// =========================================================================
	// GPU interface - graphics and input (requires WithGPU option)
	// =========================================================================
	if gpu := inst.GPU(); gpu != nil {
		// GPU methods (must be called from main thread)
		// gpu.SetWindow(window) // window.Window from gowin package
		// for gpu.Poll() {
		//     gpu.Render()
		//     gpu.Swap()
		// }

		// Get framebuffer for custom rendering
		pixels, width, height, ok := gpu.GetFramebuffer()
		_, _, _, _ = pixels, width, height, ok
	}

	// =========================================================================
	// Instance control methods
	// =========================================================================

	// Done - async VM exit notification
	doneCh := inst.Done()
	select {
	case err := <-doneCh:
		_ = err // VM exit error (nil on clean shutdown)
	default:
		// VM still running
	}

	// SetConsoleSize - update terminal dimensions
	inst.SetConsoleSize(80, 24) // cols, rows

	// SetNetworkEnabled - toggle internet access
	inst.SetNetworkEnabled(false) // disable internet
	inst.SetNetworkEnabled(true)  // re-enable internet

	// =========================================================================
	// Instance lifecycle
	// =========================================================================
	// inst.Wait() // wait for instance to terminate
	inst.Close() // close and release resources

	// =========================================================================
	// Sentinel errors
	// =========================================================================
	_ = cc.ErrNotRunning
	_ = cc.ErrAlreadyClosed
	_ = cc.ErrTimeout
	_ = cc.ErrHypervisorUnavailable

	// Error type
	var ccErr *cc.Error
	if errors.As(err, &ccErr) {
		_ = ccErr.Op
		_ = ccErr.Path
		_ = ccErr.Err
		_ = ccErr.Error()
		_ = ccErr.Unwrap()
	}

	// =========================================================================
	// FilesystemSnapshotFactory - Dockerfile-like image building
	// =========================================================================
	factory := cc.NewFilesystemSnapshotFactory(client, cache.SnapshotPath())
	_ = factory
	// Built with chainable API:
	{
		snapshot, err := factory.
			From("alpine:3.19").
			Run("apk", "add", "--no-cache", "gcc").
			Exclude("/var/cache/*").
			Build(ctx)
		_ = snapshot
		_ = err
	}

	// =========================================================================
	// DockerfileOption - Dockerfile building configuration
	// =========================================================================
	dockerfileOpts := []cc.DockerfileOption{
		cc.WithBuildContextDir("."),
		cc.WithBuildArg("VERSION", "1.0"),
		cc.WithDockerfileCacheDir(cache.SnapshotPath()),
	}

	// WithBuildContext - custom build context
	buildCtx, _ := cc.NewDirBuildContext(".")
	if buildCtx != nil {
		dockerfileOpts = append(dockerfileOpts, cc.WithBuildContext(buildCtx))
	}

	// BuildDockerfileSource - build image from Dockerfile
	dockerfile := []byte(`
FROM alpine:3.19
RUN echo "hello"
CMD ["/bin/sh"]
`)
	dockerfileSource, _ := cc.BuildDockerfileSource(ctx, dockerfile, client, dockerfileOpts...)
	if dockerfileSource != nil {
		dockerfileSource.Close()
	}

	// BuildDockerfileRuntimeConfig - parse without building
	runtimeConfig, _ := cc.BuildDockerfileRuntimeConfig(dockerfile, dockerfileOpts...)
	if runtimeConfig != nil {
		_ = runtimeConfig.Entrypoint
		_ = runtimeConfig.Cmd
		_ = runtimeConfig.User
		_ = runtimeConfig.ExposePorts
		_ = runtimeConfig.Labels
		_ = runtimeConfig.Shell
		_ = runtimeConfig.StopSignal
	}

	// =========================================================================
	// Type aliases (for reference)
	// =========================================================================
	var (
		_ cc.File                      // open file handle
		_ cc.FS                        // filesystem interface
		_ cc.Cmd                       // command handle
		_ cc.Exec                      // execution interface
		_ cc.Net                       // network interface
		_ cc.Instance                  // running VM
		_ cc.InstanceSource            // source for creating instances
		_ cc.ImageConfig               // OCI image configuration
		_ cc.OCISource                 // OCI source with Config()
		_ cc.OCIClient                 // OCI image client
		_ cc.Option                    // instance option
		_ cc.OCIPullOption             // pull option
		_ cc.PullPolicy                // pull policy
		_ cc.DownloadProgress          // download progress info
		_ cc.ProgressCallback          // download progress callback
		_ cc.Error                     // structured error
		_ cc.GPU                       // graphics interface
		_ cc.MountConfig               // mount configuration
		_ cc.FilesystemSnapshot        // filesystem snapshot
		_ cc.FilesystemSnapshotOption  // snapshot option
		_ cc.FilesystemSnapshotFactory // snapshot builder
		_ cc.DockerfileOption          // dockerfile option
		_ cc.DockerfileBuildContext    // build context
		_ cc.DockerfileRuntimeConfig   // runtime config
		_ cc.CacheDir                  // cache directory
	)

	return nil
}

// Compile-time interface checks
var (
	_ io.Closer    = (cc.File)(nil)
	_ io.Reader    = (cc.File)(nil)
	_ io.Writer    = (cc.File)(nil)
	_ io.Seeker    = (cc.File)(nil)
	_ io.ReaderAt  = (cc.File)(nil)
	_ io.WriterAt  = (cc.File)(nil)
	_ net.Listener = (interface {
		Accept() (net.Conn, error)
		Close() error
		Addr() net.Addr
	})(nil)
)
