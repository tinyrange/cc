---
title: Installation
description: Get CrumbleCracker running on your machine
---

CrumbleCracker is available as a desktop application or as a Go library. Choose based on how you want to use it.

## CrumbleCracker App

The desktop application provides a visual interface for managing VMs.

### macOS

1. Download the latest `.zip` from [GitHub Releases](https://github.com/tinyrange/cc/releases)
2. Extract and move to Applications (or run directly)
3. On first launch, approve the app in **System Preferences → Security & Privacy** if prompted

### Linux

1. Download the binary from [GitHub Releases](https://github.com/tinyrange/cc/releases)
2. Make it executable and run:

```bash
chmod +x ccapp
./ccapp
```

3. Ensure KVM is available:

```bash
# Check KVM device exists
ls -la /dev/kvm

# Add yourself to the kvm group if needed
sudo usermod -aG kvm $USER
# Log out and back in for the group change to take effect
```

### Windows

1. Download the binary from [GitHub Releases](https://github.com/tinyrange/cc/releases)
2. Enable Windows Hypervisor Platform:
   - Open "Turn Windows features on or off"
   - Check "Windows Hypervisor Platform"
   - Restart your computer
3. Run the binary

## Go Library

Add the library to your project:

```bash
go get github.com/tinyrange/cc
```

### macOS: Hypervisor Entitlement

Executables using the hypervisor must be signed with the hypervisor entitlement. For development, CrumbleCracker provides a helper:

```go
func TestMain(m *testing.M) {
    if err := cc.EnsureExecutableIsSigned(); err != nil {
        log.Fatalf("Failed to sign executable: %v", err)
    }
    os.Exit(m.Run())
}
```

This automatically signs the test binary and re-executes it. For production builds, sign properly:

```bash
codesign --entitlements entitlements.plist -s - your-binary
```

Where `entitlements.plist` contains:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>com.apple.security.hypervisor</key>
    <true/>
</dict>
</plist>
```

### Linux: KVM Access

Your program needs read/write access to `/dev/kvm`:

```bash
# Recommended: add user to kvm group
sudo usermod -aG kvm $USER
# Log out and back in

# Alternative: run with sudo (not recommended for production)
sudo ./your-program
```

### Verify Installation

Test that the hypervisor is available:

```go
if err := cc.SupportsHypervisor(); err != nil {
    log.Fatal("Hypervisor unavailable:", err)
}
fmt.Println("Ready to create VMs")
```

## Building From Source

Clone and build with the included build tool:

```bash
git clone https://github.com/tinyrange/cc.git
cd cc

# Build CLI and desktop app
./tools/build.go

# Run tests
./tools/build.go -test ./...

# Build and launch the desktop app
./tools/build.go -app
```

## Next Steps

- [Quick Start](/getting-started/quick-start/): Build your first VM
- [Go API Overview](/api/overview/): Learn the programming interface
- [App Overview](/app/overview/): Explore the desktop application
