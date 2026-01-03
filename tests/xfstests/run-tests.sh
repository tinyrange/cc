#!/bin/bash
set -e

cd /opt/xfstests

# If the first argument is "shell" then start a shell
if [ "$1" = "shell" ]; then
    shift
    echo "Starting shell with arguments: $@"
    exec bash -c "$*"
fi

# Run tests - pass any arguments to check
if [ $# -eq 0 ]; then
    # Default: run generic quick tests suitable for virtiofs
    exec ./check -g quick
else
    exec ./check "$@"
fi