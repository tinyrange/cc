from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any, Optional


@dataclass(frozen=True)
class CVMFSSource:
    mirror: str
    repo: str
    path: str
    cache_dir: Optional[str] = None

    def to_payload(self) -> dict[str, str]:
        payload: dict[str, str] = {
            "mirror": self.mirror,
            "repo": self.repo,
            "path": self.path,
        }
        if self.cache_dir is not None:
            payload["cache_dir"] = self.cache_dir
        return payload


@dataclass(frozen=True)
class ImageSource:
    type: str
    format: Optional[str] = None
    mirror: Optional[str] = None
    repo: Optional[str] = None
    path: Optional[str] = None

    def to_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {"type": self.type}
        if self.format is not None:
            payload["format"] = self.format
        if self.mirror is not None:
            payload["mirror"] = self.mirror
        if self.repo is not None:
            payload["repo"] = self.repo
        if self.path is not None:
            payload["path"] = self.path
        return payload


@dataclass(frozen=True)
class ImportImageRequest:
    source: ImageSource
    cache_dir: Optional[str] = None
    prefetch: bool = False
    prefetch_workers: Optional[int] = None

    def to_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {"source": self.source.to_payload()}
        if self.cache_dir is not None:
            payload["cache_dir"] = self.cache_dir
        if self.prefetch:
            payload["prefetch"] = True
        if self.prefetch_workers is not None:
            payload["prefetch_workers"] = self.prefetch_workers
        return payload

    @classmethod
    def from_cvmfs_container(
        cls,
        *,
        mirror: str,
        repo: str,
        path: str,
        cache_dir: Optional[str] = None,
        prefetch: bool = False,
        prefetch_workers: Optional[int] = None,
    ) -> "ImportImageRequest":
        return cls(
            source=ImageSource(
                type="cvmfs",
                mirror=mirror,
                repo=repo,
                path=path,
            ),
            cache_dir=cache_dir,
            prefetch=prefetch,
            prefetch_workers=prefetch_workers,
        )


@dataclass(frozen=True)
class ImageState:
    name: str
    status: str
    source: Optional[str] = None
    source_kind: Optional[str] = None
    error: Optional[str] = None

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> "ImageState":
        return cls(
            name=payload["name"],
            status=payload["status"],
            source=payload.get("source"),
            source_kind=payload.get("source_kind"),
            error=payload.get("error"),
        )


@dataclass(frozen=True)
class KernelState:
    status: str
    error: Optional[str] = None
    version: Optional[str] = None
    source: Optional[str] = None

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> "KernelState":
        return cls(
            status=payload["status"],
            error=payload.get("error"),
            version=payload.get("version"),
            source=payload.get("source"),
        )


@dataclass(frozen=True)
class DownloadProgress:
    status: str
    artifact: Optional[str] = None
    blob: Optional[str] = None
    progress: Optional[float] = None
    bytes_downloaded: Optional[int] = None
    bytes_total: Optional[int] = None
    rate_bytes_per_second: Optional[float] = None
    eta_seconds: Optional[float] = None
    error: Optional[str] = None

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> "DownloadProgress":
        return cls(
            status=payload["status"],
            artifact=payload.get("artifact"),
            blob=payload.get("blob"),
            progress=payload.get("progress"),
            bytes_downloaded=payload.get("bytes_downloaded"),
            bytes_total=payload.get("bytes_total"),
            rate_bytes_per_second=payload.get("rate_bytes_per_second"),
            eta_seconds=payload.get("eta_seconds"),
            error=payload.get("error"),
        )


@dataclass(frozen=True)
class ImageMetadataState:
    name: str
    status: str
    source_kind: Optional[str] = None
    architecture: Optional[str] = None
    error: Optional[str] = None

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> "ImageMetadataState":
        return cls(
            name=payload["name"],
            status=payload["status"],
            source_kind=payload.get("source_kind"),
            architecture=payload.get("architecture"),
            error=payload.get("error"),
        )


@dataclass(frozen=True)
class EmulatorState:
    status: str
    path: Optional[str] = None
    required: bool = False
    error: Optional[str] = None

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> "EmulatorState":
        return cls(
            status=payload["status"],
            path=payload.get("path"),
            required=bool(payload.get("required", False)),
            error=payload.get("error"),
        )


@dataclass(frozen=True)
class CVMFSDirectoryEntry:
    name: str
    path: str
    kind: str
    size: Optional[int] = None

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> "CVMFSDirectoryEntry":
        return cls(
            name=payload["name"],
            path=payload["path"],
            kind=payload["kind"],
            size=payload.get("size"),
        )


@dataclass(frozen=True)
class CVMFSListResponse:
    entries: tuple[CVMFSDirectoryEntry, ...]

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> "CVMFSListResponse":
        return cls(
            entries=tuple(
                CVMFSDirectoryEntry.from_payload(entry)
                for entry in payload.get("entries", [])
            )
        )


@dataclass(frozen=True)
class CVMFSReadRequest:
    mirror: str
    repo: str
    path: str
    offset: int = 0
    length: Optional[int] = None
    cache_dir: Optional[str] = None

    def to_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "mirror": self.mirror,
            "repo": self.repo,
            "path": self.path,
            "offset": self.offset,
        }
        if self.length is not None:
            payload["length"] = self.length
        if self.cache_dir is not None:
            payload["cache_dir"] = self.cache_dir
        return payload


@dataclass(frozen=True)
class CVMFSReadResponse:
    path: str
    offset: int
    data: bytes
    eof: Optional[bool] = None

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> "CVMFSReadResponse":
        raw_data = payload.get("data", "")
        if isinstance(raw_data, str):
            data = raw_data.encode()
        else:
            data = bytes(raw_data)
        return cls(
            path=payload["path"],
            offset=payload.get("offset", 0),
            data=data,
            eof=payload.get("eof"),
        )


@dataclass(frozen=True)
class ContainerReference:
    name: str
    image: str
    source: ImageSource
    cache_dir: Optional[str] = None

    @property
    def path(self) -> str:
        if self.source.path is None:
            raise ValueError("container source path is not set")
        return self.source.path


@dataclass(frozen=True)
class DeployMetadata:
    commands: tuple[str, ...] = ()
    deploy_env: tuple[str, ...] = ()


@dataclass(frozen=True)
class ShareMount:
    source: str
    mount: str
    writable: bool = False

    def to_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "source": self.source,
            "mount": self.mount,
        }
        if self.writable:
            payload["writable"] = True
        return payload


@dataclass(frozen=True)
class RunCommandRequest:
    image: str
    command: tuple[str, ...]
    shares: tuple[ShareMount, ...] = ()
    env: tuple[str, ...] = ()
    workdir: Optional[str] = None
    user: Optional[str] = None
    stdin: Optional[bytes] = None
    memory_mb: Optional[int] = None
    cpus: Optional[int] = None
    dmesg: bool = False

    def to_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "image": self.image,
            "command": list(self.command),
        }
        if self.shares:
            payload["shares"] = [share.to_payload() for share in self.shares]
        if self.env:
            payload["env"] = list(self.env)
        if self.workdir is not None:
            payload["workdir"] = self.workdir
        if self.user is not None:
            payload["user"] = self.user
        if self.stdin is not None:
            payload["stdin"] = self.stdin.decode("utf-8", errors="surrogateescape")
        if self.memory_mb is not None:
            payload["memory_mb"] = self.memory_mb
        if self.cpus is not None:
            payload["cpus"] = self.cpus
        if self.dmesg:
            payload["dmesg"] = True
        return payload


@dataclass(frozen=True)
class CommandResult:
    exit_code: int
    output: str

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> "CommandResult":
        return cls(
            exit_code=payload.get("exit_code", 0),
            output=payload.get("output", ""),
        )


@dataclass(frozen=True)
class VMState:
    status: str
    image: Optional[str] = None
    memory_mb: Optional[int] = None
    cpus: Optional[int] = None
    started_at: Optional[str] = None
    error: Optional[str] = None

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> "VMState":
        return cls(
            status=payload["status"],
            image=payload.get("image"),
            memory_mb=payload.get("memory_mb"),
            cpus=payload.get("cpus"),
            started_at=payload.get("started_at"),
            error=payload.get("error"),
        )


@dataclass(frozen=True)
class DaemonState:
    addr: str
    cache_dir: Optional[str] = None

    @property
    def base_url(self) -> str:
        return f"http://{self.addr}"

    @classmethod
    def from_file(cls, path: Path) -> "DaemonState":
        payload = path.read_text()
        import json

        data = json.loads(payload)
        addr = data.get("addr", "").strip()
        if not addr:
            raise ValueError(f"daemon state at {path} does not contain an address")
        return cls(addr=addr, cache_dir=str(path.parent))
