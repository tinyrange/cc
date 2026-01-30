---
title: Creating VMs
description: Different ways to create VMs in ccapp
---

CrumbleCracker App supports three methods for creating VMs: pulling Docker images, loading OCI tarballs, and opening bundle directories.

## From Docker Images

Pull container images directly from registries.

### Steps

1. Click the **+** button in the launcher
2. Select the **Docker Image** tab
3. Enter an image name (e.g., `alpine:latest`, `ubuntu:22.04`)
4. Click **Add**

### Supported Registries

- **Docker Hub**: `nginx`, `python:3.12`, `node:20-slim`
- **GitHub Container Registry**: `ghcr.io/user/image:tag`
- **Amazon ECR**: `123456789.dkr.ecr.us-east-1.amazonaws.com/image`
- **Google Container Registry**: `gcr.io/project/image`
- Any OCI-compliant registry

### Progress Tracking

During download, a progress bar shows:
- Current download progress
- Transfer speed
- Estimated time remaining

### Image Selection Tips

- Use slim/alpine variants for faster downloads and startup
- Check that the image has an entrypoint or CMD defined
- The image's architecture should match your host (or cross-emulation will be used)

## From OCI Tarballs

Load images exported from Docker or other tools.

### Creating a Tarball

Export an image with Docker:

```bash
docker save nginx:latest -o nginx.tar
```

Or with other tools like Podman:

```bash
podman save nginx:latest -o nginx.tar
```

### Steps

1. Click the **+** button
2. Select the **OCI Tarball** tab
3. Click **Browse...** and select your `.tar` file
4. Click **Add**

The tarball is imported and a new bundle is created in the bundles directory.

## From Bundle Directories

Open an existing bundle directory directly.

### Steps

1. Click the **+** button
2. Select the **Bundle Directory** tab
3. Click **Browse...** and select the folder containing `ccbundle.yaml`
4. Click **Add**

This validates the bundle and adds it to the launcher. Unlike the other methods, this does not copy files—it references the original directory.

### Bundle Requirements

The directory must contain:
- `ccbundle.yaml` - Bundle metadata file
- `image/` - Pre-exported OCI image directory (or path specified in metadata)

## After Creation

Once a VM is added, it appears in the launcher. You can:

- **Launch**: Click the bundle card to start the VM
- **Configure**: Click the settings icon to modify bundle options
- **Delete**: Remove the bundle from the settings dialog

## Cross-Architecture Images

ccapp supports running images built for different architectures:

- Running `linux/arm64` images on x86_64 hosts
- Running `linux/amd64` images on arm64 hosts

This uses QEMU user-mode emulation. Performance is reduced compared to native execution, but it enables running images from any architecture.

Cross-architecture support is automatic—if the image architecture doesn't match the host, ccapp downloads the appropriate QEMU binary and configures emulation.

## Troubleshooting

### "No entrypoint/CMD"

The image must define an entrypoint or command. Check the image documentation or specify a command in the bundle configuration.

### "Registry authentication required"

Private registries require authentication. Currently, ccapp doesn't support registry authentication in the UI—use `docker pull` and export as a tarball instead.

### "Image not found"

Check that:
- The image name and tag are correct
- You have network connectivity
- The registry is accessible

## Next Steps

- [Terminal Mode](/app/terminal-mode/) - Using the running VM
- [Bundles](/app/bundles/) - Customizing bundle configuration
