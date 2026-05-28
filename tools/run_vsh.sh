#!/usr/bin/env bash

set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${ROOT_DIR}/build/vsh"
TARGET_GOOS="$(go env GOOS)"
TARGET_GOARCH="$(go env GOARCH)"
TARGET_SUFFIX=""
if [[ "${TARGET_GOOS}" == "windows" ]]; then
  TARGET_SUFFIX=".exe"
fi

CCVM_OUTPUT="${BUILD_DIR}/ccvm${TARGET_SUFFIX}"
VSH_OUTPUT="${BUILD_DIR}/vsh${TARGET_SUFFIX}"
GUESTINIT_ARM64_EMBED_PATH="${ROOT_DIR}/internal/guestinit/guest-init-linux-arm64"
GUESTINIT_AMD64_EMBED_PATH="${ROOT_DIR}/internal/guestinit/guest-init-linux-amd64"

mkdir -p "${BUILD_DIR}"

(
  cd "${ROOT_DIR}"
  GOOS=linux GOARCH=arm64 go build -o "${BUILD_DIR}/init-linux-arm64" ./internal/cmd/init
  install -m 644 "${BUILD_DIR}/init-linux-arm64" "${GUESTINIT_ARM64_EMBED_PATH}"

  GOOS=linux GOARCH=amd64 go build -o "${BUILD_DIR}/init-linux-amd64" ./internal/cmd/init
  install -m 644 "${BUILD_DIR}/init-linux-amd64" "${GUESTINIT_AMD64_EMBED_PATH}"

  go build -tags embed_guestinit -o "${CCVM_OUTPUT}" ./cmd/ccvm
  go build -o "${VSH_OUTPUT}" ./cmd/vsh
)

if [[ "${TARGET_GOOS}" == "darwin" && "$(uname -s)" == "Darwin" ]]; then
  codesign -f -s - --entitlements "${ROOT_DIR}/tools/entitlements.xml" "${CCVM_OUTPUT}"
fi

exec "${VSH_OUTPUT}" -ccvm "${CCVM_OUTPUT}" "$@"
