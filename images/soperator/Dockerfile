FROM golang:1.24 AS operator_builder

ARG GO_LDFLAGS=""
ARG BUILD_TIME
ARG CGO_ENABLED=0
ARG GOOS=linux

WORKDIR /operator

# Copy only the necessary files to build the binary.
COPY api api
COPY cmd cmd
COPY internal internal
COPY pkg pkg
COPY go.mod go.sum ./

RUN go mod download

RUN GOOS=$GOOS CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -o slurm_operator ./cmd/

#######################################################################################################################
FROM alpine:latest@sha256:a8560b36e8b8210634f77d9f7f9efd7ffa463e380b75e2e74aff4511df3ef88c AS slurm-operator

COPY --from=operator_builder /operator/slurm_operator /usr/bin/

RUN addgroup -S -g 1001 operator && \
    adduser -S -u 1001 operator -G operator operator && \
    chown 1001:1001 /usr/bin/slurm_operator && \
    chmod 500 /usr/bin/slurm_operator

USER 1001

CMD ["/usr/bin/slurm_operator"]
