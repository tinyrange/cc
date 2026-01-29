package shared

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	cc "github.com/tinyrange/cc"
)

// SandboxPool manages a pool of pre-warmed sandbox instances.
type SandboxPool struct {
	mu        sync.Mutex
	instances chan cc.Instance
	source    cc.InstanceSource
	opts      []cc.Option
	size      int
	warming   bool
}

// NewSandboxPool creates a new sandbox pool.
func NewSandboxPool(source cc.InstanceSource, size int, opts ...cc.Option) *SandboxPool {
	return &SandboxPool{
		instances: make(chan cc.Instance, size),
		source:    source,
		opts:      opts,
		size:      size,
	}
}

// Warm pre-warms the pool with instances.
func (p *SandboxPool) Warm(ctx context.Context) error {
	p.mu.Lock()
	if p.warming {
		p.mu.Unlock()
		return nil
	}
	p.warming = true
	p.mu.Unlock()

	for i := 0; i < p.size; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			instance, err := cc.New(p.source, p.opts...)
			if err != nil {
				return fmt.Errorf("warming pool: %w", err)
			}
			select {
			case p.instances <- instance:
			default:
				instance.Close()
			}
		}
	}
	return nil
}

// Acquire gets an instance from the pool or creates a new one.
func (p *SandboxPool) Acquire(ctx context.Context) (cc.Instance, error) {
	select {
	case instance := <-p.instances:
		return instance, nil
	default:
		return cc.New(p.source, p.opts...)
	}
}

// Release returns an instance to the pool or closes it.
func (p *SandboxPool) Release(instance cc.Instance) {
	select {
	case p.instances <- instance:
	default:
		instance.Close()
	}
}

// Close closes all instances in the pool.
func (p *SandboxPool) Close() error {
	close(p.instances)
	for instance := range p.instances {
		instance.Close()
	}
	return nil
}

// RunResult contains the result of running a command in a sandbox.
type RunResult struct {
	Stdout   string        `json:"stdout"`
	Stderr   string        `json:"stderr"`
	ExitCode int           `json:"exit_code"`
	Duration time.Duration `json:"duration_ms"`
	TimedOut bool          `json:"timed_out,omitempty"`
	Error    string        `json:"error,omitempty"`
}

// RunCommand runs a command in an instance and captures output.
func RunCommand(ctx context.Context, instance cc.Instance, name string, args ...string) RunResult {
	start := time.Now()
	result := RunResult{}

	cmd := instance.CommandContext(ctx, name, args...)

	var stdout, stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)

	err := cmd.Run()
	result.Duration = time.Since(start)
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.ExitCode = cmd.ExitCode()

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.TimedOut = true
		}
		result.Error = err.Error()
	}

	return result
}

// RunCommandWithEnv runs a command with environment variables.
// Each env entry should be in "KEY=value" format.
func RunCommandWithEnv(ctx context.Context, instance cc.Instance, env []string, name string, args ...string) RunResult {
	start := time.Now()
	result := RunResult{}

	cmd := instance.CommandContext(ctx, name, args...)

	// Apply environment variables
	for _, e := range env {
		if key, value, ok := strings.Cut(e, "="); ok {
			cmd.SetEnv(key, value)
		}
	}

	var stdout, stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)

	err := cmd.Run()
	result.Duration = time.Since(start)
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.ExitCode = cmd.ExitCode()

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.TimedOut = true
		}
		result.Error = err.Error()
	}

	return result
}

// RunCommandWithStdin runs a command with stdin input.
func RunCommandWithStdin(ctx context.Context, instance cc.Instance, stdin io.Reader, name string, args ...string) RunResult {
	start := time.Now()
	result := RunResult{}

	cmd := instance.CommandContext(ctx, name, args...)
	cmd.SetStdin(stdin)

	var stdout, stderr bytes.Buffer
	cmd.SetStdout(&stdout)
	cmd.SetStderr(&stderr)

	err := cmd.Run()
	result.Duration = time.Since(start)
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()
	result.ExitCode = cmd.ExitCode()

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.TimedOut = true
		}
		result.Error = err.Error()
	}

	return result
}

// WriteFiles writes multiple files to an instance.
func WriteFiles(ctx context.Context, instance cc.Instance, files map[string][]byte) error {
	fs := instance.WithContext(ctx)
	for path, content := range files {
		if err := fs.WriteFile(path, content, 0644); err != nil {
			return fmt.Errorf("writing %s: %w", path, err)
		}
	}
	return nil
}

// ReadFile reads a file from an instance.
func ReadFile(ctx context.Context, instance cc.Instance, path string) ([]byte, error) {
	fs := instance.WithContext(ctx)
	return fs.ReadFile(path)
}
