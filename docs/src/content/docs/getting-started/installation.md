---
title: Installation
description: Install CrumbleCracker on your machine
---

CrumbleCracker can be installed as a desktop application (ccapp) or as a Go library for programmatic use.

## CrumbleCracker App

### macOS

Download the latest `.zip` from the [GitHub releases page](https://github.com/tinyrange/cc/releases) and extract it. Move the app to your Applications folder or run it directly.

On first launch, macOS may require you to approve the app in System Preferences → Security & Privacy.

### Linux

Download the binary from the [GitHub releases page](https://github.com/tinyrange/cc/releases) and make it executable:

```bash
chmod +x ccapp
./ccapp
```

Ensure KVM is enabled:

```bash
# Check KVM availability
ls -la /dev/kvm

# Add your user to the kvm group if needed
sudo usermod -aG kvm $USER
# Log out and back in for the group change to take effect
```

### Windows

Download the binary from the [GitHub releases page](https://github.com/tinyrange/cc/releases) and run it. You may need to enable Windows Hypervisor Platform:

1. Open "Turn Windows features on or off"
2. Enable "Windows Hypervisor Platform"
3. Restart your computer

## Go Library

Add CrumbleCracker to your Go project:

```bash
go get github.com/tinyrange/cc
```

### macOS Code Signing

On macOS, executables that use the hypervisor must be signed with the hypervisor entitlement. For development and testing, use `EnsureExecutableIsSigned()`:

```go
func TestMain(m *testing.M) {
    if err := cc.EnsureExecutableIsSigned(); err != nil {
        log.Fatalf("Failed to sign executable: %v", err)
    }
    os.Exit(m.Run())
}
```

This automatically signs the test binary with the required entitlement and re-executes it. For production builds, sign your binary properly with:

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

### Linux Permissions

On Linux, your program needs access to `/dev/kvm`:

```bash
# Option 1: Add user to kvm group
sudo usermod -aG kvm $USER

# Option 2: Run with sudo (not recommended for production)
sudo ./your-program
```

### Verifying Installation

Test that the hypervisor is available:

```go
if err := cc.SupportsHypervisor(); err != nil {
    log.Fatal("Hypervisor unavailable:", err)
}
fmt.Println("Hypervisor is ready!")
```

## Building from Source

Clone the repository and use the build tool:

```bash
git clone https://github.com/tinyrange/cc.git
cd cc

# Build the cc CLI and ccapp
./tools/build.go

# Run tests
./tools/build.go -test ./...

# Build and run ccapp
./tools/build.go -app
```

## Next Steps

- [Try the Quick Start tutorial](/getting-started/quick-start/)
- [Learn about the Go API](/api/overview/)
- [Explore ccapp features](/app/overview/)
