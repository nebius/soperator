FROM golang:1.23@sha256:927112936d6b496ed95f55f362cc09da6e3e624ef868814c56d55bd7323e0959 AS sconfigcontroller_builder

ARG GO_LDFLAGS=""
ARG BUILD_TIME
ARG CGO_ENABLED=0
ARG GOOS=linux
ARG GOARCH=amd64

WORKDIR /operator

COPY . ./

RUN go mod download

RUN GOOS=$GOOS GOARCH=$GOARCH CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -o sconfigcontroller ./cmd/sconfigcontroller

#######################################################################################################################
FROM alpine:latest@sha256:a8560b36e8b8210634f77d9f7f9efd7ffa463e380b75e2e74aff4511df3ef88c AS sconfigcontroller

COPY --from=sconfigcontroller_builder /operator/sconfigcontroller /usr/bin/

RUN addgroup -S -g 1001 sconfigcontroller && \
    adduser -S -u 1001 sconfigcontroller -G sconfigcontroller sconfigcontroller && \
    chown 1001:1001 /usr/bin/sconfigcontroller && \
    chmod 755 /usr/bin/sconfigcontroller

USER 1001

CMD ["/usr/bin/sconfigcontroller"]
