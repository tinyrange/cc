from __future__ import annotations

import io
import json
import os
from pathlib import Path
from types import SimpleNamespace

import httpx
import pytest

import neurodesk as nd
from pyneurodesk import (
    CVMFSReadRequest,
    CVMFSSource,
    ImportImageRequest,
    PyNeurodeskClient,
    resolve_base_url,
)
from pyneurodesk.api import (
    StreamProgressReporter,
    create_container_cache_dir,
    build_release_container_path,
    create_progress_reporter,
    path_join,
    resolve_ccvm_binary_path,
    resolve_release_index_dir,
    start_container_daemon,
    start_default_daemon,
)
from pyneurodesk.models import DaemonState
from pyneurodesk.client import resolve_http_timeout
from pyneurodesk.client import resolve_boot_timeout


@pytest.fixture(autouse=True)
def disable_local_release_index(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("pyneurodesk.api.resolve_release_index_dir", lambda: None)
    monkeypatch.setattr("pyneurodesk.api._search_remote_release_versions", lambda name: {})


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
            mirror="https://cvmfs.neurodesk.org",
            repo="neurodesk.ardc.edu.au",
            path="/containers/niimath_1.0.20250804_20251016",
        )
    finally:
        client.close()

    assert seen["method"] == "POST"
    assert seen["path"] == "/image/niimath"
    assert seen["json"] == (
        '{"source":{"type":"cvmfs","mirror":"https://cvmfs.neurodesk.org",'
        '"repo":"neurodesk.ardc.edu.au","path":"/containers/niimath_1.0.20250804_20251016"}}'
    )
    assert result.name == "niimath"
    assert result.status == "downloaded"
    assert result.source_kind == "cvmfs"


def test_import_image_request_from_cvmfs_container_serializes_expected_shape() -> None:
    request = ImportImageRequest.from_cvmfs_container(
        mirror="https://cvmfs.neurodesk.org",
        repo="neurodesk.ardc.edu.au",
        path="/containers/niimath_1.0.20250804_20251016",
        cache_dir="/tmp/cvmfs-cache",
    )

    assert request.to_payload() == {
        "source": {
            "type": "cvmfs",
            "mirror": "https://cvmfs.neurodesk.org",
            "repo": "neurodesk.ardc.edu.au",
            "path": "/containers/niimath_1.0.20250804_20251016",
        },
        "cache_dir": "/tmp/cvmfs-cache",
    }


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
                mirror="https://cvmfs.neurodesk.org",
                repo="neurodesk.ardc.edu.au",
                path="/containers",
                cache_dir="/tmp/cvmfs-cache",
            )
        )
    finally:
        client.close()

    assert seen["path"] == "/cvmfs/list"
    assert seen["json"] == (
        '{"mirror":"https://cvmfs.neurodesk.org","repo":"neurodesk.ardc.edu.au",'
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
                mirror="https://cvmfs.neurodesk.org",
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
        '{"mirror":"https://cvmfs.neurodesk.org","repo":"neurodesk.ardc.edu.au",'
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
    assert seen["timeout"]["read"] == 30.0
    assert result.status == "running"


def test_container_cold_start_uses_http_timeout_for_preflight_and_boot_timeout_for_vm() -> None:
    seen: list[tuple[str, float | None]] = []

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
                    json={"entries": [{"name": "niimath", "path": "/containers/niimath", "kind": "directory"}]},
                )
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(200, json={"name": "niimath", "status": "downloaded", "source_kind": "cvmfs"})
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(200, json={"status": "stopped"})
        if request.method == "POST" and request.url.path == "/kernel/download":
            return httpx.Response(200, json={"status": "downloaded"})
        if request.method == "POST" and request.url.path == "/image/niimath/qemu/download":
            return httpx.Response(200, json={"status": "downloaded", "path": "/tmp/qemu", "required": True})
        if request.method == "POST" and request.url.path == "/image/niimath/metadata":
            return httpx.Response(
                200,
                json={"name": "niimath", "status": "prepared", "source_kind": "cvmfs", "architecture": "amd64"},
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
    assert timeouts["/vm"] == 30.0


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
                    json={"entries": [{"name": "niimath", "path": "/containers/niimath", "kind": "directory"}]},
                )
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(200, json={"name": "niimath", "status": "downloaded", "source_kind": "cvmfs"})
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
    assert seen["json"] == '{"image":"niimath","dmesg":true}'
    assert result.status == "running"


def test_create_instance_sends_memory_and_cpu_overrides() -> None:
    seen: dict[str, object] = {}

    def handler(request: httpx.Request) -> httpx.Response:
        seen["path"] = request.url.path
        seen["json"] = request.read().decode()
        return httpx.Response(200, json={"status": "running", "image": "niimath", "memory_mb": 4096, "cpus": 2})

    client = make_client(httpx.MockTransport(handler))
    try:
        result = client.create_instance("niimath", memory_mb=4096, cpus=2)
    finally:
        client.close()

    assert seen["path"] == "/vm"
    assert seen["json"] == '{"image":"niimath","memory_mb":4096,"cpus":2}'
    assert result.memory_mb == 4096
    assert result.cpus == 2


def test_create_instance_stream_yields_boot_events() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.params.get("stream") == "1"
        return httpx.Response(
            200,
            text='{"kind":"status","message":"starting"}\n{"kind":"serial","data":"boot line\\n"}\n{"kind":"ready"}\n',
            headers={"content-type": "application/x-ndjson"},
        )

    client = make_client(httpx.MockTransport(handler))
    try:
        events = list(client.create_instance_stream("niimath", dmesg=True))
    finally:
        client.close()

    assert [event["kind"] for event in events] == ["status", "serial", "ready"]


def test_resolve_boot_timeout_defaults_to_30_seconds() -> None:
    timeout = resolve_boot_timeout()
    assert timeout.connect == 10.0
    assert timeout.read == 30.0
    assert timeout.write == 30.0
    assert timeout.pool == 10.0


def test_non_object_json_response_raises_type_error() -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(200, json=["unexpected"])

    client = make_client(httpx.MockTransport(handler))
    try:
        try:
            client.cvmfs_list(
                CVMFSSource(
                    mirror="https://cvmfs.neurodesk.org",
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
    seen: list[tuple[str, str, str | None]] = []

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
        if request.method == "POST" and request.url.path == "/image/niimath/qemu/download":
            return httpx.Response(200, json={"status": "downloaded", "path": "/tmp/qemu", "required": True})
        if request.method == "POST" and request.url.path == "/image/niimath/metadata":
            return httpx.Response(
                200,
                json={"name": "niimath", "status": "prepared", "source_kind": "cvmfs", "architecture": "amd64"},
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
        raise AssertionError(f"unexpected request: {request.method} {request.url.path} {body!r}")

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
        if request.method == "POST" and request.url.path == "/image/niimath/qemu/download":
            return httpx.Response(200, json={"status": "downloaded", "path": "/tmp/qemu", "required": True})
        if request.method == "POST" and request.url.path == "/image/niimath/metadata":
            return httpx.Response(
                200,
                json={"name": "niimath", "status": "prepared", "source_kind": "cvmfs", "architecture": "amd64"},
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
    seen: list[tuple[str, str, str | None]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read().decode() or None
        seen.append((request.method, request.url.path, body))
        if request.method == "POST" and request.url.path == "/cvmfs/list":
            payload = json.loads(body or "{}")
            if payload["path"] == "/containers":
                return httpx.Response(
                    200,
                    json={"entries": [{"name": "niimath", "path": "/containers/niimath", "kind": "directory"}]},
                )
            if payload["path"] == "/containers/niimath":
                return httpx.Response(200, json={"entries": []})
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(200, json={"name": "niimath", "status": "downloaded", "source_kind": "cvmfs"})
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(200, json={"status": "running", "image": "other"})
        raise AssertionError(f"unexpected request: {request.method} {request.url.path} {body!r}")

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
    monkeypatch.setattr("pyneurodesk.api._health_check", lambda base_url: base_url == "http://127.0.0.1:4567")
    state_path = tmp_path / "Library" / "Caches" / "ccx3" / "ccvm.json"
    state_path.parent.mkdir(parents=True)
    state_path.write_text('{"addr":"127.0.0.1:4567"}')

    assert resolve_base_url() == "http://127.0.0.1:4567"


def test_resolve_base_url_starts_daemon_when_state_missing(monkeypatch) -> None:
    monkeypatch.delenv("CCX3_URL", raising=False)
    monkeypatch.delenv("CCVM_URL", raising=False)
    monkeypatch.setattr("pyneurodesk.api.default_daemon_state_path", lambda: Path("/tmp/does-not-exist"))
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
    monkeypatch.setattr("pyneurodesk.api._health_check", lambda base_url: base_url == "http://127.0.0.1:3456")
    monkeypatch.setattr("pyneurodesk.api.subprocess.Popen", fake_popen)

    state = start_default_daemon()

    assert state.base_url == "http://127.0.0.1:3456"
    assert state.cache_dir == str(cache_root)
    assert seen["args"] == [str(ccvm_path), "-cache-dir", str(cache_root)]
    assert (cache_root / "ccvm.json").read_text() == '{\n  "addr": "127.0.0.1:3456"\n}'


def test_resolve_ccvm_binary_path_prefers_bundled_binary(monkeypatch, tmp_path: Path) -> None:
    bundle_root = tmp_path / "pyneurodesk"
    binary = bundle_root / "bin" / "ccvm"
    binary.parent.mkdir(parents=True)
    binary.write_text("")
    monkeypatch.setattr("pyneurodesk.api.pyneurodesk_root", lambda: bundle_root)
    monkeypatch.setattr("pyneurodesk.api.maybe_refresh_bundled_ccvm", lambda path: None)
    monkeypatch.delenv("CCX3_CCVM", raising=False)
    monkeypatch.delenv("CCVM_BINARY", raising=False)

    assert resolve_ccvm_binary_path() == binary


def test_resolve_ccvm_binary_path_rebuilds_stale_bundled_binary(monkeypatch, tmp_path: Path) -> None:
    bundle_root = tmp_path / "local" / "pyneurodesk"
    binary = bundle_root / "bin" / "ccvm"
    binary.parent.mkdir(parents=True)
    binary.write_text("old")

    calls: list[Path] = []

    monkeypatch.setattr("pyneurodesk.api.pyneurodesk_root", lambda: bundle_root)
    monkeypatch.setattr("pyneurodesk.api.maybe_refresh_bundled_ccvm", lambda path: calls.append(path))
    monkeypatch.delenv("CCX3_CCVM", raising=False)
    monkeypatch.delenv("CCVM_BINARY", raising=False)

    assert resolve_ccvm_binary_path() == binary
    assert calls == [binary]


def test_create_container_cache_dir_is_isolated(monkeypatch, tmp_path: Path) -> None:
    monkeypatch.setattr("pyneurodesk.api.default_cache_root", lambda: tmp_path / "cache")

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
                return SimpleNamespace(
                    entries=[]
                )
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

        def get_image(self, image: str) -> object | None:
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

        def run(self, image: str, command: list[str], *, shares: list[object] = ()) -> object:
            calls.append(("run", (image, tuple(command), tuple((share.source, share.mount, share.writable) for share in shares))))
            return SimpleNamespace(exit_code=0, output="ok\n")

        def close(self) -> None:
            calls.append(("close_client", None))

    monkeypatch.setattr("pyneurodesk.api.start_default_daemon", fake_start_default_daemon)
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
    assert ("run", ("niimath", ("niimath",), ())) in calls
    assert container.owns_daemon is False


def test_share_dir_arguments_are_translated_into_guest_paths(monkeypatch, tmp_path: Path) -> None:
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

        def get_image(self, image: str) -> object | None:
            return SimpleNamespace(name=image, status="downloaded")

        def import_image(self, image: str, request: object) -> object:
            raise AssertionError("image import should not be needed")

        def instance_status(self) -> object:
            return SimpleNamespace(status="running", image="niimath")

        def create_instance(self, image: str) -> object:
            raise AssertionError("instance creation should not be needed")

        def run(self, image: str, command: list[str], *, shares: list[object] = ()) -> object:
            calls.append(
                (
                    "run",
                    {
                        "image": image,
                        "command": tuple(command),
                        "shares": tuple((share.source, share.mount, share.writable) for share in shares),
                    },
                )
            )
            return SimpleNamespace(exit_code=0, output="shared\n")

        def shutdown_instance(self) -> None:
            return None

        def close(self) -> None:
            return None

    monkeypatch.setattr("pyneurodesk.api.start_default_daemon", fake_start_default_daemon)
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
    assert run_call["shares"] == ((str(share_root), run_call["command"][1].rsplit("/", 1)[0], False),)


def test_owned_container_recovers_when_daemon_connection_is_refused(monkeypatch) -> None:
    daemon_a = DaemonState(addr="127.0.0.1:4001", cache_dir="/tmp/daemon-a")
    daemon_b = DaemonState(addr="127.0.0.1:4002", cache_dir="/tmp/daemon-a")
    calls: list[tuple[str, object]] = []

    class FakeClient:
        def __init__(self, base_url: str) -> None:
            calls.append(("connect", base_url))
            self.base_url = base_url
            self._client = SimpleNamespace(base_url=base_url)
            self._run_calls = 0

        def run(self, image: str, command: list[str], *, shares: list[object] = ()) -> object:
            self._run_calls += 1
            calls.append(("run", (self.base_url, image, tuple(command))))
            if self.base_url.endswith(":4001"):
                raise httpx.ConnectError("connection refused")
            return SimpleNamespace(exit_code=0, output="ok\n")

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
    assert ("restart", daemon_a.cache_dir) in calls
    assert ("ensure_image", "niimath") in calls
    assert ("ensure_instance", "niimath") in calls
    assert ("run", ("http://127.0.0.1:4002", "niimath", ("niimath",))) in calls


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
                    json={"entries": [{"name": "niimath", "path": "/containers/niimath", "kind": "directory"}]},
                )
            raise AssertionError(f"unexpected CVMFS path probe: {payload['path']}")
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(200, json={"name": "niimath", "status": "downloaded", "source_kind": "cvmfs"})
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
            return httpx.Response(200, json={"name": "niimath", "status": "downloaded", "source_kind": "cvmfs"})
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
            raise AssertionError(f"unexpected request: {request.method} {request.url.path}")
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
                        {"name": "othertool_9.9.9_20250101", "path": "/containers/othertool_9.9.9_20250101", "kind": "directory"},
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
            raise AssertionError(f"unexpected request: {request.method} {request.url.path}")
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


def test_container_debug_boot_prints_live_stream_and_error(monkeypatch, capsys: pytest.CaptureFixture[str]) -> None:
    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read().decode() or "{}"
        if request.method == "POST" and request.url.path == "/cvmfs/list":
            payload = json.loads(body)
            if payload["path"] == "/containers/niimath":
                return httpx.Response(200, json={"entries": []})
            if payload["path"] == "/containers":
                return httpx.Response(
                    200,
                    json={"entries": [{"name": "niimath", "path": "/containers/niimath", "kind": "directory"}]},
                )
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(200, json={"name": "niimath", "status": "downloaded", "source_kind": "cvmfs"})
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(200, json={"status": "stopped"})
        if request.method == "POST" and request.url.path == "/kernel/download":
            return httpx.Response(200, json={"status": "downloaded"})
        if request.method == "POST" and request.url.path == "/image/niimath/qemu/download":
            return httpx.Response(200, json={"status": "downloaded", "path": "/tmp/qemu", "required": True})
        if request.method == "POST" and request.url.path == "/image/niimath/metadata":
            return httpx.Response(
                200,
                json={"name": "niimath", "status": "prepared", "source_kind": "cvmfs", "architecture": "amd64"},
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


def test_container_progress_reports_required_downloads(monkeypatch, capsys: pytest.CaptureFixture[str]) -> None:
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

        def get_image(self, image: str) -> object | None:
            return None

        def import_image(self, image: str, request: object) -> object:
            return SimpleNamespace(name=image, status="downloaded")

        def instance_status(self) -> object:
            return SimpleNamespace(status="stopped", image=None)

        def download_kernel(self) -> object:
            return SimpleNamespace(status="downloaded", source="/cache/vmlinuz")

        def prepare_image_emulator(self, image: str) -> object:
            return SimpleNamespace(status="downloaded", path="/tmp/qemu-system-x86_64", required=True)

        def prepare_image_metadata(self, image: str) -> object:
            return SimpleNamespace(status="prepared", architecture="amd64")

        def create_instance(self, image: str, *, dmesg: bool = False) -> object:
            return SimpleNamespace(status="running", image=image)

        def shutdown_instance(self) -> object:
            return SimpleNamespace(status="stopped", image=None)

        def close(self) -> None:
            return None

    monkeypatch.setattr("pyneurodesk.api._supports_notebook_display", lambda: False)
    monkeypatch.setattr("pyneurodesk.api.start_default_daemon", fake_start_default_daemon)
    monkeypatch.setattr("pyneurodesk.api.PyNeurodeskClient", FakeClient)

    container = nd.container("niimath")
    container.close()

    captured = capsys.readouterr()
    assert "Downloading required file 1/2: Linux kernel" in captured.out
    assert "Downloaded required file 1/2: vmlinuz" in captured.out
    assert "Downloading required file 2/2: emulator" in captured.out
    assert "Downloaded required file 2/2: qemu-system-x86_64" in captured.out


def test_container_progress_reports_rate_and_eta_from_stream(monkeypatch, capsys: pytest.CaptureFixture[str]) -> None:
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

        def get_image(self, image: str) -> object | None:
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
    monkeypatch.setattr("pyneurodesk.api.start_default_daemon", fake_start_default_daemon)
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
    monkeypatch.setattr("pyneurodesk.api.resolve_release_index_dir", lambda: releases_dir)

    assert nd.search("niimath") == ["1.0.0", "1.0.20250804"]


def test_search_uses_remote_release_metadata(monkeypatch) -> None:
    monkeypatch.setattr(
        "pyneurodesk.api._search_remote_release_versions",
        lambda name: {"1.0.0": "20250617", "1.0.20250804": "20251016"} if name == "niimath" else {},
    )

    assert nd.search("niimath") == ["1.0.0", "1.0.20250804"]


def test_container_uses_local_release_metadata_without_cvmfs_listing(monkeypatch, tmp_path: Path) -> None:
    releases_dir = tmp_path / "releases"
    (releases_dir / "niimath").mkdir(parents=True)
    (releases_dir / "niimath" / "1.0.0.json").write_text(
        '{"apps":{"niimath 1.0.0":{"version":"20250617","exec":""}}}'
    )
    (releases_dir / "niimath" / "1.0.20250804.json").write_text(
        '{"apps":{"niimath 1.0.20250804":{"version":"20251016","exec":""}}}'
    )
    monkeypatch.setattr("pyneurodesk.api.resolve_release_index_dir", lambda: releases_dir)

    seen: list[tuple[str, str, str | None]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read().decode() or None
        seen.append((request.method, request.url.path, body))
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(200, json={"name": "niimath", "status": "downloaded", "source_kind": "cvmfs"})
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(200, json={"status": "stopped"})
        if request.method == "POST" and request.url.path == "/kernel/download":
            return httpx.Response(200, json={"status": "downloaded"})
        if request.method == "POST" and request.url.path == "/image/niimath/qemu/download":
            return httpx.Response(200, json={"status": "downloaded", "path": "/tmp/qemu", "required": True})
        if request.method == "POST" and request.url.path == "/image/niimath/metadata":
            return httpx.Response(
                200,
                json={"name": "niimath", "status": "prepared", "source_kind": "cvmfs", "architecture": "amd64"},
            )
        if request.method == "POST" and request.url.path == "/vm":
            return httpx.Response(200, json={"status": "running", "image": "niimath"})
        raise AssertionError(f"unexpected request: {request.method} {request.url.path} {body!r}")

    client = make_client(httpx.MockTransport(handler))
    try:
        container = nd.container("niimath", client=client, progress=False)
    finally:
        client.close()

    assert container.path == build_release_container_path("niimath", "1.0.20250804", "20251016")
    assert all(path != "/cvmfs/list" for _, path, _ in seen)


def test_container_uses_remote_release_metadata_without_cvmfs_listing(monkeypatch) -> None:
    monkeypatch.setattr(
        "pyneurodesk.api._search_remote_release_versions",
        lambda name: {"1.0.0": "20250617", "1.0.20250804": "20251016"} if name == "niimath" else {},
    )

    seen: list[tuple[str, str, str | None]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        body = request.read().decode() or None
        seen.append((request.method, request.url.path, body))
        if request.method == "GET" and request.url.path == "/image/niimath":
            return httpx.Response(200, json={"name": "niimath", "status": "downloaded", "source_kind": "cvmfs"})
        if request.method == "GET" and request.url.path == "/vm/status":
            return httpx.Response(200, json={"status": "stopped"})
        if request.method == "POST" and request.url.path == "/kernel/download":
            return httpx.Response(200, json={"status": "downloaded"})
        if request.method == "POST" and request.url.path == "/image/niimath/qemu/download":
            return httpx.Response(200, json={"status": "downloaded", "path": "/tmp/qemu", "required": True})
        if request.method == "POST" and request.url.path == "/image/niimath/metadata":
            return httpx.Response(
                200,
                json={"name": "niimath", "status": "prepared", "source_kind": "cvmfs", "architecture": "amd64"},
            )
        if request.method == "POST" and request.url.path == "/vm":
            return httpx.Response(200, json={"status": "running", "image": "niimath"})
        raise AssertionError(f"unexpected request: {request.method} {request.url.path} {body!r}")

    client = make_client(httpx.MockTransport(handler))
    try:
        container = nd.container("niimath", client=client, progress=False)
    finally:
        client.close()

    assert container.path == build_release_container_path("niimath", "1.0.20250804", "20251016")
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
