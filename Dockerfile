# Build

FROM golang:1.24.3-alpine AS builder

WORKDIR /app
COPY . .

RUN go mod download

RUN go build -o /build ./cmd/main.go

# Runtime

FROM alpine:latest

WORKDIR /hetzner-restic

# Install restic and docker CLI
RUN apk add --no-cache restic docker-cli

COPY --from=builder /build /app

ENTRYPOINT ["/app"]