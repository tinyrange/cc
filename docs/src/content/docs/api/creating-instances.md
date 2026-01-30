---
title: Creating Instances
description: How to create and configure VM instances
---

An `Instance` is a running virtual machine. This page covers how to create instances and configure them with options.

## Basic Creation

Create an instance from an `InstanceSource` (typically pulled from a registry):

```go
client, err := cc.NewOCIClient()
if err != nil {
    return err
}

source, err := client.Pull(ctx, "alpine:latest")
if err != nil {
    return err
}

instance, err := cc.New(source)
if err != nil {
    return err
}
defer instance.Close()
```

The `New` function blocks until the VM is fully booted and ready to accept commands.

## Instance Sources

An `InstanceSource` is anything that can be used to create a VM. The package provides several ways to get one:

### From a Registry

```go
source, err := client.Pull(ctx, "alpine:latest")
source, err := client.Pull(ctx, "python:3.12-slim")
source, err := client.Pull(ctx, "ghcr.io/user/image:tag")
```

### From a Tarball

Load an OCI tarball (created with `docker save`):

```go
source, err := client.LoadFromTar("/path/to/image.tar")
```

### From a Local Directory

Load a pre-exported image directory:

```go
source, err := client.LoadFromDir("/path/to/image-dir")
```

### From a Snapshot

Use a filesystem snapshot for faster startup:

```go
snapshot, err := instance.SnapshotFilesystem()
newInstance, err := cc.New(snapshot)
```

## Common Options

### Memory

Set the VM memory size in megabytes:

```go
instance, err := cc.New(source, cc.WithMemoryMB(512))
```

The default is platform-dependent. Larger images may require more memory.

### Timeout

Set a maximum lifetime for the instance:

```go
instance, err := cc.New(source, cc.WithTimeout(30*time.Second))
```

After this duration, the instance is forcibly terminated. This is useful for sandboxed code execution.

### CPUs

Set the number of virtual CPUs:

```go
instance, err := cc.New(source, cc.WithCPUs(2))
```

The default is 1.

### User

Run commands as a specific user:

```go
// By username
instance, err := cc.New(source, cc.WithUser("nobody"))

// By UID
instance, err := cc.New(source, cc.WithUser("1000"))

// User and group
instance, err := cc.New(source, cc.WithUser("1000:1000"))
```

## Combining Options

Pass multiple options to `New`:

```go
instance, err := cc.New(source,
    cc.WithMemoryMB(256),
    cc.WithCPUs(2),
    cc.WithTimeout(60*time.Second),
    cc.WithUser("nobody"),
)
```

## Instance Lifecycle

### Closing

Always close instances to release resources:

```go
instance, err := cc.New(source)
if err != nil {
    return err
}
defer instance.Close()

// Use the instance...
```

### Waiting for Exit

If the VM's init process exits, you can wait for it:

```go
err := instance.Wait()
```

### Non-blocking Exit Check

Use the `Done()` channel for non-blocking exit monitoring:

```go
select {
case err := <-instance.Done():
    if err != nil {
        log.Printf("VM exited with error: %v", err)
    }
default:
    // VM still running
}
```

## Checking Hypervisor Availability

Check if the hypervisor is available before creating instances:

```go
if err := cc.SupportsHypervisor(); err != nil {
    log.Fatal("Hypervisor unavailable:", err)
}
```

This returns a descriptive error explaining why the hypervisor isn't available (missing permissions, disabled in BIOS, etc.).

## Interactive Mode

For interactive terminal sessions, use `WithInteractiveIO`:

```go
instance, err := cc.New(source,
    cc.WithInteractiveIO(os.Stdin, os.Stdout),
)
```

This connects stdin/stdout directly to the VM's console for live I/O.

## Debug Options

### Kernel Messages

Enable kernel dmesg output for debugging boot issues:

```go
instance, err := cc.New(source, cc.WithDmesg())
```

### Packet Capture

Capture network traffic for debugging:

```go
f, _ := os.Create("capture.pcap")
defer f.Close()

instance, err := cc.New(source, cc.WithPacketCapture(f))
```

The captured packets can be analyzed with Wireshark or tcpdump.

## Next Steps

- [Filesystem Operations](/cc/api/filesystem/) - Work with files in VMs
- [Command Execution](/cc/api/commands/) - Run programs
- [Instance Options Reference](/cc/reference/options/) - Complete options list
