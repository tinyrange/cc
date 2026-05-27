from __future__ import annotations

import io
import base64
import json
from pathlib import Path
from types import SimpleNamespace
from typing import Optional

import httpx
import pytest

import neurodesk as nd
import pyneurodesk.api as api
from pyneurodesk import (
    CVMFSReadRequest,
    CVMFSSource,
    ImportImageRequest,
    PortForward,
    PyNeurodeskClient,
    resolve_base_url,
)
from pyneurodesk.api import (
    StreamProgressReporter,
    create_container_cache_dir,
    build_release_container_path,
    create_progress_reporter,
    load_deploy_metadata,
    parse_top_level_deploy,
    runtime_deploy_env_entries,
    default_daemon_state_path,
    path_join,
    resolve_ccvm_binary_path,
    resolve_release_index_dir,
    start_default_daemon,
)
from pyneurodesk.models import DaemonState
from pyneurodesk.client import resolve_http_timeout
from pyneurodesk.client import resolve_boot_timeout


@pytest.fixture(autouse=True)
def disable_local_release_index(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("pyneurodesk.api.resolve_release_index_dir", lambda: None)
    monkeypatch.setattr(
        "pyneurodesk.api._search_remote_release_versions", lambda name: {}
    )


def make_client(handler: httpx.MockTransport) -> PyNeurodeskClient:
    http_client = httpx.Client(
        transport=handler,
        base_url="http://ccx3.test",
    )
    return PyNeurodeskClient("http://ccx3.test", client=http_client)


def test_import_cvmfs_container_posts_structured_source_payload() -> None:
    seen: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["method"] = request.method
        seen["path"] = request.url.path
        seen["json"] = request.read().decode()
        return httpx.Response(
            200,
            json={
                "name": "niimath",
                "status": "downloaded",
                "source_kind": "cvmfs",
            },
        )

    client = make_client(httpx.MockTransport(handler))
    try:
        result = client.import_cvmfs_container(
            "niimath",
            mirror="http://cvmfs.neurodesk.org",
            repo="neurodesk.ardc.edu.au",
            path="/containers/niimath_1.0.20250804_20251016",
        )
    finally:
        client.close()

    assert seen["method"] == "POST"
    assert seen["path"] == "/image/niimath"
    assert seen["json"] == (
        '{"source":{"type":"cvmfs","mirror":"http://cvmfs.neurodesk.org",'
        '"repo":"neurodesk.ardc.edu.au","path":"/containers/niimath_1.0.20250804_20251016"}}'
    )
    assert result.name == "niimath"
    assert result.status == "downloaded"
    assert result.source_kind == "cvmfs"


def test_import_image_request_from_cvmfs_container_serializes_expected_shape() -> None:
    request = ImportImageRequest.from_cvmfs_container(
        mirror="http://cvmfs.neurodesk.org",
        repo="neurodesk.ardc.edu.au",
        path="/containers/niimath_1.0.20250804_20251016",
        cache_dir="/tmp/cvmfs-cache",
    )

    assert request.to_payload() == {
        "source": {
            "type": "cvmfs",
            "mirror": "http://cvmfs.neurodesk.org",
            "repo": "neurodesk.ardc.edu.au",
            "path": "/containers/niimath_1.0.20250804_20251016",
        },
        "cache_dir": "/tmp/cvmfs-cache",
    }


def test_import_image_request_serializes_cvmfs_mirrors() -> None:
    request = ImportImageRequest.from_cvmfs_container(
        mirror="http://primary.example",
        mirrors=("http://mirror-a.example", "http://mirror-b.example"),
        repo="neurodesk.ardc.edu.au",
        path="/containers/niimath",
    )

    assert request.to_payload()["source"]["mirrors"] == [
        "http://mirror-a.example",
        "http://mirror-b.example",
    ]


def test_cvmfs_list_posts_source_and_parses_entries() -> None:
    seen: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["path"] = request.url.path
        seen["json"] = request.read().decode()
        return httpx.Response(
            200,
            json={
                "entries": [
                    {
                        "name": "niimath_1.0.20250804_20251016",
                        "path": "/containers/niimath_1.0.20250804_20251016",
                        "kind": "directory",
                    },
                    {
                        "name": "niimath_1.0.20250804_20251016.simg",
                        "path": "/containers/niimath_1.0.20250804_20251016/niimath_1.0.20250804_20251016.simg",
                        "kind": "directory",
                        "size": 134885376,
                    },
                ]
            },
        )

    client = make_client(httpx.MockTransport(handler))
    try:
        result = client.cvmfs_list(
            CVMFSSource(
                mirror="http://cvmfs.neurodesk.org",
                repo="neurodesk.ardc.edu.au",
                path="/containers",
                cache_dir="/tmp/cvmfs-cache",
            )
        )
    finally:
        client.close()

    assert seen["path"] == "/cvmfs/list"
    assert seen["json"] == (
        '{"mirror":"http://cvmfs.neurodesk.org","repo":"neurodesk.ardc.edu.au",'
        '"path":"/containers","cache_dir":"/tmp/cvmfs-cache"}'
    )
    assert len(result.entries) == 2
    assert result.entries[0].kind == "directory"
    assert result.entries[1].size == 134885376


def test_cvmfs_read_posts_offset_and_length() -> None:
    seen: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["path"] = request.url.path
        seen["json"] = request.read().decode()
        return httpx.Response(
            200,
            json={
                "path": "/containers/niimath/niimath.simg",
                "offset": 4096,
                "data": "SIF",
                "eof": False,
            },
        )

    client = make_client(httpx.MockTransport(handler))
    try:
        result = client.cvmfs_read(
            CVMFSReadRequest(
                mirror="http://cvmfs.neurodesk.org",
                repo="neurodesk.ardc.edu.au",
                path="/containers/niimath/niimath.simg",
                offset=4096,
                length=8192,
                cache_dir="/tmp/cvmfs-cache",
            )
        )
    finally:
        client.close()

    assert seen["path"] == "/cvmfs/read"
    assert seen["json"] == (
        '{"mirror":"http://cvmfs.neurodesk.org","repo":"neurodesk.ardc.edu.au",'
        '"path":"/containers/niimath/niimath.simg","offset":4096,"length":8192,'
        '"cache_dir":"/tmp/cvmfs-cache"}'
    )
    assert result.path.endswith("niimath.simg")
    assert result.offset == 4096
    assert result.data == b"SIF"
    assert result.eof is False


def test_run_command_request_serializes_runtime_shares() -> None:
    seen: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["path"] = request.url.path
        seen["json"] = request.read().decode()
        return httpx.Response(200, json={"exit_code": 0, "output": "ok"})

    client = make_client(httpx.MockTransport(handler))
    try:
        result = client.run(
            "niimath",
            ["cat", "/.share/demo/hello.txt"],
            shares=[
                nd.ShareMount(
                    source="/host/demo",
                    mount="/.share/demo",
                )
            ],
        )
    finally:
        client.close()

    assert seen["path"] == "/vm/run"
    assert seen["json"] == (
        '{"image":"niimath","command":["cat","/.share/demo/hello.txt"],'
        '"shares":[{"source":"/host/demo","mount":"/.share/demo"}]}'
    )
    assert result.output == "ok"


def test_run_command_request_serializes_timeout_seconds() -> None:
    seen: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["json"] = request.read().decode()
        return httpx.Response(200, json={"exit_code": 124, "output": "timed out"})

    client = make_client(httpx.MockTransport(handler))
    try:
        result = client.run("niimath", ["sleep", "30"], timeout_seconds=2.5)
    finally:
        client.close()

    assert (
        seen["json"]
        == '{"image":"niimath","command":["sleep","30"],"timeout_seconds":2.5}'
    )
    assert result.exit_code == 124


def test_named_vm_client_methods_send_id() -> None:
    seen: list[tuple[str, str, str, str]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read().decode()
        seen.append(
            (request.method, request.url.path, str(request.url.query.decode()), body)
        )
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(
                200, json={"id": request.url.params.get("id"), "status": "stopped"}
            )
        if request.method == "POST" and request.url.path == "/vm":
            return httpx.Response(
                200, json={"id": "analysis", "status": "running", "image": "niimath"}
            )
        if request.method == "POST" and request.url.path == "/vm/run":
            return httpx.Response(200, json={"exit_code": 0, "output": "ok"})
        if request.method == "POST" and request.url.path == "/vm/shutdown":
            return httpx.Response(
                200, json={"id": request.url.params.get("id"), "status": "stopped"}
            )
        raise AssertionError(f"unexpected request: {request.method} {request.url}")

    client = make_client(httpx.MockTransport(handler))
    try:
        status = client.instance_status(vm_id="analysis")
        created = client.create_instance("niimath", vm_id="analysis")
        result = client.run("niimath", ["true"], vm_id="analysis")
        stopped = client.shutdown_instance(vm_id="analysis")
    finally:
        client.close()

    assert status.id == "analysis"
    assert created.id == "analysis"
    assert result.output == "ok"
    assert stopped.id == "analysis"
    assert seen == [
        ("GET", "/vm/status", "id=analysis", ""),
        (
            "POST",
            "/vm",
            "",
            '{"image":"niimath","id":"analysis","timeout_seconds":5.0}',
        ),
        (
            "POST",
            "/vm/run",
            "",
            '{"image":"niimath","command":["true"],"id":"analysis"}',
        ),
        ("POST", "/vm/shutdown", "id=analysis", ""),
    ]


def test_watchdog_client_endpoints() -> None:
    seen: list[tuple[str, str, Optional[str]]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read().decode() or None
        seen.append((request.method, request.url.path, body))
        if request.url.path == "/watchdog":
            return httpx.Response(
                200, json={"status": "watching", "timeout_seconds": 30}
            )
        if request.url.path == "/watchdog/feed":
            return httpx.Response(200, json={"status": "fed"})
        raise AssertionError(f"unexpected request: {request.method} {request.url.path}")

    client = make_client(httpx.MockTransport(handler))
    try:
        assert client.create_watchdog(timeout_seconds=30)["status"] == "watching"
        assert client.feed_watchdog()["status"] == "fed"
    finally:
        client.close()

    assert seen == [
        ("POST", "/watchdog", '{"timeout_seconds":30}'),
        ("POST", "/watchdog/feed", None),
    ]


def test_watchdog_feed_loop_continues_after_transient_http_error(monkeypatch) -> None:
    stop = api.threading.Event()
    calls: list[str] = []

    class FakeResponse:
        def raise_for_status(self) -> None:
            return None

    class FakeHTTPClient:
        def __init__(self, *, base_url: str, timeout: float) -> None:
            self.base_url = base_url
            self.timeout = timeout

        def __enter__(self) -> "FakeHTTPClient":
            return self

        def __exit__(self, exc_type: object, exc: object, tb: object) -> None:
            return None

        def post(self, path: str) -> FakeResponse:
            calls.append(path)
            if len(calls) == 1:
                raise httpx.ConnectTimeout("transient timeout")
            stop.set()
            return FakeResponse()

    monkeypatch.setattr(api, "WATCHDOG_FEED_INTERVAL_SECONDS", 0.001)
    monkeypatch.setattr(api.httpx, "Client", FakeHTTPClient)

    api._feed_daemon_watchdog("http://127.0.0.1:4567", stop)

    assert calls == ["/watchdog/feed", "/watchdog/feed"]


def test_create_instance_uses_boot_timeout() -> None:
    seen: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["path"] = request.url.path
        seen["timeout"] = request.extensions.get("timeout")
        return httpx.Response(200, json={"status": "running", "image": "niimath"})

    client = make_client(httpx.MockTransport(handler))
    try:
        result = client.create_instance("niimath")
    finally:
        client.close()

    assert seen["path"] == "/vm"
    assert seen["timeout"] is not None
    assert seen["timeout"]["read"] == 5.0
    assert result.status == "running"


def test_start_instance_posts_vm_start_and_uses_boot_timeout() -> None:
    seen: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["path"] = request.url.path
        seen["json"] = request.read().decode()
        seen["timeout"] = request.extensions.get("timeout")
        return httpx.Response(200, json={"status": "running"})

    client = make_client(httpx.MockTransport(handler))
    try:
        result = client.start_instance(memory_mb=1024, cpus=1)
    finally:
        client.close()

    assert seen["path"] == "/vm/start"
    assert seen["json"] == '{"memory_mb":1024,"cpus":1,"timeout_seconds":5.0}'
    assert seen["timeout"] is not None
    assert seen["timeout"]["read"] == 5.0
    assert result.status == "running"


def test_container_cold_start_uses_http_timeout_for_preflight_and_boot_timeout_for_vm() -> (
    None
):
    seen: list[tuple[str, Optional[float]]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        timeout = request.extensions.get("timeout")
        read_timeout = timeout.get("read") if isinstance(timeout, dict) else None
        seen.append((request.url.path, read_timeout))

        if request.method == "POST" and request.url.path == "/cvmfs/list":
            payload = json.loads(request.read().decode() or "{}")
            if payload["path"] == "/containers/niimath":
                return httpx.Response(200, json={"entries": []})
            if payload["path"] == "/containers":
                return httpx.Response(
                    200,
                    json={
                        "entries": [
                            {
                                "name": "niimath",
                                "path": "/containers/niimath",
                                "kind": "directory",
                            }
                        ]
                    },
                )
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "downloaded",
                    "source_kind": "cvmfs",
                },
            )
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(200, json={"status": "stopped"})
        if request.method == "POST" and request.url.path == "/kernel/download":
            return httpx.Response(200, json={"status": "downloaded"})
        if (
            request.method == "POST"
            and request.url.path == "/image/niimath/qemu/download"
        ):
            return httpx.Response(
                200,
                json={"status": "downloaded", "path": "/tmp/qemu", "required": True},
            )
        if request.method == "POST" and request.url.path == "/image/niimath/metadata":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "prepared",
                    "source_kind": "cvmfs",
                    "architecture": "amd64",
                },
            )
        if request.method == "POST" and request.url.path == "/vm":
            return httpx.Response(200, json={"status": "running", "image": "niimath"})
        raise AssertionError(f"unexpected request: {request.method} {request.url.path}")

    client = make_client(httpx.MockTransport(handler))
    try:
        container = nd.container("niimath", client=client, progress=False)
    finally:
        client.close()

    assert container.path == "/containers/niimath"
    timeouts = dict(seen)
    inherited_timeout = timeouts["/image/niimath"]
    assert timeouts["/kernel/download"] == inherited_timeout
    assert timeouts["/image/niimath/qemu/download"] == inherited_timeout
    assert timeouts["/image/niimath/metadata"] == inherited_timeout
    assert timeouts["/vm"] == 5.0


def test_container_does_not_boot_vm_if_preflight_fails() -> None:
    seen_paths: list[str] = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen_paths.append(request.url.path)
        if request.method == "POST" and request.url.path == "/cvmfs/list":
            payload = json.loads(request.read().decode() or "{}")
            if payload["path"] == "/containers/niimath":
                return httpx.Response(200, json={"entries": []})
            if payload["path"] == "/containers":
                return httpx.Response(
                    200,
                    json={
                        "entries": [
                            {
                                "name": "niimath",
                                "path": "/containers/niimath",
                                "kind": "directory",
                            }
                        ]
                    },
                )
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "downloaded",
                    "source_kind": "cvmfs",
                },
            )
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(200, json={"status": "stopped"})
        if request.method == "POST" and request.url.path == "/kernel/download":
            return httpx.Response(500, json={"error": "kernel mirror unavailable"})
        raise AssertionError(f"unexpected request: {request.method} {request.url.path}")

    client = make_client(httpx.MockTransport(handler))
    try:
        with pytest.raises(httpx.HTTPStatusError):
            nd.container("niimath", client=client, progress=False)
    finally:
        client.close()

    assert "/vm" not in seen_paths


def test_client_http_error_includes_response_body() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "POST" and request.url.path == "/vm":
            return httpx.Response(
                400, text="boot failed: nested virtualization unavailable"
            )
        raise AssertionError(f"unexpected request: {request.method} {request.url.path}")

    client = make_client(httpx.MockTransport(handler))
    try:
        with pytest.raises(
            httpx.HTTPStatusError, match="nested virtualization unavailable"
        ):
            client.create_instance("niimath")
    finally:
        client.close()


def test_client_http_error_prefers_json_error_detail() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "POST" and request.url.path == "/vm":
            return httpx.Response(
                503,
                json={
                    "error": (
                        "kvm unavailable: /dev/kvm does not exist; hardware virtualization "
                        "is not available to this host"
                    )
                },
            )
        raise AssertionError(f"unexpected request: {request.method} {request.url.path}")

    client = make_client(httpx.MockTransport(handler))
    try:
        with pytest.raises(httpx.HTTPStatusError) as excinfo:
            client.create_instance("niimath")
    finally:
        client.close()

    message = str(excinfo.value)
    assert "Response body: kvm unavailable: /dev/kvm does not exist" in message
    assert '{"error"' not in message


def test_create_instance_sends_dmesg_when_requested() -> None:
    seen: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["path"] = request.url.path
        seen["json"] = request.read().decode()
        return httpx.Response(200, json={"status": "running", "image": "niimath"})

    client = make_client(httpx.MockTransport(handler))
    try:
        result = client.create_instance("niimath", dmesg=True)
    finally:
        client.close()

    assert seen["path"] == "/vm"
    assert seen["json"] == '{"image":"niimath","dmesg":true,"timeout_seconds":5.0}'
    assert result.status == "running"


def test_create_instance_sends_memory_and_cpu_overrides() -> None:
    seen: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["path"] = request.url.path
        seen["json"] = request.read().decode()
        return httpx.Response(
            200,
            json={
                "status": "running",
                "image": "niimath",
                "memory_mb": 4096,
                "cpus": 2,
            },
        )

    client = make_client(httpx.MockTransport(handler))
    try:
        result = client.create_instance("niimath", memory_mb=4096, cpus=2)
    finally:
        client.close()

    assert seen["path"] == "/vm"
    assert (
        seen["json"]
        == '{"image":"niimath","memory_mb":4096,"cpus":2,"timeout_seconds":5.0}'
    )
    assert result.memory_mb == 4096
    assert result.cpus == 2


def test_create_instance_can_opt_into_network() -> None:
    seen: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["path"] = request.url.path
        seen["json"] = request.read().decode()
        return httpx.Response(200, json={"status": "running", "image": "niimath"})

    client = make_client(httpx.MockTransport(handler))
    try:
        client.create_instance("niimath", with_network=True)
    finally:
        client.close()

    assert seen["path"] == "/vm"
    assert (
        seen["json"]
        == '{"image":"niimath","network":{"enabled":true},"timeout_seconds":5.0}'
    )


def test_add_port_forward_posts_dynamic_forward() -> None:
    seen: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["path"] = request.url.path
        seen["json"] = request.read().decode()
        return httpx.Response(
            200,
            json={
                "protocol": "tcp",
                "host_addr": "127.0.0.1",
                "host_port": 8080,
                "guest_addr": "10.42.0.2",
                "guest_port": 8080,
            },
        )

    client = make_client(httpx.MockTransport(handler))
    try:
        forward = client.add_port_forward(PortForward(host_port=8080, guest_port=8080))
    finally:
        client.close()

    assert seen["path"] == "/vm/forward"
    assert seen["json"] == '{"protocol":"tcp","guest_port":8080,"host_port":8080}'
    assert forward.host_port == 8080
    assert forward.guest_port == 8080


def test_create_instance_stream_yields_boot_events() -> None:
    seen: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["json"] = request.content.decode()
        assert request.url.params.get("stream") == "1"
        return httpx.Response(
            200,
            text='{"kind":"status","message":"starting"}\n{"kind":"serial","data":"boot line\\n"}\n{"kind":"ready"}\n',
            headers={"content-type": "application/x-ndjson"},
        )

    client = make_client(httpx.MockTransport(handler))
    try:
        events = list(
            client.create_instance_stream("niimath", dmesg=True, memory_mb=8192, cpus=4)
        )
    finally:
        client.close()

    assert (
        seen["json"]
        == '{"image":"niimath","dmesg":true,"memory_mb":8192,"cpus":4,"timeout_seconds":5.0}'
    )
    assert [event["kind"] for event in events] == ["status", "serial", "ready"]


def test_start_instance_stream_yields_boot_events() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/vm/start"
        assert request.url.params.get("stream") == "1"
        return httpx.Response(
            200,
            text='{"kind":"status","message":"starting"}\n{"kind":"ready"}\n',
            headers={"content-type": "application/x-ndjson"},
        )

    client = make_client(httpx.MockTransport(handler))
    try:
        events = list(client.start_instance_stream())
    finally:
        client.close()

    assert [event["kind"] for event in events] == ["status", "ready"]


def test_ensure_instance_replaces_running_blank_vm() -> None:
    seen: list[str] = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen.append(request.url.path)
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(200, json={"status": "running"})
        if request.method == "POST" and request.url.path == "/vm/shutdown":
            return httpx.Response(200, json={"status": "stopped"})
        if request.method == "POST" and request.url.path == "/vm":
            return httpx.Response(200, json={"status": "running", "image": "niimath"})
        raise AssertionError(f"unexpected request: {request.method} {request.url.path}")

    client = make_client(httpx.MockTransport(handler))
    try:
        state = client.ensure_instance("niimath")
    finally:
        client.close()

    assert state.status == "running"
    assert seen == ["/vm/status", "/vm/shutdown", "/vm"]


def test_resolve_boot_timeout_defaults_to_5_seconds() -> None:
    timeout = resolve_boot_timeout()
    assert timeout.connect == 10.0
    assert timeout.read == 5.0
    assert timeout.write == 5.0
    assert timeout.pool == 10.0


def test_non_object_json_response_raises_type_error() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json=["unexpected"])

    client = make_client(httpx.MockTransport(handler))
    try:
        try:
            client.cvmfs_list(
                CVMFSSource(
                    mirror="http://cvmfs.neurodesk.org",
                    repo="neurodesk.ardc.edu.au",
                    path="/containers",
                )
            )
        except TypeError as exc:
            assert "expected JSON object response" in str(exc)
        else:
            raise AssertionError("expected TypeError")
    finally:
        client.close()


def test_container_lookup_imports_and_runs_notebook_style_api() -> None:
    seen: list[tuple[str, str, Optional[str]]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read().decode() or None
        seen.append((request.method, request.url.path, body))

        if request.method == "POST" and request.url.path == "/cvmfs/list":
            payload = json.loads(body or "{}")
            if payload["path"] == "/containers/niimath":
                return httpx.Response(200, json={"entries": []})
            if payload["path"] == "/containers":
                return httpx.Response(
                    200,
                    json={
                        "entries": [
                            {
                                "name": "niimath_1.0.20250804_20251016",
                                "path": "/containers/niimath_1.0.20250804_20251016",
                                "kind": "directory",
                            }
                        ]
                    },
                )
            if payload["path"] == "/containers/niimath_1.0.20250804_20251016":
                return httpx.Response(
                    200,
                    json={
                        "entries": [
                            {"name": "commands.txt", "path": "", "kind": "file"},
                            {"name": "niimath", "path": "", "kind": "file"},
                        ]
                    },
                )
        if request.method == "POST" and request.url.path == "/cvmfs/read":
            payload = json.loads(body or "{}")
            if (
                payload["path"]
                == "/containers/niimath_1.0.20250804_20251016/commands.txt"
            ):
                return httpx.Response(
                    200,
                    json={
                        "path": payload["path"],
                        "offset": 0,
                        "data": base64.b64encode(b"niimath\n").decode(),
                        "eof": True,
                    },
                )
            if payload["path"] == "/containers/niimath_1.0.20250804_20251016/env.txt":
                return httpx.Response(404, json={"error": "not found"})
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(404, json={"error": "not found"})
        if request.method == "POST" and request.url.path == "/image/niimath":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "downloaded",
                    "source_kind": "cvmfs",
                },
            )
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(200, json={"status": "stopped"})
        if request.method == "POST" and request.url.path == "/kernel/download":
            return httpx.Response(200, json={"status": "downloaded"})
        if (
            request.method == "POST"
            and request.url.path == "/image/niimath/qemu/download"
        ):
            return httpx.Response(
                200,
                json={"status": "downloaded", "path": "/tmp/qemu", "required": True},
            )
        if request.method == "POST" and request.url.path == "/image/niimath/metadata":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "prepared",
                    "source_kind": "cvmfs",
                    "architecture": "amd64",
                },
            )
        if request.method == "POST" and request.url.path == "/vm":
            return httpx.Response(200, json={"status": "running", "image": "niimath"})
        if request.method == "POST" and request.url.path == "/vm/run":
            return httpx.Response(
                200,
                json={
                    "exit_code": 0,
                    "output": "niimath version 1.0\n",
                },
            )
        raise AssertionError(
            f"unexpected request: {request.method} {request.url.path} {body!r}"
        )

    client = make_client(httpx.MockTransport(handler))
    try:
        niimath = nd.container("niimath", client=client)
        out = niimath.niimath()
    finally:
        client.close()

    assert out == "niimath version 1.0\n"
    paths = [path for _, path, _ in seen]
    assert "/kernel/download" in paths
    assert "/image/niimath/qemu/download" in paths
    assert "/image/niimath/metadata" in paths
    assert "/vm" in paths
    assert "/vm/run" in paths
    assert paths.index("/kernel/download") < paths.index("/vm")
    assert paths.index("/image/niimath/qemu/download") < paths.index("/vm")
    assert paths.index("/image/niimath/metadata") < paths.index("/vm")
    assert paths.index("/vm") < paths.index("/vm/run")


def test_container_run_uses_cvmfs_deploy_env_and_exposes_commands() -> None:
    seen: list[tuple[str, str, Optional[str]]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read().decode() or None
        seen.append((request.method, request.url.path, body))

        if request.method == "POST" and request.url.path == "/cvmfs/list":
            payload = json.loads(body or "{}")
            if payload["path"] == "/containers/niimath":
                return httpx.Response(200, json={"entries": []})
            if payload["path"] == "/containers":
                return httpx.Response(
                    200,
                    json={
                        "entries": [
                            {
                                "name": "niimath_1.0.20250804_20251016",
                                "path": "/containers/niimath_1.0.20250804_20251016",
                                "kind": "directory",
                            }
                        ]
                    },
                )
            if payload["path"] == "/containers/niimath_1.0.20250804_20251016":
                return httpx.Response(
                    200,
                    json={
                        "entries": [
                            {"name": "commands.txt", "path": "", "kind": "file"},
                            {"name": "env.txt", "path": "", "kind": "file"},
                            {"name": "niimath", "path": "", "kind": "file"},
                            {"name": "bet", "path": "", "kind": "file"},
                        ]
                    },
                )
        if request.method == "POST" and request.url.path == "/cvmfs/read":
            payload = json.loads(body or "{}")
            if (
                payload["path"]
                == "/containers/niimath_1.0.20250804_20251016/commands.txt"
            ):
                return httpx.Response(
                    200,
                    json={
                        "path": payload["path"],
                        "offset": 0,
                        "data": base64.b64encode(
                            b"niimath\nbet\nmissing-wrapper\n"
                        ).decode(),
                        "eof": True,
                    },
                )
            if payload["path"] == "/containers/niimath_1.0.20250804_20251016/env.txt":
                return httpx.Response(
                    200,
                    json={
                        "path": payload["path"],
                        "offset": 0,
                        "data": base64.b64encode(
                            b"DEPLOY_ENV_FSLDIR=BASEPATH/opt/fsl\n"
                        ).decode(),
                        "eof": True,
                    },
                )
            if (
                payload["path"]
                == "/containers/niimath_1.0.20250804_20251016/build.yaml"
            ):
                return httpx.Response(
                    200,
                    json={
                        "path": payload["path"],
                        "offset": 0,
                        "data": "",
                        "eof": True,
                    },
                )
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "downloaded",
                    "source_kind": "cvmfs",
                },
            )
        if request.method == "POST" and request.url.path == "/image/niimath/metadata":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "prepared",
                    "source_kind": "cvmfs",
                    "env": ["PATH=/usr/local/bin:/usr/bin:/bin:/opt/niimath"],
                },
            )
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(200, json={"status": "running", "image": "niimath"})
        if request.method == "POST" and request.url.path == "/vm/run":
            payload = json.loads(body or "{}")
            assert payload["env"] == [
                "PATH=/usr/local/bin:/usr/bin:/bin:/opt/niimath",
                "DEPLOY_ENV_FSLDIR=/opt/fsl",
                "FSLDIR=/opt/fsl",
            ]
            return httpx.Response(200, json={"exit_code": 0, "output": "ok\n"})
        raise AssertionError(
            f"unexpected request: {request.method} {request.url.path} {body!r}"
        )

    client = make_client(httpx.MockTransport(handler))
    try:
        handle = nd.container("niimath", client=client, progress=False)
        assert handle.commands == ("bet", "niimath")
        assert handle.deploy_env == (
            "PATH=/usr/local/bin:/usr/bin:/bin:/opt/niimath",
            "DEPLOY_ENV_FSLDIR=/opt/fsl",
        )
        out = handle.niimath("-help")
    finally:
        client.close()

    assert out == "ok\n"
    paths = [path for _, path, _ in seen]
    assert paths.count("/cvmfs/list") == 3
    assert paths.count("/cvmfs/read") == 3
    assert paths[-1] == "/vm/run"


def test_load_deploy_metadata_uses_local_image_env_and_deploy_bins() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        if (
            request.method == "POST"
            and request.url.path == "/image/fulltest-image/metadata"
        ):
            return httpx.Response(
                200,
                json={
                    "name": "fulltest-image",
                    "status": "prepared",
                    "source_kind": "simg",
                    "architecture": "amd64",
                    "env": [
                        "PATH=/opt/tool:/usr/local/bin:/usr/bin:/bin",
                        "DEPLOY_PATH=/opt/tool",
                        "DEPLOY_BINS=tool:tool-helper:/bad/path",
                    ],
                },
            )
        raise AssertionError(f"unexpected request: {request.method} {request.url.path}")

    client = make_client(httpx.MockTransport(handler))
    try:
        handle = SimpleNamespace(
            _client=client,
            reference=SimpleNamespace(
                image="fulltest-image",
                path="/tmp/neurocontainers-simg/tool.simg",
                cache_dir=None,
            ),
        )
        metadata = load_deploy_metadata(handle)
    finally:
        client.close()

    assert metadata.commands == ("tool", "tool-helper")
    assert metadata.deploy_env == (
        "PATH=/opt/tool:/usr/local/bin:/usr/bin:/bin",
        "DEPLOY_PATH=/opt/tool",
        "DEPLOY_BINS=tool:tool-helper:/bad/path",
    )


def test_runtime_deploy_env_skips_internal_deploy_metadata() -> None:
    assert runtime_deploy_env_entries(
        [
            "PATH=/opt/tool:/usr/bin:/bin",
            "DEPLOY_PATH=/opt/tool",
            "DEPLOY_BINS=tool:tool-helper",
            "DEPLOY_ENV_CUSTOM=ok",
        ]
    ) == (
        "PATH=/opt/tool:/usr/bin:/bin",
        "DEPLOY_ENV_CUSTOM=ok",
        "CUSTOM=ok",
    )


def test_run_stream_reports_invalid_ndjson_event() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "POST" and request.url.path == "/vm/run":
            return httpx.Response(
                200, content=b'{"kind":"stdout","output":"unterminated\n'
            )
        raise AssertionError(f"unexpected request: {request.method} {request.url.path}")

    client = make_client(httpx.MockTransport(handler))
    try:
        events = client.run_stream("image", ["tool"])
        with pytest.raises(RuntimeError, match="invalid streamed exec event JSON"):
            list(events)
    finally:
        client.close()


def test_parse_top_level_deploy_supports_lists_and_skips_templates() -> None:
    paths, bins = parse_top_level_deploy(
        """
deploy:
  path:
    - /opt/{{ context.name }}/bin
    - /opt/tool/bin,/opt/other/bin
  bins: [tool, tool-view]
"""
    )

    assert paths == ("/opt/tool/bin", "/opt/other/bin")
    assert bins == ("tool", "tool-view")


def test_load_deploy_metadata_merges_cvmfs_build_yaml_deploy_metadata() -> None:
    read_payloads = {
        "/containers/tool/env.txt": base64.b64encode(
            b"DEPLOY_ENV_CUSTOM=ok\n"
        ).decode(),
        "/containers/tool/build.yaml": base64.b64encode(
            b"""
name: tool
deploy:
  path:
    - /opt/tool/bin
  bins: [tool, tool-helper]
"""
        ).decode(),
    }

    def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "POST" and request.url.path == "/image/tool/metadata":
            return httpx.Response(
                200,
                json={
                    "name": "tool",
                    "status": "prepared",
                    "source_kind": "cvmfs",
                    "env": [
                        "PATH=/usr/local/bin:/usr/bin:/bin",
                        "TOOLBOX_PATH=/opt/tool",
                    ],
                },
            )
        if request.method == "POST" and request.url.path == "/cvmfs/list":
            return httpx.Response(
                200,
                json={
                    "entries": [
                        {"name": "commands.txt", "path": "", "kind": "file"},
                        {"name": "env.txt", "path": "", "kind": "file"},
                    ]
                },
            )
        if request.method == "POST" and request.url.path == "/cvmfs/read":
            payload = json.loads(request.read())
            data = read_payloads.get(payload["path"], "")
            return httpx.Response(
                200,
                json={"path": payload["path"], "offset": 0, "data": data, "eof": True},
            )
        raise AssertionError(f"unexpected request: {request.method} {request.url.path}")

    client = make_client(httpx.MockTransport(handler))
    try:
        handle = SimpleNamespace(
            _client=client,
            reference=SimpleNamespace(
                image="tool",
                path="/containers/tool/tool.simg",
                cache_dir=None,
            ),
        )
        metadata = load_deploy_metadata(handle)
    finally:
        client.close()

    assert metadata.commands == ("tool", "tool-helper")
    assert metadata.deploy_env == (
        "PATH=/opt/tool/bin:/usr/local/bin:/usr/bin:/bin",
        "TOOLBOX_PATH=/opt/tool",
        "DEPLOY_PATH=/opt/tool/bin",
        "DEPLOY_BINS=tool:tool-helper",
        "DEPLOY_ENV_CUSTOM=ok",
    )


def test_container_lookup_accepts_versioned_root_simg() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        if request.method == "POST" and request.url.path == "/cvmfs/list":
            return httpx.Response(
                200,
                json={
                    "entries": [
                        {
                            "name": "niimath_1.0.20250804_20251016",
                            "path": "/containers/niimath_1.0.20250804_20251016",
                            "kind": "directory",
                        }
                    ]
                },
            )
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "downloaded",
                    "source_kind": "cvmfs",
                },
            )
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(200, json={"status": "stopped"})
        if request.method == "POST" and request.url.path == "/kernel/download":
            return httpx.Response(200, json={"status": "downloaded"})
        if (
            request.method == "POST"
            and request.url.path == "/image/niimath/qemu/download"
        ):
            return httpx.Response(
                200,
                json={"status": "downloaded", "path": "/tmp/qemu", "required": True},
            )
        if request.method == "POST" and request.url.path == "/image/niimath/metadata":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "prepared",
                    "source_kind": "cvmfs",
                    "architecture": "amd64",
                },
            )
        if request.method == "POST" and request.url.path == "/vm":
            return httpx.Response(200, json={"status": "running", "image": "niimath"})
        if request.method == "POST" and request.url.path == "/vm/run":
            return httpx.Response(200, json={"exit_code": 0, "output": "ok\n"})
        raise AssertionError(f"unexpected request: {request.method} {request.url.path}")

    client = make_client(httpx.MockTransport(handler))
    try:
        niimath = nd.container("niimath", client=client)
    finally:
        client.close()

    assert niimath.path == "/containers/niimath_1.0.20250804_20251016"


def test_container_switches_running_vm_to_requested_image() -> None:
    seen: list[tuple[str, str, Optional[str]]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read().decode() or None
        seen.append((request.method, request.url.path, body))
        if request.method == "POST" and request.url.path == "/cvmfs/list":
            payload = json.loads(body or "{}")
            if payload["path"] == "/containers":
                return httpx.Response(
                    200,
                    json={
                        "entries": [
                            {
                                "name": "niimath",
                                "path": "/containers/niimath",
                                "kind": "directory",
                            }
                        ]
                    },
                )
            if payload["path"] == "/containers/niimath":
                return httpx.Response(200, json={"entries": []})
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "downloaded",
                    "source_kind": "cvmfs",
                },
            )
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(200, json={"status": "running", "image": "other"})
        raise AssertionError(
            f"unexpected request: {request.method} {request.url.path} {body!r}"
        )

    client = make_client(httpx.MockTransport(handler))
    try:
        nd.container("niimath", client=client)
    finally:
        client.close()

    assert ("GET", "/vm/status", None) in seen
    assert all(path != "/vm/shutdown" for _, path, _ in seen)
    assert all(path != "/vm" for _, path, _ in seen)


def test_resolve_base_url_prefers_env(monkeypatch) -> None:
    monkeypatch.setenv("CCX3_URL", "http://127.0.0.1:8123/")
    assert resolve_base_url() == "http://127.0.0.1:8123"


def test_resolve_base_url_reads_daemon_state(monkeypatch, tmp_path: Path) -> None:
    monkeypatch.delenv("CCX3_URL", raising=False)
    monkeypatch.delenv("CCVM_URL", raising=False)
    monkeypatch.setenv("HOME", str(tmp_path))
    calls: list[tuple[str, str]] = []
    monkeypatch.setattr(
        "pyneurodesk.api._health_check",
        lambda base_url: base_url == "http://127.0.0.1:4567",
    )
    monkeypatch.setattr(
        "pyneurodesk.api._supports_vm_start",
        lambda base_url: calls.append(("supports", base_url)) or True,
    )
    monkeypatch.setattr(
        "pyneurodesk.api._ensure_daemon_watchdog",
        lambda base_url: calls.append(("watchdog", base_url)),
    )
    monkeypatch.setenv("XDG_CACHE_HOME", str(tmp_path / ".cache"))
    state_path = default_daemon_state_path()
    state_path.parent.mkdir(parents=True, exist_ok=True)
    state_path.write_text('{"addr":"127.0.0.1:4567"}')

    assert resolve_base_url() == "http://127.0.0.1:4567"
    assert calls == [
        ("supports", "http://127.0.0.1:4567"),
        ("watchdog", "http://127.0.0.1:4567"),
    ]


def test_resolve_base_url_starts_daemon_when_state_missing(monkeypatch) -> None:
    monkeypatch.delenv("CCX3_URL", raising=False)
    monkeypatch.delenv("CCVM_URL", raising=False)
    monkeypatch.setattr(
        "pyneurodesk.api.default_daemon_state_path", lambda: Path("/tmp/does-not-exist")
    )
    monkeypatch.setattr(
        "pyneurodesk.api.start_default_daemon",
        lambda: SimpleNamespace(base_url="http://127.0.0.1:9999"),
    )

    assert resolve_base_url() == "http://127.0.0.1:9999"


def test_start_default_daemon_writes_state(monkeypatch, tmp_path: Path) -> None:
    cache_root = tmp_path / "cache"
    ccvm_path = tmp_path / "bin" / "ccvm"
    ccvm_path.parent.mkdir(parents=True)
    ccvm_path.write_text("")

    class FakeStdout:
        def readline(self) -> str:
            return '{"addr":"127.0.0.1:3456"}\n'

    class FakeProc:
        def __init__(self) -> None:
            self.stdout = FakeStdout()
            self.killed = False

        def kill(self) -> None:
            self.killed = True

        def wait(self) -> int:
            return 0

    seen: dict[str, object] = {}

    def fake_popen(args: list[str], **kwargs: object) -> FakeProc:
        seen["args"] = args
        seen["kwargs"] = kwargs
        return FakeProc()

    monkeypatch.setattr("pyneurodesk.api.default_cache_root", lambda: cache_root)
    monkeypatch.setattr("pyneurodesk.api.resolve_ccvm_binary_path", lambda: ccvm_path)
    monkeypatch.setattr(
        "pyneurodesk.api._health_check",
        lambda base_url: base_url == "http://127.0.0.1:3456",
    )
    monkeypatch.setattr(
        "pyneurodesk.api._ensure_daemon_watchdog",
        lambda base_url: seen.setdefault("watchdog", base_url),
    )
    monkeypatch.setattr("pyneurodesk.api.subprocess.Popen", fake_popen)

    state = start_default_daemon()

    assert state.base_url == "http://127.0.0.1:3456"
    assert state.cache_dir == str(cache_root)
    assert seen["args"] == [str(ccvm_path), "-cache-dir", str(cache_root)]
    assert seen["kwargs"]["cwd"] == str(Path(__file__).resolve().parents[2])
    assert seen["watchdog"] == "http://127.0.0.1:3456"
    assert (cache_root / "ccvm.json").read_text() == '{\n  "addr": "127.0.0.1:3456"\n}'


def test_start_default_daemon_skips_startup_noise(monkeypatch, tmp_path: Path) -> None:
    cache_root = tmp_path / "cache"
    ccvm_path = tmp_path / "bin" / "ccvm"
    ccvm_path.parent.mkdir(parents=True)
    ccvm_path.write_text("")

    class FakeStdout:
        def __init__(self) -> None:
            self.lines = iter(
                [
                    f"{ccvm_path}: replacing existing signature\n",
                    '{"addr":"127.0.0.1:3456"}\n',
                ]
            )

        def readline(self) -> str:
            return next(self.lines, "")

    class FakeProc:
        stdout = FakeStdout()

        def kill(self) -> None:
            return None

        def wait(self) -> int:
            return 0

    monkeypatch.setattr("pyneurodesk.api.resolve_ccvm_binary_path", lambda: ccvm_path)
    monkeypatch.setattr(
        "pyneurodesk.api._health_check",
        lambda base_url: base_url == "http://127.0.0.1:3456",
    )
    monkeypatch.setattr(
        "pyneurodesk.api._ensure_daemon_watchdog",
        lambda base_url: None,
    )
    monkeypatch.setattr(
        "pyneurodesk.api.subprocess.Popen", lambda *args, **kwargs: FakeProc()
    )

    state = api.start_daemon_for_cache_dir(cache_root)

    assert state.base_url == "http://127.0.0.1:3456"
    assert "replacing existing signature" in (
        cache_root / "ccvm-python.log"
    ).read_text()


def test_start_default_daemon_reports_structured_startup_error(
    monkeypatch, tmp_path: Path
) -> None:
    cache_root = tmp_path / "cache"
    ccvm_path = tmp_path / "bin" / "ccvm"
    ccvm_path.parent.mkdir(parents=True)
    ccvm_path.write_text("")

    class FakeStdout:
        def readline(self) -> str:
            return '{"kind":"error","error":"ccvm failed to start","detail":"listen on bad: bind failed"}\n'

    class FakeProc:
        stdout = FakeStdout()

        def kill(self) -> None:
            return None

        def wait(self) -> int:
            return 1

    monkeypatch.setattr("pyneurodesk.api.resolve_ccvm_binary_path", lambda: ccvm_path)
    monkeypatch.setattr(
        "pyneurodesk.api.subprocess.Popen", lambda *args, **kwargs: FakeProc()
    )

    with pytest.raises(RuntimeError) as excinfo:
        api.start_daemon_for_cache_dir(cache_root)

    message = str(excinfo.value)
    assert f"ccvm failed to start from {ccvm_path}" in message
    assert "listen on bad: bind failed" in message
    assert f"See daemon log at {cache_root / 'ccvm-python.log'}" in message


def test_start_default_daemon_feeds_watchdog_for_existing_daemon(
    monkeypatch, tmp_path: Path
) -> None:
    cache_root = tmp_path / "cache"
    cache_root.mkdir(parents=True)
    (cache_root / "ccvm.json").write_text('{"addr":"127.0.0.1:3456"}')
    calls: list[tuple[str, object]] = []

    monkeypatch.setattr("pyneurodesk.api.default_cache_root", lambda: cache_root)
    monkeypatch.setattr(
        "pyneurodesk.api._health_check",
        lambda base_url: calls.append(("health", base_url)) or True,
    )
    monkeypatch.setattr(
        "pyneurodesk.api._supports_vm_start",
        lambda base_url: calls.append(("supports", base_url)) or True,
    )
    monkeypatch.setattr(
        "pyneurodesk.api._ensure_daemon_watchdog",
        lambda base_url: calls.append(("watchdog", base_url)),
    )

    state = start_default_daemon()

    assert state.base_url == "http://127.0.0.1:3456"
    assert calls == [
        ("health", "http://127.0.0.1:3456"),
        ("supports", "http://127.0.0.1:3456"),
        ("watchdog", "http://127.0.0.1:3456"),
    ]


def test_start_default_daemon_restarts_incompatible_running_daemon(
    monkeypatch, tmp_path: Path
) -> None:
    cache_root = tmp_path / "cache"
    cache_root.mkdir(parents=True)
    (cache_root / "ccvm.json").write_text('{"addr":"127.0.0.1:3456"}')
    calls: list[tuple[str, object]] = []

    monkeypatch.setattr("pyneurodesk.api.default_cache_root", lambda: cache_root)
    monkeypatch.setattr(
        "pyneurodesk.api._health_check",
        lambda base_url: calls.append(("health", base_url)) or True,
    )
    monkeypatch.setattr(
        "pyneurodesk.api._supports_vm_start",
        lambda base_url: calls.append(("supports", base_url)) or False,
    )
    monkeypatch.setattr(
        "pyneurodesk.api._shutdown_daemon_server",
        lambda base_url: calls.append(("shutdown", base_url)),
    )
    monkeypatch.setattr(
        "pyneurodesk.api.start_daemon_for_cache_dir",
        lambda root: (
            calls.append(("restart", root))
            or DaemonState(addr="127.0.0.1:4567", cache_dir=str(root))
        ),
    )

    state = start_default_daemon()

    assert state.base_url == "http://127.0.0.1:4567"
    assert calls == [
        ("health", "http://127.0.0.1:3456"),
        ("supports", "http://127.0.0.1:3456"),
        ("shutdown", "http://127.0.0.1:3456"),
        ("restart", cache_root),
    ]


def test_resolve_ccvm_binary_path_prefers_packaged_binary(
    monkeypatch, tmp_path: Path
) -> None:
    binary = tmp_path / "site-packages" / "pyneurodesk" / "bin" / "ccvm"
    binary.parent.mkdir(parents=True)
    binary.write_text("")

    monkeypatch.setattr("pyneurodesk.api.bundled_ccvm_path", lambda: binary)
    calls: list[Path] = []
    monkeypatch.setattr(
        "pyneurodesk.api.maybe_refresh_bundled_ccvm", lambda path: calls.append(path)
    )
    monkeypatch.delenv("PYNEURODESK_CCVM", raising=False)
    monkeypatch.delenv("CCX3_CCVM", raising=False)
    monkeypatch.delenv("CCVM_BINARY", raising=False)

    assert resolve_ccvm_binary_path() == binary
    assert calls == [binary]


def test_resolve_ccvm_binary_path_prefers_documented_env_var(
    monkeypatch, tmp_path: Path
) -> None:
    binary = tmp_path / "ccvm"
    binary.write_text("")

    monkeypatch.setenv("PYNEURODESK_CCVM", str(binary))

    assert resolve_ccvm_binary_path() == binary


def test_resolve_ccvm_binary_path_falls_back_to_project_binary(
    monkeypatch, tmp_path: Path
) -> None:
    bundle_root = tmp_path / "pyneurodesk"
    binary = bundle_root / "bin" / "ccvm"
    binary.parent.mkdir(parents=True)
    binary.write_text("")
    monkeypatch.setattr("pyneurodesk.api.bundled_ccvm_path", lambda: None)
    monkeypatch.setattr("pyneurodesk.api.pyneurodesk_root", lambda: bundle_root)
    monkeypatch.setattr("pyneurodesk.api.maybe_refresh_bundled_ccvm", lambda path: None)
    monkeypatch.delenv("PYNEURODESK_CCVM", raising=False)
    monkeypatch.delenv("CCX3_CCVM", raising=False)
    monkeypatch.delenv("CCVM_BINARY", raising=False)

    assert resolve_ccvm_binary_path() == binary


def test_resolve_ccvm_binary_path_rebuilds_stale_bundled_binary(
    monkeypatch, tmp_path: Path
) -> None:
    bundle_root = tmp_path / "local" / "pyneurodesk"
    binary = bundle_root / "bin" / "ccvm"
    binary.parent.mkdir(parents=True)
    binary.write_text("old")

    calls: list[Path] = []

    monkeypatch.setattr("pyneurodesk.api.bundled_ccvm_path", lambda: None)
    monkeypatch.setattr("pyneurodesk.api.pyneurodesk_root", lambda: bundle_root)
    monkeypatch.setattr(
        "pyneurodesk.api.maybe_refresh_bundled_ccvm", lambda path: calls.append(path)
    )
    monkeypatch.delenv("PYNEURODESK_CCVM", raising=False)
    monkeypatch.delenv("CCX3_CCVM", raising=False)
    monkeypatch.delenv("CCVM_BINARY", raising=False)

    assert resolve_ccvm_binary_path() == binary
    assert calls == [binary]


def test_sign_ccvm_binary_on_darwin_uses_entitlements(monkeypatch, tmp_path: Path) -> None:
    root = tmp_path / "repo"
    binary = tmp_path / "ccvm"
    entitlements = root / "tools" / "entitlements.xml"
    entitlements.parent.mkdir(parents=True)
    entitlements.write_text("<plist/>")
    binary.write_text("")
    calls: list[list[str]] = []

    def fake_run(args: list[str], **kwargs: object) -> object:
        calls.append(args)
        return SimpleNamespace(returncode=0, stdout="", stderr="")

    monkeypatch.setattr(api.sys, "platform", "darwin")
    monkeypatch.setattr(api.subprocess, "run", fake_run)

    api._sign_ccvm_binary_if_needed(binary, root)

    assert calls == [
        [
            "codesign",
            "-f",
            "-s",
            "-",
            "--entitlements",
            str(entitlements),
            str(binary),
        ]
    ]


def test_create_container_cache_dir_is_isolated(monkeypatch, tmp_path: Path) -> None:
    monkeypatch.setattr(
        "pyneurodesk.api.default_cache_root", lambda: tmp_path / "cache"
    )

    one = create_container_cache_dir()
    two = create_container_cache_dir()

    assert one != two
    assert one.parent == two.parent == (tmp_path / "cache" / "pyneurodesk-daemons")


def test_container_starts_shared_daemon_and_boots_image(monkeypatch) -> None:
    calls: list[tuple[str, object]] = []

    daemon = DaemonState(addr="127.0.0.1:4001", cache_dir="/tmp/daemon-a")

    def fake_start_default_daemon() -> DaemonState:
        calls.append(("start_daemon", None))
        return daemon

    class FakeClient:
        def __init__(self, base_url: str) -> None:
            calls.append(("connect", base_url))
            self._client = SimpleNamespace(base_url=base_url)

        def cvmfs_list(self, source: object) -> object:
            payload = source.to_payload()
            calls.append(("cvmfs_list", payload["path"]))
            if payload["path"] == "/containers/niimath":
                return SimpleNamespace(entries=[])
            if payload["path"] == "/containers":
                return SimpleNamespace(
                    entries=[
                        SimpleNamespace(
                            name="niimath_1.0.20250804_20251016",
                            path="/containers/niimath_1.0.20250804_20251016",
                            kind="directory",
                        )
                    ]
                )
            if payload["path"] == "/containers/niimath_1.0.20250804_20251016":
                return SimpleNamespace(
                    entries=[
                        SimpleNamespace(kind="file", name="commands.txt"),
                        SimpleNamespace(kind="file", name="niimath"),
                    ]
                )
            raise AssertionError(payload["path"])

        def cvmfs_read(self, request: object) -> object:
            calls.append(("cvmfs_read", request.path))
            if request.path.endswith("/commands.txt"):
                return SimpleNamespace(
                    data=base64.b64encode(b"niimath\n").decode().encode()
                )
            raise AssertionError(request.path)

        def get_image(self, image: str) -> Optional[object]:
            calls.append(("get_image", image))
            return None

        def import_image(self, image: str, request: object) -> object:
            calls.append(("import_image", image))
            return SimpleNamespace(name=image, status="downloaded")

        def instance_status(self) -> object:
            calls.append(("instance_status", None))
            return SimpleNamespace(status="stopped", image=None)

        def download_kernel(self) -> object:
            calls.append(("download_kernel", None))
            return SimpleNamespace(status="downloaded")

        def prepare_image_emulator(self, image: str) -> object:
            calls.append(("prepare_image_emulator", image))
            return SimpleNamespace(status="downloaded", path="/tmp/qemu", required=True)

        def prepare_image_metadata(self, image: str) -> object:
            calls.append(("prepare_image_metadata", image))
            return SimpleNamespace(status="prepared", architecture="amd64")

        def create_instance(self, image: str, *, dmesg: bool = False) -> object:
            calls.append(("create_instance", (image, dmesg)))
            return SimpleNamespace(status="running", image=image)

        def run(
            self,
            image: str,
            command: list[str],
            *,
            shares: list[object] = (),
            env: tuple[str, ...] = (),
        ) -> object:
            calls.append(
                (
                    "run",
                    (
                        image,
                        tuple(command),
                        tuple(
                            (share.source, share.mount, share.writable)
                            for share in shares
                        ),
                        tuple(env),
                    ),
                )
            )
            return SimpleNamespace(exit_code=0, output="ok\n")

        def close(self) -> None:
            calls.append(("close_client", None))

    monkeypatch.setattr(
        "pyneurodesk.api.start_default_daemon", fake_start_default_daemon
    )
    monkeypatch.setattr("pyneurodesk.api.PyNeurodeskClient", FakeClient)

    container = nd.container("niimath")
    try:
        out = container.niimath()
    finally:
        container.close()

    assert out == "ok\n"
    assert calls[:5] == [
        ("start_daemon", None),
        ("connect", "http://127.0.0.1:4001"),
        ("cvmfs_list", "/containers/niimath"),
        ("cvmfs_list", "/containers"),
        ("get_image", "niimath"),
    ]
    assert ("import_image", "niimath") in calls
    assert ("instance_status", None) in calls
    assert ("download_kernel", None) in calls
    assert ("prepare_image_emulator", "niimath") in calls
    assert ("prepare_image_metadata", "niimath") in calls
    assert ("create_instance", ("niimath", False)) in calls
    assert ("run", ("niimath", ("niimath",), (), ())) in calls
    assert container.owns_daemon is False


def test_share_dir_arguments_are_translated_into_guest_paths(
    monkeypatch, tmp_path: Path
) -> None:
    daemon = DaemonState(addr="127.0.0.1:4001", cache_dir="/tmp/daemon-a")
    calls: list[tuple[str, object]] = []

    def fake_start_default_daemon() -> DaemonState:
        return daemon

    class FakeClient:
        def __init__(self, base_url: str) -> None:
            self._client = SimpleNamespace(base_url=base_url)

        def cvmfs_list(self, source: object) -> object:
            payload = source.to_payload()
            if payload["path"] == "/containers/niimath":
                return SimpleNamespace(entries=[])
            if payload["path"] == "/containers":
                return SimpleNamespace(
                    entries=[
                        SimpleNamespace(
                            name="niimath_1.0.20250804_20251016",
                            path="/containers/niimath_1.0.20250804_20251016",
                            kind="directory",
                        )
                    ]
                )
            if payload["path"] == "/containers/niimath_1.0.20250804_20251016":
                return SimpleNamespace(
                    entries=[
                        SimpleNamespace(kind="file", name="commands.txt"),
                        SimpleNamespace(kind="file", name="niimath"),
                    ]
                )
            raise AssertionError(payload["path"])

        def cvmfs_read(self, request: object) -> object:
            if request.path.endswith("/commands.txt"):
                return SimpleNamespace(
                    data=base64.b64encode(b"niimath\n").decode().encode()
                )
            raise AssertionError(request.path)

        def get_image(self, image: str) -> Optional[object]:
            return SimpleNamespace(name=image, status="downloaded")

        def import_image(self, image: str, request: object) -> object:
            raise AssertionError("image import should not be needed")

        def instance_status(self) -> object:
            return SimpleNamespace(status="running", image="niimath")

        def create_instance(self, image: str) -> object:
            raise AssertionError("instance creation should not be needed")

        def run(
            self,
            image: str,
            command: list[str],
            *,
            shares: list[object] = (),
            env: tuple[str, ...] = (),
        ) -> object:
            calls.append(
                (
                    "run",
                    {
                        "image": image,
                        "command": tuple(command),
                        "shares": tuple(
                            (share.source, share.mount, share.writable)
                            for share in shares
                        ),
                        "env": tuple(env),
                    },
                )
            )
            return SimpleNamespace(exit_code=0, output="shared\n")

        def shutdown_instance(self) -> None:
            return None

        def close(self) -> None:
            return None

    monkeypatch.setattr(
        "pyneurodesk.api.start_default_daemon", fake_start_default_daemon
    )
    monkeypatch.setattr("pyneurodesk.api.PyNeurodeskClient", FakeClient)

    share_root = tmp_path / "share"
    share_root.mkdir()
    (share_root / "hello.txt").write_text("hello\n")

    container = nd.container("niimath")
    try:
        shared = nd.share_dir(share_root)
        out = container.shell("cat", shared / "hello.txt")
    finally:
        container.close()

    assert out == "shared\n"
    run_call = calls[-1][1]
    assert run_call["command"][0] == "cat"
    assert run_call["command"][1].endswith("/hello.txt")
    assert run_call["command"][1].startswith("/.share/")
    assert run_call["shares"] == (
        (str(share_root), run_call["command"][1].rsplit("/", 1)[0], False),
    )


def test_owned_container_recovers_when_daemon_connection_is_refused(
    monkeypatch,
) -> None:
    daemon_a = DaemonState(addr="127.0.0.1:4001", cache_dir="/tmp/daemon-a")
    daemon_b = DaemonState(addr="127.0.0.1:4002", cache_dir="/tmp/daemon-a")
    calls: list[tuple[str, object]] = []

    class FakeClient:
        def __init__(self, base_url: str) -> None:
            calls.append(("connect", base_url))
            self.base_url = base_url
            self._client = SimpleNamespace(base_url=base_url)
            self._run_calls = 0

        def run(
            self,
            image: str,
            command: list[str],
            *,
            shares: list[object] = (),
            env: tuple[str, ...] = (),
        ) -> object:
            self._run_calls += 1
            calls.append(("run", (self.base_url, image, tuple(command), tuple(env))))
            if self.base_url.endswith(":4001"):
                raise httpx.ConnectError("connection refused")
            return SimpleNamespace(exit_code=0, output="ok\n")

        def cvmfs_list(self, source: object) -> object:
            payload = source.to_payload()
            calls.append(("cvmfs_list", payload["path"]))
            return SimpleNamespace(entries=[])

        def ensure_image(self, reference: object) -> None:
            calls.append(("ensure_image", reference.image))

        def ensure_instance(self, image: str) -> None:
            calls.append(("ensure_instance", image))

        def shutdown_instance(self) -> None:
            calls.append(("shutdown_instance", self.base_url))

        def close(self) -> None:
            calls.append(("close_client", self.base_url))

    def fake_health_check(base_url: str) -> bool:
        return base_url.endswith(":4002")

    def fake_restart(cache_root: Path) -> DaemonState:
        calls.append(("restart", str(cache_root)))
        return daemon_b

    monkeypatch.setattr("pyneurodesk.api.PyNeurodeskClient", FakeClient)
    monkeypatch.setattr("pyneurodesk.api._health_check", fake_health_check)
    monkeypatch.setattr("pyneurodesk.api.start_daemon_for_cache_dir", fake_restart)

    container = nd.NeurodeskContainer(
        FakeClient(daemon_a.base_url),
        nd.ContainerReference(
            name="niimath",
            image="niimath",
            source=nd.ImageSource(type="cvmfs", path="/containers/niimath"),
            cache_dir=daemon_a.cache_dir,
        ),
        owned_daemon=daemon_a,
    )
    try:
        out = container.niimath()
    finally:
        container.close()

    assert out == "ok\n"
    assert ("restart", str(Path(daemon_a.cache_dir))) in calls
    assert ("ensure_image", "niimath") in calls
    assert ("ensure_instance", "niimath") in calls
    assert ("run", ("http://127.0.0.1:4002", "niimath", ("niimath",), ())) in calls


def test_container_resolution_prefers_direct_directory_probe() -> None:
    seen_paths: list[str] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read().decode() or "{}"
        if request.method == "POST" and request.url.path == "/cvmfs/list":
            payload = json.loads(body)
            seen_paths.append(payload["path"])
            if payload["path"] == "/containers/niimath":
                return httpx.Response(200, json={"entries": []})
            if payload["path"] == "/containers":
                return httpx.Response(
                    200,
                    json={
                        "entries": [
                            {
                                "name": "niimath",
                                "path": "/containers/niimath",
                                "kind": "directory",
                            }
                        ]
                    },
                )
            raise AssertionError(f"unexpected CVMFS path probe: {payload['path']}")
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "downloaded",
                    "source_kind": "cvmfs",
                },
            )
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(200, json={"status": "running", "image": "niimath"})
        raise AssertionError(f"unexpected request: {request.method} {request.url.path}")

    client = make_client(httpx.MockTransport(handler))
    try:
        container = nd.container("niimath", client=client)
    finally:
        client.close()

    assert container.path == "/containers/niimath"
    assert seen_paths == ["/containers/niimath", "/containers"]


def test_container_resolution_prefers_latest_version_from_search() -> None:
    seen_paths: list[str] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read().decode() or "{}"
        if request.method == "POST" and request.url.path == "/cvmfs/list":
            payload = json.loads(body)
            seen_paths.append(payload["path"])
            if payload["path"] == "/containers/niimath":
                return httpx.Response(200, json={"entries": []})
            if payload["path"] == "/containers":
                return httpx.Response(
                    200,
                    json={
                        "entries": [
                            {
                                "name": "niimath_1.0.20250529_20251001",
                                "path": "/containers/niimath_1.0.20250529_20251001",
                                "kind": "directory",
                            },
                            {
                                "name": "niimath_1.0.20250804_20251016",
                                "path": "/containers/niimath_1.0.20250804_20251016",
                                "kind": "directory",
                            },
                        ]
                    },
                )
            raise AssertionError(f"unexpected CVMFS path probe: {payload['path']}")
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "downloaded",
                    "source_kind": "cvmfs",
                },
            )
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(200, json={"status": "running", "image": "niimath"})
        raise AssertionError(f"unexpected request: {request.method} {request.url.path}")

    client = make_client(httpx.MockTransport(handler))
    try:
        container = nd.container("niimath", client=client)
    finally:
        client.close()

    assert container.path == "/containers/niimath_1.0.20250804_20251016"
    assert seen_paths == ["/containers/niimath", "/containers"]


def test_search_returns_versions_for_container_name() -> None:
    seen_paths: list[str] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read().decode() or "{}"
        if request.method != "POST" or request.url.path != "/cvmfs/list":
            raise AssertionError(
                f"unexpected request: {request.method} {request.url.path}"
            )
        payload = json.loads(body)
        seen_paths.append(payload["path"])
        if payload["path"] == "/containers/niimath":
            return httpx.Response(200, json={"entries": []})
        if payload["path"] == "/containers":
            return httpx.Response(
                200,
                json={
                    "entries": [
                        {
                            "name": "niimath_1.0.20250529_20251001",
                            "path": "/containers/niimath_1.0.20250529_20251001",
                            "kind": "directory",
                        },
                        {
                            "name": "niimath_1.0.20250804_20251016",
                            "path": "/containers/niimath_1.0.20250804_20251016",
                            "kind": "directory",
                        },
                        {
                            "name": "othertool_9.9.9_20250101",
                            "path": "/containers/othertool_9.9.9_20250101",
                            "kind": "directory",
                        },
                    ]
                },
            )
        raise AssertionError(f"unexpected path: {payload['path']}")

    client = make_client(httpx.MockTransport(handler))
    try:
        versions = nd.search("niimath", client=client)
    finally:
        client.close()

    assert versions == ["1.0.20250529_20251001", "1.0.20250804_20251016"]
    assert seen_paths == ["/containers/niimath", "/containers"]


def test_search_handles_missing_direct_directory() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read().decode() or "{}"
        payload = json.loads(body)
        if request.method != "POST" or request.url.path != "/cvmfs/list":
            raise AssertionError(
                f"unexpected request: {request.method} {request.url.path}"
            )
        if payload["path"] == "/containers/niimath":
            return httpx.Response(404, json={"error": "not found"})
        if payload["path"] == "/containers":
            return httpx.Response(
                200,
                json={
                    "entries": [
                        {
                            "name": "niimath_1.0.20250804_20251016",
                            "path": "/containers/niimath_1.0.20250804_20251016",
                            "kind": "directory",
                        }
                    ]
                },
            )
        raise AssertionError(f"unexpected path: {payload['path']}")

    client = make_client(httpx.MockTransport(handler))
    try:
        versions = nd.search("niimath", client=client)
    finally:
        client.close()

    assert versions == ["1.0.20250804_20251016"]


def test_container_debug_boot_prints_live_stream_and_error(
    monkeypatch, capsys: pytest.CaptureFixture[str]
) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read().decode() or "{}"
        if request.method == "POST" and request.url.path == "/cvmfs/list":
            payload = json.loads(body)
            if payload["path"] == "/containers/niimath":
                return httpx.Response(200, json={"entries": []})
            if payload["path"] == "/containers":
                return httpx.Response(
                    200,
                    json={
                        "entries": [
                            {
                                "name": "niimath",
                                "path": "/containers/niimath",
                                "kind": "directory",
                            }
                        ]
                    },
                )
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "downloaded",
                    "source_kind": "cvmfs",
                },
            )
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(200, json={"status": "stopped"})
        if request.method == "POST" and request.url.path == "/kernel/download":
            return httpx.Response(200, json={"status": "downloaded"})
        if (
            request.method == "POST"
            and request.url.path == "/image/niimath/qemu/download"
        ):
            return httpx.Response(
                200,
                json={"status": "downloaded", "path": "/tmp/qemu", "required": True},
            )
        if request.method == "POST" and request.url.path == "/image/niimath/metadata":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "prepared",
                    "source_kind": "cvmfs",
                    "architecture": "amd64",
                },
            )
        if request.method == "POST" and request.url.path == "/vm":
            payload = json.loads(body)
            assert payload["dmesg"] is True
            assert request.url.params.get("stream") == "1"
            return httpx.Response(
                200,
                text=(
                    '{"kind":"status","message":"starting VM for niimath"}\n'
                    '{"kind":"serial","data":"boot log line\\n"}\n'
                    '{"kind":"error","error":"panic: guest stuck"}\n'
                ),
                headers={"content-type": "application/x-ndjson"},
            )
        raise AssertionError(f"unexpected request: {request.method} {request.url.path}")

    client = make_client(httpx.MockTransport(handler))
    with pytest.raises(RuntimeError, match="panic: guest stuck"):
        nd.container("niimath", client=client, debug=True)

    captured = capsys.readouterr()
    assert "ccx3 boot: starting VM for niimath" in captured.out
    assert "boot log line" in captured.out


def test_create_progress_reporter_disabled_is_noop() -> None:
    reporter = create_progress_reporter(enabled=False, total_steps=9)
    reporter.update(1, "hello")
    reporter.close("done")


def test_download_kernel_stream_parses_progress_events() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.params.get("stream") == "1"
        assert request.headers["Accept"] == "application/x-ndjson"
        return httpx.Response(
            200,
            headers={"content-type": "application/x-ndjson"},
            content=(
                b'{"status":"downloading","artifact":"linux-virt.apk","bytes_downloaded":1024,'
                b'"bytes_total":4096,"rate_bytes_per_second":512,"eta_seconds":6}\n'
                b'{"status":"downloaded","artifact":"linux-virt.apk","bytes_downloaded":4096,'
                b'"bytes_total":4096,"rate_bytes_per_second":1024}\n'
            ),
        )

    client = make_client(httpx.MockTransport(handler))
    try:
        events = list(client.download_kernel_stream())
    finally:
        client.close()

    assert len(events) == 2
    assert events[0].status == "downloading"
    assert events[0].artifact == "linux-virt.apk"
    assert events[0].bytes_downloaded == 1024
    assert events[0].bytes_total == 4096
    assert events[0].eta_seconds == 6
    assert events[1].status == "downloaded"
    assert events[1].bytes_downloaded == 4096


def test_import_image_stream_parses_progress_events() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.path == "/image/niimath"
        assert request.url.params.get("stream") == "1"
        assert request.headers["Accept"] == "application/x-ndjson"
        return httpx.Response(
            200,
            headers={"content-type": "application/x-ndjson"},
            content=(
                b'{"status":"indexing","artifact":"niimath","blob":"abc123"}\n'
                b'{"status":"downloading","artifact":"niimath","blob":"rootfs.contents","bytes_downloaded":1024,'
                b'"bytes_total":4096,"files_downloaded":2,"files_total":8,'
                b'"rate_bytes_per_second":512,"eta_seconds":6}\n'
                b'{"status":"downloaded","artifact":"niimath","blob":"rootfs.contents","bytes_downloaded":4096,'
                b'"bytes_total":4096,"files_downloaded":8,"files_total":8,"progress":1}\n'
            ),
        )

    client = make_client(httpx.MockTransport(handler))
    try:
        events = list(
            client.import_image_stream(
                "niimath",
                ImportImageRequest.from_cvmfs_container(
                    mirror="http://cvmfs.neurodesk.org",
                    repo="neurodesk.ardc.edu.au",
                    path="/containers/niimath_1.0.20250804_20251016",
                    prefetch=True,
                    prefetch_workers=4,
                ),
            )
        )
    finally:
        client.close()

    assert [event.status for event in events] == [
        "indexing",
        "downloading",
        "downloaded",
    ]
    assert events[0].blob == "abc123"
    assert events[1].bytes_downloaded == 1024
    assert events[1].files_downloaded == 2
    assert events[1].files_total == 8
    assert events[1].eta_seconds == 6
    assert events[2].progress == 1


def test_stream_progress_reporter_redraws_on_single_terminal_line() -> None:
    stream = io.StringIO()
    reporter = StreamProgressReporter(total_steps=4, stream=stream)

    reporter.update(1, "Preparing")
    reporter.update(2, "Downloading")
    reporter.close("Ready")

    output = stream.getvalue()
    assert output.count("\n") == 1
    assert output.count("\r\033[2K") == 3
    assert "[######------------------] 1/4 Preparing" in output
    assert "[############------------] 2/4 Downloading" in output
    assert output.endswith("\r\033[2KReady\n")


def test_container_progress_reports_required_downloads(
    monkeypatch, capsys: pytest.CaptureFixture[str]
) -> None:
    daemon = DaemonState(addr="127.0.0.1:4001", cache_dir="/tmp/daemon-a")

    def fake_start_default_daemon() -> DaemonState:
        return daemon

    class FakeClient:
        def __init__(self, base_url: str) -> None:
            self._client = SimpleNamespace(base_url=base_url)

        def cvmfs_list(self, source: object) -> object:
            payload = source.to_payload()
            if payload["path"] == "/containers/niimath":
                return SimpleNamespace(entries=[])
            if payload["path"] == "/containers":
                return SimpleNamespace(
                    entries=[
                        SimpleNamespace(
                            name="niimath_1.0.20250804_20251016",
                            path="/containers/niimath_1.0.20250804_20251016",
                            kind="directory",
                        )
                    ]
                )
            raise AssertionError(payload["path"])

        def get_image(self, image: str) -> Optional[object]:
            return None

        def import_image(self, image: str, request: object) -> object:
            return SimpleNamespace(name=image, status="downloaded")

        def instance_status(self) -> object:
            return SimpleNamespace(status="stopped", image=None)

        def download_kernel(self) -> object:
            return SimpleNamespace(status="downloaded", source="/cache/vmlinuz")

        def prepare_image_emulator(self, image: str) -> object:
            return SimpleNamespace(
                status="downloaded", path="/tmp/qemu-system-x86_64", required=True
            )

        def prepare_image_metadata(self, image: str) -> object:
            return SimpleNamespace(status="prepared", architecture="amd64")

        def create_instance(self, image: str, *, dmesg: bool = False) -> object:
            return SimpleNamespace(status="running", image=image)

        def shutdown_instance(self) -> object:
            return SimpleNamespace(status="stopped", image=None)

        def close(self) -> None:
            return None

    monkeypatch.setattr("pyneurodesk.api._supports_notebook_display", lambda: False)
    monkeypatch.setattr(
        "pyneurodesk.api.start_default_daemon", fake_start_default_daemon
    )
    monkeypatch.setattr("pyneurodesk.api.PyNeurodeskClient", FakeClient)

    container = nd.container("niimath")
    container.close()

    captured = capsys.readouterr()
    assert "Downloading required file 1/2: Linux kernel" in captured.out
    assert "Downloaded required file 1/2: vmlinuz" in captured.out
    assert "Downloading required file 2/2: emulator" in captured.out
    assert "Downloaded required file 2/2: qemu-system-x86_64" in captured.out


def test_container_uses_streaming_image_import(
    monkeypatch, capsys: pytest.CaptureFixture[str]
) -> None:
    daemon = DaemonState(addr="127.0.0.1:4001", cache_dir="/tmp/daemon-a")
    calls: list[tuple[str, object]] = []

    def fake_start_default_daemon() -> DaemonState:
        return daemon

    class FakeClient:
        def __init__(self, base_url: str) -> None:
            self._client = SimpleNamespace(base_url=base_url)

        def cvmfs_list(self, source: object) -> object:
            payload = source.to_payload()
            if payload["path"] == "/containers/niimath":
                return SimpleNamespace(entries=[])
            if payload["path"] == "/containers":
                return SimpleNamespace(
                    entries=[
                        SimpleNamespace(
                            name="niimath_1.0.20250804_20251016",
                            path="/containers/niimath_1.0.20250804_20251016",
                            kind="directory",
                        )
                    ]
                )
            raise AssertionError(payload["path"])

        def get_image(self, image: str) -> Optional[object]:
            return None

        def import_image_stream(self, image: str, request: object):
            calls.append(("import_image_stream", image))
            yield SimpleNamespace(
                status="downloading",
                artifact=image,
                blob="rootfs.index.json",
                bytes_downloaded=1024,
                bytes_total=2048,
                rate_bytes_per_second=512,
                eta_seconds=2,
            )
            yield SimpleNamespace(
                status="downloaded",
                artifact=image,
                blob="rootfs.index.json",
                bytes_downloaded=2048,
                bytes_total=2048,
                rate_bytes_per_second=1024,
                eta_seconds=0,
            )

        def import_image(self, image: str, request: object) -> object:
            raise AssertionError(
                "blocking import_image should not be used when streaming is available"
            )

        def instance_status(self) -> object:
            return SimpleNamespace(status="stopped", image=None)

        def download_kernel(self) -> object:
            return SimpleNamespace(status="downloaded", source="/cache/vmlinuz")

        def prepare_image_emulator(self, image: str) -> object:
            return SimpleNamespace(
                status="downloaded", path="/tmp/qemu-system-x86_64", required=True
            )

        def prepare_image_metadata(self, image: str) -> object:
            return SimpleNamespace(status="prepared", architecture="amd64")

        def create_instance(self, image: str, *, dmesg: bool = False) -> object:
            return SimpleNamespace(status="running", image=image)

        def close(self) -> None:
            return None

    monkeypatch.setattr("pyneurodesk.api._supports_notebook_display", lambda: False)
    monkeypatch.setattr(
        "pyneurodesk.api.start_default_daemon", fake_start_default_daemon
    )
    monkeypatch.setattr("pyneurodesk.api.PyNeurodeskClient", FakeClient)

    container = nd.container("niimath")
    container.close()

    captured = capsys.readouterr()
    assert calls == [("import_image_stream", "niimath")]
    assert "Importing niimath | rootfs.index.json | 1.0 KB/2.0 KB" in captured.out
    assert "Imported niimath | rootfs.index.json | 2.0 KB/2.0 KB" in captured.out


def test_container_progress_reports_rate_and_eta_from_stream(
    monkeypatch, capsys: pytest.CaptureFixture[str]
) -> None:
    daemon = DaemonState(addr="127.0.0.1:4001", cache_dir="/tmp/daemon-a")

    def fake_start_default_daemon() -> DaemonState:
        return daemon

    class FakeClient:
        def __init__(self, base_url: str) -> None:
            self._client = SimpleNamespace(base_url=base_url)

        def cvmfs_list(self, source: object) -> object:
            payload = source.to_payload()
            if payload["path"] == "/containers/niimath":
                return SimpleNamespace(entries=[])
            if payload["path"] == "/containers":
                return SimpleNamespace(
                    entries=[
                        SimpleNamespace(
                            name="niimath_1.0.20250804_20251016",
                            path="/containers/niimath_1.0.20250804_20251016",
                            kind="directory",
                        )
                    ]
                )
            raise AssertionError(payload["path"])

        def get_image(self, image: str) -> Optional[object]:
            return None

        def import_image(self, image: str, request: object) -> object:
            return SimpleNamespace(name=image, status="downloaded")

        def instance_status(self) -> object:
            return SimpleNamespace(status="stopped", image=None)

        def download_kernel_stream(self):
            yield SimpleNamespace(
                status="downloading",
                artifact="linux-virt.apk",
                bytes_downloaded=1024 * 1024,
                bytes_total=4 * 1024 * 1024,
                rate_bytes_per_second=512 * 1024,
                eta_seconds=6,
            )
            yield SimpleNamespace(
                status="downloaded",
                artifact="linux-virt.apk",
                bytes_downloaded=4 * 1024 * 1024,
                bytes_total=4 * 1024 * 1024,
                rate_bytes_per_second=1024 * 1024,
                eta_seconds=0,
            )

        def prepare_image_emulator_stream(self, image: str):
            yield SimpleNamespace(
                status="downloading",
                artifact="qemu-x86_64.apk",
                bytes_downloaded=2 * 1024 * 1024,
                bytes_total=8 * 1024 * 1024,
                rate_bytes_per_second=1024 * 1024,
                eta_seconds=6,
            )
            yield SimpleNamespace(
                status="downloaded",
                artifact="qemu-system-x86_64",
                bytes_downloaded=8 * 1024 * 1024,
                bytes_total=8 * 1024 * 1024,
                rate_bytes_per_second=2 * 1024 * 1024,
                eta_seconds=0,
            )

        def prepare_image_metadata(self, image: str) -> object:
            return SimpleNamespace(status="prepared", architecture="amd64")

        def create_instance(self, image: str, *, dmesg: bool = False) -> object:
            return SimpleNamespace(status="running", image=image)

        def shutdown_instance(self) -> object:
            return SimpleNamespace(status="stopped", image=None)

        def close(self) -> None:
            return None

    monkeypatch.setattr("pyneurodesk.api._supports_notebook_display", lambda: False)
    monkeypatch.setattr(
        "pyneurodesk.api.start_default_daemon", fake_start_default_daemon
    )
    monkeypatch.setattr("pyneurodesk.api.PyNeurodeskClient", FakeClient)

    container = nd.container("niimath")
    container.close()

    captured = capsys.readouterr()
    assert "1.0 MB/4.0 MB" in captured.out
    assert "512.0 KB/s" in captured.out
    assert "ETA 6s" in captured.out
    assert "2.0 MB/8.0 MB" in captured.out
    assert "1.0 MB/s" in captured.out


def test_resolve_release_index_dir_prefers_env(monkeypatch, tmp_path: Path) -> None:
    releases_dir = tmp_path / "releases"
    releases_dir.mkdir()
    monkeypatch.setenv("PYNEURODESK_RELEASES_DIR", str(releases_dir))

    assert resolve_release_index_dir() == releases_dir


def test_search_uses_local_release_metadata(monkeypatch, tmp_path: Path) -> None:
    releases_dir = tmp_path / "releases"
    (releases_dir / "niimath").mkdir(parents=True)
    (releases_dir / "niimath" / "1.0.0.json").write_text(
        '{"apps":{"niimath 1.0.0":{"version":"20250617","exec":""}}}'
    )
    (releases_dir / "niimath" / "1.0.20250804.json").write_text(
        '{"apps":{"niimath 1.0.20250804":{"version":"20251016","exec":""}}}'
    )
    monkeypatch.setattr(
        "pyneurodesk.api.resolve_release_index_dir", lambda: releases_dir
    )

    assert nd.search("niimath") == ["1.0.0", "1.0.20250804"]


def test_search_uses_remote_release_metadata(monkeypatch) -> None:
    monkeypatch.setattr(
        "pyneurodesk.api._search_remote_release_versions",
        lambda name: (
            {"1.0.0": "20250617", "1.0.20250804": "20251016"}
            if name == "niimath"
            else {}
        ),
    )

    assert nd.search("niimath") == ["1.0.0", "1.0.20250804"]


def test_remote_release_search_uses_fresh_cache(monkeypatch, tmp_path: Path) -> None:
    monkeypatch.setattr(
        "pyneurodesk.api.default_cache_root", lambda: tmp_path / "cache"
    )
    monkeypatch.setattr(
        "pyneurodesk.api.resolve_release_cache_ttl_seconds", lambda: 60.0
    )
    monkeypatch.setattr(
        "pyneurodesk.api.fetch_remote_release_versions",
        lambda api_base, name: pytest.fail("network used"),
    )
    api.write_remote_release_cache(
        "https://api.example/releases", "niimath", {"1.0.0": "20250617"}
    )

    assert api.search_remote_release_versions(
        "https://api.example/releases", "niimath"
    ) == {"1.0.0": "20250617"}


def test_remote_release_search_refreshes_stale_cache(
    monkeypatch, tmp_path: Path
) -> None:
    monkeypatch.setattr(
        "pyneurodesk.api.default_cache_root", lambda: tmp_path / "cache"
    )
    monkeypatch.setattr(
        "pyneurodesk.api.resolve_release_cache_ttl_seconds", lambda: 60.0
    )
    monkeypatch.setattr("pyneurodesk.api.time.time", lambda: 1_000.0)
    api.write_remote_release_cache(
        "https://api.example/releases", "niimath", {"1.0.0": "old"}
    )
    monkeypatch.setattr("pyneurodesk.api.time.time", lambda: 1_061.0)
    monkeypatch.setattr(
        "pyneurodesk.api.fetch_remote_release_versions",
        lambda api_base, name: {"1.0.1": "new"},
    )
    assert api.search_remote_release_versions(
        "https://api.example/releases", "niimath"
    ) == {"1.0.1": "new"}
    assert api.read_remote_release_cache(
        "https://api.example/releases", "niimath", 60.0
    ) == {"1.0.1": "new"}


def test_remote_release_search_can_disable_cache(monkeypatch, tmp_path: Path) -> None:
    calls: list[tuple[str, str]] = []
    monkeypatch.setattr(
        "pyneurodesk.api.default_cache_root", lambda: tmp_path / "cache"
    )
    monkeypatch.setattr(
        "pyneurodesk.api.resolve_release_cache_ttl_seconds", lambda: 0.0
    )
    api.write_remote_release_cache(
        "https://api.example/releases", "niimath", {"1.0.0": "cached"}
    )

    def fetch(api_base: str, name: str) -> dict[str, str]:
        calls.append((api_base, name))
        return {"1.0.1": "fresh"}

    monkeypatch.setattr("pyneurodesk.api.fetch_remote_release_versions", fetch)
    assert api.search_remote_release_versions(
        "https://api.example/releases", "niimath"
    ) == {"1.0.1": "fresh"}
    assert calls == [("https://api.example/releases", "niimath")]


def test_container_uses_local_release_metadata_without_cvmfs_listing(
    monkeypatch, tmp_path: Path
) -> None:
    releases_dir = tmp_path / "releases"
    (releases_dir / "niimath").mkdir(parents=True)
    (releases_dir / "niimath" / "1.0.0.json").write_text(
        '{"apps":{"niimath 1.0.0":{"version":"20250617","exec":""}}}'
    )
    (releases_dir / "niimath" / "1.0.20250804.json").write_text(
        '{"apps":{"niimath 1.0.20250804":{"version":"20251016","exec":""}}}'
    )
    monkeypatch.setattr(
        "pyneurodesk.api.resolve_release_index_dir", lambda: releases_dir
    )

    seen: list[tuple[str, str, Optional[str]]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read().decode() or None
        seen.append((request.method, request.url.path, body))
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "downloaded",
                    "source_kind": "cvmfs",
                },
            )
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(200, json={"status": "stopped"})
        if request.method == "POST" and request.url.path == "/kernel/download":
            return httpx.Response(200, json={"status": "downloaded"})
        if (
            request.method == "POST"
            and request.url.path == "/image/niimath/qemu/download"
        ):
            return httpx.Response(
                200,
                json={"status": "downloaded", "path": "/tmp/qemu", "required": True},
            )
        if request.method == "POST" and request.url.path == "/image/niimath/metadata":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "prepared",
                    "source_kind": "cvmfs",
                    "architecture": "amd64",
                },
            )
        if request.method == "POST" and request.url.path == "/vm":
            return httpx.Response(200, json={"status": "running", "image": "niimath"})
        raise AssertionError(
            f"unexpected request: {request.method} {request.url.path} {body!r}"
        )

    client = make_client(httpx.MockTransport(handler))
    try:
        container = nd.container("niimath", client=client, progress=False)
    finally:
        client.close()

    assert container.path == build_release_container_path(
        "niimath", "1.0.20250804", "20251016"
    )
    assert all(path != "/cvmfs/list" for _, path, _ in seen)


def test_container_uses_remote_release_metadata_without_cvmfs_listing(
    monkeypatch,
) -> None:
    monkeypatch.setattr(
        "pyneurodesk.api._search_remote_release_versions",
        lambda name: (
            {"1.0.0": "20250617", "1.0.20250804": "20251016"}
            if name == "niimath"
            else {}
        ),
    )

    seen: list[tuple[str, str, Optional[str]]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read().decode() or None
        seen.append((request.method, request.url.path, body))
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "downloaded",
                    "source_kind": "cvmfs",
                },
            )
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(200, json={"status": "stopped"})
        if request.method == "POST" and request.url.path == "/kernel/download":
            return httpx.Response(200, json={"status": "downloaded"})
        if (
            request.method == "POST"
            and request.url.path == "/image/niimath/qemu/download"
        ):
            return httpx.Response(
                200,
                json={"status": "downloaded", "path": "/tmp/qemu", "required": True},
            )
        if request.method == "POST" and request.url.path == "/image/niimath/metadata":
            return httpx.Response(
                200,
                json={
                    "name": "niimath",
                    "status": "prepared",
                    "source_kind": "cvmfs",
                    "architecture": "amd64",
                },
            )
        if request.method == "POST" and request.url.path == "/vm":
            return httpx.Response(200, json={"status": "running", "image": "niimath"})
        raise AssertionError(
            f"unexpected request: {request.method} {request.url.path} {body!r}"
        )

    client = make_client(httpx.MockTransport(handler))
    try:
        container = nd.container("niimath", client=client, progress=False)
    finally:
        client.close()

    assert container.path == build_release_container_path(
        "niimath", "1.0.20250804", "20251016"
    )
    assert all(path != "/cvmfs/list" for _, path, _ in seen)


def test_resolve_http_timeout_defaults_to_long_read_window(monkeypatch) -> None:
    monkeypatch.delenv("PYNEURODESK_HTTP_TIMEOUT", raising=False)

    timeout = resolve_http_timeout(None)

    assert timeout.connect == 10.0
    assert timeout.read == 300.0
    assert timeout.write == 300.0
    assert timeout.pool == 10.0


def test_resolve_http_timeout_respects_env(monkeypatch) -> None:
    monkeypatch.setenv("PYNEURODESK_HTTP_TIMEOUT", "90")

    timeout = resolve_http_timeout(None)

    assert timeout.connect == 90.0
    assert timeout.read == 90.0
    assert timeout.write == 90.0
    assert timeout.pool == 90.0


def test_path_join_preserves_absolute_style() -> None:
    assert path_join("/containers", "niimath") == "/containers/niimath"
    assert path_join("/containers/", "/niimath") == "/containers/niimath"
