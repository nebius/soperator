FROM alpine:3.18@sha256:48d9183eb12a05c99bcc0bf44a003607b8e941e1d4f41f9ad12bdcc4b5672f86 AS jail-logs-sync

# Install rsync at build time
RUN apk add --no-cache rsync && \
    rm -rf /var/cache/apk/*

# Default command will be provided by the pod spec
CMD ["/bin/sh"]