// Package cc provides virtualization primitives with APIs that mirror
// the Go standard library. An Instance represents an isolated virtual machine
// that can be interacted with using familiar os, os/exec, io, and net patterns.
package cc

import (
	"context"
	"io"
	"time"

	"github.com/tinyrange/cc/internal/api"
)

// -----------------------------------------------------------------------------
// Environment Variables
// -----------------------------------------------------------------------------

// CC_VERBOSE enables verbose logging. Set it to any value to enable.
// CC_DEBUG_FILE enables binary debug logging. Set it to a file name to enable.
// CC_TIMESLICE_FILE enables timeslice recording. Set it to a file name to enable.

// -----------------------------------------------------------------------------
// Type Aliases - These re-export types from internal/api
// -----------------------------------------------------------------------------

// File represents an open file in a guest filesystem. It mirrors *os.File.
type File = api.File

// FS provides filesystem operations on a guest. It mirrors functions from
// the os package.
type FS = api.FS

// Cmd represents a command ready to be run in the guest. It mirrors *os/exec.Cmd.
type Cmd = api.Cmd

// Exec provides process execution on a guest.
type Exec = api.Exec

// Net provides network operations on a guest.
type Net = api.Net

// Instance represents a running virtual machine.
type Instance = api.Instance

// InstanceSource is the source for creating a new Instance.
type InstanceSource = api.InstanceSource

// OCIClient pulls OCI images and converts them to InstanceSources.
type OCIClient = api.OCIClient

// Option configures an Instance.
type Option = api.Option

// OCIPullOption configures an OCI pull operation.
type OCIPullOption = api.OCIPullOption

// PullPolicy determines when images are fetched from the registry.
type PullPolicy = api.PullPolicy

// Error represents a cc operation error with structured information.
type Error = api.Error

// Pull policy constants.
const (
	PullIfNotPresent = api.PullIfNotPresent
	PullAlways       = api.PullAlways
	PullNever        = api.PullNever
)

// Common sentinel errors.
var (
	ErrNotRunning    = api.ErrNotRunning
	ErrAlreadyClosed = api.ErrAlreadyClosed
	ErrTimeout       = api.ErrTimeout

	// ErrHypervisorUnavailable indicates the hypervisor is not available.
	// This can happen when:
	// - Running on a platform without hypervisor support
	// - Missing permissions (e.g., macOS entitlements, Linux /dev/kvm access)
	// - Running in a VM or container without nested virtualization
	//
	// Use errors.Is(err, cc.ErrHypervisorUnavailable) to check and skip tests in CI.
	ErrHypervisorUnavailable = api.ErrHypervisorUnavailable
)

// -----------------------------------------------------------------------------
// Instance Options
// -----------------------------------------------------------------------------

// WithMemoryMB sets the memory size in megabytes.
func WithMemoryMB(size uint64) Option {
	return &memoryOption{sizeMB: size}
}

type memoryOption struct{ sizeMB uint64 }

func (*memoryOption) IsOption()        {}
func (o *memoryOption) SizeMB() uint64 { return o.sizeMB }

// WithTimeout sets a maximum lifetime for the instance. After this duration,
// the instance is forcibly terminated.
func WithTimeout(d time.Duration) Option {
	return &timeoutOption{d: d}
}

type timeoutOption struct{ d time.Duration }

func (*timeoutOption) IsOption()                 {}
func (o *timeoutOption) Duration() time.Duration { return o.d }

// WithUser sets the user (and optionally group) to run as inside the guest.
// Format: "user" or "user:group" or numeric "1000" or "1000:1000".
func WithUser(user string) Option {
	return &userOption{user: user}
}

type userOption struct{ user string }

func (*userOption) IsOption()      {}
func (o *userOption) User() string { return o.user }

// WithSkipEntrypoint tells the instance to initialize without running the
// container's entrypoint. This is useful when you want to run commands via
// inst.Command() without the entrypoint interfering.
func WithSkipEntrypoint() Option {
	return &skipEntrypointOption{}
}

type skipEntrypointOption struct{}

func (*skipEntrypointOption) IsOption()            {}
func (*skipEntrypointOption) SkipEntrypoint() bool { return true }

// WithInteractiveIO enables interactive terminal mode and sets the stdin/stdout.
// When enabled, stdin/stdout connect to virtio-console for live I/O instead
// of the default vsock-based capture mode. This is suitable for running
// interactive commands like shells.
//
// Example:
//
//	inst, err := cc.New(source,
//	    cc.WithInteractiveIO(os.Stdin, os.Stdout),
//	)
func WithInteractiveIO(stdin io.Reader, stdout io.Writer) Option {
	return &interactiveIOOption{stdin: stdin, stdout: stdout}
}

type interactiveIOOption struct {
	stdin  io.Reader
	stdout io.Writer
}

func (*interactiveIOOption) IsOption()                               {}
func (o *interactiveIOOption) InteractiveIO() (io.Reader, io.Writer) { return o.stdin, o.stdout }

// WithCPUs sets the number of virtual CPUs. Default is 1.
func WithCPUs(count int) Option {
	return &cpuOption{count: count}
}

type cpuOption struct{ count int }

func (*cpuOption) IsOption()   {}
func (o *cpuOption) CPUs() int { return o.count }

// WithDmesg enables kernel dmesg output (loglevel=7).
// When enabled, kernel messages are printed to the console which is useful
// for debugging boot issues and driver problems.
func WithDmesg() Option {
	return &dmesgOption{}
}

type dmesgOption struct{}

func (*dmesgOption) IsOption()   {}
func (*dmesgOption) Dmesg() bool { return true }

// WithPacketCapture enables packet capture (pcap format) to the given writer.
// The captured packets can be analyzed with tools like Wireshark or tcpdump.
//
// Example:
//
//	f, _ := os.Create("capture.pcap")
//	defer f.Close()
//	inst, err := cc.New(source, cc.WithPacketCapture(f))
func WithPacketCapture(w io.Writer) Option {
	return &packetCaptureOption{w: w}
}

type packetCaptureOption struct{ w io.Writer }

func (*packetCaptureOption) IsOption()                  {}
func (o *packetCaptureOption) PacketCapture() io.Writer { return o.w }

// WithGPU enables virtio-gpu and virtio-input devices for graphical output.
// When enabled, the instance's GPU() method returns a non-nil GPU interface.
// The caller must run the display loop on the main thread using Poll/Render/Swap.
//
// Example:
//
//	runtime.LockOSThread() // Required for windowing on macOS
//	inst, err := cc.New(source, cc.WithGPU())
//	if gpu := inst.GPU(); gpu != nil {
//	    win := createWindow() // Platform-specific
//	    gpu.SetWindow(win)
//	    for {
//	        if !gpu.Poll() { break }
//	        gpu.Render()
//	        gpu.Swap()
//	    }
//	}
func WithGPU() Option {
	return &gpuOption{}
}

type gpuOption struct{}

func (*gpuOption) IsOption() {}
func (*gpuOption) GPU() bool { return true }

// WithQEMUCacheDir sets the cache directory for QEMU emulation binaries.
// This is only used when running containers for a different architecture
// than the host (cross-architecture emulation).
func WithQEMUCacheDir(dir string) Option {
	return &qemuCacheDirOption{dir: dir}
}

type qemuCacheDirOption struct{ dir string }

func (*qemuCacheDirOption) IsOption()              {}
func (o *qemuCacheDirOption) QEMUCacheDir() string { return o.dir }

// GPU provides access to guest display and input devices.
type GPU = api.GPU

// MountConfig configures a host directory mount via virtio-fs.
type MountConfig struct {
	// Tag is the virtio-fs tag used to mount in the guest.
	// The guest mounts it with: mount -t virtiofs <tag> /mnt/path
	Tag string

	// HostPath is the host directory to expose. If empty, an empty writable
	// filesystem is created.
	HostPath string

	// ReadOnly makes the mount read-only if true.
	ReadOnly bool
}

// WithMount adds a virtio-fs mount to the guest.
// The guest can mount it with: mount -t virtiofs <tag> /mnt/path
//
// Example:
//
//	inst, err := cc.New(source,
//	    cc.WithMount(cc.MountConfig{Tag: "shared", HostPath: "/host/data"}),
//	)
//
// Then in guest: mount -t virtiofs shared /mnt/shared
func WithMount(config MountConfig) Option {
	return &mountOption{config: config}
}

type mountOption struct{ config MountConfig }

func (*mountOption) IsOption()            {}
func (o *mountOption) Mount() MountConfig { return o.config }

// -----------------------------------------------------------------------------
// OCI Pull Options
// -----------------------------------------------------------------------------

// WithPlatform specifies the platform to pull (e.g., "linux/amd64", "linux/arm64").
// Defaults to the host platform.
func WithPlatform(os, arch string) OCIPullOption {
	return &platformOption{os: os, arch: arch}
}

type platformOption struct{ os, arch string }

func (*platformOption) IsOCIPullOption()             {}
func (o *platformOption) Platform() (string, string) { return o.os, o.arch }

// WithAuth provides authentication credentials for private registries.
func WithAuth(username, password string) OCIPullOption {
	return &authOption{username: username, password: password}
}

type authOption struct{ username, password string }

func (*authOption) IsOCIPullOption()         {}
func (o *authOption) Auth() (string, string) { return o.username, o.password }

// WithPullPolicy sets when to pull from the registry vs use cache.
func WithPullPolicy(policy PullPolicy) OCIPullOption {
	return &pullPolicyOption{policy: policy}
}

type pullPolicyOption struct{ policy PullPolicy }

func (*pullPolicyOption) IsOCIPullOption()     {}
func (o *pullPolicyOption) Policy() PullPolicy { return o.policy }

// -----------------------------------------------------------------------------
// Constructors
// -----------------------------------------------------------------------------

// NewOCIClient creates a new OCI client for pulling images.
// Uses the default cache directory (platform-specific user config directory).
func NewOCIClient() (OCIClient, error) {
	return api.NewOCIClient()
}

// NewOCIClientWithCacheDir creates a new OCI client with a custom cache directory.
// If cacheDir is empty, the default cache directory is used.
func NewOCIClientWithCacheDir(cacheDir string) (OCIClient, error) {
	return api.NewOCIClientWithCacheDir(cacheDir)
}

// New creates and starts a new Instance from the given source.
//
// The instance is ready for use when New returns. The caller must call
// Close when finished to release resources.
func New(source InstanceSource, opts ...Option) (Instance, error) {
	return api.New(source, opts...)
}

// EnsureExecutableIsSigned checks if the current executable is signed with
// the hypervisor entitlement (macOS only). If not, it signs the executable
// and re-executes itself. This is useful for test binaries.
//
// On non-macOS platforms, this is a no-op.
//
// Call this at the start of TestMain(). If signing and re-exec succeed,
// this function does not return. If already signed, it returns nil.
//
// Example:
//
//	func TestMain(m *testing.M) {
//	    if err := cc.EnsureExecutableIsSigned(); err != nil {
//	        log.Fatalf("Failed to sign executable: %v", err)
//	    }
//	    os.Exit(m.Run())
//	}
func EnsureExecutableIsSigned() error {
	return api.EnsureExecutableIsSigned()
}

// -----------------------------------------------------------------------------
// Filesystem Snapshot Types
// -----------------------------------------------------------------------------

// FilesystemSnapshot represents a point-in-time snapshot of a filesystem.
// It can be used as an InstanceSource to create new instances with the
// captured filesystem state.
type FilesystemSnapshot = api.FilesystemSnapshot

// FilesystemSnapshotOption configures a snapshot operation.
type FilesystemSnapshotOption = api.FilesystemSnapshotOption

// FilesystemSnapshotFactory builds filesystem snapshots using Dockerfile-like
// operations. It supports caching intermediate layers to speed up repeated builds.
type FilesystemSnapshotFactory = api.FilesystemSnapshotFactory

// NewFilesystemSnapshotFactory creates a new factory for building filesystem snapshots.
// The factory uses the provided cacheDir to store and retrieve cached layers.
//
// Example:
//
//	snap, err := cc.NewFilesystemSnapshotFactory(client, cacheDir).
//	    From("alpine:3.19").
//	    Run("apk", "add", "--no-cache", "gcc", "musl-dev").
//	    Exclude("/var/cache/*", "/tmp/*").
//	    Build(ctx)
func NewFilesystemSnapshotFactory(client OCIClient, cacheDir string) *FilesystemSnapshotFactory {
	return api.NewFilesystemSnapshotFactory(client, cacheDir)
}

// -----------------------------------------------------------------------------
// Filesystem Snapshot Options
// -----------------------------------------------------------------------------

// WithSnapshotExcludes specifies path patterns to exclude from snapshots.
// Patterns use glob-style matching (*, ?, []).
func WithSnapshotExcludes(patterns ...string) FilesystemSnapshotOption {
	return &snapshotExcludesOption{patterns: patterns}
}

type snapshotExcludesOption struct{ patterns []string }

func (*snapshotExcludesOption) IsFilesystemSnapshotOption() {}

// Excludes returns the path patterns to exclude.
func (o *snapshotExcludesOption) Excludes() []string { return o.patterns }

// WithSnapshotCacheDir sets the directory for snapshot cache storage.
func WithSnapshotCacheDir(dir string) FilesystemSnapshotOption {
	return &snapshotCacheDirOption{dir: dir}
}

type snapshotCacheDirOption struct{ dir string }

func (*snapshotCacheDirOption) IsFilesystemSnapshotOption() {}

// CacheDir returns the cache directory.
func (o *snapshotCacheDirOption) CacheDir() string { return o.dir }

// -----------------------------------------------------------------------------
// Dockerfile Building
// -----------------------------------------------------------------------------

// DockerfileOption configures Dockerfile building.
type DockerfileOption = api.DockerfileOption

// DockerfileBuildContext provides access to files during COPY/ADD operations.
type DockerfileBuildContext = api.DockerfileBuildContext

// DockerfileRuntimeConfig holds metadata from CMD, ENTRYPOINT, USER, etc.
type DockerfileRuntimeConfig = api.DockerfileRuntimeConfig

// WithBuildContext sets the build context for COPY/ADD operations.
func WithBuildContext(ctx DockerfileBuildContext) DockerfileOption {
	return api.WithBuildContext(ctx)
}

// WithBuildContextDir creates a build context from a directory path.
func WithBuildContextDir(dir string) DockerfileOption {
	return api.WithBuildContextDir(dir)
}

// WithBuildArg sets a build argument (ARG instruction).
func WithBuildArg(key, value string) DockerfileOption {
	return api.WithBuildArg(key, value)
}

// WithDockerfileCacheDir sets the cache directory for filesystem snapshots.
func WithDockerfileCacheDir(dir string) DockerfileOption {
	return api.WithDockerfileCacheDir(dir)
}

// BuildDockerfileSource builds an InstanceSource from Dockerfile content.
// It parses the Dockerfile, converts instructions to filesystem operations,
// and executes them to produce a cached filesystem snapshot.
//
// Example:
//
//	dockerfile := []byte(`
//	    FROM alpine:3.19
//	    RUN apk add --no-cache curl
//	    COPY app /usr/local/bin/
//	    CMD ["app"]
//	`)
//	source, err := cc.BuildDockerfileSource(ctx, dockerfile, client,
//	    cc.WithBuildContextDir("./build"),
//	    cc.WithDockerfileCacheDir(cacheDir),
//	)
func BuildDockerfileSource(ctx context.Context, dockerfileContent []byte, client OCIClient, opts ...DockerfileOption) (FilesystemSnapshot, error) {
	return api.BuildDockerfileSource(ctx, dockerfileContent, client, opts...)
}

// BuildDockerfileRuntimeConfig parses a Dockerfile and returns the runtime
// configuration (CMD, ENTRYPOINT, USER, etc.) without building the image.
func BuildDockerfileRuntimeConfig(dockerfileContent []byte, opts ...DockerfileOption) (*DockerfileRuntimeConfig, error) {
	return api.BuildDockerfileRuntimeConfig(dockerfileContent, opts...)
}

// NewDirBuildContext creates a new directory-based build context.
func NewDirBuildContext(dir string) (DockerfileBuildContext, error) {
	return api.NewDirBuildContext(dir)
}

// -----------------------------------------------------------------------------
// Cache Directory
// -----------------------------------------------------------------------------

// CacheDir represents a cache directory configuration.
// It provides a unified way to configure cache directories for both
// OCIClient and Instance, ensuring they share the same cache location.
type CacheDir = api.CacheDir

// NewCacheDir creates a cache directory config.
// If path is empty, uses the platform-specific default cache directory.
func NewCacheDir(path string) (*CacheDir, error) {
	return api.NewCacheDir(path)
}

// NewOCIClientWithCache creates a new OCI client using the provided CacheDir.
// This ensures the OCI client uses the same cache location as other components.
func NewOCIClientWithCache(cache *CacheDir) (OCIClient, error) {
	return api.NewOCIClientWithCache(cache)
}

// WithCache sets the cache directory for the instance.
// This is used for QEMU emulation binaries and other cached resources.
func WithCache(cache *CacheDir) Option {
	return &cacheOption{cache: cache}
}

type cacheOption struct{ cache *CacheDir }

func (*cacheOption) IsOption()          {}
func (o *cacheOption) Cache() *CacheDir { return o.cache }
