from __future__ import annotations

import argparse
import base64
import binascii
import hashlib
import json
import os
import re
import shlex
import shutil
import socket
import stat
import subprocess
import sys
import tempfile
import time
import urllib.parse
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import BinaryIO, Callable, Optional

import httpx

from .api import (
    DEFAULT_CVMFS_MIRROR,
    DEFAULT_CVMFS_REPO,
    ProgressReporter,
    _format_byte_size,
    _format_duration,
    _report_image_import,
    _report_required_download,
    _shutdown_daemon_server,
    connect,
    container,
    create_progress_reporter,
    cvmfs_mirrors_for,
    default_cache_root,
    load_deploy_metadata,
    resolve_container_reference,
    runtime_deploy_env_entries,
    start_container_daemon,
    start_daemon_for_cache_dir,
    start_default_daemon,
)
from .models import (
    ContainerReference,
    ImageSource,
    ImportImageRequest,
    NetworkConfig,
    PortForward,
    ShareMount,
)

SESSION_ENV = "PYNEURODESK_SHELL_SESSION"
SESSION_ROOT_ENV = "PYNEURODESK_SHELL_ROOT"
SESSION_BIN_ENV = "PYNEURODESK_SHELL_BIN"
BOOTSTRAP_PID_ENV = "PYNEURODESK_SHELL_BOOTSTRAP_PID"
STATE_VERSION = 1
HOST_CWD_MOUNT_ROOT = "/.hostcwd"
SESSION_ENV_FILENAME = "env.sh"
NEURODESKTOP_PROXY_TIMEOUT_SECONDS = 300


@dataclass(frozen=True)
class WrapperSpec:
    image: str
    command: str


@dataclass
class SessionState:
    session_id: str
    root: Path
    images: dict[str, dict[str, object]]
    wrappers: dict[str, WrapperSpec]
    network: dict[str, object] | None = None
    vm_id: str | None = None

    @property
    def bin_dir(self) -> Path:
        return self.root / "bin"

    @property
    def state_path(self) -> Path:
        return self.root / "state.json"

    def to_payload(self) -> dict[str, object]:
        return {
            "version": STATE_VERSION,
            "session_id": self.session_id,
            "vm_id": self.vm_id or "",
            "images": self.images,
            "network": self.network or {},
            "wrappers": {
                name: {"image": spec.image, "command": spec.command}
                for name, spec in sorted(self.wrappers.items())
            },
        }


def main(argv: Optional[list[str]] = None) -> int:
    parser = build_parser()
    args = parser.parse_args(argv)
    handler = getattr(args, "handler", None)
    if handler is None:
        parser.print_help()
        return 0
    return int(handler(args) or 0)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(description="PyNeurodesk shell integration")
    subparsers = parser.add_subparsers(dest="command")

    activate_parser = subparsers.add_parser(
        "activate", help="Emit shell activation code"
    )
    activate_parser.add_argument(
        "--shell", choices=("bash", "zsh", "powershell", "pwsh"), default=None
    )
    activate_parser.add_argument("--no-bootstrap", action="store_true")
    activate_parser.add_argument("--with-network", action="store_true")
    activate_parser.add_argument(
        "--vm", default="", help="Bind this shell session to a named cc VM"
    )
    activate_parser.set_defaults(handler=handle_activate)

    completion_parser = subparsers.add_parser(
        "completion", help="Emit shell completion code"
    )
    completion_parser.add_argument(
        "--shell", choices=("bash", "zsh", "powershell", "pwsh"), required=True
    )
    completion_parser.set_defaults(handler=handle_completion)

    neurodesktop_parser = subparsers.add_parser(
        "neurodesktop", help="Start Neurodesktop JupyterLab through cc"
    )
    neurodesktop_parser.add_argument("--image", default="neurodesktop")
    neurodesktop_parser.add_argument("--image-path", default="")
    neurodesktop_parser.add_argument("--host", default="127.0.0.1")
    neurodesktop_parser.add_argument(
        "--port",
        type=int,
        default=0,
        help="Host port for JupyterLab; 0 chooses a free port",
    )
    neurodesktop_parser.add_argument("--guest-port", type=int, default=8888)
    neurodesktop_parser.add_argument("--memory-mb", type=int, default=8192)
    neurodesktop_parser.add_argument("--cpus", type=int, default=1)
    neurodesktop_parser.add_argument("--cache-dir", default="")
    neurodesktop_parser.add_argument(
        "--no-prefetch",
        action="store_true",
        help="Disable CVMFS cache preloading before VM boot",
    )
    neurodesktop_parser.add_argument(
        "--prefetch-workers",
        type=int,
        default=0,
        help="Worker count for CVMFS preloading; 0 uses the daemon default",
    )
    neurodesktop_parser.add_argument(
        "--no-internet", action="store_true", help="Disable guest internet forwarding"
    )
    neurodesktop_parser.add_argument("--startup-timeout", type=float, default=180.0)
    neurodesktop_parser.set_defaults(handler=handle_neurodesktop)

    shell_parser = subparsers.add_parser("shell", help="Shell session commands")
    shell_subparsers = shell_parser.add_subparsers(
        dest="shell_command",
        metavar="{load,unload,list,forward,exec,completion}",
    )

    load_parser = shell_subparsers.add_parser(
        "load", help="Load an image into the current shell session"
    )
    load_parser.add_argument("image")
    load_parser.add_argument("--command", action="append", dest="commands", default=[])
    load_parser.add_argument("--source", default="")
    load_parser.add_argument("--mirror", default=DEFAULT_CVMFS_MIRROR)
    load_parser.add_argument("--repo", default=DEFAULT_CVMFS_REPO)
    load_parser.add_argument("--cache-dir", default="")
    load_parser.add_argument("--prefetch", action="store_true")
    load_parser.add_argument("--prefetch-workers", type=int, default=0)
    load_parser.add_argument("--memory-mb", type=int, default=0)
    load_parser.add_argument("--cpus", type=int, default=0)
    load_parser.add_argument("--dmesg", action="store_true")
    load_parser.add_argument("--force", action="store_true")
    load_parser.add_argument("--vm", default="", help="Use a named cc VM for this load")
    load_parser.set_defaults(handler=handle_load)

    unload_parser = shell_subparsers.add_parser(
        "unload", help="Unload an image from the current shell session"
    )
    unload_parser.add_argument("image")
    unload_parser.set_defaults(handler=handle_unload)

    list_parser = shell_subparsers.add_parser(
        "list", help="List loaded images for the current shell session"
    )
    list_parser.set_defaults(handler=handle_list)

    forward_parser = shell_subparsers.add_parser(
        "forward", help="Forward a host TCP port to the active VM"
    )
    forward_parser.add_argument("spec", help="HOST_PORT:GUEST_PORT")
    forward_parser.add_argument("--host-addr", default="127.0.0.1")
    forward_parser.add_argument("--guest-addr", default="10.42.0.2")
    forward_parser.add_argument("--vm", default="", help="Forward to a named cc VM")
    forward_parser.set_defaults(handler=handle_forward)

    exec_parser = shell_subparsers.add_parser(
        "exec", help="Run a command inside an image through the shared VM"
    )
    exec_parser.add_argument(
        "--user", default="", help="Guest user override, e.g. root"
    )
    exec_parser.add_argument("--vm", default="", help="Run through a named cc VM")
    exec_parser.add_argument("image")
    exec_parser.add_argument("command", nargs=argparse.REMAINDER)
    exec_parser.set_defaults(handler=handle_exec)

    shell_completion_parser = shell_subparsers.add_parser(
        "completion", help="Emit nd shell completion code"
    )
    shell_completion_parser.add_argument(
        "--shell", choices=("bash", "zsh", "powershell", "pwsh"), required=True
    )
    shell_completion_parser.set_defaults(handler=handle_completion)

    bootstrap_parser = add_hidden_subparser(shell_subparsers, "bootstrap")
    bootstrap_parser.set_defaults(handler=handle_bootstrap)

    neurodesktop_server_parser = add_hidden_subparser(
        shell_subparsers, "neurodesktop-server"
    )
    neurodesktop_server_parser.add_argument("--base-url", required=True)
    neurodesktop_server_parser.add_argument("--image", required=True)
    neurodesktop_server_parser.add_argument("--log", required=True)
    neurodesktop_server_parser.set_defaults(handler=handle_neurodesktop_server)

    complete_parser = add_hidden_subparser(shell_subparsers, "complete")
    complete_parser.add_argument("--index", type=int, required=True)
    complete_parser.add_argument("words", nargs=argparse.REMAINDER)
    complete_parser.set_defaults(handler=handle_complete)

    wrapper_parser = add_hidden_subparser(shell_subparsers, "run-wrapper")
    wrapper_parser.add_argument("--session", required=True)
    wrapper_parser.add_argument("--image", required=True)
    wrapper_parser.add_argument("--command", required=True)
    wrapper_parser.add_argument("args", nargs=argparse.REMAINDER)
    wrapper_parser.set_defaults(handler=handle_run_wrapper)

    return parser


def add_hidden_subparser(
    subparsers: argparse._SubParsersAction, name: str
) -> argparse.ArgumentParser:
    parser = subparsers.add_parser(name, help=argparse.SUPPRESS)
    subparsers._choices_actions = [
        action for action in subparsers._choices_actions if action.dest != name
    ]
    return parser


def handle_activate(args: argparse.Namespace) -> int:
    shell_name = normalize_shell_name(args.shell or detect_shell())
    session_id = uuid.uuid4().hex
    root = session_root_for_id(session_id)
    root.mkdir(parents=True, exist_ok=True)
    (root / "bin").mkdir(parents=True, exist_ok=True)
    network = {"enabled": True} if bool(args.with_network) else {}
    vm_id = str(args.vm or "").strip() or None
    write_state(
        SessionState(
            session_id=session_id,
            root=root,
            images={},
            wrappers={},
            network=network,
            vm_id=vm_id,
        )
    )
    print(
        render_activation(
            shell_name, session_id, root, bootstrap=not bool(args.no_bootstrap)
        )
    )
    return 0


def handle_completion(args: argparse.Namespace) -> int:
    print(render_completion(normalize_shell_name(str(args.shell))))
    return 0


def handle_neurodesktop(args: argparse.Namespace) -> int:
    reporter = create_progress_reporter(enabled=True, total_steps=11)
    progress_step = 0

    def update_progress(step: int, message: str) -> None:
        nonlocal progress_step
        progress_step = max(progress_step, int(step))
        reporter.update(step, message)

    def update_prefetch_progress(event: object) -> None:
        reporter.update(5, format_neurodesktop_prefetch_progress(event))

    image = str(args.image or "neurodesktop").strip()
    if not image:
        raise SystemExit("image name is required")
    image_path = str(args.image_path or "").strip()
    host = str(args.host or "127.0.0.1").strip() or "127.0.0.1"
    host_port = int(args.port or 0)
    if host_port == 0:
        host_port = reserve_tcp_port(host)
    if not (1 <= host_port <= 65535):
        raise SystemExit("host port must be between 1 and 65535")
    guest_port = int(args.guest_port or 8888)
    if not (1 <= guest_port <= 65535):
        raise SystemExit("guest port must be between 1 and 65535")

    client = None
    try:
        update_progress(1, "Starting ccvm daemon")
        daemon = (
            start_daemon_for_cache_dir(Path(str(args.cache_dir)).expanduser())
            if str(args.cache_dir or "").strip()
            else start_container_daemon()
        )
        update_progress(2, f"Connecting to ccvm at {daemon.base_url.rstrip('/')}")
        client = connect(base_url=daemon.base_url)
        log_path = neurodesktop_log_path(Path(daemon.cache_dir))
        reference: Optional[ContainerReference] = None
        image_state = client.get_image(image)
        if image_state is None:
            if image_path:
                update_progress(
                    3,
                    f"Using local Neurodesktop image {Path(image_path).expanduser().name}",
                )
                reference = ContainerReference(
                    name=image,
                    image=image,
                    source=ImageSource(
                        type="simg",
                        path=str(resolve_neurodesktop_image_path(image_path)),
                    ),
                )
            else:
                update_progress(3, f"Resolving Neurodesktop image {image}")
                reference = resolve_container_reference(
                    client,
                    image,
                    mirror=DEFAULT_CVMFS_MIRROR,
                    repo=DEFAULT_CVMFS_REPO,
                )
            update_progress(4, f"Importing {Path(reference.path).name}")
            import_request = ImportImageRequest(
                source=reference.source, cache_dir=reference.cache_dir
            )
            import_events = None
            if hasattr(client, "import_image_stream"):
                import_events = client.import_image_stream(image, import_request)
            _report_image_import(
                reporter,
                step=4,
                image=image,
                events=import_events,
                request=lambda: client.import_image(image, import_request),
            )
        else:
            update_progress(4, f"Image {image} is already cached")
            reference = neurodesktop_reference_from_cached_image(image, image_state)
        if bool(args.no_prefetch):
            update_progress(5, "Skipping Neurodesktop CVMFS preload")
        else:
            update_progress(5, "Filling Neurodesktop CVMFS cache")
            if not preload_neurodesktop_cvmfs(
                client,
                image=image,
                reference=reference,
                workers=int(args.prefetch_workers or 0) or None,
                on_event=update_prefetch_progress,
            ):
                update_progress(5, "Skipping Neurodesktop CVMFS preload")
        update_progress(6, "Preparing required file 1/2: Linux kernel")
        _report_required_download(
            reporter,
            step=6,
            index=1,
            total=2,
            fallback_label="kernel",
            stream_method=(lambda: client.download_kernel_stream())
            if hasattr(client, "download_kernel_stream")
            else None,
            request=lambda: client.download_kernel(),
        )
        update_progress(7, "Preparing required file 2/2: emulator")
        _report_required_download(
            reporter,
            step=7,
            index=2,
            total=2,
            fallback_label="emulator",
            stream_method=(lambda: client.prepare_image_emulator_stream(image))
            if hasattr(client, "prepare_image_emulator_stream")
            else None,
            request=lambda: client.prepare_image_emulator(image),
        )
        update_progress(8, f"Preparing image metadata for {image}")
        client.prepare_image_metadata(image)
        network = NetworkConfig(
            enabled=True,
            allow_internet=not bool(args.no_internet),
            port_forwards=(
                PortForward(
                    host_port=host_port,
                    guest_port=guest_port,
                    host_addr=host,
                    guest_addr="10.42.0.2",
                ),
            ),
        )
        update_progress(9, f"Booting {image}")
        _report_neurodesktop_boot(
            reporter,
            client,
            image,
            network=network,
            memory_mb=int(args.memory_mb or 0) or None,
            cpus=int(args.cpus or 0) or None,
            timeout=max(float(args.startup_timeout or 180.0), 1.0),
        )
        update_progress(10, f"Forwarding JupyterLab to {host}:{host_port}")
        apply_port_forwards(client, network)
    finally:
        if client is not None:
            client.close()

    update_progress(11, "Starting Neurodesktop JupyterLab")
    reporter.close()
    proc = start_neurodesktop_jupyter_process(
        daemon.base_url,
        image=image,
        log_path=log_path,
    )
    url = f"http://{host}:{host_port}/lab"
    status_url = f"http://{host}:{host_port}/api/status"
    if not wait_for_jupyter_with_log(
        status_url,
        log_path=log_path,
        timeout_seconds=float(args.startup_timeout or 180.0),
        process=proc,
        activity_snapshot=lambda: watchdog_cvmfs_activity_snapshot(
            daemon.base_url
        ),
    ):
        reporter.close(f"Failed to start Neurodesktop JupyterLab; log={log_path}")
        raise SystemExit(
            f"neurodesktop JupyterLab did not become ready at {url}; "
            f"launcher pid={proc.pid}, log={log_path}"
        )
    reporter.close("Neurodesktop JupyterLab is ready")
    desktop_url = f"http://{host}:{host_port}/neurodesktop/index.html"
    print(url)
    print(f"neurodesktop: {desktop_url}")
    print(f"ccvm: {daemon.base_url}")
    print(f"jupyter pid: {proc.pid}")
    print(f"log: {log_path}")
    print("Press Ctrl-C to stop Neurodesktop.")
    return attach_neurodesktop_session(
        daemon.base_url,
        process=proc,
        log_path=log_path,
    )


def _report_neurodesktop_boot(
    reporter: ProgressReporter,
    client: object,
    image: str,
    *,
    network: NetworkConfig,
    memory_mb: Optional[int],
    cpus: Optional[int],
    timeout: float,
) -> object:
    status_method = getattr(client, "instance_status", None)
    if callable(status_method):
        state = status_method()
        if (
            getattr(state, "status", None) == "running"
            and getattr(state, "image", None) == image
        ):
            reporter.update(8, f"VM for {image} is already running")
            return state
        if getattr(state, "status", None) == "running":
            running_image = getattr(state, "image", None) or "blank VM"
            reporter.update(8, f"Restarting VM currently running {running_image}")
            shutdown = getattr(client, "shutdown_instance", None)
            if callable(shutdown):
                shutdown()

    create_stream = getattr(client, "create_instance_stream", None)
    if callable(create_stream):
        last_error: Optional[str] = None
        last_status = "waiting for guest ready"
        for event in create_stream(
            image,
            network=network,
            memory_mb=memory_mb,
            cpus=cpus,
            timeout=timeout,
            dmesg=False,
        ):
            kind = str(event.get("kind", ""))
            if kind == "status":
                message = str(event.get("message", "")).strip()
                if message:
                    last_status = message
                    reporter.update(8, f"Booting {image}: {message}")
                continue
            if kind == "ready":
                reporter.update(8, f"Booted {image}")
                return event
            if kind == "error":
                last_error = str(event.get("error", "")).strip() or "boot failed"
                break
        if last_error:
            raise RuntimeError(last_error)
        raise RuntimeError(f"boot stream for {image} ended while {last_status}")

    ensure_instance = getattr(client, "ensure_instance")
    started = time.monotonic()
    try:
        return ensure_instance(
            image,
            network=network,
            memory_mb=memory_mb,
            cpus=cpus,
            timeout=timeout,
        )
    finally:
        elapsed = _format_duration(time.monotonic() - started)
        reporter.update(8, f"Booted {image} in {elapsed}")


def resolve_neurodesktop_image_path(raw: str) -> Path:
    if not raw.strip():
        raise SystemExit("neurodesktop image path is required")
    candidates = [Path(raw).expanduser()]
    for candidate in candidates:
        if candidate.exists():
            return candidate
    rendered = ", ".join(str(candidate) for candidate in candidates)
    raise SystemExit(f"neurodesktop image not found; checked {rendered}")


def preload_neurodesktop_cvmfs(
    client: object,
    *,
    image: str,
    reference: Optional[ContainerReference],
    workers: Optional[int] = None,
    on_event: Optional[Callable[[object], None]] = None,
) -> bool:
    if reference is None or reference.source.type.lower() != "cvmfs":
        return False

    request = ImportImageRequest(
        source=reference.source,
        cache_dir=reference.cache_dir,
        prefetch=True,
        prefetch_workers=workers,
    )
    if hasattr(client, "import_image_stream"):
        for event in client.import_image_stream(image, request):
            if on_event is not None:
                on_event(event)
    else:
        client.import_image(image, request)
    return True


def neurodesktop_reference_from_cached_image(
    image: str, image_state: object
) -> Optional[ContainerReference]:
    if str(getattr(image_state, "source_kind", "") or "").lower() != "cvmfs":
        return None
    source = str(getattr(image_state, "source", "") or "").strip()
    if not source:
        return None
    image_source = cvmfs_image_source_from_raw_source(source)
    if image_source is None:
        return None
    return ContainerReference(name=image, image=image, source=image_source)


def format_neurodesktop_prefetch_progress(event: object) -> str:
    status = str(getattr(event, "status", "") or "")
    files_done = getattr(event, "files_downloaded", None)
    files_total = getattr(event, "files_total", None)
    bytes_done = getattr(event, "bytes_downloaded", None)
    bytes_total = getattr(event, "bytes_total", None)
    rate = getattr(event, "rate_bytes_per_second", None)
    eta = getattr(event, "eta_seconds", None)

    parts = ["Filling Neurodesktop CVMFS cache"]
    if isinstance(files_done, int) and isinstance(files_total, int) and files_total > 0:
        parts.append(f"{files_done}/{files_total} files")
    elif isinstance(files_total, int) and files_total > 0:
        parts.append(f"{files_total} files")
    if isinstance(bytes_done, int) and bytes_done >= 0:
        if isinstance(bytes_total, int) and bytes_total > 0:
            parts.append(
                f"{_format_byte_size(float(bytes_done))}/{_format_byte_size(float(bytes_total))}"
            )
        else:
            parts.append(_format_byte_size(float(bytes_done)))
    elif isinstance(bytes_total, int) and bytes_total > 0:
        parts.append(_format_byte_size(float(bytes_total)))
    if isinstance(rate, (int, float)) and rate > 0:
        parts.append(f"{_format_byte_size(float(rate))}/s")
    if isinstance(eta, (int, float)) and eta > 0 and status != "downloaded":
        parts.append(f"ETA {_format_duration(float(eta))}")
    if status == "downloaded":
        parts.append("complete")
    return " | ".join(parts)


def cvmfs_image_source_from_raw_source(raw: str) -> Optional[ImageSource]:
    value = raw.strip()
    lower = value.lower()
    if lower.startswith("/cvmfs/"):
        return cvmfs_image_source_from_cvmfs_path(value)
    if lower.startswith(("http://", "https://")) and "/cvmfs/" in lower:
        parsed = urllib.parse.urlparse(value)
        marker = "/cvmfs/"
        index = parsed.path.lower().find(marker)
        return cvmfs_image_source_from_cvmfs_path(parsed.path[index:])
    if lower.startswith("http+cvmfs://"):
        parsed = urllib.parse.urlparse("https://" + value[len("http+cvmfs://") :])
        query = urllib.parse.parse_qs(parsed.query)
        query_path = (query.get("path") or [""])[0]
        if query_path:
            cvmfs_path = "/".join((parsed.path.rstrip("/"), query_path.lstrip("/")))
        else:
            cvmfs_path = parsed.path
        return cvmfs_image_source_from_cvmfs_path(cvmfs_path)
    if lower.startswith("cvmfs://"):
        parsed = urllib.parse.urlparse(value)
        query = urllib.parse.parse_qs(parsed.query)
        path_value = (query.get("path") or [""])[0]
        if not parsed.netloc or not path_value:
            return None
        return ImageSource(
            type="cvmfs",
            mirror=DEFAULT_CVMFS_MIRROR,
            repo=parsed.netloc,
            path="/" + path_value.lstrip("/"),
        )
    return None


def cvmfs_image_source_from_cvmfs_path(path_value: str) -> Optional[ImageSource]:
    parts = path_value.strip().split("/", 3)
    if len(parts) < 4 or parts[1] != "cvmfs":
        return None
    repo = parts[2].strip()
    path = "/" + parts[3].lstrip("/")
    if not repo:
        return None
    return ImageSource(
        type="cvmfs",
        mirror=DEFAULT_CVMFS_MIRROR,
        repo=repo,
        path=path,
    )


def reserve_tcp_port(host: str) -> int:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
        sock.bind((host, 0))
        return int(sock.getsockname()[1])


def neurodesktop_log_path(cache_dir: Path) -> Path:
    log_dir = cache_dir / "neurodesktop"
    log_dir.mkdir(parents=True, exist_ok=True)
    return log_dir / "jupyter.log"


def start_neurodesktop_jupyter_process(
    base_url: str, *, image: str, log_path: Path
) -> subprocess.Popen[bytes]:
    log_path.parent.mkdir(parents=True, exist_ok=True)
    log_path.write_bytes(b"")
    log_file = log_path.open("ab")
    cmd = [
        sys.executable,
        "-c",
        "from pyneurodesk.shell import main; raise SystemExit(main())",
        "shell",
        "neurodesktop-server",
        "--base-url",
        base_url,
        "--image",
        image,
        "--log",
        str(log_path),
    ]
    try:
        return subprocess.Popen(
            cmd,
            stdin=subprocess.DEVNULL,
            stdout=log_file,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )
    finally:
        log_file.close()


def wait_for_jupyter(status_url: str, *, timeout_seconds: float) -> bool:
    deadline = time.monotonic() + timeout_seconds
    with httpx.Client(timeout=2.0) as client:
        while time.monotonic() < deadline:
            try:
                response = client.get(status_url)
                if response.status_code == 200:
                    return True
            except httpx.HTTPError:
                pass
            time.sleep(0.5)
    return False


def wait_for_jupyter_with_log(
    status_url: str,
    *,
    log_path: Path,
    timeout_seconds: float,
    stream: Optional[object] = None,
    process: Optional[subprocess.Popen[bytes]] = None,
    activity_snapshot: Optional[Callable[[], object]] = None,
    poll_interval: float = 0.5,
) -> bool:
    output = stream if stream is not None else sys.stdout
    idle_timeout = max(float(timeout_seconds), 0.1)
    deadline = time.monotonic() + idle_timeout
    offset = 0
    last_activity_snapshot = (
        activity_snapshot() if activity_snapshot is not None else None
    )
    with httpx.Client(timeout=2.0) as client:
        while time.monotonic() < deadline:
            previous_offset = offset
            offset = emit_log_since(log_path, offset, stream=output)
            if offset != previous_offset:
                deadline = time.monotonic() + idle_timeout
            current_activity_snapshot = (
                activity_snapshot() if activity_snapshot is not None else None
            )
            if current_activity_snapshot != last_activity_snapshot:
                last_activity_snapshot = current_activity_snapshot
                deadline = time.monotonic() + idle_timeout
            try:
                response = client.get(status_url)
                if response.status_code == 200:
                    emit_log_since(log_path, offset, stream=output)
                    return True
            except httpx.HTTPError:
                pass
            if process is not None and process.poll() is not None:
                emit_log_since(log_path, offset, stream=output)
                return False
            time.sleep(poll_interval)
    emit_log_since(log_path, offset, stream=output)
    return False


def attach_neurodesktop_session(
    base_url: str,
    *,
    process: subprocess.Popen[bytes],
    log_path: Path,
    poll_interval: float = 1.0,
    stream: Optional[object] = None,
) -> int:
    output = stream if stream is not None else sys.stdout
    try:
        offset = log_path.stat().st_size
    except OSError:
        offset = 0

    try:
        while True:
            offset = emit_log_since(log_path, offset, stream=output)
            exit_code = process.poll()
            if exit_code is not None:
                emit_log_since(log_path, offset, stream=output)
                return int(exit_code or 0)
            time.sleep(max(float(poll_interval), 0.1))
    except KeyboardInterrupt:
        print("\nStopping Neurodesktop...", file=output, flush=True)
        _shutdown_daemon_server(base_url)
        try:
            process.wait(timeout=10.0)
        except (subprocess.TimeoutExpired, OSError):
            process.terminate()
        return 130


def watchdog_cvmfs_activity_snapshot(base_url: str) -> tuple[int, int]:
    try:
        with httpx.Client(base_url=base_url, timeout=2.0) as client:
            response = client.get("/watchdog/activity")
            response.raise_for_status()
            payload = response.json()
    except (httpx.HTTPError, ValueError, TypeError):
        return (0, 0)
    cvmfs = payload.get("cvmfs") if isinstance(payload, dict) else None
    if not isinstance(cvmfs, dict):
        return (0, 0)
    return (int(cvmfs.get("events") or 0), int(cvmfs.get("bytes") or 0))


def emit_log_since(
    log_path: Path, offset: int, *, stream: Optional[object] = None
) -> int:
    output = stream if stream is not None else sys.stdout
    try:
        with log_path.open("rb") as log:
            log.seek(offset)
            data = log.read()
            next_offset = log.tell()
    except FileNotFoundError:
        return offset
    if data:
        text = filter_internal_exec_log_events(data.decode("utf-8", errors="replace"))
        if text:
            print(text, end="", file=output, flush=True)
    return next_offset


def filter_internal_exec_log_events(text: str) -> str:
    filtered: list[str] = []
    for line in text.splitlines(keepends=True):
        try:
            payload = json.loads(line)
        except ValueError:
            filtered.append(line)
            continue
        if isinstance(payload, dict) and exec_event_is_successful_exit(payload):
            continue
        filtered.append(line)
    return "".join(filtered)


def handle_load(args: argparse.Namespace) -> int:
    state = require_session_state()
    image = str(args.image).strip()
    if not image:
        raise SystemExit("image name is required")

    reference = reference_from_load_args(args)
    memory_mb = int(args.memory_mb or 0) or None
    cpus = int(args.cpus or 0) or None
    vm_arg = str(args.vm or "").strip()
    vm_id = vm_arg or state.vm_id
    if vm_arg:
        state.vm_id = vm_arg
    handle = load_shell_container(
        image,
        reference=reference,
        prefetch=bool(args.prefetch),
        prefetch_workers=int(args.prefetch_workers or 0) or None,
        memory_mb=memory_mb,
        cpus=cpus,
        dmesg=bool(args.dmesg),
        network=session_network_config(state),
        vm_id=vm_id,
    )
    try:
        metadata = load_deploy_metadata(handle)
        commands = list(dict.fromkeys([*(args.commands or []), *metadata.commands]))
        if image not in commands:
            commands.insert(0, image)
    finally:
        handle.close()

    image_record = dict(state.images.get(image, {}))
    image_record["commands"] = commands
    image_record["deploy_env"] = list(metadata.deploy_env)
    if memory_mb is not None:
        image_record["memory_mb"] = memory_mb
    else:
        image_record.pop("memory_mb", None)
    if cpus is not None:
        image_record["cpus"] = cpus
    else:
        image_record.pop("cpus", None)
    if reference is not None:
        image_record["reference"] = container_reference_to_payload(reference)
    state.images[image] = image_record

    sync_wrappers(state, preferred_images=(image,), force=bool(args.force))
    write_session_env(state)
    write_state(state)
    print(f"loaded {image} ({len(commands)} commands)")
    return 0


def reference_from_load_args(args: argparse.Namespace) -> Optional[ContainerReference]:
    source = str(args.source or "").strip()
    if not source:
        return None
    mirror = str(args.mirror)
    repo = str(args.repo)
    cache_dir = str(args.cache_dir or "").strip() or None
    mirrors = cvmfs_mirrors_for(mirror)
    if source.startswith("/containers/"):
        image_source = ImageSource(
            type="cvmfs", mirror=mirror, mirrors=mirrors, repo=repo, path=source
        )
    elif source.startswith("cvmfs://"):
        image_source = ImageSource(
            type="cvmfs",
            mirror=mirror,
            mirrors=mirrors,
            repo=repo,
            path=cvmfs_path_from_source(source),
        )
    elif "/cvmfs/" in source:
        image_source = ImageSource(
            type="cvmfs",
            mirror=mirror,
            mirrors=mirrors,
            repo=repo,
            path=cvmfs_path_from_source(source),
        )
    else:
        image_source = ImageSource(type="simg", path=source)
    return ContainerReference(
        name=str(args.image),
        image=str(args.image),
        source=image_source,
        cache_dir=cache_dir,
    )


def load_shell_container(
    image: str,
    *,
    reference: Optional[ContainerReference],
    prefetch: bool,
    prefetch_workers: Optional[int],
    memory_mb: Optional[int],
    cpus: Optional[int],
    dmesg: bool = False,
    network: Optional[NetworkConfig] = None,
    vm_id: Optional[str] = None,
):
    if reference is None:
        if network is not None and network.enabled:
            return container(image, progress=False, with_network=True)
        return container(image, progress=False)

    active_client = connect()
    active_client.import_image(
        reference.image,
        ImportImageRequest(
            source=reference.source,
            cache_dir=reference.cache_dir,
            prefetch=prefetch,
            prefetch_workers=prefetch_workers,
        ),
    )
    if network is not None:
        ensure_instance(
            active_client,
            reference.image,
            memory_mb=memory_mb,
            cpus=cpus,
            dmesg=dmesg,
            network=network,
            vm_id=vm_id,
        )
    else:
        ensure_instance(
            active_client,
            reference.image,
            memory_mb=memory_mb,
            cpus=cpus,
            dmesg=dmesg,
            vm_id=vm_id,
        )
    apply_port_forwards(active_client, network, vm_id=vm_id)
    return container_handle_for_reference(active_client, reference)


def cvmfs_path_from_source(source: str) -> str:
    source = source.strip()
    if source.startswith("cvmfs://"):
        parsed = source[len("cvmfs://") :]
        slash = parsed.find("/")
        if slash == -1:
            return "/"
        return "/" + parsed[slash + 1 :].lstrip("/")
    path = source.split("?", 1)[0]
    if "/cvmfs/" in path:
        tail = path.split("/cvmfs/", 1)[1]
        slash = tail.find("/")
        if slash == -1:
            return "/"
        return "/" + tail[slash + 1 :].lstrip("/")
    return source


def handle_unload(args: argparse.Namespace) -> int:
    state = require_session_state()
    image = str(args.image).strip()
    if not image:
        raise SystemExit("image name is required")
    if image not in state.images:
        raise SystemExit(f"image {image!r} is not loaded")
    del state.images[image]
    sync_wrappers(state)
    write_state(state)
    print(f"unloaded {image}")
    return 0


def handle_list(args: argparse.Namespace) -> int:
    _ = args
    state = require_session_state()
    for image in sorted(state.images):
        commands = state.images[image].get("commands", [])
        if isinstance(commands, list):
            print(f"{image}\t{len(commands)} commands")
        else:
            print(image)
    return 0


def handle_forward(args: argparse.Namespace) -> int:
    state = require_session_state()
    forward = parse_forward_spec(
        str(args.spec), host_addr=str(args.host_addr), guest_addr=str(args.guest_addr)
    )
    vm_id = str(args.vm or "").strip() or state.vm_id
    network = dict(state.network or {})
    network["enabled"] = True
    forwards = list(network.get("port_forwards", []))
    payload = forward.to_payload()
    if payload not in forwards:
        forwards.append(payload)
    network["port_forwards"] = forwards
    state.network = network
    write_state(state)

    client = connect()
    try:
        status = instance_status(client, vm_id=vm_id)
        if status.status == "running":
            add_port_forward(client, forward, vm_id=vm_id)
            print(
                f"forwarded {forward.host_addr or '127.0.0.1'}:{forward.host_port} -> guest:{forward.guest_port}"
            )
        else:
            print(
                f"queued forward {forward.host_addr or '127.0.0.1'}:{forward.host_port} -> guest:{forward.guest_port}"
            )
    finally:
        client.close()
    return 0


def handle_exec(args: argparse.Namespace) -> int:
    image = str(args.image).strip()
    command = normalize_command_args(args.command)
    if not command:
        raise SystemExit("command is required")
    user = str(args.user or "").strip() or None
    vm_id = str(args.vm or "").strip() or None
    kwargs: dict[str, object] = {"user": user}
    if vm_id:
        kwargs["vm_id"] = vm_id
    return run_image_command(image, command[0], command[1:], **kwargs)


def handle_bootstrap(args: argparse.Namespace) -> int:
    _ = args
    start_default_daemon()
    return 0


def handle_neurodesktop_server(args: argparse.Namespace) -> int:
    client = connect(base_url=str(args.base_url))
    log_path = Path(str(args.log)).expanduser()
    log_path.parent.mkdir(parents=True, exist_ok=True)
    proxy_timeout = int(NEURODESKTOP_PROXY_TIMEOUT_SECONDS)
    preflight_command = [
        "bash",
        "-lc",
        "set -e; "
        "mkdir -p "
        "/home/jovyan "
        "/home/jovyan/.local/share/jupyter/runtime "
        "/home/jovyan/.jupyter/labconfig "
        "/home/jovyan/.config "
        "/home/jovyan/.neurodesk/guacamole "
        "/home/jovyan/neurodesktop-storage; "
        "chown -R 1000:100 /home/jovyan; "
        "chmod 700 /home/jovyan/.ssh 2>/dev/null || true; "
        "NEURODESKTOP_PROXY_TIMEOUT_SECONDS="
        f"{proxy_timeout} "
        "python3 - <<'PY'\n"
        "from pathlib import Path\n"
        "path = Path('/etc/jupyter/jupyter_notebook_config.py')\n"
        "try:\n"
        "    text = path.read_text()\n"
        "    updated = text\n"
        "    if 'NEURODESKTOP_PROXY_TIMEOUT_SECONDS' not in text:\n"
        "        updated = updated.replace(\n"
        "            \"'timeout': 60,\",\n"
        "            \"'timeout': int(os.environ.get('NEURODESKTOP_PROXY_TIMEOUT_SECONDS', '300')),"
        "\",\n"
        "            1,\n"
        "        )\n"
        "    if 'XAUTHORITY=/tmp/neurodesktop-Xauthority' not in text:\n"
        "        updated = updated.replace(\n"
        "            \"'NEURODESKTOP_TOMCAT_PORT=\\\"{port}\\\" exec /opt/neurodesktop/guacamole.sh',\",\n"
        "            \"'XAUTHORITY=/tmp/neurodesktop-Xauthority "
        "PYNEURODESK_GUACAMOLE_DISABLE_SFTP=1 "
        "NEURODESKTOP_TOMCAT_PORT=\\\"{port}\\\" exec /opt/neurodesktop/guacamole.sh',\",\n"
        "            1,\n"
        "        )\n"
        "    updated = updated.replace(\n"
        "        \"'path_info': 'neurodesktop',\",\n"
        "        \"'path_info': 'neurodesktop/index.html',\",\n"
        "        1,\n"
        "    )\n"
        "    if updated != text:\n"
        "        path.write_text(updated)\n"
        "    guac = Path('/opt/neurodesktop/guacamole.sh')\n"
        "    script = guac.read_text()\n"
        "    marker = 'PYNEURODESK_GUACAMOLE_DISABLE_SFTP'\n"
        "    if marker not in script:\n"
        "        script = script.replace(\n"
        "            'unset _sftp_nb_user _sftp_ok\\n',\n"
        "            'unset _sftp_nb_user _sftp_ok\\n'\n"
        "            'if [ \"${PYNEURODESK_GUACAMOLE_DISABLE_SFTP:-}\" = \"1\" ]; then\\n'\n"
        "            '    echo \"[INFO] Disabling Guacamole SFTP side-channel for pyneurodesk.\"\\n'\n"
        "            '    update_mapping_param \"vnc\" \"enable-sftp\" \"false\" || true\\n'\n"
        "            '    update_mapping_param \"rdp\" \"enable-sftp\" \"false\" || true\\n'\n"
        "            'fi\\n',\n"
        "            1,\n"
        "        )\n"
        "        guac.write_text(script)\n"
        "except Exception as exc:\n"
        "    print(f'[WARN] Failed to patch Neurodesktop Jupyter proxy config: {exc}')\n"
        "PY",
    ]
    command = [
        "bash",
        "-lc",
        "export HOME=/home/jovyan USER=jovyan LOGNAME=jovyan; "
        f"export NEURODESKTOP_PROXY_TIMEOUT_SECONDS={proxy_timeout}; "
        "export JUPYTER_TOKEN=; export JUPYTER_PASSWORD=; "
        "exec start-neurodesktop-jupyterlab",
    ]
    try:
        with log_path.open("ab") as log:
            preflight_exit_code = 0
            for event in client.run_stream(
                str(args.image),
                preflight_command,
                with_network=True,
                workdir="/",
                user="0:0",
                timeout_seconds=None,
                timeout=httpx.Timeout(connect=10.0, read=None, write=60.0, pool=10.0),
            ):
                write_exec_event_to_log(log, event)
                kind = str(event.get("kind", ""))
                if kind == "exit":
                    preflight_exit_code = int(event.get("exit_code", 0) or 0)
                elif kind == "error":
                    raise RuntimeError(
                        str(event.get("error", "neurodesktop home preflight failed"))
                    )
            if preflight_exit_code:
                raise RuntimeError(
                    f"neurodesktop home preflight failed with exit code {preflight_exit_code}; "
                    f"log={log_path}"
                )
            for event in client.run_stream(
                str(args.image),
                command,
                with_network=True,
                workdir="/home/jovyan",
                user="1000:100",
                timeout_seconds=None,
                timeout=httpx.Timeout(connect=10.0, read=None, write=60.0, pool=10.0),
            ):
                write_exec_event_to_log(log, event)
    finally:
        client.close()
    return 0


def handle_complete(args: argparse.Namespace) -> int:
    words = normalize_command_args(list(args.words))
    for candidate in complete_words(words, index=int(args.index)):
        print(candidate)
    return 0


def handle_run_wrapper(args: argparse.Namespace) -> int:
    _ = args.session
    wrapper_args = normalize_command_args(args.args)
    state = require_session_state()
    image_record = state.images.get(str(args.image), {})
    deploy_env = image_record.get("deploy_env", [])
    if not isinstance(deploy_env, list):
        deploy_env = []
    return run_image_command(
        str(args.image),
        str(args.command),
        wrapper_args,
        deploy_env=deploy_env,
        vm_id=state.vm_id,
    )


def run_image_command(
    image: str,
    command_name: str,
    args: list[str],
    *,
    deploy_env: Optional[list[str]] = None,
    user: Optional[str] = None,
    vm_id: Optional[str] = None,
) -> int:
    if not vm_id:
        try:
            vm_id = require_session_state().vm_id
        except SystemExit:
            vm_id = None
    handle = (
        shell_session_container(image, vm_id=vm_id)
        if vm_id
        else shell_session_container(image)
    )
    try:
        env = list(deploy_env or [])
        if not env:
            metadata = load_deploy_metadata(handle)
            env = list(metadata.deploy_env)
        env = list(runtime_deploy_env_entries(env))
        shares, workdir = implicit_runtime_mounts()
        env.extend(runtime_env_overrides())
        command = shell_command_with_runtime_env([command_name, *args], env)
        timeout_seconds = runtime_exec_timeout_seconds()
        timeout_kwargs: dict[str, float] = {}
        if timeout_seconds is not None:
            timeout_kwargs["timeout_seconds"] = timeout_seconds
        user_kwargs: dict[str, str] = {}
        if user:
            user_kwargs["user"] = user
        if should_stream_exec() and hasattr(handle._client, "run_stream"):
            exit_code = 0
            for event in handle._client.run_stream(
                handle.reference.image,
                command,
                env=env,
                shares=shares,
                workdir=workdir,
                **vm_id_kwargs(vm_id),
                **user_kwargs,
                **timeout_kwargs,
            ):
                kind = str(event.get("kind", ""))
                if kind in {"stdout", "stderr", "output"}:
                    write_exec_stream_event(event)
                elif kind == "exit":
                    exit_code = int(event.get("exit_code", 0) or 0)
                elif kind == "error":
                    raise RuntimeError(
                        str(event.get("error", "streamed command failed"))
                    )
            return exit_code
        result = handle._client.run(
            handle.reference.image,
            command,
            env=env,
            shares=shares,
            workdir=workdir,
            **vm_id_kwargs(vm_id),
            **user_kwargs,
            **timeout_kwargs,
        )
    finally:
        handle.close()
    if result.output:
        sys.stdout.write(result.output)
        sys.stdout.flush()
    return int(result.exit_code)


def should_stream_exec() -> bool:
    return os.environ.get("PYNEURODESK_EXEC_STREAM", "").strip().lower() in {
        "1",
        "true",
        "yes",
        "on",
    }


def runtime_exec_timeout_seconds() -> Optional[float]:
    raw = os.environ.get("PYNEURODESK_EXEC_TIMEOUT_SECONDS", "").strip()
    if not raw:
        return None
    try:
        value = float(raw)
    except ValueError:
        return None
    if value <= 0:
        return None
    return value


def shell_command_with_runtime_env(command: list[str], env: list[str]) -> list[str]:
    if len(command) < 3:
        return command
    shell_name = Path(command[0]).name
    if shell_name not in {"bash", "sh"}:
        return command
    command_index = shell_command_string_index(command)
    if command_index is None:
        return command
    exports = runtime_shell_exports(env)
    if not exports:
        return command
    updated = list(command)
    updated[command_index] = "\n".join([*exports, updated[command_index]])
    return updated


def shell_command_string_index(command: list[str]) -> Optional[int]:
    for index, arg in enumerate(command[1:], start=1):
        if not arg.startswith("-"):
            continue
        if "c" not in arg:
            continue
        next_index = index + 1
        if next_index < len(command):
            return next_index
        return None
    return None


def runtime_shell_exports(env: list[str]) -> list[str]:
    exports: list[str] = []
    for entry in env:
        if "=" not in entry:
            continue
        key, value = entry.split("=", 1)
        if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key):
            continue
        exports.append(f"export {key}={shlex.quote(value)}")
    return exports


def write_exec_stream_event(event: dict[str, object]) -> None:
    kind = str(event.get("kind", ""))
    stream = str(event.get("stream", "")) or kind
    target = sys.stderr if stream == "stderr" else sys.stdout
    data = exec_event_data(event)
    if data is not None:
        buffer = getattr(target, "buffer", None)
        if buffer is not None:
            buffer.write(data)
            buffer.flush()
        else:
            target.write(data.decode(errors="replace"))
            target.flush()
        return
    target.write(str(event.get("output", "")))
    target.flush()


def write_exec_event_to_log(log: BinaryIO, event: dict[str, object]) -> None:
    if exec_event_is_successful_exit(event):
        return
    data = exec_event_data(event)
    if data is not None:
        log.write(data)
    else:
        output = event.get("output")
        if output is not None:
            log.write(str(output).encode())
        else:
            log.write((json.dumps(event, sort_keys=True) + "\n").encode())
    log.flush()


def exec_event_is_successful_exit(event: dict[str, object]) -> bool:
    if str(event.get("kind", "")) != "exit":
        return False
    return int(event.get("exit_code", 0) or 0) == 0


def exec_event_data(event: dict[str, object]) -> Optional[bytes]:
    data = event.get("data")
    if data in (None, ""):
        return None
    if isinstance(data, bytes):
        return data
    if isinstance(data, bytearray):
        return bytes(data)
    if isinstance(data, str):
        try:
            return base64.b64decode(data, validate=True)
        except (binascii.Error, ValueError):
            return data.encode()
    if isinstance(data, list):
        try:
            return bytes(int(value) for value in data)
        except (TypeError, ValueError):
            return None
    return None


def shell_session_container(image: str, *, vm_id: Optional[str] = None):
    reference = session_container_reference(image)
    if reference is None:
        return container(image, progress=False)

    state_vm_id = vm_id
    if not state_vm_id:
        try:
            state_vm_id = require_session_state().vm_id
        except SystemExit:
            state_vm_id = None
    image_record = session_image_record(image)
    memory_mb = int(image_record.get("memory_mb") or 0) or None
    cpus = int(image_record.get("cpus") or 0) or None
    network = session_network_config()
    active_client = connect()
    active_client.ensure_image(reference)
    if network is not None:
        ensure_instance(
            active_client,
            reference.image,
            memory_mb=memory_mb,
            cpus=cpus,
            network=network,
            vm_id=state_vm_id,
        )
    else:
        ensure_instance(
            active_client,
            reference.image,
            memory_mb=memory_mb,
            cpus=cpus,
            vm_id=state_vm_id,
        )
    apply_port_forwards(active_client, network, vm_id=state_vm_id)
    return container_handle_for_reference(active_client, reference)


def session_image_record(image: str) -> dict[str, object]:
    try:
        state = require_session_state()
    except SystemExit:
        return {}
    image_record = state.images.get(image, {})
    if not isinstance(image_record, dict):
        return {}
    return image_record


def session_network_config(
    state: Optional[SessionState] = None,
) -> Optional[NetworkConfig]:
    if state is None:
        try:
            state = require_session_state()
        except SystemExit:
            return None
    payload = state.network or {}
    if not isinstance(payload, dict) or not payload:
        return None
    forwards: list[PortForward] = []
    raw_forwards = payload.get("port_forwards", [])
    if isinstance(raw_forwards, list):
        for item in raw_forwards:
            if isinstance(item, dict):
                forwards.append(port_forward_from_payload(item))
    return NetworkConfig(
        enabled=bool(payload.get("enabled", False) or forwards),
        allow_internet=bool(payload.get("allow_internet", False)),
        host_dns_name=str(payload["host_dns_name"])
        if payload.get("host_dns_name") is not None
        else None,
        port_forwards=tuple(forwards),
    )


def apply_port_forwards(
    client: object, network: Optional[NetworkConfig], *, vm_id: Optional[str] = None
) -> None:
    if network is None:
        return
    add = getattr(client, "add_port_forward", None)
    if add is None:
        return
    for forward in network.port_forwards:
        add_port_forward(client, forward, vm_id=vm_id)


def vm_id_kwargs(vm_id: Optional[str]) -> dict[str, str]:
    value = (vm_id or "").strip()
    if not value:
        return {}
    return {"vm_id": value}


def ensure_instance(client: object, image: str, **kwargs: object) -> object:
    vm_id = kwargs.pop("vm_id", None)
    ensure = getattr(client, "ensure_instance")
    if vm_id:
        try:
            return ensure(image, vm_id=vm_id, **kwargs)
        except TypeError:
            pass
    return ensure(image, **kwargs)


def instance_status(client: object, *, vm_id: Optional[str] = None) -> object:
    status = getattr(client, "instance_status")
    if vm_id:
        try:
            return status(vm_id=vm_id)
        except TypeError:
            pass
    return status()


def add_port_forward(
    client: object, forward: PortForward, *, vm_id: Optional[str] = None
) -> object:
    add = getattr(client, "add_port_forward")
    if vm_id:
        try:
            return add(forward, vm_id=vm_id)
        except TypeError:
            pass
    return add(forward)


def parse_forward_spec(
    spec: str, *, host_addr: str = "127.0.0.1", guest_addr: str = "10.42.0.2"
) -> PortForward:
    text = spec.strip()
    if ":" not in text:
        raise SystemExit("forward must be HOST_PORT:GUEST_PORT")
    host_text, guest_text = text.split(":", 1)
    try:
        host_port = int(host_text)
        guest_port = int(guest_text)
    except ValueError as exc:
        raise SystemExit("forward ports must be integers") from exc
    if not (1 <= host_port <= 65535 and 1 <= guest_port <= 65535):
        raise SystemExit("forward ports must be between 1 and 65535")
    return PortForward(
        host_port=host_port,
        guest_port=guest_port,
        host_addr=host_addr,
        guest_addr=guest_addr,
    )


def port_forward_from_payload(payload: dict[str, object]) -> PortForward:
    return PortForward(
        host_port=int(payload.get("host_port", 0) or 0),
        guest_port=int(payload.get("guest_port", 0) or 0),
        protocol=str(payload.get("protocol", "tcp") or "tcp"),
        host_addr=str(payload["host_addr"])
        if payload.get("host_addr") is not None
        else None,
        guest_addr=str(payload["guest_addr"])
        if payload.get("guest_addr") is not None
        else None,
    )


def container_handle_for_reference(client, reference: ContainerReference):
    from .api import NeurodeskContainer

    return NeurodeskContainer(client, reference)


def session_container_reference(image: str) -> Optional[ContainerReference]:
    try:
        state = require_session_state()
    except SystemExit:
        return None
    image_record = state.images.get(image, {})
    if not isinstance(image_record, dict):
        return None
    payload = image_record.get("reference")
    if not isinstance(payload, dict):
        return None
    return container_reference_from_payload(payload)


def container_reference_to_payload(reference: ContainerReference) -> dict[str, object]:
    payload: dict[str, object] = {
        "name": reference.name,
        "image": reference.image,
        "source": reference.source.to_payload(),
    }
    if reference.cache_dir is not None:
        payload["cache_dir"] = reference.cache_dir
    return payload


def container_reference_from_payload(
    payload: dict[str, object],
) -> Optional[ContainerReference]:
    source_payload = payload.get("source")
    if not isinstance(source_payload, dict):
        return None
    source = ImageSource(
        type=str(source_payload.get("type", "")),
        format=str(source_payload["format"])
        if source_payload.get("format") is not None
        else None,
        mirror=str(source_payload["mirror"])
        if source_payload.get("mirror") is not None
        else None,
        repo=str(source_payload["repo"])
        if source_payload.get("repo") is not None
        else None,
        path=str(source_payload["path"])
        if source_payload.get("path") is not None
        else None,
    )
    if not source.type:
        return None
    name = payload.get("name")
    image = payload.get("image")
    if not isinstance(name, str) or not isinstance(image, str):
        return None
    cache_dir = payload.get("cache_dir")
    return ContainerReference(
        name=name,
        image=image,
        source=source,
        cache_dir=str(cache_dir) if cache_dir is not None else None,
    )


def implicit_cwd_mount() -> tuple[list[ShareMount], str]:
    cwd = Path.cwd().resolve()
    digest = hashlib.sha256(str(cwd).encode("utf-8")).hexdigest()[:16]
    guest_mount = f"{HOST_CWD_MOUNT_ROOT}/{digest}"
    return (
        [
            ShareMount(
                source=str(cwd),
                mount=guest_mount,
                writable=True,
            )
        ],
        guest_mount,
    )


def implicit_runtime_mounts() -> tuple[list[ShareMount], str]:
    shares, workdir = implicit_cwd_mount()
    home_share = implicit_home_mount()
    if home_share is not None:
        shares.append(home_share)
    return shares, workdir


def implicit_home_mount() -> Optional[ShareMount]:
    if os.environ.get("PYNEURODESK_MOUNT_HOME", "").strip().lower() not in {
        "1",
        "true",
        "yes",
        "on",
    }:
        return None
    home = Path(os.path.expanduser("~")).resolve()
    if not home.exists() or not home.is_dir():
        return None
    return ShareMount(
        source=str(home),
        mount="/root",
        writable=True,
    )


def runtime_env_overrides() -> list[str]:
    return [
        "HOME=/tmp",
        "XDG_CACHE_HOME=/tmp",
        "NUMBA_CACHE_DIR=/tmp/numba-cache",
        "APPTAINER_CACHEDIR=/tmp/.apptainer/cache",
        "APPTAINER_CONFIGDIR=/tmp/.apptainer",
        "SINGULARITY_CACHEDIR=/tmp/.apptainer/cache",
        "SINGULARITY_CONFIGDIR=/tmp/.apptainer",
    ]


def normalize_command_args(values: list[str]) -> list[str]:
    if values and values[0] == "--":
        return values[1:]
    return values


def detect_shell() -> str:
    if is_windows_host():
        return "powershell"
    if os.environ.get("PSModulePath") and not os.environ.get("SHELL"):
        return "powershell"
    shell = Path(os.environ.get("SHELL", "")).name
    shell = normalize_shell_name(shell)
    if shell in {"bash", "zsh"}:
        return shell
    return "bash"


def is_windows_host() -> bool:
    return os.name == "nt"


def normalize_shell_name(shell_name: str) -> str:
    shell = Path(str(shell_name or "").strip()).name.lower()
    shell = shell.removesuffix(".exe")
    if shell in {"pwsh", "powershell", "powershell_ise"}:
        return "powershell"
    if shell in {"bash", "zsh"}:
        return shell
    return shell


def render_activation(
    shell_name: str, session_id: str, root: Path, *, bootstrap: bool
) -> str:
    shell_name = normalize_shell_name(shell_name)
    if shell_name == "powershell":
        return render_powershell_activation(session_id, root, bootstrap=bootstrap)
    if shell_name not in {"bash", "zsh"}:
        raise SystemExit(f"unsupported shell for activation: {shell_name}")
    quoted_root = shlex.quote(str(root))
    quoted_bin = shlex.quote(str(root / "bin"))
    quoted_session = shlex.quote(session_id)
    lines = [
        'export _PYNEURODESK_OLD_PATH="${PATH}"',
        f"export {SESSION_ENV}={quoted_session}",
        f"export {SESSION_ROOT_ENV}={quoted_root}",
        f"export {SESSION_BIN_ENV}={quoted_bin}",
        f'export PATH="${{{SESSION_BIN_ENV}}}:$PATH"',
        "_pyneurodesk_source_env() {",
        f'  if [ -f "${{{SESSION_ROOT_ENV}}}/{SESSION_ENV_FILENAME}" ]; then',
        f'    . "${{{SESSION_ROOT_ENV}}}/{SESSION_ENV_FILENAME}"',
        "  fi",
        "}",
        "_pyneurodesk_source_env",
        "nd() {",
        '  if [ "$#" -eq 0 ]; then',
        "    command neurodesk shell --help",
        "  else",
        '    command neurodesk shell "$@"',
        "    local status=$?",
        '    if [ "$1" = "load" ] && [ "$status" -eq 0 ]; then',
        "      _pyneurodesk_source_env",
        "    fi",
        '    return "$status"',
        "  fi",
        "}",
        render_completion(shell_name),
    ]
    if bootstrap:
        lines.extend(
            [
                "command neurodesk shell bootstrap >/dev/null 2>&1 &",
                f"export {BOOTSTRAP_PID_ENV}=$!",
            ]
        )
    lines.extend(
        [
            "neurodesk_deactivate() {",
            '  if [ -n "${_PYNEURODESK_OLD_PATH:-}" ]; then',
            '    export PATH="${_PYNEURODESK_OLD_PATH}"',
            "  fi",
            "  unset _PYNEURODESK_OLD_PATH",
            f"  unset {SESSION_ENV}",
            f"  unset {SESSION_ROOT_ENV}",
            f"  unset {SESSION_BIN_ENV}",
            f"  unset {BOOTSTRAP_PID_ENV}",
            render_completion_cleanup(shell_name),
            "  unset -f nd",
            "  unset -f _pyneurodesk_source_env",
            "  unset -f neurodesk_deactivate",
            "}",
        ]
    )
    return "\n".join(line for line in lines if line)


def render_powershell_activation(
    session_id: str, root: Path, *, bootstrap: bool
) -> str:
    quoted_root = powershell_quote(str(root))
    quoted_bin = powershell_quote(str(root / "bin"))
    quoted_session = powershell_quote(session_id)
    lines = [
        "$env:_PYNEURODESK_OLD_PATH = $env:PATH",
        f"$env:{SESSION_ENV} = {quoted_session}",
        f"$env:{SESSION_ROOT_ENV} = {quoted_root}",
        f"$env:{SESSION_BIN_ENV} = {quoted_bin}",
        f'$env:PATH = "$env:{SESSION_BIN_ENV};$env:PATH"',
        "function global:nd { if ($args.Count -eq 0) { neurodesk shell --help } else { neurodesk shell @args } }",
        render_completion("powershell"),
    ]
    if bootstrap:
        lines.extend(
            [
                '$__pyneurodesk_bootstrap = Start-Process -FilePath "neurodesk" -ArgumentList @("shell", "bootstrap") -WindowStyle Hidden -PassThru',
                f"$env:{BOOTSTRAP_PID_ENV} = [string]$__pyneurodesk_bootstrap.Id",
                "Remove-Variable __pyneurodesk_bootstrap -ErrorAction SilentlyContinue",
            ]
        )
    lines.extend(
        [
            "function global:neurodesk_deactivate { "
            "if ($env:_PYNEURODESK_OLD_PATH) { $env:PATH = $env:_PYNEURODESK_OLD_PATH }; "
            "Remove-Item Env:_PYNEURODESK_OLD_PATH -ErrorAction SilentlyContinue; "
            f"Remove-Item Env:{SESSION_ENV} -ErrorAction SilentlyContinue; "
            f"Remove-Item Env:{SESSION_ROOT_ENV} -ErrorAction SilentlyContinue; "
            f"Remove-Item Env:{SESSION_BIN_ENV} -ErrorAction SilentlyContinue; "
            f"Remove-Item Env:{BOOTSTRAP_PID_ENV} -ErrorAction SilentlyContinue; "
            f"{render_completion_cleanup('powershell')}; "
            "Remove-Item Function:nd -ErrorAction SilentlyContinue; "
            "Remove-Item Function:neurodesk_deactivate -ErrorAction SilentlyContinue "
            "}",
        ]
    )
    return "\n".join(line for line in lines if line)


def render_completion(shell_name: str) -> str:
    shell_name = normalize_shell_name(shell_name)
    if shell_name == "bash":
        return "\n".join(
            [
                "_neurodesk_complete() {",
                "  local IFS=$'\\n'",
                '  COMPREPLY=($(command neurodesk shell complete --index "${COMP_CWORD}" -- "${COMP_WORDS[@]}"))',
                "}",
                "_nd_complete() {",
                "  local IFS=$'\\n'",
                '  COMPREPLY=($(command neurodesk shell complete --index "${COMP_CWORD}" -- nd "${COMP_WORDS[@]:1}"))',
                "}",
                "complete -F _neurodesk_complete neurodesk",
                "complete -F _nd_complete nd",
            ]
        )
    if shell_name == "zsh":
        return "\n".join(
            [
                'if [ -n "${ZSH_VERSION:-}" ] && typeset -f compdef >/dev/null 2>&1; then',
                "  _neurodesk_complete() {",
                "    local -a reply",
                '    reply=(${(@f)$(command neurodesk shell complete --index "$((CURRENT-1))" -- "${words[@]}")})',
                "    compadd -a reply",
                "  }",
                "  _nd_complete() {",
                "    local -a nd_words reply",
                "    nd_words=(nd ${words[@]:1})",
                '    reply=(${(@f)$(command neurodesk shell complete --index "$((CURRENT-1))" -- "${nd_words[@]}")})',
                "    compadd -a reply",
                "  }",
                "  compdef _neurodesk_complete neurodesk",
                "  compdef _nd_complete nd",
                "fi",
            ]
        )
    if shell_name == "powershell":
        return (
            "Register-ArgumentCompleter -CommandName neurodesk,nd -ScriptBlock { "
            "param($commandName, $wordToComplete, $cursorPosition, $commandAst, $fakeBoundParameters); "
            "$words = @(); "
            "foreach ($element in $commandAst.CommandElements) { $words += $element.Extent.Text }; "
            "$index = [Math]::Max(0, $words.Count - 1); "
            "if ($wordToComplete -eq '') { $index = $words.Count }; "
            "neurodesk shell complete --index $index -- @words | ForEach-Object { "
            "[System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_) "
            "} "
            "}"
        )
    raise SystemExit(f"unsupported shell for completion: {shell_name}")


def render_completion_cleanup(shell_name: str) -> str:
    shell_name = normalize_shell_name(shell_name)
    if shell_name == "bash":
        return "  complete -r neurodesk nd 2>/dev/null"
    if shell_name == "zsh":
        return "  unfunction _neurodesk_complete _nd_complete 2>/dev/null"
    if shell_name == "powershell":
        return (
            'Register-ArgumentCompleter -CommandName neurodesk,nd -ScriptBlock { "" }'
        )
    raise SystemExit(f"unsupported shell for completion cleanup: {shell_name}")


def write_session_env(state: SessionState) -> None:
    env = merged_session_env(state)
    lines = ["# Generated by pyneurodesk. Source through `neurodesk activate`."]
    for entry in env:
        if "=" not in entry:
            continue
        key, value = entry.split("=", 1)
        if not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", key):
            continue
        if key == "PATH":
            lines.extend(
                [
                    f'if [ -n "${{{SESSION_BIN_ENV}:-}}" ]; then',
                    f'  export PATH="${{{SESSION_BIN_ENV}}}:{shlex.quote(value)}"',
                    "else",
                    f"  export PATH={shlex.quote(value)}",
                    "fi",
                ]
            )
            continue
        lines.append(f"export {key}={shlex.quote(value)}")
    session_env_path(state.root).write_text("\n".join(lines) + "\n")


def session_env_path(root: Path) -> Path:
    return root / SESSION_ENV_FILENAME


def merged_session_env(state: SessionState) -> tuple[str, ...]:
    entries: list[str] = []
    for image_record in state.images.values():
        deploy_env = image_record.get("deploy_env", [])
        if isinstance(deploy_env, list):
            entries.extend(str(entry) for entry in deploy_env)
    return runtime_deploy_env_entries(merge_shell_env_entries(entries))


def merge_shell_env_entries(entries: list[str]) -> tuple[str, ...]:
    merged: dict[str, str] = {}
    for entry in entries:
        if "=" not in entry:
            continue
        key, value = entry.split("=", 1)
        merged[key] = value
    return tuple(f"{key}={value}" for key, value in merged.items())


def complete_words(words: list[str], *, index: int) -> list[str]:
    normalized = [word for word in words if word != "--"]
    if normalized and normalized[0] == "nd":
        normalized = ["neurodesk", "shell", *normalized[1:]]
    if not normalized:
        return ["activate", "completion", "neurodesktop", "shell"]
    if normalized[0] != "neurodesk":
        normalized = ["neurodesk", *normalized]

    current = normalized[index] if 0 <= index < len(normalized) else ""

    def filter_prefix(candidates: list[str]) -> list[str]:
        seen: set[str] = set()
        filtered: list[str] = []
        for candidate in candidates:
            if current and not candidate.startswith(current):
                continue
            if candidate in seen:
                continue
            seen.add(candidate)
            filtered.append(candidate)
        return filtered

    if index <= 1:
        return filter_prefix(["activate", "completion", "neurodesktop", "shell"])

    top = normalized[1]
    if top == "activate":
        return filter_prefix(
            ["--shell", "--no-bootstrap", "--with-network", "bash", "zsh", "powershell"]
        )
    if top == "completion":
        return filter_prefix(["--shell", "bash", "zsh", "powershell"])
    if top != "shell":
        return []

    if index == 2:
        return filter_prefix(
            ["load", "unload", "list", "forward", "exec", "completion"]
        )

    subcommand = normalized[2]
    if subcommand == "completion":
        return filter_prefix(["--shell", "bash", "zsh", "powershell"])
    if subcommand == "load":
        return filter_prefix(["--command", "--force"])
    if subcommand == "forward":
        return filter_prefix(["--host-addr", "--guest-addr"])
    if subcommand in {"unload", "exec"}:
        return filter_prefix(loaded_images())
    return []


def loaded_images() -> list[str]:
    session_id = os.environ.get(SESSION_ENV, "").strip()
    root_env = os.environ.get(SESSION_ROOT_ENV, "").strip()
    if not session_id or not root_env:
        return []
    try:
        state = read_state(Path(root_env), session_id=session_id)
    except Exception:
        return []
    return sorted(state.images)


def sync_wrappers(
    state: SessionState,
    *,
    preferred_images: tuple[str, ...] = (),
    force: bool = False,
) -> None:
    desired = desired_wrappers(state, preferred_images=preferred_images, force=force)
    for command_name, spec in desired.items():
        write_wrapper(
            wrapper_path(state.bin_dir, command_name),
            image=spec.image,
            command=spec.command,
        )
    for command_name in sorted(set(state.wrappers) - set(desired)):
        remove_wrapper_file(wrapper_path(state.bin_dir, command_name))
    state.wrappers = desired


def desired_wrappers(
    state: SessionState,
    *,
    preferred_images: tuple[str, ...] = (),
    force: bool = False,
) -> dict[str, WrapperSpec]:
    ownership: dict[str, WrapperSpec] = {}
    preferred = {image for image in preferred_images}
    images = [image for image in state.images if image not in preferred]
    images.extend(image for image in preferred_images if image in state.images)
    for image in images:
        image_record = state.images.get(image, {})
        commands = image_record.get("commands", [])
        if not isinstance(commands, list):
            continue
        for command_name in commands:
            if not isinstance(command_name, str) or not is_valid_wrapper_name(
                command_name
            ):
                continue
            existing = ownership.get(command_name)
            desired = WrapperSpec(image=image, command=command_name)
            if existing is None or existing.image == image:
                ownership[command_name] = desired
                continue
            if force and image in preferred:
                ownership[command_name] = desired
                continue
            raise SystemExit(
                f"wrapper conflict for {command_name!r}: already owned by {existing.image}; rerun with --force to override"
            )
    return ownership


def remove_wrapper_file(path: Path) -> None:
    path.unlink(missing_ok=True)


def session_root_for_id(session_id: str) -> Path:
    return default_cache_root() / "pyneurodesk-shell" / session_id


def require_session_state() -> SessionState:
    session_id = os.environ.get(SESSION_ENV, "").strip()
    root_env = os.environ.get(SESSION_ROOT_ENV, "").strip()
    if not session_id or not root_env:
        raise SystemExit(
            "pyneurodesk shell session is not active; run: source <(neurodesk activate)"
        )
    return read_state(Path(root_env), session_id=session_id)


def read_state(root: Path, *, session_id: str) -> SessionState:
    state_path = root / "state.json"
    if not state_path.exists():
        root.mkdir(parents=True, exist_ok=True)
        (root / "bin").mkdir(parents=True, exist_ok=True)
        state = SessionState(session_id=session_id, root=root, images={}, wrappers={})
        write_state(state)
        return state
    payload = json.loads(state_path.read_text())
    images = payload.get("images", {})
    wrappers_payload = payload.get("wrappers", {})
    wrappers: dict[str, WrapperSpec] = {}
    if isinstance(wrappers_payload, dict):
        for name, spec in wrappers_payload.items():
            if not isinstance(spec, dict):
                continue
            image = spec.get("image")
            command = spec.get("command")
            if isinstance(image, str) and isinstance(command, str):
                wrappers[str(name)] = WrapperSpec(image=image, command=command)
    return SessionState(
        session_id=session_id,
        root=root,
        images=images if isinstance(images, dict) else {},
        wrappers=wrappers,
        network=payload.get("network")
        if isinstance(payload.get("network"), dict)
        else {},
        vm_id=str(payload["vm_id"]).strip() if payload.get("vm_id") else None,
    )


def write_state(state: SessionState) -> None:
    state.root.mkdir(parents=True, exist_ok=True)
    state.bin_dir.mkdir(parents=True, exist_ok=True)
    state_path = state.state_path
    with tempfile.NamedTemporaryFile("w", dir=state.root, delete=False) as tmp:
        json.dump(state.to_payload(), tmp, indent=2, sort_keys=True)
        tmp.write("\n")
        tmp_path = Path(tmp.name)
    tmp_path.replace(state_path)


def write_wrapper(path: Path, *, image: str, command: str) -> None:
    if os.name == "nt":
        write_cmd_wrapper(path, image=image, command=command)
        return
    command_path = shlex.quote(resolve_command_name())
    content = "\n".join(
        [
            "#!/bin/sh",
            f"exec {command_path} shell run-wrapper "
            + f'--session "$${SESSION_ENV}" '
            + f"--image {shlex.quote(image)} "
            + f"--command {shlex.quote(command)} "
            + '-- "$@"',
        ]
    ).replace(f"$${SESSION_ENV}", f"${SESSION_ENV}")
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile("w", dir=path.parent, delete=False) as tmp:
        tmp.write(content)
        tmp.write("\n")
        tmp_path = Path(tmp.name)
    current_mode = (
        stat.S_IRUSR
        | stat.S_IWUSR
        | stat.S_IXUSR
        | stat.S_IRGRP
        | stat.S_IXGRP
        | stat.S_IROTH
        | stat.S_IXOTH
    )
    tmp_path.chmod(current_mode)
    tmp_path.replace(path)


def write_cmd_wrapper(path: Path, *, image: str, command: str) -> None:
    command_path = resolve_command_name()
    content = "\r\n".join(
        [
            "@echo off",
            "setlocal",
            f'"{command_path}" shell run-wrapper --session "%{SESSION_ENV}%" '
            + f"--image {cmd_quote(image)} "
            + f"--command {cmd_quote(command)} "
            + "-- %*",
        ]
    )
    path.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.NamedTemporaryFile(
        "w", dir=path.parent, delete=False, newline=""
    ) as tmp:
        tmp.write(content)
        tmp.write("\r\n")
        tmp_path = Path(tmp.name)
    tmp_path.replace(path)


def wrapper_path(bin_dir: Path, command_name: str) -> Path:
    if os.name == "nt":
        return bin_dir / f"{command_name}.cmd"
    return bin_dir / command_name


def powershell_quote(value: str) -> str:
    return "'" + value.replace("'", "''") + "'"


def cmd_quote(value: str) -> str:
    return '"' + value.replace('"', '""') + '"'


def resolve_command_name() -> str:
    found = shutil.which("neurodesk")
    if found:
        return found
    return "neurodesk"


def is_valid_wrapper_name(command: str) -> bool:
    return (
        bool(command)
        and "/" not in command
        and "\\" not in command
        and command not in {".", ".."}
    )
