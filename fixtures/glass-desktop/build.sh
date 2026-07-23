#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
output=${1:-"$root/build/glass-desktop"}
cc_bin=${CC_BIN:-cc}
docker_bin=${DOCKER_BIN:-docker}
tag=${GLASS_DESKTOP_TAG:-cc-glass-desktop:bookworm}

mkdir -p "$output"
output=$(CDPATH= cd -- "$output" && pwd)
"$docker_bin" build -t "$tag" "$root/fixtures/glass-desktop"
temporary="$output/glass-desktop.docker.tar"
digest=$("$docker_bin" image inspect --format '{{.Id}}' "$tag")
digest=${digest#sha256:}
archive="$output/glass-desktop-$digest.docker.tar"
if [ ! -e "$archive" ]; then
    "$docker_bin" save -o "$temporary" "$tag"
    mv "$temporary" "$archive"
fi
"$cc_bin" pull glass-desktop "docker-archive:$archive#$tag"

printf '%s\n' "$archive"
