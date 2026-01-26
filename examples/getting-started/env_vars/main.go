// env_vars demonstrates passing environment variables to sandboxed processes.
//
// This example shows how to:
// - Set environment variables using cc.WithEnv
// - Access environment variables in sandboxed code
// - Pass sensitive configuration without hardcoding
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

	envVars := flag.String("env", "", "Environment variables (comma-separated KEY=VALUE pairs)")
	code := flag.String("code", "", "Python code to execute")
	listEnv := flag.Bool("list", false, "List all environment variables")
	timeout := flag.Duration("timeout", 30*time.Second, "Execution timeout")
	flag.Parse()

	if !*listEnv && *code == "" {
		fmt.Fprintln(os.Stderr, "error: must provide -code or -list")
		os.Exit(1)
	}

	// Parse environment variables
	var envPairs []string
	if *envVars != "" {
		envPairs = strings.Split(*envVars, ",")
	}

	if err := run(envPairs, *code, *listEnv, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(envPairs []string, code string, listEnv bool, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout+10*time.Second)
	defer cancel()

	// Create OCI client and pull Python image
	client, err := cc.NewOCIClient()
	if err != nil {
		return fmt.Errorf("creating OCI client: %w", err)
	}
	source, err := client.Pull(ctx, "python:3.12-slim")
	if err != nil {
		return fmt.Errorf("pulling image: %w", err)
	}

	// Create sandbox instance with environment variables
	opts := []cc.Option{
		cc.WithMemoryMB(256),
		cc.WithTimeout(timeout + 5*time.Second),
	}

	// Add environment variables
	if len(envPairs) > 0 {
		opts = append(opts, cc.WithEnv(envPairs...))
	}

	instance, err := cc.New(source, opts...)
	if err != nil {
		return fmt.Errorf("creating instance: %w", err)
	}
	defer instance.Close()

	execCtx, execCancel := context.WithTimeout(ctx, timeout)
	defer execCancel()

	var result shared.RunResult

	if listEnv {
		// List all environment variables
		result = shared.RunCommand(execCtx, instance, "python3", "-c", "import os\nfor k, v in sorted(os.environ.items()):\n    print(f'{k}={v}')")
	} else {
		// Write and execute the provided code
		fs := instance.WithContext(ctx)
		if err := fs.MkdirAll("/app", 0755); err != nil {
			return fmt.Errorf("creating directory: %w", err)
		}
		if err := fs.WriteFile("/app/script.py", []byte(code), 0644); err != nil {
			return fmt.Errorf("writing script: %w", err)
		}
		result = shared.RunCommand(execCtx, instance, "python3", "/app/script.py")
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
