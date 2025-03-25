FROM golang:1.24 AS soperatorchecks_builder

ARG GO_LDFLAGS=""
ARG BUILD_TIME
ARG CGO_ENABLED=0
ARG GOOS=linux
ARG GOARCH=amd64

WORKDIR /operator

COPY . ./

RUN go mod download

RUN GOOS=$GOOS GOARCH=$GOARCH CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -o soperatorchecks ./cmd/soperatorchecks

#######################################################################################################################
FROM alpine:latest@sha256:a8560b36e8b8210634f77d9f7f9efd7ffa463e380b75e2e74aff4511df3ef88c AS soperatorchecks

COPY --from=soperatorchecks_builder /operator/soperatorchecks /usr/bin/

RUN addgroup -S -g 1001 soperatorchecks && \
    adduser -S -u 1001 soperatorchecks -G soperatorchecks soperatorchecks && \
    chown 1001:1001 /usr/bin/soperatorchecks && \
    chmod 755 /usr/bin/soperatorchecks

USER 1001

CMD ["/usr/bin/soperatorchecks"]
