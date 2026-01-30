---
title: API Overview
description: The CrumbleCracker Go API at a glance
---

The CrumbleCracker Go API lets you create and manage VMs programmatically. It's designed to feel like the Go standard library—if you know `os`, `os/exec`, and `net`, you already know how to use it.

## Design Principles

**Mirror the standard library**: Filesystem operations work like `os`. Command execution works like `os/exec`. Networking works like `net`. No new paradigms.

**Explicit over implicit**: No hidden state or global configuration. Everything is passed explicitly.

**Composition**: Build complex workflows from simple primitives.

## Core Types

### Instance

An `Instance` represents a running VM. It embeds three interfaces:

```go
type Instance interface {
    FS      // Filesystem operations (mirrors os)
    Exec    // Command execution (mirrors os/exec)
    Net     // Network operations (mirrors net)

    Close() error
    Wait() error
    ID() string
    Done() <-chan error
    GPU() GPU
}
```

### FS (Filesystem)

The `FS` interface mirrors the `os` package:

```go
type FS interface {
    Open(name string) (File, error)
    Create(name string) (File, error)
    ReadFile(name string) ([]byte, error)
    WriteFile(name string, data []byte, perm fs.FileMode) error
    Stat(name string) (fs.FileInfo, error)
    Remove(name string) error
    RemoveAll(path string) error
    Mkdir(name string, perm fs.FileMode) error
    MkdirAll(path string, perm fs.FileMode) error
    ReadDir(name string) ([]fs.DirEntry, error)
    // ...
}
```

### Exec (Command Execution)

The `Exec` interface mirrors `os/exec`:

```go
type Exec interface {
    Command(name string, args ...string) Cmd
    CommandContext(ctx context.Context, name string, args ...string) Cmd
    EntrypointCommand(args ...string) Cmd
}
```

### Net (Networking)

The `Net` interface mirrors the `net` package:

```go
type Net interface {
    Dial(network, address string) (net.Conn, error)
    DialContext(ctx context.Context, network, address string) (net.Conn, error)
    Listen(network, address string) (net.Listener, error)
    ListenPacket(network, address string) (net.PacketConn, error)
}
```

## Basic Workflow

```go
// 1. Create an OCI client
client, err := cc.NewOCIClient()

// 2. Pull an image
source, err := client.Pull(ctx, "alpine:latest")

// 3. Create a VM
instance, err := cc.New(source, cc.WithMemoryMB(256))
defer instance.Close()

// 4. Use it like you would use os, os/exec, and net
output, err := instance.Command("echo", "hello").Output()
err = instance.WriteFile("/tmp/test.txt", data, 0644)
conn, err := instance.Dial("tcp", "example.com:80")
```

## Package Structure

Import the package:

```go
import cc "github.com/tinyrange/cc"
```

The API surface is intentionally small:

- **Constructors**: `New`, `NewOCIClient`, `NewFilesystemSnapshotFactory`
- **Options**: `WithMemoryMB`, `WithTimeout`, `WithUser`, etc.
- **Types**: `Instance`, `FS`, `Exec`, `Net`, `Cmd`, `File`, `OCIClient`

## Error Handling

Sentinel errors for common conditions:

```go
var (
    ErrNotRunning            // Instance is not running
    ErrAlreadyClosed         // Instance was already closed
    ErrTimeout               // Operation timed out
    ErrHypervisorUnavailable // Hypervisor not available
)
```

Check with `errors.Is()`:

```go
if errors.Is(err, cc.ErrHypervisorUnavailable) {
    log.Fatal("Hypervisor not available on this system")
}
```

## Next Steps

- [Creating Instances](/cc/api/creating-instances/): VM creation and configuration
- [Filesystem](/cc/api/filesystem/): Working with files in VMs
- [Commands](/cc/api/commands/): Running programs and capturing output
- [Networking](/cc/api/networking/): Connecting to and from VMs
