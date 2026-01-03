#!/bin/bash
set -e

cd /opt/xfstests

# Run tests - pass any arguments to check
if [ $# -eq 0 ]; then
    # Default: run generic quick tests suitable for virtiofs
    exec ./check -g quick
else
    exec ./check "$@"
fi