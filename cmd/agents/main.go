package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"runtime"
	"sort"

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
		fmt.Fprintf(os.Stderr, "agents: ensure executable is signed: %v\n", err)
		os.Exit(1)
	}

	if err := run(); err != nil {
		var exitErr *initx.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		fmt.Fprintf(os.Stderr, "agents: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	memoryFlag := flag.Uint64("memory", 0, "Memory in MB (default: agent-specific)")
	cacheDir := flag.String("cache-dir", "", "OCI image cache directory (default: platform-specific)")
	termFlag := flag.Bool("term", false, "Open interactive terminal instead of running agent")
	listFlag := flag.Bool("list", false, "List available agents")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s <agent> [flags] [args...]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Start a VM with the specified agent installed.\n\n")
		fmt.Fprintf(os.Stderr, "Available agents:\n")
		for _, a := range listAgentsSorted() {
			fmt.Fprintf(os.Stderr, "  %-12s %s\n", a.Name, a.Description)
		}
		fmt.Fprintf(os.Stderr, "\nExamples:\n")
		fmt.Fprintf(os.Stderr, "  %s claude              Run Claude Code agent\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s claude --help       Pass --help to Claude Code\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s claude -term        Open shell in Claude Code VM\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s -list               List available agents\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	// Handle -list
	if *listFlag {
		for _, a := range listAgentsSorted() {
			fmt.Printf("%-12s %s\n", a.Name, a.Description)
		}
		return nil
	}

	// Get agent name and extra args
	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		return fmt.Errorf("agent name required")
	}
	agentName := args[0]
	extraArgs := args[1:] // Arguments to pass to the command

	// Look up agent
	agent, ok := getAgent(agentName)
	if !ok {
		return fmt.Errorf("unknown agent: %s (use -list to see available agents)", agentName)
	}

	// Apply defaults from agent config
	memoryMB := agent.MemoryMB
	if *memoryFlag != 0 {
		memoryMB = *memoryFlag
	}

	// Create shared cache directory
	cache, err := cc.NewCacheDir(*cacheDir)
	if err != nil {
		return fmt.Errorf("create cache: %w", err)
	}

	// Create OCI client using shared cache
	client, err := cc.NewOCIClientWithCache(cache)
	if err != nil {
		return fmt.Errorf("create OCI client: %w", err)
	}

	ctx := context.Background()

	// Build Dockerfile source
	source, err := cc.BuildDockerfileSource(ctx, []byte(agent.Dockerfile), client,
		cc.WithDockerfileCacheDir(cache.SnapshotPath()),
		cc.WithMemoryMB(memoryMB),
	)
	if err != nil {
		return fmt.Errorf("build dockerfile: %w", err)
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
	opts = append(opts, cc.WithMemoryMB(memoryMB))
	opts = append(opts, cc.WithCache(cache))

	// Enable interactive mode when connected to a terminal
	if isTerminal {
		opts = append(opts, cc.WithInteractiveIO(wrapStdinForVT(os.Stdin), os.Stdout))
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

	// Determine command
	var cmd []string
	if *termFlag {
		cmd = []string{"/bin/bash"} // Shell for -term mode
	} else {
		cmd = append(agent.DefaultCmd, extraArgs...) // Agent's default command + extra args
	}

	// Run using exec mode (command replaces init as PID 1)
	if err := inst.Exec(cmd[0], cmd[1:]...); err != nil {
		// Restore terminal before exiting
		if oldState != nil {
			term.Restore(int(os.Stdin.Fd()), oldState)
		}
		return fmt.Errorf("exec: %w", err)
	}

	return nil
}

// listAgentsSorted returns agents sorted by name for consistent output.
func listAgentsSorted() []Agent {
	list := listAgents()
	sort.Slice(list, func(i, j int) bool {
		return list[i].Name < list[j].Name
	})
	return list
}
