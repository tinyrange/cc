---
title: Quick Start
description: Build your first VM with CrumbleCracker
---

This tutorial shows you how to create and run a VM using the CrumbleCracker Go API. You'll pull a container image, start a VM, and execute commands inside it.

## Prerequisites

- Go 1.21 or later
- Hypervisor support enabled (see [Installation](/getting-started/installation/))

## Hello World

Create a new Go project:

```bash
mkdir hello-cc && cd hello-cc
go mod init hello-cc
go get github.com/tinyrange/cc
```

Create `main.go`:

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    cc "github.com/tinyrange/cc"
)

func main() {
    // On macOS, ensure the binary is signed with hypervisor entitlement
    if err := cc.EnsureExecutableIsSigned(); err != nil {
        log.Fatalf("Failed to sign executable: %v", err)
    }

    if err := run(); err != nil {
        fmt.Fprintf(os.Stderr, "Error: %v\n", err)
        os.Exit(1)
    }
}

func run() error {
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
    defer cancel()

    // Create an OCI client to pull images
    client, err := cc.NewOCIClient()
    if err != nil {
        return fmt.Errorf("creating client: %w", err)
    }

    // Pull Alpine Linux (a small, fast-booting image)
    fmt.Println("Pulling alpine:latest...")
    source, err := client.Pull(ctx, "alpine:latest")
    if err != nil {
        return fmt.Errorf("pulling image: %w", err)
    }

    // Create a VM from the image
    fmt.Println("Starting VM...")
    instance, err := cc.New(source,
        cc.WithMemoryMB(128),
        cc.WithTimeout(30*time.Second),
    )
    if err != nil {
        return fmt.Errorf("creating instance: %w", err)
    }
    defer instance.Close()

    // Run a command inside the VM
    fmt.Println("Running command...")
    output, err := instance.Command("echo", "Hello from CrumbleCracker!").Output()
    if err != nil {
        return fmt.Errorf("running command: %w", err)
    }

    fmt.Printf("Output: %s", output)
    return nil
}
```

Run it:

```bash
go run main.go
```

You should see:

```
Pulling alpine:latest...
Starting VM...
Running command...
Output: Hello from CrumbleCracker!
```

## What Just Happened?

1. **Pull**: The OCI client downloaded the Alpine Linux container image from Docker Hub
2. **Boot**: A lightweight VM started with its own Linux kernel
3. **Execute**: The `echo` command ran inside the VM and output was captured
4. **Cleanup**: The VM was shut down when `Close()` was called

## Working with Files

You can read and write files in the VM's filesystem:

```go
// Write a file
err := instance.WriteFile("/tmp/hello.txt", []byte("Hello!"), 0644)
if err != nil {
    return err
}

// Read it back
content, err := instance.ReadFile("/tmp/hello.txt")
if err != nil {
    return err
}
fmt.Println(string(content)) // "Hello!"

// Create directories
err = instance.MkdirAll("/app/data", 0755)

// List directory contents
entries, err := instance.ReadDir("/tmp")
for _, entry := range entries {
    fmt.Println(entry.Name())
}
```

## Running Python

Use a Python image to run scripts:

```go
source, _ := client.Pull(ctx, "python:3.12-slim")
instance, _ := cc.New(source, cc.WithMemoryMB(256))
defer instance.Close()

// Write a Python script
script := `
import sys
print(f"Python {sys.version}")
print("2 + 2 =", 2 + 2)
`
instance.WriteFile("/app/script.py", []byte(script), 0644)

// Run it
output, _ := instance.Command("python3", "/app/script.py").Output()
fmt.Println(string(output))
```

## Capturing Exit Codes

Check the exit status of commands:

```go
cmd := instance.Command("sh", "-c", "exit 42")
err := cmd.Run()
if err != nil {
    fmt.Printf("Command failed with exit code: %d\n", cmd.ExitCode())
}
```

## Setting Environment Variables

Pass environment variables to commands:

```go
cmd := instance.Command("sh", "-c", "echo $MY_VAR")
cmd.SetEnv("MY_VAR", "hello")
output, _ := cmd.Output()
fmt.Println(string(output)) // "hello"
```

## Next Steps

- [Learn about the filesystem interface](/api/filesystem/)
- [Explore command execution](/api/commands/)
- [Work with OCI images](/api/oci-images/)
- [Use filesystem snapshots for faster startup](/api/snapshots/)
