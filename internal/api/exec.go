package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"

	"github.com/tinyrange/cc/internal/initx"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
)

// instanceCmd implements Cmd.
type instanceCmd struct {
	inst *instance
	ctx  context.Context

	name string
	args []string
	env  []string
	dir  string

	stdin  io.Reader
	stdout io.Writer
	stderr io.Writer

	mu       sync.Mutex
	started  bool
	finished bool
	done     chan struct{} // signaled when command completes
	exitCode int
	err      error
}

// Run starts the command and waits for it to complete.
func (c *instanceCmd) Run() error {
	if err := c.Start(); err != nil {
		return err
	}
	return c.Wait()
}

// Start starts the command but does not wait for it to complete.
func (c *instanceCmd) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.started {
		return &Error{Op: "exec", Path: c.name, Err: fmt.Errorf("already started")}
	}
	c.started = true
	c.done = make(chan struct{})

	// In the current architecture, commands are executed synchronously
	// via the command loop protocol. We'll execute immediately.
	go c.runCommand()

	return nil
}

// Wait waits for a started command to complete.
func (c *instanceCmd) Wait() error {
	// Wait for completion signal
	<-c.done

	c.mu.Lock()
	err := c.err
	c.mu.Unlock()
	return err
}

// runCommand executes the command in the guest.
func (c *instanceCmd) runCommand() {
	// Prepare command path and args
	path := c.name

	// Prepare environment
	env := c.env
	if len(env) == 0 {
		env = c.inst.env
	}

	// Build IR program using ForkExecWait helper
	errLabel := ir.Label("exec_error")
	errVar := ir.Var("exec_errno")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				initx.ForkExecWait(path, c.args, env, errLabel, errVar),
				ir.Return(errVar),
				ir.DeclareLabel(errLabel, ir.Block{
					ir.Printf("cc: exec error: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
					ir.Syscall(defs.SYS_EXIT, ir.Int64(1)),
				}),
			},
		},
	}

	// Run program via vsock
	err := c.inst.vm.Run(c.ctx, prog)

	c.mu.Lock()
	if err != nil {
		if exitErr, ok := err.(*initx.ExitError); ok {
			c.exitCode = exitErr.Code
			// Non-zero exit code is not necessarily an error
			if c.exitCode != 0 {
				c.err = &Error{Op: "exec", Path: c.name, Err: fmt.Errorf("exit status %d", c.exitCode)}
			}
		} else {
			c.err = &Error{Op: "exec", Path: c.name, Err: err}
		}
	}
	c.finished = true
	c.mu.Unlock()

	// Signal completion
	close(c.done)
}

// Output runs the command and returns its stdout.
func (c *instanceCmd) Output() ([]byte, error) {
	var stdout bytes.Buffer
	c.stdout = &stdout

	if err := c.Run(); err != nil {
		return nil, err
	}

	return stdout.Bytes(), nil
}

// CombinedOutput runs the command and returns stdout and stderr combined.
func (c *instanceCmd) CombinedOutput() ([]byte, error) {
	var combined bytes.Buffer
	c.stdout = &combined
	c.stderr = &combined

	if err := c.Run(); err != nil {
		return nil, err
	}

	return combined.Bytes(), nil
}

// StdinPipe returns a pipe connected to the command's stdin.
func (c *instanceCmd) StdinPipe() (io.WriteCloser, error) {
	return nil, &Error{Op: "exec", Path: c.name, Err: fmt.Errorf("stdin pipe not yet implemented")}
}

// StdoutPipe returns a pipe connected to the command's stdout.
func (c *instanceCmd) StdoutPipe() (io.ReadCloser, error) {
	return nil, &Error{Op: "exec", Path: c.name, Err: fmt.Errorf("stdout pipe not yet implemented")}
}

// StderrPipe returns a pipe connected to the command's stderr.
func (c *instanceCmd) StderrPipe() (io.ReadCloser, error) {
	return nil, &Error{Op: "exec", Path: c.name, Err: fmt.Errorf("stderr pipe not yet implemented")}
}

// SetStdin sets the command's stdin.
func (c *instanceCmd) SetStdin(r io.Reader) Cmd {
	c.stdin = r
	return c
}

// SetStdout sets the command's stdout.
func (c *instanceCmd) SetStdout(w io.Writer) Cmd {
	c.stdout = w
	return c
}

// SetStderr sets the command's stderr.
func (c *instanceCmd) SetStderr(w io.Writer) Cmd {
	c.stderr = w
	return c
}

// SetDir sets the working directory for the command.
func (c *instanceCmd) SetDir(dir string) Cmd {
	c.dir = dir
	return c
}

// SetEnv sets the environment variables for the command.
func (c *instanceCmd) SetEnv(env []string) Cmd {
	c.env = env
	return c
}

// ExitCode returns the exit code of the exited process.
func (c *instanceCmd) ExitCode() int {
	return c.exitCode
}

var _ Cmd = (*instanceCmd)(nil)
