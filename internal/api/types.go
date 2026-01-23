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
	Stat() (fs.FileInfo, error)
	Sync() error
	Truncate(size int64) error
	Name() string
}

// FS provides filesystem operations on a guest.
type FS interface {
	WithContext(ctx context.Context) FS
	Open(name string) (File, error)
	Create(name string) (File, error)
	OpenFile(name string, flag int, perm fs.FileMode) (File, error)
	ReadFile(name string) ([]byte, error)
	WriteFile(name string, data []byte, perm fs.FileMode) error
	Stat(name string) (fs.FileInfo, error)
	Lstat(name string) (fs.FileInfo, error)
	Remove(name string) error
	RemoveAll(path string) error
	Mkdir(name string, perm fs.FileMode) error
	MkdirAll(path string, perm fs.FileMode) error
	Rename(oldpath, newpath string) error
	Symlink(oldname, newname string) error
	Readlink(name string) (string, error)
	ReadDir(name string) ([]fs.DirEntry, error)
	Chmod(name string, mode fs.FileMode) error
	Chown(name string, uid, gid int) error
	Chtimes(name string, atime, mtime time.Time) error
}

// Cmd represents a command ready to be run in the guest.
type Cmd interface {
	Run() error
	Start() error
	Wait() error
	Output() ([]byte, error)
	CombinedOutput() ([]byte, error)
	StdinPipe() (io.WriteCloser, error)
	StdoutPipe() (io.ReadCloser, error)
	StderrPipe() (io.ReadCloser, error)
	SetStdin(r io.Reader) Cmd
	SetStdout(w io.Writer) Cmd
	SetStderr(w io.Writer) Cmd
	SetDir(dir string) Cmd
	SetEnv(env []string) Cmd
	ExitCode() int
}

// Exec provides process execution on a guest.
type Exec interface {
	Command(name string, args ...string) Cmd
	CommandContext(ctx context.Context, name string, args ...string) Cmd
}

// Net provides network operations on a guest.
type Net interface {
	Dial(network, address string) (net.Conn, error)
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
	Listen(network, address string) (net.Listener, error)
	ListenPacket(network, address string) (net.PacketConn, error)
	Expose(guestNetwork, guestAddress string, host net.Listener) (io.Closer, error)
	Forward(guest net.Listener, hostNetwork, hostAddress string) (io.Closer, error)
}

// Instance represents a running virtual machine.
type Instance interface {
	FS
	Exec
	Net
	Close() error
	Wait() error
	ID() string
}

// InstanceSource is the source for creating a new Instance.
type InstanceSource interface {
	IsInstanceSource()
}

// OCIClient pulls OCI images and converts them to InstanceSources.
type OCIClient interface {
	Pull(ctx context.Context, imageRef string, opts ...OCIPullOption) (InstanceSource, error)
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
