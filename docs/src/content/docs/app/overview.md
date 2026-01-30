---
title: CrumbleCracker App Overview
description: Introduction to the ccapp desktop application
---

CrumbleCracker App (ccapp) is a GUI desktop application for running virtual machines. It provides a visual interface for managing and interacting with VMs built from container images.

## Features

- **Visual VM Launcher**: Browse and launch installed VMs from a graphical interface
- **Terminal Mode**: Full terminal interface for interactive sessions
- **Multiple Input Sources**: Create VMs from Docker images, OCI tarballs, or bundle directories
- **Cross-Architecture Support**: Run arm64 images on x86_64 and vice versa via QEMU emulation
- **Auto-Update**: Automatic updates to the latest version
- **Recent VMs**: Quick access to recently used VMs

## Main Screens

### Launcher Screen

The launcher displays all installed VM bundles. Each bundle appears as a card showing:

- Bundle name and description
- Icon (if configured)
- Settings button for configuration

Click a bundle to start the VM.

### Terminal Screen

When a VM is running, ccapp switches to terminal mode. This provides:

- Full terminal emulation
- Keyboard and mouse input
- Network status indicator
- Shutdown controls

The terminal uses the Tokyo Night color scheme for comfortable extended use.

### Custom VM Dialog

Create new VMs from various sources:

- **Docker Image**: Pull from Docker Hub or any OCI registry
- **OCI Tarball**: Load from a `.tar` file (docker save format)
- **Bundle Directory**: Open an existing bundle folder

## Supported Platforms

| Platform | Status |
|----------|--------|
| macOS (Apple Silicon) | Fully supported |
| macOS (Intel) | Fully supported |
| Linux (x86_64) | Fully supported |
| Linux (arm64) | Fully supported |
| Windows (x86_64) | Fully supported |

## System Requirements

- **macOS**: macOS 11+ with Hypervisor.framework entitlement
- **Linux**: KVM enabled, user in `kvm` group
- **Windows**: Windows Hypervisor Platform enabled

## Quick Start

1. **Download** the latest release from GitHub
2. **Install** the application for your platform
3. **Launch** ccapp
4. **Add a VM**: Click "+" and enter a Docker image name (e.g., `alpine:latest`)
5. **Run**: Click the newly installed bundle to start the VM

## Application Data Locations

### Bundles Directory

Installed VM bundles are stored in the user config directory:

- macOS: `~/Library/Application Support/ccapp/bundles/`
- Linux: `~/.config/ccapp/bundles/`
- Windows: `%APPDATA%\ccapp\bundles\`

### Logs Directory

Application logs are written to the cache directory:

- macOS: `~/Library/Caches/ccapp/`
- Linux: `~/.cache/ccapp/`
- Windows: `%LOCALAPPDATA%\ccapp\`

### Settings

User settings are stored alongside bundles in `settings.json`.

## Next Steps

- [Creating VMs](/app/creating-vms/) - Learn the different ways to create VMs
- [Terminal Mode](/app/terminal-mode/) - Using the terminal interface
- [Bundles](/app/bundles/) - Understanding the bundle format
- [Settings](/app/settings/) - Configure the application
