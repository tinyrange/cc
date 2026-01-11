FROM debian:bookworm

ENV DEBIAN_FRONTEND=noninteractive

# Only update apt cache during build, don't install packages yet
RUN apt-get update

# Copy entrypoint script
COPY entrypoint.sh /entrypoint.sh
RUN chmod +x /entrypoint.sh

ENTRYPOINT ["/entrypoint.sh"]
