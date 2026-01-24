package api

import (
	"time"

	"github.com/tinyrange/cc/internal/hv"
)

// instanceConfig holds parsed instance options.
type instanceConfig struct {
	memoryMB       uint64
	cpus           int
	env            []string
	timeout        time.Duration
	workdir        string
	user           string
	skipEntrypoint bool
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
		case interface{ Env() []string }:
			cfg.env = o.Env()
		case interface{ Duration() time.Duration }:
			cfg.timeout = o.Duration()
		case interface{ Path() string }:
			cfg.workdir = o.Path()
		case interface{ User() string }:
			cfg.user = o.User()
		case interface{ SkipEntrypoint() bool }:
			cfg.skipEntrypoint = o.SkipEntrypoint()
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
