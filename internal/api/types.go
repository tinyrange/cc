package api

import (
	"context"
	"io"
	"io/fs"
	"net"
	"time"
)

// File represents an open file in a guest filesystem.
type File interface {
	io.Reader
	io.Writer

	io.Closer
	io.Seeker

	io.ReaderAt
	io.WriterAt

	// Stat returns the FileInfo for the named file.
	Stat() (fs.FileInfo, error)
	// Sync flushes the file to disk.
	Sync() error
	// Truncate truncates the file to the given size.
	Truncate(size int64) error
	// Name returns the name of the file.
	Name() string
}

// FS provides filesystem operations on a guest.
type FS interface {
	// WithContext returns a new FS with the given context.
	WithContext(ctx context.Context) FS
	// Open opens the named file for reading.
	Open(name string) (File, error)
	// Create creates or truncates the named file.
	Create(name string) (File, error)
	// OpenFile opens the named file with the given flags and permissions.
	OpenFile(name string, flag int, perm fs.FileMode) (File, error)
	// ReadFile reads the named file and returns its contents.
	ReadFile(name string) ([]byte, error)
	// WriteFile writes the given data to the named file.
	WriteFile(name string, data []byte, perm fs.FileMode) error
	// Stat returns the FileInfo for the named file.
	Stat(name string) (fs.FileInfo, error)
	// Lstat returns the FileInfo for the named file.
	Lstat(name string) (fs.FileInfo, error)
	// Remove removes the named file.
	Remove(name string) error
	// RemoveAll removes the named file and all its children.
	RemoveAll(path string) error
	// Mkdir creates the named directory.
	Mkdir(name string, perm fs.FileMode) error
	// MkdirAll creates the named directory and all its parents.
	MkdirAll(path string, perm fs.FileMode) error
	// Rename renames the named file.
	Rename(oldpath, newpath string) error
	// Symlink creates a new symlink.
	Symlink(oldname, newname string) error
	// Readlink reads the contents of the named symlink.
	Readlink(name string) (string, error)
	// ReadDir reads the named directory and returns its contents.
	ReadDir(name string) ([]fs.DirEntry, error)
	// Chmod changes the mode of the named file.
	Chmod(name string, mode fs.FileMode) error
	// Chown changes the owner and group of the named file.
	Chown(name string, uid, gid int) error
	// Chtimes changes the access and modification times of the named file.
	Chtimes(name string, atime, mtime time.Time) error
	// SnapshotFilesystem creates a snapshot of the current filesystem state.
	// The snapshot uses COW semantics and can be used as an InstanceSource.
	SnapshotFilesystem(opts ...FilesystemSnapshotOption) (FilesystemSnapshot, error)
}

// Cmd represents a command ready to be run in the guest.
type Cmd interface {
	// Run runs the command.
	Run() error
	// Start starts the command.
	Start() error
	// Wait waits for the command to complete.
	Wait() error
	// Output runs the command and returns its stdout.
	Output() ([]byte, error)
	// CombinedOutput runs the command and returns its stdout and stderr.
	CombinedOutput() ([]byte, error)
	// StdinPipe returns a pipe that can be used to write to the command's stdin.
	StdinPipe() (io.WriteCloser, error)
	// StdoutPipe returns a pipe that can be used to read the command's stdout.
	StdoutPipe() (io.ReadCloser, error)
	// StderrPipe returns a pipe that can be used to read the command's stderr.
	StderrPipe() (io.ReadCloser, error)
	// SetStdin sets the command's stdin.
	SetStdin(r io.Reader) Cmd
	// SetStdout sets the command's stdout.
	SetStdout(w io.Writer) Cmd
	// SetStderr sets the command's stderr.
	SetStderr(w io.Writer) Cmd
	// SetDir sets the command's working directory.
	SetDir(dir string) Cmd
	// SetEnv sets a single environment variable (like os.Setenv).
	SetEnv(key, value string) Cmd
	// GetEnv returns the value of an environment variable (like os.Getenv).
	GetEnv(key string) string
	// Environ returns a copy of the command's environment variables.
	Environ() []string
	// ExitCode returns the command's exit code.
	ExitCode() int
}

// Exec provides process execution on a guest.
type Exec interface {
	// Command creates a new command.
	Command(name string, args ...string) Cmd
	// CommandContext creates a new command with the given context.
	CommandContext(ctx context.Context, name string, args ...string) Cmd
	// EntrypointCommand returns a command preconfigured with the container's
	// entrypoint. If args are provided, they replace the image's default CMD.
	// If no args are provided, the image's CMD is appended to ENTRYPOINT.
	// If neither ENTRYPOINT nor CMD are set, defaults to /bin/sh.
	EntrypointCommand(args ...string) Cmd
	// EntrypointCommandContext is like EntrypointCommand but accepts a context.
	EntrypointCommandContext(ctx context.Context, args ...string) Cmd
	// Exec replaces the init process with the specified command (like unix.Exec).
	// This is a terminal operation - the command becomes PID 1 and there is
	// no parent process waiting. When the command exits, the VM terminates.
	Exec(name string, args ...string) error
	// ExecContext is like Exec but accepts a context.
	ExecContext(ctx context.Context, name string, args ...string) error
}

// Net provides network operations on a guest.
type Net interface {
	// Dial dials the given network and address.
	Dial(network, address string) (net.Conn, error)
	// DialContext dials the given network and address with the given context.
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
	// Listen listens for incoming connections on the given network and address.
	Listen(network, address string) (net.Listener, error)
	// ListenPacket listens for incoming packets on the given network and address.
	ListenPacket(network, address string) (net.PacketConn, error)
	// Expose exposes a guest network listener on the given host network and address.
	Expose(guestNetwork, guestAddress string, host net.Listener) (io.Closer, error)
	// Forward forwards a guest network listener to the given host network and address.
	Forward(guest net.Listener, hostNetwork, hostAddress string) (io.Closer, error)
}

// Instance represents a running virtual machine.
type Instance interface {
	FS
	Exec
	Net

	// Close closes the instance.
	Close() error
	// Wait waits for the instance to complete.
	Wait() error
	// ID returns the instance's ID.
	ID() string

	// Done returns a channel that receives an error when the VM exits.
	// The channel is closed after the error is sent.
	// This allows non-blocking monitoring of VM termination.
	Done() <-chan error

	// SetConsoleSize updates the virtio-console size so the guest sees
	// the correct terminal dimensions. This is a best-effort operation
	// (no-op if console is unavailable).
	SetConsoleSize(cols, rows int)

	// SetNetworkEnabled enables or disables internet access for the VM.
	// When disabled, the VM can still communicate with the host netstack
	// but cannot reach external networks.
	SetNetworkEnabled(enabled bool)

	// GPU returns the GPU interface if GPU is enabled, nil otherwise.
	// When non-nil, the caller must run the display loop on the main thread.
	GPU() GPU
}

// GPU provides access to guest display and input devices.
// When GPU is enabled, the caller is responsible for running the display loop
// on the main thread using Poll(), Render(), and Swap().
//
// The SetWindow method accepts a window.Window from the gowin package.
// This is typed as any to avoid exposing internal types in the public API.
type GPU interface {
	// SetWindow connects the GPU to a window for rendering.
	// The window parameter must be a window.Window from the gowin package.
	// Must be called before Poll/Render/Swap.
	SetWindow(w any)

	// Poll processes window events and forwards input to the guest.
	// Returns false if the window was closed.
	// Must be called from the main thread.
	Poll() bool

	// Render renders the current framebuffer to the window.
	// Must be called from the main thread.
	Render()

	// Swap swaps the window buffers.
	// Must be called from the main thread.
	Swap()

	// GetFramebuffer returns the current framebuffer pixels.
	// Returns pixels in BGRA format, dimensions, and whether data is valid.
	GetFramebuffer() (pixels []byte, width, height uint32, ok bool)
}

// InstanceSource is the source for creating a new Instance.
type InstanceSource interface {
	IsInstanceSource()
}

// ImageConfig contains OCI image configuration metadata.
// This provides access to container runtime settings like environment,
// entrypoint, and working directory.
type ImageConfig struct {
	Architecture string            // "amd64", "arm64", etc.
	Env          []string          // Environment variables (KEY=VALUE format)
	WorkingDir   string            // Working directory for commands
	Entrypoint   []string          // Container entrypoint
	Cmd          []string          // Container CMD
	User         string            // User specification (e.g., "1000:1000")
	Labels       map[string]string // OCI labels
}

// Command returns the combined command to run, merging Entrypoint and Cmd.
// If overrideCmd is provided, it replaces the default Cmd.
// Returns the full command suitable for execution.
func (c *ImageConfig) Command(overrideCmd []string) []string {
	var result []string
	result = append(result, c.Entrypoint...)
	if len(overrideCmd) > 0 {
		result = append(result, overrideCmd...)
	} else {
		result = append(result, c.Cmd...)
	}
	if len(result) == 0 {
		return []string{"/bin/sh"}
	}
	return result
}

// OCISource extends InstanceSource with OCI-specific metadata.
// Use type assertion or SourceConfig() to access the ImageConfig.
type OCISource interface {
	InstanceSource
	Config() *ImageConfig
}

// FilesystemSnapshot represents a snapshot of a filesystem that can be used
// as an InstanceSource. It provides COW (copy-on-write) semantics and can be
// persisted and restored.
//
// Note: Named FilesystemSnapshot (not Snapshot) to reserve Snapshot for future
// full VM snapshots (memory + devices + filesystem).
type FilesystemSnapshot interface {
	InstanceSource

	// CacheKey returns a unique key for this snapshot derived from its
	// operation chain. Used for caching and deduplication.
	CacheKey() string

	// Parent returns the parent snapshot, or nil if this is a base snapshot.
	Parent() FilesystemSnapshot

	// Close releases resources held by the snapshot.
	Close() error
}

// FilesystemSnapshotOption configures a filesystem snapshot operation.
type FilesystemSnapshotOption interface {
	IsFilesystemSnapshotOption()
}

// OCIClient pulls OCI images and converts them to InstanceSources.
type OCIClient interface {
	// Pull pulls the given image and returns an InstanceSource.
	Pull(ctx context.Context, imageRef string, opts ...OCIPullOption) (InstanceSource, error)

	// LoadFromDir loads a prebaked image from a directory containing
	// config.json and layer files (*.idx/*.contents).
	LoadFromDir(dir string, opts ...OCIPullOption) (InstanceSource, error)

	// LoadFromTar loads an OCI image from a tarball (docker save format).
	LoadFromTar(tarPath string, opts ...OCIPullOption) (InstanceSource, error)

	// ExportToDir exports an InstanceSource to a directory as prebaked
	// config.json and layer files. Only works with OCI-based sources.
	ExportToDir(source InstanceSource, dir string) error

	// CacheDir returns the cache directory used by this client.
	CacheDir() string
}

// Option configures an Instance.
type Option interface {
	IsOption()
}

// OCIPullOption configures an OCI pull operation.
type OCIPullOption interface {
	IsOCIPullOption()
}

// PullPolicy determines when images are fetched from the registry.
type PullPolicy int

const (
	PullIfNotPresent PullPolicy = iota
	PullAlways
	PullNever
)

// Error represents an operation error with structured information.
type Error struct {
	Op   string
	Path string
	Err  error
}

func (e *Error) Error() string {
	if e.Path != "" {
		return e.Op + " " + e.Path + ": " + e.Err.Error()
	}
	return e.Op + ": " + e.Err.Error()
}

func (e *Error) Unwrap() error {
	return e.Err
}
