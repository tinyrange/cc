// stdin_echo demonstrates processing stdin in a sandboxed environment.
//
// This example shows how to:
// - Pass stdin to commands in a sandbox
// - Process input and produce output
// - Use different processing modes (cat, wc, sort, etc.)
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	cc "github.com/tinyrange/cc"
	"github.com/tinyrange/cc/examples/shared"
)

func main() {
	if err := cc.EnsureExecutableIsSigned(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	mode := flag.String("mode", "cat", "Processing mode: cat, upper, lower, wc, sort, reverse")
	input := flag.String("input", "", "Input text (or reads from stdin if empty)")
	timeout := flag.Duration("timeout", 30*time.Second, "Execution timeout")
	flag.Parse()

	inputText := *input
	if inputText == "" {
		// Read from stdin
		data := make([]byte, 1024*1024) // 1MB max
		n, _ := os.Stdin.Read(data)
		inputText = string(data[:n])
	}

	if err := run(*mode, inputText, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(mode, input string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout+10*time.Second)
	defer cancel()

	// Create OCI client and pull Alpine image
	client, err := cc.NewOCIClient()
	if err != nil {
		return fmt.Errorf("creating OCI client: %w", err)
	}
	source, err := client.Pull(ctx, "alpine:3.19")
	if err != nil {
		return fmt.Errorf("pulling image: %w", err)
	}

	// Create sandbox instance
	instance, err := cc.New(source,
		cc.WithMemoryMB(128),
		cc.WithTimeout(timeout+5*time.Second),
	)
	if err != nil {
		return fmt.Errorf("creating instance: %w", err)
	}
	defer instance.Close()

	execCtx, execCancel := context.WithTimeout(ctx, timeout)
	defer execCancel()

	var result shared.RunResult
	stdinReader := strings.NewReader(input)

	switch mode {
	case "cat":
		// Simply echo the input
		result = shared.RunCommandWithStdin(execCtx, instance, stdinReader, "cat")

	case "upper":
		// Convert to uppercase
		result = shared.RunCommandWithStdin(execCtx, instance, stdinReader, "tr", "a-z", "A-Z")

	case "lower":
		// Convert to lowercase
		result = shared.RunCommandWithStdin(execCtx, instance, stdinReader, "tr", "A-Z", "a-z")

	case "wc":
		// Word count
		result = shared.RunCommandWithStdin(execCtx, instance, stdinReader, "wc")

	case "sort":
		// Sort lines
		result = shared.RunCommandWithStdin(execCtx, instance, stdinReader, "sort")

	case "reverse":
		// Reverse each line
		result = shared.RunCommandWithStdin(execCtx, instance, stdinReader, "rev")

	default:
		return fmt.Errorf("unknown mode: %s (use: cat, upper, lower, wc, sort, reverse)", mode)
	}

	// Output results
	fmt.Print(result.Stdout)
	if result.Stderr != "" {
		fmt.Fprint(os.Stderr, result.Stderr)
	}

	if result.ExitCode != 0 {
		os.Exit(result.ExitCode)
	}

	return nil
}
