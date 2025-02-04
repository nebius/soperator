FROM golang:1.23@sha256:e213430692e5c31aba27473cdc84cfff2896d0c097e984bef67b6a44c75a8181 AS rebooter_builder

ARG GO_LDFLAGS=""
ARG BUILD_TIME
ARG CGO_ENABLED=0
ARG GOOS=linux
ARG GOARCH=amd64

WORKDIR /operator

COPY . ./

RUN go mod download

RUN GOOS=$GOOS GOARCH=$GOARCH CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -o rebooter ./cmd/rebooter

#######################################################################################################################
FROM alpine:latest@sha256:56fa17d2a7e7f168a043a2712e63aed1f8543aeafdcee47c58dcffe38ed51099 AS rebooter

COPY --from=rebooter_builder /operator/rebooter /usr/bin/

RUN addgroup -S -g 1001 rebooter && \
    adduser -S -u 1001 rebooter -G rebooter rebooter && \
    chown 1001:1001 /usr/bin/rebooter && \
    chmod 755 /usr/bin/rebooter

USER 1001

CMD ["/usr/bin/rebooter"]
