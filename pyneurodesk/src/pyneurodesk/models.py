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
    mirrors: tuple[str, ...] = ()

    def to_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "mirror": self.mirror,
            "repo": self.repo,
            "path": self.path,
        }
        if self.cache_dir is not None:
            payload["cache_dir"] = self.cache_dir
        if self.mirrors:
            payload["mirrors"] = list(self.mirrors)
        return payload


@dataclass(frozen=True)
class ImageSource:
    type: str
    format: Optional[str] = None
    mirror: Optional[str] = None
    mirrors: tuple[str, ...] = ()
    repo: Optional[str] = None
    path: Optional[str] = None

    def to_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {"type": self.type}
        if self.format is not None:
            payload["format"] = self.format
        if self.mirror is not None:
            payload["mirror"] = self.mirror
        if self.mirrors:
            payload["mirrors"] = list(self.mirrors)
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
        mirrors: tuple[str, ...] = (),
        prefetch: bool = False,
        prefetch_workers: Optional[int] = None,
    ) -> "ImportImageRequest":
        return cls(
            source=ImageSource(
                type="cvmfs",
                mirror=mirror,
                mirrors=mirrors,
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
    files_downloaded: Optional[int] = None
    files_total: Optional[int] = None
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
            files_downloaded=payload.get("files_downloaded"),
            files_total=payload.get("files_total"),
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
    env: tuple[str, ...] = ()
    error: Optional[str] = None

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> "ImageMetadataState":
        return cls(
            name=payload["name"],
            status=payload["status"],
            source_kind=payload.get("source_kind"),
            architecture=payload.get("architecture"),
            env=tuple(str(item) for item in payload.get("env", ()) if isinstance(item, str)),
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
    mirrors: tuple[str, ...] = ()

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
        if self.mirrors:
            payload["mirrors"] = list(self.mirrors)
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
class PortForward:
    host_port: int
    guest_port: int
    protocol: str = "tcp"
    host_addr: Optional[str] = None
    guest_addr: Optional[str] = None

    def to_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "protocol": self.protocol,
            "guest_port": self.guest_port,
        }
        if self.host_port is not None:
            payload["host_port"] = self.host_port
        if self.host_addr:
            payload["host_addr"] = self.host_addr
        if self.guest_addr:
            payload["guest_addr"] = self.guest_addr
        return payload


@dataclass(frozen=True)
class NetworkConfig:
    enabled: bool = False
    allow_internet: bool = False
    host_dns_name: Optional[str] = None
    port_forwards: tuple[PortForward, ...] = ()

    def to_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {}
        if self.enabled or self.port_forwards:
            payload["enabled"] = True
        if self.allow_internet:
            payload["allow_internet"] = True
        if self.host_dns_name:
            payload["host_dns_name"] = self.host_dns_name
        if self.port_forwards:
            payload["port_forwards"] = [forward.to_payload() for forward in self.port_forwards]
        return payload


@dataclass(frozen=True)
class RunCommandRequest:
    image: str
    command: tuple[str, ...]
    vm_id: Optional[str] = None
    shares: tuple[ShareMount, ...] = ()
    network: Optional[NetworkConfig] = None
    env: tuple[str, ...] = ()
    workdir: Optional[str] = None
    user: Optional[str] = None
    stdin: Optional[bytes] = None
    memory_mb: Optional[int] = None
    cpus: Optional[int] = None
    dmesg: bool = False
    timeout_seconds: Optional[float] = None

    def to_payload(self) -> dict[str, Any]:
        payload: dict[str, Any] = {
            "image": self.image,
            "command": list(self.command),
        }
        if self.vm_id:
            payload["id"] = self.vm_id
        if self.shares:
            payload["shares"] = [share.to_payload() for share in self.shares]
        if self.network is not None:
            network_payload = self.network.to_payload()
            if network_payload:
                payload["network"] = network_payload
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
        if self.timeout_seconds is not None and self.timeout_seconds > 0:
            payload["timeout_seconds"] = self.timeout_seconds
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
    id: Optional[str] = None
    image: Optional[str] = None
    memory_mb: Optional[int] = None
    cpus: Optional[int] = None
    started_at: Optional[str] = None
    network_ipv4: Optional[str] = None
    error: Optional[str] = None

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> "VMState":
        return cls(
            status=payload["status"],
            id=payload.get("id"),
            image=payload.get("image"),
            memory_mb=payload.get("memory_mb"),
            cpus=payload.get("cpus"),
            started_at=payload.get("started_at"),
            network_ipv4=payload.get("network_ipv4"),
            error=payload.get("error"),
        )


@dataclass(frozen=True)
class VMSupportedState:
    supported: bool
    error: Optional[str] = None

    @classmethod
    def from_payload(cls, payload: dict[str, Any]) -> "VMSupportedState":
        return cls(
            supported=bool(payload.get("supported", False)),
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
