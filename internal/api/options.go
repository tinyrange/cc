package api

import (
	"io"
	"time"

	"github.com/tinyrange/cc/internal/hv"
)

// instanceConfig holds parsed instance options.
type instanceConfig struct {
	memoryMB       uint64
	cpus           int
	timeout        time.Duration
	user           string
	skipEntrypoint bool

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

	// QEMU emulation cache directory
	qemuCacheDir string

	// Cache directory configuration
	cache *CacheDir
}

// mountConfig holds parsed mount configuration.
type mountConfig struct {
	tag      string
	hostPath string
	readOnly bool
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
		case interface{ SkipEntrypoint() bool }:
			cfg.skipEntrypoint = o.SkipEntrypoint()
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
				ReadOnly bool
			}
		}:
			m := o.Mount()
			cfg.mounts = append(cfg.mounts, mountConfig{
				tag:      m.Tag,
				hostPath: m.HostPath,
				readOnly: m.ReadOnly,
			})
		case interface{ GPU() bool }:
			cfg.gpu = o.GPU()
		case interface{ QEMUCacheDir() string }:
			cfg.qemuCacheDir = o.QEMUCacheDir()
		case interface{ Cache() *CacheDir }:
			cfg.cache = o.Cache()
		}
	}

	return cfg
}

// pullConfig holds parsed OCI pull options.
type pullConfig struct {
	arch     hv.CpuArchitecture
	username string
	password string
	policy   PullPolicy
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
		}
	}

	return cfg
}
