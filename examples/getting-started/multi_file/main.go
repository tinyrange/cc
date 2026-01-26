// multi_file demonstrates working with multi-file projects in a sandboxed environment.
//
// This example shows how to:
// - Write multiple source files to the sandbox
// - Build a project with multiple files
// - Handle project dependencies and imports
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	cc "github.com/tinyrange/cc"
	"github.com/tinyrange/cc/examples/shared"
)

// Project represents a multi-file project.
type Project struct {
	Language string            `json:"language"` // python, node, go
	Files    map[string]string `json:"files"`    // filename -> content
	Main     string            `json:"main"`     // main file to run
}

func main() {
	if err := cc.EnsureExecutableIsSigned(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	projectJSON := flag.String("project", "", "Project JSON (language, files map, main)")
	timeout := flag.Duration("timeout", 90*time.Second, "Execution timeout")
	flag.Parse()

	if *projectJSON == "" {
		fmt.Fprintln(os.Stderr, "error: must provide -project JSON")
		os.Exit(1)
	}

	var project Project
	if err := json.Unmarshal([]byte(*projectJSON), &project); err != nil {
		fmt.Fprintf(os.Stderr, "error parsing project JSON: %v\n", err)
		os.Exit(1)
	}

	if err := run(project, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(project Project, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout+10*time.Second)
	defer cancel()

	// Determine image based on language
	var image string
	switch project.Language {
	case "python":
		image = "python:3.12-slim"
	case "node":
		image = "node:20-slim"
	case "go":
		image = "golang:1.22-alpine"
	default:
		return fmt.Errorf("unsupported language: %s (use: python, node, go)", project.Language)
	}

	// Create OCI client and pull image
	client, err := cc.NewOCIClient()
	if err != nil {
		return fmt.Errorf("creating OCI client: %w", err)
	}
	source, err := client.Pull(ctx, image)
	if err != nil {
		return fmt.Errorf("pulling image: %w", err)
	}

	// Create sandbox instance
	instance, err := cc.New(source,
		cc.WithMemoryMB(512),
		cc.WithTimeout(timeout+5*time.Second),
	)
	if err != nil {
		return fmt.Errorf("creating instance: %w", err)
	}
	defer instance.Close()

	// Write all project files
	fs := instance.WithContext(ctx)
	if err := fs.MkdirAll("/app", 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}

	for filename, content := range project.Files {
		path := "/app/" + filename
		if err := fs.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("writing %s: %w", filename, err)
		}
	}

	// Execute based on language
	execCtx, execCancel := context.WithTimeout(ctx, timeout)
	defer execCancel()

	var result shared.RunResult
	mainFile := "/app/" + project.Main

	switch project.Language {
	case "python":
		result = shared.RunCommand(execCtx, instance, "python3", mainFile)

	case "node":
		result = shared.RunCommand(execCtx, instance, "node", mainFile)

	case "go":
		// For Go, we need to compile first
		compileResult := shared.RunCommand(execCtx, instance, "go", "build", "-o", "/app/main", mainFile)
		if compileResult.ExitCode != 0 {
			fmt.Fprint(os.Stderr, compileResult.Stderr)
			os.Exit(compileResult.ExitCode)
		}
		result = shared.RunCommand(execCtx, instance, "/app/main")
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
