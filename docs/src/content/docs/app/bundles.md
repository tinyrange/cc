---
title: Bundles
description: Understanding and configuring VM bundles
---

A bundle is a directory containing a VM configuration and its container image. Bundles are the unit of distribution for VMs in ccapp.

## Bundle Structure

A minimal bundle contains:

```
my-bundle/
├── ccbundle.yaml    # Configuration file
└── image/           # Pre-exported OCI image
    ├── config.json
    └── ... (layer files)
```

## Configuration File

The `ccbundle.yaml` file defines the bundle metadata and boot configuration.

### Minimal Example

```yaml
version: 1
name: My Application
boot:
  imageDir: image
```

### Full Example

```yaml
version: 1
name: Development Environment
description: Ubuntu with development tools pre-installed
icon: icon.png

boot:
  imageDir: image
  command: ["/bin/bash"]
  cpus: 2
  memoryMB: 2048
  exec: false
  dmesg: false
  env:
    - EDITOR=vim
    - LANG=en_US.UTF-8
```

## Configuration Fields

### Top-level Fields

| Field | Type | Description |
|-------|------|-------------|
| `version` | int | Bundle format version (currently 1) |
| `name` | string | Display name in the launcher |
| `description` | string | Brief description shown below the name |
| `icon` | string | Path to icon file (relative to bundle directory) |

### Boot Configuration

The `boot` section controls how the VM starts:

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `imageDir` | string | `image` | Path to the OCI image directory |
| `command` | []string | (from image) | Command to run (overrides image CMD) |
| `cpus` | int | 1 | Number of virtual CPUs |
| `memoryMB` | int | 1024 | Memory size in megabytes |
| `exec` | bool | false | Replace init with command (terminal operation) |
| `dmesg` | bool | false | Show kernel messages (loglevel=7) |
| `env` | []string | - | Additional environment variables |

### Environment Variables

Add custom environment variables in `KEY=value` format:

```yaml
boot:
  env:
    - MY_VAR=hello
    - DEBUG=true
    - PATH=/custom/bin:/usr/bin:/bin
```

These are appended to the image's environment variables.

## Command Configuration

### Using Image Default

If `command` is not specified, the image's `ENTRYPOINT` and `CMD` are used:

```yaml
boot:
  # Uses whatever the image specifies
```

### Overriding CMD

Specify a command array to override the image's CMD (while keeping ENTRYPOINT):

```yaml
boot:
  command: ["--debug", "--port", "8080"]
```

### Running a Shell

Force an interactive shell regardless of image settings:

```yaml
boot:
  command: ["/bin/sh"]
```

Or with bash:

```yaml
boot:
  command: ["/bin/bash"]
```

## Exec Mode

When `exec: true`, the command replaces the init process (like Unix exec):

```yaml
boot:
  command: ["/usr/bin/python3", "/app/server.py"]
  exec: true
```

The command becomes PID 1. When it exits, the VM terminates.

Use this for:
- Long-running services where you don't need init functionality
- Simpler process management
- Faster shutdown (no init to clean up)

## Creating Bundles

### From Docker Image

Export an image and create the bundle manually:

```bash
# Pull and export the image
docker pull nginx:alpine
docker save nginx:alpine -o nginx.tar

# Extract the tarball to create the image directory
mkdir -p my-nginx/image
tar -xf nginx.tar -C my-nginx/image

# Create the configuration
cat > my-nginx/ccbundle.yaml << EOF
version: 1
name: Nginx Web Server
description: Lightweight web server
boot:
  imageDir: image
  memoryMB: 256
EOF
```

### Using ccapp

1. Add a VM from a Docker image in ccapp
2. Find the bundle in the bundles directory
3. Edit `ccbundle.yaml` as needed

## Bundle Locations

When you add a VM in ccapp, bundles are stored in:

- macOS: `~/Library/Application Support/ccapp/bundles/`
- Linux: `~/.config/ccapp/bundles/`
- Windows: `%APPDATA%\ccapp\bundles\`

You can also open bundle directories from other locations.

## Icons

Add a custom icon by:

1. Place an image file in the bundle directory (PNG recommended)
2. Reference it in the configuration:

```yaml
icon: my-icon.png
```

Icons are displayed in the launcher.

## Best Practices

### Memory Sizing

Start with the minimum and increase if needed:

- **128-256 MB**: Simple tools (busybox, alpine shell)
- **512-1024 MB**: Development tools, compilers
- **2048+ MB**: Large applications, databases

### CPU Allocation

More CPUs help with:
- Parallel builds
- Multi-threaded applications
- Multiple background processes

For simple single-threaded apps, 1 CPU is sufficient.

### Environment Variables

Set `TERM` for proper terminal support:

```yaml
boot:
  env:
    - TERM=xterm-256color
```

This is set automatically if not specified.

## Next Steps

- [Settings](/cc/app/settings/) - Application configuration
- [Creating VMs](/cc/app/creating-vms/) - Adding new VMs
