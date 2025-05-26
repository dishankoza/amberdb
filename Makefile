.PHONY: fmt build docker-up docker-down test all

fmt:
	go fmt ./...

build:
	go build ./cmd/node
	go build ./cmd/metaservice
	go build ./cmd/client

docker-up:
	docker-compose build
	docker-compose up -d
	docker-compose logs -f metaservice node1 node2 node3
	docker-compose logs -f client

docker-down:
	docker-compose down

test:
	go test ./internal/hlc

all: fmt build test docker-up
