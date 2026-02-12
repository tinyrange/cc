# CrumbleCracker Python Bindings

Python bindings for the CrumbleCracker virtualization library, providing a Pythonic interface to cc's virtualization primitives.

## Installation

### From PyPI (Recommended)

```bash
pip install crumblecracker
```

The PyPI package includes the native library (`libcc.so`/`libcc.dylib`) for supported platforms.

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

3. Install the package:
   ```bash
   pip install -e /path/to/cc/bindings/python
   ```

## Quick Start

```python
import crumblecracker as cc

# Initialize the library
cc.init()

# Check for hypervisor support
if cc.supports_hypervisor():
    print("Hypervisor available!")
else:
    print("No hypervisor - some features may be limited")

# Query system capabilities
caps = cc.query_capabilities()
print(f"Architecture: {caps.architecture}")

# Pull an image and create an instance
with cc.OCIClient() as client:
    # Pull with progress callback
    def on_progress(p):
        if p.total > 0:
            pct = p.current * 100 // p.total
            print(f"\rDownloading: {pct}%", end="")

    source = client.pull("alpine:latest", progress_callback=on_progress)
    print()  # newline after progress

    # Get image config
    config = source.get_config()
    print(f"Image architecture: {config.architecture}")

    # Create and run instance
    options = cc.InstanceOptions(memory_mb=512, cpus=2)
    with cc.Instance(source, options) as inst:
        print(f"Instance ID: {inst.id}")

        # Run a command
        output = inst.command("echo", "Hello from VM!").output()
        print(output.decode())

        # File operations
        inst.write_file("/tmp/test.txt", b"Hello, World!")
        data = inst.read_file("/tmp/test.txt")
        print(f"Read: {data.decode()}")

        # Directory operations
        inst.mkdir("/tmp/mydir")
        for entry in inst.read_dir("/tmp"):
            print(f"  {entry.name} (dir={entry.is_dir})")

# Cleanup
cc.shutdown()
```

## API Reference

### Module Functions

- `cc.init()` - Initialize the library (required before any other call)
- `cc.shutdown()` - Shutdown and release resources
- `cc.api_version()` - Get API version string
- `cc.api_version_compatible(major, minor)` - Check version compatibility
- `cc.supports_hypervisor()` - Check if hypervisor is available
- `cc.query_capabilities()` - Get system capabilities
- `cc.guest_protocol_version()` - Get guest protocol version

### Classes

#### `OCIClient`

OCI image client for pulling and managing container images.

```python
with cc.OCIClient(cache_dir=None) as client:
    source = client.pull("alpine:latest", options=None, progress_callback=None)
    source = client.load_tar("/path/to/image.tar")
    source = client.load_dir("/path/to/extracted")
    client.export_dir(source, "/path/to/output")
```

#### `Instance`

A running VM instance with filesystem, command, and network access.

```python
with cc.Instance(source, options) as inst:
    # Properties
    inst.id              # Instance ID
    inst.is_running      # Check if running

    # Lifecycle
    inst.wait()          # Wait for termination
    inst.close()         # Close and cleanup

    # Filesystem
    inst.read_file(path)
    inst.write_file(path, data, mode=0o644)
    inst.stat(path)
    inst.mkdir(path, mode=0o755)
    inst.remove(path)
    inst.read_dir(path)

    # File handles
    with inst.open(path) as f:
        data = f.read()
    with inst.create(path) as f:
        f.write(data)

    # Commands
    output = inst.command("echo", "hello").output()
    exit_code = inst.command("ls", "-la").run()

    # Networking
    listener = inst.listen("tcp", ":8080")

    # Snapshots
    snapshot = inst.snapshot_filesystem()
```

#### `Cmd`

Command builder for execution in instances.

```python
cmd = inst.command("env")
cmd.set_dir("/tmp")
cmd.set_env("MY_VAR", "value")

# Synchronous execution
exit_code = cmd.run()

# Capture output
stdout = cmd.output()
combined = cmd.combined_output()

# Async execution
cmd.start()
# ... do other work ...
exit_code = cmd.wait()

# Streaming I/O with pipes
cmd = inst.command("cat")
stdin = cmd.stdin_pipe()    # Returns Conn (writable)
stdout = cmd.stdout_pipe()  # Returns Conn (readable)
stderr = cmd.stderr_pipe()  # Returns Conn (readable)
cmd.start()

stdin.write(b"hello")
stdin.close()               # Close to signal EOF

data = stdout.read(256)     # Read output incrementally
stdout.close()
cmd.wait()
```

#### `File`

File handle for read/write operations.

```python
with inst.open(path) as f:
    data = f.read()
    data = f.read(1024)  # Read up to 1024 bytes
    f.seek(0, cc.SeekWhence.SET)
    info = f.stat()

with inst.create(path) as f:
    f.write(b"data")
    f.truncate(10)
    f.sync()
```

### Types

- `InstanceOptions` - VM configuration (memory_mb, cpus, timeout_seconds, user, mounts)
- `PullOptions` - Image pull options (platform, auth, policy)
- `PullPolicy` - Pull policy enum (IF_NOT_PRESENT, ALWAYS, NEVER)
- `FileInfo` - File metadata (name, size, mode, is_dir, is_symlink)
- `DirEntry` - Directory entry (name, is_dir, mode)
- `ImageConfig` - OCI image configuration
- `Capabilities` - System capabilities
- `SeekWhence` - Seek origin (SET, CUR, END)

### File Open Flags

```python
cc.O_RDONLY   # Read only
cc.O_WRONLY   # Write only
cc.O_RDWR     # Read and write
cc.O_APPEND   # Append mode
cc.O_CREATE   # Create if not exists
cc.O_TRUNC    # Truncate to zero
cc.O_EXCL     # Exclusive create
```

### Exceptions

All exceptions inherit from `cc.CCError`:

- `InvalidHandleError` - Handle is invalid or freed
- `InvalidArgumentError` - Invalid function argument
- `NotRunningError` - Instance has terminated
- `AlreadyClosedError` - Resource already closed
- `TimeoutError` - Operation timed out
- `HypervisorUnavailableError` - No hypervisor support
- `IOError` - Filesystem I/O error
- `NetworkError` - Network error
- `CancelledError` - Operation was cancelled

## Running Tests

```bash
# Install dev dependencies
pip install -e ".[dev]"

# Run tests (non-VM tests only)
pytest

# Run all tests including VM tests (requires hypervisor)
CC_RUN_VM_TESTS=1 pytest
```

## License

MIT
