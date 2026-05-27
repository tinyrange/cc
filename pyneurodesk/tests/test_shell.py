from __future__ import annotations

import base64
import hashlib
import json
import tomllib
from pathlib import Path
from types import SimpleNamespace
from typing import Optional

import pytest

from pyneurodesk import shell
from pyneurodesk.models import ContainerReference, ImageSource


def test_package_installs_nd_entrypoint() -> None:
    pyproject = tomllib.loads(
        (Path(__file__).parents[1] / "pyproject.toml").read_text()
    )

    assert pyproject["project"]["scripts"]["nd"] == "pyneurodesk:main"


def test_activate_emits_shell_code_initializes_session_and_bootstraps_by_default(
    monkeypatch,
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    monkeypatch.setattr(
        "pyneurodesk.shell.default_cache_root", lambda: tmp_path / "cache"
    )

    exit_code = shell.main(["activate", "--shell", "bash"])

    assert exit_code == 0
    output = capsys.readouterr().out
    assert "PYNEURODESK_SHELL_SESSION" in output
    assert "PYNEURODESK_SHELL_BOOTSTRAP_PID" in output
    assert "command neurodesk shell bootstrap >/dev/null 2>&1 &" in output
    assert 'if [ "$#" -eq 0 ]; then' in output
    assert "command neurodesk shell --help" in output
    assert "_pyneurodesk_source_env()" in output
    assert "_pyneurodesk_source_env" in output
    assert "_neurodesk_complete()" in output
    assert "_nd_complete()" in output
    session_dirs = list((tmp_path / "cache" / "pyneurodesk-shell").iterdir())
    assert len(session_dirs) == 1
    state = json.loads((session_dirs[0] / "state.json").read_text())
    assert state["version"] == 1
    assert state["images"] == {}
    assert state["wrappers"] == {}


def test_activate_supports_no_bootstrap(
    monkeypatch, tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    monkeypatch.setattr(
        "pyneurodesk.shell.default_cache_root", lambda: tmp_path / "cache"
    )

    exit_code = shell.main(["activate", "--shell", "bash", "--no-bootstrap"])

    assert exit_code == 0
    output = capsys.readouterr().out
    assert "shell bootstrap" not in output
    assert "PYNEURODESK_SHELL_BOOTSTRAP_PID" in output


def test_activate_with_network_persists_session_network(
    monkeypatch, tmp_path: Path
) -> None:
    monkeypatch.setattr(
        "pyneurodesk.shell.default_cache_root", lambda: tmp_path / "cache"
    )

    assert (
        shell.main(["activate", "--shell", "bash", "--no-bootstrap", "--with-network"])
        == 0
    )

    session_dirs = list((tmp_path / "cache" / "pyneurodesk-shell").iterdir())
    state = json.loads((session_dirs[0] / "state.json").read_text())
    assert state["network"] == {"enabled": True}


def test_activate_with_vm_persists_session_vm(monkeypatch, tmp_path: Path) -> None:
    monkeypatch.setattr(
        "pyneurodesk.shell.default_cache_root", lambda: tmp_path / "cache"
    )

    assert (
        shell.main(
            ["activate", "--shell", "bash", "--no-bootstrap", "--vm", "analysis"]
        )
        == 0
    )

    session_dirs = list((tmp_path / "cache" / "pyneurodesk-shell").iterdir())
    state = json.loads((session_dirs[0] / "state.json").read_text())
    assert state["vm_id"] == "analysis"


def test_completion_emits_zsh_support(capsys: pytest.CaptureFixture[str]) -> None:
    exit_code = shell.main(["completion", "--shell", "zsh"])

    assert exit_code == 0
    output = capsys.readouterr().out
    assert "compdef _neurodesk_complete neurodesk" in output
    assert "compdef _nd_complete nd" in output


def test_activate_emits_powershell_code_initializes_session(
    monkeypatch,
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    monkeypatch.setattr(
        "pyneurodesk.shell.default_cache_root", lambda: tmp_path / "cache"
    )

    exit_code = shell.main(["activate", "--shell", "powershell", "--no-bootstrap"])

    assert exit_code == 0
    output = capsys.readouterr().out
    assert "$env:PYNEURODESK_SHELL_SESSION" in output
    assert "$env:PYNEURODESK_SHELL_ROOT" in output
    assert "$env:PYNEURODESK_SHELL_BIN" in output
    assert '$env:PATH = "$env:PYNEURODESK_SHELL_BIN;$env:PATH"' in output
    assert "function global:nd" in output
    assert "neurodesk shell --help" in output
    assert "Register-ArgumentCompleter -CommandName neurodesk,nd" in output
    assert "Start-Process" not in output
    session_dirs = list((tmp_path / "cache" / "pyneurodesk-shell").iterdir())
    assert len(session_dirs) == 1
    state = json.loads((session_dirs[0] / "state.json").read_text())
    assert state["version"] == 1
    assert state["images"] == {}
    assert state["wrappers"] == {}


def test_completion_emits_powershell_support(
    capsys: pytest.CaptureFixture[str],
) -> None:
    exit_code = shell.main(["completion", "--shell", "powershell"])

    assert exit_code == 0
    output = capsys.readouterr().out
    assert "Register-ArgumentCompleter -CommandName neurodesk,nd" in output
    assert "neurodesk shell complete --index $index -- @words" in output
    assert "CompletionResult" in output


def test_shell_help_hides_internal_commands(capsys: pytest.CaptureFixture[str]) -> None:
    with pytest.raises(SystemExit) as exc:
        shell.main(["shell", "--help"])

    assert exc.value.code == 0
    output = capsys.readouterr().out
    for command in ("load", "unload", "list", "forward", "exec", "completion"):
        assert command in output
    for command in (
        "bootstrap",
        "neurodesktop-server",
        "complete",
        "run-wrapper",
        "==SUPPRESS==",
    ):
        assert command not in output


def test_activate_defaults_to_powershell_on_windows(
    monkeypatch,
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    monkeypatch.setattr("pyneurodesk.shell.is_windows_host", lambda: True)
    monkeypatch.setattr(
        "pyneurodesk.shell.default_cache_root", lambda: tmp_path / "cache"
    )

    assert shell.main(["activate", "--no-bootstrap"]) == 0

    output = capsys.readouterr().out
    assert "$env:PYNEURODESK_SHELL_SESSION" in output
    assert "Register-ArgumentCompleter -CommandName neurodesk,nd" in output


def test_pwsh_alias_uses_powershell_renderer(
    capsys: pytest.CaptureFixture[str],
) -> None:
    exit_code = shell.main(["completion", "--shell", "pwsh"])

    assert exit_code == 0
    assert (
        "Register-ArgumentCompleter -CommandName neurodesk,nd"
        in capsys.readouterr().out
    )


def test_shell_bootstrap_starts_default_daemon(monkeypatch) -> None:
    calls: list[tuple[str, object]] = []

    monkeypatch.setattr(
        "pyneurodesk.shell.start_default_daemon",
        lambda: (
            calls.append(("boot", None))
            or SimpleNamespace(base_url="http://daemon.test")
        ),
    )

    exit_code = shell.main(["shell", "bootstrap"])

    assert exit_code == 0
    assert calls == [("boot", None)]


def test_shell_load_discovers_commands_writes_wrappers_and_persists_env(
    monkeypatch,
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    root = tmp_path / "session"
    root.mkdir()
    monkeypatch.setenv(shell.SESSION_ENV, "sess-1")
    monkeypatch.setenv(shell.SESSION_ROOT_ENV, str(root))
    monkeypatch.setenv(shell.SESSION_BIN_ENV, str(root / "bin"))
    state = shell.SessionState(session_id="sess-1", root=root, images={}, wrappers={})
    shell.write_state(state)

    calls: list[tuple[str, object]] = []

    class FakeClient:
        def cvmfs_list(self, source: object) -> object:
            calls.append(("cvmfs_list", source.path))
            return SimpleNamespace(
                entries=[
                    SimpleNamespace(kind="file", name="niimath"),
                    SimpleNamespace(kind="file", name="bet"),
                    SimpleNamespace(kind="file", name="commands.txt"),
                    SimpleNamespace(kind="file", name="env.txt"),
                ]
            )

        def cvmfs_read(self, request: object) -> object:
            calls.append(("cvmfs_read", request.path))
            if request.path.endswith("/commands.txt"):
                payload = base64.b64encode(b"niimath\nbet\nmissing-wrapper\n").decode()
            elif request.path.endswith("/env.txt"):
                payload = base64.b64encode(
                    b"DEPLOY_ENV_FSLDIR=BASEPATH/opt/fsl\n"
                ).decode()
            elif request.path.endswith("/build.yaml"):
                payload = base64.b64encode(b"").decode()
            else:
                raise AssertionError(request.path)
            return SimpleNamespace(data=payload.encode())

        def prepare_image_metadata(self, image: str) -> object:
            calls.append(("prepare_image_metadata", image))
            return SimpleNamespace(
                env=["PATH=/usr/local/bin:/usr/bin:/bin:/opt/niimath"]
            )

    class FakeHandle:
        def __init__(self) -> None:
            self._client = FakeClient()
            self.reference = SimpleNamespace(
                image="niimath",
                path="/containers/niimath_1.0.20250804_20251016/niimath_1.0.20250804_20251016.simg",
                cache_dir=None,
            )

        def close(self) -> None:
            calls.append(("close", None))

    monkeypatch.setattr(
        "pyneurodesk.shell.container", lambda image, progress=False: FakeHandle()
    )
    monkeypatch.setattr(
        "pyneurodesk.shell.resolve_command_name", lambda: "/usr/local/bin/neurodesk"
    )

    exit_code = shell.main(["shell", "load", "niimath"])

    assert exit_code == 0
    output = capsys.readouterr().out
    assert "loaded niimath" in output
    new_state = shell.read_state(root, session_id="sess-1")
    assert sorted(new_state.images["niimath"]["commands"]) == ["bet", "niimath"]
    assert new_state.images["niimath"]["deploy_env"] == [
        "PATH=/usr/local/bin:/usr/bin:/bin:/opt/niimath",
        "DEPLOY_ENV_FSLDIR=/opt/fsl",
    ]
    wrapper = shell.wrapper_path(root / "bin", "niimath").read_text()
    if shell.os.name == "nt":
        assert '"/usr/local/bin/neurodesk" shell run-wrapper' in wrapper
        assert '--image "niimath"' in wrapper
        assert '--command "niimath"' in wrapper
    else:
        assert "/usr/local/bin/neurodesk shell run-wrapper" in wrapper
        assert "--image niimath" in wrapper
        assert "--command niimath" in wrapper
    assert calls[0] == ("cvmfs_list", "/containers/niimath_1.0.20250804_20251016")
    assert calls[-1] == ("close", None)


def test_write_cmd_wrapper_generates_windows_native_shim(
    monkeypatch, tmp_path: Path
) -> None:
    monkeypatch.setattr(
        "pyneurodesk.shell.resolve_command_name",
        lambda: r"C:\Program Files\Neurodesk\neurodesk.exe",
    )
    path = tmp_path / "bin" / "niimath.cmd"

    shell.write_cmd_wrapper(path, image="niimath", command="niimath")

    wrapper = path.read_text()
    assert wrapper.startswith("@echo off")
    assert r'"C:\Program Files\Neurodesk\neurodesk.exe" shell run-wrapper' in wrapper
    assert f'--session "%{shell.SESSION_ENV}%"' in wrapper
    assert '--image "niimath"' in wrapper
    assert '--command "niimath"' in wrapper
    assert "-- %*" in wrapper


def test_wrapper_path_uses_cmd_extension_on_windows(
    monkeypatch, tmp_path: Path
) -> None:
    monkeypatch.setattr(shell.os, "name", "nt")

    assert shell.wrapper_path(tmp_path, "bet") == tmp_path / "bet.cmd"


def test_shell_load_source_stores_reference_and_prepares_image(
    monkeypatch,
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    root = tmp_path / "session"
    root.mkdir()
    monkeypatch.setenv(shell.SESSION_ENV, "sess-1")
    monkeypatch.setenv(shell.SESSION_ROOT_ENV, str(root))
    monkeypatch.setenv(shell.SESSION_BIN_ENV, str(root / "bin"))
    shell.write_state(
        shell.SessionState(session_id="sess-1", root=root, images={}, wrappers={})
    )
    monkeypatch.chdir(tmp_path)

    calls: list[tuple[str, object]] = []

    class FakeClient:
        def import_image(self, name: str, request: object) -> object:
            calls.append(
                ("import_image", (name, request.source.path, request.cache_dir))
            )
            return object()

        def ensure_instance(
            self,
            image: str,
            *,
            memory_mb: Optional[int] = None,
            cpus: Optional[int] = None,
            dmesg: bool = False,
        ) -> object:
            calls.append(("ensure_instance", (image, memory_mb, cpus, dmesg)))
            return object()

        def run(self, *args: object, **kwargs: object) -> object:
            calls.append(("run", (args, kwargs)))
            return SimpleNamespace(exit_code=0, output="tool ok\n")

        def close(self) -> None:
            calls.append(("client_close", None))

    monkeypatch.setattr(
        "pyneurodesk.shell.connect",
        lambda: calls.append(("connect", None)) or FakeClient(),
    )
    monkeypatch.setattr(
        "pyneurodesk.shell.load_deploy_metadata",
        lambda handle: SimpleNamespace(commands=("tool",), deploy_env=("A=B",)),
    )
    monkeypatch.setattr(
        "pyneurodesk.shell.resolve_command_name", lambda: "/usr/local/bin/neurodesk"
    )

    exit_code = shell.main(
        [
            "shell",
            "load",
            "fulltest-image",
            "--source",
            "/containers/custom",
            "--cache-dir",
            "/tmp/cache",
            "--memory-mb",
            "512",
            "--cpus",
            "2",
            "--command",
            "sh",
        ]
    )

    assert exit_code == 0
    assert "loaded fulltest-image" in capsys.readouterr().out
    state = shell.read_state(root, session_id="sess-1")
    assert sorted(state.images["fulltest-image"]["commands"]) == [
        "fulltest-image",
        "sh",
        "tool",
    ]
    assert state.images["fulltest-image"]["deploy_env"] == ["A=B"]
    assert state.images["fulltest-image"]["memory_mb"] == 512
    assert state.images["fulltest-image"]["cpus"] == 2
    assert (
        state.images["fulltest-image"]["reference"]["source"]["path"]
        == "/containers/custom"
    )
    assert (
        (root / "env.sh").read_text()
        == "# Generated by pyneurodesk. Source through `neurodesk activate`.\nexport A=B\n"
    )
    assert calls[:3] == [
        ("connect", None),
        ("import_image", ("fulltest-image", "/containers/custom", "/tmp/cache")),
        ("ensure_instance", ("fulltest-image", 512, 2, False)),
    ]


def test_shell_load_conflict_requires_force(
    monkeypatch,
    tmp_path: Path,
) -> None:
    root = tmp_path / "session"
    root.mkdir()
    monkeypatch.setenv(shell.SESSION_ENV, "sess-1")
    monkeypatch.setenv(shell.SESSION_ROOT_ENV, str(root))
    monkeypatch.setenv(shell.SESSION_BIN_ENV, str(root / "bin"))
    shell.write_state(
        shell.SessionState(
            session_id="sess-1",
            root=root,
            images={"fsl": {"commands": ["bet"], "deploy_env": []}},
            wrappers={"bet": shell.WrapperSpec(image="fsl", command="bet")},
        )
    )
    (root / "bin").mkdir(exist_ok=True)
    shell.write_wrapper(root / "bin" / "bet", image="fsl", command="bet")

    class FakeClient:
        def cvmfs_list(self, source: object) -> object:
            return SimpleNamespace(
                entries=[
                    SimpleNamespace(kind="file", name="bet"),
                    SimpleNamespace(kind="file", name="commands.txt"),
                ]
            )

        def cvmfs_read(self, request: object) -> object:
            return SimpleNamespace(data=base64.b64encode(b"bet\n").decode().encode())

    class FakeHandle:
        def __init__(self) -> None:
            self._client = FakeClient()
            self.reference = SimpleNamespace(
                path="/containers/another/another.simg", cache_dir=None
            )

        def close(self) -> None:
            return None

    monkeypatch.setattr(
        "pyneurodesk.shell.container", lambda image, progress=False: FakeHandle()
    )

    with pytest.raises(SystemExit, match="rerun with --force"):
        shell.main(["shell", "load", "another"])


def test_shell_load_force_overrides_conflict_and_unload_restores_previous_owner(
    monkeypatch,
    tmp_path: Path,
) -> None:
    root = tmp_path / "session"
    root.mkdir()
    monkeypatch.setenv(shell.SESSION_ENV, "sess-1")
    monkeypatch.setenv(shell.SESSION_ROOT_ENV, str(root))
    monkeypatch.setenv(shell.SESSION_BIN_ENV, str(root / "bin"))
    shell.write_state(
        shell.SessionState(
            session_id="sess-1",
            root=root,
            images={"fsl": {"commands": ["bet"], "deploy_env": []}},
            wrappers={"bet": shell.WrapperSpec(image="fsl", command="bet")},
        )
    )
    shell.write_wrapper(root / "bin" / "bet", image="fsl", command="bet")

    class FakeClient:
        def cvmfs_list(self, source: object) -> object:
            return SimpleNamespace(
                entries=[
                    SimpleNamespace(kind="file", name="bet"),
                    SimpleNamespace(kind="file", name="commands.txt"),
                ]
            )

        def cvmfs_read(self, request: object) -> object:
            return SimpleNamespace(data=base64.b64encode(b"bet\n").decode().encode())

    class FakeHandle:
        def __init__(self) -> None:
            self._client = FakeClient()
            self.reference = SimpleNamespace(
                path="/containers/another/another.simg", cache_dir=None
            )

        def close(self) -> None:
            return None

    monkeypatch.setattr(
        "pyneurodesk.shell.container", lambda image, progress=False: FakeHandle()
    )

    assert shell.main(["shell", "load", "another", "--force"]) == 0
    state = shell.read_state(root, session_id="sess-1")
    assert state.wrappers["bet"] == shell.WrapperSpec(image="another", command="bet")

    assert shell.main(["shell", "unload", "another"]) == 0
    state = shell.read_state(root, session_id="sess-1")
    assert state.wrappers["bet"] == shell.WrapperSpec(image="fsl", command="bet")


def test_shell_unload_removes_image_wrappers(
    monkeypatch, tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    root = tmp_path / "session"
    root.mkdir()
    monkeypatch.setenv(shell.SESSION_ENV, "sess-1")
    monkeypatch.setenv(shell.SESSION_ROOT_ENV, str(root))
    monkeypatch.setenv(shell.SESSION_BIN_ENV, str(root / "bin"))
    shell.write_state(
        shell.SessionState(
            session_id="sess-1",
            root=root,
            images={"niimath": {"commands": ["niimath"], "deploy_env": []}},
            wrappers={"niimath": shell.WrapperSpec(image="niimath", command="niimath")},
        )
    )
    wrapper = shell.wrapper_path(root / "bin", "niimath")
    shell.write_wrapper(wrapper, image="niimath", command="niimath")

    exit_code = shell.main(["shell", "unload", "niimath"])

    assert exit_code == 0
    assert "unloaded niimath" in capsys.readouterr().out
    new_state = shell.read_state(root, session_id="sess-1")
    assert new_state.images == {}
    assert new_state.wrappers == {}
    assert not wrapper.exists()


def test_shell_complete_returns_loaded_images_for_exec(
    monkeypatch, tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    root = tmp_path / "session"
    root.mkdir()
    monkeypatch.setenv(shell.SESSION_ENV, "sess-1")
    monkeypatch.setenv(shell.SESSION_ROOT_ENV, str(root))
    monkeypatch.setenv(shell.SESSION_BIN_ENV, str(root / "bin"))
    shell.write_state(
        shell.SessionState(
            session_id="sess-1",
            root=root,
            images={
                "fsl": {"commands": ["bet"], "deploy_env": []},
                "niimath": {"commands": ["niimath"], "deploy_env": []},
            },
            wrappers={},
        )
    )

    exit_code = shell.main(
        ["shell", "complete", "--index", "3", "--", "nd", "exec", ""]
    )

    assert exit_code == 0
    assert capsys.readouterr().out.splitlines() == ["fsl", "niimath"]


def test_shell_forward_updates_running_vm_dynamically(
    monkeypatch,
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    calls: list[tuple[str, object]] = []
    root = tmp_path / "session"
    root.mkdir()
    monkeypatch.setenv(shell.SESSION_ENV, "sess-1")
    monkeypatch.setenv(shell.SESSION_ROOT_ENV, str(root))
    monkeypatch.setenv(shell.SESSION_BIN_ENV, str(root / "bin"))
    shell.write_state(
        shell.SessionState(session_id="sess-1", root=root, images={}, wrappers={})
    )

    class FakeClient:
        def instance_status(self) -> object:
            calls.append(("status", None))
            return SimpleNamespace(status="running")

        def add_port_forward(self, forward: object) -> object:
            calls.append(("forward", forward))
            return forward

        def close(self) -> None:
            calls.append(("close", None))

    monkeypatch.setattr("pyneurodesk.shell.connect", lambda: FakeClient())

    assert shell.main(["shell", "forward", "8080:8080"]) == 0

    state = shell.read_state(root, session_id="sess-1")
    assert state.network == {
        "enabled": True,
        "port_forwards": [
            {
                "protocol": "tcp",
                "guest_port": 8080,
                "host_port": 8080,
                "host_addr": "127.0.0.1",
                "guest_addr": "10.42.0.2",
            }
        ],
    }
    assert calls[0] == ("status", None)
    assert calls[1][0] == "forward"
    forward = calls[1][1]
    assert forward.host_port == 8080
    assert forward.guest_port == 8080
    assert "forwarded 127.0.0.1:8080 -> guest:8080" in capsys.readouterr().out


def test_neurodesktop_starts_vm_with_network_and_jupyter_forward(
    monkeypatch,
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    image_path = tmp_path / "neurodesktop.sif"
    image_path.write_bytes(b"sif")
    calls: list[tuple[str, object]] = []
    launched: dict[str, object] = {}

    class FakeClient:
        def get_image(self, image: str) -> object:
            calls.append(("get_image", image))
            return None

        def import_image(self, image: str, request: object) -> object:
            calls.append(("import_image", (image, request)))
            return SimpleNamespace(name=image)

        def download_kernel(self) -> object:
            calls.append(("download_kernel", None))
            return SimpleNamespace(status="available")

        def prepare_image_emulator(self, image: str) -> object:
            calls.append(("prepare_emulator", image))
            return SimpleNamespace(status="available")

        def prepare_image_metadata(self, image: str) -> object:
            calls.append(("prepare_metadata", image))
            return SimpleNamespace(status="available")

        def ensure_instance(self, image: str, **kwargs: object) -> object:
            calls.append(("ensure_instance", (image, kwargs)))
            return SimpleNamespace(status="running", image=image)

        def add_port_forward(self, forward: object) -> object:
            calls.append(("forward", forward))
            return forward

        def close(self) -> None:
            calls.append(("close", None))

    monkeypatch.setattr(
        "pyneurodesk.shell.start_container_daemon",
        lambda: SimpleNamespace(
            base_url="http://127.0.0.1:1234", cache_dir=str(tmp_path / "cache")
        ),
    )
    monkeypatch.setattr("pyneurodesk.shell.connect", lambda base_url=None: FakeClient())
    monkeypatch.setattr("pyneurodesk.shell.reserve_tcp_port", lambda host: 4567)
    monkeypatch.setattr(
        "pyneurodesk.shell.wait_for_jupyter_with_log",
        lambda url, log_path, timeout_seconds, **kwargs: True,
    )
    monkeypatch.setattr(
        "pyneurodesk.shell.start_neurodesktop_jupyter_process",
        lambda base_url, image, log_path: (
            launched.update(
                {"base_url": base_url, "image": image, "log_path": log_path}
            )
            or SimpleNamespace(pid=99)
        ),
    )
    monkeypatch.setattr(
        "pyneurodesk.shell.attach_neurodesktop_session",
        lambda base_url, process, log_path: 0,
    )

    assert (
        shell.main(
            ["neurodesktop", "--image-path", str(image_path), "--startup-timeout", "1"]
        )
        == 0
    )

    ensure_call = next(value for name, value in calls if name == "ensure_instance")
    assert ensure_call[0] == "neurodesktop"
    network = ensure_call[1]["network"]
    assert network.enabled is True
    assert network.allow_internet is True
    assert network.port_forwards[0].host_port == 4567
    assert network.port_forwards[0].guest_port == 8888
    assert ("forward", network.port_forwards[0]) in calls
    assert launched["image"] == "neurodesktop"
    output = capsys.readouterr().out
    assert "http://127.0.0.1:4567/lab" in output
    assert "jupyter pid: 99" in output


def test_neurodesktop_reports_streaming_import_and_boot_progress(
    monkeypatch,
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    calls: list[tuple[str, object]] = []

    class FakeClient:
        def get_image(self, image: str) -> object:
            calls.append(("get_image", image))
            return None

        def import_image_stream(self, image: str, request: object) -> object:
            calls.append(("import_image_stream", (image, request)))
            yield SimpleNamespace(
                status="downloading", artifact="neurodesktop.simg", bytes_downloaded=512
            )
            yield SimpleNamespace(
                status="downloaded", artifact="neurodesktop.simg", bytes_downloaded=1024
            )

        def import_image(self, image: str, request: object) -> object:
            raise AssertionError(
                "blocking import_image should not be used when streaming is available"
            )

        def download_kernel_stream(self) -> object:
            calls.append(("download_kernel_stream", None))
            yield SimpleNamespace(
                status="downloaded", artifact="vmlinuz", bytes_downloaded=2048
            )

        def download_kernel(self) -> object:
            raise AssertionError(
                "blocking download_kernel should not be used when streaming is available"
            )

        def prepare_image_emulator_stream(self, image: str) -> object:
            calls.append(("prepare_emulator_stream", image))
            yield SimpleNamespace(
                status="downloaded", artifact="qemu", bytes_downloaded=4096
            )

        def prepare_image_emulator(self, image: str) -> object:
            raise AssertionError(
                "blocking prepare_image_emulator should not be used when streaming is available"
            )

        def prepare_image_metadata(self, image: str) -> object:
            calls.append(("prepare_metadata", image))
            return SimpleNamespace(status="available")

        def instance_status(self) -> object:
            calls.append(("status", None))
            return SimpleNamespace(status="stopped", image=None)

        def create_instance_stream(self, image: str, **kwargs: object) -> object:
            calls.append(("create_instance_stream", (image, kwargs)))
            yield {"kind": "status", "message": "launching kernel"}
            yield {"kind": "ready"}

        def ensure_instance(self, image: str, **kwargs: object) -> object:
            raise AssertionError(
                "blocking ensure_instance should not be used when streaming is available"
            )

        def add_port_forward(self, forward: object) -> object:
            calls.append(("forward", forward))
            return forward

        def close(self) -> None:
            calls.append(("close", None))

    monkeypatch.setattr(
        "pyneurodesk.shell.start_container_daemon",
        lambda: SimpleNamespace(
            base_url="http://127.0.0.1:1234", cache_dir=str(tmp_path / "cache")
        ),
    )
    monkeypatch.setattr("pyneurodesk.shell.connect", lambda base_url=None: FakeClient())
    monkeypatch.setattr("pyneurodesk.shell.reserve_tcp_port", lambda host: 4567)
    monkeypatch.setattr(
        "pyneurodesk.shell.wait_for_jupyter_with_log",
        lambda url, log_path, timeout_seconds, **kwargs: True,
    )
    monkeypatch.setattr(
        "pyneurodesk.shell.start_neurodesktop_jupyter_process",
        lambda base_url, image, log_path: SimpleNamespace(pid=99),
    )
    monkeypatch.setattr(
        "pyneurodesk.shell.attach_neurodesktop_session",
        lambda base_url, process, log_path: 0,
    )
    monkeypatch.setattr(
        "pyneurodesk.shell.resolve_container_reference",
        lambda client, name, mirror, repo: ContainerReference(
            name=name,
            image=name,
            source=ImageSource(
                type="cvmfs",
                mirror=mirror,
                repo=repo,
                path="/containers/neurodesktop_20260428/neurodesktop_20260428.simg",
            ),
        ),
    )

    assert shell.main(["neurodesktop", "--startup-timeout", "1"]) == 0

    import_calls = [value for name, value in calls if name == "import_image_stream"]
    assert len(import_calls) == 2
    assert import_calls[0][1].prefetch is False
    assert import_calls[1][1].prefetch is True
    assert [
        name
        for name, _ in calls
        if name in ("import_image_stream", "create_instance_stream")
    ] == ["import_image_stream", "import_image_stream", "create_instance_stream"]
    boot_call = next(value for name, value in calls if name == "create_instance_stream")
    assert boot_call[0] == "neurodesktop"
    assert boot_call[1]["memory_mb"] == 8192
    assert boot_call[1]["cpus"] == 1
    output = capsys.readouterr().out
    assert "Importing neurodesktop.simg" in output
    assert "Filling Neurodesktop CVMFS cache" in output
    assert "Booting neurodesktop: launching kernel" in output
    assert "Neurodesktop JupyterLab is ready" in output
    assert "http://127.0.0.1:4567/lab" in output
    assert "neurodesktop: http://127.0.0.1:4567/neurodesktop/index.html" in output


def test_neurodesktop_adds_dynamic_forward_to_running_image(
    monkeypatch,
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    calls: list[tuple[str, object]] = []

    class FakeClient:
        def get_image(self, image: str) -> object:
            calls.append(("get_image", image))
            return SimpleNamespace(name=image)

        def download_kernel(self) -> object:
            calls.append(("download_kernel", None))
            return SimpleNamespace(status="available")

        def prepare_image_emulator(self, image: str) -> object:
            calls.append(("prepare_emulator", image))
            return SimpleNamespace(status="available")

        def prepare_image_metadata(self, image: str) -> object:
            calls.append(("prepare_metadata", image))
            return SimpleNamespace(status="available")

        def instance_status(self) -> object:
            calls.append(("status", None))
            return SimpleNamespace(status="running", image="neurodesktop")

        def shutdown_instance(self) -> object:
            raise AssertionError("running neurodesktop should not be restarted")

        def create_instance_stream(self, image: str, **kwargs: object) -> object:
            raise AssertionError("running neurodesktop should not be booted again")

        def add_port_forward(self, forward: object) -> object:
            calls.append(("forward", forward))
            return forward

        def close(self) -> None:
            calls.append(("close", None))

    monkeypatch.setattr(
        "pyneurodesk.shell.start_container_daemon",
        lambda: SimpleNamespace(
            base_url="http://127.0.0.1:1234", cache_dir=str(tmp_path / "cache")
        ),
    )
    monkeypatch.setattr("pyneurodesk.shell.connect", lambda base_url=None: FakeClient())
    monkeypatch.setattr("pyneurodesk.shell.reserve_tcp_port", lambda host: 4567)
    monkeypatch.setattr(
        "pyneurodesk.shell.wait_for_jupyter_with_log",
        lambda url, log_path, timeout_seconds, **kwargs: True,
    )
    monkeypatch.setattr(
        "pyneurodesk.shell.start_neurodesktop_jupyter_process",
        lambda base_url, image, log_path: SimpleNamespace(pid=99),
    )
    monkeypatch.setattr(
        "pyneurodesk.shell.attach_neurodesktop_session",
        lambda base_url, process, log_path: 0,
    )

    assert shell.main(["neurodesktop", "--startup-timeout", "1"]) == 0

    forward = next(value for name, value in calls if name == "forward")
    assert forward.host_port == 4567
    assert forward.guest_port == 8888
    output = capsys.readouterr().out
    assert "VM for neurodesktop is already running" in output
    assert "http://127.0.0.1:4567/lab" in output


def test_neurodesktop_defaults_to_cvmfs_source(monkeypatch, tmp_path: Path) -> None:
    calls: list[tuple[str, object]] = []

    class FakeClient:
        def get_image(self, image: str) -> object:
            calls.append(("get_image", image))
            return None

        def import_image(self, image: str, request: object) -> object:
            calls.append(("import_image", (image, request)))
            return SimpleNamespace(name=image)

        def download_kernel(self) -> object:
            calls.append(("download_kernel", None))
            return SimpleNamespace(status="available")

        def prepare_image_emulator(self, image: str) -> object:
            calls.append(("prepare_emulator", image))
            return SimpleNamespace(status="available")

        def prepare_image_metadata(self, image: str) -> object:
            calls.append(("prepare_metadata", image))
            return SimpleNamespace(status="available")

        def ensure_instance(self, image: str, **kwargs: object) -> object:
            calls.append(("ensure_instance", (image, kwargs)))
            return SimpleNamespace(status="running", image=image)

        def add_port_forward(self, forward: object) -> object:
            calls.append(("forward", forward))
            return forward

        def close(self) -> None:
            calls.append(("close", None))

    monkeypatch.setattr(
        "pyneurodesk.shell.start_container_daemon",
        lambda: SimpleNamespace(
            base_url="http://127.0.0.1:1234", cache_dir=str(tmp_path / "cache")
        ),
    )
    monkeypatch.setattr("pyneurodesk.shell.connect", lambda base_url=None: FakeClient())
    monkeypatch.setattr("pyneurodesk.shell.reserve_tcp_port", lambda host: 4567)
    monkeypatch.setattr(
        "pyneurodesk.shell.wait_for_jupyter_with_log",
        lambda url, log_path, timeout_seconds, **kwargs: True,
    )
    monkeypatch.setattr(
        "pyneurodesk.shell.start_neurodesktop_jupyter_process",
        lambda base_url, image, log_path: SimpleNamespace(pid=99),
    )
    monkeypatch.setattr(
        "pyneurodesk.shell.attach_neurodesktop_session",
        lambda base_url, process, log_path: 0,
    )
    monkeypatch.setattr(
        "pyneurodesk.shell.resolve_container_reference",
        lambda client, name, mirror, repo: ContainerReference(
            name=name,
            image=name,
            source=ImageSource(
                type="cvmfs",
                mirror=mirror,
                repo=repo,
                path="/containers/neurodesktop_20260428/neurodesktop_20260428.simg",
            ),
        ),
    )

    assert shell.main(["neurodesktop", "--startup-timeout", "1"]) == 0

    import_call = next(value for name, value in calls if name == "import_image")
    assert import_call[0] == "neurodesktop"
    request = import_call[1]
    assert request.source.type == "cvmfs"
    assert (
        request.source.path
        == "/containers/neurodesktop_20260428/neurodesktop_20260428.simg"
    )


def test_resolve_neurodesktop_image_path_uses_explicit_local_image(
    tmp_path: Path,
) -> None:
    image_path = tmp_path / "neurodesktop.sif"
    image_path.write_bytes(b"sif")

    assert shell.resolve_neurodesktop_image_path(str(image_path)) == image_path


def test_preload_neurodesktop_cvmfs_streams_progress(monkeypatch) -> None:
    calls: list[tuple[str, object]] = []
    events: list[object] = []

    class FakeClient:
        def import_image_stream(self, image: str, request: object) -> object:
            calls.append(("import_image_stream", (image, request)))
            yield SimpleNamespace(
                status="prefetching",
                files_downloaded=1,
                files_total=2,
                bytes_downloaded=512,
                bytes_total=1024,
                rate_bytes_per_second=256.0,
            )

    preloaded = shell.preload_neurodesktop_cvmfs(
        FakeClient(),
        image="neurodesktop",
        reference=ContainerReference(
            name="neurodesktop",
            image="neurodesktop",
            source=ImageSource(
                type="cvmfs",
                mirror="https://cvmfs.neurodesk.org/cvmfs",
                repo="neurodesk.ardc.edu.au",
                path="/containers/neurodesktop.simg",
            ),
        ),
        workers=3,
        on_event=events.append,
    )

    assert preloaded is True
    import_call = next(value for name, value in calls if name == "import_image_stream")
    assert import_call[0] == "neurodesktop"
    request = import_call[1]
    assert request.prefetch is True
    assert request.prefetch_workers == 3
    assert request.source.type == "cvmfs"
    assert events and events[0].files_total == 2


def test_format_neurodesktop_prefetch_progress_includes_file_count_size_and_rate() -> None:
    message = shell.format_neurodesktop_prefetch_progress(
        SimpleNamespace(
            status="prefetching",
            files_downloaded=12,
            files_total=48,
            bytes_downloaded=1024,
            bytes_total=4096,
            rate_bytes_per_second=512.0,
            eta_seconds=6.0,
        )
    )

    assert "12/48 files" in message
    assert "1.0 KB/4.0 KB" in message
    assert "512 B/s" in message
    assert "ETA 6s" in message


def test_cached_cvmfs_image_can_be_used_for_neurodesktop_prefetch() -> None:
    reference = shell.neurodesktop_reference_from_cached_image(
        "neurodesktop",
        SimpleNamespace(
            source_kind="cvmfs",
            source=(
                "https://cvmfs.neurodesk.org/cvmfs/neurodesk.ardc.edu.au/"
                "containers/neurodesktop_20260428/neurodesktop_20260428.simg"
            ),
        ),
    )

    assert reference is not None
    assert reference.source.type == "cvmfs"
    assert reference.source.repo == "neurodesk.ardc.edu.au"
    assert (
        reference.source.path
        == "/containers/neurodesktop_20260428/neurodesktop_20260428.simg"
    )


def test_shell_exec_passes_user_override(monkeypatch) -> None:
    calls: list[tuple[str, object]] = []

    def fake_run_image_command(
        image: str,
        command_name: str,
        args: list[str],
        *,
        deploy_env: object = None,
        user: Optional[str] = None,
    ) -> int:
        calls.append(("run", (image, command_name, args, deploy_env, user)))
        return 7

    monkeypatch.setattr("pyneurodesk.shell.run_image_command", fake_run_image_command)

    assert (
        shell.main(["shell", "exec", "--user", "root", "neurodesktop", "--", "id"]) == 7
    )
    assert calls == [("run", ("neurodesktop", "id", [], None, "root"))]


def test_neurodesktop_jupyter_process_preserves_shell_subcommand(
    monkeypatch, tmp_path: Path
) -> None:
    seen: dict[str, object] = {}
    log_path = tmp_path / "jupyter.log"
    log_path.write_text("old log\n")

    class FakePopen:
        pid = 123

    def fake_popen(cmd: list[str], **kwargs: object) -> FakePopen:
        seen["cmd"] = cmd
        seen["kwargs"] = kwargs
        return FakePopen()

    monkeypatch.setattr("pyneurodesk.shell.subprocess.Popen", fake_popen)

    proc = shell.start_neurodesktop_jupyter_process(
        "http://127.0.0.1:1234",
        image="neurodesktop",
        log_path=log_path,
    )

    assert proc.pid == 123
    assert log_path.read_text() == ""
    cmd = seen["cmd"]
    assert cmd[0] == shell.sys.executable
    assert cmd[1] == "-c"
    assert cmd[3:5] == ["shell", "neurodesktop-server"]
    assert cmd[5:] == [
        "--base-url",
        "http://127.0.0.1:1234",
        "--image",
        "neurodesktop",
        "--log",
        str(log_path),
    ]


def test_neurodesktop_server_runs_jupyter_as_jovyan(
    monkeypatch, tmp_path: Path
) -> None:
    calls: list[tuple[str, object]] = []
    log_path = tmp_path / "jupyter.log"

    class FakeClient:
        def run_stream(self, image: str, command: list[str], **kwargs: object) -> object:
            calls.append(("run_stream", (image, command, kwargs)))
            if kwargs.get("user") == "0:0":
                yield {"kind": "stdout", "output": "preflight\n"}
            else:
                yield {"kind": "stdout", "output": "started\n"}
            yield {"kind": "exit", "exit_code": 0}

        def close(self) -> None:
            calls.append(("close", None))

    monkeypatch.setattr(
        "pyneurodesk.shell.connect",
        lambda base_url: calls.append(("connect", base_url)) or FakeClient(),
    )

    exit_code = shell.handle_neurodesktop_server(
        SimpleNamespace(
            base_url="http://127.0.0.1:1234",
            image="neurodesktop",
            log=str(log_path),
        )
    )

    assert exit_code == 0
    assert ("watchdog", "http://127.0.0.1:1234") not in calls
    run_calls = [value for name, value in calls if name == "run_stream"]
    assert len(run_calls) == 2
    preflight_call = run_calls[0]
    assert preflight_call[0] == "neurodesktop"
    assert "chown -R 1000:100 /home/jovyan" in preflight_call[1][2]
    assert "NEURODESKTOP_PROXY_TIMEOUT_SECONDS" in preflight_call[1][2]
    assert "neurodesktop/index.html" in preflight_call[1][2]
    assert "PYNEURODESK_GUACAMOLE_DISABLE_SFTP" in preflight_call[1][2]
    assert preflight_call[2]["user"] == "0:0"
    assert preflight_call[2]["workdir"] == "/"
    run_call = run_calls[1]
    assert run_call[0] == "neurodesktop"
    assert "HOME=/home/jovyan" in run_call[1][2]
    assert "NEURODESKTOP_PROXY_TIMEOUT_SECONDS" in run_call[1][2]
    assert run_call[2]["user"] == "1000:100"
    assert run_call[2]["workdir"] == "/home/jovyan"
    assert log_path.read_text() == "preflight\nstarted\n"


def test_write_exec_event_to_log_keeps_nonzero_exit(tmp_path: Path) -> None:
    log_path = tmp_path / "exec.log"

    with log_path.open("ab") as log:
        shell.write_exec_event_to_log(log, {"kind": "exit", "exit_code": 0})
        shell.write_exec_event_to_log(log, {"kind": "exit", "exit_code": 3})

    assert log_path.read_text() == '{"exit_code": 3, "kind": "exit"}\n'


def test_emit_log_since_prints_new_log_bytes(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    log_path = tmp_path / "jupyter.log"
    log_path.write_text("first\n")

    offset = shell.emit_log_since(log_path, 0)
    assert offset == len("first\n")
    assert capsys.readouterr().out == "first\n"

    with log_path.open("a") as log:
        log.write("second\n")

    offset = shell.emit_log_since(log_path, offset)
    assert offset == len("first\nsecond\n")
    assert capsys.readouterr().out == "second\n"


def test_emit_log_since_filters_successful_exit_events(
    tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    log_path = tmp_path / "jupyter.log"
    log_path.write_text(
        'before\n{"kind": "exit"}\n{"exit_code": 0, "kind": "exit"}\n'
        '{"exit_code": 3, "kind": "exit"}\nafter\n'
    )

    offset = shell.emit_log_since(log_path, 0)

    assert offset == log_path.stat().st_size
    assert capsys.readouterr().out == (
        'before\n{"exit_code": 3, "kind": "exit"}\nafter\n'
    )


def test_wait_for_jupyter_extends_while_log_changes(
    monkeypatch, tmp_path: Path
) -> None:
    log_path = tmp_path / "jupyter.log"
    log_path.write_text("")
    now = 0.0

    class FakeClient:
        def __init__(self, *args: object, **kwargs: object) -> None:
            pass

        def __enter__(self) -> "FakeClient":
            return self

        def __exit__(self, *args: object) -> None:
            pass

        def get(self, url: str) -> object:
            return SimpleNamespace(status_code=503)

    def fake_monotonic() -> float:
        return now

    def fake_sleep(seconds: float) -> None:
        nonlocal now
        now += seconds
        if now in (0.5, 1.0):
            with log_path.open("a") as log:
                log.write(f"still starting {now}\n")

    monkeypatch.setattr(shell.httpx, "Client", FakeClient)
    monkeypatch.setattr(shell.time, "monotonic", fake_monotonic)
    monkeypatch.setattr(shell.time, "sleep", fake_sleep)

    ready = shell.wait_for_jupyter_with_log(
        "http://127.0.0.1:8888/api/status",
        log_path=log_path,
        timeout_seconds=1.0,
        poll_interval=0.5,
    )

    assert ready is False
    assert now >= 2.0


def test_wait_for_jupyter_extends_while_cvmfs_cache_changes(
    monkeypatch, tmp_path: Path
) -> None:
    log_path = tmp_path / "jupyter.log"
    log_path.write_text("")
    now = 0.0
    activity = (0, 0)

    class FakeClient:
        def __init__(self, *args: object, **kwargs: object) -> None:
            pass

        def __enter__(self) -> "FakeClient":
            return self

        def __exit__(self, *args: object) -> None:
            pass

        def get(self, url: str) -> object:
            return SimpleNamespace(status_code=503)

    def fake_monotonic() -> float:
        return now

    def fake_sleep(seconds: float) -> None:
        nonlocal activity, now
        now += seconds
        if now in (0.5, 1.0):
            activity = (activity[0] + 1, activity[1] + 1024)

    monkeypatch.setattr(shell.httpx, "Client", FakeClient)
    monkeypatch.setattr(shell.time, "monotonic", fake_monotonic)
    monkeypatch.setattr(shell.time, "sleep", fake_sleep)

    ready = shell.wait_for_jupyter_with_log(
        "http://127.0.0.1:8888/api/status",
        log_path=log_path,
        timeout_seconds=1.0,
        activity_snapshot=lambda: activity,
        poll_interval=0.5,
    )

    assert ready is False
    assert now >= 2.0


def test_wait_for_jupyter_stops_when_launcher_exits(
    monkeypatch, tmp_path: Path, capsys: pytest.CaptureFixture[str]
) -> None:
    log_path = tmp_path / "jupyter.log"
    log_path.write_text("failed\n")

    class FakeClient:
        def __init__(self, *args: object, **kwargs: object) -> None:
            pass

        def __enter__(self) -> "FakeClient":
            return self

        def __exit__(self, *args: object) -> None:
            pass

        def get(self, url: str) -> object:
            return SimpleNamespace(status_code=503)

    class FakeProcess:
        def poll(self) -> int:
            return 126

    def fail_sleep(seconds: float) -> None:
        raise AssertionError("wait loop should exit before sleeping")

    monkeypatch.setattr(shell.httpx, "Client", FakeClient)
    monkeypatch.setattr(shell.time, "sleep", fail_sleep)

    ready = shell.wait_for_jupyter_with_log(
        "http://127.0.0.1:8888/api/status",
        log_path=log_path,
        timeout_seconds=180.0,
        process=FakeProcess(),
    )

    assert ready is False
    assert capsys.readouterr().out == "failed\n"


def test_attach_neurodesktop_session_waits_for_launcher_exit(
    monkeypatch, tmp_path: Path
) -> None:
    log_path = tmp_path / "jupyter.log"
    log_path.write_text("ready\n")
    sleeps: list[float] = []

    class FakeProcess:
        pid = 99

        def __init__(self) -> None:
            self.polls = 0

        def poll(self) -> Optional[int]:
            self.polls += 1
            return None if self.polls == 1 else 0

    def fake_sleep(seconds: float) -> None:
        sleeps.append(seconds)
        log_path.write_text("ready\nlater\n")

    monkeypatch.setattr(shell.time, "sleep", fake_sleep)

    exit_code = shell.attach_neurodesktop_session(
        "http://127.0.0.1:1234",
        process=FakeProcess(),
        log_path=log_path,
        poll_interval=0.25,
    )

    assert exit_code == 0
    assert sleeps == [0.25]


def test_attach_neurodesktop_session_ctrl_c_shuts_down_daemon(
    monkeypatch, tmp_path: Path
) -> None:
    log_path = tmp_path / "jupyter.log"
    log_path.write_text("")
    calls: list[object] = []

    class FakeProcess:
        pid = 99

        def poll(self) -> None:
            return None

        def wait(self, timeout: float) -> int:
            calls.append(("wait", timeout))
            return 0

        def terminate(self) -> None:
            calls.append("terminate")

    def fake_sleep(seconds: float) -> None:
        raise KeyboardInterrupt

    monkeypatch.setattr(shell.time, "sleep", fake_sleep)
    monkeypatch.setattr(
        shell, "_shutdown_daemon_server", lambda base_url: calls.append(base_url)
    )

    exit_code = shell.attach_neurodesktop_session(
        "http://127.0.0.1:1234",
        process=FakeProcess(),
        log_path=log_path,
        poll_interval=0.25,
    )

    assert exit_code == 130
    assert calls == ["http://127.0.0.1:1234", ("wait", 10.0)]


def test_watchdog_cvmfs_activity_snapshot_reads_daemon_counter(monkeypatch) -> None:
    class FakeResponse:
        def raise_for_status(self) -> None:
            pass

        def json(self) -> object:
            return {"cvmfs": {"events": 3, "bytes": 4096}}

    class FakeClient:
        def __init__(self, *args: object, **kwargs: object) -> None:
            pass

        def __enter__(self) -> "FakeClient":
            return self

        def __exit__(self, *args: object) -> None:
            pass

        def get(self, url: str) -> object:
            assert url == "/watchdog/activity"
            return FakeResponse()

    monkeypatch.setattr(shell.httpx, "Client", FakeClient)

    assert shell.watchdog_cvmfs_activity_snapshot("http://127.0.0.1:1234") == (3, 4096)


def test_run_wrapper_invokes_container_command(
    monkeypatch,
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    calls: list[tuple[str, object]] = []

    session_root = tmp_path / "session"
    session_root.mkdir(parents=True)
    shell.write_state(
        shell.SessionState(
            session_id="sess-1",
            root=session_root,
            images={"niimath": {"deploy_env": ["DEPLOY_ENV_FSLDIR=/opt/fsl"]}},
            wrappers={},
        )
    )
    monkeypatch.setenv(shell.SESSION_ENV, "sess-1")
    monkeypatch.setenv(shell.SESSION_ROOT_ENV, str(session_root))
    monkeypatch.setenv(shell.SESSION_BIN_ENV, str(session_root / "bin"))
    monkeypatch.chdir(tmp_path)
    home_dir = tmp_path / "home"
    home_dir.mkdir()
    monkeypatch.setenv("HOME", str(home_dir))
    monkeypatch.setenv("USERPROFILE", str(home_dir))

    class FakeClient:
        def run(
            self,
            image: str,
            command: list[str],
            *,
            env: list[str] = (),
            shares: list[object] = (),
            workdir: Optional[str] = None,
        ) -> object:
            calls.append(
                ("run", (image, tuple(command), tuple(env), tuple(shares), workdir))
            )
            return SimpleNamespace(exit_code=0, output="hello\n")

    class FakeHandle:
        def __init__(self) -> None:
            self._client = FakeClient()
            self.reference = SimpleNamespace(image="niimath")

        def close(self) -> None:
            calls.append(("close", None))

    monkeypatch.setattr(
        "pyneurodesk.shell.container", lambda image, progress=False: FakeHandle()
    )

    exit_code = shell.main(
        [
            "shell",
            "run-wrapper",
            "--session",
            "sess-1",
            "--image",
            "niimath",
            "--command",
            "niimath",
            "--",
            "-help",
        ]
    )

    assert exit_code == 0
    assert capsys.readouterr().out == "hello\n"
    guest_mount = f"{shell.HOST_CWD_MOUNT_ROOT}/{hashlib.sha256(str(tmp_path.resolve()).encode('utf-8')).hexdigest()[:16]}"
    expected_share = shell.ShareMount(
        source=str(tmp_path.resolve()), mount=guest_mount, writable=True
    )
    assert calls == [
        (
            "run",
            (
                "niimath",
                ("niimath", "-help"),
                (
                    "DEPLOY_ENV_FSLDIR=/opt/fsl",
                    "FSLDIR=/opt/fsl",
                    "HOME=/tmp",
                    "XDG_CACHE_HOME=/tmp",
                    "NUMBA_CACHE_DIR=/tmp/numba-cache",
                    "APPTAINER_CACHEDIR=/tmp/.apptainer/cache",
                    "APPTAINER_CONFIGDIR=/tmp/.apptainer",
                    "SINGULARITY_CACHEDIR=/tmp/.apptainer/cache",
                    "SINGULARITY_CONFIGDIR=/tmp/.apptainer",
                ),
                (expected_share,),
                guest_mount,
            ),
        ),
        ("close", None),
    ]


def test_shell_command_with_runtime_env_exports_into_login_shell() -> None:
    command = shell.shell_command_with_runtime_env(
        ["bash", "-lc", "command -v vina"],
        [
            "DEPLOY_PATH=/opt/vina",
            "PATH=/opt/vina:/usr/bin:/bin",
            "INVALID-NAME=skip",
        ],
    )

    assert command == [
        "bash",
        "-lc",
        "export DEPLOY_PATH=/opt/vina\nexport PATH=/opt/vina:/usr/bin:/bin\ncommand -v vina",
    ]


def test_shell_command_with_runtime_env_leaves_direct_command_unchanged() -> None:
    assert shell.shell_command_with_runtime_env(
        ["vina", "--version"],
        ["PATH=/opt/vina:/usr/bin:/bin"],
    ) == ["vina", "--version"]


def test_run_wrapper_stream_preserves_stdout_stderr_and_binary_data(
    monkeypatch,
    capsysbinary: pytest.CaptureFixture[bytes],
) -> None:
    monkeypatch.setenv("PYNEURODESK_EXEC_STREAM", "1")
    calls: list[tuple[str, object]] = []

    class FakeClient:
        def run_stream(
            self,
            image: str,
            command: list[str],
            *,
            env: list[str] = (),
            shares: list[object] = (),
            workdir: Optional[str] = None,
            user: Optional[str] = None,
        ) -> object:
            calls.append(
                (
                    "run_stream",
                    (image, tuple(command), tuple(env), tuple(shares), workdir, user),
                )
            )
            return iter(
                [
                    {"kind": "stdout", "output": "hello\n"},
                    {"kind": "stderr", "output": "warn\n"},
                    {"kind": "stdout", "data": base64.b64encode(b"\x00bin\n").decode()},
                    {
                        "kind": "output",
                        "stream": "stderr",
                        "data": base64.b64encode(b"rawerr\n").decode(),
                    },
                    {"kind": "exit", "exit_code": 3},
                ]
            )

    class FakeHandle:
        def __init__(self) -> None:
            self._client = FakeClient()
            self.reference = SimpleNamespace(image="niimath")

        def close(self) -> None:
            calls.append(("close", None))

    monkeypatch.setattr(
        "pyneurodesk.shell.shell_session_container", lambda image: FakeHandle()
    )
    monkeypatch.setattr(
        "pyneurodesk.shell.load_deploy_metadata",
        lambda handle: SimpleNamespace(deploy_env=()),
    )
    monkeypatch.setattr("pyneurodesk.shell.implicit_runtime_mounts", lambda: ([], None))
    monkeypatch.setattr("pyneurodesk.shell.runtime_env_overrides", lambda: [])

    exit_code = shell.run_image_command("niimath", "niimath", ["-help"])

    captured = capsysbinary.readouterr()
    assert exit_code == 3
    assert captured.out == b"hello\n\x00bin\n"
    assert captured.err == b"warn\nrawerr\n"
    assert calls == [
        ("run_stream", ("niimath", ("niimath", "-help"), (), (), None, None)),
        ("close", None),
    ]


def test_run_wrapper_uses_session_reference_when_present(
    monkeypatch,
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    calls: list[tuple[str, object]] = []
    reference = ContainerReference(
        name="suite",
        image="fulltest-image",
        source=ImageSource(type="simg", path="/tmp/container.simg"),
        cache_dir="/tmp/cache",
    )
    session_root = tmp_path / "session"
    session_root.mkdir(parents=True)
    shell.write_state(
        shell.SessionState(
            session_id="sess-1",
            root=session_root,
            images={
                "fulltest-image": {
                    "deploy_env": ["PATH=/opt/tool:/opt/base"],
                    "memory_mb": 512,
                    "cpus": 2,
                    "reference": shell.container_reference_to_payload(reference),
                }
            },
            wrappers={},
        )
    )
    monkeypatch.setenv(shell.SESSION_ENV, "sess-1")
    monkeypatch.setenv(shell.SESSION_ROOT_ENV, str(session_root))
    monkeypatch.setenv(shell.SESSION_BIN_ENV, str(session_root / "bin"))
    monkeypatch.chdir(tmp_path)
    home_dir = tmp_path / "home"
    home_dir.mkdir()
    monkeypatch.setenv("HOME", str(home_dir))
    monkeypatch.setenv("USERPROFILE", str(home_dir))

    class FakeClient:
        def ensure_image(self, ref: ContainerReference) -> object:
            calls.append(("ensure_image", ref))
            return object()

        def ensure_instance(
            self,
            image: str,
            *,
            memory_mb: Optional[int] = None,
            cpus: Optional[int] = None,
            **kwargs: object,
        ) -> object:
            calls.append(("ensure_instance", (image, memory_mb, cpus)))
            return object()

        def run(
            self,
            image: str,
            command: list[str],
            *,
            env: list[str] = (),
            shares: list[object] = (),
            workdir: Optional[str] = None,
        ) -> object:
            calls.append(
                ("run", (image, tuple(command), tuple(env), tuple(shares), workdir))
            )
            return SimpleNamespace(exit_code=0, output="ok\n")

        def close(self) -> None:
            calls.append(("client_close", None))

    monkeypatch.setattr(
        "pyneurodesk.shell.connect",
        lambda: calls.append(("connect", None)) or FakeClient(),
    )

    exit_code = shell.main(
        [
            "shell",
            "run-wrapper",
            "--session",
            "sess-1",
            "--image",
            "fulltest-image",
            "--command",
            "niimath",
            "--",
            "-help",
        ]
    )

    assert exit_code == 0
    assert capsys.readouterr().out == "ok\n"
    assert calls[0:3] == [
        ("connect", None),
        ("ensure_image", reference),
        ("ensure_instance", ("fulltest-image", 512, 2)),
    ]
    guest_mount = f"{shell.HOST_CWD_MOUNT_ROOT}/{hashlib.sha256(str(tmp_path.resolve()).encode('utf-8')).hexdigest()[:16]}"
    expected_share = shell.ShareMount(
        source=str(tmp_path.resolve()), mount=guest_mount, writable=True
    )
    assert calls[3] == (
        "run",
        (
            "fulltest-image",
            ("niimath", "-help"),
            (
                "PATH=/opt/tool:/opt/base",
                "HOME=/tmp",
                "XDG_CACHE_HOME=/tmp",
                "NUMBA_CACHE_DIR=/tmp/numba-cache",
                "APPTAINER_CACHEDIR=/tmp/.apptainer/cache",
                "APPTAINER_CONFIGDIR=/tmp/.apptainer",
                "SINGULARITY_CACHEDIR=/tmp/.apptainer/cache",
                "SINGULARITY_CONFIGDIR=/tmp/.apptainer",
            ),
            (expected_share,),
            guest_mount,
        ),
    )


def test_run_wrapper_uses_session_vm_id(
    monkeypatch,
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    calls: list[tuple[str, object]] = []
    reference = ContainerReference(
        name="suite",
        image="fulltest-image",
        source=ImageSource(type="simg", path="/tmp/container.simg"),
    )
    session_root = tmp_path / "session"
    session_root.mkdir(parents=True)
    shell.write_state(
        shell.SessionState(
            session_id="sess-1",
            root=session_root,
            images={
                "fulltest-image": {
                    "reference": shell.container_reference_to_payload(reference)
                }
            },
            wrappers={},
            vm_id="analysis",
        )
    )
    monkeypatch.setenv(shell.SESSION_ENV, "sess-1")
    monkeypatch.setenv(shell.SESSION_ROOT_ENV, str(session_root))
    monkeypatch.setenv(shell.SESSION_BIN_ENV, str(session_root / "bin"))
    monkeypatch.chdir(tmp_path)

    class FakeClient:
        def ensure_image(self, ref: ContainerReference) -> object:
            calls.append(("ensure_image", ref))
            return object()

        def ensure_instance(self, image: str, **kwargs: object) -> object:
            calls.append(("ensure_instance", (image, kwargs)))
            return object()

        def run(self, image: str, command: list[str], **kwargs: object) -> object:
            calls.append(("run", (image, tuple(command), kwargs)))
            return SimpleNamespace(exit_code=0, output="ok\n")

        def close(self) -> None:
            calls.append(("client_close", None))

    monkeypatch.setattr(
        "pyneurodesk.shell.connect",
        lambda: calls.append(("connect", None)) or FakeClient(),
    )
    monkeypatch.setattr(
        "pyneurodesk.shell.load_deploy_metadata",
        lambda handle: SimpleNamespace(deploy_env=()),
    )
    monkeypatch.setattr("pyneurodesk.shell.runtime_env_overrides", lambda: [])

    exit_code = shell.main(
        [
            "shell",
            "run-wrapper",
            "--session",
            "sess-1",
            "--image",
            "fulltest-image",
            "--command",
            "niimath",
            "--",
            "-help",
        ]
    )

    assert exit_code == 0
    assert capsys.readouterr().out == "ok\n"
    ensure_call = next(value for name, value in calls if name == "ensure_instance")
    assert ensure_call[1]["vm_id"] == "analysis"
    run_call = next(value for name, value in calls if name == "run")
    assert run_call[2]["vm_id"] == "analysis"
    assert calls[-1] == ("client_close", None)
