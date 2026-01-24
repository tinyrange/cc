// Package cc provides virtualization primitives with APIs that mirror
// the Go standard library. An Instance represents an isolated virtual machine
// that can be interacted with using familiar os, os/exec, io, and net patterns.
package cc

import (
	"time"

	"github.com/tinyrange/cc/internal/api"
)

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

// WithEnv sets environment variables for the guest init process.
// Each entry should be in "KEY=value" format.
func WithEnv(env ...string) Option {
	return &envOption{env: env}
}

type envOption struct{ env []string }

func (*envOption) IsOption()        {}
func (o *envOption) Env() []string { return o.env }

// WithTimeout sets a maximum lifetime for the instance. After this duration,
// the instance is forcibly terminated.
func WithTimeout(d time.Duration) Option {
	return &timeoutOption{d: d}
}

type timeoutOption struct{ d time.Duration }

func (*timeoutOption) IsOption()                 {}
func (o *timeoutOption) Duration() time.Duration { return o.d }

// WithWorkdir sets the initial working directory for commands.
func WithWorkdir(path string) Option {
	return &workdirOption{path: path}
}

type workdirOption struct{ path string }

func (*workdirOption) IsOption()      {}
func (o *workdirOption) Path() string { return o.path }

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
func NewOCIClient() (OCIClient, error) {
	return api.NewOCIClient()
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
