ARG ALPINE_VERSION=3.19
FROM alpine:${ALPINE_VERSION}

ARG APP_USER=appuser
ARG APP_DIR=/app

# Use variables in instructions
WORKDIR ${APP_DIR}
ENV MY_USER=${APP_USER}
ENV MY_HOME=${APP_DIR}

RUN adduser -D myuser
USER myuser

EXPOSE 8080
LABEL version="1.0" \
      app="test"

ENTRYPOINT ["/app/start"]
CMD ["--help"]
