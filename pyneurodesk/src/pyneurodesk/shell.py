from __future__ import annotations

import argparse
import hashlib
import json
import os
import shlex
import shutil
import stat
import sys
import tempfile
import uuid
from dataclasses import dataclass
from pathlib import Path
from typing import Optional

import httpx

from .api import connect, container, default_cache_root, load_deploy_metadata, start_default_daemon
from .models import ContainerReference, ImageSource, ImportImageRequest, ShareMount

SESSION_ENV = "PYNEURODESK_SHELL_SESSION"
SESSION_ROOT_ENV = "PYNEURODESK_SHELL_ROOT"
SESSION_BIN_ENV = "PYNEURODESK_SHELL_BIN"
BOOTSTRAP_PID_ENV = "PYNEURODESK_SHELL_BOOTSTRAP_PID"
STATE_VERSION = 1
HOST_CWD_MOUNT_ROOT = "/.hostcwd"


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
            "images": self.images,
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

    activate_parser = subparsers.add_parser("activate", help="Emit shell activation code")
    activate_parser.add_argument("--shell", choices=("bash", "zsh"), default=None)
    activate_parser.add_argument("--no-bootstrap", action="store_true")
    activate_parser.set_defaults(handler=handle_activate)

    completion_parser = subparsers.add_parser("completion", help="Emit shell completion code")
    completion_parser.add_argument("--shell", choices=("bash", "zsh"), required=True)
    completion_parser.set_defaults(handler=handle_completion)

    shell_parser = subparsers.add_parser("shell", help="Shell session commands")
    shell_subparsers = shell_parser.add_subparsers(dest="shell_command")

    load_parser = shell_subparsers.add_parser("load", help="Load an image into the current shell session")
    load_parser.add_argument("image")
    load_parser.add_argument("--command", action="append", dest="commands", default=[])
    load_parser.add_argument("--source", default="")
    load_parser.add_argument("--mirror", default="https://cvmfs.neurodesk.org")
    load_parser.add_argument("--repo", default="neurodesk.ardc.edu.au")
    load_parser.add_argument("--cache-dir", default="")
    load_parser.add_argument("--prefetch", action="store_true")
    load_parser.add_argument("--prefetch-workers", type=int, default=0)
    load_parser.add_argument("--memory-mb", type=int, default=0)
    load_parser.add_argument("--cpus", type=int, default=0)
    load_parser.add_argument("--force", action="store_true")
    load_parser.set_defaults(handler=handle_load)

    unload_parser = shell_subparsers.add_parser("unload", help="Unload an image from the current shell session")
    unload_parser.add_argument("image")
    unload_parser.set_defaults(handler=handle_unload)

    list_parser = shell_subparsers.add_parser("list", help="List loaded images for the current shell session")
    list_parser.set_defaults(handler=handle_list)

    exec_parser = shell_subparsers.add_parser("exec", help="Run a command inside an image through the shared VM")
    exec_parser.add_argument("image")
    exec_parser.add_argument("command", nargs=argparse.REMAINDER)
    exec_parser.set_defaults(handler=handle_exec)

    shell_completion_parser = shell_subparsers.add_parser("completion", help="Emit nd shell completion code")
    shell_completion_parser.add_argument("--shell", choices=("bash", "zsh"), required=True)
    shell_completion_parser.set_defaults(handler=handle_completion)

    bootstrap_parser = shell_subparsers.add_parser("bootstrap", help=argparse.SUPPRESS)
    bootstrap_parser.set_defaults(handler=handle_bootstrap)

    complete_parser = shell_subparsers.add_parser("complete", help=argparse.SUPPRESS)
    complete_parser.add_argument("--index", type=int, required=True)
    complete_parser.add_argument("words", nargs=argparse.REMAINDER)
    complete_parser.set_defaults(handler=handle_complete)

    wrapper_parser = shell_subparsers.add_parser("run-wrapper", help=argparse.SUPPRESS)
    wrapper_parser.add_argument("--session", required=True)
    wrapper_parser.add_argument("--image", required=True)
    wrapper_parser.add_argument("--command", required=True)
    wrapper_parser.add_argument("args", nargs=argparse.REMAINDER)
    wrapper_parser.set_defaults(handler=handle_run_wrapper)

    return parser


def handle_activate(args: argparse.Namespace) -> int:
    shell_name = args.shell or detect_shell()
    session_id = uuid.uuid4().hex
    root = session_root_for_id(session_id)
    root.mkdir(parents=True, exist_ok=True)
    (root / "bin").mkdir(parents=True, exist_ok=True)
    write_state(SessionState(session_id=session_id, root=root, images={}, wrappers={}))
    print(render_activation(shell_name, session_id, root, bootstrap=not bool(args.no_bootstrap)))
    return 0


def handle_completion(args: argparse.Namespace) -> int:
    print(render_completion(str(args.shell)))
    return 0


def handle_load(args: argparse.Namespace) -> int:
    state = require_session_state()
    image = str(args.image).strip()
    if not image:
        raise SystemExit("image name is required")

    reference = reference_from_load_args(args)
    handle = load_shell_container(
        image,
        reference=reference,
        prefetch=bool(args.prefetch),
        prefetch_workers=int(args.prefetch_workers or 0) or None,
        memory_mb=int(args.memory_mb or 0) or None,
        cpus=int(args.cpus or 0) or None,
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
    if reference is not None:
        image_record["reference"] = container_reference_to_payload(reference)
    state.images[image] = image_record

    sync_wrappers(state, preferred_images=(image,), force=bool(args.force))
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
    if source.startswith("/containers/"):
        image_source = ImageSource(type="cvmfs", mirror=mirror, repo=repo, path=source)
    elif source.startswith("cvmfs://"):
        image_source = ImageSource(type="cvmfs", mirror=mirror, repo=repo, path=cvmfs_path_from_source(source))
    elif "/cvmfs/" in source:
        image_source = ImageSource(type="cvmfs", mirror=mirror, repo=repo, path=cvmfs_path_from_source(source))
    else:
        image_source = ImageSource(type="simg", path=source)
    return ContainerReference(name=str(args.image), image=str(args.image), source=image_source, cache_dir=cache_dir)


def load_shell_container(
    image: str,
    *,
    reference: Optional[ContainerReference],
    prefetch: bool,
    prefetch_workers: Optional[int],
    memory_mb: Optional[int],
    cpus: Optional[int],
):
    if reference is None:
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
    active_client.ensure_instance(reference.image, memory_mb=memory_mb, cpus=cpus)
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


def handle_exec(args: argparse.Namespace) -> int:
    image = str(args.image).strip()
    command = normalize_command_args(args.command)
    if not command:
        raise SystemExit("command is required")
    return run_image_command(image, command[0], command[1:])


def handle_bootstrap(args: argparse.Namespace) -> int:
    _ = args
    daemon = start_default_daemon()
    with connect(base_url=daemon.base_url) as client:
        state = client.instance_status()
        if state.status != "running":
            try:
                client.start_instance()
            except httpx.HTTPStatusError as exc:
                if exc.response.status_code != 404:
                    raise
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
    return run_image_command(str(args.image), str(args.command), wrapper_args, deploy_env=deploy_env)


def run_image_command(image: str, command_name: str, args: list[str], *, deploy_env: Optional[list[str]] = None) -> int:
    handle = shell_session_container(image)
    try:
        env = list(deploy_env or [])
        if not env:
            metadata = load_deploy_metadata(handle)
            env = list(metadata.deploy_env)
        shares, workdir = implicit_runtime_mounts()
        env.extend(runtime_env_overrides())
        if hasattr(handle._client, "run_stream"):
            exit_code = 0
            for event in handle._client.run_stream(
                handle.reference.image,
                [command_name, *args],
                env=env,
                shares=shares,
                workdir=workdir,
            ):
                kind = str(event.get("kind", ""))
                if kind in {"stdout", "stderr", "output"}:
                    sys.stdout.write(str(event.get("output", "")))
                    sys.stdout.flush()
                elif kind == "exit":
                    exit_code = int(event.get("exit_code", 0) or 0)
                elif kind == "error":
                    raise RuntimeError(str(event.get("error", "streamed command failed")))
            return exit_code
        result = handle._client.run(handle.reference.image, [command_name, *args], env=env, shares=shares, workdir=workdir)
    finally:
        handle.close()
    if result.output:
        sys.stdout.write(result.output)
        sys.stdout.flush()
    return int(result.exit_code)


def shell_session_container(image: str):
    reference = session_container_reference(image)
    if reference is None:
        return container(image, progress=False)

    active_client = connect()
    active_client.ensure_image(reference)
    active_client.ensure_instance(reference.image)
    return container_handle_for_reference(active_client, reference)


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


def container_reference_from_payload(payload: dict[str, object]) -> Optional[ContainerReference]:
    source_payload = payload.get("source")
    if not isinstance(source_payload, dict):
        return None
    source = ImageSource(
        type=str(source_payload.get("type", "")),
        format=str(source_payload["format"]) if source_payload.get("format") is not None else None,
        mirror=str(source_payload["mirror"]) if source_payload.get("mirror") is not None else None,
        repo=str(source_payload["repo"]) if source_payload.get("repo") is not None else None,
        path=str(source_payload["path"]) if source_payload.get("path") is not None else None,
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
    home = Path(os.path.expanduser("~")).resolve()
    if not home.exists() or not home.is_dir():
        return None
    return ShareMount(
        source=str(home),
        mount="/root",
        writable=True,
    )


def runtime_env_overrides() -> list[str]:
    if implicit_home_mount() is None:
        return []
    return [
        "HOME=/root",
        "XDG_CACHE_HOME=/root/.cache",
        "APPTAINER_CACHEDIR=/root/.apptainer/cache",
        "APPTAINER_CONFIGDIR=/root/.apptainer",
        "SINGULARITY_CACHEDIR=/root/.apptainer/cache",
        "SINGULARITY_CONFIGDIR=/root/.apptainer",
    ]


def normalize_command_args(values: list[str]) -> list[str]:
    if values and values[0] == "--":
        return values[1:]
    return values


def detect_shell() -> str:
    shell = Path(os.environ.get("SHELL", "")).name
    if shell in {"bash", "zsh"}:
        return shell
    return "bash"


def render_activation(shell_name: str, session_id: str, root: Path, *, bootstrap: bool) -> str:
    quoted_root = shlex.quote(str(root))
    quoted_bin = shlex.quote(str(root / "bin"))
    quoted_session = shlex.quote(session_id)
    lines = [
        'export _PYNEURODESK_OLD_PATH="${PATH}"',
        f"export {SESSION_ENV}={quoted_session}",
        f"export {SESSION_ROOT_ENV}={quoted_root}",
        f"export {SESSION_BIN_ENV}={quoted_bin}",
        f'export PATH="${{{SESSION_BIN_ENV}}}:$PATH"',
        "nd() {",
        '  if [ "$#" -eq 0 ]; then',
        '    command neurodesk shell --help',
        "  else",
        '    command neurodesk shell "$@"',
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
            "  unset -f neurodesk_deactivate",
            "}",
        ]
    )
    return "\n".join(line for line in lines if line)


def render_completion(shell_name: str) -> str:
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
                "if [ -n \"${ZSH_VERSION:-}\" ] && typeset -f compdef >/dev/null 2>&1; then",
                "  _neurodesk_complete() {",
                "    local -a reply",
                "    reply=(${(@f)$(command neurodesk shell complete --index \"$((CURRENT-1))\" -- \"${words[@]}\")})",
                "    compadd -a reply",
                "  }",
                "  _nd_complete() {",
                "    local -a nd_words reply",
                "    nd_words=(nd ${words[@]:1})",
                "    reply=(${(@f)$(command neurodesk shell complete --index \"$((CURRENT-1))\" -- \"${nd_words[@]}\")})",
                "    compadd -a reply",
                "  }",
                "  compdef _neurodesk_complete neurodesk",
                "  compdef _nd_complete nd",
                "fi",
            ]
        )
    raise SystemExit(f"unsupported shell for completion: {shell_name}")


def render_completion_cleanup(shell_name: str) -> str:
    if shell_name == "bash":
        return "  complete -r neurodesk nd 2>/dev/null"
    if shell_name == "zsh":
        return "  unfunction _neurodesk_complete _nd_complete 2>/dev/null"
    raise SystemExit(f"unsupported shell for completion cleanup: {shell_name}")


def complete_words(words: list[str], *, index: int) -> list[str]:
    normalized = [word for word in words if word != "--"]
    if normalized and normalized[0] == "nd":
        normalized = ["neurodesk", "shell", *normalized[1:]]
    if not normalized:
        return ["activate", "completion", "shell"]
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
        return filter_prefix(["activate", "completion", "shell"])

    top = normalized[1]
    if top == "activate":
        return filter_prefix(["--shell", "--no-bootstrap", "bash", "zsh"])
    if top == "completion":
        return filter_prefix(["--shell", "bash", "zsh"])
    if top != "shell":
        return []

    if index == 2:
        return filter_prefix(["load", "unload", "list", "exec", "completion"])

    subcommand = normalized[2]
    if subcommand == "completion":
        return filter_prefix(["--shell", "bash", "zsh"])
    if subcommand == "load":
        return filter_prefix(["--command", "--force"])
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
        write_wrapper(state.bin_dir / command_name, image=spec.image, command=spec.command)
    for command_name in sorted(set(state.wrappers) - set(desired)):
        remove_wrapper_file(state.bin_dir / command_name)
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
            if not isinstance(command_name, str) or not is_valid_wrapper_name(command_name):
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
        raise SystemExit("pyneurodesk shell session is not active; run: source <(neurodesk activate)")
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
    command_path = shlex.quote(resolve_command_name())
    content = "\n".join(
        [
            "#!/bin/sh",
            f"exec {command_path} shell run-wrapper "
            + f"--session \"$${SESSION_ENV}\" "
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
    current_mode = stat.S_IRUSR | stat.S_IWUSR | stat.S_IXUSR | stat.S_IRGRP | stat.S_IXGRP | stat.S_IROTH | stat.S_IXOTH
    tmp_path.chmod(current_mode)
    tmp_path.replace(path)


def resolve_command_name() -> str:
    found = shutil.which("neurodesk")
    if found:
        return found
    return "neurodesk"


def is_valid_wrapper_name(command: str) -> bool:
    return bool(command) and "/" not in command and command not in {".", ".."}
