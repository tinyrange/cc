from __future__ import annotations

import os
import platform
import shutil
import stat
from pathlib import Path
from typing import Any

import uv_build


ROOT = Path(__file__).resolve().parents[1]
PACKAGE_BIN = ROOT / "src" / "pyneurodesk" / "bin"


def build_sdist(*args: Any, **kwargs: Any) -> str:
    return uv_build.build_sdist(*args, **kwargs)


def build_wheel(*args: Any, **kwargs: Any) -> str:
    ensure_bundled_ccvm()
    return uv_build.build_wheel(*args, **kwargs)


def build_editable(*args: Any, **kwargs: Any) -> str:
    ensure_bundled_ccvm()
    return uv_build.build_editable(*args, **kwargs)


def prepare_metadata_for_build_wheel(*args: Any, **kwargs: Any) -> str:
    return uv_build.prepare_metadata_for_build_wheel(*args, **kwargs)


def get_requires_for_build_sdist(*args: Any, **kwargs: Any) -> list[str]:
    return uv_build.get_requires_for_build_sdist(*args, **kwargs)


def get_requires_for_build_wheel(*args: Any, **kwargs: Any) -> list[str]:
    return uv_build.get_requires_for_build_wheel(*args, **kwargs)


def get_requires_for_build_editable(*args: Any, **kwargs: Any) -> list[str]:
    return uv_build.get_requires_for_build_editable(*args, **kwargs)


def ensure_bundled_ccvm() -> None:
    existing = bundled_ccvm_path()
    if existing is not None:
        make_executable(existing)
        return

    source = find_ccvm()
    if source is None:
        raise RuntimeError(
            "neurodesk source installs require a ccvm binary. "
            "Set PYNEURODESK_CCVM, CCX3_CCVM, or CCVM_BINARY to an existing "
            "ccvm binary, or install a platform wheel instead."
        )

    suffix = ".exe" if source.name.endswith(".exe") or os.name == "nt" else ""
    destination = PACKAGE_BIN / f"ccvm{suffix}"
    destination.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy2(source, destination)
    make_executable(destination)


def bundled_ccvm_path() -> Path | None:
    for name in ("ccvm", "ccvm.exe"):
        candidate = PACKAGE_BIN / name
        if candidate.is_file():
            return candidate
    return None


def find_ccvm() -> Path | None:
    for env_name in ("PYNEURODESK_CCVM", "CCX3_CCVM", "CCVM_BINARY"):
        value = os.environ.get(env_name, "").strip()
        if not value:
            continue
        path = Path(value).expanduser()
        if path.is_file():
            return path
        raise RuntimeError(f"{env_name} points to missing ccvm binary: {path}")

    for name in ("ccvm", "ccvm.exe"):
        found = shutil.which(name)
        if found:
            return Path(found)
    repo_binary = repo_build_ccvm_path()
    if repo_binary is not None:
        return repo_binary
    return None


def repo_build_ccvm_path() -> Path | None:
    repo_root = ROOT.parent
    goos = python_goos()
    goarch = python_goarch()
    if goos is None or goarch is None:
        return None
    suffix = ".exe" if goos == "windows" else ""
    candidate = repo_root / "build" / f"ccvm-{goos}-{goarch}{suffix}"
    if candidate.is_file():
        return candidate
    return None


def repo_root() -> Path | None:
    repo_root = ROOT.parent
    if (repo_root / "go.mod").exists() and (repo_root / "cmd" / "ccvm" / "main.go").exists():
        return repo_root
    return None


def python_goos() -> str | None:
    system = platform.system().lower()
    if system == "darwin":
        return "darwin"
    if system == "linux":
        return "linux"
    if system == "windows":
        return "windows"
    return None


def python_goarch() -> str | None:
    machine = platform.machine().lower()
    if machine in {"x86_64", "amd64"}:
        return "amd64"
    if machine in {"arm64", "aarch64"}:
        return "arm64"
    return None


def make_executable(path: Path) -> None:
    if os.name == "nt":
        return
    mode = path.stat().st_mode
    path.chmod(mode | stat.S_IXUSR | stat.S_IXGRP | stat.S_IXOTH)
