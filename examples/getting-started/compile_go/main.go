// compile_go demonstrates compiling and running Go code in a sandboxed environment.
//
// This example shows how to:
// - Use FilesystemSnapshotFactory to cache the Go build environment
// - Pull an OCI image (golang:1.22-alpine)
// - Create a sandbox instance from a cached snapshot
// - Write Go source code to the filesystem
// - Compile the code using go build
// - Execute the compiled binary
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

	code := flag.String("code", "", "Go code to compile and run")
	file := flag.String("file", "", "Go file to compile and run")
	timeout := flag.Duration("timeout", 60*time.Second, "Execution timeout")
	flag.Parse()

	if *code == "" && *file == "" {
		fmt.Fprintln(os.Stderr, "error: must provide -code or -file")
		os.Exit(1)
	}

	goCode := *code
	if *file != "" {
		data, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading file: %v\n", err)
			os.Exit(1)
		}
		goCode = string(data)
	}

	if err := run(goCode, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(code string, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout+10*time.Second)
	defer cancel()

	// Create OCI client
	client, err := cc.NewOCIClient()
	if err != nil {
		return fmt.Errorf("creating OCI client: %w", err)
	}

	// Use FilesystemSnapshotFactory to cache Go environment setup
	// This caches the base image with GOCACHE directory prepared
	cacheDir := shared.GetCacheDir()
	snap, err := cc.NewFilesystemSnapshotFactory(client, cacheDir).
		From("golang:1.22-alpine").
		Env("GOCACHE=/tmp/gocache").
		Run("mkdir", "-p", "/tmp/gocache").
		Exclude("/tmp/*").
		Build(ctx)
	if err != nil {
		return fmt.Errorf("building snapshot: %w", err)
	}
	defer snap.Close()

	// Create sandbox instance from the cached snapshot
	instance, err := cc.New(snap,
		cc.WithMemoryMB(512),
		cc.WithTimeout(timeout+5*time.Second),
		cc.WithEnv("GOCACHE=/tmp/gocache"),
	)
	if err != nil {
		return fmt.Errorf("creating instance: %w", err)
	}
	defer instance.Close()

	// Write the Go source file
	fs := instance.WithContext(ctx)
	if err := fs.MkdirAll("/app", 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}
	if err := fs.WriteFile("/app/main.go", []byte(code), 0644); err != nil {
		return fmt.Errorf("writing source: %w", err)
	}

	// Compile the code
	compileCtx, compileCancel := context.WithTimeout(ctx, timeout/2)
	defer compileCancel()

	compileResult := shared.RunCommand(compileCtx, instance, "go", "build", "-o", "/app/main", "/app/main.go")
	if compileResult.ExitCode != 0 {
		fmt.Fprint(os.Stderr, compileResult.Stderr)
		os.Exit(compileResult.ExitCode)
	}

	// Execute the compiled binary
	execCtx, execCancel := context.WithTimeout(ctx, timeout/2)
	defer execCancel()

	result := shared.RunCommand(execCtx, instance, "/app/main")

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
