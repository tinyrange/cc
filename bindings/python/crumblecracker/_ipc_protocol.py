"""
Binary protocol encoder/decoder for the cc-helper IPC protocol.

Wire format: [2 bytes: msg_type (big endian)][4 bytes: payload_len (big endian)][payload]

All multi-byte integers are big-endian. Strings and byte slices are length-prefixed
with a 4-byte big-endian length.
"""

from __future__ import annotations

import struct
from typing import TYPE_CHECKING, Any

from .errors import error_from_code

if TYPE_CHECKING:
    from .errors import CCError

# Header size: 2 bytes msg_type + 4 bytes payload_len
HEADER_SIZE = 6

# =========================================================================
# Message type constants (must match internal/ipc/protocol.go)
# =========================================================================

# Instance lifecycle (0x01xx)
MSG_INSTANCE_NEW = 0x0100
MSG_INSTANCE_CLOSE = 0x0101
MSG_INSTANCE_WAIT = 0x0102
MSG_INSTANCE_ID = 0x0103
MSG_INSTANCE_IS_RUNNING = 0x0104
MSG_INSTANCE_SET_CONSOLE = 0x0105
MSG_INSTANCE_SET_NETWORK = 0x0106
MSG_INSTANCE_EXEC = 0x0107

# Filesystem operations (0x02xx)
MSG_FS_OPEN = 0x0200
MSG_FS_CREATE = 0x0201
MSG_FS_OPEN_FILE = 0x0202
MSG_FS_READ_FILE = 0x0203
MSG_FS_WRITE_FILE = 0x0204
MSG_FS_STAT = 0x0205
MSG_FS_LSTAT = 0x0206
MSG_FS_REMOVE = 0x0207
MSG_FS_REMOVE_ALL = 0x0208
MSG_FS_MKDIR = 0x0209
MSG_FS_MKDIR_ALL = 0x020A
MSG_FS_RENAME = 0x020B
MSG_FS_SYMLINK = 0x020C
MSG_FS_READLINK = 0x020D
MSG_FS_READ_DIR = 0x020E
MSG_FS_CHMOD = 0x020F
MSG_FS_CHOWN = 0x0210
MSG_FS_CHTIMES = 0x0211
MSG_FS_SNAPSHOT = 0x0212

# File operations (0x03xx)
MSG_FILE_CLOSE = 0x0300
MSG_FILE_READ = 0x0301
MSG_FILE_WRITE = 0x0302
MSG_FILE_SEEK = 0x0303
MSG_FILE_SYNC = 0x0304
MSG_FILE_TRUNCATE = 0x0305
MSG_FILE_STAT = 0x0306
MSG_FILE_NAME = 0x0307

# Command operations (0x04xx)
MSG_CMD_NEW = 0x0400
MSG_CMD_ENTRYPOINT = 0x0401
MSG_CMD_FREE = 0x0402
MSG_CMD_SET_DIR = 0x0403
MSG_CMD_SET_ENV = 0x0404
MSG_CMD_GET_ENV = 0x0405
MSG_CMD_ENVIRON = 0x0406
MSG_CMD_START = 0x0407
MSG_CMD_WAIT = 0x0408
MSG_CMD_RUN = 0x0409
MSG_CMD_OUTPUT = 0x040A
MSG_CMD_COMBINED_OUTPUT = 0x040B
MSG_CMD_EXIT_CODE = 0x040C
MSG_CMD_KILL = 0x040D
MSG_CMD_STDOUT_PIPE = 0x040E
MSG_CMD_STDERR_PIPE = 0x040F
MSG_CMD_STDIN_PIPE = 0x0410
MSG_CMD_RUN_STREAMING = 0x0411

# Network operations (0x05xx)
MSG_NET_LISTEN = 0x0500
MSG_LISTENER_ACCEPT = 0x0501
MSG_LISTENER_CLOSE = 0x0502
MSG_LISTENER_ADDR = 0x0503
MSG_CONN_READ = 0x0504
MSG_CONN_WRITE = 0x0505
MSG_CONN_CLOSE = 0x0506
MSG_CONN_LOCAL_ADDR = 0x0507
MSG_CONN_REMOTE_ADDR = 0x0508

# Snapshot operations (0x06xx)
MSG_SNAPSHOT_CACHE_KEY = 0x0600
MSG_SNAPSHOT_PARENT = 0x0601
MSG_SNAPSHOT_CLOSE = 0x0602
MSG_SNAPSHOT_AS_SOURCE = 0x0603

# Dockerfile operations (0x07xx)
MSG_BUILD_DOCKERFILE = 0x0700

# Response types (0xFFxx)
MSG_RESPONSE = 0xFF00
MSG_ERROR = 0xFF01
MSG_STREAM_CHUNK = 0xFF02
MSG_STREAM_END = 0xFF03

# Error codes
ERR_CODE_OK = 0


# =========================================================================
# Encoder
# =========================================================================


class Encoder:
    """Binary encoder for IPC messages."""

    __slots__ = ("_parts",)

    def __init__(self) -> None:
        self._parts: list[bytes] = []

    def get_bytes(self) -> bytes:
        return b"".join(self._parts)

    def uint8(self, v: int) -> "Encoder":
        self._parts.append(struct.pack("!B", v))
        return self

    def uint16(self, v: int) -> "Encoder":
        self._parts.append(struct.pack("!H", v))
        return self

    def uint32(self, v: int) -> "Encoder":
        self._parts.append(struct.pack("!I", v))
        return self

    def uint64(self, v: int) -> "Encoder":
        self._parts.append(struct.pack("!Q", v))
        return self

    def int32(self, v: int) -> "Encoder":
        self._parts.append(struct.pack("!i", v))
        return self

    def int64(self, v: int) -> "Encoder":
        self._parts.append(struct.pack("!q", v))
        return self

    def encode_bool(self, v: bool) -> "Encoder":
        self._parts.append(b"\x01" if v else b"\x00")
        return self

    def string(self, s: str) -> "Encoder":
        data = s.encode("utf-8")
        self._parts.append(struct.pack("!I", len(data)))
        self._parts.append(data)
        return self

    def write_bytes(self, b: bytes) -> "Encoder":
        self._parts.append(struct.pack("!I", len(b)))
        self._parts.append(b)
        return self

    def string_slice(self, ss: list[str]) -> "Encoder":
        self.uint32(len(ss))
        for s in ss:
            self.string(s)
        return self

    def mount_config(self, tag: str, host_path: str, writable: bool) -> "Encoder":
        self.string(tag)
        self.string(host_path)
        self.encode_bool(writable)
        return self

    def instance_options(
        self,
        memory_mb: int,
        cpus: int,
        timeout_seconds: float,
        user: str,
        enable_dmesg: bool,
        mounts: list[tuple[str, str, bool]],
    ) -> "Encoder":
        self.uint64(memory_mb)
        self.int32(cpus)
        # Convert timeout to nanoseconds
        timeout_nanos = int(timeout_seconds * 1e9)
        self.uint64(timeout_nanos)
        self.string(user)
        self.encode_bool(enable_dmesg)
        self.uint32(len(mounts))
        for tag, host_path, writable in mounts:
            self.mount_config(tag, host_path, writable)
        return self

    def snapshot_options(self, excludes: list[str], cache_dir: str) -> "Encoder":
        self.string_slice(excludes)
        self.string(cache_dir)
        return self

    def dockerfile_options(
        self,
        dockerfile: bytes,
        context_dir: str,
        cache_dir: str,
        build_args: dict[str, str],
    ) -> "Encoder":
        self.write_bytes(dockerfile)
        self.string(context_dir)
        self.string(cache_dir)
        self.uint32(len(build_args))
        for k, v in build_args.items():
            self.string(k)
            self.string(v)
        return self


# =========================================================================
# Decoder
# =========================================================================


class Decoder:
    """Binary decoder for IPC messages."""

    __slots__ = ("_buf", "_pos")

    def __init__(self, buf: bytes) -> None:
        self._buf = buf
        self._pos = 0

    @property
    def remaining(self) -> int:
        return len(self._buf) - self._pos

    def _check(self, n: int) -> None:
        if self._pos + n > len(self._buf):
            raise ValueError("Unexpected end of buffer")

    def uint8(self) -> int:
        self._check(1)
        (v,) = struct.unpack_from("!B", self._buf, self._pos)
        self._pos += 1
        return int(v)

    def uint16(self) -> int:
        self._check(2)
        (v,) = struct.unpack_from("!H", self._buf, self._pos)
        self._pos += 2
        return int(v)

    def uint32(self) -> int:
        self._check(4)
        (v,) = struct.unpack_from("!I", self._buf, self._pos)
        self._pos += 4
        return int(v)

    def uint64(self) -> int:
        self._check(8)
        (v,) = struct.unpack_from("!Q", self._buf, self._pos)
        self._pos += 8
        return int(v)

    def int32(self) -> int:
        self._check(4)
        (v,) = struct.unpack_from("!i", self._buf, self._pos)
        self._pos += 4
        return int(v)

    def int64(self) -> int:
        self._check(8)
        (v,) = struct.unpack_from("!q", self._buf, self._pos)
        self._pos += 8
        return int(v)

    def decode_bool(self) -> bool:
        return self.uint8() != 0

    def string(self) -> str:
        length = self.uint32()
        self._check(length)
        s = self._buf[self._pos : self._pos + length].decode("utf-8")
        self._pos += length
        return s

    def read_bytes(self) -> bytes:
        length = self.uint32()
        self._check(length)
        b = self._buf[self._pos : self._pos + length]
        self._pos += length
        return b

    def string_slice(self) -> list[str]:
        count = self.uint32()
        return [self.string() for _ in range(count)]

    def file_info(self) -> dict[str, Any]:
        return {
            "name": self.string(),
            "size": self.int64(),
            "mode": self.uint32(),
            "mod_time_unix": self.int64(),
            "is_dir": self.decode_bool(),
            "is_symlink": self.decode_bool(),
        }

    def dir_entry(self) -> dict[str, Any]:
        return {
            "name": self.string(),
            "is_dir": self.decode_bool(),
            "mode": self.uint32(),
        }

    def error(self) -> "CCError | None":
        """Decode an error response. Returns None if no error (code = 0)."""
        code = self.uint8()
        if code == ERR_CODE_OK:
            return None
        message = self.string()
        op = self.string() or None
        path = self.string() or None
        return error_from_code(code, message, op, path)


# =========================================================================
# Header helpers
# =========================================================================


def encode_header(msg_type: int, payload_len: int) -> bytes:
    """Encode a message header."""
    return struct.pack("!HI", msg_type, payload_len)


def decode_header(data: bytes) -> tuple[int, int]:
    """Decode a message header. Returns (msg_type, payload_len)."""
    msg_type, payload_len = struct.unpack("!HI", data[:HEADER_SIZE])
    return msg_type, payload_len
