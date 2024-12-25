FROM golang:1.23@sha256:f06d2bb355a67ccc6c23f3699766323a09ed0a4b724a6b25a300d4b30e01f02c AS operator_builder

ARG GO_LDFLAGS=""
ARG BUILD_TIME
ARG CGO_ENABLED=0
ARG GOOS=linux
ARG GOARCH=amd64

WORKDIR /operator

COPY . ./

RUN go mod download

RUN GOOS=$GOOS GOARCH=$GOARCH CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -o slurm_operator ./cmd/

#######################################################################################################################
FROM alpine:latest@sha256:21dc6063fd678b478f57c0e13f47560d0ea4eeba26dfc947b2a4f81f686b9f45 AS slurm-operator

COPY --from=operator_builder /operator/slurm_operator /usr/bin/

RUN addgroup -S -g 1001 operator && \
    adduser -S -u 1001 operator -G operator operator && \
    chown 1001:1001 /usr/bin/slurm_operator && \
    chmod 500 /usr/bin/slurm_operator

USER 1001

CMD ["/usr/bin/slurm_operator"]
