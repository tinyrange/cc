// file_ops demonstrates filesystem operations in a sandboxed environment.
//
// This example shows how to:
// - Use the cc.FS interface for file operations
// - Create directories with MkdirAll
// - Write files with WriteFile
// - Read files with ReadFile
// - List directories with ReadDir
// - Remove files with Remove and RemoveAll
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	cc "github.com/tinyrange/cc"
)

func main() {
	if err := cc.EnsureExecutableIsSigned(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	operation := flag.String("op", "", "Operation: write, read, list, mkdir, remove")
	path := flag.String("path", "", "File or directory path")
	content := flag.String("content", "", "Content to write (for write operation)")
	timeout := flag.Duration("timeout", 30*time.Second, "Execution timeout")
	flag.Parse()

	if *operation == "" {
		fmt.Fprintln(os.Stderr, "error: must provide -op (write, read, list, mkdir, remove)")
		os.Exit(1)
	}
	if *path == "" {
		fmt.Fprintln(os.Stderr, "error: must provide -path")
		os.Exit(1)
	}

	if err := run(*operation, *path, *content, *timeout); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(operation, path, content string, timeout time.Duration) error {
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

	fs := instance.WithContext(ctx)

	switch operation {
	case "mkdir":
		if err := fs.MkdirAll(path, 0755); err != nil {
			return fmt.Errorf("mkdir: %w", err)
		}
		fmt.Printf("created directory: %s\n", path)

	case "write":
		// Ensure parent directory exists
		dir := path[:strings.LastIndex(path, "/")]
		if dir != "" {
			if err := fs.MkdirAll(dir, 0755); err != nil {
				return fmt.Errorf("mkdir: %w", err)
			}
		}
		if err := fs.WriteFile(path, []byte(content), 0644); err != nil {
			return fmt.Errorf("write: %w", err)
		}
		fmt.Printf("wrote %d bytes to %s\n", len(content), path)

	case "read":
		data, err := fs.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		fmt.Print(string(data))

	case "list":
		entries, err := fs.ReadDir(path)
		if err != nil {
			return fmt.Errorf("list: %w", err)
		}
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() {
				name += "/"
			}
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Println(name)
		}

	case "remove":
		if err := fs.RemoveAll(path); err != nil {
			return fmt.Errorf("remove: %w", err)
		}
		fmt.Printf("removed: %s\n", path)

	default:
		return fmt.Errorf("unknown operation: %s", operation)
	}

	return nil
}
