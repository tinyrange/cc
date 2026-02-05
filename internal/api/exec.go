package api

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"sync"

	"github.com/tinyrange/cc/internal/initx"
	"github.com/tinyrange/cc/internal/ir"
	"github.com/tinyrange/cc/internal/linux/defs"
)

const defaultPathEnv = "/bin:/usr/bin"

func extractPathFromEnv(env []string) string {
	for _, entry := range env {
		if after, ok := strings.CutPrefix(entry, "PATH="); ok {
			return after
		}
	}
	return defaultPathEnv
}

// lookupPathInBackend walks a path through the fsBackend, following symlinks,
// and returns the final node ID and attributes.
// Returns errno != 0 if the path doesn't exist.
func (c *instanceCmd) lookupPathInBackend(p string) (nodeID uint64, mode uint32, errno int32) {
	const maxSymlinkDepth = 40

	p = path.Clean(p)
	if p == "/" || p == "" {
		attr, err := c.inst.fsBackend.GetAttr(1)
		return 1, attr.Mode, err
	}

	readlinkBackend, hasReadlink := c.inst.fsBackend.(interface {
		Readlink(nodeID uint64) (target string, errno int32)
	})

	// Remove leading slash and split
	p = strings.TrimPrefix(p, "/")
	parts := strings.Split(p, "/")

	currentID := uint64(1) // root node ID
	symlinkDepth := 0

	for i := 0; i < len(parts); i++ {
		part := parts[i]
		if part == "" {
			continue
		}
		childID, attr, err := c.inst.fsBackend.Lookup(currentID, part)
		if err != 0 {
			return 0, 0, err
		}

		// Check if this is a symlink and follow it
		if hasReadlink && (attr.Mode&0170000) == 0120000 {
			symlinkDepth++
			if symlinkDepth > maxSymlinkDepth {
				return 0, 0, -int32(40) // ELOOP
			}

			target, err := readlinkBackend.Readlink(childID)
			if err != 0 {
				return 0, 0, err
			}

			// Construct new path
			var newPath string
			if strings.HasPrefix(target, "/") {
				// Absolute symlink
				newPath = target
			} else {
				// Relative symlink - resolve from current directory
				currentParts := parts[:i]
				newPath = "/" + strings.Join(currentParts, "/") + "/" + target
			}

			// Append remaining parts
			if i+1 < len(parts) {
				newPath = newPath + "/" + strings.Join(parts[i+1:], "/")
			}

			// Restart resolution with new path
			newPath = path.Clean(newPath)
			currentID = 1
			parts = strings.Split(strings.TrimPrefix(newPath, "/"), "/")
			i = -1 // Will be incremented to 0
			continue
		}

		currentID = childID
		mode = attr.Mode
	}

	return currentID, mode, 0
}

func (c *instanceCmd) lookPath(file string) (string, error) {
	if file == "" {
		return "", fmt.Errorf("executable name is empty")
	}

	pathEnv := extractPathFromEnv(c.env)
	if pathEnv == "" {
		pathEnv = defaultPathEnv
	}

	workDir := c.dir
	if workDir == "" {
		workDir = "/"
	}

	for dir := range strings.SplitSeq(pathEnv, ":") {
		switch {
		case dir == "":
			dir = workDir
		case !path.IsAbs(dir):
			dir = path.Join(workDir, dir)
		}

		candidate := path.Join(dir, file)

		// Look up the file in the live filesystem
		_, mode, errno := c.lookupPathInBackend(candidate)
		if errno != 0 {
			continue
		}

		// Check if it's a regular file with execute permission
		// S_IFMT = 0170000, S_IFREG = 0100000
		isRegular := (mode & 0170000) == 0100000
		isExecutable := (mode & 0111) != 0

		if !isRegular || !isExecutable {
			continue
		}

		return candidate, nil
	}

	return "", fmt.Errorf("executable %q not found in PATH", file)
}

// instanceCmd implements Cmd.
type instanceCmd struct {
	inst *instance
	ctx  context.Context

	name string
	args []string
	env  []string
	dir  string
	user string

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
	// Check if instance is in interactive mode
	if c.inst.interactive {
		c.runInteractiveCommand()
		return
	}

	// Prepare command path and args
	cmdPath := c.name
	args := c.args

	// Resolve command path if it doesn't contain "/"
	// Search the live filesystem (fsBackend) for the executable in PATH.
	if !strings.Contains(cmdPath, "/") {
		resolved, err := c.lookPath(cmdPath)
		if err != nil {
			c.mu.Lock()
			c.err = &Error{Op: "exec", Path: c.name, Err: err}
			c.finished = true
			c.mu.Unlock()
			close(c.done)
			return
		}
		cmdPath = resolved
	}

	// If a user is specified, wrap command with su
	if c.user != "" {
		// Build the command string for su -c
		cmdStr := cmdPath
		for _, arg := range args {
			cmdStr += " " + shellQuote(arg)
		}
		cmdPath = "/bin/su"
		args = []string{"-", c.user, "-c", cmdStr}
	}

	// Check if stdin is set (we'll use streaming mode if so)
	hasStdin := c.stdin != nil

	// Use the command's environment (already merged in CommandContext)
	env := c.env

	// Build IR program using ForkExecWaitWithCwd helper (supports working directory)
	errLabel := ir.Label("exec_error")
	execErrLabel := ir.Label("exec_child_error")
	errVar := ir.Var("exec_errno")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				initx.ForkExecWaitWithCwd(cmdPath, args, env, c.dir, errLabel, execErrLabel, errVar),
				ir.Return(errVar),
				ir.DeclareLabel(errLabel, ir.Block{
					ir.Printf("cc: exec error: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
					ir.Return(errVar),
				}),
				ir.DeclareLabel(execErrLabel, ir.Block{
					ir.Syscall(defs.SYS_EXIT, ir.Int64(1)),
				}),
			},
		},
	}

	// Determine capture flags based on whether stdout/stderr writers are set
	var captureFlags uint32 = initx.CaptureFlagNone
	if c.stdout != nil {
		captureFlags |= initx.CaptureFlagStdout
	}
	if c.stderr != nil {
		// If stdout and stderr point to the same writer, use combined mode
		if c.stdout == c.stderr {
			captureFlags |= initx.CaptureFlagCombine
		} else {
			captureFlags |= initx.CaptureFlagStderr
		}
	}

	// Run program via vsock with capture and optional streaming stdin
	var result *initx.ProgramResult
	var err error

	// Set stdin flag if stdin was provided (even if empty, to signal EOF)
	if hasStdin {
		captureFlags |= initx.CaptureFlagStdin
	}

	// Use streaming stdin when stdin is provided, otherwise use regular capture
	if hasStdin || captureFlags != initx.CaptureFlagNone {
		// RunWithStreamingStdin handles both streaming stdin and capture
		// If stdin is nil, it uses StdinModeNone; otherwise StdinModeStreaming
		result, err = c.inst.vm.RunWithStreamingStdin(c.ctx, prog, captureFlags, c.stdin)
	} else {
		err = c.inst.vm.Run(c.ctx, prog)
	}

	c.mu.Lock()
	if err != nil {
		if exitErr, ok := err.(*initx.ExitError); ok {
			c.exitCode = exitErr.Code
			// Non-zero exit code is not necessarily an error
			if c.exitCode != 0 {
				if c.exitCode < 0 {
					c.err = &Error{Op: "exec", Path: c.name, Err: fmt.Errorf("errno=0x%x", -c.exitCode)}
				} else {
					c.err = &Error{Op: "exec", Path: c.name, Err: fmt.Errorf("exit status %d", c.exitCode)}
				}
			}
		} else {
			c.err = &Error{Op: "exec", Path: c.name, Err: err}
		}
	} else if result != nil {
		// Process capture result
		c.exitCode = int(result.ExitCode)
		if c.exitCode != 0 {
			if c.exitCode < 0 {
				c.err = &Error{Op: "exec", Path: c.name, Err: fmt.Errorf("errno=0x%x", -c.exitCode)}
			} else {
				c.err = &Error{Op: "exec", Path: c.name, Err: fmt.Errorf("exit status %d", c.exitCode)}
			}
		}
	}
	c.finished = true
	c.mu.Unlock()

	// Write captured output to writers (outside the lock)
	if result != nil {
		if c.stdout != nil && len(result.Stdout) > 0 {
			c.stdout.Write(result.Stdout)
		}
		if c.stderr != nil && c.stderr != c.stdout && len(result.Stderr) > 0 {
			c.stderr.Write(result.Stderr)
		}
	}

	// Signal completion
	close(c.done)
}

// runInteractiveCommand executes a command in interactive mode using virtio-console.
// In this mode, stdin/stdout are already connected to the console via WithStdin/WithConsoleOutput,
// so we just run the command and let the console handle I/O directly.
func (c *instanceCmd) runInteractiveCommand() {
	// Prepare command path and args
	cmdPath := c.name
	args := c.args

	// Resolve command path if it doesn't contain "/"
	if !strings.Contains(cmdPath, "/") {
		resolved, err := c.lookPath(cmdPath)
		if err != nil {
			c.mu.Lock()
			c.err = &Error{Op: "exec", Path: c.name, Err: err}
			c.finished = true
			c.mu.Unlock()
			close(c.done)
			return
		}
		cmdPath = resolved
	}

	// If a user is specified, wrap command with su
	if c.user != "" {
		// Build the command string for su -c
		cmdStr := cmdPath
		for _, arg := range args {
			cmdStr += " " + shellQuote(arg)
		}
		cmdPath = "/bin/su"
		args = []string{"-", c.user, "-c", cmdStr}
	}

	// Use the command's environment (already merged in CommandContext)
	env := c.env

	// Build IR program using ForkExecWaitWithCwd helper (supports working directory)
	errLabel := ir.Label("exec_error")
	execErrLabel := ir.Label("exec_child_error")
	errVar := ir.Var("exec_errno")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				initx.ForkExecWaitWithCwd(cmdPath, args, env, c.dir, errLabel, execErrLabel, errVar),
				ir.Return(errVar),
				ir.DeclareLabel(errLabel, ir.Block{
					ir.Printf("cc: exec error: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
					ir.Return(errVar),
				}),
				ir.DeclareLabel(execErrLabel, ir.Block{
					ir.Syscall(defs.SYS_EXIT, ir.Int64(1)),
				}),
			},
		},
	}

	// Start stdin forwarding before running the command so the user can type
	c.inst.vm.StartStdinForwarding()

	// Run the program directly - output goes to virtio-console
	err := c.inst.vm.Run(c.ctx, prog)

	c.mu.Lock()
	if err != nil {
		if exitErr, ok := err.(*initx.ExitError); ok {
			c.exitCode = exitErr.Code
			if c.exitCode != 0 {
				if c.exitCode < 0 {
					c.err = &Error{Op: "exec", Path: c.name, Err: fmt.Errorf("errno=0x%x", -c.exitCode)}
				} else {
					c.err = &Error{Op: "exec", Path: c.name, Err: fmt.Errorf("exit status %d", c.exitCode)}
				}
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
// This method is not supported in interactive mode; use Run() instead.
func (c *instanceCmd) Output() ([]byte, error) {
	if c.inst.interactive {
		return nil, &Error{Op: "exec", Path: c.name, Err: fmt.Errorf("Output() is not supported in interactive mode; use Run() instead")}
	}

	var stdout bytes.Buffer
	c.stdout = &stdout

	if err := c.Run(); err != nil {
		return nil, err
	}

	return stdout.Bytes(), nil
}

// CombinedOutput runs the command and returns stdout and stderr combined.
// This method is not supported in interactive mode; use Run() instead.
func (c *instanceCmd) CombinedOutput() ([]byte, error) {
	if c.inst.interactive {
		return nil, &Error{Op: "exec", Path: c.name, Err: fmt.Errorf("CombinedOutput() is not supported in interactive mode; use Run() instead")}
	}

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

// SetUser sets the user to run the command as.
func (c *instanceCmd) SetUser(user string) Cmd {
	c.user = user
	return c
}

// SetEnv sets a single environment variable (like os.Setenv).
func (c *instanceCmd) SetEnv(key, value string) Cmd {
	// Look for existing key and update it
	prefix := key + "="
	for i, entry := range c.env {
		if strings.HasPrefix(entry, prefix) {
			c.env[i] = key + "=" + value
			return c
		}
	}
	// Not found, append new entry
	c.env = append(c.env, key+"="+value)
	return c
}

// GetEnv returns the value of an environment variable (like os.Getenv).
func (c *instanceCmd) GetEnv(key string) string {
	prefix := key + "="
	for _, entry := range c.env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}

// Environ returns a copy of the command's environment variables.
func (c *instanceCmd) Environ() []string {
	result := make([]string, len(c.env))
	copy(result, c.env)
	return result
}

// ExitCode returns the exit code of the exited process.
func (c *instanceCmd) ExitCode() int {
	return c.exitCode
}

var _ Cmd = (*instanceCmd)(nil)

// execCommand executes a command in "exec mode" - the command replaces init as PID 1.
// Unlike Command().Run(), there is no fork - the command directly replaces the init process.
// When the command exits, the VM terminates.
func (inst *instance) execCommand(ctx context.Context, name string, args []string) error {
	// Resolve command path
	cmdPath := name
	if !strings.Contains(cmdPath, "/") {
		// Create a temporary cmd to use lookPath
		tmpCmd := &instanceCmd{
			inst: inst,
			env:  inst.imageConfig.Env,
			dir:  inst.imageConfig.WorkingDir,
		}
		resolved, err := tmpCmd.lookPath(cmdPath)
		if err != nil {
			return &Error{Op: "exec", Path: name, Err: err}
		}
		cmdPath = resolved
	}

	// Merge environment from image config
	env := append([]string{}, inst.imageConfig.Env...)

	// Build IR program using ExecOnly helper (no fork, just exec)
	errLabel := ir.Label("exec_error")
	errVar := ir.Var("exec_errno")

	prog := &ir.Program{
		Entrypoint: "main",
		Methods: map[string]ir.Method{
			"main": {
				initx.ExecOnly(cmdPath, args, env, errLabel, errVar),
				// If we get here, execve failed
				ir.DeclareLabel(errLabel, ir.Block{
					ir.Printf("cc: exec error: errno=0x%x\n", ir.Op(ir.OpSub, ir.Int64(0), errVar)),
					ir.Return(errVar),
				}),
			},
		},
	}

	// Run the program - this will replace init with the specified command
	err := inst.vm.Run(ctx, prog)
	if err != nil {
		if exitErr, ok := err.(*initx.ExitError); ok {
			if exitErr.Code != 0 {
				if exitErr.Code < 0 {
					return &Error{Op: "exec", Path: name, Err: fmt.Errorf("errno=0x%x", -exitErr.Code)}
				}
				return &Error{Op: "exec", Path: name, Err: fmt.Errorf("exit status %d", exitErr.Code)}
			}
			return nil
		}
		return &Error{Op: "exec", Path: name, Err: err}
	}
	return nil
}

// shellQuote quotes a string for safe use in a shell command.
func shellQuote(s string) string {
	// If the string is safe (alphanumeric, underscore, dash, dot, slash), return as-is
	safe := true
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') ||
			c == '_' || c == '-' || c == '.' || c == '/') {
			safe = false
			break
		}
	}
	if safe && len(s) > 0 {
		return s
	}
	// Use single quotes and escape any single quotes in the string
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
