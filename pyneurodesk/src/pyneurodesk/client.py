from __future__ import annotations

import os
import json
from collections.abc import Iterable
from typing import Any, Optional, Union

import httpx

from .models import (
    CommandResult,
    CVMFSListResponse,
    CVMFSReadRequest,
    CVMFSReadResponse,
    CVMFSSource,
    ContainerReference,
    DownloadProgress,
    EmulatorState,
    ImageMetadataState,
    ImageState,
    ImportImageRequest,
    KernelState,
    RunCommandRequest,
    ShareMount,
    VMState,
)

DEFAULT_BOOT_TIMEOUT_SECONDS = 5.0


class PyNeurodeskClient:
    def __init__(
        self,
        base_url: str,
        *,
        client: Optional[httpx.Client] = None,
        timeout: Optional[Union[float, httpx.Timeout]] = None,
    ) -> None:
        self._owns_client = client is None
        self._client = client or httpx.Client(base_url=base_url, timeout=resolve_http_timeout(timeout))

    def close(self) -> None:
        if self._owns_client:
            self._client.close()

    def __enter__(self) -> "PyNeurodeskClient":
        return self

    def __exit__(self, exc_type: object, exc: object, tb: object) -> None:
        self.close()

    def import_image(self, name: str, request: ImportImageRequest) -> ImageState:
        response = self._client.post(f"/image/{name}", json=request.to_payload())
        payload = self._decode_json(response)
        return ImageState.from_payload(payload)

    def import_image_stream(
        self,
        name: str,
        request: ImportImageRequest,
        *,
        timeout: Optional[Union[float, httpx.Timeout]] = None,
    ) -> Iterable[DownloadProgress]:
        stream_timeout = timeout if timeout is not None else httpx.Timeout(connect=10.0, read=None, write=300.0, pool=10.0)
        with self._client.stream(
            "POST",
            f"/image/{name}",
            params={"stream": "1"},
            json=request.to_payload(),
            headers={"Accept": "application/x-ndjson"},
            timeout=stream_timeout,
        ) as response:
            response.raise_for_status()
            for line in response.iter_lines():
                if not line:
                    continue
                event = json.loads(line)
                if not isinstance(event, dict):
                    raise TypeError(f"expected image import progress object, got {type(event)!r}")
                yield DownloadProgress.from_payload(event)

    def import_cvmfs_container(
        self,
        name: str,
        *,
        mirror: str,
        repo: str,
        path: str,
        cache_dir: Optional[str] = None,
        prefetch: bool = False,
        prefetch_workers: Optional[int] = None,
    ) -> ImageState:
        return self.import_image(
            name,
            ImportImageRequest.from_cvmfs_container(
                mirror=mirror,
                repo=repo,
                path=path,
                cache_dir=cache_dir,
                prefetch=prefetch,
                prefetch_workers=prefetch_workers,
            ),
        )

    def import_cvmfs_container_stream(
        self,
        name: str,
        *,
        mirror: str,
        repo: str,
        path: str,
        cache_dir: Optional[str] = None,
        prefetch: bool = False,
        prefetch_workers: Optional[int] = None,
        timeout: Optional[Union[float, httpx.Timeout]] = None,
    ) -> Iterable[DownloadProgress]:
        return self.import_image_stream(
            name,
            ImportImageRequest.from_cvmfs_container(
                mirror=mirror,
                repo=repo,
                path=path,
                cache_dir=cache_dir,
                prefetch=prefetch,
                prefetch_workers=prefetch_workers,
            ),
            timeout=timeout,
        )

    def kernel_status(self) -> KernelState:
        response = self._client.get("/kernel")
        payload = self._decode_json(response)
        return KernelState.from_payload(payload)

    def download_kernel(self) -> KernelState:
        response = self._client.post("/kernel/download", json={})
        payload = self._decode_json(response)
        return KernelState.from_payload(payload)

    def download_kernel_stream(self) -> Iterable[DownloadProgress]:
        with self._client.stream(
            "POST",
            "/kernel/download",
            params={"stream": "1"},
            json={},
            headers={"Accept": "application/x-ndjson"},
        ) as response:
            response.raise_for_status()
            for line in response.iter_lines():
                if not line:
                    continue
                event = json.loads(line)
                if not isinstance(event, dict):
                    raise TypeError(f"expected download progress object, got {type(event)!r}")
                yield DownloadProgress.from_payload(event)

    def get_image(self, name: str) -> Optional[ImageState]:
        response = self._client.get(f"/image/{name}")
        if response.status_code == 404:
            return None
        payload = self._decode_json(response)
        return ImageState.from_payload(payload)

    def prepare_image_metadata(self, name: str) -> ImageMetadataState:
        response = self._client.post(f"/image/{name}/metadata", json={})
        payload = self._decode_json(response)
        return ImageMetadataState.from_payload(payload)

    def prepare_image_emulator(self, name: str) -> EmulatorState:
        response = self._client.post(f"/image/{name}/qemu/download", json={})
        payload = self._decode_json(response)
        return EmulatorState.from_payload(payload)

    def prepare_image_emulator_stream(self, name: str) -> Iterable[DownloadProgress]:
        with self._client.stream(
            "POST",
            f"/image/{name}/qemu/download",
            params={"stream": "1"},
            json={},
            headers={"Accept": "application/x-ndjson"},
        ) as response:
            response.raise_for_status()
            for line in response.iter_lines():
                if not line:
                    continue
                event = json.loads(line)
                if not isinstance(event, dict):
                    raise TypeError(f"expected download progress object, got {type(event)!r}")
                yield DownloadProgress.from_payload(event)

    def ensure_image(self, reference: ContainerReference) -> ImageState:
        existing = self.get_image(reference.image)
        if existing is not None:
            return existing
        return self.import_image(
            reference.image,
            ImportImageRequest(source=reference.source, cache_dir=reference.cache_dir),
        )

    def instance_status(self) -> VMState:
        response = self._client.get("/vm/status")
        payload = self._decode_json(response)
        return VMState.from_payload(payload)

    def create_instance(
        self,
        image: str,
        *,
        timeout: Optional[Union[float, httpx.Timeout]] = None,
        dmesg: bool = False,
        memory_mb: Optional[int] = None,
        cpus: Optional[int] = None,
    ) -> VMState:
        payload: dict[str, Any] = {"image": image}
        if dmesg:
            payload["dmesg"] = True
        if memory_mb is not None:
            payload["memory_mb"] = memory_mb
        if cpus is not None:
            payload["cpus"] = cpus
        timeout_seconds = resolve_boot_timeout_seconds(timeout)
        if timeout_seconds is not None:
            payload["timeout_seconds"] = timeout_seconds
        response = self._client.post("/vm", json=payload, timeout=resolve_boot_timeout(timeout))
        payload = self._decode_json(response)
        return VMState.from_payload(payload)

    def start_instance(
        self,
        *,
        timeout: Optional[Union[float, httpx.Timeout]] = None,
        dmesg: bool = False,
        memory_mb: Optional[int] = None,
        cpus: Optional[int] = None,
    ) -> VMState:
        payload: dict[str, Any] = {}
        if dmesg:
            payload["dmesg"] = True
        if memory_mb is not None:
            payload["memory_mb"] = memory_mb
        if cpus is not None:
            payload["cpus"] = cpus
        timeout_seconds = resolve_boot_timeout_seconds(timeout)
        if timeout_seconds is not None:
            payload["timeout_seconds"] = timeout_seconds
        response = self._client.post("/vm/start", json=payload, timeout=resolve_boot_timeout(timeout))
        payload = self._decode_json(response)
        return VMState.from_payload(payload)

    def start_instance_stream(
        self,
        *,
        timeout: Optional[Union[float, httpx.Timeout]] = None,
        dmesg: bool = False,
        memory_mb: Optional[int] = None,
        cpus: Optional[int] = None,
    ) -> Iterable[dict[str, Any]]:
        payload: dict[str, Any] = {}
        if dmesg:
            payload["dmesg"] = True
        if memory_mb is not None:
            payload["memory_mb"] = memory_mb
        if cpus is not None:
            payload["cpus"] = cpus
        timeout_seconds = resolve_boot_timeout_seconds(timeout)
        if timeout_seconds is not None:
            payload["timeout_seconds"] = timeout_seconds
        with self._client.stream(
            "POST",
            "/vm/start",
            params={"stream": "1"},
            json=payload,
            headers={"Accept": "application/x-ndjson"},
            timeout=resolve_boot_timeout(timeout),
        ) as response:
            response.raise_for_status()
            for line in response.iter_lines():
                if not line:
                    continue
                event = json.loads(line)
                if not isinstance(event, dict):
                    raise TypeError(f"expected boot event object, got {type(event)!r}")
                yield event

    def create_instance_stream(
        self,
        image: str,
        *,
        timeout: Optional[Union[float, httpx.Timeout]] = None,
        dmesg: bool = False,
    ) -> Iterable[dict[str, Any]]:
        payload: dict[str, Any] = {"image": image}
        if dmesg:
            payload["dmesg"] = True
        timeout_seconds = resolve_boot_timeout_seconds(timeout)
        if timeout_seconds is not None:
            payload["timeout_seconds"] = timeout_seconds
        with self._client.stream(
            "POST",
            "/vm",
            params={"stream": "1"},
            json=payload,
            headers={"Accept": "application/x-ndjson"},
            timeout=resolve_boot_timeout(timeout),
        ) as response:
            response.raise_for_status()
            for line in response.iter_lines():
                if not line:
                    continue
                event = json.loads(line)
                if not isinstance(event, dict):
                    raise TypeError(f"expected boot event object, got {type(event)!r}")
                yield event

    def shutdown_instance(self) -> VMState:
        response = self._client.post("/vm/shutdown")
        payload = self._decode_json(response)
        return VMState.from_payload(payload)

    def create_watchdog(self, *, timeout_seconds: float = 30.0) -> dict[str, Any]:
        response = self._client.post("/watchdog", json={"timeout_seconds": timeout_seconds})
        return self._decode_json(response)

    def feed_watchdog(self) -> dict[str, Any]:
        response = self._client.post("/watchdog/feed")
        return self._decode_json(response)

    def ensure_instance(
        self,
        image: str,
        *,
        timeout: Optional[Union[float, httpx.Timeout]] = None,
        dmesg: bool = False,
        memory_mb: Optional[int] = None,
        cpus: Optional[int] = None,
    ) -> VMState:
        state = self.instance_status()
        if state.status == "running" and state.image == image:
            return state
        if state.status == "running" and state.image not in (image,):
            self.shutdown_instance()
            return self.create_instance(image, timeout=timeout, dmesg=dmesg, memory_mb=memory_mb, cpus=cpus)
        if state.status == "stopped":
            return self.create_instance(image, timeout=timeout, dmesg=dmesg, memory_mb=memory_mb, cpus=cpus)
        if state.status == "running":
            return self.create_instance(image, timeout=timeout, dmesg=dmesg, memory_mb=memory_mb, cpus=cpus)
        return self.create_instance(image, timeout=timeout, dmesg=dmesg, memory_mb=memory_mb, cpus=cpus)

    def cvmfs_list(self, source: CVMFSSource) -> CVMFSListResponse:
        response = self._client.post("/cvmfs/list", json=source.to_payload())
        payload = self._decode_json(response)
        return CVMFSListResponse.from_payload(payload)

    def cvmfs_read(self, request: CVMFSReadRequest) -> CVMFSReadResponse:
        response = self._client.post("/cvmfs/read", json=request.to_payload())
        payload = self._decode_json(response)
        return CVMFSReadResponse.from_payload(payload)

    def run_command(
        self,
        request: RunCommandRequest,
        *,
        timeout: Optional[Union[float, httpx.Timeout]] = None,
    ) -> CommandResult:
        response = self._client.post("/vm/run", json=request.to_payload(), timeout=timeout)
        payload = self._decode_json(response)
        return CommandResult.from_payload(payload)

    def run(
        self,
        image: str,
        command: Iterable[str],
        *,
        shares: Iterable[ShareMount] = (),
        env: Iterable[str] = (),
        workdir: Optional[str] = None,
        user: Optional[str] = None,
        stdin: Optional[bytes] = None,
        timeout: Optional[Union[float, httpx.Timeout]] = None,
    ) -> CommandResult:
        return self.run_command(
            RunCommandRequest(
                image=image,
                command=tuple(command),
                shares=tuple(shares),
                env=tuple(env),
                workdir=workdir,
                user=user,
                stdin=stdin,
            ),
            timeout=timeout,
        )

    def run_stream(
        self,
        image: str,
        command: Iterable[str],
        *,
        shares: Iterable[ShareMount] = (),
        env: Iterable[str] = (),
        workdir: Optional[str] = None,
        user: Optional[str] = None,
        stdin: Optional[bytes] = None,
        timeout: Optional[Union[float, httpx.Timeout]] = None,
    ) -> Iterable[dict[str, Any]]:
        request = RunCommandRequest(
            image=image,
            command=tuple(command),
            shares=tuple(shares),
            env=tuple(env),
            workdir=workdir,
            user=user,
            stdin=stdin,
        )
        with self._client.stream(
            "POST",
            "/vm/run",
            params={"stream": "1"},
            json=request.to_payload(),
            headers={"Accept": "application/x-ndjson"},
            timeout=timeout,
        ) as response:
            response.raise_for_status()
            for line in response.iter_lines():
                if not line:
                    continue
                event = json.loads(line)
                if not isinstance(event, dict):
                    raise TypeError(f"expected exec event object, got {type(event)!r}")
                yield event

    @staticmethod
    def _decode_json(response: httpx.Response) -> dict[str, Any]:
        try:
            response.raise_for_status()
        except httpx.HTTPStatusError as exc:
            detail = response.text.strip()
            if detail:
                message = f"{exc} Response body: {detail}"
                raise httpx.HTTPStatusError(message, request=exc.request, response=exc.response) from exc
            raise
        payload = response.json()
        if not isinstance(payload, dict):
            raise TypeError(f"expected JSON object response, got {type(payload)!r}")
        return payload


def resolve_http_timeout(timeout: Optional[Union[float, httpx.Timeout]]) -> httpx.Timeout:
    if isinstance(timeout, httpx.Timeout):
        return timeout
    if timeout is not None:
        return httpx.Timeout(timeout)

    raw = os.environ.get("PYNEURODESK_HTTP_TIMEOUT", "").strip()
    if raw:
        return httpx.Timeout(float(raw))

    return httpx.Timeout(connect=10.0, read=300.0, write=300.0, pool=10.0)


def resolve_boot_timeout(timeout: Optional[Union[float, httpx.Timeout]] = None) -> httpx.Timeout:
    if isinstance(timeout, httpx.Timeout):
        return timeout
    if timeout is not None:
        return httpx.Timeout(timeout)

    raw = os.environ.get("PYNEURODESK_BOOT_TIMEOUT", "").strip()
    if raw:
        return httpx.Timeout(float(raw))

    return httpx.Timeout(connect=10.0, read=DEFAULT_BOOT_TIMEOUT_SECONDS, write=DEFAULT_BOOT_TIMEOUT_SECONDS, pool=10.0)


def resolve_boot_timeout_seconds(timeout: Optional[Union[float, httpx.Timeout]] = None) -> Optional[float]:
    if isinstance(timeout, httpx.Timeout):
        return None
    if timeout is not None:
        return float(timeout)

    raw = os.environ.get("PYNEURODESK_BOOT_TIMEOUT", "").strip()
    if raw:
        return float(raw)

    return DEFAULT_BOOT_TIMEOUT_SECONDS
