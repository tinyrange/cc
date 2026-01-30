---
title: Introduction
description: What is CrumbleCracker and what can you do with it?
---

CrumbleCracker is a lightweight virtualization platform that runs OCI containers inside virtual machines. It combines the convenience of Docker images with the security of hardware-level isolation.

## What is CrumbleCracker?

CrumbleCracker takes standard container images (the same ones you use with Docker) and runs them inside lightweight virtual machines. Each VM has its own Linux kernel and uses your platform's native hypervisor for hardware-level isolation.

**Key characteristics:**

- **Container-compatible**: Pull images from Docker Hub, GitHub Container Registry, or any OCI registry
- **Hardware isolation**: Each VM runs in a separate virtual machine, not just a namespace
- **Native performance**: Uses platform hypervisors (macOS Hypervisor.framework, Linux KVM, Windows WHP)
- **Go-native API**: Program VMs using familiar patterns from `os`, `os/exec`, and `net` packages

## Use Cases

### Sandboxed Code Execution

Run untrusted code in isolated environments:

```go
instance, _ := cc.New(source, cc.WithTimeout(30*time.Second))
defer instance.Close()

instance.WriteFile("/code/script.py", userCode, 0644)
output, _ := instance.Command("python3", "/code/script.py").Output()
```

### Development Environments

Test code in clean, reproducible environments without affecting your host system.

### CI/CD Runners

Build isolated test runners with stronger isolation than containers alone.

### Cross-Platform Testing

Test Linux software from macOS or Windows development machines.

## Two Products

### CrumbleCracker App (ccapp)

A GUI desktop application for running VMs interactively. It provides:

- Visual launcher for installed VMs
- Terminal interface for running sessions
- Easy installation of Docker images as VM bundles
- Cross-architecture support via QEMU emulation

Perfect for developers who want to quickly spin up and interact with containerized environments.

[Learn more about ccapp →](/app/overview/)

### Go API

A programming library for embedding VMs in Go applications. Use it to:

- Execute code in sandboxed environments
- Build custom container tooling
- Create isolated test harnesses
- Implement secure multi-tenant systems

The API is designed to feel natural to Go developers, mirroring standard library patterns.

[Learn more about the API →](/api/overview/)

## Supported Platforms

| Platform | Hypervisor | Architectures |
|----------|------------|---------------|
| macOS | Hypervisor.framework | x86_64, arm64 |
| Linux | KVM | x86_64, arm64 |
| Windows | Windows Hypervisor Platform | x86_64 |

Cross-architecture execution (e.g., running arm64 images on x86_64 hosts) is supported via QEMU user-mode emulation.

## Next Steps

- [Install CrumbleCracker](/getting-started/installation/)
- [Try the Quick Start tutorial](/getting-started/quick-start/)
