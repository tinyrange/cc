from __future__ import annotations

import base64
import json
from pathlib import Path
from types import SimpleNamespace

import httpx
import pytest

from pyneurodesk import shell


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


def test_completion_emits_zsh_support(capsys: pytest.CaptureFixture[str]) -> None:
    exit_code = shell.main(["completion", "--shell", "zsh"])

    assert exit_code == 0
    output = capsys.readouterr().out
    assert "compdef _neurodesk_complete neurodesk" in output
    assert "compdef _nd_complete nd" in output


def test_shell_bootstrap_starts_default_daemon(monkeypatch) -> None:
    calls: list[tuple[str, object]] = []

    class FakeClient:
        def __enter__(self) -> "FakeClient":
            return self

        def __exit__(self, exc_type: object, exc: object, tb: object) -> None:
            return None

        def instance_status(self) -> object:
            calls.append(("status", None))
            return SimpleNamespace(status="stopped")

        def start_instance(self) -> object:
            calls.append(("start_instance", None))
            return SimpleNamespace(status="running")

    monkeypatch.setattr(
        "pyneurodesk.shell.start_default_daemon",
        lambda: calls.append(("boot", None)) or SimpleNamespace(base_url="http://daemon.test"),
    )
    monkeypatch.setattr("pyneurodesk.shell.connect", lambda base_url: calls.append(("connect", base_url)) or FakeClient())

    exit_code = shell.main(["shell", "bootstrap"])

    assert exit_code == 0
    assert calls == [
        ("boot", None),
        ("connect", "http://daemon.test"),
        ("status", None),
        ("start_instance", None),
    ]


def test_shell_bootstrap_ignores_missing_vm_start_on_existing_daemon(monkeypatch) -> None:
    calls: list[tuple[str, object]] = []

    class FakeClient:
        def __enter__(self) -> "FakeClient":
            return self

        def __exit__(self, exc_type: object, exc: object, tb: object) -> None:
            return None

        def instance_status(self) -> object:
            calls.append(("status", None))
            return SimpleNamespace(status="stopped")

        def start_instance(self) -> object:
            calls.append(("start_instance", None))
            request = httpx.Request("POST", "http://daemon.test/vm/start")
            response = httpx.Response(404, request=request)
            raise httpx.HTTPStatusError("missing", request=request, response=response)

    monkeypatch.setattr(
        "pyneurodesk.shell.start_default_daemon",
        lambda: calls.append(("boot", None)) or SimpleNamespace(base_url="http://daemon.test"),
    )
    monkeypatch.setattr("pyneurodesk.shell.connect", lambda base_url: calls.append(("connect", base_url)) or FakeClient())

    exit_code = shell.main(["shell", "bootstrap"])

    assert exit_code == 0
    assert calls == [
        ("boot", None),
        ("connect", "http://daemon.test"),
        ("status", None),
        ("start_instance", None),
    ]


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
            else:
                raise AssertionError(request.path)
            return SimpleNamespace(data=payload.encode())

    class FakeHandle:
        def __init__(self) -> None:
            self._client = FakeClient()
            self.reference = SimpleNamespace(
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
    assert new_state.images["niimath"]["deploy_env"] == ["DEPLOY_ENV_FSLDIR=/opt/fsl"]
    wrapper = (root / "bin" / "niimath").read_text()
    assert "/usr/local/bin/neurodesk shell run-wrapper" in wrapper
    assert '--image niimath' in wrapper
    assert '--command niimath' in wrapper
    assert calls[0] == ("cvmfs_list", "/containers/niimath_1.0.20250804_20251016")
    assert calls[-1] == ("close", None)


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
    shell.write_wrapper(root / "bin" / "niimath", image="niimath", command="niimath")

    exit_code = shell.main(["shell", "unload", "niimath"])

    assert exit_code == 0
    assert "unloaded niimath" in capsys.readouterr().out
    new_state = shell.read_state(root, session_id="sess-1")
    assert new_state.images == {}
    assert new_state.wrappers == {}
    assert not (root / "bin" / "niimath").exists()


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

    class FakeClient:
        def run(
            self,
            image: str,
            command: list[str],
            *,
            env: list[str] = (),
            shares: list[object] = (),
            workdir: str | None = None,
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
    expected_share = shell.ShareMount(source=str(tmp_path.resolve()), mount=shell.HOST_CWD_MOUNT, writable=True)
    assert calls == [
        (
            "run",
            (
                "niimath",
                ("niimath", "-help"),
                ("DEPLOY_ENV_FSLDIR=/opt/fsl",),
                (expected_share,),
                shell.HOST_CWD_MOUNT,
            ),
        ),
        ("close", None),
    ]
