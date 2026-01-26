// run_python demonstrates running Python scripts in a sandboxed environment.
//
// This example shows how to:
// - Pull an OCI image (python:3.12-slim)
// - Create a sandbox instance
// - Write a Python script to the filesystem
// - Execute the script and capture output
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	cc "github.com/tinyrange/cc"
	"github.com/tinyrange/cc/examples/shared"
)

func main() {
	if err := cc.EnsureExecutableIsSigned(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	code := flag.String("code", "", "Python code to execute")
	file := flag.String("file", "", "Python file to execute")
	timeout := flag.Duration("timeout", 30*time.Second, "Execution timeout")
	flag.Parse()

	if *code == "" && *file == "" {
		fmt.Fprintln(os.Stderr, "error: must provide -code or -file")
		os.Exit(1)
	}

	pythonCode := *code
	if *file != "" {
		data, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading file: %v\n", err)
			os.Exit(1)
		}
		pythonCode = string(data)
	}

	if err := run(pythonCode, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(code string, timeout time.Duration) error {
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

	// Create sandbox instance
	instance, err := cc.New(source,
		cc.WithMemoryMB(256),
		cc.WithTimeout(timeout+5*time.Second),
	)
	if err != nil {
		return fmt.Errorf("creating instance: %w", err)
	}
	defer instance.Close()

	// Write the Python script
	fs := instance.WithContext(ctx)
	if err := fs.MkdirAll("/app", 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}
	if err := fs.WriteFile("/app/script.py", []byte(code), 0644); err != nil {
		return fmt.Errorf("writing script: %w", err)
	}

	// Execute the script
	execCtx, execCancel := context.WithTimeout(ctx, timeout)
	defer execCancel()

	result := shared.RunCommand(execCtx, instance, "python3", "/app/script.py")

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
