# Build stage
FROM golang:1.21-alpine AS builder

# Install build dependencies
RUN apk add --no-cache gcc musl-dev sqlite-dev

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=1 GOOS=linux go build -o amberdb-node ./cmd/node/
RUN CGO_ENABLED=1 GOOS=linux go build -o amberdb-metaservice ./cmd/metaservice/

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN apk add --no-cache sqlite-libs ca-certificates

WORKDIR /app
COPY --from=builder /app/amberdb-node /app/amberdb-node
COPY --from=builder /app/amberdb-metaservice /app/amberdb-metaservice

# Create necessary directories
RUN mkdir -p /data /config /logs

# Set environment variables
ENV DB_PATH=/data/node.db \
    RAFT_CONFIG_PATH=/config/raft_config.json \
    SHARD_CONFIG_PATH=/config/shard_config.json

# Default command (will be overridden by k8s)
CMD ["./amberdb-node"] 