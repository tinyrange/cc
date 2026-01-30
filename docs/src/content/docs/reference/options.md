---
title: Options Reference
description: Complete list of all configuration options
---

This page documents all options available for configuring instances, OCI pulls, and snapshots.

## Instance Options

Options passed to `cc.New()` when creating an instance.

### WithMemoryMB

```go
cc.WithMemoryMB(size uint64) Option
```

Sets the VM memory size in megabytes.

**Example:**
```go
instance, err := cc.New(source, cc.WithMemoryMB(512))
```

**Default:** Platform-dependent (typically 256-512 MB)

### WithCPUs

```go
cc.WithCPUs(count int) Option
```

Sets the number of virtual CPUs.

**Example:**
```go
instance, err := cc.New(source, cc.WithCPUs(4))
```

**Default:** 1

### WithTimeout

```go
cc.WithTimeout(d time.Duration) Option
```

Sets a maximum lifetime for the instance. After this duration, the instance is forcibly terminated.

**Example:**
```go
instance, err := cc.New(source, cc.WithTimeout(30*time.Second))
```

**Default:** No timeout

### WithUser

```go
cc.WithUser(user string) Option
```

Sets the user (and optionally group) to run as inside the guest.

**Formats:**
- `"username"` - By name
- `"1000"` - By UID
- `"user:group"` - User and group
- `"1000:1000"` - UID and GID

**Example:**
```go
instance, err := cc.New(source, cc.WithUser("nobody"))
```

**Default:** Root (or image default)

### WithInteractiveIO

```go
cc.WithInteractiveIO(stdin io.Reader, stdout io.Writer) Option
```

Enables interactive terminal mode and sets stdin/stdout. When enabled, I/O connects directly to virtio-console for live terminal interaction.

**Example:**
```go
instance, err := cc.New(source,
    cc.WithInteractiveIO(os.Stdin, os.Stdout),
)
```

### WithDmesg

```go
cc.WithDmesg() Option
```

Enables kernel dmesg output (loglevel=7). Useful for debugging boot issues.

**Example:**
```go
instance, err := cc.New(source, cc.WithDmesg())
```

### WithPacketCapture

```go
cc.WithPacketCapture(w io.Writer) Option
```

Enables packet capture (pcap format) to the given writer.

**Example:**
```go
f, _ := os.Create("capture.pcap")
defer f.Close()
instance, err := cc.New(source, cc.WithPacketCapture(f))
```

### WithGPU

```go
cc.WithGPU() Option
```

Enables virtio-gpu and virtio-input devices for graphical output.

**Example:**
```go
runtime.LockOSThread()
instance, err := cc.New(source, cc.WithGPU())
```

**Note:** Requires running the display loop on the main thread.

### WithMount

```go
cc.WithMount(config MountConfig) Option
```

Adds a virtio-fs mount to the guest.

**MountConfig fields:**
- `Tag` - Mount tag used in guest
- `HostPath` - Host directory to expose (empty for ephemeral)
- `Writable` - Whether the mount is writable (default: read-only)

**Example:**
```go
instance, err := cc.New(source,
    cc.WithMount(cc.MountConfig{
        Tag:      "shared",
        HostPath: "/host/data",
        Writable: true,
    }),
)
```

Guest mount command: `mount -t virtiofs shared /mnt/shared`

### WithCache

```go
cc.WithCache(cache CacheDir) Option
```

Sets the cache directory for the instance (used for QEMU binaries, etc.).

**Example:**
```go
cache, _ := cc.NewCacheDir("/path/to/cache")
instance, err := cc.New(source, cc.WithCache(cache))
```

## OCI Pull Options

Options passed to `client.Pull()` when pulling images.

### WithPlatform

```go
cc.WithPlatform(os, arch string) OCIPullOption
```

Specifies the platform to pull.

**Example:**
```go
source, err := client.Pull(ctx, "alpine:latest",
    cc.WithPlatform("linux", "arm64"),
)
```

**Default:** Host platform

### WithAuth

```go
cc.WithAuth(username, password string) OCIPullOption
```

Provides authentication for private registries.

**Example:**
```go
source, err := client.Pull(ctx, "private.registry.com/image:tag",
    cc.WithAuth("user", "token"),
)
```

### WithPullPolicy

```go
cc.WithPullPolicy(policy PullPolicy) OCIPullOption
```

Sets when to fetch from the registry vs use cache.

**Policies:**
- `cc.PullIfNotPresent` - Only pull if not cached (default)
- `cc.PullAlways` - Always check registry for updates
- `cc.PullNever` - Never pull, fail if not cached

**Example:**
```go
source, err := client.Pull(ctx, "alpine:latest",
    cc.WithPullPolicy(cc.PullAlways),
)
```

### WithProgressCallback

```go
cc.WithProgressCallback(fn ProgressCallback) OCIPullOption
```

Sets a callback for download progress updates.

**Example:**
```go
source, err := client.Pull(ctx, "large-image:latest",
    cc.WithProgressCallback(func(p cc.DownloadProgress) {
        fmt.Printf("%s: %d/%d bytes\n", p.Filename, p.Current, p.Total)
    }),
)
```

**DownloadProgress fields:**
- `Current` - Bytes downloaded
- `Total` - Total bytes (0 if unknown)
- `Filename` - Current file being downloaded
- `BlobIndex` - Current blob index
- `BlobCount` - Total number of blobs
- `BytesPerSecond` - Download speed
- `ETA` - Estimated time remaining

## Snapshot Options

Options for filesystem snapshot operations.

### WithSnapshotExcludes

```go
cc.WithSnapshotExcludes(patterns ...string) FilesystemSnapshotOption
```

Excludes paths matching the given patterns from snapshots.

**Example:**
```go
snapshot, err := instance.SnapshotFilesystem(
    cc.WithSnapshotExcludes("/var/cache/*", "/tmp/*", "*.log"),
)
```

### WithSnapshotCacheDir

```go
cc.WithSnapshotCacheDir(dir string) FilesystemSnapshotOption
```

Sets the directory for snapshot cache storage.

**Example:**
```go
snapshot, err := instance.SnapshotFilesystem(
    cc.WithSnapshotCacheDir("/path/to/cache"),
)
```

## Dockerfile Options

Options for `BuildDockerfileSource()`.

### WithBuildContext

```go
cc.WithBuildContext(ctx DockerfileBuildContext) DockerfileOption
```

Sets a custom build context for COPY/ADD operations.

### WithBuildContextDir

```go
cc.WithBuildContextDir(dir string) DockerfileOption
```

Creates a build context from a directory path.

**Example:**
```go
source, err := cc.BuildDockerfileSource(ctx, dockerfile, client,
    cc.WithBuildContextDir("./app"),
)
```

### WithBuildArg

```go
cc.WithBuildArg(key, value string) DockerfileOption
```

Sets a build argument (ARG instruction).

**Example:**
```go
source, err := cc.BuildDockerfileSource(ctx, dockerfile, client,
    cc.WithBuildArg("VERSION", "1.2.3"),
)
```

### WithDockerfileCacheDir

```go
cc.WithDockerfileCacheDir(dir string) DockerfileOption
```

Sets the cache directory for filesystem snapshots during Dockerfile builds.

**Example:**
```go
source, err := cc.BuildDockerfileSource(ctx, dockerfile, client,
    cc.WithDockerfileCacheDir("/path/to/cache"),
)
```
