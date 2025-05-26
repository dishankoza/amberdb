# Dockerfile for MetaService
FROM golang:1.23-alpine AS builder
# Install CGO and sqlite3 deps
RUN apk add --no-cache gcc musl-dev sqlite-dev
ENV CGO_ENABLED=1 GOOS=linux
WORKDIR /app
# Copy only go.mod and go.sum to leverage Docker cache
COPY go.mod go.sum ./
# Download Go modules
RUN go mod download
# Copy rest of the code
COPY . .
# Build metaservice
RUN go build -o metaservice ./cmd/metaservice

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/metaservice /usr/local/bin/metaservice
WORKDIR /app
ENTRYPOINT ["metaservice"]
