from __future__ import annotations

import json
import os
import posixpath
import re
import shlex
import subprocess
import sys
import uuid
import base64
from collections.abc import Callable
from importlib import resources
from pathlib import Path
from typing import Optional, Protocol, Union

import httpx

from .client import PyNeurodeskClient
from .models import (
    CVMFSReadRequest,
    CVMFSSource,
    ContainerReference,
    DaemonState,
    DeployMetadata,
    ImageSource,
    ImportImageRequest,
    ShareMount,
)

DEFAULT_CVMFS_MIRROR = "https://cvmfs.neurodesk.org"
DEFAULT_CVMFS_REPO = "neurodesk.ardc.edu.au"
DEFAULT_CONTAINERS_PATH = "/containers"
DEFAULT_RELEASES_API = "https://api.github.com/repos/NeuroDesk/neurocontainers/contents/releases"
SINGULARITY_ENV_FILES = (
    "10-docker2singularity.sh",
    "90-environment.sh",
)


class ProgressReporter(Protocol):
    def update(self, step: int, message: str) -> None: ...

    def close(self, message: Optional[str] = None) -> None: ...


class NullProgressReporter:
    def update(self, step: int, message: str) -> None:
        return None

    def close(self, message: Optional[str] = None) -> None:
        return None


class StreamProgressReporter:
    def __init__(self, total_steps: int, stream: Optional[object] = None) -> None:
        self.total_steps = max(total_steps, 1)
        self.stream = stream if stream is not None else sys.stdout
        self._active = False

    def update(self, step: int, message: str) -> None:
        current = max(0, min(step, self.total_steps))
        width = 24
        filled = int(width * current / self.total_steps)
        bar = "#" * filled + "-" * (width - filled)
        line = f"[{bar}] {current}/{self.total_steps} {message}"
        print(f"\r\033[2K{line}", end="", file=self.stream, flush=True)
        self._active = True

    def close(self, message: Optional[str] = None) -> None:
        if message:
            if self._active:
                print(f"\r\033[2K{message}", file=self.stream, flush=True)
            else:
                print(message, file=self.stream, flush=True)
            self._active = False
            return
        if self._active:
            print(file=self.stream, flush=True)
            self._active = False


class NotebookProgressReporter:
    def __init__(self, total_steps: int) -> None:
        self.total_steps = max(total_steps, 1)
        from IPython.display import HTML, display

        self._HTML = HTML
        self._display = display
        self._handle = display(self._render(0, "Starting..."), display_id=True)

    def update(self, step: int, message: str) -> None:
        current = max(0, min(step, self.total_steps))
        self._handle.update(self._render(current, message))

    def close(self, message: Optional[str] = None) -> None:
        if message:
            self._handle.update(self._render(self.total_steps, message))

    def _render(self, step: int, message: str):
        percent = int((100 * step) / self.total_steps)
        return self._HTML(
            """
            <div style="max-width: 42rem; font-family: sans-serif;">
              <div style="margin-bottom: 0.4rem; font-weight: 600;">{message}</div>
              <div style="width: 100%; background: #e5e7eb; border-radius: 9999px; overflow: hidden; height: 0.8rem;">
                <div style="width: {percent}%; background: #2563eb; height: 100%; transition: width 150ms ease;"></div>
              </div>
              <div style="margin-top: 0.35rem; color: #4b5563; font-size: 0.9rem;">Step {step} of {total}</div>
            </div>
            """.format(message=_escape_html(message), percent=percent, step=step, total=self.total_steps)
        )


class SharedDirectory:
    def __init__(self, source: Union[str, os.PathLike[str]], *, writable: bool = False, share_id: Optional[str] = None) -> None:
        source_path = Path(source).expanduser().resolve(strict=True)
        if not source_path.is_dir():
            raise NotADirectoryError(str(source_path))
        self.source = source_path
        self.writable = writable
        self.share_id = share_id or uuid.uuid4().hex

    @property
    def guest_path(self) -> str:
        return f"/.share/{self.share_id}"

    def __truediv__(self, child: Union[str, os.PathLike[str]]) -> "SharedPath":
        return SharedPath(self, (str(child),))


class SharedPath:
    def __init__(self, directory: SharedDirectory, parts: tuple[str, ...] = ()) -> None:
        self.directory = directory
        self.parts = parts

    @property
    def guest_path(self) -> str:
        current = self.directory.guest_path
        for part in self.parts:
            current = posixpath.join(current, str(part))
        return current

    def __truediv__(self, child: Union[str, os.PathLike[str]]) -> "SharedPath":
        return SharedPath(self.directory, self.parts + (str(child),))


def share_dir(source: Union[str, os.PathLike[str]], *, writable: bool = False) -> SharedDirectory:
    return SharedDirectory(source, writable=writable)


class NeurodeskContainer:
    def __init__(
        self,
        client: PyNeurodeskClient,
        reference: ContainerReference,
        *,
        owned_daemon: Optional[DaemonState] = None,
    ) -> None:
        self._client = client
        self.reference = reference
        self._owned_daemon = owned_daemon
        self._closed = False
        self._deploy_metadata: Optional[DeployMetadata] = None

    @property
    def name(self) -> str:
        return self.reference.name

    @property
    def image(self) -> str:
        return self.reference.image

    @property
    def path(self) -> str:
        return self.reference.path

    @property
    def base_url(self) -> str:
        return str(self._client._client.base_url).rstrip("/")

    @property
    def owns_daemon(self) -> bool:
        return self._owned_daemon is not None

    @property
    def deploy_metadata(self) -> DeployMetadata:
        if self._deploy_metadata is None:
            self._deploy_metadata = load_deploy_metadata(self)
        return self._deploy_metadata

    @property
    def commands(self) -> tuple[str, ...]:
        return self.deploy_metadata.commands

    @property
    def deploy_env(self) -> tuple[str, ...]:
        return self.deploy_metadata.deploy_env

    def run(self, *args: object) -> str:
        if self._closed:
            raise RuntimeError("container handle is closed")
        command, shares = self._resolve_command(args)
        if not command:
            raise ValueError("at least one command argument is required")
        deploy_env = self.deploy_env
        try:
            result = self._run_command(command, shares=shares, env=deploy_env)
        except httpx.ConnectError as exc:
            if not self._recover_owned_daemon():
                raise RuntimeError(
                    f"container daemon at {self.base_url} is no longer reachable; create a new container handle"
                ) from exc
            result = self._run_command(command, shares=shares, env=deploy_env)
        if result.exit_code != 0:
            raise RuntimeError(
                f"command {' '.join(command)!r} exited with status {result.exit_code}: {result.output}"
            )
        return result.output

    def shell(self, *args: object) -> str:
        return self.run(*args)

    def __getattr__(self, name: str) -> Callable[..., str]:
        if not name.isidentifier():
            raise AttributeError(name)

        def invoke(*args: object) -> str:
            return self.run(name, *args)

        return invoke

    def close(self) -> None:
        if self._closed:
            return
        try:
            if self._owned_daemon is not None:
                try:
                    self._client.shutdown_instance()
                except httpx.HTTPError:
                    pass
                try:
                    with httpx.Client(base_url=self._owned_daemon.base_url, timeout=2.0) as http_client:
                        http_client.post("/shutdown")
                except httpx.HTTPError:
                    pass
                state_path = daemon_state_path_for_cache_dir(Path(self._owned_daemon.cache_dir))
                state_path.unlink(missing_ok=True)
        finally:
            self._client.close()
            self._closed = True

    def __enter__(self) -> "NeurodeskContainer":
        return self

    def __exit__(self, exc_type: object, exc: object, tb: object) -> None:
        self.close()

    def _resolve_command(self, args: tuple[object, ...]) -> tuple[list[str], list[ShareMount]]:
        share_map: dict[str, ShareMount] = {}
        command: list[str] = []
        for arg in args:
            if isinstance(arg, SharedDirectory):
                share = self._mount_for_directory(arg)
                share_map.setdefault(share.mount, share)
                command.append(arg.guest_path)
                continue
            if isinstance(arg, SharedPath):
                share = self._mount_for_directory(arg.directory)
                share_map.setdefault(share.mount, share)
                command.append(arg.guest_path)
                continue
            command.append(str(arg))
        return command, list(share_map.values())

    def _mount_for_directory(self, directory: SharedDirectory) -> ShareMount:
        return ShareMount(
            source=str(directory.source),
            mount=directory.guest_path,
            writable=directory.writable,
        )

    def _run_command(
        self,
        command: list[str],
        *,
        shares: list[ShareMount],
        env: tuple[str, ...],
    ) -> object:
        try:
            return self._client.run(self.reference.image, command, shares=shares, env=env)
        except TypeError:
            if env:
                raise
            return self._client.run(self.reference.image, command, shares=shares)

    def _recover_owned_daemon(self) -> bool:
        if self._owned_daemon is None or not self._owned_daemon.cache_dir:
            return False
        if _health_check(self.base_url):
            return False
        cache_root = Path(self._owned_daemon.cache_dir)
        self._client.close()
        new_state = start_daemon_for_cache_dir(cache_root)
        self._owned_daemon = new_state
        self._client = PyNeurodeskClient(new_state.base_url)
        self._client.ensure_image(self.reference)
        self._client.ensure_instance(self.reference.image)
        return True


def connect(*, base_url: Optional[str] = None) -> PyNeurodeskClient:
    resolved_base_url = resolve_base_url(base_url)
    return PyNeurodeskClient(resolved_base_url)


def search(
    name: str,
    *,
    base_url: Optional[str] = None,
    client: Optional[PyNeurodeskClient] = None,
    mirror: str = DEFAULT_CVMFS_MIRROR,
    repo: str = DEFAULT_CVMFS_REPO,
    cache_dir: Optional[str] = None,
) -> list[str]:
    local_versions = _search_local_release_versions(name)
    if local_versions:
        return sorted(local_versions)
    remote_versions = _search_remote_release_versions(name)
    if remote_versions:
        return sorted(remote_versions)

    active_client = client if client is not None else connect(base_url=base_url)
    versions, _, _ = _search_versions(
        active_client,
        name,
        mirror=mirror,
        repo=repo,
        cache_dir=cache_dir,
    )
    return versions


def _search_versions(
    client: PyNeurodeskClient,
    name: str,
    *,
    mirror: str,
    repo: str,
    cache_dir: Optional[str],
) -> tuple[list[str], Optional[object], object]:
    normalized_name = name.strip()
    if not normalized_name:
        raise ValueError("container name is required")

    versions: set[str] = set()

    direct_directory = path_join(DEFAULT_CONTAINERS_PATH, normalized_name)
    try:
        direct_entries = client.cvmfs_list(
            CVMFSSource(
                mirror=mirror,
                repo=repo,
                path=direct_directory,
                cache_dir=cache_dir,
            )
        )
    except httpx.HTTPError:
        direct_entries = None
    if direct_entries is not None:
        versions.update(_extract_versions_from_entries(direct_entries, normalized_name))

    root_entries = client.cvmfs_list(
        CVMFSSource(
            mirror=mirror,
            repo=repo,
            path=DEFAULT_CONTAINERS_PATH,
            cache_dir=cache_dir,
        )
    )
    versions.update(_extract_versions_from_entries(root_entries, normalized_name))
    return sorted(versions), direct_entries, root_entries


def container(
    name: str,
    *,
    base_url: Optional[str] = None,
    client: Optional[PyNeurodeskClient] = None,
    mirror: str = DEFAULT_CVMFS_MIRROR,
    repo: str = DEFAULT_CVMFS_REPO,
    cache_dir: Optional[str] = None,
    prefetch: bool = False,
    prefetch_workers: int = 4,
    progress: bool = True,
    debug: bool = False,
) -> NeurodeskContainer:
    reporter = create_progress_reporter(enabled=progress, total_steps=11)
    reporter.update(1, f"Preparing container {name}")
    try:
        reporter.update(2, f"Checking bundled metadata for {name}")
        reference = _resolve_release_reference(name, mirror=mirror, repo=repo, cache_dir=cache_dir)

        if client is not None:
            reporter.update(3, f"Using existing daemon client for {name}")
            active_client = client
        elif base_url is not None and base_url.strip():
            reporter.update(3, f"Connecting to daemon at {base_url.rstrip('/')}")
            active_client = connect(base_url=base_url)
        else:
            reporter.update(3, "Starting shared daemon")
            daemon = start_default_daemon()
            active_client = PyNeurodeskClient(daemon.base_url)

        if reference is None:
            reporter.update(4, f"Searching CVMFS for {name}")
            reference = resolve_container_reference(
                active_client,
                name,
                mirror=mirror,
                repo=repo,
                cache_dir=cache_dir,
            )
        else:
            reporter.update(4, f"Resolved {name} from bundled release metadata")

        reporter.update(5, f"Checking image cache for {reference.image}")
        image_state = active_client.get_image(reference.image)
        if image_state is None:
            reporter.update(6, f"Importing {Path(reference.path).name}")
            active_client.import_image(
                reference.image,
                ImportImageRequest(
                    source=reference.source,
                    cache_dir=reference.cache_dir,
                    prefetch=prefetch,
                    prefetch_workers=prefetch_workers if prefetch else None,
                ),
            )
        else:
            reporter.update(6, f"Image {reference.image} is already cached")

        reporter.update(7, f"Checking VM status for {reference.image}")
        vm_state = active_client.instance_status()
        if vm_state.status == "running":
            if vm_state.image == reference.image:
                reporter.update(8, f"VM for {reference.image} is already running")
            elif vm_state.image in ("", None):
                reporter.update(8, f"Restarting blank VM for {reference.image}")
                active_client.shutdown_instance()
                vm_state = active_client.instance_status()
            else:
                reporter.update(8, f"VM is already running with {vm_state.image}; {reference.image} will mount on demand")
        if vm_state.status != "running":
            reporter.update(8, "Downloading required file 1/2: Linux kernel")
            _report_required_download(
                reporter,
                step=8,
                index=1,
                total=2,
                fallback_label="kernel",
                stream_method=(lambda: active_client.download_kernel_stream())
                if hasattr(active_client, "download_kernel_stream")
                else None,
                request=lambda: active_client.download_kernel(),
            )
            reporter.update(9, "Downloading required file 2/2: emulator")
            _report_required_download(
                reporter,
                step=9,
                index=2,
                total=2,
                fallback_label="emulator",
                stream_method=(lambda: active_client.prepare_image_emulator_stream(reference.image))
                if hasattr(active_client, "prepare_image_emulator_stream")
                else None,
                request=lambda: active_client.prepare_image_emulator(reference.image),
            )
            reporter.update(10, f"Preparing image metadata for {reference.image}")
            active_client.prepare_image_metadata(reference.image)
            boot_message = f"Requesting boot of {reference.image} and waiting for guest ready"
            if debug:
                boot_message += " with serial console enabled"
            reporter.update(11, boot_message)
            if debug:
                _stream_debug_boot(active_client, reference.image)
            else:
                active_client.create_instance(reference.image, dmesg=False)
        container_handle = NeurodeskContainer(active_client, reference)
        reporter.close(f"{reference.image} is ready")
        return container_handle
    except httpx.HTTPStatusError as exc:
        if debug:
            _emit_debug_boot_log(exc)
        reporter.close(f"Failed to prepare {name}")
        raise
    except Exception:
        reporter.close(f"Failed to prepare {name}")
        raise


def resolve_container_reference(
    client: Optional[PyNeurodeskClient],
    name: str,
    *,
    base_url: Optional[str] = None,
    mirror: str,
    repo: str,
    cache_dir: Optional[str] = None,
) -> ContainerReference:
    normalized_name = name.strip()
    if not normalized_name:
        raise ValueError("container name is required")

    metadata_reference = _resolve_release_reference(
        normalized_name,
        mirror=mirror,
        repo=repo,
        cache_dir=cache_dir,
    )
    if metadata_reference is not None:
        return metadata_reference

    active_client = client if client is not None else connect(base_url=base_url)

    versions, direct_entries, root_entries = _search_versions(
        active_client,
        normalized_name,
        mirror=mirror,
        repo=repo,
        cache_dir=cache_dir,
    )
    if not versions:
        raise LookupError(f"container {normalized_name!r} was not found in CVMFS")

    selected_version = _select_preferred_version(versions)
    selected_path = _resolve_version_path(
        active_client,
        normalized_name,
        selected_version,
        direct_entries=direct_entries,
        root_entries=root_entries,
        mirror=mirror,
        repo=repo,
        cache_dir=cache_dir,
    )
    return ContainerReference(
        name=normalized_name,
        image=normalized_name,
        source=ImageSource(
            type="cvmfs",
            mirror=mirror,
            repo=repo,
            path=selected_path,
        ),
        cache_dir=cache_dir,
    )


def load_deploy_metadata(container_handle: NeurodeskContainer) -> DeployMetadata:
    directory = deploy_directory_for_reference_path(container_handle.reference.path)
    if not directory.startswith(DEFAULT_CONTAINERS_PATH):
        return load_local_deploy_metadata(container_handle)
    source = CVMFSSource(
        mirror=DEFAULT_CVMFS_MIRROR,
        repo=DEFAULT_CVMFS_REPO,
        path=directory,
        cache_dir=container_handle.reference.cache_dir,
    )
    try:
        entries = container_handle._client.cvmfs_list(source)
    except (AttributeError, httpx.HTTPError):
        return DeployMetadata()
    entry_names = {entry.name for entry in entries.entries if entry.kind == "file"}
    commands_text = read_cvmfs_text(container_handle, f"{directory}/commands.txt", allow_missing=True)
    commands = tuple(
        sorted(
            {
                line.strip()
                for line in commands_text.splitlines()
                if line.strip() in entry_names and is_valid_wrapper_name(line.strip())
            }
        )
    )
    deploy_env_text = read_cvmfs_text(container_handle, f"{directory}/env.txt", allow_missing=True)
    deploy_env = [
        line
        for line in (
            normalize_deploy_env_line(line.strip())
            for line in deploy_env_text.splitlines()
            if line.strip()
        )
        if line is not None
    ]
    image_env = load_singularity_env(container_handle, directory)
    return DeployMetadata(commands=commands, deploy_env=merge_env_entries([*image_env, *deploy_env]))


def load_local_deploy_metadata(container_handle: NeurodeskContainer) -> DeployMetadata:
    try:
        metadata = container_handle._client.prepare_image_metadata(container_handle.reference.image)
    except (AttributeError, httpx.HTTPError):
        return DeployMetadata()
    deploy_env = tuple(metadata.env)
    deploy_bins = []
    for entry in deploy_env:
        key, _, value = entry.partition("=")
        if key != "DEPLOY_BINS":
            continue
        deploy_bins.extend(part for part in value.split(":") if is_valid_wrapper_name(part))
    return DeployMetadata(commands=tuple(sorted(set(deploy_bins))), deploy_env=merge_env_entries(list(deploy_env)))


def load_singularity_env(container_handle: NeurodeskContainer, directory: str) -> tuple[str, ...]:
    image_env_dir = singularity_env_dir_for_deploy_directory(directory)
    env: list[str] = []
    for name in SINGULARITY_ENV_FILES:
        text = read_cvmfs_text(container_handle, f"{image_env_dir}/{name}", allow_missing=True)
        env.extend(parse_singularity_env_exports(text))
    return tuple(env)


def singularity_env_dir_for_deploy_directory(directory: str) -> str:
    clean = directory.rstrip("/")
    image_root = clean if clean.endswith(".simg") else f"{clean}/{posixpath.basename(clean)}.simg"
    return f"{image_root}/.singularity.d/env"


def parse_singularity_env_exports(text: str) -> tuple[str, ...]:
    env: list[str] = []
    for raw_line in text.splitlines():
        line = raw_line.strip()
        if not line.startswith("export "):
            continue
        assignment = line.removeprefix("export ").strip()
        if "=" not in assignment:
            continue
        key, value = assignment.split("=", 1)
        key = key.strip()
        if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key):
            continue
        parsed = parse_singularity_env_value(value.strip())
        if parsed is None:
            continue
        env.append(f"{key}={parsed}")
    return merge_env_entries(env)


def parse_singularity_env_value(value: str) -> Optional[str]:
    default_value = value
    if len(default_value) >= 2 and default_value[0] == default_value[-1] and default_value[0] in {"'", '"'}:
        default_value = default_value[1:-1]
    default_match = re.fullmatch(r"""\$\{[A-Za-z_][A-Za-z0-9_]*:-(["'])(.*)\1\}""", default_value)
    if default_match:
        return default_match.group(2)
    try:
        parts = shlex.split(value, posix=True)
    except ValueError:
        return None
    if len(parts) != 1:
        return None
    parsed = parts[0]
    if "$" in parsed:
        return None
    return parsed


def merge_env_entries(entries: list[str]) -> tuple[str, ...]:
    merged: dict[str, str] = {}
    for entry in entries:
        if "=" not in entry:
            continue
        key, value = entry.split("=", 1)
        merged[key] = value
    return tuple(f"{key}={value}" for key, value in merged.items())


def read_cvmfs_text(
    container_handle: NeurodeskContainer,
    path: str,
    *,
    allow_missing: bool = False,
) -> str:
    try:
        response = container_handle._client.cvmfs_read(
            CVMFSReadRequest(
                mirror=DEFAULT_CVMFS_MIRROR,
                repo=DEFAULT_CVMFS_REPO,
                path=path,
                length=1_000_000,
                cache_dir=container_handle.reference.cache_dir,
            )
        )
    except (AttributeError, AssertionError, httpx.HTTPError):
        if allow_missing:
            return ""
        raise
    return decode_cvmfs_text(response.data)


def decode_cvmfs_text(raw: bytes) -> str:
    text = raw.decode("utf-8", errors="replace")
    stripped = "".join(text.split())
    if stripped:
        try:
            decoded = base64.b64decode(stripped, validate=True)
        except Exception:
            return text
        decoded_text = decoded.decode("utf-8", errors="replace")
        if decoded_text:
            return decoded_text
    return text


def deploy_directory_for_reference_path(path: str) -> str:
    if path.endswith(".simg"):
        return str(Path(path).parent).replace("\\", "/")
    return path


def normalize_deploy_env_line(line: str) -> Optional[str]:
    if "=" not in line:
        return None
    key, value = line.split("=", 1)
    key = key.strip()
    value = value.strip()
    if not key:
        return None
    value = value.replace("BASEPATH/", "/")
    value = value.replace("BASEPATH", "/")
    return f"{key}={value}"


def is_valid_wrapper_name(command: str) -> bool:
    return bool(command) and "/" not in command and command not in {".", ".."}


def _resolve_release_reference(
    name: str,
    *,
    mirror: str,
    repo: str,
    cache_dir: Optional[str],
) -> Optional[ContainerReference]:
    normalized_name = name.strip()
    if not normalized_name:
        raise ValueError("container name is required")

    local_versions = _search_local_release_versions(normalized_name)
    if local_versions:
        selected_version = _select_preferred_version(sorted(local_versions))
        selected_path = build_release_container_path(
            normalized_name,
            selected_version,
            local_versions[selected_version],
        )
        return ContainerReference(
            name=normalized_name,
            image=normalized_name,
            source=ImageSource(
                type="cvmfs",
                mirror=mirror,
                repo=repo,
                path=selected_path,
            ),
            cache_dir=cache_dir,
        )

    remote_versions = _search_remote_release_versions(normalized_name)
    if remote_versions:
        selected_version = _select_preferred_version(sorted(remote_versions))
        selected_path = build_release_container_path(
            normalized_name,
            selected_version,
            remote_versions[selected_version],
        )
        return ContainerReference(
            name=normalized_name,
            image=normalized_name,
            source=ImageSource(
                type="cvmfs",
                mirror=mirror,
                repo=repo,
                path=selected_path,
            ),
            cache_dir=cache_dir,
        )
    return None


def start_default_daemon() -> DaemonState:
    cache_root = default_cache_root()
    cache_root.mkdir(parents=True, exist_ok=True)
    state_path = daemon_state_path_for_cache_dir(cache_root)
    if state_path.exists():
        state = DaemonState.from_file(state_path)
        if _health_check(state.base_url):
            if _supports_vm_start(state.base_url):
                return state
            _shutdown_daemon_server(state.base_url)
            state_path.unlink(missing_ok=True)
            return start_daemon_for_cache_dir(cache_root)
        state_path.unlink(missing_ok=True)

    return start_daemon_for_cache_dir(cache_root)


def start_container_daemon() -> DaemonState:
    cache_root = create_container_cache_dir()
    return start_daemon_for_cache_dir(cache_root)


def start_daemon_for_cache_dir(cache_root: Path) -> DaemonState:
    cache_root.mkdir(parents=True, exist_ok=True)
    state_path = daemon_state_path_for_cache_dir(cache_root)
    ccvm_path = resolve_ccvm_binary_path()
    working_dir = repo_root() or ccvm_path.parent
    log_path = cache_root / "ccvm-python.log"
    with log_path.open("ab") as log_file:
        proc = subprocess.Popen(
            [str(ccvm_path), "-cache-dir", str(cache_root)],
            stdout=subprocess.PIPE,
            stderr=log_file,
            text=True,
            start_new_session=True,
            cwd=str(working_dir),
        )
        hello_line = ""
        assert proc.stdout is not None
        hello_line = proc.stdout.readline()
        if not hello_line:
            proc.kill()
            proc.wait()
            raise RuntimeError(f"failed to start ccvm from {ccvm_path}; see {log_path}")

    try:
        payload = json.loads(hello_line)
    except json.JSONDecodeError as exc:
        proc.kill()
        proc.wait()
        raise RuntimeError(f"failed to decode ccvm startup banner: {hello_line!r}") from exc

    addr = str(payload.get("addr", "")).strip()
    if not addr:
        proc.kill()
        proc.wait()
        raise RuntimeError(f"ccvm startup banner did not include an address: {payload!r}")

    state = DaemonState(addr=addr, cache_dir=str(cache_root))
    state_path.write_text(json.dumps({"addr": addr}, indent=2))
    if not _health_check(state.base_url):
        state_path.unlink(missing_ok=True)
        raise RuntimeError(f"started ccvm at {state.base_url}, but health check failed")
    return state


def resolve_ccvm_binary_path() -> Path:
    for env_name in ("PYNEURODESK_CCVM", "CCX3_CCVM", "CCVM_BINARY"):
        value = os.environ.get(env_name, "").strip()
        if value:
            path = Path(value).expanduser()
            if path.exists():
                return path
            raise RuntimeError(f"{env_name} points to missing ccvm binary: {path}")

    package_binary = bundled_ccvm_path()
    if package_binary is not None:
        return package_binary

    project_root = pyneurodesk_root()
    candidates = [
        project_root / "bin" / "ccvm",
        project_root / "bin" / "ccvm.exe",
    ]
    for candidate in candidates:
        if candidate.exists():
            maybe_refresh_bundled_ccvm(candidate)
            return candidate

    raise RuntimeError(
        "unable to find bundled ccvm binary; expected one of "
        + ", ".join(str(candidate) for candidate in candidates)
    )


def bundled_ccvm_path() -> Optional[Path]:
    for name in ("ccvm", "ccvm.exe"):
        candidate = resources.files("pyneurodesk").joinpath("bin", name)
        if isinstance(candidate, Path) and candidate.exists():
            return candidate
    return None


def pyneurodesk_root() -> Path:
    return Path(__file__).resolve().parents[2]


def repo_root() -> Optional[Path]:
    for root in (pyneurodesk_root(), *pyneurodesk_root().parents):
        if (root / "go.mod").exists() and (root / "cmd" / "ccvm" / "main.go").exists():
            return root
    return None


def maybe_refresh_bundled_ccvm(binary_path: Path) -> None:
    root = repo_root()
    if root is None:
        return
    if not _should_rebuild_ccvm(binary_path, root):
        return
    _build_ccvm_binary(binary_path, root)


def _should_rebuild_ccvm(binary_path: Path, root: Path) -> bool:
    if not binary_path.exists():
        return True
    try:
        binary_mtime = binary_path.stat().st_mtime
    except OSError:
        return True
    for rel_dir in ("cmd/ccvm", "client", "internal"):
        source_dir = root / rel_dir
        if not source_dir.exists():
            continue
        for source in source_dir.rglob("*.go"):
            try:
                if source.stat().st_mtime > binary_mtime:
                    return True
            except OSError:
                continue
    return False


def _build_ccvm_binary(binary_path: Path, root: Path) -> None:
    binary_path.parent.mkdir(parents=True, exist_ok=True)
    proc = subprocess.run(
        ["go", "build", "-o", str(binary_path), "./cmd/ccvm"],
        cwd=root,
        capture_output=True,
        text=True,
        check=False,
    )
    if proc.returncode != 0:
        stderr = proc.stderr.strip()
        stdout = proc.stdout.strip()
        detail = stderr or stdout or f"exit status {proc.returncode}"
        raise RuntimeError(f"failed to build bundled ccvm binary: {detail}")


def default_cache_root() -> Path:
    home = Path.home()
    if sys.platform == "darwin":
        cache_root = Path(os.environ.get("HOME", str(home))) / "Library" / "Caches"
    elif os.name == "nt":
        cache_root = Path(os.environ.get("LOCALAPPDATA", home / "AppData" / "Local"))
    else:
        cache_root = Path(os.environ.get("XDG_CACHE_HOME", home / ".cache"))
    return cache_root / "ccx3"


def default_daemon_state_path() -> Path:
    return daemon_state_path_for_cache_dir(default_cache_root())


def daemon_state_path_for_cache_dir(cache_root: Path) -> Path:
    return cache_root / "ccvm.json"


def create_container_cache_dir() -> Path:
    return default_cache_root() / "pyneurodesk-daemons" / uuid.uuid4().hex


def _health_check(base_url: str) -> bool:
    try:
        with httpx.Client(base_url=base_url, timeout=2.0) as client:
            response = client.get("/healthz")
            return response.status_code == 200
    except httpx.HTTPError:
        return False


def _supports_vm_start(base_url: str) -> bool:
    try:
        with httpx.Client(base_url=base_url, timeout=2.0) as client:
            response = client.get("/vm/start")
            return response.status_code != 404
    except httpx.HTTPError:
        return False


def _shutdown_daemon_server(base_url: str) -> None:
    try:
        with httpx.Client(base_url=base_url, timeout=2.0) as client:
            client.post("/shutdown")
    except httpx.HTTPError:
        return


def resolve_base_url(base_url: Optional[str] = None) -> str:
    if base_url is not None and base_url.strip():
        return base_url.rstrip("/")

    for env_name in ("CCX3_URL", "CCVM_URL"):
        value = os.environ.get(env_name, "").strip()
        if value:
            return value.rstrip("/")

    state_path = default_daemon_state_path()
    if state_path.exists():
        state = DaemonState.from_file(state_path)
        if _health_check(state.base_url):
            return state.base_url
        state_path.unlink(missing_ok=True)

    return start_default_daemon().base_url


def create_progress_reporter(*, enabled: bool, total_steps: int) -> ProgressReporter:
    if not enabled:
        return NullProgressReporter()
    if _supports_notebook_display():
        try:
            return NotebookProgressReporter(total_steps)
        except Exception:
            pass
    return StreamProgressReporter(total_steps)


def _supports_notebook_display() -> bool:
    try:
        from IPython import get_ipython
    except Exception:
        return False
    shell = get_ipython()
    if shell is None:
        return False
    return shell.__class__.__name__ in {"ZMQInteractiveShell", "Shell"}


def _escape_html(value: str) -> str:
    return (
        value.replace("&", "&amp;")
        .replace("<", "&lt;")
        .replace(">", "&gt;")
        .replace('"', "&quot;")
    )


def _describe_required_download(index: int, total: int, label: str, state: object) -> str:
    status = getattr(state, "status", None)
    path = getattr(state, "path", None)
    source = getattr(state, "source", None)
    required = getattr(state, "required", None)
    artifact_name = label
    if isinstance(path, str) and path:
        artifact_name = Path(path).name or label
    elif isinstance(source, str) and source:
        artifact_name = Path(source).name or label
    if required is False and status != "downloaded":
        return f"Required file {index}/{total}: {artifact_name} already available"
    if status == "downloaded":
        return f"Downloaded required file {index}/{total}: {artifact_name}"
    return f"Preparing required file {index}/{total}: {artifact_name}"


def _stream_required_download_progress(
    reporter: ProgressReporter,
    *,
    step: int,
    index: int,
    total: int,
    fallback_label: str,
    events: object,
) -> None:
    last_message: Optional[str] = None
    for event in events:
        status = getattr(event, "status", None)
        if status == "error":
            error = getattr(event, "error", None) or f"required file {index}/{total} failed"
            raise RuntimeError(error)
        message = _format_required_download_progress(index, total, fallback_label, event)
        last_message = message
        reporter.update(step, message)
    if last_message is None:
        reporter.update(step, f"Prepared required file {index}/{total}: {fallback_label}")


def _report_required_download(
    reporter: ProgressReporter,
    *,
    step: int,
    index: int,
    total: int,
    fallback_label: str,
    stream_method: Optional[object],
    request: Callable[[], object],
) -> None:
    if callable(stream_method):
        _stream_required_download_progress(
            reporter,
            step=step,
            index=index,
            total=total,
            fallback_label=fallback_label,
            events=stream_method(),
        )
        return
    reporter.update(step, _describe_required_download(index, total, fallback_label, request()))


def _format_required_download_progress(index: int, total: int, fallback_label: str, event: object) -> str:
    artifact = getattr(event, "artifact", None) or fallback_label
    status = getattr(event, "status", None)
    downloaded = getattr(event, "bytes_downloaded", None)
    total_bytes = getattr(event, "bytes_total", None)
    rate = getattr(event, "rate_bytes_per_second", None)
    eta = getattr(event, "eta_seconds", None)

    parts = [f"required file {index}/{total}: {artifact}"]
    if isinstance(downloaded, int) and downloaded >= 0:
        if isinstance(total_bytes, int) and total_bytes > 0:
            parts.append(f"{_format_byte_size(downloaded)}/{_format_byte_size(total_bytes)}")
        else:
            parts.append(_format_byte_size(downloaded))
    if isinstance(rate, (int, float)) and rate > 0:
        parts.append(f"{_format_byte_size(float(rate))}/s")
    if isinstance(eta, (int, float)) and eta > 0:
        parts.append(f"ETA {_format_duration(float(eta))}")

    prefix = "Downloading"
    if status == "downloaded":
        prefix = "Downloaded"
    elif status not in ("downloading", None):
        prefix = "Preparing"
    return f"{prefix} " + " | ".join(parts)


def _format_byte_size(value: float) -> str:
    units = ("B", "KB", "MB", "GB", "TB")
    size = float(value)
    for unit in units:
        if size < 1024.0 or unit == units[-1]:
            if unit == "B":
                return f"{int(size)} {unit}"
            return f"{size:.1f} {unit}"
        size /= 1024.0
    return f"{size:.1f} TB"


def _format_duration(seconds: float) -> str:
    remaining = max(0, int(round(seconds)))
    minutes, secs = divmod(remaining, 60)
    hours, minutes = divmod(minutes, 60)
    if hours > 0:
        return f"{hours}h{minutes:02d}m"
    if minutes > 0:
        return f"{minutes}m{secs:02d}s"
    return f"{secs}s"


def _emit_debug_boot_log(exc: httpx.HTTPStatusError) -> None:
    response = exc.response
    if response is None:
        print(f"ccx3 boot debug: {exc}", flush=True)
        return
    body = response.text.strip()
    if not body:
        print(f"ccx3 boot debug: {exc}", flush=True)
        return
    print("ccx3 boot debug output:", flush=True)
    print(body, flush=True)


def _stream_debug_boot(client: PyNeurodeskClient, image: str) -> None:
    last_error: Optional[str] = None
    for event in client.create_instance_stream(image, dmesg=True):
        kind = str(event.get("kind", ""))
        if kind == "serial":
            data = event.get("data", "")
            if data:
                print(data, end="", flush=True)
            continue
        if kind == "status":
            message = str(event.get("message", "")).strip()
            if message:
                print(f"ccx3 boot: {message}", flush=True)
            continue
        if kind == "ready":
            return
        if kind == "error":
            last_error = str(event.get("error", "")).strip() or "boot failed"
            break
    if last_error:
        raise RuntimeError(last_error)
    raise RuntimeError(f"boot stream for {image} ended before reporting readiness")


def _find_exact_directory(entries: object, name: str):
    for entry in entries.entries:
        if entry.kind == "directory" and entry.name == name:
            return entry
    return None


def _find_prefix_directory(entries: object, name: str):
    matches = [
        entry
        for entry in entries.entries
        if entry.kind == "directory" and entry.name.startswith(f"{name}_")
    ]
    if not matches:
        return None
    return sorted(matches, key=lambda entry: entry.name)[-1]

def _find_best_container_path(entries: object, name: str) -> Optional[str]:
    exact_dir: Optional[str] = None
    prefix_dirs: list[str] = []
    for entry in entries.entries:
        if entry.kind == "directory":
            if entry.name == name:
                exact_dir = entry.path
            elif entry.name.startswith(f"{name}_"):
                prefix_dirs.append(entry.path)
    if exact_dir is not None:
        return exact_dir
    if prefix_dirs:
        return sorted(prefix_dirs)[-1]
    return None


def _find_simg_in_directory(
    client: PyNeurodeskClient,
    directory: str,
    name: str,
    mirror: str,
    repo: str,
    cache_dir: Optional[str],
) -> str:
    entries = client.cvmfs_list(
        CVMFSSource(
            mirror=mirror,
            repo=repo,
            path=directory,
            cache_dir=cache_dir,
        )
    )
    container_path = _find_best_container_path(entries, name)
    if container_path is not None:
        return container_path
    raise LookupError(f"no container directory found under {directory}")


def _try_find_simg_in_directory(
    client: PyNeurodeskClient,
    directory: str,
    name: str,
    mirror: str,
    repo: str,
    cache_dir: Optional[str],
) -> Optional[str]:
    try:
        return _find_simg_in_directory(client, directory, name, mirror, repo, cache_dir)
    except (LookupError, httpx.HTTPStatusError):
        return None


def path_join(base: str, name: str) -> str:
    base = base.rstrip("/")
    name = name.lstrip("/")
    if not base:
        return "/" + name
    return f"{base}/{name}"


def _extract_versions_from_entries(entries: object, name: str) -> set[str]:
    versions: set[str] = set()
    prefix = f"{name}_"
    for entry in entries.entries:
        if entry.kind == "directory":
            if entry.name == name:
                versions.add(name)
            elif entry.name.startswith(prefix):
                versions.add(entry.name[len(prefix) :])
    return versions


def _search_local_release_versions(name: str) -> dict[str, str]:
    releases_dir = resolve_release_index_dir()
    if releases_dir is None:
        return {}
    container_dir = releases_dir / name
    if not container_dir.is_dir():
        return {}

    ret: dict[str, str] = {}
    for path in sorted(container_dir.glob("*.json")):
        build = _extract_release_build(path)
        if build:
            ret[path.stem] = build
    return ret


def _search_remote_release_versions(name: str) -> dict[str, str]:
    api_base = os.environ.get("PYNEURODESK_RELEASES_API", DEFAULT_RELEASES_API).rstrip("/")
    url = f"{api_base}/{name}"
    try:
        with httpx.Client(timeout=httpx.Timeout(connect=5.0, read=20.0, write=20.0, pool=5.0)) as client:
            response = client.get(url, headers={"Accept": "application/vnd.github+json"})
            if response.status_code == 404:
                return {}
            response.raise_for_status()
            payload = response.json()
    except Exception:
        return {}

    if not isinstance(payload, list):
        return {}

    versions: dict[str, str] = {}
    for entry in payload:
        if not isinstance(entry, dict):
            continue
        name_value = entry.get("name")
        download_url = entry.get("download_url")
        if not isinstance(name_value, str) or not name_value.endswith(".json"):
            continue
        if not isinstance(download_url, str) or not download_url:
            continue
        build = _extract_remote_release_build(download_url)
        if build:
            versions[Path(name_value).stem] = build
    return versions


def _extract_remote_release_build(download_url: str) -> Optional[str]:
    try:
        with httpx.Client(timeout=httpx.Timeout(connect=5.0, read=20.0, write=20.0, pool=5.0)) as client:
            response = client.get(download_url)
            response.raise_for_status()
            payload = response.json()
    except Exception:
        return None
    apps = payload.get("apps")
    if not isinstance(apps, dict) or not apps:
        return None
    first = next(iter(apps.values()))
    if not isinstance(first, dict):
        return None
    build = first.get("version")
    if not isinstance(build, str) or not build.strip():
        return None
    return build.strip()


def resolve_release_index_dir() -> Optional[Path]:
    env = os.environ.get("PYNEURODESK_RELEASES_DIR", "").strip()
    if env:
        path = Path(env).expanduser()
        return path if path.is_dir() else None

    candidate = pyneurodesk_root().parent / "neurocontainers" / "releases"
    if candidate.is_dir():
        return candidate
    return None


def _extract_release_build(path: Path) -> Optional[str]:
    try:
        payload = json.loads(path.read_text())
    except Exception:
        return None
    apps = payload.get("apps")
    if not isinstance(apps, dict) or not apps:
        return None
    first = next(iter(apps.values()))
    if not isinstance(first, dict):
        return None
    build = first.get("version")
    if not isinstance(build, str) or not build.strip():
        return None
    return build.strip()


def build_release_container_path(name: str, version: str, build: str) -> str:
    stem = f"{name}_{version}_{build}"
    return path_join(path_join(DEFAULT_CONTAINERS_PATH, stem), f"{stem}.simg")


def _select_preferred_version(versions: list[str]) -> str:
    def sort_key(value: str) -> tuple[int, str]:
        if value == "":
            return (0, value)
        if value.isidentifier():
            return (0, value)
        return (1, value)

    return sorted(versions, key=sort_key)[-1]


def _resolve_version_path(
    client: PyNeurodeskClient,
    name: str,
    version: str,
    *,
    direct_entries: Optional[object],
    root_entries: object,
    mirror: str,
    repo: str,
    cache_dir: Optional[str],
) -> str:
    if version == name:
        return path_join(DEFAULT_CONTAINERS_PATH, name)

    versioned_prefix = f"{name}_{version}"
    root_entry = _find_matching_entry(root_entries, versioned_prefix)
    if root_entry is not None:
        if root_entry.kind == "directory":
            return root_entry.path
        if root_entry.kind == "file":
            return root_entry.path
    versioned_directory = path_join(DEFAULT_CONTAINERS_PATH, versioned_prefix)
    simg_path = _try_find_simg_in_directory(client, versioned_directory, name, mirror, repo, cache_dir)
    if simg_path is not None:
        return simg_path
    return versioned_directory


def _find_matching_entry(entries: object, stem: str):
    for entry in entries.entries:
        if entry.kind == "directory" and entry.name == stem:
            return entry
    return None
