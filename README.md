# AmberDB

AmberDB is a distributed key-value database designed for high availability and fault tolerance using the Raft consensus algorithm. It is implemented in Go and provides a simple interface for storing and retrieving data across multiple nodes.

## Features

- Distributed key-value storage with ACID transactions
- Raft consensus for high availability and fault tolerance
- Two-Phase Commit (2PC) for cross-shard transactions
- Dynamic sharding for horizontal scalability
- Hybrid Logical Clock (HLC) for consistent reads
- gRPC-based communication
- Docker support for easy deployment

## Project Structure

- **cmd/**: Contains entry points for different components:
  - `node/`: Main binary for running a database node.
  - `metaservice/`: Metadata service for managing sharding and distributed transactions.
  - `client/`: Client for interacting with the cluster.
- **internal/**: Core logic and internal modules:
  - `raftstore/`: Raft consensus implementation and configuration.
  - `kvstore/`: Key-value storage engine.
  - `metastore/`: Sharding and metadata management.
  - `coordinator/`: Two-Phase Commit coordination.
  - `rpc/`: gRPC server implementation.
  - `hlc/`: Hybrid logical clock utilities.
- **proto/**: Protocol buffer definitions and generated gRPC code.
- **raft-data/**: Data directories for each node's Raft state and logs.

## Prerequisites

1. Go 1.18 or later
2. Docker and Docker Compose (optional)
3. Protobuf compiler (optional, for development)

## Running Locally

### Option 1: Using Docker Compose

1. Start the cluster:
   ```bash
   docker-compose up -d
   ```

2. Check logs:
   ```bash
   docker-compose logs -f
   ```

3. Stop the cluster:
   ```bash
   docker-compose down
   ```

### Option 2: Running Directly

1. Build the binaries:
   ```bash
   make build
   ```

2. Start the metaservice:
   ```bash
   META_PORT=8080 ./amberdb-metaservice
   ```

3. Start the nodes:
   ```bash
   # Node 1
   NODE_ID=node1 RAFT_ADDR=localhost:9001 PORT=50051 DB_PATH=node1.db ./amberdb-node

   # Node 2
   NODE_ID=node2 RAFT_ADDR=localhost:9002 PORT=50052 DB_PATH=node2.db ./amberdb-node

   # Node 3
   NODE_ID=node3 RAFT_ADDR=localhost:9003 PORT=50053 DB_PATH=node3.db ./amberdb-node
   ```

4. Or use the provided script:
   ```bash
   ./run-local-nodes.sh
   ```

## Using the Database

### Basic Operations

1. Start a transaction:
   ```bash
   curl -X POST http://localhost:8080/transaction/begin
   # Response: {"transaction_id": "<txid>"}
   ```

2. Write data:
   ```bash
   curl -X POST http://localhost:8080/transaction/prepare \
     -H "Content-Type: application/json" \
     -d '{
       "transaction_id": "<txid>",
       "writes": {
         "key1": {"key": "key1", "value": "value1"},
         "key2": {"key": "key2", "value": "value2"}
       }
     }'
   ```

3. Commit transaction:
   ```bash
   curl -X POST http://localhost:8080/transaction/commit \
     -H "Content-Type: application/json" \
     -d '{"transaction_id": "<txid>"}'
   ```

4. Read data:
   ```bash
   curl -X GET "http://localhost:50051/read?key=key1"
   ```

### Sharding Operations

1. Add a new shard:
   ```bash
   curl -X POST http://localhost:8080/shards \
     -H "Content-Type: application/json" \
     -d '{
       "id": "shard1",
       "min_key": "a",
       "max_key": "m",
       "nodes": ["localhost:50051", "localhost:50052"],
       "primary": "localhost:50051"
     }'
   ```

2. List shards:
   ```bash
   curl http://localhost:8080/shards
   ```

3. Update shard:
   ```bash
   curl -X PUT http://localhost:8080/shards/shard1 \
     -H "Content-Type: application/json" \
     -d '{
       "nodes": ["localhost:50051", "localhost:50052", "localhost:50053"]
     }'
   ```

## Monitoring

- Node logs: `node1.log`, `node2.log`, `node3.log`
- Metaservice log: `metaservice.log`
- Transaction status:
  ```bash
  curl "http://localhost:8080/transaction/status?transaction_id=<txid>"
  ```

## Configuration

1. Raft Configuration (`internal/raftstore/raft_config.json`):
   ```json
   [
     {"id": "node1", "address": "localhost:9001"},
     {"id": "node2", "address": "localhost:9002"},
     {"id": "node3", "address": "localhost:9003"}
   ]
   ```

2. Shard Configuration (`internal/metastore/shard_config.json`):
   ```json
   {
     "shards": [
       {
         "id": "shard1",
         "min_key": "a",
         "max_key": "m",
         "nodes": ["localhost:50051", "localhost:50052"],
         "primary": "localhost:50051"
       }
     ]
   }
   ```

## Development

1. Generate Protocol Buffers:
   ```bash
   protoc --go_out=. --go-grpc_out=. proto/amberdb.proto
   ```

2. Run tests:
   ```bash
   go test ./...
   ```

3. Build binaries:
   ```bash
   make build
   ```

## License

MIT License


## Instructions to run

NODE_ID=node1 RAFT_ADDR=localhost:9001 PORT=50051 DB_PATH=node1.db go run cmd/node/main.go
NODE_ID=node2 RAFT_ADDR=localhost:9002 PORT=50052 DB_PATH=node2.db go run cmd/node/main.go
NODE_ID=node3 RAFT_ADDR=localhost:9003 PORT=50053 DB_PATH=node3.db go run cmd/node/main.go