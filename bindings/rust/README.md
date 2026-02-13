# cc Rust Bindings

Rust bindings for the cc virtualization library, providing a safe Rust interface to cc's virtualization primitives.

## Build Requirements

**IMPORTANT**: Building this crate requires Go 1.21+ to compile the native `libcc` library.

| Requirement | Version | Notes |
|-------------|---------|-------|
| Rust | 1.70+ | Stable toolchain |
| Go | 1.21+ | Required to build libcc |

The build.rs script will automatically compile libcc using Go if `LIBCC_PATH` is not set.

## Installation

### From crates.io

```toml
[dependencies]
cc-vm = "0.1"
```

Ensure Go is installed and available in your PATH:
```bash
# macOS
brew install go

# Ubuntu/Debian
sudo apt install golang

# Verify installation
go version
```

### From Source

1. Build the `libcc` shared library:
   ```bash
   cd /path/to/cc
   ./tools/build.go -bindings-c
   ```

2. Set the library path (or copy `libcc.dylib`/`libcc.so` to a standard location):
   ```bash
   export LIBCC_PATH=/path/to/cc/build/libcc.dylib
   ```

3. Add to Cargo.toml:
   ```toml
   [dependencies]
   cc-vm = { path = "/path/to/cc/bindings/rust" }
   ```

## Quick Start

```rust
use cc::{OciClient, Instance, InstanceOptions};

fn main() -> cc::Result<()> {
    // Initialize the library
    cc::init()?;

    // Check for hypervisor support
    if cc::supports_hypervisor()? {
        println!("Hypervisor available!");
    } else {
        println!("No hypervisor - some features may be limited");
        cc::shutdown();
        return Ok(());
    }

    // Query system capabilities
    let caps = cc::query_capabilities()?;
    println!("Architecture: {}", caps.architecture);

    // Pull an image and create an instance
    let client = OciClient::new()?;
    let source = client.pull("alpine:latest", None, None)?;

    // Get image config
    let config = source.get_config()?;
    println!("Image architecture: {:?}", config.architecture);

    // Create and run instance
    let opts = InstanceOptions {
        memory_mb: 512,
        cpus: 2,
        ..Default::default()
    };

    let inst = Instance::new(source, Some(opts))?;
    println!("Instance ID: {}", inst.id());

    // Run a command
    let output = inst.command("echo", &["Hello from Rust!"])?.output()?;
    println!("Output: {}", String::from_utf8_lossy(&output.stdout));

    // File operations
    inst.write_file("/tmp/test.txt", b"Hello, World!", 0o644)?;
    let data = inst.read_file("/tmp/test.txt")?;
    println!("Read: {}", String::from_utf8_lossy(&data));

    // Directory operations
    inst.mkdir("/tmp/mydir", 0o755)?;
    for entry in inst.read_dir("/tmp")? {
        println!("  {} (dir={})", entry.name, entry.is_dir);
    }

    // Cleanup happens automatically on drop
    cc::shutdown();
    Ok(())
}
```

## API Reference

### Module Functions

- `cc::init()` - Initialize the library (required before any other call)
- `cc::shutdown()` - Shutdown and release resources
- `cc::api_version()` - Get API version string
- `cc::api_version_compatible(major, minor)` - Check version compatibility
- `cc::supports_hypervisor()` - Check if hypervisor is available
- `cc::query_capabilities()` - Get system capabilities
- `cc::guest_protocol_version()` - Get guest protocol version

### Structs

#### `OciClient`

OCI image client for pulling and managing container images.

```rust
let client = OciClient::new()?;
let client = OciClient::with_cache_dir("/custom/cache")?;

let source = client.pull("alpine:latest", None, None)?;
let source = client.load_tar("/path/to/image.tar", None)?;
let source = client.load_dir("/path/to/extracted", None)?;
client.export_dir(&source, "/path/to/output")?;
```

#### `Instance`

A running VM instance with filesystem, command, and network access.

```rust
let inst = Instance::new(source, Some(opts))?;

// Properties
inst.id();           // Instance ID
inst.is_running();   // Check if running

// Lifecycle
inst.wait(None)?;    // Wait for termination
inst.close()?;       // Close and cleanup

// Filesystem
inst.read_file(path)?;
inst.write_file(path, data, mode)?;
inst.stat(path)?;
inst.mkdir(path, mode)?;
inst.remove(path)?;
inst.read_dir(path)?;

// File handles (implement std::io::Read/Write/Seek)
let mut f = inst.open(path)?;
let mut f = inst.create(path)?;

// Commands
let output = inst.command("echo", &["hello"])?.output()?;
let exit_code = inst.command("ls", &["-la"])?.run()?;

// Networking
let listener = inst.listen("tcp", ":8080")?;

// Snapshots
let snapshot = inst.snapshot(None)?;
```

#### `Cmd`

Command builder for execution in instances.

```rust
let cmd = inst.command("env", &[])?;
let cmd = cmd.dir("/tmp")?;
let cmd = cmd.env("MY_VAR", "value")?;

// Synchronous execution
let exit_code = cmd.run()?;

// Capture output
let output = cmd.output()?;
let combined = cmd.combined_output()?;

// Async execution
let mut cmd = inst.command("sleep", &["10"])?;
cmd.start()?;
// ... do other work ...
let exit_code = cmd.wait()?;

// Streaming I/O with pipes
let mut cmd = inst.command("cat", &[])?;
let mut stdin = cmd.stdin_pipe()?;    // Returns Conn (writable)
let mut stdout = cmd.stdout_pipe()?;  // Returns Conn (readable)
let mut stderr = cmd.stderr_pipe()?;  // Returns Conn (readable)
cmd.start()?;

stdin.write(b"hello")?;
stdin.close()?;                       // Close to signal EOF

let mut buf = [0u8; 256];
let n = stdout.read(&mut buf)?;       // Read output incrementally
stdout.close()?;
cmd.wait()?;
```

#### `File`

File handle for read/write operations. Implements `std::io::Read`, `Write`, and `Seek`.

```rust
use std::io::{Read, Write, Seek};

let mut f = inst.open(path)?;
let mut contents = String::new();
f.read_to_string(&mut contents)?;

let mut f = inst.create(path)?;
f.write_all(b"data")?;
f.seek(std::io::SeekFrom::Start(0))?;
```

### Types

- `InstanceOptions` - VM configuration (memory_mb, cpus, timeout_seconds, user, mounts)
- `PullOptions` - Image pull options (platform, auth, policy)
- `PullPolicy` - Pull policy enum (IfNotPresent, Always, Never)
- `FileInfo` - File metadata (name, size, mode, is_dir, is_symlink)
- `DirEntry` - Directory entry (name, is_dir, mode)
- `ImageConfig` - OCI image configuration
- `Capabilities` - System capabilities
- `SeekWhence` - Seek origin (Set, Current, End)
- `CommandOutput` - Command output with stdout and exit_code

### File Open Flags

```rust
use cc::flags::*;

O_RDONLY   // Read only
O_WRONLY   // Write only
O_RDWR     // Read and write
O_APPEND   // Append mode
O_CREATE   // Create if not exists
O_TRUNC    // Truncate to zero
O_EXCL     // Exclusive create (fail if exists)
```

### Error Handling

All operations return `cc::Result<T>`, which is `Result<T, cc::Error>`.

```rust
use cc::Error;

match result {
    Err(Error::InvalidHandle) => println!("Handle is invalid"),
    Err(Error::InvalidArgument(msg)) => println!("Invalid argument: {}", msg),
    Err(Error::NotRunning) => println!("Instance has terminated"),
    Err(Error::AlreadyClosed) => println!("Resource already closed"),
    Err(Error::Timeout) => println!("Operation timed out"),
    Err(Error::HypervisorUnavailable(msg)) => println!("No hypervisor: {}", msg),
    Err(Error::Io { message, op, path }) => println!("I/O error: {}", message),
    Err(Error::Network(msg)) => println!("Network error: {}", msg),
    Err(Error::Cancelled) => println!("Operation was cancelled"),
    Err(Error::Unknown(msg)) => println!("Unknown error: {}", msg),
    Ok(value) => { /* success */ }
}
```

## Code Signing

**Your Rust binary does NOT need code signing.**

The libcc shared library handles virtualization through cc-helper, which is already signed with the necessary entitlements. Your application simply links against libcc and does not require any special entitlements or code signing.

## Running Tests

```bash
# Build libcc first
./tools/build.go -bindings-c

# Set library path
export LIBCC_PATH=$(pwd)/build/libcc.dylib  # or .so on Linux

# Run tests (non-VM tests only)
cd bindings/rust
cargo test

# Run all tests including VM tests (requires hypervisor)
CC_RUN_VM_TESTS=1 cargo test
```

## Running Examples

```bash
# Set library path
export LIBCC_PATH=$(pwd)/build/libcc.dylib

# Run basic example
cargo run --example basic
```

## Thread Safety

All types in this crate implement `Send` and `Sync`, allowing them to be used across threads. However, operations on a single instance should be synchronized externally if accessed from multiple threads simultaneously.

## License

MIT
