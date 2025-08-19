FROM go-base:${TARGETARCH} AS sconfigcontroller_builder

ARG GO_LDFLAGS=""
ARG BUILD_TIME
ARG CGO_ENABLED=0
ARG GOOS=linux

COPY cmd/sconfigcontroller cmd/sconfigcontroller
RUN --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$GOOS CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -v -o sconfigcontroller ./cmd/sconfigcontroller

#######################################################################################################################
FROM alpine:latest@sha256:a8560b36e8b8210634f77d9f7f9efd7ffa463e380b75e2e74aff4511df3ef88c AS sconfigcontroller

COPY --from=sconfigcontroller_builder /build/sconfigcontroller /usr/bin/

RUN addgroup -S -g 1001 sconfigcontroller && \
    adduser -S -u 1001 sconfigcontroller -G sconfigcontroller sconfigcontroller && \
    chown 1001:1001 /usr/bin/sconfigcontroller && \
    chmod 755 /usr/bin/sconfigcontroller

USER 1001

CMD ["/usr/bin/sconfigcontroller"]
