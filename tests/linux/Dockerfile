FROM alpine:latest

# Install build dependencies
RUN apk add --no-cache \
    gcc \
    make \
    musl-dev \
    wget \
    tar \
    xz \
    bash \
    bison \
    flex \
    perl \
    openssl-dev \
    ncurses-dev

# Download and extract the Linux kernel archive
WORKDIR /build
RUN wget -q https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-6.18.1.tar.xz && \
    tar -xJf linux-6.18.1.tar.xz && \
    rm linux-6.18.1.tar.xz

# Create entrypoint script
RUN echo '#!/bin/bash' > /entrypoint.sh && \
    echo 'set -e' >> /entrypoint.sh && \
    echo 'cd /build/linux-6.18.1' >> /entrypoint.sh && \
    echo 'make tinyconfig' >> /entrypoint.sh && \
    echo 'make' >> /entrypoint.sh && \
    chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
