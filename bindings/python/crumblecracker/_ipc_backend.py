"""
IPC backend for the cc Python bindings.

Maps the high-level API to cc-helper IPC calls. Used on Windows
or when CC_USE_HELPER=1 is set, as an alternative to the ctypes/FFI backend.
"""

from __future__ import annotations

from ._ipc_client import IPCClient, spawn_helper
from ._ipc_protocol import (
    Decoder,
    Encoder,
    MSG_BUILD_DOCKERFILE,
    MSG_CMD_COMBINED_OUTPUT,
    MSG_CMD_ENTRYPOINT,
    MSG_CMD_EXIT_CODE,
    MSG_CMD_FREE,
    MSG_CMD_GET_ENV,
    MSG_CMD_ENVIRON,
    MSG_CMD_KILL,
    MSG_CMD_NEW,
    MSG_CMD_OUTPUT,
    MSG_CMD_RUN,
    MSG_CMD_SET_DIR,
    MSG_CMD_SET_ENV,
    MSG_CMD_START,
    MSG_CMD_STDERR_PIPE,
    MSG_CMD_STDIN_PIPE,
    MSG_CMD_STDOUT_PIPE,
    MSG_CMD_WAIT,
    MSG_CONN_CLOSE,
    MSG_CONN_LOCAL_ADDR,
    MSG_CONN_READ,
    MSG_CONN_REMOTE_ADDR,
    MSG_CONN_WRITE,
    MSG_FILE_CLOSE,
    MSG_FILE_NAME,
    MSG_FILE_READ,
    MSG_FILE_SEEK,
    MSG_FILE_STAT,
    MSG_FILE_SYNC,
    MSG_FILE_TRUNCATE,
    MSG_FILE_WRITE,
    MSG_FS_CHMOD,
    MSG_FS_CHOWN,
    MSG_FS_CHTIMES,
    MSG_FS_CREATE,
    MSG_FS_LSTAT,
    MSG_FS_MKDIR,
    MSG_FS_MKDIR_ALL,
    MSG_FS_OPEN,
    MSG_FS_OPEN_FILE,
    MSG_FS_READ_DIR,
    MSG_FS_READ_FILE,
    MSG_FS_READLINK,
    MSG_FS_REMOVE,
    MSG_FS_REMOVE_ALL,
    MSG_FS_RENAME,
    MSG_FS_SNAPSHOT,
    MSG_FS_STAT,
    MSG_FS_SYMLINK,
    MSG_FS_WRITE_FILE,
    MSG_INSTANCE_CLOSE,
    MSG_INSTANCE_EXEC,
    MSG_INSTANCE_ID,
    MSG_INSTANCE_IS_RUNNING,
    MSG_INSTANCE_NEW,
    MSG_INSTANCE_SET_CONSOLE,
    MSG_INSTANCE_SET_NETWORK,
    MSG_INSTANCE_WAIT,
    MSG_LISTENER_ACCEPT,
    MSG_LISTENER_ADDR,
    MSG_LISTENER_CLOSE,
    MSG_NET_LISTEN,
    MSG_SNAPSHOT_AS_SOURCE,
    MSG_SNAPSHOT_CACHE_KEY,
    MSG_SNAPSHOT_CLOSE,
    MSG_SNAPSHOT_PARENT,
)
from .errors import CCError
from .types import DirEntry, FileInfo


def _check_error(dec: Decoder) -> None:
    """Check the status byte in a response and raise if error."""
    err = dec.error()
    if err is not None:
        raise err


class IPCBackend:
    """Backend that communicates with cc-helper over IPC."""

    def __init__(self) -> None:
        self._client: IPCClient | None = None

    def _ensure_client(self) -> IPCClient:
        if self._client is None or self._client.is_closed:
            self._client = spawn_helper()
        return self._client

    @property
    def client(self) -> IPCClient:
        return self._ensure_client()

    def close(self) -> None:
        if self._client is not None:
            self._client.close()
            self._client = None

    # ======================================================================
    # Instance lifecycle
    # ======================================================================

    def instance_new(
        self,
        source_type: int,
        source_path: str,
        image_ref: str,
        cache_dir: str,
        memory_mb: int = 256,
        cpus: int = 1,
        timeout_seconds: float = 0,
        user: str = "",
        enable_dmesg: bool = False,
        mounts: list[tuple[str, str, bool]] | None = None,
    ) -> None:
        enc = Encoder()
        enc.uint8(source_type)
        enc.string(source_path)
        enc.string(image_ref)
        enc.string(cache_dir)
        enc.instance_options(
            memory_mb, cpus, timeout_seconds, user, enable_dmesg, mounts or []
        )
        resp = self.client.call(MSG_INSTANCE_NEW, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def instance_close(self) -> None:
        resp = self.client.call(MSG_INSTANCE_CLOSE)
        dec = Decoder(resp)
        _check_error(dec)

    def instance_wait(self) -> None:
        resp = self.client.call(MSG_INSTANCE_WAIT)
        dec = Decoder(resp)
        _check_error(dec)

    def instance_id(self) -> str:
        resp = self.client.call(MSG_INSTANCE_ID)
        dec = Decoder(resp)
        _check_error(dec)
        return dec.string()

    def instance_is_running(self) -> bool:
        resp = self.client.call(MSG_INSTANCE_IS_RUNNING)
        dec = Decoder(resp)
        _check_error(dec)
        return dec.decode_bool()

    def instance_set_console(self, cols: int, rows: int) -> None:
        enc = Encoder()
        enc.int32(cols)
        enc.int32(rows)
        resp = self.client.call(MSG_INSTANCE_SET_CONSOLE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def instance_set_network(self, enabled: bool) -> None:
        enc = Encoder()
        enc.encode_bool(enabled)
        resp = self.client.call(MSG_INSTANCE_SET_NETWORK, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def instance_exec(self, name: str, args: list[str]) -> None:
        enc = Encoder()
        enc.string(name)
        enc.string_slice(args)
        resp = self.client.call(MSG_INSTANCE_EXEC, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    # ======================================================================
    # Filesystem operations
    # ======================================================================

    def fs_open(self, path: str) -> int:
        enc = Encoder()
        enc.string(path)
        resp = self.client.call(MSG_FS_OPEN, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.uint64()

    def fs_create(self, path: str) -> int:
        enc = Encoder()
        enc.string(path)
        resp = self.client.call(MSG_FS_CREATE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.uint64()

    def fs_open_file(self, path: str, flags: int, mode: int) -> int:
        enc = Encoder()
        enc.string(path)
        enc.int32(flags)
        enc.uint32(mode)
        resp = self.client.call(MSG_FS_OPEN_FILE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.uint64()

    def fs_read_file(self, path: str) -> bytes:
        enc = Encoder()
        enc.string(path)
        resp = self.client.call(MSG_FS_READ_FILE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.read_bytes()

    def fs_write_file(self, path: str, data: bytes, mode: int) -> None:
        enc = Encoder()
        enc.string(path)
        enc.write_bytes(data)
        enc.uint32(mode)
        resp = self.client.call(MSG_FS_WRITE_FILE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def fs_stat(self, path: str) -> FileInfo:
        enc = Encoder()
        enc.string(path)
        resp = self.client.call(MSG_FS_STAT, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        fi = dec.file_info()
        return FileInfo(**fi)

    def fs_lstat(self, path: str) -> FileInfo:
        enc = Encoder()
        enc.string(path)
        resp = self.client.call(MSG_FS_LSTAT, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        fi = dec.file_info()
        return FileInfo(**fi)

    def fs_remove(self, path: str) -> None:
        enc = Encoder()
        enc.string(path)
        resp = self.client.call(MSG_FS_REMOVE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def fs_remove_all(self, path: str) -> None:
        enc = Encoder()
        enc.string(path)
        resp = self.client.call(MSG_FS_REMOVE_ALL, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def fs_mkdir(self, path: str, mode: int) -> None:
        enc = Encoder()
        enc.string(path)
        enc.uint32(mode)
        resp = self.client.call(MSG_FS_MKDIR, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def fs_mkdir_all(self, path: str, mode: int) -> None:
        enc = Encoder()
        enc.string(path)
        enc.uint32(mode)
        resp = self.client.call(MSG_FS_MKDIR_ALL, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def fs_rename(self, old_path: str, new_path: str) -> None:
        enc = Encoder()
        enc.string(old_path)
        enc.string(new_path)
        resp = self.client.call(MSG_FS_RENAME, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def fs_symlink(self, target: str, link_path: str) -> None:
        enc = Encoder()
        enc.string(target)
        enc.string(link_path)
        resp = self.client.call(MSG_FS_SYMLINK, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def fs_readlink(self, path: str) -> str:
        enc = Encoder()
        enc.string(path)
        resp = self.client.call(MSG_FS_READLINK, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.string()

    def fs_read_dir(self, path: str) -> list[DirEntry]:
        enc = Encoder()
        enc.string(path)
        resp = self.client.call(MSG_FS_READ_DIR, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        count = dec.uint32()
        entries = []
        for _ in range(count):
            d = dec.dir_entry()
            entries.append(DirEntry(**d))
        return entries

    def fs_chmod(self, path: str, mode: int) -> None:
        enc = Encoder()
        enc.string(path)
        enc.uint32(mode)
        resp = self.client.call(MSG_FS_CHMOD, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def fs_chown(self, path: str, uid: int, gid: int) -> None:
        enc = Encoder()
        enc.string(path)
        enc.int32(uid)
        enc.int32(gid)
        resp = self.client.call(MSG_FS_CHOWN, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def fs_chtimes(self, path: str, atime_unix: int, mtime_unix: int) -> None:
        enc = Encoder()
        enc.string(path)
        enc.int64(atime_unix)
        enc.int64(mtime_unix)
        resp = self.client.call(MSG_FS_CHTIMES, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def fs_snapshot(self, excludes: list[str], cache_dir: str) -> int:
        enc = Encoder()
        enc.snapshot_options(excludes, cache_dir)
        resp = self.client.call(MSG_FS_SNAPSHOT, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.uint64()

    # ======================================================================
    # File operations
    # ======================================================================

    def file_close(self, handle: int) -> None:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_FILE_CLOSE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def file_read(self, handle: int, length: int) -> bytes:
        enc = Encoder()
        enc.uint64(handle)
        enc.uint32(length)
        resp = self.client.call(MSG_FILE_READ, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.read_bytes()

    def file_write(self, handle: int, data: bytes) -> int:
        enc = Encoder()
        enc.uint64(handle)
        enc.write_bytes(data)
        resp = self.client.call(MSG_FILE_WRITE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.uint32()

    def file_seek(self, handle: int, offset: int, whence: int) -> int:
        enc = Encoder()
        enc.uint64(handle)
        enc.int64(offset)
        enc.int32(whence)
        resp = self.client.call(MSG_FILE_SEEK, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.int64()

    def file_sync(self, handle: int) -> None:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_FILE_SYNC, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def file_truncate(self, handle: int, size: int) -> None:
        enc = Encoder()
        enc.uint64(handle)
        enc.int64(size)
        resp = self.client.call(MSG_FILE_TRUNCATE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def file_stat(self, handle: int) -> FileInfo:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_FILE_STAT, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        fi = dec.file_info()
        return FileInfo(**fi)

    def file_name(self, handle: int) -> str:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_FILE_NAME, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.string()

    # ======================================================================
    # Command operations
    # ======================================================================

    def cmd_new(self, name: str, args: list[str]) -> int:
        enc = Encoder()
        enc.string(name)
        enc.string_slice(args)
        resp = self.client.call(MSG_CMD_NEW, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.uint64()

    def cmd_entrypoint(self, args: list[str]) -> int:
        enc = Encoder()
        enc.string_slice(args)
        resp = self.client.call(MSG_CMD_ENTRYPOINT, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.uint64()

    def cmd_free(self, handle: int) -> None:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_CMD_FREE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def cmd_set_dir(self, handle: int, dir: str) -> None:
        enc = Encoder()
        enc.uint64(handle)
        enc.string(dir)
        resp = self.client.call(MSG_CMD_SET_DIR, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def cmd_set_env(self, handle: int, key: str, value: str) -> None:
        enc = Encoder()
        enc.uint64(handle)
        enc.string(key)
        enc.string(value)
        resp = self.client.call(MSG_CMD_SET_ENV, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def cmd_get_env(self, handle: int, key: str) -> str:
        enc = Encoder()
        enc.uint64(handle)
        enc.string(key)
        resp = self.client.call(MSG_CMD_GET_ENV, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.string()

    def cmd_environ(self, handle: int) -> list[str]:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_CMD_ENVIRON, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.string_slice()

    def cmd_start(self, handle: int) -> None:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_CMD_START, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def cmd_wait(self, handle: int) -> int:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_CMD_WAIT, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.int32()

    def cmd_run(self, handle: int) -> int:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_CMD_RUN, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.int32()

    def cmd_output(self, handle: int) -> tuple[bytes, int]:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_CMD_OUTPUT, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        data = dec.read_bytes()
        exit_code = dec.int32()
        return data, exit_code

    def cmd_combined_output(self, handle: int) -> tuple[bytes, int]:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_CMD_COMBINED_OUTPUT, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        data = dec.read_bytes()
        exit_code = dec.int32()
        return data, exit_code

    def cmd_exit_code(self, handle: int) -> int:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_CMD_EXIT_CODE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.int32()

    def cmd_kill(self, handle: int) -> None:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_CMD_KILL, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def cmd_stdout_pipe(self, handle: int) -> int:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_CMD_STDOUT_PIPE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.uint64()

    def cmd_stderr_pipe(self, handle: int) -> int:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_CMD_STDERR_PIPE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.uint64()

    def cmd_stdin_pipe(self, handle: int) -> int:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_CMD_STDIN_PIPE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.uint64()

    # ======================================================================
    # Network operations
    # ======================================================================

    def net_listen(self, network: str, address: str) -> int:
        enc = Encoder()
        enc.string(network)
        enc.string(address)
        resp = self.client.call(MSG_NET_LISTEN, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.uint64()

    def listener_accept(self, handle: int) -> int:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_LISTENER_ACCEPT, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.uint64()

    def listener_close(self, handle: int) -> None:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_LISTENER_CLOSE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def listener_addr(self, handle: int) -> str:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_LISTENER_ADDR, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.string()

    def conn_read(self, handle: int, length: int) -> bytes:
        enc = Encoder()
        enc.uint64(handle)
        enc.uint32(length)
        resp = self.client.call(MSG_CONN_READ, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.read_bytes()

    def conn_write(self, handle: int, data: bytes) -> int:
        enc = Encoder()
        enc.uint64(handle)
        enc.write_bytes(data)
        resp = self.client.call(MSG_CONN_WRITE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.uint32()

    def conn_close(self, handle: int) -> None:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_CONN_CLOSE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def conn_local_addr(self, handle: int) -> str:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_CONN_LOCAL_ADDR, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.string()

    def conn_remote_addr(self, handle: int) -> str:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_CONN_REMOTE_ADDR, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.string()

    # ======================================================================
    # Snapshot operations
    # ======================================================================

    def snapshot_cache_key(self, handle: int) -> str:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_SNAPSHOT_CACHE_KEY, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.string()

    def snapshot_parent(self, handle: int) -> int:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_SNAPSHOT_PARENT, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.uint64()

    def snapshot_close(self, handle: int) -> None:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_SNAPSHOT_CLOSE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)

    def snapshot_as_source(self, handle: int) -> int:
        enc = Encoder()
        enc.uint64(handle)
        resp = self.client.call(MSG_SNAPSHOT_AS_SOURCE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.uint64()

    # ======================================================================
    # Dockerfile operations
    # ======================================================================

    def build_dockerfile(
        self,
        dockerfile: bytes,
        cache_dir: str,
        context_dir: str = "",
        build_args: dict[str, str] | None = None,
    ) -> int:
        enc = Encoder()
        enc.dockerfile_options(dockerfile, context_dir, cache_dir, build_args or {})
        resp = self.client.call(MSG_BUILD_DOCKERFILE, enc.get_bytes())
        dec = Decoder(resp)
        _check_error(dec)
        return dec.uint64()
