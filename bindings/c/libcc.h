/*
 * libcc - C bindings for the cc virtualization library
 *
 * This header provides a C-compatible API for interacting with cc's
 * virtualization primitives. It uses opaque handles for Go objects
 * and follows explicit memory management conventions.
 *
 * Memory ownership:
 * - Input strings: Copied internally, caller retains ownership
 * - Output strings: Caller owns, must free with cc_free_string()
 * - Output byte buffers: Caller owns, must free with cc_free_bytes()
 * - Handles: Caller owns, must free with corresponding cc_*_free() function
 *
 * Thread safety:
 * - Operations on different instances are thread-safe
 * - A single instance should not be used from multiple threads without
 *   external synchronization
 */

#ifndef LIBCC_H
#define LIBCC_H

#include <stdint.h>
#include <stddef.h>
#include <stdbool.h>

#ifdef __cplusplus
extern "C" {
#endif

/* ==========================================================================
 * API Version
 * ========================================================================== */

#define CC_API_VERSION_MAJOR 0
#define CC_API_VERSION_MINOR 1
#define CC_API_VERSION_PATCH 0

/* Returns the API version as a string (e.g., "0.1.0") */
const char* cc_api_version(void);

/* Check if the runtime library is compatible with the header version */
bool cc_api_version_compatible(int major, int minor);

/* ==========================================================================
 * Handle Types (opaque references to Go objects)
 *
 * Each handle type is a distinct struct to enable compile-time type checking.
 * Passing the wrong handle type to a function will produce a compiler error.
 * ========================================================================== */

#define CC_DEFINE_HANDLE(name) typedef struct { uint64_t _h; } name

CC_DEFINE_HANDLE(cc_oci_client);
CC_DEFINE_HANDLE(cc_instance_source);
CC_DEFINE_HANDLE(cc_instance);
CC_DEFINE_HANDLE(cc_file);
CC_DEFINE_HANDLE(cc_cmd);
CC_DEFINE_HANDLE(cc_listener);
CC_DEFINE_HANDLE(cc_conn);
CC_DEFINE_HANDLE(cc_snapshot);
CC_DEFINE_HANDLE(cc_snapshot_factory);
CC_DEFINE_HANDLE(cc_cancel_token);

/* Check if a handle is valid (non-zero) */
#define CC_HANDLE_VALID(h) ((h)._h != 0)

/* Initialize a handle to invalid state */
#define CC_HANDLE_INVALID(type) ((type){0})

/* ==========================================================================
 * Error Handling
 * ========================================================================== */

typedef enum {
    CC_OK = 0,
    CC_ERR_INVALID_HANDLE = 1,      /* Handle is NULL, zero, or already freed */
    CC_ERR_INVALID_ARGUMENT = 2,    /* Function argument is invalid */
    CC_ERR_NOT_RUNNING = 3,         /* Instance has terminated */
    CC_ERR_ALREADY_CLOSED = 4,      /* Resource was already closed */
    CC_ERR_TIMEOUT = 5,             /* Operation exceeded time limit */
    CC_ERR_HYPERVISOR_UNAVAILABLE = 6, /* No hypervisor support */
    CC_ERR_IO = 7,                  /* Filesystem I/O error (local to guest) */
    CC_ERR_NETWORK = 8,             /* Network error (DNS, TCP connect, etc.) */
    CC_ERR_CANCELLED = 9,           /* Operation was cancelled via cancel token */
    CC_ERR_UNKNOWN = 99
} cc_error_code;

/*
 * Error classification:
 * - CC_ERR_IO: Guest filesystem operations (open, read, write, stat, etc.)
 * - CC_ERR_NETWORK: External network operations (registry pulls, DNS, HTTP)
 * For guest network operations (e.g., Dial to guest port), use CC_ERR_IO.
 */

typedef struct {
    cc_error_code code;
    char* message;      /* Error message (NULL on success) */
    char* op;           /* Operation that failed (NULL on success, may be NULL on error) */
    char* path;         /* Path involved (NULL on success, may be NULL on error) */
} cc_error;

/*
 * IMPORTANT: On success (CC_OK), all pointers in cc_error are NULL.
 * On error, call cc_error_free() to release any allocated strings.
 * Safe to call cc_error_free() even when code == CC_OK (no-op).
 */

/* Free error message strings. Safe to call on success (no-op). */
void cc_error_free(cc_error* err);

/* Free a string allocated by the library */
void cc_free_string(char* str);

/* Free a byte buffer allocated by the library */
void cc_free_bytes(uint8_t* buf);

/* ==========================================================================
 * Cancellation
 * ========================================================================== */

/* Create a cancellation token. Must be freed with cc_cancel_token_free(). */
cc_cancel_token cc_cancel_token_new(void);

/* Cancel the token. All operations using this token will return CC_ERR_CANCELLED. */
void cc_cancel_token_cancel(cc_cancel_token token);

/* Check if token is cancelled. */
bool cc_cancel_token_is_cancelled(cc_cancel_token token);

/* Free a cancellation token. */
void cc_cancel_token_free(cc_cancel_token token);

/* ==========================================================================
 * Library Initialization
 * ========================================================================== */

/*
 * Initialize the library. Must be called before any other function.
 * Returns CC_OK on success. Safe to call multiple times (reference counted).
 */
cc_error_code cc_init(void);

/*
 * Shutdown the library and release all resources.
 * After shutdown, all handles become invalid and any function call
 * (except cc_init) returns CC_ERR_INVALID_HANDLE.
 * Reference counted: only shuts down when all cc_init calls are balanced.
 */
void cc_shutdown(void);

/*
 * Check if hypervisor is available on this system.
 * Returns CC_OK if available, CC_ERR_HYPERVISOR_UNAVAILABLE otherwise.
 */
cc_error_code cc_supports_hypervisor(cc_error* err);

/* Query system capabilities. */
typedef struct {
    bool hypervisor_available;
    uint64_t max_memory_mb;     /* 0 if unknown */
    int max_cpus;               /* 0 if unknown */
    const char* architecture;   /* "x86_64", "arm64", etc. */
} cc_capabilities;

/*
 * Guest protocol version for host/guest compatibility checking.
 * The protocol version is incremented when the host-guest interface changes
 * in incompatible ways (virtio features, init program format, etc.).
 */
#define CC_GUEST_PROTOCOL_VERSION 1

/* Get the guest protocol version supported by this library. */
int cc_guest_protocol_version(void);

cc_error_code cc_query_capabilities(cc_capabilities* out, cc_error* err);

/* ==========================================================================
 * OCI Client - Image Management
 * ========================================================================== */

/* Create a new OCI client with default cache directory. */
cc_error_code cc_oci_client_new(cc_oci_client* out, cc_error* err);

/* Create OCI client with custom cache directory. */
cc_error_code cc_oci_client_new_with_cache(
    const char* cache_dir,
    cc_oci_client* out,
    cc_error* err
);

/* Free an OCI client. */
void cc_oci_client_free(cc_oci_client client);

/* Pull policy for image fetching. */
typedef enum {
    CC_PULL_IF_NOT_PRESENT = 0,
    CC_PULL_ALWAYS = 1,
    CC_PULL_NEVER = 2
} cc_pull_policy;

/* Options for pulling images. */
typedef struct {
    const char* platform_os;    /* e.g., "linux" (NULL for default) */
    const char* platform_arch;  /* e.g., "amd64", "arm64" (NULL for default) */
    const char* username;       /* Registry auth (NULL for anonymous) */
    const char* password;       /* Registry auth (NULL for anonymous) */
    cc_pull_policy policy;
} cc_pull_options;

/* Progress callback for downloads. */
typedef struct {
    int64_t current;            /* Bytes downloaded so far */
    int64_t total;              /* Total bytes (-1 if unknown) */
    const char* filename;       /* Current file being downloaded */
    int blob_index;             /* Current blob index (0-based) */
    int blob_count;             /* Total number of blobs */
    double bytes_per_second;    /* Download speed */
    double eta_seconds;         /* Estimated time remaining (-1 if unknown) */
} cc_download_progress;

typedef void (*cc_progress_callback)(const cc_download_progress* progress, void* user_data);

/*
 * Pull an OCI image from a registry.
 * The progress_user_data pointer must remain valid until the function returns.
 * Pass CC_HANDLE_INVALID(cc_cancel_token) if cancellation is not needed.
 */
cc_error_code cc_oci_client_pull(
    cc_oci_client client,
    const char* image_ref,
    const cc_pull_options* opts,        /* May be NULL for defaults */
    cc_progress_callback progress_cb,   /* May be NULL */
    void* progress_user_data,           /* Must remain valid until return */
    cc_cancel_token cancel,             /* For cancellation, or invalid handle */
    cc_instance_source* out,
    cc_error* err
);

/* Load image from a local tar file (docker save format). */
cc_error_code cc_oci_client_load_tar(
    cc_oci_client client,
    const char* tar_path,
    const cc_pull_options* opts,
    cc_instance_source* out,
    cc_error* err
);

/* Load image from a prebaked directory. */
cc_error_code cc_oci_client_load_dir(
    cc_oci_client client,
    const char* dir_path,
    const cc_pull_options* opts,
    cc_instance_source* out,
    cc_error* err
);

/* Export an instance source to a directory. */
cc_error_code cc_oci_client_export_dir(
    cc_oci_client client,
    cc_instance_source source,
    const char* dir_path,
    cc_error* err
);

/* Get cache directory path. Caller must free with cc_free_string(). */
char* cc_oci_client_cache_dir(cc_oci_client client);

/* Free an instance source. */
void cc_instance_source_free(cc_instance_source source);

/* ==========================================================================
 * Image Configuration
 * ========================================================================== */

typedef struct {
    char* architecture;     /* "amd64", "arm64", etc. */
    char** env;             /* NULL-terminated array of "KEY=VALUE" strings */
    size_t env_count;
    char* working_dir;
    char** entrypoint;      /* NULL-terminated array */
    size_t entrypoint_count;
    char** cmd;             /* NULL-terminated array */
    size_t cmd_count;
    char* user;
} cc_image_config;

/* Get image configuration from a source. Caller must free with cc_image_config_free(). */
cc_error_code cc_source_get_config(
    cc_instance_source source,
    cc_image_config** out,
    cc_error* err
);

void cc_image_config_free(cc_image_config* config);

/* ==========================================================================
 * Instance Creation and Lifecycle
 * ========================================================================== */

/* Mount configuration for virtio-fs. */
typedef struct {
    const char* tag;        /* Mount tag (guest uses: mount -t virtiofs <tag> /mnt) */
    const char* host_path;  /* Host directory (NULL for empty writable fs) */
    bool writable;          /* Read-only by default */
} cc_mount_config;

/* Instance creation options. */
typedef struct {
    uint64_t memory_mb;         /* Memory in MB (default: 256) */
    int cpus;                   /* Number of vCPUs (default: 1) */
    double timeout_seconds;     /* Instance timeout (0 for no timeout) */
    const char* user;           /* User:group to run as (e.g., "1000:1000") */
    bool enable_dmesg;          /* Enable kernel dmesg output */
    const cc_mount_config* mounts;
    size_t mount_count;
    /* Note: GPU is not supported in bindings. Use Go API directly. */
} cc_instance_options;

/* Create and start a new instance from a source. */
cc_error_code cc_instance_new(
    cc_instance_source source,
    const cc_instance_options* opts,    /* May be NULL for defaults */
    cc_instance* out,
    cc_error* err
);

/* Close an instance and release resources. */
cc_error_code cc_instance_close(cc_instance inst, cc_error* err);

/*
 * Wait for an instance to terminate.
 * Pass CC_HANDLE_INVALID(cc_cancel_token) if cancellation is not needed.
 */
cc_error_code cc_instance_wait(
    cc_instance inst,
    cc_cancel_token cancel,     /* For cancellation, or invalid handle */
    cc_error* err
);

/* Get instance ID. Caller must free with cc_free_string(). Returns NULL if handle is invalid. */
char* cc_instance_id(cc_instance inst);

/* Check if instance is still running. Returns false if handle is invalid. */
bool cc_instance_is_running(cc_instance inst);

/* Set console size (for interactive mode). Returns CC_ERR_INVALID_HANDLE if handle is invalid. */
cc_error_code cc_instance_set_console_size(cc_instance inst, int cols, int rows, cc_error* err);

/* Enable/disable network access. Returns CC_ERR_INVALID_HANDLE if handle is invalid. */
cc_error_code cc_instance_set_network_enabled(cc_instance inst, bool enabled, cc_error* err);

/*
 * NOTE: All functions in this API validate handles and return CC_ERR_INVALID_HANDLE
 * if the handle is invalid. Functions that cannot return errors (bool/pointer returns)
 * return false/NULL for invalid handles.
 */

/* ==========================================================================
 * Filesystem Operations (mirrors Go's os package)
 * ========================================================================== */

/* File open flags (match POSIX). */
#define CC_O_RDONLY    0x0000
#define CC_O_WRONLY    0x0001
#define CC_O_RDWR      0x0002
#define CC_O_APPEND    0x0008
#define CC_O_CREATE    0x0200
#define CC_O_TRUNC     0x0400
#define CC_O_EXCL      0x0800

/* File mode/permission bits. */
typedef uint32_t cc_file_mode;

/* File information structure. */
typedef struct {
    char* name;
    int64_t size;
    cc_file_mode mode;
    int64_t mod_time_unix;  /* Unix timestamp (seconds) */
    bool is_dir;
    bool is_symlink;
} cc_file_info;

void cc_file_info_free(cc_file_info* info);

/* Directory entry. */
typedef struct {
    char* name;
    bool is_dir;
    cc_file_mode mode;
} cc_dir_entry;

void cc_dir_entries_free(cc_dir_entry* entries, size_t count);

/* Open a file for reading. */
cc_error_code cc_fs_open(
    cc_instance inst,
    const char* path,
    cc_file* out,
    cc_error* err
);

/* Create or truncate a file. */
cc_error_code cc_fs_create(
    cc_instance inst,
    const char* path,
    cc_file* out,
    cc_error* err
);

/* Open a file with flags and permissions. */
cc_error_code cc_fs_open_file(
    cc_instance inst,
    const char* path,
    int flags,
    cc_file_mode perm,
    cc_file* out,
    cc_error* err
);

/* Close a file. */
cc_error_code cc_file_close(cc_file f, cc_error* err);

/* Read from a file. Returns bytes read in *n. */
cc_error_code cc_file_read(
    cc_file f,
    uint8_t* buf,
    size_t len,
    size_t* n,
    cc_error* err
);

/* Write to a file. Returns bytes written in *n. */
cc_error_code cc_file_write(
    cc_file f,
    const uint8_t* buf,
    size_t len,
    size_t* n,
    cc_error* err
);

/* Seek within a file. */
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

/* Sync file to disk. */
cc_error_code cc_file_sync(cc_file f, cc_error* err);

/* Truncate file to size. */
cc_error_code cc_file_truncate(cc_file f, int64_t size, cc_error* err);

/* Get file info. */
cc_error_code cc_file_stat(cc_file f, cc_file_info* out, cc_error* err);

/* Get file name. Caller must free with cc_free_string(). */
char* cc_file_name(cc_file f);

/* Read entire file contents. Caller must free with cc_free_bytes(). */
cc_error_code cc_fs_read_file(
    cc_instance inst,
    const char* path,
    uint8_t** out,
    size_t* len,
    cc_error* err
);

/* Write entire file contents. */
cc_error_code cc_fs_write_file(
    cc_instance inst,
    const char* path,
    const uint8_t* data,
    size_t len,
    cc_file_mode perm,
    cc_error* err
);

/* Get file info by path. */
cc_error_code cc_fs_stat(
    cc_instance inst,
    const char* path,
    cc_file_info* out,
    cc_error* err
);

/* Get file info (don't follow symlinks). */
cc_error_code cc_fs_lstat(
    cc_instance inst,
    const char* path,
    cc_file_info* out,
    cc_error* err
);

/* Remove a file. */
cc_error_code cc_fs_remove(
    cc_instance inst,
    const char* path,
    cc_error* err
);

/* Remove a file or directory recursively. */
cc_error_code cc_fs_remove_all(
    cc_instance inst,
    const char* path,
    cc_error* err
);

/* Create a directory. */
cc_error_code cc_fs_mkdir(
    cc_instance inst,
    const char* path,
    cc_file_mode perm,
    cc_error* err
);

/* Create a directory and all parents. */
cc_error_code cc_fs_mkdir_all(
    cc_instance inst,
    const char* path,
    cc_file_mode perm,
    cc_error* err
);

/* Rename a file or directory. */
cc_error_code cc_fs_rename(
    cc_instance inst,
    const char* oldpath,
    const char* newpath,
    cc_error* err
);

/* Create a symbolic link. */
cc_error_code cc_fs_symlink(
    cc_instance inst,
    const char* oldname,
    const char* newname,
    cc_error* err
);

/* Read a symbolic link. Caller must free with cc_free_string(). */
cc_error_code cc_fs_readlink(
    cc_instance inst,
    const char* path,
    char** out,
    cc_error* err
);

/* Read directory contents. */
cc_error_code cc_fs_read_dir(
    cc_instance inst,
    const char* path,
    cc_dir_entry** out,
    size_t* count,
    cc_error* err
);

/* Change file mode. */
cc_error_code cc_fs_chmod(
    cc_instance inst,
    const char* path,
    cc_file_mode mode,
    cc_error* err
);

/* Change file owner. */
cc_error_code cc_fs_chown(
    cc_instance inst,
    const char* path,
    int uid,
    int gid,
    cc_error* err
);

/* Change file times. */
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

/* Create a command to run in the instance. */
cc_error_code cc_cmd_new(
    cc_instance inst,
    const char* name,
    const char* const* args,    /* NULL-terminated array */
    cc_cmd* out,
    cc_error* err
);

/* Create a command using the container's entrypoint. */
cc_error_code cc_cmd_entrypoint(
    cc_instance inst,
    const char* const* args,    /* Optional override args (NULL for default CMD) */
    cc_cmd* out,
    cc_error* err
);

/* Free a command (if not yet started). */
void cc_cmd_free(cc_cmd cmd);

/* Set working directory. */
cc_error_code cc_cmd_set_dir(cc_cmd cmd, const char* dir, cc_error* err);

/* Set an environment variable. */
cc_error_code cc_cmd_set_env(cc_cmd cmd, const char* key, const char* value, cc_error* err);

/* Get an environment variable. Caller must free with cc_free_string(). */
char* cc_cmd_get_env(cc_cmd cmd, const char* key);

/* Get all environment variables. Caller must free array and strings. */
cc_error_code cc_cmd_environ(
    cc_cmd cmd,
    char*** out,
    size_t* count,
    cc_error* err
);

/* Start the command (non-blocking). */
cc_error_code cc_cmd_start(cc_cmd cmd, cc_error* err);

/* Wait for command to complete. Returns exit code in *exit_code. */
cc_error_code cc_cmd_wait(cc_cmd cmd, int* exit_code, cc_error* err);

/* Run command and wait for completion. */
cc_error_code cc_cmd_run(cc_cmd cmd, int* exit_code, cc_error* err);

/* Run command and capture stdout. Caller must free with cc_free_bytes(). */
cc_error_code cc_cmd_output(
    cc_cmd cmd,
    uint8_t** out,
    size_t* len,
    int* exit_code,
    cc_error* err
);

/* Run command and capture stdout+stderr. Caller must free with cc_free_bytes(). */
cc_error_code cc_cmd_combined_output(
    cc_cmd cmd,
    uint8_t** out,
    size_t* len,
    int* exit_code,
    cc_error* err
);

/* Get exit code (after Wait). */
int cc_cmd_exit_code(cc_cmd cmd);

/*
 * Kill a started command and release resources.
 * Safe to call on commands that have already completed.
 * After calling, the handle is invalid.
 */
cc_error_code cc_cmd_kill(cc_cmd cmd, cc_error* err);

/*
 * Get a pipe connected to the command's stdout.
 * Must be called before cc_cmd_start(). Read from the returned cc_conn
 * with cc_conn_read() while the command is running.
 */
cc_error_code cc_cmd_stdout_pipe(cc_cmd cmd, cc_conn* out, cc_error* err);

/*
 * Get a pipe connected to the command's stderr.
 * Must be called before cc_cmd_start(). Read from the returned cc_conn
 * with cc_conn_read() while the command is running.
 */
cc_error_code cc_cmd_stderr_pipe(cc_cmd cmd, cc_conn* out, cc_error* err);

/*
 * Get a pipe connected to the command's stdin.
 * Must be called before cc_cmd_start(). Write to the returned cc_conn
 * with cc_conn_write(). Close with cc_conn_close() to signal EOF.
 */
cc_error_code cc_cmd_stdin_pipe(cc_cmd cmd, cc_conn* out, cc_error* err);

/* Replace init process with command (terminal operation). */
cc_error_code cc_instance_exec(
    cc_instance inst,
    const char* name,
    const char* const* args,
    cc_error* err
);

/* ==========================================================================
 * Networking (mirrors Go's net package)
 * ========================================================================== */

/* Listen for connections on the guest network. */
cc_error_code cc_net_listen(
    cc_instance inst,
    const char* network,    /* "tcp", "tcp4" */
    const char* address,    /* e.g., ":8080", "0.0.0.0:80" */
    cc_listener* out,
    cc_error* err
);

/* Accept a connection from a listener. */
cc_error_code cc_listener_accept(
    cc_listener ln,
    cc_conn* out,
    cc_error* err
);

/* Close a listener. */
cc_error_code cc_listener_close(cc_listener ln, cc_error* err);

/* Get listener address. Caller must free with cc_free_string(). */
char* cc_listener_addr(cc_listener ln);

/* Read from a connection. */
cc_error_code cc_conn_read(
    cc_conn c,
    uint8_t* buf,
    size_t len,
    size_t* n,
    cc_error* err
);

/* Write to a connection. */
cc_error_code cc_conn_write(
    cc_conn c,
    const uint8_t* buf,
    size_t len,
    size_t* n,
    cc_error* err
);

/* Close a connection. */
cc_error_code cc_conn_close(cc_conn c, cc_error* err);

/* Get local address. Caller must free with cc_free_string(). */
char* cc_conn_local_addr(cc_conn c);

/* Get remote address. Caller must free with cc_free_string(). */
char* cc_conn_remote_addr(cc_conn c);

/* ==========================================================================
 * Filesystem Snapshots
 * ========================================================================== */

/* Snapshot options. */
typedef struct {
    const char* const* excludes;    /* NULL-terminated array of glob patterns */
    size_t exclude_count;
    const char* cache_dir;          /* Cache directory for layers */
} cc_snapshot_options;

/* ==========================================================================
 * Dockerfile Building
 * ========================================================================== */

/* Build argument key-value pair for Dockerfile ARG instructions. */
typedef struct {
    const char* key;
    const char* value;
} cc_build_arg;

/* Dockerfile build options. */
typedef struct {
    const char* context_dir;           /* Directory for COPY/ADD (optional) */
    const char* cache_dir;             /* Cache directory (required) */
    const cc_build_arg* build_args;    /* NULL-terminated array of build args (optional) */
    size_t build_arg_count;            /* Number of build args */
} cc_dockerfile_options;

/*
 * Build a filesystem snapshot from Dockerfile content.
 * This parses the Dockerfile and executes instructions to produce a snapshot.
 *
 * Parameters:
 *   client - OCI client for pulling base images
 *   dockerfile - Dockerfile content as bytes
 *   dockerfile_len - Length of dockerfile content
 *   options - Build options (cache_dir is required)
 *   cancel - Cancellation token, or CC_HANDLE_INVALID(cc_cancel_token)
 *   out_snapshot - Output snapshot handle
 *   err - Error output
 *
 * Returns CC_OK on success, error code otherwise.
 */
void cc_build_dockerfile_source(
    cc_oci_client client,
    const uint8_t* dockerfile,
    size_t dockerfile_len,
    const cc_dockerfile_options* options,
    cc_cancel_token cancel,
    cc_snapshot* out_snapshot,
    cc_error* err
);

/* Take a filesystem snapshot. */
cc_error_code cc_fs_snapshot(
    cc_instance inst,
    const cc_snapshot_options* opts,
    cc_snapshot* out,
    cc_error* err
);

/* Get snapshot cache key. Caller must free with cc_free_string(). */
char* cc_snapshot_cache_key(cc_snapshot snap);

/* Get parent snapshot (returns CC_HANDLE_INVALID if none). */
cc_snapshot cc_snapshot_parent(cc_snapshot snap);

/* Free a snapshot. */
cc_error_code cc_snapshot_close(cc_snapshot snap, cc_error* err);

/* A snapshot can be used as an instance source. */
cc_instance_source cc_snapshot_as_source(cc_snapshot snap);

#ifdef __cplusplus
}
#endif

#endif /* LIBCC_H */
