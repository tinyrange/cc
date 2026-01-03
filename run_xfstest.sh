#!/bin/bash
set -e

# The debug file should be used with ./tools/build.go -dbg-tool --
DEBUG_FILE="local/xfstests.debug"

./tools/build.go -run -- -debug \
    -debug-file $DEBUG_FILE \
    -add-virtiofs testfs,scratchfs \
    ./build/test-xfstests.tar $@