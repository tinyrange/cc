# Ubuntu + Xorg (modesetting on virtio-gpu) + XFCE
FROM ubuntu:24.04

SHELL ["/bin/bash", "-o", "pipefail", "-c"]
ENV DEBIAN_FRONTEND=noninteractive
ENV DISPLAY=:0

# Core desktop + Xorg + input/video drivers + dbus + a tiny init
RUN apt-get update && \
    apt-get install -y --no-install-recommends \
    ca-certificates \
    dbus-x11 \
    sudo \
    tini \
    xfce4 xfce4-goodies \
    xorg \
    xserver-xorg-core \
    xserver-xorg-legacy \
    xserver-xorg-video-modesetting \
    xserver-xorg-input-libinput \
    x11-xserver-utils \
    x11-utils \
    xauth \
    mesa-utils \
    && \
    rm -rf /var/lib/apt/lists/*

# Allow Xorg to run without a display manager/logind inside the container.
RUN printf '%s\n' \
    'allowed_users=anybody' \
    'needs_root_rights=yes' \
    > /etc/X11/Xwrapper.config

# Force the generic KMS "modesetting" driver (works well with virtio-gpu DRM/KMS).
RUN mkdir -p /etc/X11/xorg.conf.d && \
    cat > /etc/X11/xorg.conf.d/10-modesetting.conf <<'EOF'
Section "Device"
  Identifier "GPU0"
  Driver "modesetting"
  Option "PrimaryGPU" "true"
EndSection
EOF

# Create an unprivileged desktop user
RUN useradd -m -s /bin/bash xfce && \
    chown -R xfce:xfce /home/xfce && \
    chmod -R 0755 /home/xfce && \
    echo 'xfce ALL=(ALL) NOPASSWD:ALL' > /etc/sudoers.d/xfce && \
    chmod 0440 /etc/sudoers.d/xfce

# Entrypoint script: start Xorg on :0, then start an XFCE session
RUN cat > /usr/local/bin/start-xfce <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

export DISPLAY="${DISPLAY:-:0}"

mount -t tmpfs -o size=1024M tmpfs /tmp

mkdir -p /tmp/.X11-unix
chmod 1777 /tmp/.X11-unix

# Runtime dir for the session bus / XFCE components
export XDG_RUNTIME_DIR="/tmp/xdg-runtime-xfce"
mkdir -p "${XDG_RUNTIME_DIR}"
chown xfce:xfce "${XDG_RUNTIME_DIR}"
chmod 0700 "${XDG_RUNTIME_DIR}"

# Xorg log directory
mkdir -p /var/log

# Start Xorg (DRM/KMS via modesetting; input via libinput)
Xorg "${DISPLAY}" \
  -nolisten tcp \
  -noreset \
  -logfile /var/log/Xorg.0.log \
  -configdir /etc/X11/xorg.conf.d \
  &
xorg_pid="$!"

cleanup() {
  kill "${xorg_pid}" 2>/dev/null || true
}
trap cleanup EXIT

echo "Waiting for X to come up"

# Wait for X to come up
for _ in {1..200}; do
  if [[ -S "/tmp/.X11-unix/X0" ]] && xdpyinfo -display "${DISPLAY}" >/dev/null 2>&1; then
    break
  fi
  sleep 0.05
done

echo "X is up, running XFCE session"

# Run XFCE session with a proper session D-Bus
exec sudo -u xfce -H bash -lc '
  export DISPLAY="'"${DISPLAY}"'"
  export XDG_RUNTIME_DIR="'"${XDG_RUNTIME_DIR}"'"
  exec dbus-run-session -- startxfce4
'
EOF
RUN chmod +x /usr/local/bin/start-xfce

ENTRYPOINT ["/usr/bin/tini", "--"]
CMD ["/usr/local/bin/start-xfce"]