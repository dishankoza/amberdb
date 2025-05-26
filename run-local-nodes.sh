#!/bin/bash

set -e

# Build AmberDB node binary
echo "Building AmberDB node binary..."
go build -o amberdb-node ./cmd/node

# Build AmberDB metaservice binary (optional, uncomment if needed)
# echo "Building AmberDB metaservice binary..."
# go build -o amberdb-metaservice ./cmd/metaservice

# Prepare raft_config.json for localhost
cat > ./internal/raftstore/raft_config.json <<EOF
[
  {"id": "node1", "address": "localhost:9001"},
  {"id": "node2", "address": "localhost:9002"},
  {"id": "node3", "address": "localhost:9003"}
]
EOF

# Prepare data directories
for i in 1 2 3; do
  mkdir -p ./raft-data/node$i
  rm -f ./node$i.db
done

# Start node1
echo "Starting node1..."
NODE_ID=node1 \
RAFT_ADDR=localhost:9001 \
RAFT_BIND_ADDR=0.0.0.0:9001 \
PORT=50051 \
DB_PATH=./node1.db \
RAFT_CONFIG_PATH=./internal/raftstore/raft_config.json \
./amberdb-node > node1.log 2>&1 &

# Start node2
echo "Starting node2..."
NODE_ID=node2 \
RAFT_ADDR=localhost:9002 \
RAFT_BIND_ADDR=0.0.0.0:9002 \
PORT=50052 \
DB_PATH=./node2.db \
RAFT_CONFIG_PATH=./internal/raftstore/raft_config.json \
./amberdb-node > node2.log 2>&1 &

# Start node3
echo "Starting node3..."
NODE_ID=node3 \
RAFT_ADDR=localhost:9003 \
RAFT_BIND_ADDR=0.0.0.0:9003 \
PORT=50053 \
DB_PATH=./node3.db \
RAFT_CONFIG_PATH=./internal/raftstore/raft_config.json \
./amberdb-node > node3.log 2>&1 &

echo "All nodes started."
echo "Logs: node1.log, node2.log, node3.log"
echo "To stop all nodes: pkill amberdb-node"

# Optionally, start metaservice (uncomment if needed)
echo "Starting metaservice..."
META_PORT=8080 \
SHARD_CONFIG_PATH=./internal/metastore/shard_config.json \
RAFT_CONFIG_PATH=./internal/raftstore/raft_config.json \
./amberdb-metaservice > metaservice.log 2>&1 &

# Start client (optional, adjust args as needed)
# echo "Starting client..."
# ./amberdb-client --server=localhost:50051 > client.log 2>&1 &

echo "Metaservice started. Log: metaservice.log"
