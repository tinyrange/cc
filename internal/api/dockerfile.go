package api

import (
	"context"
	"fmt"
	"os"

	"github.com/tinyrange/cc/internal/dockerfile"
)

// DockerfileOption configures Dockerfile building.
type DockerfileOption interface {
	IsDockerfileOption()
}

// DockerfileBuildContext is an alias for dockerfile.BuildContext.
type DockerfileBuildContext = dockerfile.BuildContext

// DockerfileRuntimeConfig is an alias for dockerfile.RuntimeConfig.
type DockerfileRuntimeConfig = dockerfile.RuntimeConfig

// dockerfileConfig holds configuration for building a Dockerfile.
type dockerfileConfig struct {
	context   dockerfile.BuildContext
	buildArgs map[string]string
	cacheDir  string
}

// dockerfileOption implements DockerfileOption using a function.
type dockerfileOption struct {
	apply func(*dockerfileConfig)
}

func (o *dockerfileOption) IsDockerfileOption() {}

// WithBuildContext sets the build context for COPY/ADD operations.
func WithBuildContext(ctx dockerfile.BuildContext) DockerfileOption {
	return &dockerfileOption{
		apply: func(c *dockerfileConfig) {
			c.context = ctx
		},
	}
}

// WithBuildContextDir creates a build context from a directory path.
func WithBuildContextDir(dir string) DockerfileOption {
	return &dockerfileOption{
		apply: func(c *dockerfileConfig) {
			ctx, err := dockerfile.NewDirBuildContext(dir)
			if err == nil {
				c.context = ctx
			}
			// Error will be caught later when context is used
		},
	}
}

// WithBuildArg sets a build argument (ARG instruction).
func WithBuildArg(key, value string) DockerfileOption {
	return &dockerfileOption{
		apply: func(c *dockerfileConfig) {
			if c.buildArgs == nil {
				c.buildArgs = make(map[string]string)
			}
			c.buildArgs[key] = value
		},
	}
}

// WithDockerfileCacheDir sets the cache directory for filesystem snapshots.
func WithDockerfileCacheDir(dir string) DockerfileOption {
	return &dockerfileOption{
		apply: func(c *dockerfileConfig) {
			c.cacheDir = dir
		},
	}
}

// parseDockerfileOptions parses options into a config.
func parseDockerfileOptions(opts []DockerfileOption) *dockerfileConfig {
	cfg := &dockerfileConfig{}
	for _, opt := range opts {
		if o, ok := opt.(*dockerfileOption); ok {
			o.apply(cfg)
		}
	}
	return cfg
}

// dockerfileOpAdapter wraps a dockerfile.FSLayerOp to implement api.FSLayerOp.
type dockerfileOpAdapter struct {
	op dockerfile.FSLayerOp
}

func (a *dockerfileOpAdapter) CacheKey() string {
	return a.op.CacheKey()
}

func (a *dockerfileOpAdapter) Apply(ctx context.Context, inst Instance) error {
	// Adapt the Instance interface
	adapter := &instanceAdapter{inst: inst}
	return a.op.Apply(ctx, adapter)
}

// instanceAdapter adapts api.Instance to dockerfile.Instance.
type instanceAdapter struct {
	inst Instance
}

func (a *instanceAdapter) CommandContext(ctx context.Context, name string, args ...string) dockerfile.Cmd {
	return &cmdAdapter{cmd: a.inst.CommandContext(ctx, name, args...)}
}

func (a *instanceAdapter) WriteFile(name string, data []byte, perm os.FileMode) error {
	return a.inst.WriteFile(name, data, perm)
}

// cmdAdapter adapts api.Cmd to dockerfile.Cmd.
type cmdAdapter struct {
	cmd Cmd
}

func (a *cmdAdapter) Run() error {
	return a.cmd.Run()
}

func (a *cmdAdapter) SetEnv(env []string) dockerfile.Cmd {
	return &cmdAdapter{cmd: a.cmd.SetEnv(env)}
}

func (a *cmdAdapter) SetDir(dir string) dockerfile.Cmd {
	return &cmdAdapter{cmd: a.cmd.SetDir(dir)}
}

// BuildDockerfileSource builds an InstanceSource from Dockerfile content.
// It parses the Dockerfile, converts instructions to filesystem operations,
// and executes them to produce a cached filesystem snapshot.
func BuildDockerfileSource(ctx context.Context, dockerfileContent []byte, client OCIClient, opts ...DockerfileOption) (FilesystemSnapshot, error) {
	// Parse options
	cfg := parseDockerfileOptions(opts)
	if cfg.cacheDir == "" {
		return nil, fmt.Errorf("cache directory required: use WithDockerfileCacheDir")
	}

	// Parse Dockerfile
	df, err := dockerfile.Parse(dockerfileContent)
	if err != nil {
		return nil, fmt.Errorf("parse dockerfile: %w", err)
	}

	// Build operations
	builder := dockerfile.NewBuilder(df)
	if cfg.context != nil {
		builder = builder.WithContext(cfg.context)
	}
	for k, v := range cfg.buildArgs {
		builder = builder.WithBuildArg(k, v)
	}

	result, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("build dockerfile: %w", err)
	}

	// Create factory and execute
	factory := NewFilesystemSnapshotFactory(client, cfg.cacheDir).From(result.ImageRef)

	// Set initial environment and workdir
	if len(result.Env) > 0 {
		factory = factory.Env(result.Env...)
	}
	if result.WorkDir != "" {
		factory = factory.WorkDir(result.WorkDir)
	}

	// Add operations wrapped in adapters
	for _, op := range result.Ops {
		factory.ops = append(factory.ops, &dockerfileOpAdapter{op: op})
	}

	// Build and return
	return factory.Build(ctx)
}

// BuildDockerfileRuntimeConfig parses a Dockerfile and returns the runtime configuration
// (CMD, ENTRYPOINT, USER, etc.) without building the image.
func BuildDockerfileRuntimeConfig(dockerfileContent []byte, opts ...DockerfileOption) (*dockerfile.RuntimeConfig, error) {
	// Parse options
	cfg := parseDockerfileOptions(opts)

	// Parse Dockerfile
	df, err := dockerfile.Parse(dockerfileContent)
	if err != nil {
		return nil, fmt.Errorf("parse dockerfile: %w", err)
	}

	// Build (just to extract config)
	builder := dockerfile.NewBuilder(df)
	if cfg.context != nil {
		builder = builder.WithContext(cfg.context)
	}
	for k, v := range cfg.buildArgs {
		builder = builder.WithBuildArg(k, v)
	}

	result, err := builder.Build()
	if err != nil {
		return nil, fmt.Errorf("build dockerfile: %w", err)
	}

	return &result.RuntimeConfig, nil
}

// NewDirBuildContext creates a new directory-based build context.
func NewDirBuildContext(dir string) (*dockerfile.DirBuildContext, error) {
	return dockerfile.NewDirBuildContext(dir)
}
