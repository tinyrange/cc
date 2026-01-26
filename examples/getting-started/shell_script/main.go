// shell_script demonstrates running shell scripts in a sandboxed environment.
//
// This example shows how to:
// - Execute shell scripts with /bin/sh
// - Use shell features like pipes, redirects, and loops
// - Run complex shell commands
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

	script := flag.String("script", "", "Shell script to execute")
	file := flag.String("file", "", "Shell script file to execute")
	command := flag.String("c", "", "Single shell command to execute")
	timeout := flag.Duration("timeout", 30*time.Second, "Execution timeout")
	flag.Parse()

	shellCode := *script
	if *file != "" {
		data, err := os.ReadFile(*file)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error reading file: %v\n", err)
			os.Exit(1)
		}
		shellCode = string(data)
	} else if *command != "" {
		shellCode = *command
	}

	if shellCode == "" {
		fmt.Fprintln(os.Stderr, "error: must provide -script, -file, or -c")
		os.Exit(1)
	}

	if err := run(shellCode, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(script string, timeout time.Duration) error {
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

	// Write the script to a file and execute it
	fs := instance.WithContext(ctx)
	if err := fs.MkdirAll("/app", 0755); err != nil {
		return fmt.Errorf("creating directory: %w", err)
	}
	if err := fs.WriteFile("/app/script.sh", []byte(script), 0755); err != nil {
		return fmt.Errorf("writing script: %w", err)
	}

	// Execute the script
	execCtx, execCancel := context.WithTimeout(ctx, timeout)
	defer execCancel()

	result := shared.RunCommand(execCtx, instance, "/bin/sh", "/app/script.sh")

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
