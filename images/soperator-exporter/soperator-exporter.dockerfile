FROM go-base AS exporter-builder

ARG GO_LDFLAGS=""
ARG BUILD_TIME
ARG CGO_ENABLED=0
ARG GOOS=linux

COPY cmd/exporter cmd/exporter
RUN --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$GOOS CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -o exporter ./cmd/exporter

#######################################################################################################################
FROM alpine:latest@sha256:a8560b36e8b8210634f77d9f7f9efd7ffa463e380b75e2e74aff4511df3ef88c AS soperator-exporter

COPY --from=exporter-builder /build/exporter /usr/bin/

RUN addgroup -S -g 1001 exporter && \
    adduser -S -u 1001 exporter -G exporter exporter && \
    chown 1001:1001 /usr/bin/exporter && \
    chmod 755 /usr/bin/exporter

USER 1001

ENTRYPOINT ["/usr/bin/exporter"]
