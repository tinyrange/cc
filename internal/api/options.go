package api

import (
	"io"
	"time"

	"github.com/tinyrange/cc/internal/hv"
)

// instanceConfig holds parsed instance options.
type instanceConfig struct {
	memoryMB uint64
	cpus     int
	timeout  time.Duration
	user     string

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
	gpu bool

	// Cache directory configuration
	cache CacheDir

	// Boot snapshot configuration
	bootSnapshotEnabled  bool // true = use snapshots, false = disabled
	bootSnapshotExplicit bool // true if user explicitly set the option
}

// mountConfig holds parsed mount configuration.
type mountConfig struct {
	tag      string
	hostPath string
	writable bool
}

// defaultInstanceConfig returns a config with default values.
func defaultInstanceConfig() instanceConfig {
	return instanceConfig{
		memoryMB: 256,
		cpus:     1,
	}
}

// parseInstanceOptions extracts configuration from Option slice.
func parseInstanceOptions(opts []Option) instanceConfig {
	cfg := defaultInstanceConfig()

	for _, opt := range opts {
		switch o := opt.(type) {
		case interface{ SizeMB() uint64 }:
			cfg.memoryMB = o.SizeMB()
		case interface{ CPUs() int }:
			cfg.cpus = o.CPUs()
		case interface{ Duration() time.Duration }:
			cfg.timeout = o.Duration()
		case interface{ User() string }:
			cfg.user = o.User()
		case interface{ InteractiveIO() (io.Reader, io.Writer) }:
			cfg.interactive = true
			cfg.interactiveStdin, cfg.interactiveStdout = o.InteractiveIO()
		case interface{ Dmesg() bool }:
			cfg.dmesg = o.Dmesg()
		case interface{ PacketCapture() io.Writer }:
			cfg.packetCapture = o.PacketCapture()
		case interface {
			Mount() struct {
				Tag      string
				HostPath string
				Writable bool
			}
		}:
			m := o.Mount()
			cfg.mounts = append(cfg.mounts, mountConfig{
				tag:      m.Tag,
				hostPath: m.HostPath,
				writable: m.Writable,
			})
		case interface{ GPU() bool }:
			cfg.gpu = o.GPU()
		case interface{ Cache() CacheDir }:
			cfg.cache = o.Cache()
		case interface{ BootSnapshot() bool }:
			cfg.bootSnapshotEnabled = o.BootSnapshot()
			cfg.bootSnapshotExplicit = true
		}
	}

	// Boot snapshots are disabled by default until the vsock reconnection
	// after snapshot restore is properly implemented.
	// Users can explicitly enable with WithBootSnapshot().
	// TODO: Enable by default once snapshot restore properly handles vsock

	return cfg
}

// DownloadProgress represents the current state of a download.
// This mirrors the oci.DownloadProgress type for the public API.
type DownloadProgress struct {
	Current  int64  // Bytes downloaded so far
	Total    int64  // Total bytes to download (-1 if unknown)
	Filename string // Name/path being downloaded

	// Blob count tracking
	BlobIndex int // Index of current blob (0-based)
	BlobCount int // Total number of blobs to download

	// Speed and ETA tracking
	BytesPerSecond float64       // Current download speed in bytes per second
	ETA            time.Duration // Estimated time remaining (-1 if unknown)
}

// ProgressCallback is called periodically during downloads.
type ProgressCallback func(progress DownloadProgress)

// pullConfig holds parsed OCI pull options.
type pullConfig struct {
	arch             hv.CpuArchitecture
	username         string
	password         string
	policy           PullPolicy
	progressCallback ProgressCallback
}

// defaultPullConfig returns a config with default values.
func defaultPullConfig() pullConfig {
	return pullConfig{
		arch:   hv.ArchitectureNative,
		policy: PullIfNotPresent,
	}
}

// parsePullOptions extracts configuration from OCIPullOption slice.
func parsePullOptions(opts []OCIPullOption) pullConfig {
	cfg := defaultPullConfig()

	for _, opt := range opts {
		switch o := opt.(type) {
		case interface{ Platform() (string, string) }:
			os, arch := o.Platform()
			if os == "linux" {
				switch arch {
				case "amd64":
					cfg.arch = hv.ArchitectureX86_64
				case "arm64":
					cfg.arch = hv.ArchitectureARM64
				}
			}
		case interface{ Auth() (string, string) }:
			cfg.username, cfg.password = o.Auth()
		case interface{ Policy() PullPolicy }:
			cfg.policy = o.Policy()
		case interface{ ProgressCallback() ProgressCallback }:
			cfg.progressCallback = o.ProgressCallback()
		}
	}

	return cfg
}
