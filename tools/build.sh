#!/usr/bin/env bash

set -ex

export CGO_ENABLED=0

GOOS=linux go build -o build/init ./internal/cmd/init
go build -o build/ccvm ./cmd/ccvm
go build -o build/cc ./cmd/cc