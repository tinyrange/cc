# CC Language Bindings Plan

This document describes the design for CGO-based bindings that expose cc's virtualization capabilities to Python, Node.js, Bun, Rust, and C.

## Table of Contents

1. [Overview](#overview)
2. [Core C API Design](#core-c-api-design)
3. [Python Bindings](#python-bindings)
4. [Node.js/Bun Bindings](#nodejsbun-bindings)
5. [Rust Bindings](#rust-bindings)
6. [Pure C Usage](#pure-c-usage)
7. [Implementation Strategy](#implementation-strategy)

---

## Overview

The cc library provides virtualization primitives with APIs that mirror the Go standard library. The binding strategy is:

1. **CGO Layer**: Export a C-compatible API from Go using `//export` directives
2. **Language Wrappers**: Idiomatic wrappers for each target language
3. **Handle-Based Design**: Opaque handles for Go objects, preventing direct memory access from foreign code

### Design Principles

- **Handle-based opaque types**: All Go objects are referenced by integer handles
- **Explicit memory management**: `cc_*_free()` functions for all allocated resources
- **Error handling via structs**: Errors returned as `cc_error` with code and message
- **Thread safety**: The C API is thread-safe; Go's runtime handles synchronization
- **Mirrored APIs**: Language bindings mirror Go's `os`, `os/exec`, and `net` patterns

---

## Core C API Design

### Header File: `libcc.h`

```c
#ifndef LIBCC_H
#define LIBCC_H

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

/* ==========================================================================
 * Handle Types (opaque references to Go objects)
 * ========================================================================== */

typedef uint64_t cc_handle;

#define CC_INVALID_HANDLE 0

typedef cc_handle cc_oci_client;
typedef cc_handle cc_instance_source;
typedef cc_handle cc_instance;
typedef cc_handle cc_file;
typedef cc_handle cc_cmd;
typedef cc_handle cc_listener;
typedef cc_handle cc_conn;
typedef cc_handle cc_snapshot;
typedef cc_handle cc_snapshot_factory;

/* ==========================================================================
 * Error Handling
 * ========================================================================== */

typedef enum {
    CC_OK = 0,
    CC_ERR_INVALID_HANDLE = 1,
    CC_ERR_INVALID_ARGUMENT = 2,
    CC_ERR_NOT_RUNNING = 3,
    CC_ERR_ALREADY_CLOSED = 4,
    CC_ERR_TIMEOUT = 5,
    CC_ERR_HYPERVISOR_UNAVAILABLE = 6,
    CC_ERR_IO = 7,
    CC_ERR_NETWORK = 8,
    CC_ERR_UNKNOWN = 99
} cc_error_code;

typedef struct {
    cc_error_code code;
    char* message;      // Caller must free with cc_free_string()
    char* op;           // Operation that failed (may be NULL)
    char* path;         // Path involved (may be NULL)
} cc_error;

// Free error message strings
void cc_error_free(cc_error* err);

// Free a string allocated by the library
void cc_free_string(char* str);

// Free a byte buffer allocated by the library
void cc_free_bytes(uint8_t* buf);

/* ==========================================================================
 * Library Initialization
 * ========================================================================== */

// Initialize the library. Must be called before any other function.
// Returns CC_OK on success.
cc_error_code cc_init(void);

// Shutdown the library and release all resources.
void cc_shutdown(void);

// Check if hypervisor is available on this system.
// Returns CC_OK if available, CC_ERR_HYPERVISOR_UNAVAILABLE otherwise.
cc_error_code cc_supports_hypervisor(cc_error* err);

/* ==========================================================================
 * OCI Client - Image Management
 * ========================================================================== */

// Create a new OCI client with default cache directory.
cc_error_code cc_oci_client_new(cc_oci_client* out, cc_error* err);

// Create OCI client with custom cache directory.
cc_error_code cc_oci_client_new_with_cache(
    const char* cache_dir,
    cc_oci_client* out,
    cc_error* err
);

// Free an OCI client.
void cc_oci_client_free(cc_oci_client client);

// Pull policy for image fetching.
typedef enum {
    CC_PULL_IF_NOT_PRESENT = 0,
    CC_PULL_ALWAYS = 1,
    CC_PULL_NEVER = 2
} cc_pull_policy;

// Options for pulling images.
typedef struct {
    const char* platform_os;    // e.g., "linux" (NULL for default)
    const char* platform_arch;  // e.g., "amd64", "arm64" (NULL for default)
    const char* username;       // Registry auth (NULL for anonymous)
    const char* password;       // Registry auth (NULL for anonymous)
    cc_pull_policy policy;
} cc_pull_options;

// Progress callback for downloads.
typedef struct {
    int64_t current;            // Bytes downloaded so far
    int64_t total;              // Total bytes (-1 if unknown)
    const char* filename;       // Current file being downloaded
    int blob_index;             // Current blob index (0-based)
    int blob_count;             // Total number of blobs
    double bytes_per_second;    // Download speed
    double eta_seconds;         // Estimated time remaining (-1 if unknown)
} cc_download_progress;

typedef void (*cc_progress_callback)(const cc_download_progress* progress, void* user_data);

// Pull an OCI image from a registry.
cc_error_code cc_oci_client_pull(
    cc_oci_client client,
    const char* image_ref,
    const cc_pull_options* opts,        // May be NULL for defaults
    cc_progress_callback progress_cb,   // May be NULL
    void* progress_user_data,
    cc_instance_source* out,
    cc_error* err
);

// Load image from a local tar file (docker save format).
cc_error_code cc_oci_client_load_tar(
    cc_oci_client client,
    const char* tar_path,
    const cc_pull_options* opts,
    cc_instance_source* out,
    cc_error* err
);

// Load image from a prebaked directory.
cc_error_code cc_oci_client_load_dir(
    cc_oci_client client,
    const char* dir_path,
    const cc_pull_options* opts,
    cc_instance_source* out,
    cc_error* err
);

// Export an instance source to a directory.
cc_error_code cc_oci_client_export_dir(
    cc_oci_client client,
    cc_instance_source source,
    const char* dir_path,
    cc_error* err
);

// Get cache directory path. Caller must free with cc_free_string().
char* cc_oci_client_cache_dir(cc_oci_client client);

// Free an instance source.
void cc_instance_source_free(cc_instance_source source);

/* ==========================================================================
 * Image Configuration
 * ========================================================================== */

typedef struct {
    char* architecture;     // "amd64", "arm64", etc.
    char** env;             // NULL-terminated array of "KEY=VALUE" strings
    size_t env_count;
    char* working_dir;
    char** entrypoint;      // NULL-terminated array
    size_t entrypoint_count;
    char** cmd;             // NULL-terminated array
    size_t cmd_count;
    char* user;
} cc_image_config;

// Get image configuration from a source. Caller must free with cc_image_config_free().
cc_error_code cc_source_get_config(
    cc_instance_source source,
    cc_image_config** out,
    cc_error* err
);

void cc_image_config_free(cc_image_config* config);

/* ==========================================================================
 * Instance Creation and Lifecycle
 * ========================================================================== */

// Mount configuration for virtio-fs.
typedef struct {
    const char* tag;        // Mount tag (guest uses: mount -t virtiofs <tag> /mnt)
    const char* host_path;  // Host directory (NULL for empty writable fs)
    bool writable;          // Read-only by default
} cc_mount_config;

// Instance creation options.
typedef struct {
    uint64_t memory_mb;         // Memory in MB (default: 256)
    int cpus;                   // Number of vCPUs (default: 1)
    double timeout_seconds;     // Instance timeout (0 for no timeout)
    const char* user;           // User:group to run as (e.g., "1000:1000")
    bool enable_gpu;            // Enable virtio-gpu
    bool enable_dmesg;          // Enable kernel dmesg output
    const cc_mount_config* mounts;
    size_t mount_count;
} cc_instance_options;

// Create and start a new instance from a source.
cc_error_code cc_instance_new(
    cc_instance_source source,
    const cc_instance_options* opts,    // May be NULL for defaults
    cc_instance* out,
    cc_error* err
);

// Close an instance and release resources.
cc_error_code cc_instance_close(cc_instance inst, cc_error* err);

// Wait for an instance to terminate.
cc_error_code cc_instance_wait(cc_instance inst, cc_error* err);

// Get instance ID. Caller must free with cc_free_string().
char* cc_instance_id(cc_instance inst);

// Check if instance is still running.
bool cc_instance_is_running(cc_instance inst);

// Set console size (for interactive mode).
void cc_instance_set_console_size(cc_instance inst, int cols, int rows);

// Enable/disable network access.
void cc_instance_set_network_enabled(cc_instance inst, bool enabled);
```

/* ==========================================================================
 * Filesystem Operations (mirrors Go's os package)
 * ========================================================================== */

// File open flags (match POSIX).
#define CC_O_RDONLY    0x0000
#define CC_O_WRONLY    0x0001
#define CC_O_RDWR      0x0002
#define CC_O_APPEND    0x0008
#define CC_O_CREATE    0x0200
#define CC_O_TRUNC     0x0400
#define CC_O_EXCL      0x0800

// File mode/permission bits.
typedef uint32_t cc_file_mode;

// File information structure.
typedef struct {
    char* name;
    int64_t size;
    cc_file_mode mode;
    int64_t mod_time_unix;  // Unix timestamp (seconds)
    bool is_dir;
    bool is_symlink;
} cc_file_info;

void cc_file_info_free(cc_file_info* info);

// Directory entry.
typedef struct {
    char* name;
    bool is_dir;
    cc_file_mode mode;
} cc_dir_entry;

void cc_dir_entries_free(cc_dir_entry* entries, size_t count);

// Open a file for reading.
cc_error_code cc_fs_open(
    cc_instance inst,
    const char* path,
    cc_file* out,
    cc_error* err
);

// Create or truncate a file.
cc_error_code cc_fs_create(
    cc_instance inst,
    const char* path,
    cc_file* out,
    cc_error* err
);

// Open a file with flags and permissions.
cc_error_code cc_fs_open_file(
    cc_instance inst,
    const char* path,
    int flags,
    cc_file_mode perm,
    cc_file* out,
    cc_error* err
);

// Close a file.
cc_error_code cc_file_close(cc_file f, cc_error* err);

// Read from a file. Returns bytes read in *n.
cc_error_code cc_file_read(
    cc_file f,
    uint8_t* buf,
    size_t len,
    size_t* n,
    cc_error* err
);

// Write to a file. Returns bytes written in *n.
cc_error_code cc_file_write(
    cc_file f,
    const uint8_t* buf,
    size_t len,
    size_t* n,
    cc_error* err
);

// Seek within a file.
typedef enum {
    CC_SEEK_SET = 0,
    CC_SEEK_CUR = 1,
    CC_SEEK_END = 2
} cc_seek_whence;

cc_error_code cc_file_seek(
    cc_file f,
    int64_t offset,
    cc_seek_whence whence,
    int64_t* new_offset,
    cc_error* err
);

// Sync file to disk.
cc_error_code cc_file_sync(cc_file f, cc_error* err);

// Truncate file to size.
cc_error_code cc_file_truncate(cc_file f, int64_t size, cc_error* err);

// Get file info.
cc_error_code cc_file_stat(cc_file f, cc_file_info* out, cc_error* err);

// Get file name.
char* cc_file_name(cc_file f);

// Read entire file contents. Caller must free with cc_free_bytes().
cc_error_code cc_fs_read_file(
    cc_instance inst,
    const char* path,
    uint8_t** out,
    size_t* len,
    cc_error* err
);

// Write entire file contents.
cc_error_code cc_fs_write_file(
    cc_instance inst,
    const char* path,
    const uint8_t* data,
    size_t len,
    cc_file_mode perm,
    cc_error* err
);

// Get file info by path.
cc_error_code cc_fs_stat(
    cc_instance inst,
    const char* path,
    cc_file_info* out,
    cc_error* err
);

// Get file info (don't follow symlinks).
cc_error_code cc_fs_lstat(
    cc_instance inst,
    const char* path,
    cc_file_info* out,
    cc_error* err
);

// Remove a file.
cc_error_code cc_fs_remove(
    cc_instance inst,
    const char* path,
    cc_error* err
);

// Remove a file or directory recursively.
cc_error_code cc_fs_remove_all(
    cc_instance inst,
    const char* path,
    cc_error* err
);

// Create a directory.
cc_error_code cc_fs_mkdir(
    cc_instance inst,
    const char* path,
    cc_file_mode perm,
    cc_error* err
);

// Create a directory and all parents.
cc_error_code cc_fs_mkdir_all(
    cc_instance inst,
    const char* path,
    cc_file_mode perm,
    cc_error* err
);

// Rename a file or directory.
cc_error_code cc_fs_rename(
    cc_instance inst,
    const char* oldpath,
    const char* newpath,
    cc_error* err
);

// Create a symbolic link.
cc_error_code cc_fs_symlink(
    cc_instance inst,
    const char* oldname,
    const char* newname,
    cc_error* err
);

// Read a symbolic link. Caller must free with cc_free_string().
cc_error_code cc_fs_readlink(
    cc_instance inst,
    const char* path,
    char** out,
    cc_error* err
);

// Read directory contents.
cc_error_code cc_fs_read_dir(
    cc_instance inst,
    const char* path,
    cc_dir_entry** out,
    size_t* count,
    cc_error* err
);

// Change file mode.
cc_error_code cc_fs_chmod(
    cc_instance inst,
    const char* path,
    cc_file_mode mode,
    cc_error* err
);

// Change file owner.
cc_error_code cc_fs_chown(
    cc_instance inst,
    const char* path,
    int uid,
    int gid,
    cc_error* err
);

// Change file times.
cc_error_code cc_fs_chtimes(
    cc_instance inst,
    const char* path,
    int64_t atime_unix,
    int64_t mtime_unix,
    cc_error* err
);

/* ==========================================================================
 * Command Execution (mirrors Go's os/exec package)
 * ========================================================================== */

// Create a command to run in the instance.
cc_error_code cc_cmd_new(
    cc_instance inst,
    const char* name,
    const char* const* args,    // NULL-terminated array
    cc_cmd* out,
    cc_error* err
);

// Create a command using the container's entrypoint.
cc_error_code cc_cmd_entrypoint(
    cc_instance inst,
    const char* const* args,    // Optional override args (NULL for default CMD)
    cc_cmd* out,
    cc_error* err
);

// Free a command (if not yet started).
void cc_cmd_free(cc_cmd cmd);

// Set working directory.
cc_error_code cc_cmd_set_dir(cc_cmd cmd, const char* dir, cc_error* err);

// Set an environment variable.
cc_error_code cc_cmd_set_env(cc_cmd cmd, const char* key, const char* value, cc_error* err);

// Get an environment variable. Caller must free with cc_free_string().
char* cc_cmd_get_env(cc_cmd cmd, const char* key);

// Get all environment variables. Caller must free array and strings.
cc_error_code cc_cmd_environ(
    cc_cmd cmd,
    char*** out,
    size_t* count,
    cc_error* err
);

// Pipe handles for stdin/stdout/stderr.
typedef cc_handle cc_write_pipe;
typedef cc_handle cc_read_pipe;

// Get stdin pipe (for writing to process).
cc_error_code cc_cmd_stdin_pipe(cc_cmd cmd, cc_write_pipe* out, cc_error* err);

// Get stdout pipe (for reading from process).
cc_error_code cc_cmd_stdout_pipe(cc_cmd cmd, cc_read_pipe* out, cc_error* err);

// Get stderr pipe (for reading from process).
cc_error_code cc_cmd_stderr_pipe(cc_cmd cmd, cc_read_pipe* out, cc_error* err);

// Write to a pipe.
cc_error_code cc_write_pipe_write(
    cc_write_pipe p,
    const uint8_t* buf,
    size_t len,
    size_t* n,
    cc_error* err
);

// Close a write pipe.
cc_error_code cc_write_pipe_close(cc_write_pipe p, cc_error* err);

// Read from a pipe.
cc_error_code cc_read_pipe_read(
    cc_read_pipe p,
    uint8_t* buf,
    size_t len,
    size_t* n,
    cc_error* err
);

// Close a read pipe.
cc_error_code cc_read_pipe_close(cc_read_pipe p, cc_error* err);

// Start the command (non-blocking).
cc_error_code cc_cmd_start(cc_cmd cmd, cc_error* err);

// Wait for command to complete. Returns exit code in *exit_code.
cc_error_code cc_cmd_wait(cc_cmd cmd, int* exit_code, cc_error* err);

// Run command and wait for completion.
cc_error_code cc_cmd_run(cc_cmd cmd, int* exit_code, cc_error* err);

// Run command and capture stdout. Caller must free with cc_free_bytes().
cc_error_code cc_cmd_output(
    cc_cmd cmd,
    uint8_t** out,
    size_t* len,
    int* exit_code,
    cc_error* err
);

// Run command and capture stdout+stderr. Caller must free with cc_free_bytes().
cc_error_code cc_cmd_combined_output(
    cc_cmd cmd,
    uint8_t** out,
    size_t* len,
    int* exit_code,
    cc_error* err
);

// Get exit code (after Wait).
int cc_cmd_exit_code(cc_cmd cmd);

// Replace init process with command (terminal operation).
cc_error_code cc_instance_exec(
    cc_instance inst,
    const char* name,
    const char* const* args,
    cc_error* err
);

/* ==========================================================================
 * Networking (mirrors Go's net package)
 * ========================================================================== */

// Listen for connections on the guest network.
cc_error_code cc_net_listen(
    cc_instance inst,
    const char* network,    // "tcp", "tcp4"
    const char* address,    // e.g., ":8080", "0.0.0.0:80"
    cc_listener* out,
    cc_error* err
);

// Accept a connection from a listener.
cc_error_code cc_listener_accept(
    cc_listener ln,
    cc_conn* out,
    cc_error* err
);

// Close a listener.
cc_error_code cc_listener_close(cc_listener ln, cc_error* err);

// Get listener address. Caller must free with cc_free_string().
char* cc_listener_addr(cc_listener ln);

// Dial to a remote address (from the guest's perspective).
cc_error_code cc_net_dial(
    cc_instance inst,
    const char* network,    // "tcp", "tcp4", "udp"
    const char* address,    // e.g., "example.com:80"
    cc_conn* out,
    cc_error* err
);

// Read from a connection.
cc_error_code cc_conn_read(
    cc_conn c,
    uint8_t* buf,
    size_t len,
    size_t* n,
    cc_error* err
);

// Write to a connection.
cc_error_code cc_conn_write(
    cc_conn c,
    const uint8_t* buf,
    size_t len,
    size_t* n,
    cc_error* err
);

// Close a connection.
cc_error_code cc_conn_close(cc_conn c, cc_error* err);

// Get local address. Caller must free with cc_free_string().
char* cc_conn_local_addr(cc_conn c);

// Get remote address. Caller must free with cc_free_string().
char* cc_conn_remote_addr(cc_conn c);

// Set read deadline (Unix timestamp, 0 to clear).
cc_error_code cc_conn_set_read_deadline(cc_conn c, int64_t unix_time, cc_error* err);

// Set write deadline (Unix timestamp, 0 to clear).
cc_error_code cc_conn_set_write_deadline(cc_conn c, int64_t unix_time, cc_error* err);

/* ==========================================================================
 * GPU Operations (virtio-gpu)
 * ========================================================================== */

typedef cc_handle cc_gpu;

// Get GPU interface (returns CC_INVALID_HANDLE if GPU not enabled).
cc_gpu cc_instance_gpu(cc_instance inst);

// Set the window for rendering (platform-specific window handle).
// On macOS: NSWindow*, on Linux: X11 Window or Wayland surface.
cc_error_code cc_gpu_set_window(cc_gpu gpu, void* window, cc_error* err);

// Poll for window events. Returns false if window closed.
bool cc_gpu_poll(cc_gpu gpu);

// Render current framebuffer to window.
void cc_gpu_render(cc_gpu gpu);

// Swap window buffers.
void cc_gpu_swap(cc_gpu gpu);

// Framebuffer data.
typedef struct {
    uint8_t* pixels;    // BGRA format, caller must NOT free
    uint32_t width;
    uint32_t height;
    bool valid;
} cc_framebuffer;

// Get current framebuffer (pixels valid until next render).
cc_framebuffer cc_gpu_get_framebuffer(cc_gpu gpu);

/* ==========================================================================
 * Filesystem Snapshots
 * ========================================================================== */

// Snapshot options.
typedef struct {
    const char* const* excludes;    // NULL-terminated array of glob patterns
    size_t exclude_count;
    const char* cache_dir;          // Cache directory for layers
} cc_snapshot_options;

// Take a filesystem snapshot.
cc_error_code cc_fs_snapshot(
    cc_instance inst,
    const cc_snapshot_options* opts,
    cc_snapshot* out,
    cc_error* err
);

// Get snapshot cache key. Caller must free with cc_free_string().
char* cc_snapshot_cache_key(cc_snapshot snap);

// Get parent snapshot (returns CC_INVALID_HANDLE if none).
cc_snapshot cc_snapshot_parent(cc_snapshot snap);

// Free a snapshot.
cc_error_code cc_snapshot_close(cc_snapshot snap, cc_error* err);

// A snapshot can be used as an instance source.
cc_instance_source cc_snapshot_as_source(cc_snapshot snap);

/* ==========================================================================
 * Filesystem Snapshot Factory (Dockerfile-like builder)
 * ========================================================================== */

// Create a new snapshot factory.
cc_error_code cc_snapshot_factory_new(
    cc_oci_client client,
    const char* cache_dir,
    cc_snapshot_factory* out,
    cc_error* err
);

// Set base image (must be called first).
cc_error_code cc_snapshot_factory_from(
    cc_snapshot_factory factory,
    const char* image_ref,
    cc_error* err
);

// Add a run command.
cc_error_code cc_snapshot_factory_run(
    cc_snapshot_factory factory,
    const char* const* cmd,     // NULL-terminated
    cc_error* err
);

// Copy a file from host to guest.
cc_error_code cc_snapshot_factory_copy(
    cc_snapshot_factory factory,
    const char* src,
    const char* dst,
    cc_error* err
);

// Copy data from buffer to guest.
cc_error_code cc_snapshot_factory_copy_bytes(
    cc_snapshot_factory factory,
    const uint8_t* data,
    size_t len,
    const char* dst,
    cc_error* err
);

// Set environment variables for subsequent Run commands.
cc_error_code cc_snapshot_factory_env(
    cc_snapshot_factory factory,
    const char* const* env,     // NULL-terminated "KEY=VALUE" strings
    cc_error* err
);

// Set working directory for subsequent Run commands.
cc_error_code cc_snapshot_factory_workdir(
    cc_snapshot_factory factory,
    const char* dir,
    cc_error* err
);

// Add exclude patterns for snapshots.
cc_error_code cc_snapshot_factory_exclude(
    cc_snapshot_factory factory,
    const char* const* patterns,    // NULL-terminated
    cc_error* err
);

// Build the snapshot (executes all operations).
cc_error_code cc_snapshot_factory_build(
    cc_snapshot_factory factory,
    cc_snapshot* out,
    cc_error* err
);

// Free a snapshot factory.
void cc_snapshot_factory_free(cc_snapshot_factory factory);

/* ==========================================================================
 * Dockerfile Building
 * ========================================================================== */

typedef struct {
    const char* context_dir;        // Build context directory
    const char* const* build_args;  // NULL-terminated "KEY=VALUE" strings
    size_t build_arg_count;
    const char* cache_dir;
} cc_dockerfile_options;

// Build an instance source from Dockerfile content.
cc_error_code cc_build_dockerfile(
    const uint8_t* dockerfile,
    size_t len,
    cc_oci_client client,
    const cc_dockerfile_options* opts,
    cc_snapshot* out,
    cc_error* err
);

// Runtime config parsed from Dockerfile.
typedef struct {
    char** cmd;
    size_t cmd_count;
    char** entrypoint;
    size_t entrypoint_count;
    char* user;
    char* workdir;
    char** env;
    size_t env_count;
} cc_dockerfile_runtime_config;

// Parse Dockerfile and get runtime config (without building).
cc_error_code cc_parse_dockerfile_config(
    const uint8_t* dockerfile,
    size_t len,
    const cc_dockerfile_options* opts,
    cc_dockerfile_runtime_config** out,
    cc_error* err
);

void cc_dockerfile_runtime_config_free(cc_dockerfile_runtime_config* config);

#ifdef __cplusplus
}
#endif

#endif /* LIBCC_H */
```

---

## Python Bindings

The Python bindings use `ctypes` or `cffi` to wrap the C API, providing a Pythonic interface with context managers, async support, and type hints.

### Installation

```bash
pip install cc-python
```

### Module Structure

```
cc/
├── __init__.py         # Main exports
├── _ffi.py            # ctypes/cffi bindings to libcc
├── client.py          # OCIClient
├── instance.py        # Instance, FS, Exec, Net
├── cmd.py             # Cmd class
├── snapshot.py        # Snapshot and SnapshotFactory
├── types.py           # Enums, dataclasses
└── errors.py          # Exception hierarchy
```

### Core Classes

```python
# cc/__init__.py
"""
CC - Virtualization primitives with familiar APIs.

Example:
    import cc

    with cc.OCIClient() as client:
        source = client.pull("alpine:latest")
        with cc.Instance(source, memory_mb=512) as inst:
            result = inst.command("echo", "Hello").output()
            print(result.decode())
"""

from .client import OCIClient
from .instance import Instance
from .snapshot import Snapshot, SnapshotFactory
from .types import (
    PullPolicy,
    InstanceOptions,
    MountConfig,
    DownloadProgress,
    ImageConfig,
)
from .errors import (
    CCError,
    HypervisorUnavailable,
    NotRunning,
    Timeout,
)

__all__ = [
    "OCIClient",
    "Instance",
    "Snapshot",
    "SnapshotFactory",
    "PullPolicy",
    "InstanceOptions",
    "MountConfig",
    "DownloadProgress",
    "ImageConfig",
    "CCError",
    "HypervisorUnavailable",
    "NotRunning",
    "Timeout",
    "supports_hypervisor",
]

def supports_hypervisor() -> bool:
    """Check if hypervisor is available on this system."""
    ...
```

### Error Handling

```python
# cc/errors.py
from dataclasses import dataclass
from typing import Optional

class CCError(Exception):
    """Base exception for all CC errors."""
    def __init__(self, message: str, op: Optional[str] = None, path: Optional[str] = None):
        self.op = op
        self.path = path
        super().__init__(message)

class HypervisorUnavailable(CCError):
    """Hypervisor is not available on this system."""
    pass

class NotRunning(CCError):
    """Instance is not running."""
    pass

class AlreadyClosed(CCError):
    """Resource has already been closed."""
    pass

class Timeout(CCError):
    """Operation timed out."""
    pass
```

### OCI Client

```python
# cc/client.py
from typing import Optional, Callable, Iterator
from contextlib import contextmanager
from pathlib import Path
from .types import PullPolicy, DownloadProgress, ImageConfig
from ._ffi import lib, ffi

class InstanceSource:
    """Opaque reference to an image source."""

    def __init__(self, handle: int):
        self._handle = handle

    def __del__(self):
        if self._handle:
            lib.cc_instance_source_free(self._handle)

    @property
    def config(self) -> Optional[ImageConfig]:
        """Get image configuration if available."""
        ...

class OCIClient:
    """Client for pulling and managing OCI images.

    Example:
        with OCIClient() as client:
            source = client.pull("alpine:latest")
            # Use source to create instances

        # Or with custom cache:
        client = OCIClient(cache_dir="/path/to/cache")
    """

    def __init__(self, cache_dir: Optional[str] = None):
        self._handle = self._create(cache_dir)

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.close()

    def close(self):
        """Release client resources."""
        if self._handle:
            lib.cc_oci_client_free(self._handle)
            self._handle = 0

    def pull(
        self,
        image_ref: str,
        *,
        platform: Optional[tuple[str, str]] = None,  # (os, arch)
        auth: Optional[tuple[str, str]] = None,      # (username, password)
        policy: PullPolicy = PullPolicy.IF_NOT_PRESENT,
        on_progress: Optional[Callable[[DownloadProgress], None]] = None,
    ) -> InstanceSource:
        """Pull an image from a registry.

        Args:
            image_ref: Image reference (e.g., "alpine:latest", "ghcr.io/org/image:tag")
            platform: Target platform (os, arch). Defaults to host platform.
            auth: Registry credentials (username, password).
            policy: When to pull from registry vs use cache.
            on_progress: Callback for download progress updates.

        Returns:
            InstanceSource that can be used with Instance().

        Example:
            source = client.pull(
                "ubuntu:22.04",
                platform=("linux", "arm64"),
                on_progress=lambda p: print(f"{p.current}/{p.total}")
            )
        """
        ...

    def load_tar(self, path: str | Path) -> InstanceSource:
        """Load an image from a tar file (docker save format)."""
        ...

    def load_dir(self, path: str | Path) -> InstanceSource:
        """Load a prebaked image from a directory."""
        ...

    def export_dir(self, source: InstanceSource, path: str | Path) -> None:
        """Export an image source to a directory."""
        ...

    @property
    def cache_dir(self) -> str:
        """Get the cache directory path."""
        ...
```

### Instance

```python
# cc/instance.py
from typing import Optional, Iterator, BinaryIO
from pathlib import Path
from contextlib import contextmanager
import os
from .types import InstanceOptions, MountConfig, FileInfo
from .cmd import Cmd

class File:
    """An open file in the guest filesystem.

    Implements the file protocol (read, write, seek, close).
    Can be used as a context manager.
    """

    def __init__(self, handle: int):
        self._handle = handle

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.close()

    def read(self, size: int = -1) -> bytes:
        """Read up to size bytes (-1 for all)."""
        ...

    def write(self, data: bytes) -> int:
        """Write data, return bytes written."""
        ...

    def seek(self, offset: int, whence: int = os.SEEK_SET) -> int:
        """Seek to position, return new position."""
        ...

    def close(self) -> None:
        """Close the file."""
        ...

    def stat(self) -> FileInfo:
        """Get file information."""
        ...

    @property
    def name(self) -> str:
        """Get file name."""
        ...

class Instance:
    """A running virtual machine instance.

    Provides filesystem, command execution, and networking APIs
    that mirror Python's os, subprocess, and socket modules.

    Example:
        with Instance(source, memory_mb=512, cpus=2) as inst:
            # Filesystem operations (like os module)
            inst.write_file("/tmp/hello.txt", b"Hello, World!")
            data = inst.read_file("/tmp/hello.txt")

            # Command execution (like subprocess)
            result = inst.command("cat", "/etc/os-release").output()

            # Or use the context manager pattern:
            with inst.open("/etc/passwd") as f:
                print(f.read())
    """

    def __init__(
        self,
        source: InstanceSource,
        *,
        memory_mb: int = 256,
        cpus: int = 1,
        timeout: Optional[float] = None,
        user: Optional[str] = None,
        mounts: Optional[list[MountConfig]] = None,
        gpu: bool = False,
        dmesg: bool = False,
    ):
        self._handle = self._create(source, InstanceOptions(...))

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.close()

    def close(self) -> None:
        """Close the instance and release resources."""
        ...

    def wait(self) -> None:
        """Wait for the instance to terminate."""
        ...

    @property
    def id(self) -> str:
        """Get instance ID."""
        ...

    # ========== Filesystem Operations (os module pattern) ==========

    def open(self, path: str, mode: str = "r") -> File:
        """Open a file in the guest.

        Args:
            path: File path in guest
            mode: Open mode ("r", "w", "a", "rb", "wb", etc.)
        """
        ...

    def read_file(self, path: str) -> bytes:
        """Read entire file contents."""
        ...

    def write_file(self, path: str, data: bytes, mode: int = 0o644) -> None:
        """Write data to a file."""
        ...

    def stat(self, path: str) -> FileInfo:
        """Get file information."""
        ...

    def lstat(self, path: str) -> FileInfo:
        """Get file information (don't follow symlinks)."""
        ...

    def exists(self, path: str) -> bool:
        """Check if path exists."""
        try:
            self.stat(path)
            return True
        except CCError:
            return False

    def remove(self, path: str) -> None:
        """Remove a file."""
        ...

    def rmtree(self, path: str) -> None:
        """Remove directory tree recursively."""
        ...

    def mkdir(self, path: str, mode: int = 0o755) -> None:
        """Create a directory."""
        ...

    def makedirs(self, path: str, mode: int = 0o755, exist_ok: bool = False) -> None:
        """Create directory and parents."""
        ...

    def rename(self, src: str, dst: str) -> None:
        """Rename a file or directory."""
        ...

    def symlink(self, src: str, dst: str) -> None:
        """Create a symbolic link."""
        ...

    def readlink(self, path: str) -> str:
        """Read symbolic link target."""
        ...

    def listdir(self, path: str) -> list[str]:
        """List directory contents."""
        ...

    def chmod(self, path: str, mode: int) -> None:
        """Change file mode."""
        ...

    def chown(self, path: str, uid: int, gid: int) -> None:
        """Change file owner."""
        ...

    # ========== Command Execution (subprocess pattern) ==========

    def command(self, name: str, *args: str) -> Cmd:
        """Create a command to run in the guest.

        Example:
            # Simple command
            output = inst.command("echo", "hello").output()

            # With environment and working directory
            cmd = inst.command("make", "build")
            cmd.env["CC"] = "gcc"
            cmd.cwd = "/src"
            result = cmd.run()
        """
        ...

    def entrypoint(self, *args: str) -> Cmd:
        """Create a command using the container's entrypoint."""
        ...

    def exec(self, name: str, *args: str) -> None:
        """Replace init with command (terminal operation)."""
        ...

    # ========== Networking ==========

    def listen(self, address: str, network: str = "tcp") -> "Listener":
        """Listen for connections on guest network.

        Example:
            with inst.listen(":8080") as ln:
                conn = ln.accept()
                data = conn.recv(1024)
        """
        ...

    # ========== Snapshots ==========

    def snapshot(
        self,
        *,
        excludes: Optional[list[str]] = None,
        cache_dir: Optional[str] = None,
    ) -> "Snapshot":
        """Take a filesystem snapshot.

        The snapshot can be used as an InstanceSource to create new instances.
        """
        ...

    # ========== GPU ==========

    @property
    def gpu(self) -> Optional["GPU"]:
        """Get GPU interface if enabled."""
        ...

    def set_console_size(self, cols: int, rows: int) -> None:
        """Set console size."""
        ...

    def set_network_enabled(self, enabled: bool) -> None:
        """Enable/disable network access."""
        ...
```

### Command Execution

```python
# cc/cmd.py
from typing import Optional, BinaryIO
from dataclasses import dataclass, field

@dataclass
class CompletedProcess:
    """Result of a completed command."""
    returncode: int
    stdout: Optional[bytes] = None
    stderr: Optional[bytes] = None

class Cmd:
    """A command ready to run in the guest.

    Example:
        # Simple execution
        cmd = inst.command("ls", "-la")
        result = cmd.run()
        print(f"Exit code: {result.returncode}")

        # Capture output
        output = inst.command("cat", "/etc/passwd").output()

        # With pipes
        cmd = inst.command("python", "script.py")
        cmd.stdin = b"input data"
        result = cmd.run(capture_output=True)

        # Streaming I/O
        with inst.command("long_running_cmd").popen() as proc:
            for line in proc.stdout:
                print(line)
    """

    def __init__(self, handle: int):
        self._handle = handle
        self._env: dict[str, str] = {}
        self._cwd: Optional[str] = None
        self._stdin: Optional[bytes] = None

    @property
    def env(self) -> dict[str, str]:
        """Environment variables for the command."""
        return self._env

    @property
    def cwd(self) -> Optional[str]:
        """Working directory."""
        return self._cwd

    @cwd.setter
    def cwd(self, value: str):
        self._cwd = value

    def run(self, *, capture_output: bool = False) -> CompletedProcess:
        """Run command and wait for completion."""
        ...

    def output(self) -> bytes:
        """Run command and return stdout."""
        ...

    def combined_output(self) -> bytes:
        """Run command and return stdout + stderr."""
        ...

    @contextmanager
    def popen(self):
        """Start command and return process handle for streaming I/O.

        Example:
            with cmd.popen() as proc:
                proc.stdin.write(b"input\\n")
                proc.stdin.close()
                output = proc.stdout.read()
                proc.wait()
        """
        ...
```

### Async Support

```python
# cc/async_instance.py
import asyncio
from typing import Optional
from .instance import Instance, InstanceSource

class AsyncInstance:
    """Async wrapper for Instance.

    Example:
        async with AsyncInstance(source) as inst:
            output = await inst.command("echo", "hello").output()

            # Concurrent file operations
            results = await asyncio.gather(
                inst.read_file("/etc/passwd"),
                inst.read_file("/etc/group"),
            )
    """

    def __init__(self, source: InstanceSource, **kwargs):
        self._inst = Instance(source, **kwargs)
        self._executor = None

    async def __aenter__(self):
        return self

    async def __aexit__(self, *args):
        await self.close()

    async def close(self):
        loop = asyncio.get_event_loop()
        await loop.run_in_executor(self._executor, self._inst.close)

    async def read_file(self, path: str) -> bytes:
        loop = asyncio.get_event_loop()
        return await loop.run_in_executor(
            self._executor,
            self._inst.read_file,
            path
        )

    async def write_file(self, path: str, data: bytes, mode: int = 0o644):
        loop = asyncio.get_event_loop()
        await loop.run_in_executor(
            self._executor,
            self._inst.write_file,
            path, data, mode
        )

    def command(self, name: str, *args: str) -> "AsyncCmd":
        return AsyncCmd(self._inst.command(name, *args), self._executor)

class AsyncCmd:
    """Async command wrapper."""

    def __init__(self, cmd, executor):
        self._cmd = cmd
        self._executor = executor

    async def output(self) -> bytes:
        loop = asyncio.get_event_loop()
        return await loop.run_in_executor(self._executor, self._cmd.output)

    async def run(self) -> "CompletedProcess":
        loop = asyncio.get_event_loop()
        return await loop.run_in_executor(self._executor, self._cmd.run)
```

### Usage Examples

```python
# Example 1: Simple container execution
import cc

with cc.OCIClient() as client:
    source = client.pull("python:3.12-slim")
    with cc.Instance(source) as inst:
        # Run a Python script
        inst.write_file("/app/hello.py", b'print("Hello from VM!")')
        output = inst.command("python", "/app/hello.py").output()
        print(output.decode())

# Example 2: Build and cache a development environment
import cc

with cc.OCIClient() as client:
    factory = cc.SnapshotFactory(client, cache_dir="~/.cache/cc")
    snapshot = (factory
        .from_image("ubuntu:22.04")
        .run("apt-get", "update")
        .run("apt-get", "install", "-y", "build-essential", "python3")
        .exclude("/var/cache/*", "/tmp/*")
        .build())

    # Use the snapshot for multiple instances
    with cc.Instance(snapshot) as inst:
        inst.command("gcc", "--version").run()

# Example 3: Async web server testing
import asyncio
import aiohttp
import cc

async def test_web_app():
    async with cc.AsyncInstance(source, memory_mb=512) as inst:
        # Start web server
        await inst.command("python", "-m", "http.server", "8080").start()

        # Connect from host (via netstack)
        async with aiohttp.ClientSession() as session:
            async with session.get("http://guest:8080/") as resp:
                print(await resp.text())
```

---

## Node.js/Bun Bindings

The JavaScript bindings use N-API for Node.js compatibility and optionally Bun FFI for performance. TypeScript types are provided.

### Installation

```bash
npm install @cc/node
# or
bun add @cc/node
```

### Module Structure

```
@cc/node/
├── src/
│   ├── index.ts           # Main exports
│   ├── ffi.ts             # N-API/Bun FFI bindings
│   ├── client.ts          # OCIClient
│   ├── instance.ts        # Instance class
│   ├── cmd.ts             # Cmd class
│   ├── snapshot.ts        # Snapshot classes
│   └── types.ts           # TypeScript interfaces
├── native/
│   └── binding.cc         # N-API C++ wrapper
├── package.json
└── tsconfig.json
```

### TypeScript Definitions

```typescript
// types.ts

export enum PullPolicy {
  IfNotPresent = 0,
  Always = 1,
  Never = 2,
}

export interface DownloadProgress {
  current: number;
  total: number;
  filename: string;
  blobIndex: number;
  blobCount: number;
  bytesPerSecond: number;
  etaSeconds: number;
}

export interface ImageConfig {
  architecture: string;
  env: string[];
  workingDir: string;
  entrypoint: string[];
  cmd: string[];
  user: string;
}

export interface MountConfig {
  tag: string;
  hostPath?: string;
  writable?: boolean;
}

export interface InstanceOptions {
  memoryMB?: number;
  cpus?: number;
  timeoutSeconds?: number;
  user?: string;
  mounts?: MountConfig[];
  gpu?: boolean;
  dmesg?: boolean;
}

export interface PullOptions {
  platform?: { os: string; arch: string };
  auth?: { username: string; password: string };
  policy?: PullPolicy;
  onProgress?: (progress: DownloadProgress) => void;
}

export interface FileInfo {
  name: string;
  size: number;
  mode: number;
  modTime: Date;
  isDirectory: boolean;
  isSymlink: boolean;
}

export interface CompletedProcess {
  exitCode: number;
  stdout?: Buffer;
  stderr?: Buffer;
}
```

### OCI Client

```typescript
// client.ts
import { PullOptions, InstanceSource } from './types';

export class OCIClient implements Disposable {
  private handle: number;

  constructor(cacheDir?: string) {
    this.handle = bindings.cc_oci_client_new(cacheDir);
  }

  /**
   * Pull an image from a registry.
   *
   * @example
   * const source = await client.pull('alpine:latest');
   *
   * @example
   * const source = await client.pull('ubuntu:22.04', {
   *   platform: { os: 'linux', arch: 'arm64' },
   *   onProgress: (p) => console.log(`${p.current}/${p.total}`),
   * });
   */
  async pull(imageRef: string, options?: PullOptions): Promise<InstanceSource> {
    return new Promise((resolve, reject) => {
      bindings.cc_oci_client_pull_async(
        this.handle,
        imageRef,
        options,
        (err, handle) => {
          if (err) reject(new CCError(err));
          else resolve(new InstanceSource(handle));
        }
      );
    });
  }

  /**
   * Pull an image synchronously.
   */
  pullSync(imageRef: string, options?: PullOptions): InstanceSource {
    const handle = bindings.cc_oci_client_pull(this.handle, imageRef, options);
    return new InstanceSource(handle);
  }

  /**
   * Load an image from a tar file.
   */
  async loadTar(path: string): Promise<InstanceSource> {
    return new Promise((resolve, reject) => {
      bindings.cc_oci_client_load_tar_async(this.handle, path, (err, handle) => {
        if (err) reject(new CCError(err));
        else resolve(new InstanceSource(handle));
      });
    });
  }

  /**
   * Load an image from a directory.
   */
  loadDir(path: string): InstanceSource {
    return new InstanceSource(bindings.cc_oci_client_load_dir(this.handle, path));
  }

  get cacheDir(): string {
    return bindings.cc_oci_client_cache_dir(this.handle);
  }

  close(): void {
    if (this.handle) {
      bindings.cc_oci_client_free(this.handle);
      this.handle = 0;
    }
  }

  // Symbol.dispose for 'using' declarations (TypeScript 5.2+)
  [Symbol.dispose](): void {
    this.close();
  }
}
```

### Instance

```typescript
// instance.ts
import { InstanceOptions, FileInfo, InstanceSource } from './types';
import { Cmd } from './cmd';

export class File implements Disposable {
  private handle: number;

  constructor(handle: number) {
    this.handle = handle;
  }

  async read(size?: number): Promise<Buffer> {
    return bindings.cc_file_read_async(this.handle, size ?? -1);
  }

  readSync(size?: number): Buffer {
    return bindings.cc_file_read(this.handle, size ?? -1);
  }

  async write(data: Buffer | string): Promise<number> {
    const buf = typeof data === 'string' ? Buffer.from(data) : data;
    return bindings.cc_file_write_async(this.handle, buf);
  }

  writeSync(data: Buffer | string): number {
    const buf = typeof data === 'string' ? Buffer.from(data) : data;
    return bindings.cc_file_write(this.handle, buf);
  }

  seek(offset: number, whence: 'set' | 'cur' | 'end' = 'set'): number {
    const w = { set: 0, cur: 1, end: 2 }[whence];
    return bindings.cc_file_seek(this.handle, offset, w);
  }

  stat(): FileInfo {
    return bindings.cc_file_stat(this.handle);
  }

  get name(): string {
    return bindings.cc_file_name(this.handle);
  }

  close(): void {
    if (this.handle) {
      bindings.cc_file_close(this.handle);
      this.handle = 0;
    }
  }

  [Symbol.dispose](): void {
    this.close();
  }
}

export class Instance implements Disposable {
  private handle: number;

  /**
   * Create and start a new VM instance.
   *
   * @example
   * using inst = new Instance(source, { memoryMB: 512, cpus: 2 });
   * const output = await inst.command('echo', 'hello').output();
   */
  constructor(source: InstanceSource, options?: InstanceOptions) {
    this.handle = bindings.cc_instance_new(source.handle, options);
  }

  get id(): string {
    return bindings.cc_instance_id(this.handle);
  }

  // ========== Filesystem Operations ==========

  /**
   * Open a file for reading.
   */
  open(path: string): File {
    return new File(bindings.cc_fs_open(this.handle, path));
  }

  /**
   * Create or truncate a file.
   */
  create(path: string): File {
    return new File(bindings.cc_fs_create(this.handle, path));
  }

  /**
   * Read entire file contents.
   */
  async readFile(path: string): Promise<Buffer> {
    return bindings.cc_fs_read_file_async(this.handle, path);
  }

  readFileSync(path: string): Buffer {
    return bindings.cc_fs_read_file(this.handle, path);
  }

  /**
   * Write data to a file.
   */
  async writeFile(path: string, data: Buffer | string, mode = 0o644): Promise<void> {
    const buf = typeof data === 'string' ? Buffer.from(data) : data;
    return bindings.cc_fs_write_file_async(this.handle, path, buf, mode);
  }

  writeFileSync(path: string, data: Buffer | string, mode = 0o644): void {
    const buf = typeof data === 'string' ? Buffer.from(data) : data;
    bindings.cc_fs_write_file(this.handle, path, buf, mode);
  }

  stat(path: string): FileInfo {
    return bindings.cc_fs_stat(this.handle, path);
  }

  lstat(path: string): FileInfo {
    return bindings.cc_fs_lstat(this.handle, path);
  }

  exists(path: string): boolean {
    try {
      this.stat(path);
      return true;
    } catch {
      return false;
    }
  }

  remove(path: string): void {
    bindings.cc_fs_remove(this.handle, path);
  }

  rmdir(path: string, options?: { recursive?: boolean }): void {
    if (options?.recursive) {
      bindings.cc_fs_remove_all(this.handle, path);
    } else {
      bindings.cc_fs_remove(this.handle, path);
    }
  }

  mkdir(path: string, options?: { recursive?: boolean; mode?: number }): void {
    if (options?.recursive) {
      bindings.cc_fs_mkdir_all(this.handle, path, options?.mode ?? 0o755);
    } else {
      bindings.cc_fs_mkdir(this.handle, path, options?.mode ?? 0o755);
    }
  }

  rename(oldPath: string, newPath: string): void {
    bindings.cc_fs_rename(this.handle, oldPath, newPath);
  }

  symlink(target: string, path: string): void {
    bindings.cc_fs_symlink(this.handle, target, path);
  }

  readlink(path: string): string {
    return bindings.cc_fs_readlink(this.handle, path);
  }

  readdir(path: string): string[] {
    return bindings.cc_fs_read_dir(this.handle, path).map((e: any) => e.name);
  }

  chmod(path: string, mode: number): void {
    bindings.cc_fs_chmod(this.handle, path, mode);
  }

  chown(path: string, uid: number, gid: number): void {
    bindings.cc_fs_chown(this.handle, path, uid, gid);
  }

  // ========== Command Execution ==========

  /**
   * Create a command to run in the guest.
   *
   * @example
   * const output = await inst.command('cat', '/etc/passwd').output();
   *
   * @example
   * const cmd = inst.command('make', 'build');
   * cmd.cwd = '/src';
   * cmd.env.CC = 'gcc';
   * const result = await cmd.run();
   */
  command(name: string, ...args: string[]): Cmd {
    return new Cmd(bindings.cc_cmd_new(this.handle, name, args));
  }

  /**
   * Create command using container entrypoint.
   */
  entrypoint(...args: string[]): Cmd {
    return new Cmd(bindings.cc_cmd_entrypoint(this.handle, args));
  }

  /**
   * Replace init with command (terminal operation).
   */
  exec(name: string, ...args: string[]): void {
    bindings.cc_instance_exec(this.handle, name, args);
  }

  // ========== Networking ==========

  /**
   * Listen for connections on guest network.
   */
  listen(address: string, network = 'tcp'): Listener {
    return new Listener(bindings.cc_net_listen(this.handle, network, address));
  }

  // ========== Snapshots ==========

  /**
   * Take a filesystem snapshot.
   */
  snapshot(options?: { excludes?: string[]; cacheDir?: string }): Snapshot {
    return new Snapshot(bindings.cc_fs_snapshot(this.handle, options));
  }

  // ========== Lifecycle ==========

  async wait(): Promise<void> {
    return bindings.cc_instance_wait_async(this.handle);
  }

  setConsoleSize(cols: number, rows: number): void {
    bindings.cc_instance_set_console_size(this.handle, cols, rows);
  }

  setNetworkEnabled(enabled: boolean): void {
    bindings.cc_instance_set_network_enabled(this.handle, enabled);
  }

  close(): void {
    if (this.handle) {
      bindings.cc_instance_close(this.handle);
      this.handle = 0;
    }
  }

  [Symbol.dispose](): void {
    this.close();
  }
}
```

### Command

```typescript
// cmd.ts
import { CompletedProcess } from './types';

export class Cmd {
  private handle: number;
  private _env: Record<string, string> = {};
  private _cwd?: string;

  constructor(handle: number) {
    this.handle = handle;
  }

  get env(): Record<string, string> {
    return this._env;
  }

  get cwd(): string | undefined {
    return this._cwd;
  }

  set cwd(value: string) {
    this._cwd = value;
    bindings.cc_cmd_set_dir(this.handle, value);
  }

  setEnv(key: string, value: string): this {
    this._env[key] = value;
    bindings.cc_cmd_set_env(this.handle, key, value);
    return this;
  }

  /**
   * Run command and wait for completion.
   */
  async run(): Promise<CompletedProcess> {
    return bindings.cc_cmd_run_async(this.handle);
  }

  runSync(): CompletedProcess {
    return bindings.cc_cmd_run(this.handle);
  }

  /**
   * Run and return stdout.
   */
  async output(): Promise<Buffer> {
    return bindings.cc_cmd_output_async(this.handle);
  }

  outputSync(): Buffer {
    return bindings.cc_cmd_output(this.handle);
  }

  /**
   * Run and return stdout + stderr.
   */
  async combinedOutput(): Promise<Buffer> {
    return bindings.cc_cmd_combined_output_async(this.handle);
  }

  /**
   * Start command without waiting.
   */
  start(): ChildProcess {
    bindings.cc_cmd_start(this.handle);
    return new ChildProcess(this.handle);
  }
}

export class ChildProcess {
  private handle: number;
  readonly stdin: WritableStream;
  readonly stdout: ReadableStream;
  readonly stderr: ReadableStream;

  constructor(handle: number) {
    this.handle = handle;
    // Create streams from pipe handles
    this.stdin = this.createStdinStream();
    this.stdout = this.createStdoutStream();
    this.stderr = this.createStderrStream();
  }

  private createStdinStream(): WritableStream {
    const pipeHandle = bindings.cc_cmd_stdin_pipe(this.handle);
    return new WritableStream({
      write(chunk) {
        bindings.cc_write_pipe_write(pipeHandle, chunk);
      },
      close() {
        bindings.cc_write_pipe_close(pipeHandle);
      },
    });
  }

  private createStdoutStream(): ReadableStream {
    const pipeHandle = bindings.cc_cmd_stdout_pipe(this.handle);
    return new ReadableStream({
      pull(controller) {
        const chunk = bindings.cc_read_pipe_read(pipeHandle, 4096);
        if (chunk.length === 0) {
          controller.close();
        } else {
          controller.enqueue(chunk);
        }
      },
    });
  }

  async wait(): Promise<number> {
    return bindings.cc_cmd_wait_async(this.handle);
  }

  get exitCode(): number {
    return bindings.cc_cmd_exit_code(this.handle);
  }
}
```

### Usage Examples

```typescript
// Example 1: Simple container execution
import { OCIClient, Instance } from '@cc/node';

{
  using client = new OCIClient();
  const source = await client.pull('alpine:latest');

  using inst = new Instance(source);
  const output = await inst.command('echo', 'Hello from VM!').output();
  console.log(output.toString());
}

// Example 2: Building a snapshot
import { OCIClient, SnapshotFactory } from '@cc/node';

const client = new OCIClient();
const factory = new SnapshotFactory(client, '~/.cache/cc');

const snapshot = await factory
  .from('node:20-slim')
  .run('npm', 'install', '-g', 'typescript')
  .exclude('/root/.npm/_cacache/*')
  .build();

{
  using inst = new Instance(snapshot);
  await inst.command('tsc', '--version').run();
}

// Example 3: File operations
{
  using inst = new Instance(source);

  // Write a file
  await inst.writeFile('/app/config.json', JSON.stringify({ port: 3000 }));

  // Read it back
  const config = JSON.parse((await inst.readFile('/app/config.json')).toString());
  console.log(config.port);

  // Use file handle
  {
    using file = inst.create('/app/log.txt');
    file.writeSync('Line 1\n');
    file.writeSync('Line 2\n');
  }
}

// Example 4: Streaming command output
{
  using inst = new Instance(source);

  const proc = inst.command('find', '/', '-name', '*.conf').start();

  const reader = proc.stdout.getReader();
  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    process.stdout.write(value);
  }

  await proc.wait();
}
```

---

## Rust Bindings

The Rust bindings use `bindgen` to generate FFI bindings from `libcc.h`, with a safe wrapper layer that provides RAII semantics via `Drop` and `Result<T, Error>` error handling.

### Cargo.toml

```toml
[package]
name = "cc"
version = "0.1.0"
edition = "2021"
description = "Virtualization primitives with familiar APIs"
license = "MIT"

[dependencies]
thiserror = "1.0"

[build-dependencies]
bindgen = "0.69"
```

### Crate Structure

```
cc/
├── src/
│   ├── lib.rs          # Public API
│   ├── ffi.rs          # Raw bindgen output
│   ├── error.rs        # Error types
│   ├── client.rs       # OciClient
│   ├── instance.rs     # Instance
│   ├── fs.rs           # File, filesystem ops
│   ├── cmd.rs          # Cmd
│   ├── net.rs          # Listener, Conn
│   └── snapshot.rs     # Snapshot, SnapshotFactory
├── build.rs            # Bindgen configuration
└── Cargo.toml
```

### Error Handling

```rust
// src/error.rs
use thiserror::Error;

#[derive(Error, Debug)]
pub enum Error {
    #[error("hypervisor unavailable: {0}")]
    HypervisorUnavailable(String),

    #[error("instance not running")]
    NotRunning,

    #[error("already closed")]
    AlreadyClosed,

    #[error("operation timed out")]
    Timeout,

    #[error("I/O error: {0}")]
    Io(String),

    #[error("network error: {0}")]
    Network(String),

    #[error("{op}: {message}")]
    Operation {
        op: String,
        path: Option<String>,
        message: String,
    },
}

pub type Result<T> = std::result::Result<T, Error>;

impl From<ffi::cc_error> for Error {
    fn from(err: ffi::cc_error) -> Self {
        let message = unsafe {
            std::ffi::CStr::from_ptr(err.message)
                .to_string_lossy()
                .into_owned()
        };

        match err.code {
            ffi::CC_ERR_HYPERVISOR_UNAVAILABLE => Error::HypervisorUnavailable(message),
            ffi::CC_ERR_NOT_RUNNING => Error::NotRunning,
            ffi::CC_ERR_ALREADY_CLOSED => Error::AlreadyClosed,
            ffi::CC_ERR_TIMEOUT => Error::Timeout,
            ffi::CC_ERR_IO => Error::Io(message),
            ffi::CC_ERR_NETWORK => Error::Network(message),
            _ => Error::Operation {
                op: unsafe {
                    err.op
                        .as_ref()
                        .map(|p| std::ffi::CStr::from_ptr(p).to_string_lossy().into_owned())
                        .unwrap_or_default()
                },
                path: unsafe {
                    err.path
                        .as_ref()
                        .map(|p| std::ffi::CStr::from_ptr(p).to_string_lossy().into_owned())
                },
                message,
            },
        }
    }
}
```

### OCI Client

```rust
// src/client.rs
use std::ffi::{CStr, CString};
use std::path::Path;

use crate::error::{Error, Result};
use crate::ffi;
use crate::snapshot::Snapshot;

/// Policy for pulling images from registry.
#[derive(Debug, Clone, Copy, Default)]
pub enum PullPolicy {
    #[default]
    IfNotPresent,
    Always,
    Never,
}

/// Download progress information.
#[derive(Debug, Clone)]
pub struct DownloadProgress {
    pub current: i64,
    pub total: i64,
    pub filename: String,
    pub blob_index: i32,
    pub blob_count: i32,
    pub bytes_per_second: f64,
    pub eta_seconds: f64,
}

/// Options for pulling images.
#[derive(Debug, Clone, Default)]
pub struct PullOptions {
    pub platform: Option<(String, String)>,  // (os, arch)
    pub auth: Option<(String, String)>,       // (username, password)
    pub policy: PullPolicy,
}

/// Image configuration from OCI manifest.
#[derive(Debug, Clone)]
pub struct ImageConfig {
    pub architecture: String,
    pub env: Vec<String>,
    pub working_dir: String,
    pub entrypoint: Vec<String>,
    pub cmd: Vec<String>,
    pub user: String,
}

/// An image source that can be used to create instances.
pub struct InstanceSource {
    pub(crate) handle: ffi::cc_instance_source,
}

impl InstanceSource {
    /// Get image configuration if available.
    pub fn config(&self) -> Option<ImageConfig> {
        // ... implementation
        None
    }
}

impl Drop for InstanceSource {
    fn drop(&mut self) {
        if self.handle != 0 {
            unsafe { ffi::cc_instance_source_free(self.handle) };
        }
    }
}

/// Client for pulling and managing OCI images.
///
/// # Example
///
/// ```rust
/// use cc::{OciClient, Instance};
///
/// let client = OciClient::new()?;
/// let source = client.pull("alpine:latest", None)?;
/// let inst = Instance::new(&source, None)?;
/// ```
pub struct OciClient {
    handle: ffi::cc_oci_client,
}

impl OciClient {
    /// Create a new OCI client with default cache directory.
    pub fn new() -> Result<Self> {
        let mut handle: ffi::cc_oci_client = 0;
        let mut err = ffi::cc_error::default();

        let code = unsafe { ffi::cc_oci_client_new(&mut handle, &mut err) };

        if code != ffi::CC_OK {
            return Err(err.into());
        }

        Ok(Self { handle })
    }

    /// Create OCI client with custom cache directory.
    pub fn with_cache_dir<P: AsRef<Path>>(cache_dir: P) -> Result<Self> {
        let path = CString::new(cache_dir.as_ref().to_string_lossy().as_bytes())
            .map_err(|_| Error::Io("invalid path".into()))?;

        let mut handle: ffi::cc_oci_client = 0;
        let mut err = ffi::cc_error::default();

        let code = unsafe {
            ffi::cc_oci_client_new_with_cache(path.as_ptr(), &mut handle, &mut err)
        };

        if code != ffi::CC_OK {
            return Err(err.into());
        }

        Ok(Self { handle })
    }

    /// Pull an image from a registry.
    ///
    /// # Example
    ///
    /// ```rust
    /// let source = client.pull("ubuntu:22.04", Some(PullOptions {
    ///     platform: Some(("linux".into(), "arm64".into())),
    ///     ..Default::default()
    /// }))?;
    /// ```
    pub fn pull(&self, image_ref: &str, options: Option<PullOptions>) -> Result<InstanceSource> {
        let ref_cstr = CString::new(image_ref)
            .map_err(|_| Error::Io("invalid image reference".into()))?;

        let mut handle: ffi::cc_instance_source = 0;
        let mut err = ffi::cc_error::default();

        // Build options struct
        let opts = options.map(|o| ffi::cc_pull_options {
            platform_os: o.platform.as_ref().map(|(os, _)| os.as_ptr()).unwrap_or(std::ptr::null()),
            platform_arch: o.platform.as_ref().map(|(_, arch)| arch.as_ptr()).unwrap_or(std::ptr::null()),
            username: o.auth.as_ref().map(|(u, _)| u.as_ptr()).unwrap_or(std::ptr::null()),
            password: o.auth.as_ref().map(|(_, p)| p.as_ptr()).unwrap_or(std::ptr::null()),
            policy: match o.policy {
                PullPolicy::IfNotPresent => ffi::CC_PULL_IF_NOT_PRESENT,
                PullPolicy::Always => ffi::CC_PULL_ALWAYS,
                PullPolicy::Never => ffi::CC_PULL_NEVER,
            },
        });

        let code = unsafe {
            ffi::cc_oci_client_pull(
                self.handle,
                ref_cstr.as_ptr(),
                opts.as_ref().map(|o| o as *const _).unwrap_or(std::ptr::null()),
                None,
                std::ptr::null_mut(),
                &mut handle,
                &mut err,
            )
        };

        if code != ffi::CC_OK {
            return Err(err.into());
        }

        Ok(InstanceSource { handle })
    }

    /// Pull with progress callback.
    pub fn pull_with_progress<F>(
        &self,
        image_ref: &str,
        options: Option<PullOptions>,
        mut on_progress: F,
    ) -> Result<InstanceSource>
    where
        F: FnMut(DownloadProgress),
    {
        // Implementation wraps callback in extern "C" fn
        self.pull(image_ref, options) // simplified
    }

    /// Load image from tar file.
    pub fn load_tar<P: AsRef<Path>>(&self, path: P) -> Result<InstanceSource> {
        // ... implementation
        todo!()
    }

    /// Load image from directory.
    pub fn load_dir<P: AsRef<Path>>(&self, path: P) -> Result<InstanceSource> {
        // ... implementation
        todo!()
    }

    /// Get cache directory path.
    pub fn cache_dir(&self) -> String {
        unsafe {
            let ptr = ffi::cc_oci_client_cache_dir(self.handle);
            let s = CStr::from_ptr(ptr).to_string_lossy().into_owned();
            ffi::cc_free_string(ptr);
            s
        }
    }
}

impl Drop for OciClient {
    fn drop(&mut self) {
        if self.handle != 0 {
            unsafe { ffi::cc_oci_client_free(self.handle) };
        }
    }
}
```

### Instance

```rust
// src/instance.rs
use std::ffi::CString;
use std::io::{Read, Write};
use std::path::Path;

use crate::cmd::Cmd;
use crate::error::{Error, Result};
use crate::ffi;
use crate::fs::File;
use crate::snapshot::Snapshot;
use crate::InstanceSource;

/// Mount configuration for virtio-fs.
#[derive(Debug, Clone)]
pub struct MountConfig {
    pub tag: String,
    pub host_path: Option<String>,
    pub writable: bool,
}

/// Options for creating an instance.
#[derive(Debug, Clone, Default)]
pub struct InstanceOptions {
    pub memory_mb: Option<u64>,
    pub cpus: Option<i32>,
    pub timeout_seconds: Option<f64>,
    pub user: Option<String>,
    pub mounts: Vec<MountConfig>,
    pub gpu: bool,
    pub dmesg: bool,
}

/// File information.
#[derive(Debug, Clone)]
pub struct FileInfo {
    pub name: String,
    pub size: i64,
    pub mode: u32,
    pub mod_time: i64,
    pub is_dir: bool,
    pub is_symlink: bool,
}

/// A running virtual machine instance.
///
/// Provides filesystem, command execution, and networking APIs.
///
/// # Example
///
/// ```rust
/// use cc::{Instance, OciClient};
///
/// let client = OciClient::new()?;
/// let source = client.pull("alpine:latest", None)?;
///
/// let inst = Instance::new(&source, None)?;
///
/// // Filesystem operations
/// inst.write_file("/tmp/hello.txt", b"Hello!")?;
/// let data = inst.read_file("/tmp/hello.txt")?;
///
/// // Command execution
/// let output = inst.command("cat", &["/etc/os-release"])?.output()?;
/// println!("{}", String::from_utf8_lossy(&output));
/// ```
pub struct Instance {
    handle: ffi::cc_instance,
}

impl Instance {
    /// Create and start a new instance.
    pub fn new(source: &InstanceSource, options: Option<InstanceOptions>) -> Result<Self> {
        let mut handle: ffi::cc_instance = 0;
        let mut err = ffi::cc_error::default();

        // Build options struct (simplified)
        let code = unsafe {
            ffi::cc_instance_new(source.handle, std::ptr::null(), &mut handle, &mut err)
        };

        if code != ffi::CC_OK {
            return Err(err.into());
        }

        Ok(Self { handle })
    }

    /// Get instance ID.
    pub fn id(&self) -> String {
        unsafe {
            let ptr = ffi::cc_instance_id(self.handle);
            let s = std::ffi::CStr::from_ptr(ptr).to_string_lossy().into_owned();
            ffi::cc_free_string(ptr);
            s
        }
    }

    // ========== Filesystem Operations ==========

    /// Open a file for reading.
    pub fn open<P: AsRef<Path>>(&self, path: P) -> Result<File> {
        let path_cstr = CString::new(path.as_ref().to_string_lossy().as_bytes())
            .map_err(|_| Error::Io("invalid path".into()))?;

        let mut handle: ffi::cc_file = 0;
        let mut err = ffi::cc_error::default();

        let code = unsafe {
            ffi::cc_fs_open(self.handle, path_cstr.as_ptr(), &mut handle, &mut err)
        };

        if code != ffi::CC_OK {
            return Err(err.into());
        }

        Ok(File { handle })
    }

    /// Create or truncate a file.
    pub fn create<P: AsRef<Path>>(&self, path: P) -> Result<File> {
        let path_cstr = CString::new(path.as_ref().to_string_lossy().as_bytes())
            .map_err(|_| Error::Io("invalid path".into()))?;

        let mut handle: ffi::cc_file = 0;
        let mut err = ffi::cc_error::default();

        let code = unsafe {
            ffi::cc_fs_create(self.handle, path_cstr.as_ptr(), &mut handle, &mut err)
        };

        if code != ffi::CC_OK {
            return Err(err.into());
        }

        Ok(File { handle })
    }

    /// Read entire file contents.
    pub fn read_file<P: AsRef<Path>>(&self, path: P) -> Result<Vec<u8>> {
        let path_cstr = CString::new(path.as_ref().to_string_lossy().as_bytes())
            .map_err(|_| Error::Io("invalid path".into()))?;

        let mut data: *mut u8 = std::ptr::null_mut();
        let mut len: usize = 0;
        let mut err = ffi::cc_error::default();

        let code = unsafe {
            ffi::cc_fs_read_file(
                self.handle,
                path_cstr.as_ptr(),
                &mut data,
                &mut len,
                &mut err,
            )
        };

        if code != ffi::CC_OK {
            return Err(err.into());
        }

        let vec = unsafe { Vec::from_raw_parts(data, len, len) };
        Ok(vec)
    }

    /// Write data to a file.
    pub fn write_file<P: AsRef<Path>>(&self, path: P, data: &[u8]) -> Result<()> {
        self.write_file_mode(path, data, 0o644)
    }

    /// Write data to a file with specific permissions.
    pub fn write_file_mode<P: AsRef<Path>>(
        &self,
        path: P,
        data: &[u8],
        mode: u32,
    ) -> Result<()> {
        let path_cstr = CString::new(path.as_ref().to_string_lossy().as_bytes())
            .map_err(|_| Error::Io("invalid path".into()))?;

        let mut err = ffi::cc_error::default();

        let code = unsafe {
            ffi::cc_fs_write_file(
                self.handle,
                path_cstr.as_ptr(),
                data.as_ptr(),
                data.len(),
                mode,
                &mut err,
            )
        };

        if code != ffi::CC_OK {
            return Err(err.into());
        }

        Ok(())
    }

    /// Get file information.
    pub fn stat<P: AsRef<Path>>(&self, path: P) -> Result<FileInfo> {
        // ... implementation
        todo!()
    }

    /// Check if path exists.
    pub fn exists<P: AsRef<Path>>(&self, path: P) -> bool {
        self.stat(path).is_ok()
    }

    /// Remove a file.
    pub fn remove<P: AsRef<Path>>(&self, path: P) -> Result<()> {
        // ... implementation
        todo!()
    }

    /// Remove directory tree recursively.
    pub fn remove_all<P: AsRef<Path>>(&self, path: P) -> Result<()> {
        // ... implementation
        todo!()
    }

    /// Create a directory.
    pub fn mkdir<P: AsRef<Path>>(&self, path: P) -> Result<()> {
        self.mkdir_mode(path, 0o755)
    }

    /// Create a directory with specific permissions.
    pub fn mkdir_mode<P: AsRef<Path>>(&self, path: P, mode: u32) -> Result<()> {
        // ... implementation
        todo!()
    }

    /// Create directory and all parents.
    pub fn mkdir_all<P: AsRef<Path>>(&self, path: P) -> Result<()> {
        // ... implementation
        todo!()
    }

    /// Rename a file or directory.
    pub fn rename<P: AsRef<Path>, Q: AsRef<Path>>(&self, from: P, to: Q) -> Result<()> {
        // ... implementation
        todo!()
    }

    /// Create a symbolic link.
    pub fn symlink<P: AsRef<Path>, Q: AsRef<Path>>(&self, original: P, link: Q) -> Result<()> {
        // ... implementation
        todo!()
    }

    /// Read symbolic link target.
    pub fn read_link<P: AsRef<Path>>(&self, path: P) -> Result<String> {
        // ... implementation
        todo!()
    }

    /// Read directory contents.
    pub fn read_dir<P: AsRef<Path>>(&self, path: P) -> Result<Vec<String>> {
        // ... implementation
        todo!()
    }

    // ========== Command Execution ==========

    /// Create a command to run in the guest.
    ///
    /// # Example
    ///
    /// ```rust
    /// let output = inst.command("echo", &["hello", "world"])?.output()?;
    /// ```
    pub fn command(&self, name: &str, args: &[&str]) -> Result<Cmd> {
        let name_cstr = CString::new(name)
            .map_err(|_| Error::Io("invalid command name".into()))?;

        let args_cstrs: Vec<CString> = args
            .iter()
            .map(|a| CString::new(*a).unwrap())
            .collect();

        let args_ptrs: Vec<*const i8> = args_cstrs
            .iter()
            .map(|s| s.as_ptr())
            .chain(std::iter::once(std::ptr::null()))
            .collect();

        let mut handle: ffi::cc_cmd = 0;
        let mut err = ffi::cc_error::default();

        let code = unsafe {
            ffi::cc_cmd_new(
                self.handle,
                name_cstr.as_ptr(),
                args_ptrs.as_ptr(),
                &mut handle,
                &mut err,
            )
        };

        if code != ffi::CC_OK {
            return Err(err.into());
        }

        Ok(Cmd::new(handle))
    }

    /// Create command using container entrypoint.
    pub fn entrypoint(&self, args: &[&str]) -> Result<Cmd> {
        // ... implementation
        todo!()
    }

    /// Replace init with command (terminal operation).
    pub fn exec(&self, name: &str, args: &[&str]) -> Result<()> {
        // ... implementation
        todo!()
    }

    // ========== Snapshots ==========

    /// Take a filesystem snapshot.
    pub fn snapshot(&self) -> Result<Snapshot> {
        self.snapshot_with_options(None, None)
    }

    /// Take a snapshot with options.
    pub fn snapshot_with_options(
        &self,
        excludes: Option<&[&str]>,
        cache_dir: Option<&str>,
    ) -> Result<Snapshot> {
        // ... implementation
        todo!()
    }

    // ========== Lifecycle ==========

    /// Wait for instance to terminate.
    pub fn wait(&self) -> Result<()> {
        let mut err = ffi::cc_error::default();
        let code = unsafe { ffi::cc_instance_wait(self.handle, &mut err) };

        if code != ffi::CC_OK {
            return Err(err.into());
        }

        Ok(())
    }

    /// Set console size.
    pub fn set_console_size(&self, cols: i32, rows: i32) {
        unsafe { ffi::cc_instance_set_console_size(self.handle, cols, rows) };
    }

    /// Enable or disable network access.
    pub fn set_network_enabled(&self, enabled: bool) {
        unsafe { ffi::cc_instance_set_network_enabled(self.handle, enabled) };
    }
}

impl Drop for Instance {
    fn drop(&mut self) {
        if self.handle != 0 {
            let mut err = ffi::cc_error::default();
            unsafe { ffi::cc_instance_close(self.handle, &mut err) };
        }
    }
}
```

### Command

```rust
// src/cmd.rs
use std::ffi::CString;
use std::io::{Read, Write};

use crate::error::{Error, Result};
use crate::ffi;

/// Result of a completed command.
#[derive(Debug)]
pub struct Output {
    pub exit_code: i32,
    pub stdout: Vec<u8>,
    pub stderr: Vec<u8>,
}

/// A command ready to run in the guest.
pub struct Cmd {
    handle: ffi::cc_cmd,
}

impl Cmd {
    pub(crate) fn new(handle: ffi::cc_cmd) -> Self {
        Self { handle }
    }

    /// Set working directory.
    pub fn current_dir(self, dir: &str) -> Result<Self> {
        let dir_cstr = CString::new(dir)
            .map_err(|_| Error::Io("invalid directory".into()))?;

        let mut err = ffi::cc_error::default();
        let code = unsafe {
            ffi::cc_cmd_set_dir(self.handle, dir_cstr.as_ptr(), &mut err)
        };

        if code != ffi::CC_OK {
            return Err(err.into());
        }

        Ok(self)
    }

    /// Set an environment variable.
    pub fn env(self, key: &str, value: &str) -> Result<Self> {
        let key_cstr = CString::new(key).map_err(|_| Error::Io("invalid key".into()))?;
        let value_cstr = CString::new(value).map_err(|_| Error::Io("invalid value".into()))?;

        let mut err = ffi::cc_error::default();
        let code = unsafe {
            ffi::cc_cmd_set_env(self.handle, key_cstr.as_ptr(), value_cstr.as_ptr(), &mut err)
        };

        if code != ffi::CC_OK {
            return Err(err.into());
        }

        Ok(self)
    }

    /// Run command and wait for completion.
    pub fn run(self) -> Result<i32> {
        let mut exit_code: i32 = 0;
        let mut err = ffi::cc_error::default();

        let code = unsafe { ffi::cc_cmd_run(self.handle, &mut exit_code, &mut err) };

        if code != ffi::CC_OK {
            return Err(err.into());
        }

        Ok(exit_code)
    }

    /// Run and capture stdout.
    pub fn output(self) -> Result<Vec<u8>> {
        let mut data: *mut u8 = std::ptr::null_mut();
        let mut len: usize = 0;
        let mut exit_code: i32 = 0;
        let mut err = ffi::cc_error::default();

        let code = unsafe {
            ffi::cc_cmd_output(self.handle, &mut data, &mut len, &mut exit_code, &mut err)
        };

        if code != ffi::CC_OK {
            return Err(err.into());
        }

        let vec = unsafe { Vec::from_raw_parts(data, len, len) };
        Ok(vec)
    }

    /// Run and capture stdout + stderr.
    pub fn combined_output(self) -> Result<Vec<u8>> {
        let mut data: *mut u8 = std::ptr::null_mut();
        let mut len: usize = 0;
        let mut exit_code: i32 = 0;
        let mut err = ffi::cc_error::default();

        let code = unsafe {
            ffi::cc_cmd_combined_output(self.handle, &mut data, &mut len, &mut exit_code, &mut err)
        };

        if code != ffi::CC_OK {
            return Err(err.into());
        }

        let vec = unsafe { Vec::from_raw_parts(data, len, len) };
        Ok(vec)
    }
}

impl Drop for Cmd {
    fn drop(&mut self) {
        if self.handle != 0 {
            unsafe { ffi::cc_cmd_free(self.handle) };
        }
    }
}
```

### Usage Examples

```rust
use cc::{Instance, OciClient, PullOptions};

fn main() -> cc::Result<()> {
    // Example 1: Simple container execution
    let client = OciClient::new()?;
    let source = client.pull("alpine:latest", None)?;

    let inst = Instance::new(&source, None)?;
    let output = inst.command("echo", &["Hello from VM!"])?.output()?;
    println!("{}", String::from_utf8_lossy(&output));

    // Example 2: File operations
    inst.write_file("/tmp/test.txt", b"Hello, World!")?;
    let data = inst.read_file("/tmp/test.txt")?;
    assert_eq!(data, b"Hello, World!");

    // Example 3: Command with environment
    let output = inst
        .command("sh", &["-c", "echo $MY_VAR"])?
        .env("MY_VAR", "hello")?
        .current_dir("/tmp")?
        .output()?;

    // Example 4: Cross-platform pull
    let arm_source = client.pull("ubuntu:22.04", Some(PullOptions {
        platform: Some(("linux".into(), "arm64".into())),
        ..Default::default()
    }))?;

    Ok(())
}
```

---

## Pure C Usage

The C API can be used directly without any wrapper, providing maximum control and minimal overhead.

### Complete Example

```c
// example.c
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "libcc.h"

int main(int argc, char** argv) {
    cc_error err = {0};

    // Initialize library
    if (cc_init() != CC_OK) {
        fprintf(stderr, "Failed to initialize cc\n");
        return 1;
    }

    // Check hypervisor availability
    if (cc_supports_hypervisor(&err) != CC_OK) {
        fprintf(stderr, "Hypervisor not available: %s\n", err.message);
        cc_error_free(&err);
        cc_shutdown();
        return 1;
    }

    // Create OCI client
    cc_oci_client client = 0;
    if (cc_oci_client_new(&client, &err) != CC_OK) {
        fprintf(stderr, "Failed to create client: %s\n", err.message);
        cc_error_free(&err);
        cc_shutdown();
        return 1;
    }

    // Pull image
    cc_instance_source source = 0;
    cc_pull_options pull_opts = {
        .platform_os = "linux",
        .platform_arch = NULL,  // Use host architecture
        .policy = CC_PULL_IF_NOT_PRESENT,
    };

    printf("Pulling alpine:latest...\n");
    if (cc_oci_client_pull(client, "alpine:latest", &pull_opts,
                           NULL, NULL, &source, &err) != CC_OK) {
        fprintf(stderr, "Failed to pull image: %s\n", err.message);
        cc_error_free(&err);
        cc_oci_client_free(client);
        cc_shutdown();
        return 1;
    }

    // Create instance
    cc_instance inst = 0;
    cc_instance_options inst_opts = {
        .memory_mb = 256,
        .cpus = 1,
        .timeout_seconds = 60.0,
    };

    printf("Creating instance...\n");
    if (cc_instance_new(source, &inst_opts, &inst, &err) != CC_OK) {
        fprintf(stderr, "Failed to create instance: %s\n", err.message);
        cc_error_free(&err);
        cc_instance_source_free(source);
        cc_oci_client_free(client);
        cc_shutdown();
        return 1;
    }

    // Write a file
    const char* content = "Hello from C!";
    if (cc_fs_write_file(inst, "/tmp/hello.txt",
                         (const uint8_t*)content, strlen(content),
                         0644, &err) != CC_OK) {
        fprintf(stderr, "Failed to write file: %s\n", err.message);
        cc_error_free(&err);
    }

    // Read it back
    uint8_t* data = NULL;
    size_t len = 0;
    if (cc_fs_read_file(inst, "/tmp/hello.txt", &data, &len, &err) == CC_OK) {
        printf("File contents: %.*s\n", (int)len, data);
        cc_free_bytes(data);
    }

    // Run a command
    const char* args[] = {"cat", "/etc/os-release", NULL};
    cc_cmd cmd = 0;
    if (cc_cmd_new(inst, "cat", args + 1, &cmd, &err) != CC_OK) {
        fprintf(stderr, "Failed to create command: %s\n", err.message);
        cc_error_free(&err);
    } else {
        uint8_t* output = NULL;
        size_t output_len = 0;
        int exit_code = 0;

        if (cc_cmd_output(cmd, &output, &output_len, &exit_code, &err) == CC_OK) {
            printf("OS Release:\n%.*s\n", (int)output_len, output);
            printf("Exit code: %d\n", exit_code);
            cc_free_bytes(output);
        } else {
            fprintf(stderr, "Command failed: %s\n", err.message);
            cc_error_free(&err);
        }
    }

    // Cleanup
    cc_instance_close(inst, &err);
    cc_instance_source_free(source);
    cc_oci_client_free(client);
    cc_shutdown();

    return 0;
}
```

### Build

```bash
# macOS
gcc -o example example.c -L./lib -lcc -Wl,-rpath,./lib

# Linux
gcc -o example example.c -L./lib -lcc -Wl,-rpath,'$ORIGIN/lib'
```

### Memory Management Guidelines

1. **Handle Ownership**: Handles returned by `cc_*_new()` functions must be freed with corresponding `cc_*_free()` functions.

2. **String Ownership**: Strings returned by functions (e.g., `cc_instance_id()`, `cc_oci_client_cache_dir()`) must be freed with `cc_free_string()`.

3. **Byte Buffer Ownership**: Byte buffers (e.g., from `cc_fs_read_file()`, `cc_cmd_output()`) must be freed with `cc_free_bytes()`.

4. **Error Cleanup**: Always call `cc_error_free()` after handling an error to free the message strings.

5. **Input Strings**: Input string parameters (e.g., paths, image refs) are copied internally and can be freed immediately after the call returns.

### Thread Safety Notes

- The library is thread-safe. Multiple threads can use different instances concurrently.
- A single instance should not be used from multiple threads simultaneously without external synchronization.
- OCI client operations (pull, load) are thread-safe and can run concurrently.
- Handle values are stable and can be passed between threads.

---

## Implementation Strategy

### Directory Structure

```
bindings/
├── c/
│   ├── libcc.h              # Public header (from this document)
│   ├── libcc.go             # CGO exports
│   ├── handles.go           # Handle table management
│   ├── callbacks.go         # Callback wrapper utilities
│   └── Makefile
├── python/
│   ├── cc/
│   │   ├── __init__.py
│   │   ├── _ffi.py
│   │   ├── client.py
│   │   ├── instance.py
│   │   └── ...
│   ├── setup.py
│   └── pyproject.toml
├── node/
│   ├── src/
│   │   ├── index.ts
│   │   ├── ffi.ts
│   │   └── ...
│   ├── native/
│   │   └── binding.cc
│   ├── package.json
│   └── tsconfig.json
├── rust/
│   ├── src/
│   │   ├── lib.rs
│   │   ├── ffi.rs
│   │   └── ...
│   ├── build.rs
│   └── Cargo.toml
└── README.md
```

### CGO Implementation

```go
// bindings/c/libcc.go
package main

/*
#include "libcc.h"
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"sync"
	"unsafe"

	cc "github.com/tinyrange/cc"
)

// Handle table for tracking Go objects
var (
	handleMu    sync.RWMutex
	nextHandle  uint64 = 1
	handles     = make(map[uint64]interface{})
)

func newHandle(obj interface{}) C.cc_handle {
	handleMu.Lock()
	defer handleMu.Unlock()

	h := nextHandle
	nextHandle++
	handles[h] = obj
	return C.cc_handle(h)
}

func getHandle[T any](h C.cc_handle) (T, bool) {
	handleMu.RLock()
	defer handleMu.RUnlock()

	obj, ok := handles[uint64(h)]
	if !ok {
		var zero T
		return zero, false
	}

	typed, ok := obj.(T)
	return typed, ok
}

func freeHandle(h C.cc_handle) {
	handleMu.Lock()
	defer handleMu.Unlock()
	delete(handles, uint64(h))
}

func setError(err *C.cc_error, code C.cc_error_code, msg string) {
	if err != nil {
		err.code = code
		err.message = C.CString(msg)
	}
}

//export cc_init
func cc_init() C.cc_error_code {
	// Initialize Go runtime if needed
	return C.CC_OK
}

//export cc_shutdown
func cc_shutdown() {
	handleMu.Lock()
	defer handleMu.Unlock()

	// Close all tracked handles
	for h, obj := range handles {
		if closer, ok := obj.(interface{ Close() error }); ok {
			closer.Close()
		}
		delete(handles, h)
	}
}

//export cc_supports_hypervisor
func cc_supports_hypervisor(err *C.cc_error) C.cc_error_code {
	if e := cc.SupportsHypervisor(); e != nil {
		setError(err, C.CC_ERR_HYPERVISOR_UNAVAILABLE, e.Error())
		return C.CC_ERR_HYPERVISOR_UNAVAILABLE
	}
	return C.CC_OK
}

//export cc_oci_client_new
func cc_oci_client_new(out *C.cc_oci_client, err *C.cc_error) C.cc_error_code {
	client, e := cc.NewOCIClient()
	if e != nil {
		setError(err, C.CC_ERR_IO, e.Error())
		return C.CC_ERR_IO
	}

	*out = newHandle(client)
	return C.CC_OK
}

//export cc_oci_client_free
func cc_oci_client_free(client C.cc_oci_client) {
	if obj, ok := getHandle[cc.OCIClient](client); ok {
		// OCIClient doesn't have Close, but free the handle
		_ = obj
	}
	freeHandle(client)
}

//export cc_oci_client_pull
func cc_oci_client_pull(
	client C.cc_oci_client,
	imageRef *C.char,
	opts *C.cc_pull_options,
	progressCb C.cc_progress_callback,
	progressUserData unsafe.Pointer,
	out *C.cc_instance_source,
	err *C.cc_error,
) C.cc_error_code {
	cli, ok := getHandle[cc.OCIClient](client)
	if !ok {
		setError(err, C.CC_ERR_INVALID_HANDLE, "invalid client handle")
		return C.CC_ERR_INVALID_HANDLE
	}

	ref := C.GoString(imageRef)

	var pullOpts []cc.OCIPullOption
	if opts != nil {
		if opts.platform_os != nil && opts.platform_arch != nil {
			os := C.GoString(opts.platform_os)
			arch := C.GoString(opts.platform_arch)
			pullOpts = append(pullOpts, cc.WithPlatform(os, arch))
		}
		if opts.username != nil && opts.password != nil {
			user := C.GoString(opts.username)
			pass := C.GoString(opts.password)
			pullOpts = append(pullOpts, cc.WithAuth(user, pass))
		}
		pullOpts = append(pullOpts, cc.WithPullPolicy(cc.PullPolicy(opts.policy)))
	}

	// TODO: Handle progress callback

	source, e := cli.Pull(context.Background(), ref, pullOpts...)
	if e != nil {
		setError(err, C.CC_ERR_IO, e.Error())
		return C.CC_ERR_IO
	}

	*out = newHandle(source)
	return C.CC_OK
}

//export cc_instance_source_free
func cc_instance_source_free(source C.cc_instance_source) {
	freeHandle(source)
}

//export cc_instance_new
func cc_instance_new(
	source C.cc_instance_source,
	opts *C.cc_instance_options,
	out *C.cc_instance,
	err *C.cc_error,
) C.cc_error_code {
	src, ok := getHandle[cc.InstanceSource](source)
	if !ok {
		setError(err, C.CC_ERR_INVALID_HANDLE, "invalid source handle")
		return C.CC_ERR_INVALID_HANDLE
	}

	var instOpts []cc.Option
	if opts != nil {
		if opts.memory_mb > 0 {
			instOpts = append(instOpts, cc.WithMemoryMB(uint64(opts.memory_mb)))
		}
		if opts.cpus > 0 {
			instOpts = append(instOpts, cc.WithCPUs(int(opts.cpus)))
		}
		// ... other options
	}

	inst, e := cc.New(src, instOpts...)
	if e != nil {
		if errors.Is(e, cc.ErrHypervisorUnavailable) {
			setError(err, C.CC_ERR_HYPERVISOR_UNAVAILABLE, e.Error())
			return C.CC_ERR_HYPERVISOR_UNAVAILABLE
		}
		setError(err, C.CC_ERR_IO, e.Error())
		return C.CC_ERR_IO
	}

	*out = newHandle(inst)
	return C.CC_OK
}

//export cc_instance_close
func cc_instance_close(inst C.cc_instance, err *C.cc_error) C.cc_error_code {
	instance, ok := getHandle[cc.Instance](inst)
	if !ok {
		setError(err, C.CC_ERR_INVALID_HANDLE, "invalid instance handle")
		return C.CC_ERR_INVALID_HANDLE
	}

	if e := instance.Close(); e != nil {
		setError(err, C.CC_ERR_IO, e.Error())
		return C.CC_ERR_IO
	}

	freeHandle(inst)
	return C.CC_OK
}

//export cc_fs_read_file
func cc_fs_read_file(
	inst C.cc_instance,
	path *C.char,
	out **C.uint8_t,
	outLen *C.size_t,
	err *C.cc_error,
) C.cc_error_code {
	instance, ok := getHandle[cc.Instance](inst)
	if !ok {
		setError(err, C.CC_ERR_INVALID_HANDLE, "invalid instance handle")
		return C.CC_ERR_INVALID_HANDLE
	}

	data, e := instance.ReadFile(C.GoString(path))
	if e != nil {
		setError(err, C.CC_ERR_IO, e.Error())
		return C.CC_ERR_IO
	}

	// Allocate C memory and copy data
	cdata := C.malloc(C.size_t(len(data)))
	C.memcpy(cdata, unsafe.Pointer(&data[0]), C.size_t(len(data)))

	*out = (*C.uint8_t)(cdata)
	*outLen = C.size_t(len(data))

	return C.CC_OK
}

//export cc_free_string
func cc_free_string(s *C.char) {
	C.free(unsafe.Pointer(s))
}

//export cc_free_bytes
func cc_free_bytes(buf *C.uint8_t) {
	C.free(unsafe.Pointer(buf))
}

//export cc_error_free
func cc_error_free(err *C.cc_error) {
	if err != nil {
		if err.message != nil {
			C.free(unsafe.Pointer(err.message))
			err.message = nil
		}
		if err.op != nil {
			C.free(unsafe.Pointer(err.op))
			err.op = nil
		}
		if err.path != nil {
			C.free(unsafe.Pointer(err.path))
			err.path = nil
		}
	}
}

func main() {}
```

### Build System (Makefile)

```makefile
# bindings/c/Makefile

GOOS ?= $(shell go env GOOS)
GOARCH ?= $(shell go env GOARCH)

LIB_NAME := libcc
ifeq ($(GOOS),darwin)
    LIB_EXT := dylib
    LDFLAGS := -dynamiclib
else ifeq ($(GOOS),windows)
    LIB_EXT := dll
    LDFLAGS := -shared
else
    LIB_EXT := so
    LDFLAGS := -shared
endif

OUTPUT := $(LIB_NAME).$(LIB_EXT)

.PHONY: all clean test

all: $(OUTPUT)

$(OUTPUT): libcc.go handles.go
	CGO_ENABLED=1 go build -buildmode=c-shared -o $@ .

clean:
	rm -f $(LIB_NAME).* *.h

test: $(OUTPUT)
	$(CC) -o test_example test_example.c -L. -lcc -Wl,-rpath,.
	./test_example
```

### Platform Considerations

#### macOS
- Requires code signing with hypervisor entitlement for the shared library
- Use `codesign` to add entitlements after building
- Fat binaries can be created with `lipo` for universal (x86_64 + arm64) support

```bash
# Sign the library
codesign --entitlements entitlements.plist --force --sign - libcc.dylib
```

#### Linux
- Requires access to `/dev/kvm` for KVM acceleration
- Users may need to be in the `kvm` group
- Static linking possible but increases binary size significantly

#### Windows
- Hypervisor Platform API (WHPX) used when available
- Requires Windows 10 1803+ with Hyper-V enabled
- DLL must be in PATH or application directory

### Testing Strategy

1. **Unit Tests**: Test handle management, error handling, string conversion
2. **Integration Tests**: Full workflow tests for each language binding
3. **Memory Tests**: Use Valgrind (Linux) or Instruments (macOS) to detect leaks
4. **Cross-Platform CI**: Build and test on macOS, Linux, Windows

### Versioning

- C API version embedded in header: `CC_API_VERSION`
- Semantic versioning for language packages
- ABI compatibility maintained within major versions

---

## Appendix: API Reference Summary

| Go Type | C Handle Type | Primary Operations |
|---------|---------------|-------------------|
| `OCIClient` | `cc_oci_client` | pull, load_tar, load_dir |
| `InstanceSource` | `cc_instance_source` | (passed to cc_instance_new) |
| `Instance` | `cc_instance` | fs ops, cmd, net, snapshot |
| `File` | `cc_file` | read, write, seek, stat |
| `Cmd` | `cc_cmd` | run, output, pipes |
| `Listener` | `cc_listener` | accept, addr |
| `Conn` | `cc_conn` | read, write, close |
| `Snapshot` | `cc_snapshot` | cache_key, parent, as_source |
| `SnapshotFactory` | `cc_snapshot_factory` | from, run, copy, build |

### Error Code Reference

| Code | Name | Description |
|------|------|-------------|
| 0 | `CC_OK` | Success |
| 1 | `CC_ERR_INVALID_HANDLE` | Handle is invalid or freed |
| 2 | `CC_ERR_INVALID_ARGUMENT` | Invalid function argument |
| 3 | `CC_ERR_NOT_RUNNING` | Instance is not running |
| 4 | `CC_ERR_ALREADY_CLOSED` | Resource already closed |
| 5 | `CC_ERR_TIMEOUT` | Operation timed out |
| 6 | `CC_ERR_HYPERVISOR_UNAVAILABLE` | Hypervisor not available |
| 7 | `CC_ERR_IO` | I/O or filesystem error |
| 8 | `CC_ERR_NETWORK` | Network error |
| 99 | `CC_ERR_UNKNOWN` | Unknown error |