"""
Low-level ctypes bindings to libcc.

This module provides direct bindings to the C API. Users should prefer
the higher-level wrapper classes in the other modules.
"""

import ctypes
import ctypes.util
import os
import platform
import sys
from ctypes import (
    CFUNCTYPE,
    POINTER,
    Structure,
    byref,
    c_bool,
    c_char_p,
    c_double,
    c_int,
    c_int64,
    c_size_t,
    c_uint8,
    c_uint32,
    c_uint64,
    c_void_p,
)
from typing import Any

from .errors import CCError, HypervisorUnavailableError, error_from_code

# ==========================================================================
# Library loading
# ==========================================================================


def _find_library() -> str:
    """Find the libcc shared library."""
    # Check for explicit path via environment
    if "LIBCC_PATH" in os.environ:
        return os.environ["LIBCC_PATH"]

    # Platform-specific library names
    system = platform.system()
    if system == "Darwin":
        lib_name = "libcc.dylib"
    elif system == "Linux":
        lib_name = "libcc.so"
    elif system == "Windows":
        lib_name = "libcc.dll"
    else:
        raise RuntimeError(f"Unsupported platform: {system}")

    # Search paths
    search_paths = [
        # Same directory as this file
        os.path.dirname(__file__),
        # Parent directory (package root)
        os.path.dirname(os.path.dirname(__file__)),
        # Current working directory
        os.getcwd(),
        # build directory relative to cwd
        os.path.join(os.getcwd(), "build"),
        # Standard library paths
        "/usr/local/lib",
        "/usr/lib",
    ]

    # Add paths from environment
    if "LD_LIBRARY_PATH" in os.environ:
        search_paths.extend(os.environ["LD_LIBRARY_PATH"].split(os.pathsep))
    if "DYLD_LIBRARY_PATH" in os.environ:
        search_paths.extend(os.environ["DYLD_LIBRARY_PATH"].split(os.pathsep))

    for path in search_paths:
        lib_path = os.path.join(path, lib_name)
        if os.path.exists(lib_path):
            return lib_path

    # Try ctypes.util.find_library as last resort
    found = ctypes.util.find_library("cc")
    if found:
        return found

    raise RuntimeError(
        f"Could not find {lib_name}. Set LIBCC_PATH environment variable "
        "or place the library in the same directory as the Python package."
    )


# Load the library
_lib: Any = None


def _get_lib() -> Any:
    """Get the loaded library, loading it if necessary."""
    global _lib
    if _lib is None:
        lib_path = _find_library()
        _lib = ctypes.CDLL(lib_path)
        _setup_functions(_lib)
    return _lib


# ==========================================================================
# Handle types
# ==========================================================================


class Handle(Structure):
    """Base handle type (opaque reference to Go object)."""

    _fields_ = [("_h", c_uint64)]

    def __bool__(self) -> bool:
        return self._h != 0

    @classmethod
    def invalid(cls) -> "Handle":
        return cls(0)


class OCIClientHandle(Handle):
    pass


class InstanceSourceHandle(Handle):
    pass


class InstanceHandle(Handle):
    pass


class FileHandle(Handle):
    pass


class CmdHandle(Handle):
    pass


class ListenerHandle(Handle):
    pass


class ConnHandle(Handle):
    pass


class SnapshotHandle(Handle):
    pass


class SnapshotFactoryHandle(Handle):
    pass


class CancelTokenHandle(Handle):
    pass


# ==========================================================================
# Error handling
# ==========================================================================


class CCErrorStruct(Structure):
    """C error structure."""

    _fields_ = [
        ("code", c_int),
        ("message", c_char_p),
        ("op", c_char_p),
        ("path", c_char_p),
    ]


def check_error(code: int, err: CCErrorStruct) -> None:
    """Check error code and raise exception if non-zero."""
    if code == 0:
        return

    message = err.message.decode("utf-8") if err.message else "Unknown error"
    op = err.op.decode("utf-8") if err.op else None
    path = err.path.decode("utf-8") if err.path else None

    # Free the error strings
    lib = _get_lib()
    lib.cc_error_free(byref(err))

    raise error_from_code(code, message, op, path)


# ==========================================================================
# Structures
# ==========================================================================


class PullOptionsStruct(Structure):
    _fields_ = [
        ("platform_os", c_char_p),
        ("platform_arch", c_char_p),
        ("username", c_char_p),
        ("password", c_char_p),
        ("policy", c_int),
    ]


class DownloadProgressStruct(Structure):
    _fields_ = [
        ("current", c_int64),
        ("total", c_int64),
        ("filename", c_char_p),
        ("blob_index", c_int),
        ("blob_count", c_int),
        ("bytes_per_second", c_double),
        ("eta_seconds", c_double),
    ]


# Progress callback type
ProgressCallbackType = CFUNCTYPE(None, POINTER(DownloadProgressStruct), c_void_p)


class MountConfigStruct(Structure):
    _fields_ = [
        ("tag", c_char_p),
        ("host_path", c_char_p),
        ("writable", c_bool),
    ]


class InstanceOptionsStruct(Structure):
    _fields_ = [
        ("memory_mb", c_uint64),
        ("cpus", c_int),
        ("timeout_seconds", c_double),
        ("user", c_char_p),
        ("enable_dmesg", c_bool),
        ("mounts", POINTER(MountConfigStruct)),
        ("mount_count", c_size_t),
    ]


class FileInfoStruct(Structure):
    _fields_ = [
        ("name", c_char_p),
        ("size", c_int64),
        ("mode", c_uint32),
        ("mod_time_unix", c_int64),
        ("is_dir", c_bool),
        ("is_symlink", c_bool),
    ]


class DirEntryStruct(Structure):
    _fields_ = [
        ("name", c_char_p),
        ("is_dir", c_bool),
        ("mode", c_uint32),
    ]


class ImageConfigStruct(Structure):
    _fields_ = [
        ("architecture", c_char_p),
        ("env", POINTER(c_char_p)),
        ("env_count", c_size_t),
        ("working_dir", c_char_p),
        ("entrypoint", POINTER(c_char_p)),
        ("entrypoint_count", c_size_t),
        ("cmd", POINTER(c_char_p)),
        ("cmd_count", c_size_t),
        ("user", c_char_p),
    ]


class CapabilitiesStruct(Structure):
    _fields_ = [
        ("hypervisor_available", c_bool),
        ("max_memory_mb", c_uint64),
        ("max_cpus", c_int),
        ("architecture", c_void_p),  # String pointer that must be freed
    ]


class SnapshotOptionsStruct(Structure):
    _fields_ = [
        ("excludes", POINTER(c_char_p)),
        ("exclude_count", c_size_t),
        ("cache_dir", c_char_p),
    ]


# ==========================================================================
# Function setup
# ==========================================================================


def _setup_functions(lib: Any) -> None:
    """Set up function signatures for the library."""

    # API version
    lib.cc_api_version.argtypes = []
    lib.cc_api_version.restype = c_char_p

    lib.cc_api_version_compatible.argtypes = [c_int, c_int]
    lib.cc_api_version_compatible.restype = c_bool

    # Library init/shutdown
    lib.cc_init.argtypes = []
    lib.cc_init.restype = c_int

    lib.cc_shutdown.argtypes = []
    lib.cc_shutdown.restype = None

    # Hypervisor support
    lib.cc_supports_hypervisor.argtypes = [POINTER(CCErrorStruct)]
    lib.cc_supports_hypervisor.restype = c_int

    lib.cc_guest_protocol_version.argtypes = []
    lib.cc_guest_protocol_version.restype = c_int

    lib.cc_query_capabilities.argtypes = [POINTER(CapabilitiesStruct), POINTER(CCErrorStruct)]
    lib.cc_query_capabilities.restype = c_int

    # Error/memory management
    lib.cc_error_free.argtypes = [POINTER(CCErrorStruct)]
    lib.cc_error_free.restype = None

    lib.cc_free_string.argtypes = [c_void_p]
    lib.cc_free_string.restype = None

    lib.cc_free_bytes.argtypes = [POINTER(c_uint8)]
    lib.cc_free_bytes.restype = None

    # Cancel token
    lib.cc_cancel_token_new.argtypes = []
    lib.cc_cancel_token_new.restype = CancelTokenHandle

    lib.cc_cancel_token_cancel.argtypes = [CancelTokenHandle]
    lib.cc_cancel_token_cancel.restype = None

    lib.cc_cancel_token_is_cancelled.argtypes = [CancelTokenHandle]
    lib.cc_cancel_token_is_cancelled.restype = c_bool

    lib.cc_cancel_token_free.argtypes = [CancelTokenHandle]
    lib.cc_cancel_token_free.restype = None

    # OCI client
    lib.cc_oci_client_new.argtypes = [POINTER(OCIClientHandle), POINTER(CCErrorStruct)]
    lib.cc_oci_client_new.restype = c_int

    lib.cc_oci_client_new_with_cache.argtypes = [c_char_p, POINTER(OCIClientHandle), POINTER(CCErrorStruct)]
    lib.cc_oci_client_new_with_cache.restype = c_int

    lib.cc_oci_client_free.argtypes = [OCIClientHandle]
    lib.cc_oci_client_free.restype = None

    lib.cc_oci_client_pull.argtypes = [
        OCIClientHandle,
        c_char_p,
        POINTER(PullOptionsStruct),
        ProgressCallbackType,
        c_void_p,
        CancelTokenHandle,
        POINTER(InstanceSourceHandle),
        POINTER(CCErrorStruct),
    ]
    lib.cc_oci_client_pull.restype = c_int

    lib.cc_oci_client_load_tar.argtypes = [
        OCIClientHandle,
        c_char_p,
        POINTER(PullOptionsStruct),
        POINTER(InstanceSourceHandle),
        POINTER(CCErrorStruct),
    ]
    lib.cc_oci_client_load_tar.restype = c_int

    lib.cc_oci_client_load_dir.argtypes = [
        OCIClientHandle,
        c_char_p,
        POINTER(PullOptionsStruct),
        POINTER(InstanceSourceHandle),
        POINTER(CCErrorStruct),
    ]
    lib.cc_oci_client_load_dir.restype = c_int

    lib.cc_oci_client_export_dir.argtypes = [
        OCIClientHandle,
        InstanceSourceHandle,
        c_char_p,
        POINTER(CCErrorStruct),
    ]
    lib.cc_oci_client_export_dir.restype = c_int

    lib.cc_oci_client_cache_dir.argtypes = [OCIClientHandle]
    lib.cc_oci_client_cache_dir.restype = c_void_p  # Returns string that must be freed

    lib.cc_instance_source_free.argtypes = [InstanceSourceHandle]
    lib.cc_instance_source_free.restype = None

    lib.cc_source_get_config.argtypes = [
        InstanceSourceHandle,
        POINTER(POINTER(ImageConfigStruct)),
        POINTER(CCErrorStruct),
    ]
    lib.cc_source_get_config.restype = c_int

    lib.cc_image_config_free.argtypes = [POINTER(ImageConfigStruct)]
    lib.cc_image_config_free.restype = None

    # Instance
    lib.cc_instance_new.argtypes = [
        InstanceSourceHandle,
        POINTER(InstanceOptionsStruct),
        POINTER(InstanceHandle),
        POINTER(CCErrorStruct),
    ]
    lib.cc_instance_new.restype = c_int

    lib.cc_instance_close.argtypes = [InstanceHandle, POINTER(CCErrorStruct)]
    lib.cc_instance_close.restype = c_int

    lib.cc_instance_wait.argtypes = [InstanceHandle, CancelTokenHandle, POINTER(CCErrorStruct)]
    lib.cc_instance_wait.restype = c_int

    lib.cc_instance_id.argtypes = [InstanceHandle]
    lib.cc_instance_id.restype = c_void_p  # Returns string that must be freed

    lib.cc_instance_is_running.argtypes = [InstanceHandle]
    lib.cc_instance_is_running.restype = c_bool

    lib.cc_instance_set_console_size.argtypes = [InstanceHandle, c_int, c_int, POINTER(CCErrorStruct)]
    lib.cc_instance_set_console_size.restype = c_int

    lib.cc_instance_set_network_enabled.argtypes = [InstanceHandle, c_bool, POINTER(CCErrorStruct)]
    lib.cc_instance_set_network_enabled.restype = c_int

    # Filesystem operations
    lib.cc_fs_open.argtypes = [InstanceHandle, c_char_p, POINTER(FileHandle), POINTER(CCErrorStruct)]
    lib.cc_fs_open.restype = c_int

    lib.cc_fs_create.argtypes = [InstanceHandle, c_char_p, POINTER(FileHandle), POINTER(CCErrorStruct)]
    lib.cc_fs_create.restype = c_int

    lib.cc_fs_open_file.argtypes = [
        InstanceHandle,
        c_char_p,
        c_int,
        c_uint32,
        POINTER(FileHandle),
        POINTER(CCErrorStruct),
    ]
    lib.cc_fs_open_file.restype = c_int

    lib.cc_file_close.argtypes = [FileHandle, POINTER(CCErrorStruct)]
    lib.cc_file_close.restype = c_int

    lib.cc_file_read.argtypes = [FileHandle, POINTER(c_uint8), c_size_t, POINTER(c_size_t), POINTER(CCErrorStruct)]
    lib.cc_file_read.restype = c_int

    lib.cc_file_write.argtypes = [FileHandle, POINTER(c_uint8), c_size_t, POINTER(c_size_t), POINTER(CCErrorStruct)]
    lib.cc_file_write.restype = c_int

    lib.cc_file_seek.argtypes = [FileHandle, c_int64, c_int, POINTER(c_int64), POINTER(CCErrorStruct)]
    lib.cc_file_seek.restype = c_int

    lib.cc_file_sync.argtypes = [FileHandle, POINTER(CCErrorStruct)]
    lib.cc_file_sync.restype = c_int

    lib.cc_file_truncate.argtypes = [FileHandle, c_int64, POINTER(CCErrorStruct)]
    lib.cc_file_truncate.restype = c_int

    lib.cc_file_stat.argtypes = [FileHandle, POINTER(FileInfoStruct), POINTER(CCErrorStruct)]
    lib.cc_file_stat.restype = c_int

    lib.cc_file_name.argtypes = [FileHandle]
    lib.cc_file_name.restype = c_void_p  # Returns string that must be freed

    lib.cc_fs_read_file.argtypes = [
        InstanceHandle,
        c_char_p,
        POINTER(POINTER(c_uint8)),
        POINTER(c_size_t),
        POINTER(CCErrorStruct),
    ]
    lib.cc_fs_read_file.restype = c_int

    lib.cc_fs_write_file.argtypes = [InstanceHandle, c_char_p, POINTER(c_uint8), c_size_t, c_uint32, POINTER(CCErrorStruct)]
    lib.cc_fs_write_file.restype = c_int

    lib.cc_fs_stat.argtypes = [InstanceHandle, c_char_p, POINTER(FileInfoStruct), POINTER(CCErrorStruct)]
    lib.cc_fs_stat.restype = c_int

    lib.cc_fs_lstat.argtypes = [InstanceHandle, c_char_p, POINTER(FileInfoStruct), POINTER(CCErrorStruct)]
    lib.cc_fs_lstat.restype = c_int

    lib.cc_file_info_free.argtypes = [POINTER(FileInfoStruct)]
    lib.cc_file_info_free.restype = None

    lib.cc_fs_remove.argtypes = [InstanceHandle, c_char_p, POINTER(CCErrorStruct)]
    lib.cc_fs_remove.restype = c_int

    lib.cc_fs_remove_all.argtypes = [InstanceHandle, c_char_p, POINTER(CCErrorStruct)]
    lib.cc_fs_remove_all.restype = c_int

    lib.cc_fs_mkdir.argtypes = [InstanceHandle, c_char_p, c_uint32, POINTER(CCErrorStruct)]
    lib.cc_fs_mkdir.restype = c_int

    lib.cc_fs_mkdir_all.argtypes = [InstanceHandle, c_char_p, c_uint32, POINTER(CCErrorStruct)]
    lib.cc_fs_mkdir_all.restype = c_int

    lib.cc_fs_rename.argtypes = [InstanceHandle, c_char_p, c_char_p, POINTER(CCErrorStruct)]
    lib.cc_fs_rename.restype = c_int

    lib.cc_fs_symlink.argtypes = [InstanceHandle, c_char_p, c_char_p, POINTER(CCErrorStruct)]
    lib.cc_fs_symlink.restype = c_int

    lib.cc_fs_readlink.argtypes = [InstanceHandle, c_char_p, POINTER(c_void_p), POINTER(CCErrorStruct)]
    lib.cc_fs_readlink.restype = c_int

    lib.cc_fs_read_dir.argtypes = [
        InstanceHandle,
        c_char_p,
        POINTER(POINTER(DirEntryStruct)),
        POINTER(c_size_t),
        POINTER(CCErrorStruct),
    ]
    lib.cc_fs_read_dir.restype = c_int

    lib.cc_dir_entries_free.argtypes = [POINTER(DirEntryStruct), c_size_t]
    lib.cc_dir_entries_free.restype = None

    lib.cc_fs_chmod.argtypes = [InstanceHandle, c_char_p, c_uint32, POINTER(CCErrorStruct)]
    lib.cc_fs_chmod.restype = c_int

    lib.cc_fs_chown.argtypes = [InstanceHandle, c_char_p, c_int, c_int, POINTER(CCErrorStruct)]
    lib.cc_fs_chown.restype = c_int

    lib.cc_fs_chtimes.argtypes = [InstanceHandle, c_char_p, c_int64, c_int64, POINTER(CCErrorStruct)]
    lib.cc_fs_chtimes.restype = c_int

    # Command execution
    lib.cc_cmd_new.argtypes = [InstanceHandle, c_char_p, POINTER(c_char_p), POINTER(CmdHandle), POINTER(CCErrorStruct)]
    lib.cc_cmd_new.restype = c_int

    lib.cc_cmd_entrypoint.argtypes = [InstanceHandle, POINTER(c_char_p), POINTER(CmdHandle), POINTER(CCErrorStruct)]
    lib.cc_cmd_entrypoint.restype = c_int

    lib.cc_cmd_free.argtypes = [CmdHandle]
    lib.cc_cmd_free.restype = None

    lib.cc_cmd_set_dir.argtypes = [CmdHandle, c_char_p, POINTER(CCErrorStruct)]
    lib.cc_cmd_set_dir.restype = c_int

    lib.cc_cmd_set_env.argtypes = [CmdHandle, c_char_p, c_char_p, POINTER(CCErrorStruct)]
    lib.cc_cmd_set_env.restype = c_int

    lib.cc_cmd_get_env.argtypes = [CmdHandle, c_char_p]
    lib.cc_cmd_get_env.restype = c_void_p  # Returns string that must be freed

    lib.cc_cmd_environ.argtypes = [CmdHandle, POINTER(POINTER(c_void_p)), POINTER(c_size_t), POINTER(CCErrorStruct)]
    lib.cc_cmd_environ.restype = c_int

    lib.cc_cmd_start.argtypes = [CmdHandle, POINTER(CCErrorStruct)]
    lib.cc_cmd_start.restype = c_int

    lib.cc_cmd_wait.argtypes = [CmdHandle, POINTER(c_int), POINTER(CCErrorStruct)]
    lib.cc_cmd_wait.restype = c_int

    lib.cc_cmd_run.argtypes = [CmdHandle, POINTER(c_int), POINTER(CCErrorStruct)]
    lib.cc_cmd_run.restype = c_int

    lib.cc_cmd_output.argtypes = [
        CmdHandle,
        POINTER(POINTER(c_uint8)),
        POINTER(c_size_t),
        POINTER(c_int),
        POINTER(CCErrorStruct),
    ]
    lib.cc_cmd_output.restype = c_int

    lib.cc_cmd_combined_output.argtypes = [
        CmdHandle,
        POINTER(POINTER(c_uint8)),
        POINTER(c_size_t),
        POINTER(c_int),
        POINTER(CCErrorStruct),
    ]
    lib.cc_cmd_combined_output.restype = c_int

    lib.cc_cmd_exit_code.argtypes = [CmdHandle]
    lib.cc_cmd_exit_code.restype = c_int

    lib.cc_cmd_kill.argtypes = [CmdHandle, POINTER(CCErrorStruct)]
    lib.cc_cmd_kill.restype = c_int

    lib.cc_instance_exec.argtypes = [InstanceHandle, c_char_p, POINTER(c_char_p), POINTER(CCErrorStruct)]
    lib.cc_instance_exec.restype = c_int

    # Networking
    lib.cc_net_listen.argtypes = [
        InstanceHandle,
        c_char_p,
        c_char_p,
        POINTER(ListenerHandle),
        POINTER(CCErrorStruct),
    ]
    lib.cc_net_listen.restype = c_int

    lib.cc_listener_accept.argtypes = [ListenerHandle, POINTER(ConnHandle), POINTER(CCErrorStruct)]
    lib.cc_listener_accept.restype = c_int

    lib.cc_listener_close.argtypes = [ListenerHandle, POINTER(CCErrorStruct)]
    lib.cc_listener_close.restype = c_int

    lib.cc_listener_addr.argtypes = [ListenerHandle]
    lib.cc_listener_addr.restype = c_void_p  # Returns string that must be freed

    lib.cc_conn_read.argtypes = [ConnHandle, POINTER(c_uint8), c_size_t, POINTER(c_size_t), POINTER(CCErrorStruct)]
    lib.cc_conn_read.restype = c_int

    lib.cc_conn_write.argtypes = [ConnHandle, POINTER(c_uint8), c_size_t, POINTER(c_size_t), POINTER(CCErrorStruct)]
    lib.cc_conn_write.restype = c_int

    lib.cc_conn_close.argtypes = [ConnHandle, POINTER(CCErrorStruct)]
    lib.cc_conn_close.restype = c_int

    lib.cc_conn_local_addr.argtypes = [ConnHandle]
    lib.cc_conn_local_addr.restype = c_void_p  # Returns string that must be freed

    lib.cc_conn_remote_addr.argtypes = [ConnHandle]
    lib.cc_conn_remote_addr.restype = c_void_p  # Returns string that must be freed

    # Snapshots
    lib.cc_fs_snapshot.argtypes = [
        InstanceHandle,
        POINTER(SnapshotOptionsStruct),
        POINTER(SnapshotHandle),
        POINTER(CCErrorStruct),
    ]
    lib.cc_fs_snapshot.restype = c_int

    lib.cc_snapshot_cache_key.argtypes = [SnapshotHandle]
    lib.cc_snapshot_cache_key.restype = c_void_p  # Returns string that must be freed

    lib.cc_snapshot_parent.argtypes = [SnapshotHandle]
    lib.cc_snapshot_parent.restype = SnapshotHandle

    lib.cc_snapshot_close.argtypes = [SnapshotHandle, POINTER(CCErrorStruct)]
    lib.cc_snapshot_close.restype = c_int

    lib.cc_snapshot_as_source.argtypes = [SnapshotHandle]
    lib.cc_snapshot_as_source.restype = InstanceSourceHandle


# ==========================================================================
# Helper for freeing strings
# ==========================================================================


def _get_string_and_free(lib, ptr: int) -> str:
    """Extract string from a c_void_p pointer and free it.

    This is needed because ctypes converts c_char_p return values to Python bytes,
    losing the original pointer needed for freeing.
    """
    if not ptr:
        return ""
    value = ctypes.cast(ptr, c_char_p).value
    result = value.decode("utf-8") if value else ""
    lib.cc_free_string(ptr)
    return result


# ==========================================================================
# High-level wrapper functions
# ==========================================================================


def api_version() -> str:
    """Get the API version string."""
    lib = _get_lib()
    # Use c_void_p to keep the original pointer for freeing
    lib.cc_api_version.restype = c_void_p
    ptr = lib.cc_api_version()
    if not ptr:
        return ""
    version = ctypes.cast(ptr, c_char_p).value.decode("utf-8")
    lib.cc_free_string(ptr)
    return version


def api_version_compatible(major: int, minor: int) -> bool:
    """Check if the runtime library is compatible with the given version."""
    return bool(_get_lib().cc_api_version_compatible(major, minor))


def init() -> None:
    """Initialize the library. Must be called before any other function."""
    code = _get_lib().cc_init()
    if code != 0:
        raise CCError(f"Failed to initialize library: code {code}", code=code)


def shutdown() -> None:
    """Shutdown the library and release all resources."""
    _get_lib().cc_shutdown()


def supports_hypervisor() -> bool:
    """Check if hypervisor is available. Raises HypervisorUnavailableError if not."""
    lib = _get_lib()
    err = CCErrorStruct()
    code = lib.cc_supports_hypervisor(byref(err))
    if code == 6:  # CC_ERR_HYPERVISOR_UNAVAILABLE
        lib.cc_error_free(byref(err))
        return False
    check_error(code, err)
    return True


def guest_protocol_version() -> int:
    """Get the guest protocol version supported by this library."""
    return _get_lib().cc_guest_protocol_version()


# Convenience function to get the library for use in other modules
def get_lib() -> Any:
    """Get the loaded library instance."""
    return _get_lib()
