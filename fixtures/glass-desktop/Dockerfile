FROM debian:bookworm-slim AS test-app-build

RUN apt-get update \
    && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        build-essential \
        libx11-dev \
    && rm -rf /var/lib/apt/lists/*

COPY glass-test-app.c /src/glass-test-app.c
COPY glass-clipboard.c /src/glass-clipboard.c
COPY glass-resize.c /src/glass-resize.c
RUN cc -O2 -Wall -Wextra -o /glass-test-app /src/glass-test-app.c -lX11 \
    && cc -O2 -Wall -Wextra -o /glass-clipboard /src/glass-clipboard.c -lX11 \
    && cc -O2 -Wall -Wextra -o /glass-resize /src/glass-resize.c

FROM debian:bookworm-slim

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        dbus \
        dbus-x11 \
        fonts-dejavu-core \
        libx11-6 \
        systemd \
        systemd-sysv \
        x11-xserver-utils \
        xclip \
        xfce4 \
        xfce4-terminal \
        xserver-xorg-core \
        xserver-xorg-input-evdev \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --create-home --uid 1000 --shell /bin/bash glass \
    && install -d -o glass -g glass -m 0700 /home/glass/.config \
    && : > /etc/machine-id

COPY --from=test-app-build /glass-test-app /usr/local/bin/glass-test-app
COPY --from=test-app-build /glass-clipboard /usr/local/bin/glass-clipboard
COPY --from=test-app-build /glass-resize /usr/local/bin/glass-resize
COPY glass-session /usr/local/sbin/glass-session
COPY glass-desktop.service /etc/systemd/system/glass-desktop.service
COPY glass.target /etc/systemd/system/glass.target
COPY xorg.conf /etc/X11/xorg.conf

RUN chmod 0755 /usr/local/bin/glass-test-app /usr/local/bin/glass-clipboard /usr/local/bin/glass-resize /usr/local/sbin/glass-session \
    && systemctl enable glass-desktop.service \
    && ln -sf /etc/systemd/system/glass.target /etc/systemd/system/default.target \
    && systemctl mask \
        apt-daily-upgrade.timer \
        apt-daily.timer \
        systemd-networkd-wait-online.service

CMD ["/sbin/init"]
