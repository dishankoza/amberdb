# Dockerfile for AmberDB node
FROM golang:1.23-alpine AS builder
# Install CGO dependencies for sqlite3
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -o amberdb-node ./cmd/node

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/amberdb-node /usr/local/bin/amberdb-node
WORKDIR /data
ENTRYPOINT ["amberdb-node"]
