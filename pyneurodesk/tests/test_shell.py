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
    pyproject = tomllib.loads((Path(__file__).parents[1] / "pyproject.toml").read_text())

    assert pyproject["project"]["scripts"]["nd"] == "pyneurodesk:main"


def test_activate_emits_shell_code_initializes_session_and_bootstraps_by_default(
    monkeypatch,
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    monkeypatch.setattr("pyneurodesk.shell.default_cache_root", lambda: tmp_path / "cache")

    exit_code = shell.main(["activate", "--shell", "bash"])

    assert exit_code == 0
    output = capsys.readouterr().out
    assert "PYNEURODESK_SHELL_SESSION" in output
    assert "PYNEURODESK_SHELL_BOOTSTRAP_PID" in output
    assert "command neurodesk shell bootstrap >/dev/null 2>&1 &" in output
    assert 'if [ "$#" -eq 0 ]; then' in output
    assert 'command neurodesk shell --help' in output
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


def test_activate_supports_no_bootstrap(monkeypatch, tmp_path: Path, capsys: pytest.CaptureFixture[str]) -> None:
    monkeypatch.setattr("pyneurodesk.shell.default_cache_root", lambda: tmp_path / "cache")

    exit_code = shell.main(["activate", "--shell", "bash", "--no-bootstrap"])

    assert exit_code == 0
    output = capsys.readouterr().out
    assert "shell bootstrap" not in output
    assert "PYNEURODESK_SHELL_BOOTSTRAP_PID" in output


def test_activate_with_network_persists_session_network(monkeypatch, tmp_path: Path) -> None:
    monkeypatch.setattr("pyneurodesk.shell.default_cache_root", lambda: tmp_path / "cache")

    assert shell.main(["activate", "--shell", "bash", "--no-bootstrap", "--with-network"]) == 0

    session_dirs = list((tmp_path / "cache" / "pyneurodesk-shell").iterdir())
    state = json.loads((session_dirs[0] / "state.json").read_text())
    assert state["network"] == {"enabled": True}


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
    monkeypatch.setattr("pyneurodesk.shell.default_cache_root", lambda: tmp_path / "cache")

    exit_code = shell.main(["activate", "--shell", "powershell", "--no-bootstrap"])

    assert exit_code == 0
    output = capsys.readouterr().out
    assert "$env:PYNEURODESK_SHELL_SESSION" in output
    assert "$env:PYNEURODESK_SHELL_ROOT" in output
    assert "$env:PYNEURODESK_SHELL_BIN" in output
    assert "$env:PATH = \"$env:PYNEURODESK_SHELL_BIN;$env:PATH\"" in output
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


def test_completion_emits_powershell_support(capsys: pytest.CaptureFixture[str]) -> None:
    exit_code = shell.main(["completion", "--shell", "powershell"])

    assert exit_code == 0
    output = capsys.readouterr().out
    assert "Register-ArgumentCompleter -CommandName neurodesk,nd" in output
    assert "neurodesk shell complete --index $index -- @words" in output
    assert "CompletionResult" in output


def test_activate_defaults_to_powershell_on_windows(
    monkeypatch,
    tmp_path: Path,
    capsys: pytest.CaptureFixture[str],
) -> None:
    monkeypatch.setattr("pyneurodesk.shell.is_windows_host", lambda: True)
    monkeypatch.setattr("pyneurodesk.shell.default_cache_root", lambda: tmp_path / "cache")

    assert shell.main(["activate", "--no-bootstrap"]) == 0

    output = capsys.readouterr().out
    assert "$env:PYNEURODESK_SHELL_SESSION" in output
    assert "Register-ArgumentCompleter -CommandName neurodesk,nd" in output


def test_pwsh_alias_uses_powershell_renderer(capsys: pytest.CaptureFixture[str]) -> None:
    exit_code = shell.main(["completion", "--shell", "pwsh"])

    assert exit_code == 0
    assert "Register-ArgumentCompleter -CommandName neurodesk,nd" in capsys.readouterr().out


def test_shell_bootstrap_starts_default_daemon(monkeypatch) -> None:
    calls: list[tuple[str, object]] = []

    monkeypatch.setattr(
        "pyneurodesk.shell.start_default_daemon",
        lambda: calls.append(("boot", None)) or SimpleNamespace(base_url="http://daemon.test"),
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
                payload = base64.b64encode(b"DEPLOY_ENV_FSLDIR=BASEPATH/opt/fsl\n").decode()
            elif request.path.endswith("/build.yaml"):
                payload = base64.b64encode(b"").decode()
            else:
                raise AssertionError(request.path)
            return SimpleNamespace(data=payload.encode())

        def prepare_image_metadata(self, image: str) -> object:
            calls.append(("prepare_image_metadata", image))
            return SimpleNamespace(env=["PATH=/usr/local/bin:/usr/bin:/bin:/opt/niimath"])

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

    monkeypatch.setattr("pyneurodesk.shell.container", lambda image, progress=False: FakeHandle())
    monkeypatch.setattr("pyneurodesk.shell.resolve_command_name", lambda: "/usr/local/bin/neurodesk")

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
        assert '--image niimath' in wrapper
        assert '--command niimath' in wrapper
    assert calls[0] == ("cvmfs_list", "/containers/niimath_1.0.20250804_20251016")
    assert calls[-1] == ("close", None)


def test_write_cmd_wrapper_generates_windows_native_shim(monkeypatch, tmp_path: Path) -> None:
    monkeypatch.setattr("pyneurodesk.shell.resolve_command_name", lambda: r"C:\Program Files\Neurodesk\neurodesk.exe")
    path = tmp_path / "bin" / "niimath.cmd"

    shell.write_cmd_wrapper(path, image="niimath", command="niimath")

    wrapper = path.read_text()
    assert wrapper.startswith("@echo off")
    assert r'"C:\Program Files\Neurodesk\neurodesk.exe" shell run-wrapper' in wrapper
    assert f'--session "%{shell.SESSION_ENV}%"' in wrapper
    assert '--image "niimath"' in wrapper
    assert '--command "niimath"' in wrapper
    assert "-- %*" in wrapper


def test_wrapper_path_uses_cmd_extension_on_windows(monkeypatch, tmp_path: Path) -> None:
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
    shell.write_state(shell.SessionState(session_id="sess-1", root=root, images={}, wrappers={}))
    monkeypatch.chdir(tmp_path)

    calls: list[tuple[str, object]] = []

    class FakeClient:
        def import_image(self, name: str, request: object) -> object:
            calls.append(("import_image", (name, request.source.path, request.cache_dir)))
            return object()

        def ensure_instance(self, image: str, *, memory_mb: Optional[int] = None, cpus: Optional[int] = None) -> object:
            calls.append(("ensure_instance", (image, memory_mb, cpus)))
            return object()

        def run(self, *args: object, **kwargs: object) -> object:
            calls.append(("run", (args, kwargs)))
            return SimpleNamespace(exit_code=0, output="tool ok\n")

        def close(self) -> None:
            calls.append(("client_close", None))

    monkeypatch.setattr("pyneurodesk.shell.connect", lambda: calls.append(("connect", None)) or FakeClient())
    monkeypatch.setattr("pyneurodesk.shell.load_deploy_metadata", lambda handle: SimpleNamespace(commands=("tool",), deploy_env=("A=B",)))
    monkeypatch.setattr("pyneurodesk.shell.resolve_command_name", lambda: "/usr/local/bin/neurodesk")

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
    assert sorted(state.images["fulltest-image"]["commands"]) == ["fulltest-image", "sh", "tool"]
    assert state.images["fulltest-image"]["deploy_env"] == ["A=B"]
    assert state.images["fulltest-image"]["memory_mb"] == 512
    assert state.images["fulltest-image"]["cpus"] == 2
    assert state.images["fulltest-image"]["reference"]["source"]["path"] == "/containers/custom"
    assert (root / "env.sh").read_text() == "# Generated by pyneurodesk. Source through `neurodesk activate`.\nexport A=B\n"
    assert calls[:3] == [
        ("connect", None),
        ("import_image", ("fulltest-image", "/containers/custom", "/tmp/cache")),
        ("ensure_instance", ("fulltest-image", 512, 2)),
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
            self.reference = SimpleNamespace(path="/containers/another/another.simg", cache_dir=None)

        def close(self) -> None:
            return None

    monkeypatch.setattr("pyneurodesk.shell.container", lambda image, progress=False: FakeHandle())

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
            self.reference = SimpleNamespace(path="/containers/another/another.simg", cache_dir=None)

        def close(self) -> None:
            return None

    monkeypatch.setattr("pyneurodesk.shell.container", lambda image, progress=False: FakeHandle())

    assert shell.main(["shell", "load", "another", "--force"]) == 0
    state = shell.read_state(root, session_id="sess-1")
    assert state.wrappers["bet"] == shell.WrapperSpec(image="another", command="bet")

    assert shell.main(["shell", "unload", "another"]) == 0
    state = shell.read_state(root, session_id="sess-1")
    assert state.wrappers["bet"] == shell.WrapperSpec(image="fsl", command="bet")


def test_shell_unload_removes_image_wrappers(monkeypatch, tmp_path: Path, capsys: pytest.CaptureFixture[str]) -> None:
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


def test_shell_complete_returns_loaded_images_for_exec(monkeypatch, tmp_path: Path, capsys: pytest.CaptureFixture[str]) -> None:
    root = tmp_path / "session"
    root.mkdir()
    monkeypatch.setenv(shell.SESSION_ENV, "sess-1")
    monkeypatch.setenv(shell.SESSION_ROOT_ENV, str(root))
    monkeypatch.setenv(shell.SESSION_BIN_ENV, str(root / "bin"))
    shell.write_state(
        shell.SessionState(
            session_id="sess-1",
            root=root,
            images={"fsl": {"commands": ["bet"], "deploy_env": []}, "niimath": {"commands": ["niimath"], "deploy_env": []}},
            wrappers={},
        )
    )

    exit_code = shell.main(["shell", "complete", "--index", "3", "--", "nd", "exec", ""])

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
    shell.write_state(shell.SessionState(session_id="sess-1", root=root, images={}, wrappers={}))

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
            calls.append(("run", (image, tuple(command), tuple(env), tuple(shares), workdir)))
            return SimpleNamespace(exit_code=0, output="hello\n")

    class FakeHandle:
        def __init__(self) -> None:
            self._client = FakeClient()
            self.reference = SimpleNamespace(image="niimath")

        def close(self) -> None:
            calls.append(("close", None))

    monkeypatch.setattr("pyneurodesk.shell.container", lambda image, progress=False: FakeHandle())

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
    expected_share = shell.ShareMount(source=str(tmp_path.resolve()), mount=guest_mount, writable=True)
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
        ) -> object:
            calls.append(("run_stream", (image, tuple(command), tuple(env), tuple(shares), workdir)))
            return iter(
                [
                    {"kind": "stdout", "output": "hello\n"},
                    {"kind": "stderr", "output": "warn\n"},
                    {"kind": "stdout", "data": base64.b64encode(b"\x00bin\n").decode()},
                    {"kind": "output", "stream": "stderr", "data": base64.b64encode(b"rawerr\n").decode()},
                    {"kind": "exit", "exit_code": 3},
                ]
            )

    class FakeHandle:
        def __init__(self) -> None:
            self._client = FakeClient()
            self.reference = SimpleNamespace(image="niimath")

        def close(self) -> None:
            calls.append(("close", None))

    monkeypatch.setattr("pyneurodesk.shell.shell_session_container", lambda image: FakeHandle())
    monkeypatch.setattr("pyneurodesk.shell.load_deploy_metadata", lambda handle: SimpleNamespace(deploy_env=()))
    monkeypatch.setattr("pyneurodesk.shell.implicit_runtime_mounts", lambda: ([], None))
    monkeypatch.setattr("pyneurodesk.shell.runtime_env_overrides", lambda: [])

    exit_code = shell.run_image_command("niimath", "niimath", ["-help"])

    captured = capsysbinary.readouterr()
    assert exit_code == 3
    assert captured.out == b"hello\n\x00bin\n"
    assert captured.err == b"warn\nrawerr\n"
    assert calls == [
        ("run_stream", ("niimath", ("niimath", "-help"), (), (), None)),
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

        def ensure_instance(self, image: str, *, memory_mb: Optional[int] = None, cpus: Optional[int] = None) -> object:
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
            calls.append(("run", (image, tuple(command), tuple(env), tuple(shares), workdir)))
            return SimpleNamespace(exit_code=0, output="ok\n")

        def close(self) -> None:
            calls.append(("client_close", None))

    monkeypatch.setattr("pyneurodesk.shell.connect", lambda: calls.append(("connect", None)) or FakeClient())

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
    expected_share = shell.ShareMount(source=str(tmp_path.resolve()), mount=guest_mount, writable=True)
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
    assert calls[-1] == ("client_close", None)
