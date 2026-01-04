FROM ubuntu:24.04

ENV DEBIAN_FRONTEND=noninteractive

# Update and install Xubuntu desktop with essential packages
RUN apt-get update && apt-get install -y \
    xubuntu-desktop \
    dbus-x11 \
    xserver-xorg-video-all \
    mesa-utils \
    libgl1-mesa-dri \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Configure Xorg to use virtio-gpu
RUN mkdir -p /etc/X11/xorg.conf.d && cat <<'EOF' > /etc/X11/xorg.conf.d/10-virtio.conf
Section "Device"
    Identifier "virtio"
    Driver "modesetting"
    Option "AccelMethod" "glamor"
EndSection

Section "Screen"
    Identifier "Screen0"
    Device "virtio"
EndSection
EOF

# Enable LightDM for graphical login
RUN systemctl set-default graphical.target

# Create a default user with auto-login
RUN useradd -m -s /bin/bash -G sudo,video,render user \
    && echo "user:user" | chpasswd

# Configure LightDM autologin
RUN mkdir -p /etc/lightdm/lightdm.conf.d && cat <<'EOF' > /etc/lightdm/lightdm.conf.d/50-autologin.conf
[Seat:*]
autologin-user=user
autologin-user-timeout=0
EOF

# Set locale
RUN locale-gen en_US.UTF-8
ENV LANG=en_US.UTF-8

ENTRYPOINT ["/sbin/init"]