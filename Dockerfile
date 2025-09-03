# Build

FROM golang:alpine AS builder

WORKDIR /app
COPY . .

RUN go mod download

RUN go build -o server ./cmd/server/main.go
RUN go build -o cli ./cmd/cli/main.go

# Runtime

FROM alpine:latest

WORKDIR /hetzner-restic

# Install restic and docker CLI
RUN apk add --no-cache restic docker-cli

COPY --from=builder /app/server .
COPY --from=builder /app/cli ./hetzner-restic

ENTRYPOINT ["/app/server"]