FROM alpine:latest

# Install packages with continuation
RUN apk add --no-cache \
    gcc \
    make \
    musl-dev

WORKDIR /build

RUN echo "line1" && \
    echo "line2" && \
    echo "line3"

CMD ["sh"]
