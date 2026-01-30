---
title: Introduction
description: What CrumbleCracker does and why it matters
---

CrumbleCracker runs OCI containers inside lightweight virtual machines. You get the convenience of Docker images with hardware-level isolation—each container runs in its own VM with its own kernel.

## The Problem With Containers

Standard containers share your host kernel. That's what makes them fast, but it's also what makes them vulnerable. Container escapes are a real and recurring threat. When containers share a kernel, a single vulnerability can compromise everything.

CrumbleCracker takes a different approach: **every container gets its own VM**.

## How It Works

When you run a container with CrumbleCracker:

1. **Pull**: Download the image from Docker Hub or any OCI registry—same images you already use
2. **Boot**: Start a lightweight VM with its own Linux kernel
3. **Run**: Execute your workload inside the VM
4. **Isolate**: If something goes wrong, it hits the hypervisor, not your host

The result is the familiar container workflow with VM-grade security boundaries.

## Use Cases

**Sandboxed code execution**: Run untrusted code without risk. Each execution gets its own VM that's destroyed when done.

```go
instance, _ := cc.New(source, cc.WithTimeout(30*time.Second))
defer instance.Close()

instance.WriteFile("/code/script.py", userCode, 0644)
output, _ := instance.Command("python3", "/code/script.py").Output()
```

**CI/CD runners**: Build and test in isolated environments. No contamination between jobs, no persistent state to clean up.

**Development environments**: Test Linux software from macOS or Windows. Reproduce production environments locally without affecting your host.

**Multi-tenant systems**: Give each tenant their own VM. Isolation is enforced by hardware, not trust in namespace boundaries.

## Two Products

### CrumbleCracker App

A desktop application for running VMs interactively:

- Visual launcher for managing VM bundles
- Full terminal interface for running sessions
- Easy installation from Docker images
- Cross-architecture support via QEMU emulation

Best for developers who want to quickly spin up and interact with containerized environments.

[Learn more about the App →](/app/overview/)

### Go API

A programming library for embedding VMs in Go applications:

- API mirrors `os`, `os/exec`, and `net` packages
- Full filesystem, command, and networking support
- Snapshots for fast cold starts

Best for building automated systems that need strong isolation.

[Learn more about the API →](/api/overview/)

## Platform Support

| Platform | Hypervisor | Architectures |
|----------|------------|---------------|
| macOS | Hypervisor.framework | arm64 |
| Linux | KVM | x86_64, arm64 |
| Windows | Windows Hypervisor Platform | x86_64, arm64 |

Cross-architecture execution (running arm64 images on x86_64 or vice versa) is supported via QEMU user-mode emulation.

## Next Steps

- [Install CrumbleCracker](/getting-started/installation/) on your machine
- [Build your first VM](/getting-started/quick-start/) in the Quick Start tutorial
