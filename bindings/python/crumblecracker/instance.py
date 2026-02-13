"""
Instance class for running VM instances and filesystem operations.
"""

from __future__ import annotations

from ctypes import POINTER, byref, c_char_p, c_int, c_int64, c_size_t, c_uint8, c_void_p
from typing import TYPE_CHECKING, Any

from . import _ffi
from .errors import CCError
from .types import (
    Capabilities,
    DirEntry,
    FileInfo,
    InstanceOptions,
    MountConfig,
    SeekWhence,
    SnapshotOptions,
)

if TYPE_CHECKING:
    from .client import CancelToken, InstanceSource
    from .cmd import Cmd


class File:
    """A file handle for reading/writing files in a VM instance.

    Supports context manager protocol for automatic cleanup.
    """

    def __init__(self, handle: Any, path: str, *, _ipc: bool = False) -> None:
        self._handle = handle
        self._path = path
        self._closed = False
        self._ipc = _ipc

    def __del__(self) -> None:
        self.close()

    def __enter__(self) -> "File":
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()

    def close(self) -> None:
        """Close the file."""
        if not self._closed and self._handle is not None:
            if self._ipc:
                _ffi.get_ipc_backend().file_close(self._handle)
            else:
                lib = _ffi.get_lib()
                err = _ffi.CCErrorStruct()
                lib.cc_file_close(self._handle, byref(err))
            self._closed = True

    @property
    def closed(self) -> bool:
        """Check if the file is closed."""
        return self._closed

    @property
    def name(self) -> str:
        """Get the file name."""
        if self._closed:
            return self._path
        if self._ipc:
            return _ffi.get_ipc_backend().file_name(self._handle) or self._path
        lib = _ffi.get_lib()
        ptr = lib.cc_file_name(self._handle)
        name = _ffi._get_string_and_free(lib, ptr)
        return name if name else self._path

    def read(self, size: int = -1) -> bytes:
        """Read up to size bytes from the file.

        Args:
            size: Maximum number of bytes to read. If -1, read until EOF.

        Returns:
            The bytes read.
        """
        if self._closed:
            raise CCError("File is closed", code=4)

        if size == -1:
            chunks = []
            chunk_size = 65536
            while True:
                data = self._read_chunk(chunk_size)
                if not data:
                    break
                chunks.append(data)
            return b"".join(chunks)

        return self._read_chunk(size)

    def _read_chunk(self, size: int) -> bytes:
        """Read a chunk of data."""
        if self._ipc:
            return _ffi.get_ipc_backend().file_read(self._handle, size)

        lib = _ffi.get_lib()
        buf = (c_uint8 * size)()
        n = c_size_t()
        err = _ffi.CCErrorStruct()

        code = lib.cc_file_read(self._handle, buf, size, byref(n), byref(err))
        _ffi.check_error(code, err)

        return bytes(buf[: n.value])

    def write(self, data: bytes) -> int:
        """Write data to the file.

        Args:
            data: Bytes to write

        Returns:
            Number of bytes written
        """
        if self._closed:
            raise CCError("File is closed", code=4)

        if self._ipc:
            return _ffi.get_ipc_backend().file_write(self._handle, data)

        lib = _ffi.get_lib()
        buf = (c_uint8 * len(data)).from_buffer_copy(data)
        n = c_size_t()
        err = _ffi.CCErrorStruct()

        code = lib.cc_file_write(self._handle, buf, len(data), byref(n), byref(err))
        _ffi.check_error(code, err)

        return n.value

    def seek(self, offset: int, whence: SeekWhence = SeekWhence.SET) -> int:
        """Seek to a position in the file."""
        if self._closed:
            raise CCError("File is closed", code=4)

        if self._ipc:
            return _ffi.get_ipc_backend().file_seek(self._handle, offset, int(whence))

        lib = _ffi.get_lib()
        new_offset = c_int64()
        err = _ffi.CCErrorStruct()

        code = lib.cc_file_seek(self._handle, offset, int(whence), byref(new_offset), byref(err))
        _ffi.check_error(code, err)

        return new_offset.value

    def sync(self) -> None:
        """Sync the file to disk."""
        if self._closed:
            raise CCError("File is closed", code=4)

        if self._ipc:
            _ffi.get_ipc_backend().file_sync(self._handle)
            return

        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()

        code = lib.cc_file_sync(self._handle, byref(err))
        _ffi.check_error(code, err)

    def truncate(self, size: int) -> None:
        """Truncate the file to the given size."""
        if self._closed:
            raise CCError("File is closed", code=4)

        if self._ipc:
            _ffi.get_ipc_backend().file_truncate(self._handle, size)
            return

        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()

        code = lib.cc_file_truncate(self._handle, size, byref(err))
        _ffi.check_error(code, err)

    def stat(self) -> FileInfo:
        """Get file information."""
        if self._closed:
            raise CCError("File is closed", code=4)

        if self._ipc:
            return _ffi.get_ipc_backend().file_stat(self._handle)

        lib = _ffi.get_lib()
        info = _ffi.FileInfoStruct()
        err = _ffi.CCErrorStruct()

        code = lib.cc_file_stat(self._handle, byref(info), byref(err))
        _ffi.check_error(code, err)

        try:
            return FileInfo(
                name=info.name.decode("utf-8") if info.name else "",
                size=info.size,
                mode=info.mode,
                mod_time_unix=info.mod_time_unix,
                is_dir=info.is_dir,
                is_symlink=info.is_symlink,
            )
        finally:
            lib.cc_file_info_free(byref(info))


class Snapshot:
    """A filesystem snapshot from a VM instance.

    Snapshots can be used to create new instances from a known state.
    """

    def __init__(self, handle: Any, *, _ipc: bool = False) -> None:
        self._handle = handle
        self._closed = False
        self._ipc = _ipc

    def __del__(self) -> None:
        self.close()

    def __enter__(self) -> "Snapshot":
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()

    def close(self) -> None:
        """Close the snapshot."""
        if not self._closed and self._handle is not None:
            if self._ipc:
                _ffi.get_ipc_backend().snapshot_close(self._handle)
            else:
                lib = _ffi.get_lib()
                err = _ffi.CCErrorStruct()
                lib.cc_snapshot_close(self._handle, byref(err))
            self._closed = True

    @property
    def cache_key(self) -> str:
        """Get the snapshot cache key."""
        if self._closed:
            raise CCError("Snapshot is closed", code=4)

        if self._ipc:
            return _ffi.get_ipc_backend().snapshot_cache_key(self._handle)

        lib = _ffi.get_lib()
        ptr = lib.cc_snapshot_cache_key(self._handle)
        return _ffi._get_string_and_free(lib, ptr)

    @property
    def parent(self) -> "Snapshot | None":
        """Get the parent snapshot, if any."""
        if self._closed:
            raise CCError("Snapshot is closed", code=4)

        if self._ipc:
            parent_handle = _ffi.get_ipc_backend().snapshot_parent(self._handle)
            if parent_handle == 0:
                return None
            return Snapshot(parent_handle, _ipc=True)

        lib = _ffi.get_lib()
        parent_handle = lib.cc_snapshot_parent(self._handle)

        if not parent_handle:
            return None

        return Snapshot(parent_handle)

    def as_source(self) -> "InstanceSource":
        """Convert the snapshot to an instance source."""
        if self._closed:
            raise CCError("Snapshot is closed", code=4)

        from .client import InstanceSource

        if self._ipc:
            source_handle = _ffi.get_ipc_backend().snapshot_as_source(self._handle)
            return InstanceSource(source_handle)

        lib = _ffi.get_lib()
        source_handle = lib.cc_snapshot_as_source(self._handle)

        return InstanceSource(source_handle)


class Instance:
    """A running VM instance.

    Provides filesystem operations, command execution, and networking.

    Example:
        with Instance(source, options) as inst:
            # Run a command
            output = inst.command("echo", "hello").output()

            # Read a file
            data = inst.read_file("/etc/hosts")

            # Write a file
            inst.write_file("/tmp/test.txt", b"hello world")
    """

    def __init__(self, source: "InstanceSource", options: InstanceOptions | None = None):
        """Create and start a new VM instance.

        Args:
            source: The instance source (from OCIClient.pull() etc.)
            options: Instance configuration options
        """
        # Initialize state first to prevent __del__ errors if __init__ fails
        self._handle = None
        self._closed = True
        self._ipc = _ffi.using_ipc()

        if self._ipc:
            backend = _ffi.get_ipc_backend()
            mounts = []
            if options and options.mounts:
                mounts = [
                    (m.tag, m.host_path or "", m.writable)
                    for m in options.mounts
                ]
            backend.instance_new(
                source_type=source._ipc_source_type,
                source_path=source._ipc_source_path,
                image_ref=source._ipc_image_ref,
                cache_dir=source._ipc_cache_dir,
                memory_mb=options.memory_mb if options else 256,
                cpus=options.cpus if options else 1,
                timeout_seconds=options.timeout_seconds if options else 0,
                user=options.user or "" if options else "",
                enable_dmesg=options.enable_dmesg if options else False,
                mounts=mounts,
            )
            self._closed = False
            return

        lib = _ffi.get_lib()
        handle = _ffi.InstanceHandle()
        err = _ffi.CCErrorStruct()

        # Build options struct
        opts_ptr = None
        mounts_array = None
        if options:
            opts = _ffi.InstanceOptionsStruct()
            opts.memory_mb = options.memory_mb
            opts.cpus = options.cpus
            opts.timeout_seconds = options.timeout_seconds
            if options.user:
                opts.user = options.user.encode("utf-8")
            opts.enable_dmesg = options.enable_dmesg

            # Handle mounts
            if options.mounts:
                mounts_array = (_ffi.MountConfigStruct * len(options.mounts))()
                for i, mount in enumerate(options.mounts):
                    mounts_array[i].tag = mount.tag.encode("utf-8")
                    if mount.host_path:
                        mounts_array[i].host_path = mount.host_path.encode("utf-8")
                    mounts_array[i].writable = mount.writable
                opts.mounts = mounts_array
                opts.mount_count = len(options.mounts)

            opts_ptr = byref(opts)

        code = lib.cc_instance_new(source.handle, opts_ptr, byref(handle), byref(err))
        _ffi.check_error(code, err)

        self._handle = handle
        self._closed = False

    def __del__(self) -> None:
        self.close()

    def __enter__(self) -> "Instance":
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()

    def close(self) -> None:
        """Close the instance and release resources."""
        if not self._closed:
            if self._ipc:
                _ffi.get_ipc_backend().instance_close()
            elif self._handle:
                lib = _ffi.get_lib()
                err = _ffi.CCErrorStruct()
                lib.cc_instance_close(self._handle, byref(err))
            self._closed = True

    @property
    def handle(self) -> Any:
        """Get the underlying handle."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        return self._handle

    @property
    def id(self) -> str:
        """Get the instance ID."""
        if self._closed:
            return ""
        if self._ipc:
            return _ffi.get_ipc_backend().instance_id()
        lib = _ffi.get_lib()
        ptr = lib.cc_instance_id(self._handle)
        return _ffi._get_string_and_free(lib, ptr)

    @property
    def is_running(self) -> bool:
        """Check if the instance is still running."""
        if self._closed:
            return False
        if self._ipc:
            return _ffi.get_ipc_backend().instance_is_running()
        return bool(_ffi.get_lib().cc_instance_is_running(self._handle))

    def wait(self, cancel_token: "CancelToken | None" = None) -> None:
        """Wait for the instance to terminate."""
        if self._closed:
            return
        if self._ipc:
            _ffi.get_ipc_backend().instance_wait()
            return

        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()

        cancel_handle = _ffi.CancelTokenHandle.invalid()
        if cancel_token:
            cancel_handle = cancel_token.handle

        code = lib.cc_instance_wait(self._handle, cancel_handle, byref(err))
        _ffi.check_error(code, err)

    def set_console_size(self, cols: int, rows: int) -> None:
        """Set the console size for interactive mode."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            _ffi.get_ipc_backend().instance_set_console(cols, rows)
            return

        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()

        code = lib.cc_instance_set_console_size(self._handle, cols, rows, byref(err))
        _ffi.check_error(code, err)

    def set_network_enabled(self, enabled: bool) -> None:
        """Enable or disable network access."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            _ffi.get_ipc_backend().instance_set_network(enabled)
            return

        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()

        code = lib.cc_instance_set_network_enabled(self._handle, enabled, byref(err))
        _ffi.check_error(code, err)

    # ========== Filesystem Operations ==========

    def open(self, path: str) -> File:
        """Open a file for reading."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            ipc_handle = _ffi.get_ipc_backend().fs_open(path)
            return File(ipc_handle, path, _ipc=True)

        lib = _ffi.get_lib()
        handle = _ffi.FileHandle()
        err = _ffi.CCErrorStruct()
        code = lib.cc_fs_open(self._handle, path.encode("utf-8"), byref(handle), byref(err))
        _ffi.check_error(code, err)
        return File(handle, path)

    def create(self, path: str) -> File:
        """Create or truncate a file."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            ipc_handle = _ffi.get_ipc_backend().fs_create(path)
            return File(ipc_handle, path, _ipc=True)

        lib = _ffi.get_lib()
        handle = _ffi.FileHandle()
        err = _ffi.CCErrorStruct()
        code = lib.cc_fs_create(self._handle, path.encode("utf-8"), byref(handle), byref(err))
        _ffi.check_error(code, err)
        return File(handle, path)

    def open_file(self, path: str, flags: int, mode: int = 0o644) -> File:
        """Open a file with specific flags and permissions."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            ipc_handle = _ffi.get_ipc_backend().fs_open_file(path, flags, mode)
            return File(ipc_handle, path, _ipc=True)

        lib = _ffi.get_lib()
        handle = _ffi.FileHandle()
        err = _ffi.CCErrorStruct()
        code = lib.cc_fs_open_file(
            self._handle, path.encode("utf-8"), flags, mode, byref(handle), byref(err)
        )
        _ffi.check_error(code, err)
        return File(handle, path)

    def read_file(self, path: str) -> bytes:
        """Read entire file contents."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            return _ffi.get_ipc_backend().fs_read_file(path)

        lib = _ffi.get_lib()
        data_ptr = POINTER(c_uint8)()
        length = c_size_t()
        err = _ffi.CCErrorStruct()
        code = lib.cc_fs_read_file(
            self._handle, path.encode("utf-8"), byref(data_ptr), byref(length), byref(err)
        )
        _ffi.check_error(code, err)
        if data_ptr and length.value > 0:
            result = bytes(data_ptr[: length.value])
            lib.cc_free_bytes(data_ptr)
            return result
        return b""

    def write_file(self, path: str, data: bytes, mode: int = 0o644) -> None:
        """Write entire file contents."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            _ffi.get_ipc_backend().fs_write_file(path, data, mode)
            return

        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()
        buf = (c_uint8 * len(data)).from_buffer_copy(data)
        code = lib.cc_fs_write_file(
            self._handle, path.encode("utf-8"), buf, len(data), mode, byref(err)
        )
        _ffi.check_error(code, err)

    def stat(self, path: str) -> FileInfo:
        """Get file information by path."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            return _ffi.get_ipc_backend().fs_stat(path)

        lib = _ffi.get_lib()
        info = _ffi.FileInfoStruct()
        err = _ffi.CCErrorStruct()
        code = lib.cc_fs_stat(self._handle, path.encode("utf-8"), byref(info), byref(err))
        _ffi.check_error(code, err)
        try:
            return FileInfo(
                name=info.name.decode("utf-8") if info.name else "",
                size=info.size, mode=info.mode, mod_time_unix=info.mod_time_unix,
                is_dir=info.is_dir, is_symlink=info.is_symlink,
            )
        finally:
            lib.cc_file_info_free(byref(info))

    def lstat(self, path: str) -> FileInfo:
        """Get file information (don't follow symlinks)."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            return _ffi.get_ipc_backend().fs_lstat(path)

        lib = _ffi.get_lib()
        info = _ffi.FileInfoStruct()
        err = _ffi.CCErrorStruct()
        code = lib.cc_fs_lstat(self._handle, path.encode("utf-8"), byref(info), byref(err))
        _ffi.check_error(code, err)
        try:
            return FileInfo(
                name=info.name.decode("utf-8") if info.name else "",
                size=info.size, mode=info.mode, mod_time_unix=info.mod_time_unix,
                is_dir=info.is_dir, is_symlink=info.is_symlink,
            )
        finally:
            lib.cc_file_info_free(byref(info))

    def remove(self, path: str) -> None:
        """Remove a file."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            _ffi.get_ipc_backend().fs_remove(path)
            return
        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()
        code = lib.cc_fs_remove(self._handle, path.encode("utf-8"), byref(err))
        _ffi.check_error(code, err)

    def remove_all(self, path: str) -> None:
        """Remove a file or directory recursively."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            _ffi.get_ipc_backend().fs_remove_all(path)
            return
        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()
        code = lib.cc_fs_remove_all(self._handle, path.encode("utf-8"), byref(err))
        _ffi.check_error(code, err)

    def mkdir(self, path: str, mode: int = 0o755) -> None:
        """Create a directory."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            _ffi.get_ipc_backend().fs_mkdir(path, mode)
            return
        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()
        code = lib.cc_fs_mkdir(self._handle, path.encode("utf-8"), mode, byref(err))
        _ffi.check_error(code, err)

    def mkdir_all(self, path: str, mode: int = 0o755) -> None:
        """Create a directory and all parent directories."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            _ffi.get_ipc_backend().fs_mkdir_all(path, mode)
            return
        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()
        code = lib.cc_fs_mkdir_all(self._handle, path.encode("utf-8"), mode, byref(err))
        _ffi.check_error(code, err)

    def rename(self, old_path: str, new_path: str) -> None:
        """Rename a file or directory."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            _ffi.get_ipc_backend().fs_rename(old_path, new_path)
            return
        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()
        code = lib.cc_fs_rename(
            self._handle, old_path.encode("utf-8"), new_path.encode("utf-8"), byref(err)
        )
        _ffi.check_error(code, err)

    def symlink(self, target: str, link_path: str) -> None:
        """Create a symbolic link."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            _ffi.get_ipc_backend().fs_symlink(target, link_path)
            return
        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()
        code = lib.cc_fs_symlink(
            self._handle, target.encode("utf-8"), link_path.encode("utf-8"), byref(err)
        )
        _ffi.check_error(code, err)

    def readlink(self, path: str) -> str:
        """Read a symbolic link target."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            return _ffi.get_ipc_backend().fs_readlink(path)
        lib = _ffi.get_lib()
        target = c_void_p()
        err = _ffi.CCErrorStruct()
        code = lib.cc_fs_readlink(self._handle, path.encode("utf-8"), byref(target), byref(err))
        _ffi.check_error(code, err)
        return _ffi._get_string_and_free(lib, target.value or 0)

    def read_dir(self, path: str) -> list[DirEntry]:
        """Read directory contents."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            return _ffi.get_ipc_backend().fs_read_dir(path)

        lib = _ffi.get_lib()
        entries_ptr = POINTER(_ffi.DirEntryStruct)()
        count = c_size_t()
        err = _ffi.CCErrorStruct()
        code = lib.cc_fs_read_dir(
            self._handle, path.encode("utf-8"), byref(entries_ptr), byref(count), byref(err)
        )
        _ffi.check_error(code, err)
        entries = []
        if entries_ptr and count.value > 0:
            for i in range(count.value):
                e = entries_ptr[i]
                entries.append(DirEntry(
                    name=e.name.decode("utf-8") if e.name else "",
                    is_dir=e.is_dir, mode=e.mode,
                ))
            lib.cc_dir_entries_free(entries_ptr, count)
        return entries

    def chmod(self, path: str, mode: int) -> None:
        """Change file permissions."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            _ffi.get_ipc_backend().fs_chmod(path, mode)
            return
        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()
        code = lib.cc_fs_chmod(self._handle, path.encode("utf-8"), mode, byref(err))
        _ffi.check_error(code, err)

    def chown(self, path: str, uid: int, gid: int) -> None:
        """Change file owner."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            _ffi.get_ipc_backend().fs_chown(path, uid, gid)
            return
        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()
        code = lib.cc_fs_chown(self._handle, path.encode("utf-8"), uid, gid, byref(err))
        _ffi.check_error(code, err)

    def chtimes(self, path: str, atime_unix: int, mtime_unix: int) -> None:
        """Change file access and modification times."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            _ffi.get_ipc_backend().fs_chtimes(path, atime_unix, mtime_unix)
            return
        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()
        code = lib.cc_fs_chtimes(
            self._handle, path.encode("utf-8"), atime_unix, mtime_unix, byref(err)
        )
        _ffi.check_error(code, err)

    # ========== Command Execution ==========

    def command(self, name: str, *args: str) -> "Cmd":
        """Create a command to run in the instance.

        Args:
            name: Command name (e.g., "echo", "/bin/sh")
            *args: Command arguments

        Returns:
            A Cmd object that can be configured and executed.
        """
        from .cmd import Cmd

        return Cmd(self, name, list(args))

    def entrypoint_command(self, *args: str) -> "Cmd":
        """Create a command using the container's entrypoint.

        Args:
            *args: Optional override arguments (None for default CMD)

        Returns:
            A Cmd object that can be configured and executed.
        """
        from .cmd import Cmd

        return Cmd.entrypoint(self, list(args) if args else None)

    def exec(self, name: str, *args: str) -> None:
        """Replace the init process with the specified command."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            _ffi.get_ipc_backend().instance_exec(name, list(args))
            return

        lib = _ffi.get_lib()
        err = _ffi.CCErrorStruct()
        args_array = (c_char_p * (len(args) + 1))()
        for i, arg in enumerate(args):
            args_array[i] = arg.encode("utf-8")
        args_array[len(args)] = None
        code = lib.cc_instance_exec(
            self._handle, name.encode("utf-8"), args_array, byref(err)
        )
        _ffi.check_error(code, err)

    # ========== Networking ==========

    def listen(self, network: str, address: str) -> "Listener":
        """Listen for connections on the guest network."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            ipc_handle = _ffi.get_ipc_backend().net_listen(network, address)
            return Listener(ipc_handle, _ipc=True)

        lib = _ffi.get_lib()
        handle = _ffi.ListenerHandle()
        err = _ffi.CCErrorStruct()
        code = lib.cc_net_listen(
            self._handle,
            network.encode("utf-8"),
            address.encode("utf-8"),
            byref(handle),
            byref(err),
        )
        _ffi.check_error(code, err)
        return Listener(handle)

    # ========== Snapshots ==========

    def snapshot_filesystem(self, options: SnapshotOptions | None = None) -> Snapshot:
        """Take a filesystem snapshot."""
        if self._closed:
            raise CCError("Instance is closed", code=4)
        if self._ipc:
            excludes = options.excludes or [] if options else []
            cache_dir = options.cache_dir or "" if options else ""
            ipc_handle = _ffi.get_ipc_backend().fs_snapshot(excludes, cache_dir)
            return Snapshot(ipc_handle, _ipc=True)

        lib = _ffi.get_lib()
        handle = _ffi.SnapshotHandle()
        err = _ffi.CCErrorStruct()
        opts_ptr = None
        excludes_array = None
        if options:
            opts = _ffi.SnapshotOptionsStruct()
            if options.excludes:
                excludes_array = (c_char_p * (len(options.excludes) + 1))()
                for i, exc in enumerate(options.excludes):
                    excludes_array[i] = exc.encode("utf-8")
                excludes_array[len(options.excludes)] = None
                opts.excludes = excludes_array
                opts.exclude_count = len(options.excludes)
            if options.cache_dir:
                opts.cache_dir = options.cache_dir.encode("utf-8")
            opts_ptr = byref(opts)
        code = lib.cc_fs_snapshot(self._handle, opts_ptr, byref(handle), byref(err))
        _ffi.check_error(code, err)
        return Snapshot(handle)


class Listener:
    """A network listener for accepting connections."""

    def __init__(self, handle: Any, *, _ipc: bool = False) -> None:
        self._handle = handle
        self._closed = False
        self._ipc = _ipc

    def __del__(self) -> None:
        self.close()

    def __enter__(self) -> "Listener":
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()

    def close(self) -> None:
        """Close the listener."""
        if not self._closed and self._handle is not None:
            if self._ipc:
                _ffi.get_ipc_backend().listener_close(self._handle)
            else:
                lib = _ffi.get_lib()
                err = _ffi.CCErrorStruct()
                lib.cc_listener_close(self._handle, byref(err))
            self._closed = True

    @property
    def addr(self) -> str:
        """Get the listener address."""
        if self._closed:
            return ""
        if self._ipc:
            return _ffi.get_ipc_backend().listener_addr(self._handle)
        lib = _ffi.get_lib()
        ptr = lib.cc_listener_addr(self._handle)
        return _ffi._get_string_and_free(lib, ptr)

    def accept(self) -> "Conn":
        """Accept a connection from a client."""
        if self._closed:
            raise CCError("Listener is closed", code=4)
        if self._ipc:
            ipc_handle = _ffi.get_ipc_backend().listener_accept(self._handle)
            return Conn(ipc_handle, _ipc=True)

        lib = _ffi.get_lib()
        handle = _ffi.ConnHandle()
        err = _ffi.CCErrorStruct()
        code = lib.cc_listener_accept(self._handle, byref(handle), byref(err))
        _ffi.check_error(code, err)
        return Conn(handle)


class Conn:
    """A network connection."""

    def __init__(self, handle: Any, *, _ipc: bool = False) -> None:
        self._handle = handle
        self._closed = False
        self._ipc = _ipc

    def __del__(self) -> None:
        self.close()

    def __enter__(self) -> "Conn":
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()

    def close(self) -> None:
        """Close the connection."""
        if not self._closed and self._handle is not None:
            if self._ipc:
                _ffi.get_ipc_backend().conn_close(self._handle)
            else:
                lib = _ffi.get_lib()
                err = _ffi.CCErrorStruct()
                lib.cc_conn_close(self._handle, byref(err))
            self._closed = True

    @property
    def local_addr(self) -> str:
        """Get the local address."""
        if self._closed:
            return ""
        if self._ipc:
            return _ffi.get_ipc_backend().conn_local_addr(self._handle)
        lib = _ffi.get_lib()
        ptr = lib.cc_conn_local_addr(self._handle)
        return _ffi._get_string_and_free(lib, ptr)

    @property
    def remote_addr(self) -> str:
        """Get the remote address."""
        if self._closed:
            return ""
        if self._ipc:
            return _ffi.get_ipc_backend().conn_remote_addr(self._handle)
        lib = _ffi.get_lib()
        ptr = lib.cc_conn_remote_addr(self._handle)
        return _ffi._get_string_and_free(lib, ptr)

    def read(self, size: int) -> bytes:
        """Read up to size bytes from the connection."""
        if self._closed:
            raise CCError("Connection is closed", code=4)
        if self._ipc:
            return _ffi.get_ipc_backend().conn_read(self._handle, size)

        lib = _ffi.get_lib()
        buf = (c_uint8 * size)()
        n = c_size_t()
        err = _ffi.CCErrorStruct()
        code = lib.cc_conn_read(self._handle, buf, size, byref(n), byref(err))
        _ffi.check_error(code, err)
        return bytes(buf[: n.value])

    def write(self, data: bytes) -> int:
        """Write data to the connection."""
        if self._closed:
            raise CCError("Connection is closed", code=4)
        if self._ipc:
            return _ffi.get_ipc_backend().conn_write(self._handle, data)

        lib = _ffi.get_lib()
        buf = (c_uint8 * len(data)).from_buffer_copy(data)
        n = c_size_t()
        err = _ffi.CCErrorStruct()
        code = lib.cc_conn_write(self._handle, buf, len(data), byref(n), byref(err))
        _ffi.check_error(code, err)
        return n.value


# Module-level functions


def query_capabilities() -> Capabilities:
    """Query system capabilities."""
    if _ffi.using_ipc():
        # When using IPC, capabilities are determined locally.
        # The helper process runs on the same machine.
        import platform
        machine = platform.machine().lower()
        arch_map = {"x86_64": "amd64", "amd64": "amd64", "aarch64": "arm64", "arm64": "arm64"}
        arch = arch_map.get(machine, machine)
        return Capabilities(
            hypervisor_available=True,
            max_memory_mb=0,
            max_cpus=0,
            architecture=arch,
        )

    lib = _ffi.get_lib()
    caps = _ffi.CapabilitiesStruct()
    err = _ffi.CCErrorStruct()

    code = lib.cc_query_capabilities(byref(caps), byref(err))
    _ffi.check_error(code, err)

    arch = _ffi._get_string_and_free(lib, caps.architecture)

    return Capabilities(
        hypervisor_available=caps.hypervisor_available,
        max_memory_mb=caps.max_memory_mb,
        max_cpus=caps.max_cpus,
        architecture=arch,
    )
