# Build

FROM golang:alpine AS builder

WORKDIR /app
COPY . .

RUN go mod download

RUN go build -o server ./cmd/server/main.go
RUN go build -o auto-restic ./cmd/cli/main.go

# Runtime

FROM alpine:latest

WORKDIR /auto-restic

# Install restic and docker CLI
RUN apk add --no-cache restic docker-cli

COPY --from=builder /app/server .
COPY --from=builder /app/auto-restic .

ENV PATH="/auto-restic:${PATH}"

ENTRYPOINT ["/auto-restic/server"]