FROM debian:bookworm

ENV DEBIAN_FRONTEND=noninteractive
ENV container=docker

# Install systemd and packages typical of a Debian VM
RUN apt-get update && apt-get install -y \
    systemd \
    systemd-sysv \
    dbus \
    sudo \
    openssh-client \
    curl \
    wget \
    vim \
    nano \
    less \
    procps \
    iproute2 \
    iputils-ping \
    net-tools \
    dnsutils \
    ca-certificates \
    gnupg \
    lsb-release \
    locales \
    bash-completion \
    man-db \
    apt-utils \
    && apt-get clean \
    && rm -rf /var/lib/apt/lists/*

# Generate locales
RUN sed -i '/en_US.UTF-8/s/^# //g' /etc/locale.gen && locale-gen
ENV LANG=en_US.UTF-8
ENV LANGUAGE=en_US:en
ENV LC_ALL=en_US.UTF-8

# Create regular user with sudo access
RUN useradd -m -s /bin/bash -G sudo user \
    && echo "user:user" | chpasswd \
    && echo "user ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/user \
    && chmod 0440 /etc/sudoers.d/user

# Set root password
RUN echo "root:root" | chpasswd

# Configure autologin on tty1
RUN mkdir -p /etc/systemd/system/getty@tty1.service.d \
    && printf '[Service]\nExecStart=\nExecStart=-/sbin/agetty --autologin user --noclear %%I $TERM\nType=idle\n' \
    > /etc/systemd/system/getty@tty1.service.d/autologin.conf

# Set default target to multi-user (standard for servers)
RUN systemctl set-default multi-user.target

# Set hostname
RUN echo "debian" > /etc/hostname

STOPSIGNAL SIGRTMIN+3

VOLUME ["/sys/fs/cgroup", "/tmp", "/run", "/run/lock"]

ENTRYPOINT ["/sbin/init"]