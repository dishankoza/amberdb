.PHONY: all build clean test fmt lint proto docker-build docker-up docker-down run-local

# Binary names
NODE_BINARY = amberdb-node
META_BINARY = amberdb-metaservice
CLIENT_BINARY = amberdb-client

all: fmt lint test build

build:
	go build -o $(NODE_BINARY) ./cmd/node
	go build -o $(META_BINARY) ./cmd/metaservice
	go build -o $(CLIENT_BINARY) ./cmd/client

clean:
	rm -f $(NODE_BINARY) $(META_BINARY) $(CLIENT_BINARY)
	rm -rf raft-data/
	rm -f *.db
	rm -f *.log

test:
	go test -v -race ./...

fmt:
	go fmt ./...

lint:
	go vet ./...
	golangci-lint run

proto:
	protoc --go_out=. --go-grpc_out=. proto/amberdb.proto

docker-build:
	docker-compose build

docker-up:
	docker-compose up -d
	docker-compose logs -f

docker-down:
	docker-compose down
	docker-compose rm -f

run-local: build
	./run-local-nodes.sh

.DEFAULT_GOAL := all
