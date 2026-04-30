from __future__ import annotations

import os
import sys
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
    load_timeout_for,
    load_suite,
    Options,
    apply_env_setup,
    substitute_variables,
    timeout_for,
    validate_test,
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


def test_load_suite_converts_matlab_script_tests(tmp_path: Path) -> None:
    recipe = tmp_path / "fulltest.yaml"
    recipe.write_text(
        """
name: fieldtrip-mini
container: fieldtrip_20220617_20220627.simg
test_data:
  scripts_dir: test_scripts
matlab_runtime:
  path: /opt/MCR/v99
  runner: /opt/fieldtrip/run_fieldtrip.sh
tests:
  - name: MATLAB smoke test
    script: |
      x = 1:10;
      disp(sum(x));
    expected_output_contains: "55"
""".lstrip()
    )

    suite = load_suite(recipe)

    assert len(suite.tests) == 1
    command = suite.tests[0].command
    assert command.startswith("bash -lc ")
    assert "test_scripts/fieldtrip-mini-001-matlab-smoke-test.m" in command
    assert "/opt/fieldtrip/run_fieldtrip.sh /opt/MCR/v99" in command
    assert "disp(sum(x));" in command
    assert suite.tests[0].expected_output_contains == ("55",)


def test_load_suite_script_requires_matlab_runtime(tmp_path: Path) -> None:
    recipe = tmp_path / "fulltest.yaml"
    recipe.write_text(
        """
name: broken-script-suite
container: fieldtrip.simg
tests:
  - name: MATLAB smoke test
    script: disp('ok');
""".lstrip()
    )

    try:
        load_suite(recipe)
    except ValueError as exc:
        assert "matlab_runtime.runner/path" in str(exc)
    else:
        raise AssertionError("load_suite() did not reject script without matlab_runtime")


def test_load_suite_parses_ignore_exit_code(tmp_path: Path) -> None:
    recipe = tmp_path / "fulltest.yaml"
    recipe.write_text(
        """
name: ignore-exit-suite
container: tool.simg
tests:
  - name: Help can exit nonzero
    command: tool --help
    ignore_exit_code: true
    expected_output_contains: "Usage"
""".lstrip()
    )

    suite = load_suite(recipe)

    assert suite.tests[0].ignore_exit_code is True
    assert validate_test("Usage: tool", 2, suite.tests[0], {}) == ""


def test_load_suite_parses_host_setup_and_cleanup(tmp_path: Path) -> None:
    recipe = tmp_path / "fulltest.yaml"
    recipe.write_text(
        """
name: host-script-suite
container: tool.simg
setup:
  host_script: |
    echo "$input" > generated.txt
  script: echo guest setup
cleanup:
  host_script: rm -f generated.txt
  script: echo guest cleanup
tests:
  - name: Smoke
    command: tool --help
""".lstrip()
    )

    suite = load_suite(recipe)

    assert suite.setup.host_script.strip() == 'echo "$input" > generated.txt'
    assert suite.setup.script.strip() == "echo guest setup"
    assert suite.cleanup.host_script.strip() == "rm -f generated.txt"
    assert suite.cleanup.script.strip() == "echo guest cleanup"


def test_load_suite_parses_env_setup(tmp_path: Path) -> None:
    recipe = tmp_path / "fulltest.yaml"
    recipe.write_text(
        """
name: env-setup-suite
container: tool.simg
env_setup: source /opt/conda/etc/profile.d/conda.sh && conda activate tool
tests:
  - name: Smoke
    command: python -c "import tool"
""".lstrip()
    )

    suite = load_suite(recipe)

    assert suite.env_setup == "source /opt/conda/etc/profile.d/conda.sh && conda activate tool"


def test_apply_env_setup_prepends_setup_command() -> None:
    assert apply_env_setup("python -c 'import tool'", "conda activate tool") == (
        "conda activate tool\npython -c 'import tool'"
    )
    assert apply_env_setup("python -c 'import tool'", "") == "python -c 'import tool'"
    fulltest_command = apply_env_setup(
        "python -c 'import tool'",
        "conda activate tool",
        include_fulltest_defaults=True,
    )
    assert "export QT_QPA_PLATFORM=" in fulltest_command
    assert "export MCR_CACHE_ROOT=" in fulltest_command
    assert fulltest_command.endswith("conda activate tool\npython -c 'import tool'")


def test_run_host_script_uses_work_dir_and_host_variables(tmp_path: Path) -> None:
    output, exit_code = fulltest.run_host_script(
        "echo ${input}> generated.txt",
        tmp_path,
        {"input": "host-value"},
        10.0,
    )

    assert output == ""
    assert exit_code == 0
    assert (tmp_path / "generated.txt").read_text().strip() == "host-value"


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


def test_load_timeout_respects_configured_boot_timeout(monkeypatch) -> None:
    monkeypatch.setenv("PYNEURODESK_BOOT_TIMEOUT", "300")

    assert load_timeout_for(0) == 330.0


def test_load_timeout_respects_larger_suite_default(monkeypatch) -> None:
    monkeypatch.setenv("PYNEURODESK_BOOT_TIMEOUT", "300")

    assert load_timeout_for(600) == 600.0


def test_run_shell_reports_timeout_without_traceback(tmp_path: Path) -> None:
    command = f'{sys.executable} -c "import time; time.sleep(10)"'
    output, exit_code = fulltest.run_shell(os.environ.copy(), tmp_path, command, 0.01)

    assert exit_code == 124
    assert "command timed out after 0.0s" in output
    assert command in output


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
    monkeypatch.setattr(fulltest, "resolve_neurodesk_command", lambda: "neurodesk")
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


def test_activated_shell_session_runs_recipe_shell_in_guest(monkeypatch, tmp_path: Path) -> None:
    calls: list[tuple[dict[str, str], Path, str, float]] = []

    def fake_run_shell(env: dict[str, str], work_dir: Path, command: str, timeout_seconds: float) -> tuple[str, int]:
        calls.append((env, work_dir, command, timeout_seconds))
        return "ok", 0

    monkeypatch.setattr(fulltest, "run_shell", fake_run_shell)
    monkeypatch.setattr(fulltest, "resolve_neurodesk_command", lambda: "neurodesk")
    activation_script = tmp_path / "activate.sh"
    session = ActivatedShellSession(
        work_dir=tmp_path,
        activation_script=activation_script,
        env={"CCX3_URL": "http://example.test"},
        image="fulltest-image",
    )

    output, exit_code = session.run("$ASHS_ROOT/bin/ashs_main.sh -h 2>&1 | head -20", 30.0)

    assert (output, exit_code) == ("ok", 0)
    assert len(calls) == 1
    assert calls[0][2].startswith("source ")
    assert (
        "neurodesk shell exec fulltest-image -- bash -lc "
        "'$ASHS_ROOT/bin/ashs_main.sh -h 2>&1 | head -20'"
    ) in calls[0][2]


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


def test_load_command_defaults_to_12gb_memory() -> None:
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

    assert "--memory-mb 12288" in command
