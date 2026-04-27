from __future__ import annotations

from pathlib import Path

import pyneurodesk.fulltest as fulltest
from pyneurodesk.fulltest import (
    ActivatedShellSession,
    build_container_reference,
    cvmfs_path_from_source,
    default_recipe_path,
    first_shell_command,
    guest_shell_command,
    image_cache_name,
    infer_shell_hook_commands,
    load_command,
    load_suite,
    Options,
    substitute_variables,
    timeout_for,
)


FIXTURE_RECIPE = Path(__file__).parents[1] / "fixtures" / "niimath" / "fulltest.yaml"


def test_load_suite_parses_niimath_recipe() -> None:
    suite = load_suite(FIXTURE_RECIPE)

    assert suite.name == "niimath"
    assert suite.container == "niimath_1.0.20250804_20251016.simg"
    assert len(suite.required_files) == 0
    assert len(suite.tests) >= 3
    assert suite.tests[0].name == "Version check"
    assert suite.tests[0].expected_output_contains == ("niimath version",)
    assert suite.tests[1].expected_output_contains == ("Usage: niimath",)


def test_build_container_reference_defaults_to_cvmfs_directory() -> None:
    suite = load_suite(FIXTURE_RECIPE)

    reference = build_container_reference(
        suite,
        image_name="niimath",
        image_source="",
        mirror="https://cvmfs.neurodesk.org",
        repo="neurodesk.ardc.edu.au",
        cache_dir="/tmp/cvmfs-cache",
    )

    assert reference.image == "niimath"
    assert reference.source.type == "cvmfs"
    assert reference.source.path == "/containers/niimath_1.0.20250804_20251016"
    assert reference.cache_dir == "/tmp/cvmfs-cache"


def test_cvmfs_path_from_source_supports_cvmfs_uri() -> None:
    assert (
        cvmfs_path_from_source(
            "cvmfs://neurodesk.ardc.edu.au/containers/niimath_1.0.20250804_20251016"
        )
        == "/containers/niimath_1.0.20250804_20251016"
    )


def test_cvmfs_path_from_source_supports_http_mount_path() -> None:
    assert (
        cvmfs_path_from_source(
            "https://cvmfs.neurodesk.org/cvmfs/neurodesk.ardc.edu.au/containers/niimath_1.0.20250804_20251016"
        )
        == "/containers/niimath_1.0.20250804_20251016"
    )


def test_substitute_variables_replaces_braced_and_unbraced_names() -> None:
    text = "cp ${input} $output"
    variables = {"input": "/work/in.nii.gz", "output": "/work/out.nii.gz"}

    assert substitute_variables(text, variables) == "cp /work/in.nii.gz /work/out.nii.gz"


def test_build_shell_hook_vars_preserves_guest_absolute_paths() -> None:
    assert fulltest.build_shell_hook_vars({"tool_data": "/opt/tool/data.nii", "relative": "ds000001/file.nii"}) == {
        "tool_data": "/opt/tool/data.nii",
        "relative": "ds000001/file.nii",
    }


def test_timeout_for_prefers_test_timeout_then_default() -> None:
    assert timeout_for(45, 90) == 45.0
    assert timeout_for(0, 90) == 90.0
    assert timeout_for(0, 0) == 120.0


def test_image_cache_name_is_stable() -> None:
    assert image_cache_name("niimath") == image_cache_name("niimath")
    assert image_cache_name("niimath") != image_cache_name("other")


def test_default_recipe_path_points_to_existing_recipe() -> None:
    recipe = default_recipe_path()

    assert recipe.is_file()
    assert recipe.name == "fulltest.yaml"
    assert recipe.parts[-2:] == ("niimath", "fulltest.yaml")


def test_infer_shell_hook_commands_collects_recipe_entrypoints() -> None:
    suite = load_suite(FIXTURE_RECIPE)

    assert infer_shell_hook_commands(suite) == {"niimath"}


def test_first_shell_command_handles_quoted_arguments() -> None:
    assert first_shell_command("niimath -help") == "niimath"
    assert first_shell_command("sh -lc 'echo ok'") == "sh"
    assert first_shell_command("'broken") == ""


def test_guest_shell_command_detects_login_shell_execution() -> None:
    assert guest_shell_command("bash -lc 'ls /opt/tool'") == ["bash", "-lc", "ls /opt/tool"]
    assert guest_shell_command("sh -l -c 'ls /opt/tool'") == ["sh", "-l", "-c", "ls /opt/tool"]
    assert guest_shell_command("bash -c 'ls /opt/tool'") is None
    assert guest_shell_command("env QT_QPA_PLATFORM=offscreen bash -lc 'tool'") is None


def test_activated_shell_session_runs_login_shell_directly_in_guest(monkeypatch, tmp_path: Path) -> None:
    calls: list[tuple[dict[str, str], Path, str, float]] = []

    def fake_run_shell(env: dict[str, str], work_dir: Path, command: str, timeout_seconds: float) -> tuple[str, int]:
        calls.append((env, work_dir, command, timeout_seconds))
        return "ok", 0

    monkeypatch.setattr(fulltest, "run_shell", fake_run_shell)
    activation_script = tmp_path / "activate.sh"
    session = ActivatedShellSession(
        work_dir=tmp_path,
        activation_script=activation_script,
        env={"CCX3_URL": "http://example.test"},
        image="fulltest-image",
    )

    output, exit_code = session.run("bash -lc 'ls /opt/tool'", 30.0)

    assert (output, exit_code) == ("ok", 0)
    assert len(calls) == 1
    assert calls[0][0] == {"CCX3_URL": "http://example.test"}
    assert calls[0][1] == tmp_path
    assert calls[0][3] == 30.0
    assert calls[0][2].startswith("source ")
    assert "neurodesk shell exec fulltest-image -- bash -lc 'ls /opt/tool'" in calls[0][2]


def test_load_command_uses_nd_load_with_source_and_recipe_commands() -> None:
    suite = load_suite(FIXTURE_RECIPE)
    reference = build_container_reference(
        suite,
        image_name="fulltest-image",
        image_source="/containers/custom container",
        mirror="https://mirror.example",
        repo="repo.example",
        cache_dir="/tmp/cache dir",
    )

    command = load_command(
        reference,
        suite,
        Options(
            recipe=default_recipe_path(),
            mirror="https://mirror.example",
            repo="repo.example",
            memory_mb=512,
            cpus=2,
        ),
    )

    assert command.startswith("nd load fulltest-image --source")
    assert "'/containers/custom container'" in command
    assert "--command niimath" in command
    assert "--command sh" not in command
    assert "--cache-dir '/tmp/cache dir'" in command
    assert "--memory-mb 512" in command
    assert "--cpus 2" in command


def test_load_command_defaults_to_8gb_memory() -> None:
    suite = load_suite(FIXTURE_RECIPE)
    reference = build_container_reference(
        suite,
        image_name="fulltest-image",
        image_source="",
        mirror="https://mirror.example",
        repo="repo.example",
        cache_dir=None,
    )

    command = load_command(reference, suite, Options(recipe=default_recipe_path()))

    assert "--memory-mb 8192" in command
