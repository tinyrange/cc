#!/bin/bash
set -e

./tools/build.go -run -- -debug -add-virtiofs testfs,scratchfs ./build/test-xfstests.tar $@