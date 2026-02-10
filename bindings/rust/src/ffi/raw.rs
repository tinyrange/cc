//! Raw C function bindings for libcc.
//!
//! This module contains the direct FFI bindings to the C API.
//! Users should prefer the safe Rust wrappers in the parent modules.

use std::os::raw::{c_char, c_double, c_int, c_void};

use super::handles::*;

/// Error code returned by C functions.
pub type CcErrorCode = c_int;

// Error codes
pub const CC_OK: CcErrorCode = 0;
pub const CC_ERR_INVALID_HANDLE: CcErrorCode = 1;
pub const CC_ERR_INVALID_ARGUMENT: CcErrorCode = 2;
pub const CC_ERR_NOT_RUNNING: CcErrorCode = 3;
pub const CC_ERR_ALREADY_CLOSED: CcErrorCode = 4;
pub const CC_ERR_TIMEOUT: CcErrorCode = 5;
pub const CC_ERR_HYPERVISOR_UNAVAILABLE: CcErrorCode = 6;
pub const CC_ERR_IO: CcErrorCode = 7;
pub const CC_ERR_NETWORK: CcErrorCode = 8;
pub const CC_ERR_CANCELLED: CcErrorCode = 9;
pub const CC_ERR_UNKNOWN: CcErrorCode = 99;

// Seek whence values
pub const CC_SEEK_SET: c_int = 0;
pub const CC_SEEK_CUR: c_int = 1;
pub const CC_SEEK_END: c_int = 2;

// Pull policy values
pub const CC_PULL_IF_NOT_PRESENT: c_int = 0;
pub const CC_PULL_ALWAYS: c_int = 1;
pub const CC_PULL_NEVER: c_int = 2;

// File open flags
pub const CC_O_RDONLY: c_int = 0x0000;
pub const CC_O_WRONLY: c_int = 0x0001;
pub const CC_O_RDWR: c_int = 0x0002;
pub const CC_O_APPEND: c_int = 0x0008;
pub const CC_O_CREATE: c_int = 0x0200;
pub const CC_O_TRUNC: c_int = 0x0400;
pub const CC_O_EXCL: c_int = 0x0800;

/// File mode type (Unix permissions).
pub type CcFileMode = u32;

/// C error structure.
#[repr(C)]
pub struct CcError {
    pub code: CcErrorCode,
    pub message: *mut c_char,
    pub op: *mut c_char,
    pub path: *mut c_char,
}

impl Default for CcError {
    fn default() -> Self {
        Self {
            code: CC_OK,
            message: std::ptr::null_mut(),
            op: std::ptr::null_mut(),
            path: std::ptr::null_mut(),
        }
    }
}

/// Pull options structure.
#[repr(C)]
pub struct CcPullOptions {
    pub platform_os: *const c_char,
    pub platform_arch: *const c_char,
    pub username: *const c_char,
    pub password: *const c_char,
    pub policy: c_int,
}

impl Default for CcPullOptions {
    fn default() -> Self {
        Self {
            platform_os: std::ptr::null(),
            platform_arch: std::ptr::null(),
            username: std::ptr::null(),
            password: std::ptr::null(),
            policy: CC_PULL_IF_NOT_PRESENT,
        }
    }
}

/// Download progress structure.
#[repr(C)]
pub struct CcDownloadProgress {
    pub current: i64,
    pub total: i64,
    pub filename: *const c_char,
    pub blob_index: c_int,
    pub blob_count: c_int,
    pub bytes_per_second: c_double,
    pub eta_seconds: c_double,
}

/// Progress callback type.
pub type CcProgressCallback =
    Option<unsafe extern "C" fn(progress: *const CcDownloadProgress, user_data: *mut c_void)>;

/// Mount configuration structure.
#[repr(C)]
pub struct CcMountConfig {
    pub tag: *const c_char,
    pub host_path: *const c_char,
    pub writable: bool,
}

/// Instance creation options.
#[repr(C)]
pub struct CcInstanceOptions {
    pub memory_mb: u64,
    pub cpus: c_int,
    pub timeout_seconds: c_double,
    pub user: *const c_char,
    pub enable_dmesg: bool,
    pub mounts: *const CcMountConfig,
    pub mount_count: usize,
}

impl Default for CcInstanceOptions {
    fn default() -> Self {
        Self {
            memory_mb: 256,
            cpus: 1,
            timeout_seconds: 0.0,
            user: std::ptr::null(),
            enable_dmesg: false,
            mounts: std::ptr::null(),
            mount_count: 0,
        }
    }
}

/// File information structure.
#[repr(C)]
pub struct CcFileInfo {
    pub name: *mut c_char,
    pub size: i64,
    pub mode: CcFileMode,
    pub mod_time_unix: i64,
    pub is_dir: bool,
    pub is_symlink: bool,
}

impl Default for CcFileInfo {
    fn default() -> Self {
        Self {
            name: std::ptr::null_mut(),
            size: 0,
            mode: 0,
            mod_time_unix: 0,
            is_dir: false,
            is_symlink: false,
        }
    }
}

/// Directory entry structure.
#[repr(C)]
pub struct CcDirEntry {
    pub name: *mut c_char,
    pub is_dir: bool,
    pub mode: CcFileMode,
}

/// System capabilities structure.
#[repr(C)]
pub struct CcCapabilities {
    pub hypervisor_available: bool,
    pub max_memory_mb: u64,
    pub max_cpus: c_int,
    pub architecture: *const c_char,
}

impl Default for CcCapabilities {
    fn default() -> Self {
        Self {
            hypervisor_available: false,
            max_memory_mb: 0,
            max_cpus: 0,
            architecture: std::ptr::null(),
        }
    }
}

/// Image configuration structure.
#[repr(C)]
pub struct CcImageConfig {
    pub architecture: *mut c_char,
    pub env: *mut *mut c_char,
    pub env_count: usize,
    pub working_dir: *mut c_char,
    pub entrypoint: *mut *mut c_char,
    pub entrypoint_count: usize,
    pub cmd: *mut *mut c_char,
    pub cmd_count: usize,
    pub user: *mut c_char,
}

/// Snapshot options structure.
#[repr(C)]
pub struct CcSnapshotOptions {
    pub excludes: *const *const c_char,
    pub exclude_count: usize,
    pub cache_dir: *const c_char,
}

impl Default for CcSnapshotOptions {
    fn default() -> Self {
        Self {
            excludes: std::ptr::null(),
            exclude_count: 0,
            cache_dir: std::ptr::null(),
        }
    }
}

/// Build argument for Dockerfile builds.
#[repr(C)]
pub struct CcBuildArg {
    pub key: *const c_char,
    pub value: *const c_char,
}

/// Dockerfile build options.
#[repr(C)]
pub struct CcDockerfileOptions {
    pub context_dir: *const c_char,
    pub cache_dir: *const c_char,
    pub build_args: *const CcBuildArg,
    pub build_arg_count: usize,
}

impl Default for CcDockerfileOptions {
    fn default() -> Self {
        Self {
            context_dir: std::ptr::null(),
            cache_dir: std::ptr::null(),
            build_args: std::ptr::null(),
            build_arg_count: 0,
        }
    }
}

// External C functions
extern "C" {
    // API version
    pub fn cc_api_version() -> *const c_char;
    pub fn cc_api_version_compatible(major: c_int, minor: c_int) -> bool;

    // Library init/shutdown
    pub fn cc_init() -> CcErrorCode;
    pub fn cc_shutdown();

    // Hypervisor support
    pub fn cc_supports_hypervisor(err: *mut CcError) -> CcErrorCode;
    pub fn cc_guest_protocol_version() -> c_int;
    pub fn cc_query_capabilities(out: *mut CcCapabilities, err: *mut CcError) -> CcErrorCode;

    // Memory management
    pub fn cc_error_free(err: *mut CcError);
    pub fn cc_free_string(s: *mut c_char);
    pub fn cc_free_bytes(buf: *mut u8);
    pub fn cc_file_info_free(info: *mut CcFileInfo);
    pub fn cc_dir_entries_free(entries: *mut CcDirEntry, count: usize);
    pub fn cc_image_config_free(config: *mut CcImageConfig);

    // Cancel token
    pub fn cc_cancel_token_new() -> CcCancelToken;
    pub fn cc_cancel_token_cancel(token: CcCancelToken);
    pub fn cc_cancel_token_is_cancelled(token: CcCancelToken) -> bool;
    pub fn cc_cancel_token_free(token: CcCancelToken);

    // OCI client
    pub fn cc_oci_client_new(out: *mut CcOciClient, err: *mut CcError) -> CcErrorCode;
    pub fn cc_oci_client_new_with_cache(
        cache_dir: *const c_char,
        out: *mut CcOciClient,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_oci_client_free(client: CcOciClient);
    pub fn cc_oci_client_pull(
        client: CcOciClient,
        image_ref: *const c_char,
        opts: *const CcPullOptions,
        progress_cb: CcProgressCallback,
        progress_user_data: *mut c_void,
        cancel: CcCancelToken,
        out: *mut CcInstanceSource,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_oci_client_load_tar(
        client: CcOciClient,
        tar_path: *const c_char,
        opts: *const CcPullOptions,
        out: *mut CcInstanceSource,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_oci_client_load_dir(
        client: CcOciClient,
        dir_path: *const c_char,
        opts: *const CcPullOptions,
        out: *mut CcInstanceSource,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_oci_client_export_dir(
        client: CcOciClient,
        source: CcInstanceSource,
        dir_path: *const c_char,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_oci_client_cache_dir(client: CcOciClient) -> *mut c_char;

    // Instance source
    pub fn cc_instance_source_free(source: CcInstanceSource);
    pub fn cc_source_get_config(
        source: CcInstanceSource,
        out: *mut *mut CcImageConfig,
        err: *mut CcError,
    ) -> CcErrorCode;

    // Instance
    pub fn cc_instance_new(
        source: CcInstanceSource,
        opts: *const CcInstanceOptions,
        out: *mut CcInstance,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_instance_close(inst: CcInstance, err: *mut CcError) -> CcErrorCode;
    pub fn cc_instance_wait(
        inst: CcInstance,
        cancel: CcCancelToken,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_instance_id(inst: CcInstance) -> *mut c_char;
    pub fn cc_instance_is_running(inst: CcInstance) -> bool;
    pub fn cc_instance_set_console_size(
        inst: CcInstance,
        cols: c_int,
        rows: c_int,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_instance_set_network_enabled(
        inst: CcInstance,
        enabled: bool,
        err: *mut CcError,
    ) -> CcErrorCode;

    // Filesystem operations
    pub fn cc_fs_open(
        inst: CcInstance,
        path: *const c_char,
        out: *mut CcFile,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_fs_create(
        inst: CcInstance,
        path: *const c_char,
        out: *mut CcFile,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_fs_open_file(
        inst: CcInstance,
        path: *const c_char,
        flags: c_int,
        perm: CcFileMode,
        out: *mut CcFile,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_file_close(f: CcFile, err: *mut CcError) -> CcErrorCode;
    pub fn cc_file_read(
        f: CcFile,
        buf: *mut u8,
        len: usize,
        n: *mut usize,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_file_write(
        f: CcFile,
        buf: *const u8,
        len: usize,
        n: *mut usize,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_file_seek(
        f: CcFile,
        offset: i64,
        whence: c_int,
        new_offset: *mut i64,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_file_sync(f: CcFile, err: *mut CcError) -> CcErrorCode;
    pub fn cc_file_truncate(f: CcFile, size: i64, err: *mut CcError) -> CcErrorCode;
    pub fn cc_file_stat(f: CcFile, out: *mut CcFileInfo, err: *mut CcError) -> CcErrorCode;
    pub fn cc_file_name(f: CcFile) -> *mut c_char;

    pub fn cc_fs_read_file(
        inst: CcInstance,
        path: *const c_char,
        out: *mut *mut u8,
        len: *mut usize,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_fs_write_file(
        inst: CcInstance,
        path: *const c_char,
        data: *const u8,
        len: usize,
        perm: CcFileMode,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_fs_stat(
        inst: CcInstance,
        path: *const c_char,
        out: *mut CcFileInfo,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_fs_lstat(
        inst: CcInstance,
        path: *const c_char,
        out: *mut CcFileInfo,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_fs_remove(inst: CcInstance, path: *const c_char, err: *mut CcError) -> CcErrorCode;
    pub fn cc_fs_remove_all(inst: CcInstance, path: *const c_char, err: *mut CcError)
        -> CcErrorCode;
    pub fn cc_fs_mkdir(
        inst: CcInstance,
        path: *const c_char,
        perm: CcFileMode,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_fs_mkdir_all(
        inst: CcInstance,
        path: *const c_char,
        perm: CcFileMode,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_fs_rename(
        inst: CcInstance,
        oldpath: *const c_char,
        newpath: *const c_char,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_fs_symlink(
        inst: CcInstance,
        oldname: *const c_char,
        newname: *const c_char,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_fs_readlink(
        inst: CcInstance,
        path: *const c_char,
        out: *mut *mut c_char,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_fs_read_dir(
        inst: CcInstance,
        path: *const c_char,
        out: *mut *mut CcDirEntry,
        count: *mut usize,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_fs_chmod(
        inst: CcInstance,
        path: *const c_char,
        mode: CcFileMode,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_fs_chown(
        inst: CcInstance,
        path: *const c_char,
        uid: c_int,
        gid: c_int,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_fs_chtimes(
        inst: CcInstance,
        path: *const c_char,
        atime_unix: i64,
        mtime_unix: i64,
        err: *mut CcError,
    ) -> CcErrorCode;

    // Command execution
    pub fn cc_cmd_new(
        inst: CcInstance,
        name: *const c_char,
        args: *const *const c_char,
        out: *mut CcCmd,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_cmd_entrypoint(
        inst: CcInstance,
        args: *const *const c_char,
        out: *mut CcCmd,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_cmd_free(cmd: CcCmd);
    pub fn cc_cmd_set_dir(cmd: CcCmd, dir: *const c_char, err: *mut CcError) -> CcErrorCode;
    pub fn cc_cmd_set_env(
        cmd: CcCmd,
        key: *const c_char,
        value: *const c_char,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_cmd_get_env(cmd: CcCmd, key: *const c_char) -> *mut c_char;
    pub fn cc_cmd_environ(
        cmd: CcCmd,
        out: *mut *mut *mut c_char,
        count: *mut usize,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_cmd_start(cmd: CcCmd, err: *mut CcError) -> CcErrorCode;
    pub fn cc_cmd_wait(cmd: CcCmd, exit_code: *mut c_int, err: *mut CcError) -> CcErrorCode;
    pub fn cc_cmd_run(cmd: CcCmd, exit_code: *mut c_int, err: *mut CcError) -> CcErrorCode;
    pub fn cc_cmd_output(
        cmd: CcCmd,
        out: *mut *mut u8,
        len: *mut usize,
        exit_code: *mut c_int,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_cmd_combined_output(
        cmd: CcCmd,
        out: *mut *mut u8,
        len: *mut usize,
        exit_code: *mut c_int,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_cmd_exit_code(cmd: CcCmd) -> c_int;
    pub fn cc_cmd_kill(cmd: CcCmd, err: *mut CcError) -> CcErrorCode;

    pub fn cc_instance_exec(
        inst: CcInstance,
        name: *const c_char,
        args: *const *const c_char,
        err: *mut CcError,
    ) -> CcErrorCode;

    // Networking
    pub fn cc_net_listen(
        inst: CcInstance,
        network: *const c_char,
        address: *const c_char,
        out: *mut CcListener,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_listener_accept(
        ln: CcListener,
        out: *mut CcConn,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_listener_close(ln: CcListener, err: *mut CcError) -> CcErrorCode;
    pub fn cc_listener_addr(ln: CcListener) -> *mut c_char;
    pub fn cc_conn_read(
        c: CcConn,
        buf: *mut u8,
        len: usize,
        n: *mut usize,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_conn_write(
        c: CcConn,
        buf: *const u8,
        len: usize,
        n: *mut usize,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_conn_close(c: CcConn, err: *mut CcError) -> CcErrorCode;
    pub fn cc_conn_local_addr(c: CcConn) -> *mut c_char;
    pub fn cc_conn_remote_addr(c: CcConn) -> *mut c_char;

    // Snapshots
    pub fn cc_fs_snapshot(
        inst: CcInstance,
        opts: *const CcSnapshotOptions,
        out: *mut CcSnapshot,
        err: *mut CcError,
    ) -> CcErrorCode;
    pub fn cc_snapshot_cache_key(snap: CcSnapshot) -> *mut c_char;
    pub fn cc_snapshot_parent(snap: CcSnapshot) -> CcSnapshot;
    pub fn cc_snapshot_close(snap: CcSnapshot, err: *mut CcError) -> CcErrorCode;
    pub fn cc_snapshot_as_source(snap: CcSnapshot) -> CcInstanceSource;

    // Dockerfile building
    pub fn cc_build_dockerfile_source(
        client: CcOciClient,
        dockerfile: *const u8,
        dockerfile_len: usize,
        options: *const CcDockerfileOptions,
        cancel: CcCancelToken,
        out_snapshot: *mut CcSnapshot,
        err: *mut CcError,
    );
}
