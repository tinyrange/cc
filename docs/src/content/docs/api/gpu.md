---
title: GPU Support
description: Graphical output and input devices
---

CrumbleCracker supports virtio-gpu and virtio-input devices for graphical applications. This enables running desktop environments, games, and other GUI applications inside VMs.

## Overview

GPU support provides:

- **Framebuffer rendering**: Access the VM's display output
- **Keyboard input**: Send keyboard events to the VM
- **Mouse input**: Send mouse movement and clicks

## Enabling GPU

Use `WithGPU()` when creating an instance:

```go
runtime.LockOSThread() // Required for windowing on macOS

instance, err := cc.New(source, cc.WithGPU())
if err != nil {
    return err
}
defer instance.Close()
```

## The GPU Interface

When GPU is enabled, `instance.GPU()` returns a non-nil interface:

```go
type GPU interface {
    SetWindow(w any)
    Poll() bool
    Render()
    Swap()
    GetFramebuffer() (pixels []byte, width, height uint32, ok bool)
}
```

## Display Loop

You must run the display loop on the main thread:

```go
func main() {
    runtime.LockOSThread()

    instance, _ := cc.New(source, cc.WithGPU())
    defer instance.Close()

    gpu := instance.GPU()
    if gpu == nil {
        log.Fatal("GPU not available")
    }

    // Create a window (platform-specific)
    window := createWindow(800, 600)
    gpu.SetWindow(window)

    // Display loop
    for {
        if !gpu.Poll() {
            break // Window closed
        }
        gpu.Render()
        gpu.Swap()
    }
}
```

### Poll

Process window events (keyboard, mouse, close) and forward input to the VM:

```go
if !gpu.Poll() {
    // Window was closed
    break
}
```

### Render

Render the current framebuffer to the window:

```go
gpu.Render()
```

### Swap

Swap the window buffers (double-buffering):

```go
gpu.Swap()
```

## Getting Raw Framebuffer

Access the framebuffer pixels directly:

```go
pixels, width, height, ok := gpu.GetFramebuffer()
if ok {
    // pixels is in BGRA format
    // width and height are the framebuffer dimensions
    saveScreenshot(pixels, width, height)
}
```

This is useful for:
- Taking screenshots
- Recording video
- Headless rendering
- Remote display streaming

## Example: Run Sway Desktop

```go
func runSway() error {
    runtime.LockOSThread()

    client, _ := cc.NewOCIClient()
    source, _ := client.Pull(ctx, "ghcr.io/example/sway-desktop")

    instance, err := cc.New(source,
        cc.WithMemoryMB(2048),
        cc.WithCPUs(2),
        cc.WithGPU(),
    )
    if err != nil {
        return err
    }
    defer instance.Close()

    gpu := instance.GPU()

    // Start Sway (Wayland compositor) in the VM
    go func() {
        instance.Command("sway").Run()
    }()

    // Create window and run display loop
    window := createWindow(1280, 720)
    gpu.SetWindow(window)

    for {
        if !gpu.Poll() {
            break
        }
        gpu.Render()
        gpu.Swap()
    }

    return nil
}
```

## Example: Headless Screenshot

Capture a screenshot without displaying a window:

```go
func captureScreenshot() ([]byte, error) {
    instance, _ := cc.New(source, cc.WithGPU())
    defer instance.Close()

    gpu := instance.GPU()

    // Start a GUI application
    go func() {
        instance.Command("firefox", "--screenshot", "/tmp/page.png", "https://example.com").Run()
    }()

    // Wait for rendering
    time.Sleep(5 * time.Second)

    // Capture framebuffer
    pixels, width, height, ok := gpu.GetFramebuffer()
    if !ok {
        return nil, errors.New("framebuffer not available")
    }

    // Convert BGRA to PNG
    return convertToPNG(pixels, width, height)
}
```

## Input Handling

Input events are handled automatically by `Poll()`. The GPU interface forwards:

- Keyboard key presses and releases
- Mouse movement
- Mouse button clicks
- Mouse wheel scrolling

## Requirements

GPU support requires:

1. A display server running in the guest (X11 or Wayland)
2. Virtio-GPU drivers in the guest kernel (included in most distributions)
3. A windowing system on the host (not available in headless SSH sessions)

## Platform Notes

### macOS

- Requires `runtime.LockOSThread()` for Cocoa windowing
- Uses Metal for rendering

### Linux

- Requires a display server (X11 or Wayland)
- Uses OpenGL for rendering

### Windows

- Uses WGL for rendering
- Window creation is handled internally

## Console Size

For terminal applications, set the console size:

```go
instance.SetConsoleSize(80, 24) // columns, rows
```

This updates the virtio-console device so the guest sees the correct terminal dimensions.

## Next Steps

- [Instance Options](/reference/options/) - All GPU-related options
- [ccapp Overview](/app/overview/) - GUI application with built-in GPU support
