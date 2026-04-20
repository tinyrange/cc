from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path
from typing import Any


@dataclass(frozen=True)
class CVMFSSource:
    mirror: str
    repo: str
    path: str
    cache_dir: str | None = None

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
    format: str | None = None
    mirror: str | None = None
    repo: str | None = None
    path: str | None = None

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
    cache_dir: str | None = None

    def to_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {"source": self.source.to_payload()}
        if self.cache_dir is not None:
            payload["cache_dir"] = self.cache_dir
        return payload

    @classmethod
    def from_cvmfs_container(
        cls,
        *,
        mirror: str,
        repo: str,
        path: str,
        cache_dir: str | None = None,
    ) -> "ImportImageRequest":
        return cls(
            source=ImageSource(
                type="cvmfs",
                mirror=mirror,
                repo=repo,
                path=path,
            ),
            cache_dir=cache_dir,
        )


@dataclass(frozen=True)
class ImageState:
    name: str
    status: str
    source: str | None = None
    source_kind: str | None = None
    error: str | None = None

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
class CVMFSDirectoryEntry:
    name: str
    path: str
    kind: str
    size: int | None = None

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
    length: int | None = None
    cache_dir: str | None = None

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
    eof: bool | None = None

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
    cache_dir: str | None = None

    @property
    def path(self) -> str:
        if self.source.path is None:
            raise ValueError("container source path is not set")
        return self.source.path


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
    workdir: str | None = None
    user: str | None = None
    stdin: bytes | None = None

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
    image: str | None = None
    memory_mb: int | None = None
    cpus: int | None = None
    started_at: str | None = None
    error: str | None = None

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
    cache_dir: str | None = None

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
