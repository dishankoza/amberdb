FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o amberdb-client ./cmd/client

FROM alpine:latest
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/amberdb-client /usr/local/bin/amberdb-client
ENTRYPOINT ["amberdb-client"]
