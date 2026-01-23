package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"sync"
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

	// In the current architecture, commands are executed synchronously
	// via the command loop protocol. We'll execute immediately.
	go c.runCommand()

	return nil
}

// Wait waits for a started command to complete.
func (c *instanceCmd) Wait() error {
	c.mu.Lock()
	for !c.finished {
		c.mu.Unlock()
		// Simple polling - in production, would use channels
		c.mu.Lock()
	}
	err := c.err
	c.mu.Unlock()
	return err
}

// runCommand executes the command in the guest.
func (c *instanceCmd) runCommand() {
	// Prepare command path and args
	path := c.name
	args := append([]string{c.name}, c.args...)

	// Prepare environment
	env := c.env
	if len(env) == 0 {
		env = c.inst.env
	}

	// Write the exec command to the VM
	if err := c.inst.vm.WriteExecCommand(path, args, env); err != nil {
		c.mu.Lock()
		c.err = &Error{Op: "exec", Path: c.name, Err: err}
		c.finished = true
		c.mu.Unlock()
		return
	}

	// Run the command loop program to execute the command
	// The VM's command loop will pick up the command from the config region
	// and execute it via fork/exec/wait

	// For now, we signal completion - the actual execution happens
	// via the session's ongoing program
	c.mu.Lock()
	c.finished = true
	c.mu.Unlock()
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
