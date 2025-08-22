FROM golang:1.24.6 AS go-base

WORKDIR /build

# Layer 1: Go modules (changes rarely)
COPY go.mod go.sum ./
RUN go mod download

# Layer 2: Shared code (changes moderately)
COPY api api
COPY internal internal
COPY pkg pkg
