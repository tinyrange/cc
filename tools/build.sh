#!/usr/bin/env bash

set -euo pipefail
set -x

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BUILD_DIR="${ROOT_DIR}/build"
PYNEURODESK_BIN_DIR="${ROOT_DIR}/pyneurodesk/src/pyneurodesk/bin"
GUESTINIT_ARM64_EMBED_PATH="${ROOT_DIR}/internal/guestinit/guest-init-linux-arm64"
GUESTINIT_AMD64_EMBED_PATH="${ROOT_DIR}/internal/guestinit/guest-init-linux-amd64"
TARGET_GOOS="${CCX3_TARGET_GOOS:-$(go env GOOS)}"
TARGET_GOARCH="${CCX3_TARGET_GOARCH:-$(go env GOARCH)}"
TARGET_SUFFIX=""
if [[ "${TARGET_GOOS}" == "windows" ]]; then
  TARGET_SUFFIX=".exe"
fi
CCVM_OUTPUT="${BUILD_DIR}/ccvm-${TARGET_GOOS}-${TARGET_GOARCH}${TARGET_SUFFIX}"
PYNEURODESK_CCVM_OUTPUT="${PYNEURODESK_BIN_DIR}/ccvm${TARGET_SUFFIX}"

export CGO_ENABLED=0

mkdir -p "${BUILD_DIR}" "${PYNEURODESK_BIN_DIR}"

GOOS=linux GOARCH=arm64 go build -o "${BUILD_DIR}/init-linux-arm64" ./internal/cmd/init
install -m 644 "${BUILD_DIR}/init-linux-arm64" "${GUESTINIT_ARM64_EMBED_PATH}"

GOOS=linux GOARCH=amd64 go build -o "${BUILD_DIR}/init-linux-amd64" ./internal/cmd/init
install -m 644 "${BUILD_DIR}/init-linux-amd64" "${GUESTINIT_AMD64_EMBED_PATH}"

for bsd in openbsd freebsd netbsd; do
  GOOS="${bsd}" GOARCH="${TARGET_GOARCH}" go build -o "${BUILD_DIR}/guest-init-${bsd}-${TARGET_GOARCH}" "./internal/cmd/${bsd}-init"
  install -m 644 "${BUILD_DIR}/guest-init-${bsd}-${TARGET_GOARCH}" "${ROOT_DIR}/internal/${bsd}/guestinit/guest-init-${bsd}-${TARGET_GOARCH}"
done

GOOS="${TARGET_GOOS}" GOARCH="${TARGET_GOARCH}" go build -o "${CCVM_OUTPUT}" ./cmd/ccvm
GOOS="${TARGET_GOOS}" GOARCH="${TARGET_GOARCH}" go build -o "${BUILD_DIR}/cc-${TARGET_GOOS}-${TARGET_GOARCH}${TARGET_SUFFIX}" ./cmd/cc
GOOS="${TARGET_GOOS}" GOARCH="${TARGET_GOARCH}" go build -o "${BUILD_DIR}/glass-${TARGET_GOOS}-${TARGET_GOARCH}${TARGET_SUFFIX}" ./cmd/glass

install -m 755 "${CCVM_OUTPUT}" "${PYNEURODESK_CCVM_OUTPUT}"

if [[ "${TARGET_GOOS}" == "darwin" && "$(uname -s)" == "Darwin" ]]; then
  codesign -f -s - --entitlements "${ROOT_DIR}/tools/entitlements.xml" "${CCVM_OUTPUT}"
  codesign -f -s - --entitlements "${ROOT_DIR}/tools/entitlements.xml" "${PYNEURODESK_CCVM_OUTPUT}"
fi
