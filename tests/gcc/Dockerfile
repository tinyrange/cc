FROM alpine:latest

RUN apk add --no-cache \
    gcc \
    make \
    musl-dev \
    wget \
    tar \
    bash

WORKDIR /build
RUN echo 'int main(){return 0;}' > /build/test.c

RUN echo '#!/bin/bash' > /entrypoint.sh && \
    echo 'set -e' >> /entrypoint.sh && \
    echo 'gcc -o /build/test /build/test.c' >> /entrypoint.sh && \
    echo '/build/test' >> /entrypoint.sh && \
    chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
CMD ["/build/test"]