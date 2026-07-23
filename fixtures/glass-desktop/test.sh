#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd)
cc_bin=${CC_BIN:-cc}
glass_bin=${GLASS_BIN:-glass}
output=${1:-"$root/build/glass-desktop"}
vm_name=${GLASS_VM_NAME:-glass-desktop-test}

cleanup() {
    if [ -n "${probe_pid:-}" ]; then
        kill "$probe_pid" >/dev/null 2>&1 || true
        wait "$probe_pid" >/dev/null 2>&1 || true
    fi
    if [ -n "${clipboard_pid:-}" ]; then
        kill "$clipboard_pid" >/dev/null 2>&1 || true
        wait "$clipboard_pid" >/dev/null 2>&1 || true
    fi
    "$cc_bin" vm stop "$vm_name" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

CC_BIN="$cc_bin" "$root/fixtures/glass-desktop/build.sh" "$output" >/dev/null
start=$("$cc_bin" vm start --vnc --display 800x600 --init systemd --timeout 3m "$vm_name" glass-desktop)
address=$(printf '%s\n' "$start" | jq -er '.display.vnc_address')

"$glass_bin" -timeout 180s wait-pixel "$address" 10 500 152233
"$glass_bin" -timeout 15s resize "$address" 1024 768 >/dev/null
"$glass_bin" -timeout 180s wait-pixel "$address" 10 668 152233

host_clipboard="glass host → guest clipboard
second line"
"$glass_bin" -timeout 15s clipboard-set "$address" "$host_clipboard"
attempt=0
while :; do
    guest_clipboard=$("$cc_bin" vm run "$vm_name" -- env DISPLAY=:0 xclip -selection clipboard -out 2>/dev/null || true)
    if [ "$guest_clipboard" = "$host_clipboard" ]; then
        break
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 100 ]; then
        echo "glass: host clipboard did not reach guest" >&2
        exit 1
    fi
    sleep 0.05
done

guest_clipboard="glass guest → host clipboard
second line"
printf %s "$guest_clipboard" | "$cc_bin" vm run "$vm_name" -- env \
    DISPLAY=:0 xclip -quiet -selection clipboard -in 2>/dev/null &
clipboard_pid=$!
attempt=0
while :; do
    viewer_clipboard=$("$glass_bin" -timeout 15s clipboard-get "$address")
    if [ "$viewer_clipboard" = "$guest_clipboard" ]; then
        break
    fi
    attempt=$((attempt + 1))
    if [ "$attempt" -ge 100 ]; then
        echo "glass: guest clipboard did not reach viewer" >&2
        exit 1
    fi
    sleep 0.05
done
kill "$clipboard_pid" >/dev/null 2>&1 || true
wait "$clipboard_pid" >/dev/null 2>&1 || true
clipboard_pid=

"$cc_bin" vm run "$vm_name" -- env \
    DISPLAY=:0 \
    HOME=/home/glass \
    USER=glass \
    XDG_RUNTIME_DIR=/run/user/1000 \
    /usr/local/bin/glass-test-app 2>/dev/null &
probe_pid=$!

"$glass_bin" -timeout 180s wait-pixel "$address" 100 150 cc2828
"$glass_bin" -timeout 15s capture "$address" "$output/glass-desktop.png"
"$glass_bin" -timeout 15s click "$address" 100 150
"$glass_bin" -timeout 15s type "$address" A
"$glass_bin" -timeout 15s key "$address" enter

events=$("$cc_bin" vm run "$vm_name" -- cat /tmp/glass-events.jsonl)
printf '%s\n' "$events" | jq -se '
    map({kind, value}) == [
        {kind:"button", value:1},
        {kind:"key", value:65505},
        {kind:"key", value:97},
        {kind:"key", value:65293}
    ]
' >/dev/null

kill "$probe_pid" >/dev/null 2>&1 || true
wait "$probe_pid" >/dev/null 2>&1 || true
probe_pid=

printf '%s\n' "$output/glass-desktop.png"
