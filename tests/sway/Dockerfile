FROM alpine:3.23

RUN apk add --no-cache \
    sway \
    swaybg \
    seatd \
    libseat \
    foot \
    mesa-dri-gallium \
    ttf-dejavu \
    libinput \
    xwayland

COPY start-sway.sh /usr/local/bin/start-sway
COPY sway-config /etc/sway/config
RUN chmod +x /usr/local/bin/start-sway

ENV XDG_RUNTIME_DIR=/tmp/xdg-runtime
ENV LIBSEAT_BACKEND=seatd

CMD ["/usr/local/bin/start-sway"]