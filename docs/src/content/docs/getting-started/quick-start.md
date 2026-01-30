---
title: Quick Start
description: Boot your first VM in under 5 minutes
---

This tutorial walks you through creating and running a VM using the CrumbleCracker Go API. You'll pull a container image, boot a VM, and run commands inside it.

## Prerequisites

- Go 1.24.7 or later
- Hypervisor enabled ([see Installation](/getting-started/installation/))

## Create a Project

```bash
mkdir hello-cc && cd hello-cc
go mod init hello-cc
go get github.com/tinyrange/cc
```

## Write the Code

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
    // On macOS, handle hypervisor entitlement
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

    // Create an OCI client
    client, err := cc.NewOCIClient()
    if err != nil {
        return fmt.Errorf("creating client: %w", err)
    }

    // Pull Alpine (small and fast to boot)
    fmt.Println("Pulling alpine:latest...")
    source, err := client.Pull(ctx, "alpine:latest")
    if err != nil {
        return fmt.Errorf("pulling image: %w", err)
    }

    // Create a VM
    fmt.Println("Booting VM...")
    instance, err := cc.New(source,
        cc.WithMemoryMB(128),
        cc.WithTimeout(30*time.Second),
    )
    if err != nil {
        return fmt.Errorf("creating instance: %w", err)
    }
    defer instance.Close()

    // Run a command
    fmt.Println("Running command...")
    output, err := instance.Command("echo", "Hello from CrumbleCracker!").Output()
    if err != nil {
        return fmt.Errorf("running command: %w", err)
    }

    fmt.Printf("Output: %s", output)
    return nil
}
```

## Run It

```bash
go run main.go
```

Expected output:

```
Pulling alpine:latest...
Booting VM...
Running command...
Output: Hello from CrumbleCracker!
```

## What Happened

1. **Pull**: Downloaded Alpine Linux from Docker Hub
2. **Boot**: Started a lightweight VM with its own Linux kernel
3. **Execute**: Ran `echo` inside the VM and captured output
4. **Cleanup**: Shut down the VM when `Close()` was called

## Working With Files

Read and write files in the VM's filesystem:

```go
// Write a file
err := instance.WriteFile("/tmp/hello.txt", []byte("Hello!"), 0644)

// Read it back
content, err := instance.ReadFile("/tmp/hello.txt")
fmt.Println(string(content)) // "Hello!"

// Create directories
err = instance.MkdirAll("/app/data", 0755)

// List contents
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

script := `
import sys
print(f"Python {sys.version}")
print("2 + 2 =", 2 + 2)
`
instance.WriteFile("/app/script.py", []byte(script), 0644)

output, _ := instance.Command("python3", "/app/script.py").Output()
fmt.Println(string(output))
```

## Checking Exit Codes

```go
cmd := instance.Command("sh", "-c", "exit 42")
err := cmd.Run()
if err != nil {
    fmt.Printf("Exit code: %d\n", cmd.ExitCode())
}
```

## Environment Variables

```go
cmd := instance.Command("sh", "-c", "echo $MY_VAR")
cmd.SetEnv("MY_VAR", "hello")
output, _ := cmd.Output()
fmt.Println(string(output)) // "hello"
```

## Next Steps

- [Filesystem Operations](/api/filesystem/): Full file manipulation API
- [Command Execution](/api/commands/): Streaming, stdin, and more
- [OCI Images](/api/oci-images/): Working with registries and images
- [Snapshots](/api/snapshots/): Fast startup with filesystem snapshots
