# syntax=docker.io/docker/dockerfile-upstream:1.20.0

FROM golang:1.25 AS go-base

WORKDIR /build

# Layer 1: Go modules (changes rarely)
COPY go.mod go.sum ./
RUN go mod download

# Layer 2: Shared code (changes moderately)
COPY api api
COPY internal internal
COPY pkg pkg

#######################################################################################################################
FROM go-base AS slurm-operator-builder

ARG GO_LDFLAGS=""
ARG BUILD_TIME
ARG CGO_ENABLED=0
ARG GOOS=linux

COPY cmd cmd
RUN --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$GOOS CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -v -o slurm_operator ./cmd/

#######################################################################################################################
FROM alpine:latest@sha256:a8560b36e8b8210634f77d9f7f9efd7ffa463e380b75e2e74aff4511df3ef88c AS slurm-operator

COPY --from=slurm-operator-builder /build/slurm_operator /usr/bin/

RUN addgroup -S -g 1001 operator && \
    adduser -S -u 1001 operator -G operator operator && \
    chown 1001:1001 /usr/bin/slurm_operator && \
    chmod 500 /usr/bin/slurm_operator

USER 1001

CMD ["/usr/bin/slurm_operator"]

#######################################################################################################################
FROM go-base AS sconfigcontroller_builder

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

#######################################################################################################################
FROM go-base AS rebooter_builder

ARG GO_LDFLAGS=""
ARG BUILD_TIME
ARG CGO_ENABLED=0
ARG GOOS=linux

COPY cmd/rebooter cmd/rebooter
RUN --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$GOOS CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -v -o rebooter ./cmd/rebooter

#######################################################################################################################
FROM alpine:latest@sha256:a8560b36e8b8210634f77d9f7f9efd7ffa463e380b75e2e74aff4511df3ef88c AS rebooter

COPY --from=rebooter_builder /build/rebooter /usr/bin/

RUN addgroup -S -g 1001 rebooter && \
    adduser -S -u 1001 rebooter -G rebooter rebooter && \
    chown 1001:1001 /usr/bin/rebooter && \
    chmod 755 /usr/bin/rebooter

USER 1001

CMD ["/usr/bin/rebooter"]

#######################################################################################################################
FROM go-base AS soperatorchecks_builder

ARG GO_LDFLAGS=""
ARG BUILD_TIME
ARG CGO_ENABLED=0
ARG GOOS=linux

COPY cmd/soperatorchecks cmd/soperatorchecks
RUN --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$GOOS CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -v -o soperatorchecks ./cmd/soperatorchecks

#######################################################################################################################
FROM alpine:latest@sha256:a8560b36e8b8210634f77d9f7f9efd7ffa463e380b75e2e74aff4511df3ef88c AS soperatorchecks

COPY --from=soperatorchecks_builder /build/soperatorchecks /usr/bin/

RUN addgroup -S -g 1001 soperatorchecks && \
    adduser -S -u 1001 soperatorchecks -G soperatorchecks soperatorchecks && \
    chown 1001:1001 /usr/bin/soperatorchecks && \
    chmod 755 /usr/bin/soperatorchecks

USER 1001

CMD ["/usr/bin/soperatorchecks"]

#######################################################################################################################
FROM go-base AS exporter-builder

ARG GO_LDFLAGS=""
ARG BUILD_TIME
ARG CGO_ENABLED=0
ARG GOOS=linux

COPY cmd/exporter cmd/exporter
RUN --mount=type=cache,target=/root/.cache/go-build \
    GOOS=$GOOS CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -v -o exporter ./cmd/exporter

#######################################################################################################################
FROM alpine:latest@sha256:a8560b36e8b8210634f77d9f7f9efd7ffa463e380b75e2e74aff4511df3ef88c AS soperator-exporter

COPY --from=exporter-builder /build/exporter /usr/bin/

RUN addgroup -S -g 1001 exporter && \
    adduser -S -u 1001 exporter -G exporter exporter && \
    chown 1001:1001 /usr/bin/exporter && \
    chmod 755 /usr/bin/exporter

USER 1001

ENTRYPOINT ["/usr/bin/exporter"]
