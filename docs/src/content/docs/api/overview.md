---
title: API Overview
description: Introduction to the CrumbleCracker Go API
---

The CrumbleCracker Go API lets you create and manage virtual machines programmatically. It's designed to feel familiar to Go developers by mirroring standard library patterns.

## Design Philosophy

The API is built around three core principles:

1. **Mirror the standard library**: Filesystem operations mirror `os`, command execution mirrors `os/exec`, and networking mirrors `net`
2. **Explicit over implicit**: No hidden state or global configuration
3. **Composition**: Build complex workflows from simple primitives

## Core Types

### Instance

An `Instance` represents a running virtual machine. It embeds three interfaces:

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

The `FS` interface provides filesystem operations that mirror the `os` package:

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
    // ... more
}
```

### Exec (Command Execution)

The `Exec` interface provides command execution that mirrors `os/exec`:

```go
type Exec interface {
    Command(name string, args ...string) Cmd
    CommandContext(ctx context.Context, name string, args ...string) Cmd
    EntrypointCommand(args ...string) Cmd
}
```

### Net (Networking)

The `Net` interface provides network operations that mirror the `net` package:

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

// 3. Create an instance
instance, err := cc.New(source, cc.WithMemoryMB(256))
defer instance.Close()

// 4. Use the instance
output, err := instance.Command("echo", "hello").Output()
err = instance.WriteFile("/tmp/test.txt", data, 0644)
conn, err := instance.Dial("tcp", "example.com:80")
```

## Package Structure

Import the package:

```go
import cc "github.com/tinyrange/cc"
```

All public types and functions are exported from the main `cc` package. The API surface is intentionally small:

- **Constructors**: `New`, `NewOCIClient`, `NewFilesystemSnapshotFactory`
- **Options**: `WithMemoryMB`, `WithTimeout`, `WithUser`, etc.
- **Types**: `Instance`, `FS`, `Exec`, `Net`, `Cmd`, `File`, `OCIClient`

## Error Handling

Errors follow Go conventions. The package provides sentinel errors for common conditions:

```go
var (
    ErrNotRunning            // Instance is not running
    ErrAlreadyClosed         // Instance was already closed
    ErrTimeout               // Operation timed out
    ErrHypervisorUnavailable // Hypervisor not available
)
```

Use `errors.Is()` to check for specific errors:

```go
if errors.Is(err, cc.ErrHypervisorUnavailable) {
    log.Fatal("Hypervisor not available on this system")
}
```

## Next Steps

- [Creating Instances](/api/creating-instances/) - Learn about instance creation and options
- [Filesystem Operations](/api/filesystem/) - Work with files in VMs
- [Command Execution](/api/commands/) - Run programs and capture output
- [Networking](/api/networking/) - Connect to and from VMs
