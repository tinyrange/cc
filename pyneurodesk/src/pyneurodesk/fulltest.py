from __future__ import annotations

import argparse
import hashlib
import os
import re
import shlex
import shutil
import signal
import subprocess
import tempfile
import time
from dataclasses import dataclass, replace
from pathlib import Path
from typing import Any, Optional
from urllib.parse import urlparse

import httpx

from .api import (
    DEFAULT_CVMFS_MIRROR,
    DEFAULT_CVMFS_REPO,
    start_container_daemon,
)
from .client import PyNeurodeskClient, resolve_boot_timeout_seconds
from .models import ContainerReference, ImageSource, ImportImageRequest
from . import shell as shell_hooks


DEFAULT_OPENNEURO_BASE = "https://s3.amazonaws.com/openneuro.org"
FULLTEST_EXTRA_MESSAGE = "pyneurodesk fulltest dependencies are not installed; install pyneurodesk[fulltest]"
DEFAULT_FULLTEST_MEMORY_MB = 12288
DEFAULT_FULLTEST_CPUS = min(os.cpu_count() or 1, 8)


@dataclass(frozen=True)
class RequiredDataset:
    dataset: str
    files: tuple[str, ...]


@dataclass(frozen=True)
class SuiteScript:
    script: str = ""
    host_script: str = ""


@dataclass(frozen=True)
class TestCase:
    name: str
    command: str
    timeout: int = 0
    depends_on: tuple[str, ...] = ()
    expected_output_contains: tuple[str, ...] = ()
    expected_exit_code: int = 0
    ignore_exit_code: bool = False
    validate: tuple[dict[str, Any], ...] = ()


@dataclass(frozen=True)
class Suite:
    name: str
    container: str
    env_setup: str
    required_files: tuple[RequiredDataset, ...]
    test_data: dict[str, str]
    setup: SuiteScript
    cleanup: SuiteScript
    tests: tuple[TestCase, ...]
    default_timeout: int = 0


@dataclass(frozen=True)
class Options:
    recipe: Path
    image_source: str = ""
    image_name: str = ""
    work_dir: Optional[Path] = None
    filter_text: str = ""
    keep_vm: bool = False
    mirror: str = DEFAULT_CVMFS_MIRROR
    repo: str = DEFAULT_CVMFS_REPO
    cache_dir: Optional[str] = None
    prefetch: bool = False
    prefetch_workers: Optional[int] = None
    memory_mb: Optional[int] = DEFAULT_FULLTEST_MEMORY_MB
    cpus: Optional[int] = DEFAULT_FULLTEST_CPUS


@dataclass(frozen=True)
class TestResult:
    name: str
    passed: bool = False
    skipped: bool = False
    duration_seconds: float = 0.0
    message: str = ""


@dataclass(frozen=True)
class RunResult:
    suite: str
    work_dir: Path
    results: tuple[TestResult, ...]


class FullTestRunner:
    def __init__(self, *, http_client: Optional[httpx.Client] = None) -> None:
        self.http = http_client or httpx.Client(follow_redirects=True, timeout=httpx.Timeout(120.0))
        self._owns_http = http_client is None

    def close(self) -> None:
        if self._owns_http:
            self.http.close()

    def run(self, options: Options) -> RunResult:
        suite = load_suite(options.recipe)
        work_dir = options.work_dir or Path(tempfile.mkdtemp(prefix="ccx3-fulltest-"))
        work_dir.mkdir(parents=True, exist_ok=True)
        prepare_required_files(self.http, work_dir, suite.required_files)

        image_name = options.image_name or image_cache_name(options.image_source or suite.container)
        reference = build_container_reference(
            suite,
            image_name=image_name,
            image_source=options.image_source,
            mirror=options.mirror,
            repo=options.repo,
            cache_dir=options.cache_dir,
        )

        daemon = start_container_daemon()
        client = PyNeurodeskClient(daemon.base_url)
        shell_session: Optional[ActivatedShellSession] = None
        guest_vars = build_shell_hook_vars(suite.test_data)
        host_vars = build_host_vars(work_dir, suite.test_data)
        results: list[TestResult] = []
        failed: set[str] = set()
        keep_vm = options.keep_vm
        selected_tests = [
            test
            for test in suite.tests
            if not options.filter_text or options.filter_text.lower() in test.name.lower()
        ]

        try:
            print(f"[fulltest] suite={suite.name} tests={len(selected_tests)} work_dir={work_dir}", flush=True)
            memory_text = f" memory={options.memory_mb}MiB" if options.memory_mb is not None else ""
            cpu_text = f" cpus={options.cpus}" if options.cpus is not None else ""
            print(f"[fulltest] importing image={reference.image} source={reference.path}", flush=True)
            stream_import_image(client, reference, options)
            print("[fulltest] activating shell hooks", flush=True)
            shell_session = activate_shell_session(
                daemon_base_url=daemon.base_url,
                work_dir=work_dir,
            )
            print(f"[fulltest] loading image={reference.image} source={reference.path}{memory_text}{cpu_text}", flush=True)
            load_options = replace(options, prefetch=False, prefetch_workers=None)
            output, exit_code = shell_session.run(load_command(reference, suite, load_options), load_timeout_for(suite.default_timeout))
            if exit_code != 0:
                raise RuntimeError(f"shell hook load failed with exit code {exit_code}: {output}")
            shell_session.image = reference.image
            print(f"[fulltest] shell hooks ready image={reference.image}{memory_text}{cpu_text}", flush=True)

            if suite.setup.host_script.strip():
                print("[fulltest] host setup", flush=True)
                output, exit_code = run_host_script(
                    suite.setup.host_script,
                    work_dir,
                    host_vars,
                    timeout_for(120, suite.default_timeout),
                )
                if exit_code != 0:
                    raise RuntimeError(f"host setup failed with exit code {exit_code}: {output}")

            if suite.setup.script.strip():
                print("[fulltest] setup", flush=True)
                output, exit_code = shell_session.run(
                    apply_env_setup(
                        substitute_variables(suite.setup.script, guest_vars),
                        suite.env_setup,
                        include_fulltest_defaults=True,
                    ),
                    timeout_for(120, suite.default_timeout),
                )
                if exit_code != 0:
                    raise RuntimeError(f"setup failed with exit code {exit_code}: {output}")

            for index, test in enumerate(selected_tests, start=1):
                print(f"[fulltest] [{index}/{len(selected_tests)}] {test.name}", flush=True)
                if any(dep in failed for dep in test.depends_on):
                    results.append(TestResult(name=test.name, skipped=True, message="dependency failed"))
                    failed.add(test.name)
                    print(f"[fulltest] skipped {test.name}: dependency failed", flush=True)
                    continue

                start = time.perf_counter()
                output = ""
                exit_code = -1
                try:
                    output, exit_code = shell_session.run(
                        apply_env_setup(
                            substitute_variables(test.command, guest_vars),
                            suite.env_setup,
                            include_fulltest_defaults=True,
                        ),
                        timeout_for(test.timeout, suite.default_timeout),
                    )
                    message = validate_test(output, exit_code, test, host_vars)
                    if message:
                        failed.add(test.name)
                        duration = time.perf_counter() - start
                        results.append(
                            TestResult(
                                name=test.name,
                                duration_seconds=duration,
                                message=message,
                            )
                        )
                        print(f"[fulltest] failed {test.name} ({duration:.2f}s): {message}", flush=True)
                        continue
                    duration = time.perf_counter() - start
                    results.append(
                        TestResult(
                            name=test.name,
                            passed=True,
                            duration_seconds=duration,
                            message="ok",
                        )
                    )
                    print(f"[fulltest] passed {test.name} ({duration:.2f}s)", flush=True)
                except Exception as exc:
                    failed.add(test.name)
                    duration = time.perf_counter() - start
                    results.append(
                        TestResult(
                            name=test.name,
                            duration_seconds=duration,
                            message=str(exc),
                        )
                    )
                    print(f"[fulltest] error {test.name} ({duration:.2f}s): {exc}", flush=True)

            if suite.cleanup.script.strip():
                try:
                    print("[fulltest] cleanup", flush=True)
                    shell_session.run(
                        apply_env_setup(
                            substitute_variables(suite.cleanup.script, guest_vars),
                            suite.env_setup,
                            include_fulltest_defaults=True,
                        ),
                        timeout_for(60, suite.default_timeout),
                    )
                except Exception:
                    pass

            if suite.cleanup.host_script.strip():
                try:
                    print("[fulltest] host cleanup", flush=True)
                    run_host_script(
                        suite.cleanup.host_script,
                        work_dir,
                        host_vars,
                        timeout_for(60, suite.default_timeout),
                    )
                except Exception:
                    pass

            return RunResult(suite=suite.name, work_dir=work_dir, results=tuple(results))
        finally:
            if not keep_vm:
                try:
                    client.shutdown_instance()
                except Exception:
                    pass
                if shell_session is not None:
                    try:
                        shell_session.close()
                    except Exception:
                        pass
            client.close()
            try:
                with httpx.Client(base_url=daemon.base_url, timeout=2.0) as shutdown_client:
                    shutdown_client.post("/shutdown")
            except Exception:
                pass


def load_suite(path: Path) -> Suite:
    yaml = _import_yaml()
    payload = yaml.safe_load(path.read_text()) or {}
    tests = []
    matlab_runtime = payload.get("matlab_runtime") or {}
    for index, item in enumerate(payload.get("tests", []), start=1):
        expected_exit_code = item.get("expected_exit_code")
        if expected_exit_code is None:
            expected_exit_code = 0
        name = str(item["name"])
        command = test_command_from_item(
            item,
            suite_name=str(payload.get("name") or path.stem),
            test_name=name,
            test_index=index,
            test_data={str(key): str(value) for key, value in (payload.get("test_data") or {}).items()},
            matlab_runtime=matlab_runtime,
        )
        tests.append(
            TestCase(
                name=name,
                command=command,
                timeout=int(item.get("timeout") or 0),
                depends_on=tuple(to_string_list(item.get("depends_on"))),
                expected_output_contains=tuple(to_string_list(item.get("expected_output_contains"))),
                expected_exit_code=int(expected_exit_code),
                ignore_exit_code=bool(item.get("ignore_exit_code")),
                validate=tuple(item.get("validate") or ()),
            )
        )
    return Suite(
        name=str(payload.get("name") or path.stem),
        container=str(payload["container"]),
        env_setup=str(payload.get("env_setup") or ""),
        required_files=tuple(
            RequiredDataset(
                dataset=str(entry["dataset"]),
                files=tuple(str(file_name) for file_name in entry.get("files", [])),
            )
            for entry in payload.get("required_files", [])
        ),
        test_data={str(key): str(value) for key, value in (payload.get("test_data") or {}).items()},
        setup=load_suite_script(payload.get("setup")),
        cleanup=load_suite_script(payload.get("cleanup")),
        tests=tuple(tests),
        default_timeout=int(payload.get("default_timeout") or payload.get("default_timout") or 0),
    )


def load_suite_script(value: Any) -> SuiteScript:
    if not isinstance(value, dict):
        return SuiteScript()
    return SuiteScript(
        script=str(value.get("script") or ""),
        host_script=str(value.get("host_script") or ""),
    )


def test_command_from_item(
    item: dict[str, Any],
    *,
    suite_name: str,
    test_name: str,
    test_index: int,
    test_data: dict[str, str],
    matlab_runtime: Any,
) -> str:
    if "command" in item:
        return str(item["command"])
    if "script" in item:
        return matlab_script_command(
            str(item["script"]),
            suite_name=suite_name,
            test_name=test_name,
            test_index=test_index,
            scripts_dir=test_data.get("scripts_dir", "test_scripts"),
            matlab_runtime=matlab_runtime,
        )
    raise ValueError(f"test {test_index} {test_name!r} must define either 'command' or 'script'")


def matlab_script_command(
    script: str,
    *,
    suite_name: str,
    test_name: str,
    test_index: int,
    scripts_dir: str,
    matlab_runtime: Any,
) -> str:
    if not isinstance(matlab_runtime, dict):
        raise ValueError(f"test {test_index} {test_name!r} defines 'script' but matlab_runtime is not a mapping")
    runner = str(matlab_runtime.get("runner") or "").strip()
    runtime_path = str(matlab_runtime.get("path") or "").strip()
    if not runner or not runtime_path:
        raise ValueError(
            f"test {test_index} {test_name!r} defines 'script' but matlab_runtime.runner/path are not set"
        )

    script_path = Path(scripts_dir) / f"{slugify_test_script_name(suite_name, test_index, test_name)}.m"
    quoted_script_path = shlex.quote(script_path.as_posix())
    quoted_runner = shlex.quote(runner)
    quoted_runtime_path = shlex.quote(runtime_path)
    quoted_parent = shlex.quote(script_path.parent.as_posix())
    return "\n".join(
        [
            "bash -lc " + shlex.quote(
                "\n".join(
                    [
                        "set -euo pipefail",
                        f"mkdir -p {quoted_parent}",
                        f"cat > {quoted_script_path} <<'PYNEURODESK_FULLTEST_MATLAB'",
                        script.rstrip(),
                        "PYNEURODESK_FULLTEST_MATLAB",
                        f"{quoted_runner} {quoted_runtime_path} {quoted_script_path}",
                    ]
                )
            )
        ]
    )


def slugify_test_script_name(suite_name: str, test_index: int, test_name: str) -> str:
    raw = f"{suite_name}-{test_index:03d}-{test_name}".lower()
    slug = re.sub(r"[^a-z0-9]+", "-", raw).strip("-")
    return slug or f"test-{test_index:03d}"


def to_string_list(value: Any) -> list[str]:
    if value is None:
        return []
    if isinstance(value, str):
        return [value]
    if isinstance(value, list):
        return [str(item) for item in value]
    return []


def build_guest_vars(test_data: dict[str, str]) -> dict[str, str]:
    out: dict[str, str] = {}
    for key, value in test_data.items():
        out[key] = "/work/" + value.lstrip("/")
    return out


def build_shell_hook_vars(test_data: dict[str, str]) -> dict[str, str]:
    return dict(test_data)


def build_host_vars(work_dir: Path, test_data: dict[str, str]) -> dict[str, str]:
    return {key: str(work_dir / Path(value)) for key, value in test_data.items()}


def substitute_variables(text: str, variables: dict[str, str]) -> str:
    result = text
    for key in sorted(variables):
        result = result.replace("${" + key + "}", variables[key])
        result = result.replace("$" + key, variables[key])
    return result


def apply_env_setup(command: str, env_setup: str, *, include_fulltest_defaults: bool = False) -> str:
    setup_parts: list[str] = []
    if include_fulltest_defaults:
        setup_parts.append(default_fulltest_env_setup())
    if env_setup.strip():
        setup_parts.append(env_setup.strip())
    setup = "\n".join(part for part in setup_parts if part)
    if not setup:
        return command
    command = command.strip("\n")
    if not command:
        return setup
    return setup + "\n" + command


def default_fulltest_env_setup() -> str:
    return "\n".join(
        [
            'export MPLBACKEND="${MPLBACKEND:-agg}"',
            'export QT_QPA_PLATFORM="${QT_QPA_PLATFORM:-offscreen}"',
            'export NO_AT_BRIDGE="${NO_AT_BRIDGE:-1}"',
            'export KMP_AFFINITY="${KMP_AFFINITY:-disabled}"',
            'export MCR_CACHE_ROOT="${MCR_CACHE_ROOT:-/tmp/pyneurodesk-mcr-cache}"',
            'export MATLAB_PREFDIR="${MATLAB_PREFDIR:-/tmp/pyneurodesk-matlab-prefdir}"',
            'mkdir -p "$MCR_CACHE_ROOT" "$MATLAB_PREFDIR" 2>/dev/null || true',
        ]
    )


def prepare_required_files(http: httpx.Client, work_dir: Path, required: tuple[RequiredDataset, ...]) -> None:
    for dataset in required:
        for relative in dataset.files:
            destination = work_dir / dataset.dataset / Path(relative)
            if destination.exists():
                continue
            destination.parent.mkdir(parents=True, exist_ok=True)
            download_file(http, f"{DEFAULT_OPENNEURO_BASE}/{dataset.dataset}/{relative}", destination)


def download_file(http: httpx.Client, url: str, destination: Path) -> None:
    tmp = destination.with_suffix(destination.suffix + ".tmp")
    with http.stream("GET", url) as response:
        response.raise_for_status()
        with tmp.open("wb") as output:
            for chunk in response.iter_bytes():
                output.write(chunk)
    tmp.replace(destination)


def build_container_reference(
    suite: Suite,
    *,
    image_name: str,
    image_source: str,
    mirror: str,
    repo: str,
    cache_dir: Optional[str],
) -> ContainerReference:
    source = image_source.strip()
    if source:
        if source.startswith("/containers/"):
            return ContainerReference(
                name=suite.name,
                image=image_name,
                source=ImageSource(type="cvmfs", mirror=mirror, repo=repo, path=source),
                cache_dir=cache_dir,
            )
        if source.startswith("cvmfs://") or "/cvmfs/" in source:
            path = cvmfs_path_from_source(source)
            return ContainerReference(
                name=suite.name,
                image=image_name,
                source=ImageSource(type="cvmfs", mirror=mirror, repo=repo, path=path),
                cache_dir=cache_dir,
            )
        return ContainerReference(
            name=suite.name,
            image=image_name,
            source=ImageSource(type="simg", path=source),
            cache_dir=cache_dir,
        )

    container_name = suite.container
    if container_name.endswith(".simg"):
        container_name = container_name[:-5]
    return ContainerReference(
        name=suite.name,
        image=image_name,
        source=ImageSource(type="cvmfs", mirror=mirror, repo=repo, path=f"/containers/{container_name}"),
        cache_dir=cache_dir,
    )


def image_cache_name(seed: str) -> str:
    digest = hashlib.sha1(seed.encode("utf-8")).hexdigest()[:16]
    return f"fulltest-{digest}"


def cvmfs_path_from_source(source: str) -> str:
    source = source.strip()
    if source.startswith("cvmfs://"):
        parsed = source[len("cvmfs://") :]
        slash = parsed.find("/")
        if slash == -1:
            return "/"
        return "/" + parsed[slash + 1 :].lstrip("/")
    parsed = urlparse(source)
    if "/cvmfs/" in parsed.path:
        tail = parsed.path.split("/cvmfs/", 1)[1]
        slash = tail.find("/")
        if slash == -1:
            return "/"
        return "/" + tail[slash + 1 :].lstrip("/")
    raise ValueError(f"unsupported CVMFS source: {source}")


def timeout_for(test_timeout: int, default_timeout: int) -> float:
    if test_timeout > 0:
        return float(test_timeout)
    if default_timeout > 0:
        return float(default_timeout)
    return 120.0


def load_timeout_for(default_timeout: int) -> float:
    timeout = float(default_timeout) if default_timeout > 0 else 120.0
    boot_timeout = resolve_boot_timeout_seconds()
    if boot_timeout is not None:
        timeout = max(timeout, boot_timeout + 30.0)
    return timeout


def run_shell(
    env: dict[str, str],
    work_dir: Path,
    command: str,
    timeout_seconds: float,
) -> tuple[str, int]:
    shell_command = ["bash", "-lc", command]
    if os.name == "nt":
        shell_command = [os.environ.get("COMSPEC", "cmd.exe"), "/d", "/c", command]
        try:
            proc = subprocess.run(
                shell_command,
                cwd=work_dir,
                env=env,
                capture_output=True,
                text=True,
                timeout=timeout_seconds,
                check=False,
            )
        except subprocess.TimeoutExpired as exc:
            output = timeout_expired_output(exc)
            output += f"\n[fulltest] command timed out after {timeout_seconds:.1f}s: {command}\n"
            return output, 124
        return proc.stdout + proc.stderr, proc.returncode

    proc = subprocess.Popen(
        shell_command,
        cwd=work_dir,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
        start_new_session=True,
    )
    try:
        stdout, stderr = proc.communicate(timeout=timeout_seconds)
    except subprocess.TimeoutExpired:
        try:
            os.killpg(proc.pid, signal.SIGTERM)
        except ProcessLookupError:
            pass
        try:
            stdout, stderr = proc.communicate(timeout=5.0)
            output = stdout + stderr
        except subprocess.TimeoutExpired as exc:
            try:
                os.killpg(proc.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            stdout, stderr = proc.communicate()
            output = timeout_expired_output(exc) or (stdout + stderr)
        output += f"\n[fulltest] command timed out after {timeout_seconds:.1f}s: {command}\n"
        return output, 124
    return stdout + stderr, proc.returncode


def timeout_expired_output(exc: subprocess.TimeoutExpired) -> str:
    parts = []
    for stream in (exc.stdout, exc.stderr):
        if isinstance(stream, bytes):
            parts.append(stream.decode("utf-8", errors="replace"))
        elif isinstance(stream, str):
            parts.append(stream)
    return "".join(parts)


def run_host_script(
    script: str,
    work_dir: Path,
    variables: dict[str, str],
    timeout_seconds: float,
) -> tuple[str, int]:
    return run_shell(
        os.environ.copy(),
        work_dir,
        substitute_variables(script, variables),
        timeout_seconds,
    )


@dataclass
class ActivatedShellSession:
    work_dir: Path
    activation_script: Path
    env: dict[str, str]
    image: Optional[str] = None
    root: Optional[Path] = None

    def run(self, command: str, timeout_seconds: float) -> tuple[str, int]:
        guest_command = guest_shell_command(command)
        if self.image:
            if guest_command:
                return self.run_direct_guest(guest_command, timeout_seconds)
            return self.run_guest_script(command, timeout_seconds)
        return run_shell(
            self.env,
            self.work_dir,
            "source " + shlex.quote(str(self.activation_script)) + "\n" + command,
            timeout_seconds,
        )

    def run_direct_guest(self, command: list[str], timeout_seconds: float) -> tuple[str, int]:
        words = [resolve_neurodesk_command(), "shell", "exec", str(self.image), "--", *command]
        return run_shell(
            self.env,
            self.work_dir,
            "source " + shlex.quote(str(self.activation_script)) + "\n" + " ".join(shlex.quote(word) for word in words),
            timeout_seconds,
        )

    def run_guest_script(self, command: str, timeout_seconds: float) -> tuple[str, int]:
        return self.run_direct_guest(["bash", "-lc", command], timeout_seconds)

    def close(self) -> None:
        try:
            self.run("neurodesk_deactivate >/dev/null 2>&1 || true", 10.0)
        finally:
            if self.root is not None:
                shutil.rmtree(self.root, ignore_errors=True)
            self.activation_script.unlink(missing_ok=True)


def activate_shell_session(
    *,
    daemon_base_url: str,
    work_dir: Path,
) -> ActivatedShellSession:
    env = os.environ.copy()
    env["CCX3_URL"] = daemon_base_url
    activation_script = work_dir / ".pyneurodesk-fulltest-activate.sh"
    proc = subprocess.run(
        ["neurodesk", "activate", "--shell", "bash", "--no-bootstrap"],
        cwd=work_dir,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    if proc.returncode != 0:
        raise RuntimeError(f"neurodesk activate failed with exit code {proc.returncode}: {proc.stdout}{proc.stderr}")
    activation_script.write_text(proc.stdout)
    session = ActivatedShellSession(work_dir=work_dir, activation_script=activation_script, env=env)
    root_output, exit_code = session.run("printf '%s' \"${PYNEURODESK_SHELL_ROOT}\"", 10.0)
    if exit_code == 0 and root_output.strip():
        session.root = Path(root_output.strip())
    return session


def resolve_neurodesk_command() -> str:
    return shutil.which("neurodesk") or "neurodesk"


def load_command(reference: ContainerReference, suite: Suite, options: Options) -> str:
    words = [
        "nd",
        "load",
        reference.image,
        "--source",
        reference_source_arg(reference),
        "--mirror",
        options.mirror,
        "--repo",
        options.repo,
        "--force",
    ]
    for command in sorted(infer_shell_hook_commands(suite)):
        words.extend(["--command", command])
    if reference.cache_dir:
        words.extend(["--cache-dir", reference.cache_dir])
    if options.prefetch:
        words.append("--prefetch")
    if options.prefetch_workers is not None:
        words.extend(["--prefetch-workers", str(options.prefetch_workers)])
    if options.memory_mb is not None:
        words.extend(["--memory-mb", str(options.memory_mb)])
    if options.cpus is not None:
        words.extend(["--cpus", str(options.cpus)])
    return " ".join(shlex.quote(word) for word in words)


def reference_source_arg(reference: ContainerReference) -> str:
    if reference.source.path is None:
        raise ValueError("container source path is not set")
    return reference.source.path


def infer_shell_hook_commands(suite: Suite) -> set[str]:
    commands: set[str] = set()
    scripts = [suite.setup.script, suite.cleanup.script, *(test.command for test in suite.tests)]
    for script in scripts:
        if guest_shell_command(script):
            continue
        command = first_shell_command(script)
        if command and shell_hooks.is_valid_wrapper_name(command):
            commands.add(command)
    return commands


def guest_shell_command(script: str) -> Optional[list[str]]:
    try:
        words = shlex.split(script, comments=False, posix=True)
    except ValueError:
        return None
    if len(words) < 3 or words[0] not in {"bash", "sh"}:
        return None
    if words[1] == "-lc":
        return words
    if len(words) >= 4 and words[1] == "-l" and words[2] == "-c":
        return words
    return None


def first_shell_command(script: str) -> str:
    try:
        words = shlex.split(script, comments=False, posix=True)
    except ValueError:
        return ""
    return words[0] if words else ""


def validate_test(output: str, exit_code: int, test: TestCase, host_vars: dict[str, str]) -> str:
    if not test.ignore_exit_code and exit_code != test.expected_exit_code:
        return f"exit code {exit_code}, want {test.expected_exit_code}\n{output}".strip()
    for fragment in test.expected_output_contains:
        if fragment and fragment not in output:
            return f"missing output fragment {fragment!r}"
    for validation in test.validate:
        for kind, arg in validation.items():
            if kind == "output_exists":
                file_path = Path(substitute_variables(str(arg), host_vars))
                if not file_path.exists():
                    return f"missing output {file_path}"
            elif kind == "same_dimensions":
                nib = _import_nibabel()
                if not isinstance(arg, list) or len(arg) != 2:
                    return "invalid same_dimensions validation"
                left = Path(substitute_variables(str(arg[0]), host_vars))
                right = Path(substitute_variables(str(arg[1]), host_vars))
                if nib.load(str(left)).shape != nib.load(str(right)).shape:
                    return f"dimension mismatch {left} vs {right}"
            elif kind == "is_3d":
                nib = _import_nibabel()
                file_path = Path(substitute_variables(str(arg), host_vars))
                if len(nib.load(str(file_path)).shape) != 3:
                    return f"{file_path} is not 3D"
    return ""


def _import_yaml() -> Any:
    try:
        import yaml
    except ImportError as exc:
        raise RuntimeError(FULLTEST_EXTRA_MESSAGE) from exc
    return yaml


def _import_nibabel() -> Any:
    try:
        import nibabel as nib
    except ImportError as exc:
        raise RuntimeError(FULLTEST_EXTRA_MESSAGE) from exc
    return nib


def stream_import_image(client: PyNeurodeskClient, reference: ContainerReference, options: Options) -> None:
    last_line = ""
    for event in client.import_image_stream(
        reference.image,
        ImportImageRequest(
            source=reference.source,
            cache_dir=reference.cache_dir,
            prefetch=options.prefetch,
            prefetch_workers=options.prefetch_workers,
        ),
    ):
        status = event.status
        if status == "error":
            raise RuntimeError(event.error or f"image import failed for {reference.image}")
        line = format_import_progress_line(reference.image, event)
        if line and line != last_line:
            print(line, flush=True)
            last_line = line


def format_import_progress_line(image_name: str, event: Any) -> str:
    status = getattr(event, "status", "") or "preparing"
    artifact = getattr(event, "artifact", None) or image_name
    blob = getattr(event, "blob", None)
    downloaded = getattr(event, "bytes_downloaded", None)
    total_bytes = getattr(event, "bytes_total", None)
    rate = getattr(event, "rate_bytes_per_second", None)
    eta = getattr(event, "eta_seconds", None)

    parts = [f"[fulltest] pull {artifact}", status]
    if blob:
        parts.append(str(blob))
    if isinstance(downloaded, int) and downloaded >= 0:
        if isinstance(total_bytes, int) and total_bytes > 0:
            parts.append(f"{format_byte_size(downloaded)}/{format_byte_size(total_bytes)}")
        else:
            parts.append(format_byte_size(downloaded))
    if isinstance(rate, (int, float)) and rate > 0:
        parts.append(f"{format_byte_size(float(rate))}/s")
    if isinstance(eta, (int, float)) and eta > 0:
        parts.append(f"ETA {format_duration(float(eta))}")
    return " | ".join(parts)


def format_byte_size(value: float) -> str:
    units = ("B", "KiB", "MiB", "GiB", "TiB")
    size = float(value)
    unit = units[0]
    for unit in units:
        if abs(size) < 1024.0 or unit == units[-1]:
            break
        size /= 1024.0
    if unit == "B":
        return f"{int(size)} {unit}"
    return f"{size:.1f} {unit}"


def format_duration(seconds: float) -> str:
    remaining = max(0, int(round(seconds)))
    minutes, sec = divmod(remaining, 60)
    hours, minutes = divmod(minutes, 60)
    if hours:
        return f"{hours}h{minutes:02d}m{sec:02d}s"
    if minutes:
        return f"{minutes}m{sec:02d}s"
    return f"{sec}s"


def print_summary(result: RunResult) -> int:
    passed = sum(1 for item in result.results if item.passed)
    failed = sum(1 for item in result.results if not item.passed and not item.skipped)
    skipped = sum(1 for item in result.results if item.skipped)
    for item in result.results:
        if item.passed:
            print(f"PASS {item.name} ({item.duration_seconds:.2f}s)")
        elif item.skipped:
            print(f"SKIP {item.name}: {item.message}")
        else:
            print(f"FAIL {item.name}: {item.message}")
    print()
    print(f"Suite: {result.suite}")
    print(f"Work dir: {result.work_dir}")
    print(f"Passed: {passed}")
    print(f"Failed: {failed}")
    print(f"Skipped: {skipped}")
    return 1 if failed else 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="Run Neurodesk fulltest suites through pyneurodesk")
    parser.add_argument("--recipe", default=str(default_recipe_path()))
    parser.add_argument("--image-source", default="")
    parser.add_argument("--image-name", default="")
    parser.add_argument("--work-dir", default="")
    parser.add_argument("--filter", default="")
    parser.add_argument("--keep-vm", action="store_true")
    parser.add_argument("--mirror", default=DEFAULT_CVMFS_MIRROR)
    parser.add_argument("--repo", default=DEFAULT_CVMFS_REPO)
    parser.add_argument("--cache-dir", default="")
    parser.add_argument("--prefetch", action="store_true")
    parser.add_argument("--prefetch-workers", type=int, default=4)
    parser.add_argument("--memory-mb", type=int, default=DEFAULT_FULLTEST_MEMORY_MB)
    parser.add_argument("--cpus", type=int, default=DEFAULT_FULLTEST_CPUS)
    return parser


def default_recipe_path() -> Path:
    project_root = Path(__file__).resolve().parents[3]
    local_recipe = project_root / "local" / "neurocontainers" / "recipes" / "niimath" / "fulltest.yaml"
    if local_recipe.is_file():
        return local_recipe
    return project_root / "pyneurodesk" / "fixtures" / "niimath" / "fulltest.yaml"


def main() -> None:
    parser = build_parser()
    args = parser.parse_args()
    runner = FullTestRunner()
    try:
        result = runner.run(
            Options(
                recipe=Path(args.recipe),
                image_source=args.image_source,
                image_name=args.image_name,
                work_dir=Path(args.work_dir) if args.work_dir else None,
                filter_text=args.filter,
                keep_vm=args.keep_vm,
                mirror=args.mirror,
                repo=args.repo,
                cache_dir=args.cache_dir or None,
                prefetch=bool(args.prefetch),
                prefetch_workers=(int(args.prefetch_workers or 0) or None) if args.prefetch else None,
                memory_mb=args.memory_mb or None,
                cpus=args.cpus or None,
            )
        )
    finally:
        runner.close()
    raise SystemExit(print_summary(result))
