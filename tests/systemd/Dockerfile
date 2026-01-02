FROM debian:bookworm

ENV DEBIAN_FRONTEND=noninteractive

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

RUN sed -i '/en_US.UTF-8/s/^# //g' /etc/locale.gen && locale-gen
ENV LANG=en_US.UTF-8
ENV LANGUAGE=en_US:en
ENV LC_ALL=en_US.UTF-8

RUN useradd -m -s /bin/bash -G sudo user \
    && echo "user:user" | chpasswd \
    && echo "user ALL=(ALL) NOPASSWD:ALL" > /etc/sudoers.d/user \
    && chmod 0440 /etc/sudoers.d/user

RUN echo "root:root" | chpasswd

# Create console getty service
RUN printf '[Unit]\n\
Description=Console Getty\n\
After=systemd-user-sessions.service\n\
\n\
[Service]\n\
ExecStartPre=-/bin/sh -c "test -e /dev/console || mknod /dev/console c 5 1"\n\
ExecStart=-/sbin/agetty --noclear --keep-baud console 115200,38400,9600 linux\n\
Type=idle\n\
Restart=always\n\
RestartSec=0\n\
TTYPath=/dev/console\n\
TTYReset=yes\n\
TTYVHangup=yes\n\
StandardInput=tty\n\
StandardOutput=tty\n' > /etc/systemd/system/console-login.service \
    && mkdir -p /etc/systemd/system/multi-user.target.wants \
    && ln -s /etc/systemd/system/console-login.service \
         /etc/systemd/system/multi-user.target.wants/console-login.service

RUN systemctl set-default multi-user.target

RUN echo "debian" > /etc/hostname

STOPSIGNAL SIGRTMIN+3

VOLUME ["/sys/fs/cgroup", "/tmp", "/run", "/run/lock"]

ENTRYPOINT ["/sbin/init"]