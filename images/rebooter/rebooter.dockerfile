FROM go-base AS rebooter_builder

ARG GO_LDFLAGS=""
ARG BUILD_TIME
ARG CGO_ENABLED=0
ARG GOOS=linux

COPY cmd/rebooter cmd/rebooter
RUN --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$GOOS CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -o rebooter ./cmd/rebooter

#######################################################################################################################
FROM alpine:latest@sha256:a8560b36e8b8210634f77d9f7f9efd7ffa463e380b75e2e74aff4511df3ef88c AS rebooter

COPY --from=rebooter_builder /build/rebooter /usr/bin/

RUN addgroup -S -g 1001 rebooter && \
    adduser -S -u 1001 rebooter -G rebooter rebooter && \
    chown 1001:1001 /usr/bin/rebooter && \
    chmod 755 /usr/bin/rebooter

USER 1001

CMD ["/usr/bin/rebooter"]
