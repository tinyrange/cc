#!/usr/bin/env bash

set -euo pipefail
set -x

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${ROOT_DIR}/build"
PYNEURODESK_BIN_DIR="${ROOT_DIR}/pyneurodesk/bin"
GUESTINIT_EMBED_PATH="${ROOT_DIR}/internal/guestinit/guest-init-linux-arm64"
CCVM_OUTPUT="${BUILD_DIR}/ccvm"
PYNEURODESK_CCVM_OUTPUT="${PYNEURODESK_BIN_DIR}/ccvm"

export CGO_ENABLED=0

mkdir -p "${BUILD_DIR}" "${PYNEURODESK_BIN_DIR}"

GOOS=linux go build -o "${BUILD_DIR}/init" ./internal/cmd/init
install -m 644 "${BUILD_DIR}/init" "${GUESTINIT_EMBED_PATH}"
go build -o "${CCVM_OUTPUT}" ./cmd/ccvm
go build -o "${BUILD_DIR}/cc" ./cmd/cc

install -m 755 "${CCVM_OUTPUT}" "${PYNEURODESK_CCVM_OUTPUT}"

if [[ "$(uname -s)" == "Darwin" ]]; then
  codesign -f -s - --entitlements "${ROOT_DIR}/tools/entitlements.xml" "${CCVM_OUTPUT}"
  codesign -f -s - --entitlements "${ROOT_DIR}/tools/entitlements.xml" "${PYNEURODESK_CCVM_OUTPUT}"
fi
