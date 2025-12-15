FROM alpine:latest

# Install build dependencies
RUN apk add --no-cache \
    gcc \
    make \
    musl-dev \
    wget \
    tar \
    bash

# Download and extract the hello archive
WORKDIR /build
RUN wget -q https://ftp.gnu.org/gnu/hello/hello-2.12.tar.gz && \
    tar -xzf hello-2.12.tar.gz && \
    rm hello-2.12.tar.gz

# Create entrypoint script
RUN echo '#!/bin/bash' > /entrypoint.sh && \
    echo 'set -e' >> /entrypoint.sh && \
    echo 'cd /build/hello-2.12' >> /entrypoint.sh && \
    echo './configure' >> /entrypoint.sh && \
    echo 'make' >> /entrypoint.sh && \
    echo 'make install' >> /entrypoint.sh && \
    echo 'exec "$@"' >> /entrypoint.sh && \
    chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
CMD ["hello"]

