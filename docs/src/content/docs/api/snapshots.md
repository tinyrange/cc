---
title: Snapshots
description: Filesystem snapshots for faster startup
---

Filesystem snapshots capture the state of a VM's filesystem and can be used to quickly create new instances. They're especially useful when you need to run many VMs with the same base setup.

## Why Snapshots?

Consider a development workflow where you need to run user code with Python and some pip packages. Without snapshots:

1. Pull `python:3.12-slim` image (cached after first time)
2. Start VM
3. Run `pip install` (slow, downloads packages each time)
4. Run user code
5. Shutdown

With snapshots:

1. First time: Create snapshot after `pip install`
2. Subsequent runs: Start from snapshot (packages already installed)

Startup goes from several seconds to under a second.

## Basic Usage

### Creating a Snapshot

After setting up a VM, capture its filesystem state:

```go
instance, err := cc.New(source)
if err != nil {
    return err
}

// Set up the environment
instance.Command("pip", "install", "numpy", "pandas").Run()

// Create a snapshot
snapshot, err := instance.SnapshotFilesystem()
if err != nil {
    return err
}
defer snapshot.Close()

// Use the snapshot to create new instances
newInstance, err := cc.New(snapshot)
```

### Snapshot Options

Exclude paths from snapshots to reduce size:

```go
snapshot, err := instance.SnapshotFilesystem(
    cc.WithSnapshotExcludes("/var/cache/*", "/tmp/*"),
)
```

## FilesystemSnapshotFactory

The factory provides a declarative way to build snapshots with automatic caching:

```go
factory := cc.NewFilesystemSnapshotFactory(client, cacheDir)

snapshot, err := factory.
    From("alpine:3.19").
    Run("apk", "add", "--no-cache", "gcc", "musl-dev").
    Exclude("/var/cache/*", "/tmp/*").
    Build(ctx)
if err != nil {
    return err
}
defer snapshot.Close()

instance, err := cc.New(snapshot)
```

### How Caching Works

The factory generates a cache key from the operation chain. On subsequent runs:

1. If a cached snapshot exists for this key, load it directly
2. Otherwise, execute the operations and cache the result

This means `factory.From("alpine:3.19").Run("apk", "add", "gcc")` will only run `apk add` once, regardless of how many times you build.

### Factory Operations

#### From

Start from a container image:

```go
factory.From("python:3.12-slim")
```

#### Run

Execute a command and capture the result:

```go
factory.
    From("alpine:3.19").
    Run("apk", "add", "--no-cache", "curl").
    Run("apk", "add", "--no-cache", "jq")
```

Each `Run` creates a layer that's cached independently.

#### Exclude

Exclude paths from the snapshot:

```go
factory.
    From("python:3.12-slim").
    Run("pip", "install", "flask").
    Exclude("/root/.cache/*", "/var/cache/*", "/tmp/*")
```

This reduces snapshot size by excluding caches and temporary files.

#### Build

Execute the operation chain and return the snapshot:

```go
snapshot, err := factory.
    From("python:3.12-slim").
    Run("pip", "install", "numpy").
    Build(ctx)
```

## Example: Compiler Cache

Create a snapshot with a C compiler installed:

```go
func getCompilerSnapshot(client cc.OCIClient, cacheDir string) (cc.FilesystemSnapshot, error) {
    return cc.NewFilesystemSnapshotFactory(client, cacheDir).
        From("alpine:3.19").
        Run("apk", "add", "--no-cache", "gcc", "musl-dev", "make").
        Exclude("/var/cache/*").
        Build(context.Background())
}

func compileCode(code string) ([]byte, error) {
    client, _ := cc.NewOCIClient()
    cacheDir := filepath.Join(os.TempDir(), "cc-cache")

    snapshot, err := getCompilerSnapshot(client, cacheDir)
    if err != nil {
        return nil, err
    }
    defer snapshot.Close()

    instance, err := cc.New(snapshot, cc.WithMemoryMB(256))
    if err != nil {
        return nil, err
    }
    defer instance.Close()

    // Compiler is already installed, just compile
    instance.WriteFile("/code/main.c", []byte(code), 0644)

    output, err := instance.Command("gcc", "-o", "/code/main", "/code/main.c").CombinedOutput()
    if err != nil {
        return nil, fmt.Errorf("compile failed: %s", output)
    }

    return instance.Command("/code/main").Output()
}
```

## Example: Python with Dependencies

Pre-install Python packages:

```go
func getPythonDataScienceSnapshot(client cc.OCIClient, cacheDir string) (cc.FilesystemSnapshot, error) {
    return cc.NewFilesystemSnapshotFactory(client, cacheDir).
        From("python:3.12-slim").
        Run("pip", "install", "--no-cache-dir", "numpy", "pandas", "matplotlib").
        Exclude("/root/.cache/*", "/tmp/*").
        Build(context.Background())
}
```

## Example: Multi-stage Snapshots

Build complex environments in stages:

```go
func getFullDevSnapshot(client cc.OCIClient, cacheDir string) (cc.FilesystemSnapshot, error) {
    factory := cc.NewFilesystemSnapshotFactory(client, cacheDir)

    // Stage 1: Base with build tools
    base, err := factory.
        From("debian:bookworm-slim").
        Run("apt-get", "update").
        Run("apt-get", "install", "-y", "--no-install-recommends",
            "build-essential", "curl", "git").
        Build(context.Background())
    if err != nil {
        return nil, err
    }

    // Use base snapshot to install more tools
    instance, _ := cc.New(base)
    defer instance.Close()

    // Install Node.js
    instance.Command("sh", "-c",
        "curl -fsSL https://deb.nodesource.com/setup_20.x | bash -").Run()
    instance.Command("apt-get", "install", "-y", "nodejs").Run()

    // Create final snapshot
    return instance.SnapshotFilesystem(
        cc.WithSnapshotExcludes("/var/cache/*", "/var/lib/apt/lists/*"),
    )
}
```

## Cache Key Computation

The factory generates deterministic cache keys based on:

- Base image reference
- Commands (exact string match)
- Exclude patterns

Changing any of these creates a new cache entry. The factory uses content-addressed storage, so identical operations share cached layers.

## Snapshot Internals

A `FilesystemSnapshot` implements `InstanceSource`, so it can be passed to `cc.New()`. Snapshots use copy-on-write semantics:

- The base filesystem is shared (read-only)
- Changes are stored in an overlay
- Multiple instances from the same snapshot share the base

This makes creating many instances from a snapshot very fast and memory-efficient.

## Next Steps

- [Dockerfile Support](/cc/api/dockerfile/) - Build from Dockerfiles
- [Creating Instances](/cc/api/creating-instances/) - Use snapshots to create VMs
