from __future__ import annotations

import os
import json
from collections.abc import Iterable
from typing import Any

import httpx

from .models import (
    CommandResult,
    CVMFSListResponse,
    CVMFSReadRequest,
    CVMFSReadResponse,
    CVMFSSource,
    ContainerReference,
    ImageState,
    ImportImageRequest,
    RunCommandRequest,
    ShareMount,
    VMState,
)

DEFAULT_BOOT_TIMEOUT_SECONDS = 30.0


class PyNeurodeskClient:
    def __init__(
        self,
        base_url: str,
        *,
        client: httpx.Client | None = None,
        timeout: float | httpx.Timeout | None = None,
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

    def import_cvmfs_container(
        self,
        name: str,
        *,
        mirror: str,
        repo: str,
        path: str,
        cache_dir: str | None = None,
    ) -> ImageState:
        return self.import_image(
            name,
            ImportImageRequest.from_cvmfs_container(
                mirror=mirror,
                repo=repo,
                path=path,
                cache_dir=cache_dir,
            ),
        )

    def get_image(self, name: str) -> ImageState | None:
        response = self._client.get(f"/image/{name}")
        if response.status_code == 404:
            return None
        payload = self._decode_json(response)
        return ImageState.from_payload(payload)

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
        timeout: float | httpx.Timeout | None = None,
        dmesg: bool = False,
        memory_mb: int | None = None,
        cpus: int | None = None,
    ) -> VMState:
        payload: dict[str, Any] = {"image": image}
        if dmesg:
            payload["dmesg"] = True
        if memory_mb is not None:
            payload["memory_mb"] = memory_mb
        if cpus is not None:
            payload["cpus"] = cpus
        response = self._client.post("/vm", json=payload, timeout=resolve_boot_timeout(timeout))
        payload = self._decode_json(response)
        return VMState.from_payload(payload)

    def create_instance_stream(
        self,
        image: str,
        *,
        timeout: float | httpx.Timeout | None = None,
        dmesg: bool = False,
    ) -> Iterable[dict[str, Any]]:
        payload: dict[str, Any] = {"image": image}
        if dmesg:
            payload["dmesg"] = True
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

    def ensure_instance(
        self,
        image: str,
        *,
        timeout: float | httpx.Timeout | None = None,
        dmesg: bool = False,
        memory_mb: int | None = None,
        cpus: int | None = None,
    ) -> VMState:
        state = self.instance_status()
        if state.status == "running" and state.image == image:
            return state
        if state.status == "running" and state.image not in ("", None, image):
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
        timeout: float | httpx.Timeout | None = None,
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
        workdir: str | None = None,
        user: str | None = None,
        stdin: bytes | None = None,
        timeout: float | httpx.Timeout | None = None,
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

    @staticmethod
    def _decode_json(response: httpx.Response) -> dict[str, Any]:
        response.raise_for_status()
        payload = response.json()
        if not isinstance(payload, dict):
            raise TypeError(f"expected JSON object response, got {type(payload)!r}")
        return payload


def resolve_http_timeout(timeout: float | httpx.Timeout | None) -> httpx.Timeout:
    if isinstance(timeout, httpx.Timeout):
        return timeout
    if timeout is not None:
        return httpx.Timeout(timeout)

    raw = os.environ.get("PYNEURODESK_HTTP_TIMEOUT", "").strip()
    if raw:
        return httpx.Timeout(float(raw))

    return httpx.Timeout(connect=10.0, read=300.0, write=300.0, pool=10.0)


def resolve_boot_timeout(timeout: float | httpx.Timeout | None = None) -> httpx.Timeout:
    if isinstance(timeout, httpx.Timeout):
        return timeout
    if timeout is not None:
        return httpx.Timeout(timeout)

    raw = os.environ.get("PYNEURODESK_BOOT_TIMEOUT", "").strip()
    if raw:
        return httpx.Timeout(float(raw))

    return httpx.Timeout(connect=10.0, read=DEFAULT_BOOT_TIMEOUT_SECONDS, write=DEFAULT_BOOT_TIMEOUT_SECONDS, pool=10.0)
