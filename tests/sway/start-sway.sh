#!/bin/sh
set -eu

mkdir -p "$XDG_RUNTIME_DIR"
chmod 700 "$XDG_RUNTIME_DIR"

# libinput's udev backend expects udev properties (ID_INPUT, etc).
# Bring up a minimal udevd so /dev/input devices are tagged correctly.
mkdir -p /run /run/udev
udevd --daemon
udevadm trigger --type=subsystems --action=add
udevadm trigger --type=devices --action=add
udevadm settle

# seatd provides seat/session management without elogind/systemd.
# Running as root is simplest in a containerized VM.
seatd -u root -g root >/tmp/seatd.log 2>&1 &

# Helpful for some virtio-gpu setups (cursor planes can be flaky).
# If you don't need it, remove it.
export WLR_NO_HARDWARE_CURSORS=1

# Start Sway on DRM/libinput.
exec sway --config /etc/sway/config