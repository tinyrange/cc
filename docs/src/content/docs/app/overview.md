---
title: App Overview
description: The CrumbleCracker desktop application
---

CrumbleCracker App is a desktop application for running VMs interactively. It provides a visual interface for managing VMs built from container images—no code required.

## What It Does

- **Visual VM launcher**: Browse and launch VMs from a graphical interface
- **Full terminal access**: Interactive terminal sessions inside running VMs
- **Multiple image sources**: Create VMs from Docker images, OCI tarballs, or bundle directories
- **Cross-architecture**: Run arm64 images on x86_64 and vice versa via QEMU emulation

## Main Screens

### Launcher

The launcher displays installed VM bundles. Each bundle shows:

- Name and description
- Icon (if configured)
- Settings button

Click a bundle to start its VM.

### Terminal

When a VM is running, the app switches to terminal mode:

- Full terminal emulation
- Keyboard and mouse input
- Network status indicator
- Shutdown controls

The terminal uses the Tokyo Night color scheme.

### Custom VM Dialog

Create new VMs from:

- **Docker Image**: Pull from Docker Hub or any OCI registry
- **OCI Tarball**: Load from a `.tar` file (docker save format)
- **Bundle Directory**: Open an existing bundle folder

## Platform Support

| Platform | Status |
|----------|--------|
| macOS (Apple Silicon) | Supported |
| Linux (x86_64) | Supported |
| Linux (arm64) | Supported |
| Windows (x86_64) | Supported |

## System Requirements

- **macOS**: macOS 11+ with Hypervisor.framework
- **Linux**: KVM enabled, user in `kvm` group
- **Windows**: Windows Hypervisor Platform enabled

## Quick Start

1. Download the latest release from [GitHub](https://github.com/tinyrange/cc/releases)
2. Install for your platform
3. Launch the app
4. Click "+" and enter an image name (e.g., `alpine:latest`)
5. Click the new bundle to start the VM

## Data Locations

### Bundles

Installed bundles are stored in:

- **macOS**: `~/Library/Application Support/ccapp/bundles/`
- **Linux**: `~/.config/ccapp/bundles/`
- **Windows**: `%APPDATA%\ccapp\bundles\`

### Logs

Application logs are written to:

- **macOS**: `~/Library/Caches/ccapp/`
- **Linux**: `~/.cache/ccapp/`
- **Windows**: `%LOCALAPPDATA%\ccapp\`

### Settings

User settings are stored in `settings.json` alongside bundles.

## Next Steps

- [Creating VMs](/app/creating-vms/): Different ways to create VMs
- [Terminal Mode](/app/terminal-mode/): Using the terminal interface
- [Bundles](/app/bundles/): Understanding the bundle format
- [Settings](/app/settings/): Configure the application
