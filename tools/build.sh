#!/usr/bin/env bash

set -ex

export CGO_ENABLED=0

mkdir -p build

GOOS=linux go build -o build/init ./internal/cmd/init
go build -o build/ccvm ./cmd/ccvm
go build -o build/cc ./cmd/cc

if [[ "$(uname -s)" == "Darwin" ]]; then
  codesign -f -s - --entitlements tools/entitlements.xml build/ccvm
fi
