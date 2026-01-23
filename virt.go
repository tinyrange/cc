// Package cc provides virtualization primitives with APIs that mirror
// the Go standard library. An Instance represents an isolated virtual machine
// that can be interacted with using familiar os, os/exec, io, and net patterns.
package cc

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"time"
)

// -----------------------------------------------------------------------------
// File System
// -----------------------------------------------------------------------------

// File represents an open file in a guest filesystem. It mirrors *os.File.
type File interface {
	io.Reader
	io.Writer
	io.Closer
	io.Seeker
	io.ReaderAt
	io.WriterAt

	// Stat returns the FileInfo for the file.
	Stat() (fs.FileInfo, error)

	// Sync commits the file's contents to stable storage.
	Sync() error

	// Truncate changes the size of the file.
	Truncate(size int64) error

	// Name returns the name of the file as presented to Open or Create.
	Name() string
}

// FS provides filesystem operations on a guest. It mirrors functions from
// the os package.
//
// Use WithContext to set a deadline or enable cancellation for a series of
// operations:
//
//	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
//	defer cancel()
//
//	fs := instance.WithContext(ctx)
//	fs.MkdirAll("/app", 0755)
//	fs.WriteFile("/app/config.json", data, 0644)
//	// Both operations must complete within 5 seconds total
type FS interface {
	// WithContext returns an FS that uses the given context for all operations.
	// The returned FS shares the same underlying connection.
	WithContext(ctx context.Context) FS

	// Open opens a file for reading.
	Open(name string) (File, error)

	// Create creates or truncates a file for writing.
	Create(name string) (File, error)

	// OpenFile is the generalized open call. It opens a file with the
	// specified flag (O_RDONLY, O_WRONLY, O_RDWR, etc.) and permissions.
	OpenFile(name string, flag int, perm fs.FileMode) (File, error)

	// ReadFile reads the entire contents of a file.
	ReadFile(name string) ([]byte, error)

	// WriteFile writes data to a file, creating it if necessary.
	WriteFile(name string, data []byte, perm fs.FileMode) error

	// Stat returns file info for the named file.
	Stat(name string) (fs.FileInfo, error)

	// Lstat returns file info without following symlinks.
	Lstat(name string) (fs.FileInfo, error)

	// Remove removes a file or empty directory.
	Remove(name string) error

	// RemoveAll removes a path and any children it contains.
	RemoveAll(path string) error

	// Mkdir creates a directory.
	Mkdir(name string, perm fs.FileMode) error

	// MkdirAll creates a directory and any necessary parents.
	MkdirAll(path string, perm fs.FileMode) error

	// Rename renames (moves) a file.
	Rename(oldpath, newpath string) error

	// Symlink creates a symbolic link.
	Symlink(oldname, newname string) error

	// Readlink returns the destination of a symbolic link.
	Readlink(name string) (string, error)

	// ReadDir reads the named directory and returns its entries.
	ReadDir(name string) ([]fs.DirEntry, error)

	// Chmod changes the mode of the named file.
	Chmod(name string, mode fs.FileMode) error

	// Chown changes the numeric uid and gid of the named file.
	Chown(name string, uid, gid int) error

	// Chtimes changes the access and modification times of the named file.
	Chtimes(name string, atime, mtime time.Time) error
}

// -----------------------------------------------------------------------------
// Process Execution
// -----------------------------------------------------------------------------

// Cmd represents a command ready to be run in the guest. It mirrors *os/exec.Cmd.
//
// A Cmd cannot be reused after calling Run, Output, or CombinedOutput.
type Cmd interface {
	// Run starts the command and waits for it to complete.
	Run() error

	// Start starts the command but does not wait for it to complete.
	Start() error

	// Wait waits for a started command to complete.
	Wait() error

	// Output runs the command and returns its stdout.
	Output() ([]byte, error)

	// CombinedOutput runs the command and returns stdout and stderr combined.
	CombinedOutput() ([]byte, error)

	// StdinPipe returns a pipe connected to the command's stdin.
	StdinPipe() (io.WriteCloser, error)

	// StdoutPipe returns a pipe connected to the command's stdout.
	StdoutPipe() (io.ReadCloser, error)

	// StderrPipe returns a pipe connected to the command's stderr.
	StderrPipe() (io.ReadCloser, error)

	// SetStdin sets the command's stdin.
	SetStdin(r io.Reader) Cmd

	// SetStdout sets the command's stdout.
	SetStdout(w io.Writer) Cmd

	// SetStderr sets the command's stderr.
	SetStderr(w io.Writer) Cmd

	// SetDir sets the working directory for the command.
	SetDir(dir string) Cmd

	// SetEnv sets the environment variables for the command.
	// Each entry is of the form "key=value".
	SetEnv(env []string) Cmd

	// ExitCode returns the exit code of the exited process.
	// Only valid after Wait or Run complete.
	ExitCode() int
}

// Exec provides process execution on a guest. It mirrors functions from
// the os/exec package.
type Exec interface {
	// Command returns a Cmd ready to run the named program with the given args.
	Command(name string, args ...string) Cmd

	// CommandContext is like Command but includes a context that can cancel
	// the command. If the context is canceled, the process is killed.
	CommandContext(ctx context.Context, name string, args ...string) Cmd
}

// -----------------------------------------------------------------------------
// Networking
// -----------------------------------------------------------------------------

// Net provides network operations on a guest. It mirrors functions from
// the net package.
//
// Use WithContext to set a deadline or enable cancellation for a series of
// network setup operations.
type Net interface {
	// Dial connects to the address on the named network within the guest.
	// Networks are "tcp", "tcp4", "tcp6", "udp", "udp4", "udp6", "unix".
	Dial(network, address string) (net.Conn, error)

	// DialContext is like Dial but includes a context that can cancel the operation.
	DialContext(ctx context.Context, network, address string) (net.Conn, error)

	// Listen announces on the local network address within the guest.
	// The returned Listener accepts connections from within the guest.
	Listen(network, address string) (net.Listener, error)

	// ListenPacket announces on the local network address within the guest.
	ListenPacket(network, address string) (net.PacketConn, error)

	// Expose makes a host service accessible to the guest. When the guest
	// connects to guestAddress, the connection is forwarded to the host listener.
	//
	//	┌─────────┐           ┌─────────┐
	//	│  Guest  │  ──────►  │  Host   │
	//	│         │  connect  │         │
	//	│ :8080   │           │ :8080   │
	//	└─────────┘           └─────────┘
	//
	// Example: Expose a host database to the guest:
	//
	//	hostDB, _ := net.Listen("tcp", "localhost:5432")
	//	closer, _ := instance.Expose("tcp", "localhost:5432", hostDB)
	//
	// Returns a closer to stop exposing the service.
	Expose(guestNetwork, guestAddress string, host net.Listener) (io.Closer, error)

	// Forward makes a guest service accessible to the host. Connections accepted
	// by the guest listener are forwarded to the host address.
	//
	//	┌─────────┐           ┌─────────┐
	//	│  Guest  │  ◄──────  │  Host   │
	//	│         │  connect  │         │
	//	│ :3000   │           │ :3000   │
	//	└─────────┘           └─────────┘
	//
	// Example: Access a web server running in the guest from the host:
	//
	//	guestWeb, _ := instance.Listen("tcp", ":3000")
	//	closer, _ := instance.Forward(guestWeb, "tcp", "localhost:3000")
	//	// Now host can connect to localhost:3000
	//
	// Returns a closer to stop forwarding.
	Forward(guest net.Listener, hostNetwork, hostAddress string) (io.Closer, error)
}

// -----------------------------------------------------------------------------
// Instance
// -----------------------------------------------------------------------------

// Instance represents a running virtual machine. It composes FS, Exec, and Net
// to provide a complete interface for interacting with a guest using familiar
// Go standard library patterns.
//
// All operations are safe for concurrent use.
//
// An Instance must be closed when no longer needed to release resources.
type Instance interface {
	FS
	Exec
	Net

	// Close shuts down the instance and releases all resources.
	// Safe to call multiple times.
	Close() error

	// Wait blocks until the instance terminates.
	Wait() error

	// ID returns a unique identifier for this instance.
	ID() string
}

// -----------------------------------------------------------------------------
// Instance Options
// -----------------------------------------------------------------------------

// Option configures an Instance. Options are created by the With* functions
// in this package.
type Option interface {
	isOption()
}

// WithMemoryMB sets the memory size in megabytes.
func WithMemoryMB(size uint64) Option {
	return &memoryOption{sizeMB: size}
}

type memoryOption struct{ sizeMB uint64 }

func (*memoryOption) isOption() {}

// WithEnv sets environment variables for the guest init process.
// Each entry should be in "KEY=value" format.
func WithEnv(env ...string) Option {
	return &envOption{env: env}
}

type envOption struct{ env []string }

func (*envOption) isOption() {}

// WithTimeout sets a maximum lifetime for the instance. After this duration,
// the instance is forcibly terminated.
func WithTimeout(d time.Duration) Option {
	return &timeoutOption{d: d}
}

type timeoutOption struct{ d time.Duration }

func (*timeoutOption) isOption() {}

// WithWorkdir sets the initial working directory for commands.
func WithWorkdir(path string) Option {
	return &workdirOption{path: path}
}

type workdirOption struct{ path string }

func (*workdirOption) isOption() {}

// WithUser sets the user (and optionally group) to run as inside the guest.
// Format: "user" or "user:group" or numeric "1000" or "1000:1000".
func WithUser(user string) Option {
	return &userOption{user: user}
}

type userOption struct{ user string }

func (*userOption) isOption() {}

// -----------------------------------------------------------------------------
// Instance Sources
// -----------------------------------------------------------------------------

// InstanceSource is the source for creating a new Instance.
// Sources are obtained from OCIClient.Pull, etc.
type InstanceSource interface {
	isInstanceSource()
}

// -----------------------------------------------------------------------------
// OCI Client
// -----------------------------------------------------------------------------

// OCIClient pulls OCI images and converts them to InstanceSources.
type OCIClient interface {
	// Pull fetches an image and prepares it for use with New.
	// imageRef is a standard OCI reference like "alpine:latest" or
	// "ghcr.io/org/image@sha256:...".
	Pull(ctx context.Context, imageRef string, opts ...OCIPullOption) (InstanceSource, error)
}

// OCIPullOption configures an OCI pull operation.
type OCIPullOption interface {
	isOCIPullOption()
}

// WithPlatform specifies the platform to pull (e.g., "linux/amd64", "linux/arm64").
// Defaults to the host platform.
func WithPlatform(os, arch string) OCIPullOption {
	return &platformOption{os: os, arch: arch}
}

type platformOption struct{ os, arch string }

func (*platformOption) isOCIPullOption() {}

// WithAuth provides authentication credentials for private registries.
func WithAuth(username, password string) OCIPullOption {
	return &authOption{username: username, password: password}
}

type authOption struct{ username, password string }

func (*authOption) isOCIPullOption() {}

// PullPolicy determines when images are fetched from the registry.
type PullPolicy int

const (
	// PullIfNotPresent only pulls if the image is not in the local cache.
	PullIfNotPresent PullPolicy = iota
	// PullAlways always checks the registry, even if cached locally.
	PullAlways
	// PullNever only uses locally cached images; fails if not present.
	PullNever
)

func (p PullPolicy) String() string {
	switch p {
	case PullIfNotPresent:
		return "IfNotPresent"
	case PullAlways:
		return "Always"
	case PullNever:
		return "Never"
	default:
		return "Unknown"
	}
}

// WithPullPolicy sets when to pull from the registry vs use cache.
func WithPullPolicy(policy PullPolicy) OCIPullOption {
	return &pullPolicyOption{policy: policy}
}

type pullPolicyOption struct{ policy PullPolicy }

func (*pullPolicyOption) isOCIPullOption() {}

// NewOCIClient creates a new OCI client for pulling images.
func NewOCIClient() OCIClient {
	panic("unimplemented")
}

// -----------------------------------------------------------------------------
// Constructor
// -----------------------------------------------------------------------------

// New creates and starts a new Instance from the given source.
//
// The instance is ready for use when New returns. The caller must call
// Close when finished to release resources.
func New(source InstanceSource, opts ...Option) (Instance, error) {
	panic("unimplemented")
}

// -----------------------------------------------------------------------------
// Errors
// -----------------------------------------------------------------------------

// Error represents a cc operation error with structured information.
type Error struct {
	// Op is the operation that failed (e.g., "exec", "open", "dial").
	Op string

	// Path is the file path, if applicable.
	Path string

	// Err is the underlying error.
	Err error
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

// Common sentinel errors for use with errors.Is.
var (
	// ErrNotRunning indicates the instance is not running.
	ErrNotRunning = errors.New("instance not running")

	// ErrAlreadyClosed indicates the instance has been closed.
	ErrAlreadyClosed = errors.New("instance already closed")

	// ErrTimeout indicates an operation timed out.
	ErrTimeout = errors.New("operation timed out")
)
