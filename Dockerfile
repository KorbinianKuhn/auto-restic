# Build

FROM golang:1.27.3-alpine@sha256:b8c9b82affbbb82a020bbcf998dad2ba9516944ac7e883524642a481a9505e9c AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

# Static binaries with embedded tzdata, independent of the runtime libc
ENV CGO_ENABLED=0
RUN go build -trimpath -tags timetzdata -o server ./cmd/server/main.go
RUN go build -trimpath -tags timetzdata -o auto-restic ./cmd/cli/main.go

# Runtime

FROM cgr.dev/chainguard/wolfi-base:latest@sha256:360896efb0753a5701c48f860e17130c205eae5a264797aa3e7ea126ee2923a7

WORKDIR /auto-restic

# Install restic and docker CLI
RUN apk add --no-cache restic docker-cli

COPY --from=builder /app/server .
COPY --from=builder /app/auto-restic .

ENV PATH="/auto-restic:${PATH}"

ENTRYPOINT ["/auto-restic/server"]
