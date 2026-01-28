package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	cc "github.com/tinyrange/cc"
	"github.com/tinyrange/cc/internal/initx"
	"golang.org/x/term"
)

func main() {
	// On macOS, pin to main thread for potential future window support
	if runtime.GOOS == "darwin" {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
	}

	if err := cc.EnsureExecutableIsSigned(); err != nil {
		fmt.Fprintf(os.Stderr, "cc2: ensure executable is signed: %v\n", err)
		os.Exit(1)
	}

	if err := run(); err != nil {
		var exitErr *initx.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		fmt.Fprintf(os.Stderr, "cc2: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	memoryMB := flag.Uint64("memory", 256, "Memory in MB")
	timeout := flag.Duration("timeout", 0, "Timeout for the container")
	workdir := flag.String("workdir", "", "Working directory inside the container")
	user := flag.String("user", "", "User to run as (uid or uid:gid)")
	envFlags := &stringSlice{}
	flag.Var(envFlags, "env", "Environment variables (KEY=value), can be specified multiple times")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] <image> [command] [args...]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Run a command inside an OCI container image in a virtual machine.\n\n")
		fmt.Fprintf(os.Stderr, "This is a simplified version of 'cc' using the public API.\n\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  %s alpine:latest /bin/sh -c 'echo hello'\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s ubuntu:22.04 ls -la\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -memory 512 -timeout 30s alpine:latest sleep 10\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		return fmt.Errorf("image reference required")
	}

	imageRef := args[0]
	var cmd []string
	if len(args) > 1 {
		cmd = args[1:]
	}

	// Create OCI client
	client, err := cc.NewOCIClient()
	if err != nil {
		return fmt.Errorf("create OCI client: %w", err)
	}

	// Create context with optional timeout
	ctx := context.Background()
	if *timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, *timeout)
		defer cancel()
	}

	// Pull the image
	source, err := client.Pull(ctx, imageRef)
	if err != nil {
		return fmt.Errorf("pull image: %w", err)
	}
	defer func() {
		if closer, ok := source.(io.Closer); ok {
			closer.Close()
		}
	}()

	// Detect if stdin is a terminal for interactive mode
	isTerminal := term.IsTerminal(int(os.Stdin.Fd()))

	// Build instance options
	var opts []cc.Option
	opts = append(opts, cc.WithMemoryMB(*memoryMB))
	opts = append(opts, cc.WithSkipEntrypoint())

	if len(envFlags.values) > 0 {
		opts = append(opts, cc.WithEnv(envFlags.values...))
	}

	if *workdir != "" {
		opts = append(opts, cc.WithWorkdir(*workdir))
	}

	if *user != "" {
		opts = append(opts, cc.WithUser(*user))
	}

	if *timeout > 0 {
		opts = append(opts, cc.WithTimeout(*timeout))
	}

	// Enable interactive mode when connected to a terminal
	if isTerminal {
		opts = append(opts, cc.WithInteractiveIO(os.Stdin, os.Stdout))
	}

	// Create and start the instance
	inst, err := cc.New(source, opts...)
	if err != nil {
		if errors.Is(err, cc.ErrHypervisorUnavailable) {
			return fmt.Errorf("hypervisor unavailable: %w", err)
		}
		return fmt.Errorf("create instance: %w", err)
	}
	defer inst.Close()

	// If no command specified, run /bin/sh interactively
	if len(cmd) == 0 {
		cmd = []string{"/bin/sh"}
	}

	// Put stdin into raw mode if it's a terminal (for interactive mode)
	var oldState *term.State
	if isTerminal {
		// Enable Windows VT processing if needed (no-op on Unix)
		restoreVT, err := enableVTProcessing()
		if err != nil {
			// Not fatal - some older Windows versions may not support VT processing
			fmt.Fprintf(os.Stderr, "warning: could not enable VT processing: %v\n", err)
		} else {
			defer restoreVT()
		}

		oldState, err = term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return fmt.Errorf("enable raw mode: %w", err)
		}
		defer term.Restore(int(os.Stdin.Fd()), oldState)
	}

	// Run the command
	var cmdObj cc.Cmd
	if isTerminal {
		// Interactive mode - stdin/stdout are handled by virtio-console
		cmdObj = inst.Command(cmd[0], cmd[1:]...)
	} else {
		// Capture mode - use vsock for stdin/stdout
		cmdObj = inst.Command(cmd[0], cmd[1:]...).
			SetStdin(os.Stdin).
			SetStdout(os.Stdout).
			SetStderr(os.Stderr)
	}

	if err := cmdObj.Run(); err != nil {
		// Restore terminal before exiting
		if oldState != nil {
			term.Restore(int(os.Stdin.Fd()), oldState)
		}

		// Check if it's an exit error with a non-zero code
		exitCode := cmdObj.ExitCode()
		if exitCode != 0 {
			return &initx.ExitError{Code: exitCode}
		}
		return fmt.Errorf("run command: %w", err)
	}

	return nil
}

// stringSlice implements flag.Value for collecting multiple string flags.
type stringSlice struct {
	values []string
}

func (s *stringSlice) String() string {
	return strings.Join(s.values, ", ")
}

func (s *stringSlice) Set(value string) error {
	s.values = append(s.values, value)
	return nil
}
