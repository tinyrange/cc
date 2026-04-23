from __future__ import annotations

import argparse
import hashlib
import tempfile
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

import httpx
import nibabel as nib
import yaml

from .api import (
    DEFAULT_CVMFS_MIRROR,
    DEFAULT_CVMFS_REPO,
    start_container_daemon,
)
from .client import PyNeurodeskClient
from .models import ContainerReference, ImageSource, ImportImageRequest, ShareMount


DEFAULT_OPENNEURO_BASE = "https://s3.amazonaws.com/openneuro.org"


@dataclass(frozen=True)
class RequiredDataset:
    dataset: str
    files: tuple[str, ...]


@dataclass(frozen=True)
class SuiteScript:
    script: str = ""


@dataclass(frozen=True)
class TestCase:
    name: str
    command: str
    timeout: int = 0
    depends_on: tuple[str, ...] = ()
    expected_output_contains: tuple[str, ...] = ()
    expected_exit_code: int = 0
    validate: tuple[dict[str, Any], ...] = ()


@dataclass(frozen=True)
class Suite:
    name: str
    container: str
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
    work_dir: Path | None = None
    filter_text: str = ""
    keep_vm: bool = False
    mirror: str = DEFAULT_CVMFS_MIRROR
    repo: str = DEFAULT_CVMFS_REPO
    cache_dir: str | None = None
    memory_mb: int | None = None
    cpus: int | None = None


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
    def __init__(self, *, http_client: httpx.Client | None = None) -> None:
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
        share = ShareMount(source=str(work_dir), mount="/work", writable=True)
        guest_vars = build_guest_vars(suite.test_data)
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
            print(f"[fulltest] importing image={reference.image} source={reference.path}", flush=True)
            client.import_image(
                reference.image,
                ImportImageRequest(source=reference.source, cache_dir=reference.cache_dir),
            )
            memory_text = f" memory={options.memory_mb}MiB" if options.memory_mb is not None else ""
            cpu_text = f" cpus={options.cpus}" if options.cpus is not None else ""
            print(f"[fulltest] booting image={reference.image}{memory_text}{cpu_text}", flush=True)
            client.create_instance(reference.image, memory_mb=options.memory_mb, cpus=options.cpus)
            print(f"[fulltest] vm ready image={reference.image}{memory_text}{cpu_text}", flush=True)

            if suite.setup.script.strip():
                print("[fulltest] setup", flush=True)
                output, exit_code = run_shell(
                    client,
                    reference.image,
                    share,
                    substitute_variables(suite.setup.script, guest_vars),
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
                    output, exit_code = run_shell(
                        client,
                        reference.image,
                        share,
                        substitute_variables(test.command, guest_vars),
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
                    run_shell(
                        client,
                        reference.image,
                        share,
                        substitute_variables(suite.cleanup.script, guest_vars),
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
            client.close()
            try:
                with httpx.Client(base_url=daemon.base_url, timeout=2.0) as shutdown_client:
                    shutdown_client.post("/shutdown")
            except Exception:
                pass


def load_suite(path: Path) -> Suite:
    payload = yaml.safe_load(path.read_text()) or {}
    tests = []
    for item in payload.get("tests", []):
        expected_exit_code = item.get("expected_exit_code")
        if expected_exit_code is None:
            expected_exit_code = 0
        tests.append(
            TestCase(
                name=str(item["name"]),
                command=str(item["command"]),
                timeout=int(item.get("timeout") or 0),
                depends_on=tuple(to_string_list(item.get("depends_on"))),
                expected_output_contains=tuple(to_string_list(item.get("expected_output_contains"))),
                expected_exit_code=int(expected_exit_code),
                validate=tuple(item.get("validate") or ()),
            )
        )
    return Suite(
        name=str(payload.get("name") or path.stem),
        container=str(payload["container"]),
        required_files=tuple(
            RequiredDataset(
                dataset=str(entry["dataset"]),
                files=tuple(str(file_name) for file_name in entry.get("files", [])),
            )
            for entry in payload.get("required_files", [])
        ),
        test_data={str(key): str(value) for key, value in (payload.get("test_data") or {}).items()},
        setup=SuiteScript(script=str((payload.get("setup") or {}).get("script", ""))),
        cleanup=SuiteScript(script=str((payload.get("cleanup") or {}).get("script", ""))),
        tests=tuple(tests),
        default_timeout=int(payload.get("default_timeout") or payload.get("default_timout") or 0),
    )


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


def build_host_vars(work_dir: Path, test_data: dict[str, str]) -> dict[str, str]:
    return {key: str(work_dir / Path(value)) for key, value in test_data.items()}


def substitute_variables(text: str, variables: dict[str, str]) -> str:
    result = text
    for key in sorted(variables):
        result = result.replace("${" + key + "}", variables[key])
        result = result.replace("$" + key, variables[key])
    return result


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
    cache_dir: str | None,
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


def run_shell(
    client: PyNeurodeskClient,
    image: str,
    share: ShareMount,
    command: str,
    timeout_seconds: float,
) -> tuple[str, int]:
    result = client.run(
        image,
        ["sh", "-lc", command],
        shares=[share],
        workdir="/work",
        stdin=None,
        timeout=timeout_seconds,
    )
    return result.output, result.exit_code


def validate_test(output: str, exit_code: int, test: TestCase, host_vars: dict[str, str]) -> str:
    if exit_code != test.expected_exit_code:
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
                if not isinstance(arg, list) or len(arg) != 2:
                    return "invalid same_dimensions validation"
                left = Path(substitute_variables(str(arg[0]), host_vars))
                right = Path(substitute_variables(str(arg[1]), host_vars))
                if nib.load(str(left)).shape != nib.load(str(right)).shape:
                    return f"dimension mismatch {left} vs {right}"
            elif kind == "is_3d":
                file_path = Path(substitute_variables(str(arg), host_vars))
                if len(nib.load(str(file_path)).shape) != 3:
                    return f"{file_path} is not 3D"
    return ""


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
    parser.add_argument("--memory-mb", type=int, default=0)
    parser.add_argument("--cpus", type=int, default=0)
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
                memory_mb=args.memory_mb or None,
                cpus=args.cpus or None,
            )
        )
    finally:
        runner.close()
    raise SystemExit(print_summary(result))
