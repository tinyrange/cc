"""
Type definitions and enums for the cc Python bindings.
"""

from dataclasses import dataclass
from enum import IntEnum
from typing import Callable


class PullPolicy(IntEnum):
    """Pull policy for image fetching."""

    IF_NOT_PRESENT = 0
    ALWAYS = 1
    NEVER = 2


class SeekWhence(IntEnum):
    """Seek origin for file operations."""

    SET = 0  # Seek from beginning of file
    CUR = 1  # Seek from current position
    END = 2  # Seek from end of file


# File open flags (match POSIX)
O_RDONLY = 0x0000
O_WRONLY = 0x0001
O_RDWR = 0x0002
O_APPEND = 0x0008
O_CREATE = 0x0200
O_TRUNC = 0x0400
O_EXCL = 0x0800


@dataclass
class DownloadProgress:
    """Progress information for downloads."""

    current: int
    total: int  # -1 if unknown
    filename: str | None
    blob_index: int
    blob_count: int
    bytes_per_second: float
    eta_seconds: float  # -1 if unknown


# Type alias for progress callback
ProgressCallback = Callable[[DownloadProgress], None]


@dataclass
class PullOptions:
    """Options for pulling images."""

    platform_os: str | None = None
    platform_arch: str | None = None
    username: str | None = None
    password: str | None = None
    policy: PullPolicy = PullPolicy.IF_NOT_PRESENT


@dataclass
class MountConfig:
    """Mount configuration for virtio-fs."""

    tag: str
    host_path: str | None = None  # None for empty writable fs
    writable: bool = False


@dataclass
class InstanceOptions:
    """Options for creating an instance."""

    memory_mb: int = 256
    cpus: int = 1
    timeout_seconds: float = 0  # 0 for no timeout
    user: str | None = None
    enable_dmesg: bool = False
    mounts: list[MountConfig] | None = None


@dataclass
class FileInfo:
    """File information structure."""

    name: str
    size: int
    mode: int
    mod_time_unix: int
    is_dir: bool
    is_symlink: bool


@dataclass
class DirEntry:
    """Directory entry."""

    name: str
    is_dir: bool
    mode: int


@dataclass
class ImageConfig:
    """OCI image configuration."""

    architecture: str | None
    env: list[str]
    working_dir: str | None
    entrypoint: list[str]
    cmd: list[str]
    user: str | None


@dataclass
class Capabilities:
    """System capabilities."""

    hypervisor_available: bool
    max_memory_mb: int  # 0 if unknown
    max_cpus: int  # 0 if unknown
    architecture: str


@dataclass
class SnapshotOptions:
    """Options for filesystem snapshots."""

    excludes: list[str] | None = None
    cache_dir: str | None = None
