---
title: OCI Images
description: Pulling and managing container images
---

The `OCIClient` handles pulling, loading, and exporting OCI container images. It provides access to images from Docker Hub, GitHub Container Registry, and other OCI-compliant registries.

## Overview

```go
// Create a client
client, err := cc.NewOCIClient()

// Pull an image
source, err := client.Pull(ctx, "alpine:latest")

// Use it to create a VM
instance, err := cc.New(source)
```

## Creating a Client

### Default Cache

Create a client with the default cache directory:

```go
client, err := cc.NewOCIClient()
if err != nil {
    return err
}
```

The cache location is platform-specific:
- macOS: `~/Library/Application Support/cc/oci`
- Linux: `~/.cache/cc/oci`
- Windows: `%APPDATA%\cc\oci`

### Custom Cache

Use a specific cache directory:

```go
cacheDir, err := cc.NewCacheDir("/path/to/cache")
if err != nil {
    return err
}

client, err := cc.NewOCIClientWithCache(cacheDir)
```

## Pulling Images

### Basic Pull

```go
source, err := client.Pull(ctx, "alpine:latest")
source, err := client.Pull(ctx, "python:3.12-slim")
source, err := client.Pull(ctx, "ubuntu:22.04")
```

### From Other Registries

```go
// GitHub Container Registry
source, err := client.Pull(ctx, "ghcr.io/username/image:tag")

// Amazon ECR
source, err := client.Pull(ctx, "123456789.dkr.ecr.us-east-1.amazonaws.com/myapp:latest")

// Google Container Registry
source, err := client.Pull(ctx, "gcr.io/project/image:tag")
```

### With Authentication

Pull from private registries:

```go
source, err := client.Pull(ctx, "private-registry.com/image:tag",
    cc.WithAuth("username", "password"),
)
```

### Platform Selection

Pull a specific platform variant:

```go
// Pull arm64 image
source, err := client.Pull(ctx, "alpine:latest",
    cc.WithPlatform("linux", "arm64"),
)

// Pull amd64 image
source, err := client.Pull(ctx, "python:3.12",
    cc.WithPlatform("linux", "amd64"),
)
```

By default, pulls match the host platform.

## Pull Policies

Control when to fetch from the registry:

```go
// Only pull if not in cache (default)
source, err := client.Pull(ctx, "alpine:latest",
    cc.WithPullPolicy(cc.PullIfNotPresent),
)

// Always check registry for updates
source, err := client.Pull(ctx, "alpine:latest",
    cc.WithPullPolicy(cc.PullAlways),
)

// Never pull, fail if not cached
source, err := client.Pull(ctx, "alpine:latest",
    cc.WithPullPolicy(cc.PullNever),
)
```

## Progress Reporting

Track download progress:

```go
source, err := client.Pull(ctx, "large-image:latest",
    cc.WithProgressCallback(func(p cc.DownloadProgress) {
        if p.Total > 0 {
            pct := float64(p.Current) / float64(p.Total) * 100
            fmt.Printf("\rDownloading: %.1f%% (%s)", pct, p.Filename)
        }
    }),
)
```

The `DownloadProgress` struct contains:

```go
type DownloadProgress struct {
    Current        int64   // Bytes downloaded
    Total          int64   // Total bytes (0 if unknown)
    Filename       string  // Current file being downloaded
    BlobIndex      int     // Current blob index
    BlobCount      int     // Total number of blobs
    BytesPerSecond float64 // Download speed
    ETA            time.Duration // Estimated time remaining
}
```

## Loading Local Images

### From Tarball

Load an image exported with `docker save`:

```go
source, err := client.LoadFromTar("/path/to/image.tar")
```

### From Directory

Load a pre-extracted image:

```go
source, err := client.LoadFromDir("/path/to/image-dir")
```

## Exporting Images

Export an image to a directory for faster future loading:

```go
err := client.ExportToDir(source, "/path/to/export-dir")
```

This creates a directory with:
- `config.json` - Image configuration
- Layer files in an optimized format

## Image Configuration

Access container metadata from the image:

```go
config := cc.SourceConfig(source)
if config != nil {
    fmt.Println("Architecture:", config.Architecture)
    fmt.Println("Entrypoint:", config.Entrypoint)
    fmt.Println("Cmd:", config.Cmd)
    fmt.Println("Env:", config.Env)
    fmt.Println("WorkingDir:", config.WorkingDir)
    fmt.Println("User:", config.User)
}
```

The `ImageConfig` struct:

```go
type ImageConfig struct {
    Architecture string            // "amd64", "arm64", etc.
    Env          []string          // Environment variables
    WorkingDir   string            // Working directory
    Entrypoint   []string          // Container entrypoint
    Cmd          []string          // Container CMD
    User         string            // User specification
    Labels       map[string]string // OCI labels
}
```

## Cache Management

Get the cache directory path:

```go
cacheDir := client.CacheDir()
fmt.Println("Cache at:", cacheDir)
```

Clear the cache manually:

```bash
rm -rf ~/.cache/cc/oci  # Linux
rm -rf ~/Library/Application\ Support/cc/oci  # macOS
```

## Example: Pull and Inspect

```go
func inspectImage(imageName string) error {
    client, err := cc.NewOCIClient()
    if err != nil {
        return err
    }

    source, err := client.Pull(context.Background(), imageName,
        cc.WithProgressCallback(func(p cc.DownloadProgress) {
            if p.Total > 0 {
                fmt.Printf("\rPulling %s: %d/%d bytes",
                    p.Filename, p.Current, p.Total)
            }
        }),
    )
    if err != nil {
        return err
    }
    fmt.Println() // newline after progress

    config := cc.SourceConfig(source)
    if config == nil {
        fmt.Println("No config available")
        return nil
    }

    fmt.Println("Image:", imageName)
    fmt.Println("Architecture:", config.Architecture)

    if len(config.Entrypoint) > 0 {
        fmt.Println("Entrypoint:", strings.Join(config.Entrypoint, " "))
    }
    if len(config.Cmd) > 0 {
        fmt.Println("Cmd:", strings.Join(config.Cmd, " "))
    }
    if config.WorkingDir != "" {
        fmt.Println("WorkDir:", config.WorkingDir)
    }
    if config.User != "" {
        fmt.Println("User:", config.User)
    }

    fmt.Println("\nEnvironment:")
    for _, env := range config.Env {
        fmt.Println("  ", env)
    }

    return nil
}
```

## Example: Offline Mode

Pre-download images for offline use:

```go
func downloadForOffline(images []string, exportDir string) error {
    client, err := cc.NewOCIClient()
    if err != nil {
        return err
    }

    for _, image := range images {
        fmt.Printf("Downloading %s...\n", image)

        source, err := client.Pull(context.Background(), image)
        if err != nil {
            return fmt.Errorf("pull %s: %w", image, err)
        }

        // Export to directory for fast loading later
        safeName := strings.ReplaceAll(image, "/", "_")
        safeName = strings.ReplaceAll(safeName, ":", "_")
        dir := filepath.Join(exportDir, safeName)

        if err := client.ExportToDir(source, dir); err != nil {
            return fmt.Errorf("export %s: %w", image, err)
        }

        fmt.Printf("Exported to %s\n", dir)
    }

    return nil
}

// Later, load without network
func loadOffline(exportDir, imageName string) (cc.InstanceSource, error) {
    client, err := cc.NewOCIClient()
    if err != nil {
        return nil, err
    }

    safeName := strings.ReplaceAll(imageName, "/", "_")
    safeName = strings.ReplaceAll(safeName, ":", "_")

    return client.LoadFromDir(filepath.Join(exportDir, safeName))
}
```

## Next Steps

- [Filesystem Snapshots](/cc/api/snapshots/) - Cache modified filesystems
- [Creating Instances](/cc/api/creating-instances/) - Start VMs from images
