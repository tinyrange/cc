#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PACKAGE_DIR="${ROOT_DIR}/pyneurodesk"
DIST_DIR="${PACKAGE_DIR}/dist"
PACKAGE_BIN_DIR="${PACKAGE_DIR}/src/pyneurodesk/bin"
GUESTINIT_ARM64_EMBED_PATH="${ROOT_DIR}/internal/guestinit/guest-init-linux-arm64"
GUESTINIT_AMD64_EMBED_PATH="${ROOT_DIR}/internal/guestinit/guest-init-linux-amd64"
BUILD_CCVM=1
INCLUDE_SDIST=0
DRY_RUN=0
PUBLISH_ARGS=()
TARGETS=()

usage() {
  cat <<'USAGE'
Usage: tools/publish_neurodesk.sh [options] [-- uv-publish-args...]

Build and publish the neurodesk Python package to PyPI.

By default this script:
  - cross-compiles ccvm for all currently supported host platforms
  - uses CGO_ENABLED=0 for static Go binaries
  - builds one platform wheel per supported target
  - verifies every wheel contains both Python packages and bundled ccvm
  - installs each wheel in a temporary venv as a smoke test
  - uploads wheels with uv publish

Supported default targets:
  linux/amd64, linux/arm64, darwin/arm64, windows/amd64

Options:
  --include-sdist       Also build and upload a source-only sdist.
  --skip-build-ccvm     Reuse an existing pyneurodesk/src/pyneurodesk/bin/ccvm binary.
                        This may only be used with exactly one --target.
  --target GOOS/GOARCH  Build one target. Can be repeated.
  --dry-run             Run all checks, then call uv publish --dry-run.
  -h, --help            Show this help.

The sdist intentionally does not contain ccvm. Installing from the sdist will
fail during wheel build unless ccvm is discoverable through PYNEURODESK_CCVM,
CCX3_CCVM, CCVM_BINARY, or PATH.

Authentication is handled by uv publish. For PyPI, set UV_PUBLISH_TOKEN or pass
publish options after --, for example:

  UV_PUBLISH_TOKEN=pypi-... tools/publish_neurodesk.sh
  tools/publish_neurodesk.sh --dry-run -- --publish-url https://test.pypi.org/legacy/
USAGE
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --include-sdist)
      INCLUDE_SDIST=1
      shift
      ;;
    --skip-build-ccvm)
      BUILD_CCVM=0
      shift
      ;;
    --target)
      if [[ $# -lt 2 ]]; then
        echo "--target requires GOOS/GOARCH" >&2
        exit 1
      fi
      TARGETS+=("$2")
      shift 2
      ;;
    --dry-run)
      DRY_RUN=1
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    --)
      shift
      PUBLISH_ARGS+=("$@")
      break
      ;;
    *)
      PUBLISH_ARGS+=("$1")
      shift
      ;;
  esac
done

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "missing required command: $1" >&2
    exit 1
  fi
}

require_command uv
require_command python3
if [[ "${BUILD_CCVM}" -eq 1 ]]; then
  require_command go
fi

if [[ "${#TARGETS[@]}" -eq 0 ]]; then
  TARGETS=("linux/amd64" "linux/arm64" "darwin/arm64" "windows/amd64")
fi

if [[ "${BUILD_CCVM}" -eq 0 && "${#TARGETS[@]}" -ne 1 ]]; then
  echo "--skip-build-ccvm requires exactly one --target" >&2
  exit 1
fi

target_platform_tag() {
  case "$1" in
    linux/amd64) echo "manylinux_2_17_x86_64" ;;
    linux/arm64) echo "manylinux_2_17_aarch64" ;;
    darwin/arm64) echo "macosx_11_0_arm64" ;;
    windows/amd64) echo "win_amd64" ;;
    *)
      echo "unsupported release target: $1" >&2
      exit 1
      ;;
  esac
}

target_suffix() {
  case "$1" in
    windows/*) echo ".exe" ;;
    *) echo "" ;;
  esac
}

target_binary_name() {
  case "$1" in
    windows/*) echo "ccvm.exe" ;;
    *) echo "ccvm" ;;
  esac
}

build_guestinit_payloads() {
  mkdir -p "${ROOT_DIR}/build"
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
    go build -o "${ROOT_DIR}/build/init-linux-arm64" ./internal/cmd/init
  install -m 644 "${ROOT_DIR}/build/init-linux-arm64" "${GUESTINIT_ARM64_EMBED_PATH}"

  CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o "${ROOT_DIR}/build/init-linux-amd64" ./internal/cmd/init
  install -m 644 "${ROOT_DIR}/build/init-linux-amd64" "${GUESTINIT_AMD64_EMBED_PATH}"
}

build_ccvm_for_target() {
  local target="$1"
  local goos="${target%/*}"
  local goarch="${target#*/}"
  local suffix
  suffix="$(target_suffix "${target}")"
  local output="${ROOT_DIR}/build/ccvm-${goos}-${goarch}${suffix}"

  CGO_ENABLED=0 GOOS="${goos}" GOARCH="${goarch}" \
    go build -tags embed_guestinit -o "${output}" ./cmd/ccvm

  rm -rf "${PACKAGE_BIN_DIR}"
  mkdir -p "${PACKAGE_BIN_DIR}"
  install -m 755 "${output}" "${PACKAGE_BIN_DIR}/ccvm${suffix}"

  if [[ "${goos}" == "darwin" && "$(uname -s)" == "Darwin" ]]; then
    codesign -f -s - --entitlements "${ROOT_DIR}/tools/entitlements.xml" "${output}"
    codesign -f -s - --entitlements "${ROOT_DIR}/tools/entitlements.xml" "${PACKAGE_BIN_DIR}/ccvm${suffix}"
  fi
}

assert_reused_ccvm() {
  if [[ ! -x "${PACKAGE_BIN_DIR}/ccvm" && ! -x "${PACKAGE_BIN_DIR}/ccvm.exe" ]]; then
    echo "missing bundled ccvm binary in pyneurodesk/src/pyneurodesk/bin/" >&2
    echo "remove --skip-build-ccvm or provide a prebuilt binary there" >&2
    exit 1
  fi
}

build_wheel_for_target() {
  local target="$1"
  local platform_tag
  platform_tag="$(target_platform_tag "${target}")"

  (
    cd "${PACKAGE_DIR}"
    rm -f dist/neurodesk-*-py3-none-any.whl
    uv build --wheel
    local wheel_path
    wheel_path="$(find "${DIST_DIR}" -maxdepth 1 -name 'neurodesk-*-py3-none-any.whl' -type f | sort | head -n 1)"
    if [[ -z "${wheel_path}" ]]; then
      echo "expected uv to build a py3-none-any wheel before retagging" >&2
      exit 1
    fi
    python3 "${ROOT_DIR}/tools/retag_wheel.py" "${wheel_path}" --platform-tag "${platform_tag}"
  )
}

build_source_only_sdist() {
  rm -rf "${PACKAGE_BIN_DIR}"
  (
    cd "${PACKAGE_DIR}"
    uv build --sdist
  )
}

wheel_matches_host() {
  local wheel_name
  wheel_name="$(basename "$1")"
  case "$(uname -s)/$(uname -m)" in
    Darwin/arm64) [[ "${wheel_name}" == *"macosx_"*"arm64.whl" ]] ;;
    Darwin/x86_64) [[ "${wheel_name}" == *"macosx_"*"x86_64.whl" ]] ;;
    Linux/x86_64) [[ "${wheel_name}" == *"manylinux_"*"x86_64.whl" ]] ;;
    Linux/aarch64|Linux/arm64) [[ "${wheel_name}" == *"manylinux_"*"aarch64.whl" ]] ;;
    *) return 1 ;;
  esac
}

rm -rf "${DIST_DIR}"
mkdir -p "${DIST_DIR}"

if [[ "${BUILD_CCVM}" -eq 1 ]]; then
  build_guestinit_payloads
fi

for target in "${TARGETS[@]}"; do
  target_platform_tag "${target}" >/dev/null
  if [[ "${BUILD_CCVM}" -eq 1 ]]; then
    build_ccvm_for_target "${target}"
  else
    assert_reused_ccvm
  fi
  build_wheel_for_target "${target}"
done

if [[ "${INCLUDE_SDIST}" -eq 1 ]]; then
  build_source_only_sdist
fi

WHEELS=()
while IFS= read -r artifact; do
  WHEELS+=("${artifact}")
done < <(find "${DIST_DIR}" -maxdepth 1 -name 'neurodesk-*.whl' -type f | sort)
if [[ "${#WHEELS[@]}" -ne "${#TARGETS[@]}" ]]; then
  echo "expected ${#TARGETS[@]} neurodesk wheels, found ${#WHEELS[@]}" >&2
  exit 1
fi

ARTIFACTS=("${WHEELS[@]}")
SDISTS=()
if [[ "${INCLUDE_SDIST}" -eq 1 ]]; then
  while IFS= read -r artifact; do
    SDISTS+=("${artifact}")
  done < <(find "${DIST_DIR}" -maxdepth 1 -name 'neurodesk-*.tar.gz' -type f | sort)
  if [[ "${#SDISTS[@]}" -ne 1 ]]; then
    echo "expected exactly one neurodesk sdist, found ${#SDISTS[@]}" >&2
    exit 1
  fi
  ARTIFACTS+=("${SDISTS[0]}")
fi

python3 - "${ARTIFACTS[@]}" <<'PY'
from __future__ import annotations

import sys
import tarfile
from email.parser import Parser
from pathlib import Path
from zipfile import ZipFile


def has_ccvm(name: str) -> bool:
    return name in {
        "pyneurodesk/bin/ccvm",
        "pyneurodesk/bin/ccvm.exe",
    } or name.endswith(("/pyneurodesk/bin/ccvm", "/pyneurodesk/bin/ccvm.exe"))


def has_source_ccvm(name: str) -> bool:
    return name.endswith(
        (
            "src/pyneurodesk/bin/ccvm",
            "src/pyneurodesk/bin/ccvm.exe",
            "/src/pyneurodesk/bin/ccvm",
            "/src/pyneurodesk/bin/ccvm.exe",
        )
    )


def verify_wheel(path: Path) -> None:
    if path.name.endswith("-none-any.whl"):
        raise SystemExit(f"{path}: bundled-binary wheel must not be tagged py3-none-any")
    with ZipFile(path) as zf:
        names = set(zf.namelist())
        metadata_name = next(name for name in names if name.endswith(".dist-info/METADATA"))
        metadata = Parser().parsestr(zf.read(metadata_name).decode("utf-8"))
    if metadata["Name"] != "neurodesk":
        raise SystemExit(f"{path}: expected package name neurodesk, got {metadata['Name']!r}")
    if not any(name.startswith("neurodesk/") for name in names):
        raise SystemExit(f"{path}: missing neurodesk import package")
    if not any(name.startswith("pyneurodesk/") for name in names):
        raise SystemExit(f"{path}: missing pyneurodesk implementation package")
    if not any(has_ccvm(name) for name in names):
        raise SystemExit(f"{path}: missing bundled ccvm binary")


def verify_sdist(path: Path) -> None:
    with tarfile.open(path, "r:gz") as tf:
        names = set(tf.getnames())
    if not any(name.endswith("pyproject.toml") for name in names):
        raise SystemExit(f"{path}: missing pyproject.toml")
    if not any(name.endswith("build_backend/neurodesk_build_backend.py") for name in names):
        raise SystemExit(f"{path}: missing source-install build backend")
    if not any(name.endswith("src/neurodesk/__init__.py") for name in names):
        raise SystemExit(f"{path}: missing neurodesk import package")
    if not any(name.endswith("src/pyneurodesk/__init__.py") for name in names):
        raise SystemExit(f"{path}: missing pyneurodesk implementation package")
    if any(has_source_ccvm(name) for name in names):
        raise SystemExit(f"{path}: sdist must not contain bundled ccvm")


for raw in sys.argv[1:]:
    path = Path(raw)
    if path.suffix == ".whl":
        verify_wheel(path)
    elif path.name.endswith(".tar.gz"):
        verify_sdist(path)
    else:
        raise SystemExit(f"unsupported artifact: {path}")
    print(f"verified {path}")
PY

for target in "${TARGETS[@]}"; do
  platform_tag="$(target_platform_tag "${target}")"
  binary_name="$(target_binary_name "${target}")"
  wheel_path="$(find "${DIST_DIR}" -maxdepth 1 -name "neurodesk-*-py3-none-${platform_tag}.whl" -type f | sort | head -n 1)"
  if [[ -z "${wheel_path}" ]]; then
    echo "missing wheel for ${target} (${platform_tag})" >&2
    exit 1
  fi
  python3 - "${wheel_path}" "${binary_name}" <<'PY'
from pathlib import Path
from zipfile import ZipFile
import sys

wheel = Path(sys.argv[1])
binary = f"pyneurodesk/bin/{sys.argv[2]}"
with ZipFile(wheel) as zf:
    names = set(zf.namelist())
if binary not in names:
    raise SystemExit(f"{wheel}: expected bundled binary {binary}")
print(f"verified {wheel.name} contains {binary}")
PY
done

TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

for wheel in "${WHEELS[@]}"; do
  if ! wheel_matches_host "${wheel}"; then
    echo "skipping install smoke test for non-host wheel: $(basename "${wheel}")"
    continue
  fi
  rm -rf "${TMP_DIR}/venv"
  python3 -m venv "${TMP_DIR}/venv"
  "${TMP_DIR}/venv/bin/python" -m pip install -q "${wheel}"
  "${TMP_DIR}/venv/bin/python" - <<'PY'
import importlib.metadata
import neurodesk
import pyneurodesk

assert importlib.metadata.metadata("neurodesk")["Name"] == "neurodesk"
assert neurodesk.connect is pyneurodesk.connect
PY
  "${TMP_DIR}/venv/bin/neurodesk" --help >/dev/null
done

PUBLISH_COMMAND=(uv publish)
if [[ "${DRY_RUN}" -eq 1 ]]; then
  PUBLISH_COMMAND+=(--dry-run)
fi
if [[ "${#PUBLISH_ARGS[@]}" -gt 0 ]]; then
  PUBLISH_COMMAND+=("${PUBLISH_ARGS[@]}")
fi
PUBLISH_COMMAND+=("${ARTIFACTS[@]}")

(
  cd "${PACKAGE_DIR}"
  "${PUBLISH_COMMAND[@]}"
)
