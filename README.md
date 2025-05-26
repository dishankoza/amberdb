# AmberDB

AmberDB is a distributed key-value database designed for high availability and fault tolerance using the Raft consensus algorithm. It is implemented in Go and provides a simple interface for storing and retrieving data across multiple nodes.

## Project Structure

- **cmd/**: Contains entry points for different components:
  - `node/`: Main binary for running a database node.
  - `metaservice/`: Optional metadata service for managing sharding and cluster metadata.
  - `client/`: (If present) Client for interacting with the cluster.
- **internal/**: Core logic and internal modules:
  - `raftstore/`: Raft consensus implementation and configuration.
  - `kvstore/`: Key-value storage engine.
  - `metastore/`: Sharding and metadata management.
  - `rpc/`: gRPC server implementation.
  - `hlc/`: Hybrid logical clock utilities.
- **proto/**: Protocol buffer definitions and generated gRPC code.
- **raft-data/**: Data directories for each node's Raft state and logs.
- **run-local-nodes.sh**: Script to build and launch a 3-node local cluster and optional metaservice.
- **webServer/**: (Optional) Web server component for UI or API access.

## Architecture Overview

AmberDB uses a replicated state machine architecture based on the Raft consensus protocol. Each node maintains its own copy of the data and participates in leader election and log replication. The cluster can tolerate node failures as long as a majority of nodes are available.

- **Nodes**: Each node runs the AmberDB binary and participates in the Raft cluster.
- **Raft Consensus**: Ensures consistency and fault tolerance across nodes.
- **Metaservice**: (Optional) Handles sharding and metadata for scaling out to multiple clusters or partitions.
- **Client**: Connects to any node to perform read/write operations.

## Data Flow

1. **Client Request**: A client sends a request (read/write) to any node.
2. **Leader Forwarding**: If the node is not the leader, it forwards the request to the current leader.
3. **Replication**: The leader appends the request to its log and replicates it to follower nodes using Raft.
4. **Commit**: Once a majority of nodes acknowledge, the leader commits the entry and applies it to the state machine.
5. **Response**: The leader responds to the client with the result.

## How to Run Locally

1. **Prerequisites**:
   - Go installed (version 1.18+ recommended)
   - (Optional) Protobuf compiler for regenerating gRPC code

2. **Build and Start Nodes**:
   - Run the provided script:
     ```sh
     ./run-local-nodes.sh
     ```
   - This will:
     - Build the AmberDB node binary
     - Prepare Raft configuration and data directories
     - Start 3 nodes (node1, node2, node3) on localhost
     - Optionally start the metaservice (uncomment in script if needed)

3. **Logs**:
   - Node logs: `node1.log`, `node2.log`, `node3.log`
   - Metaservice log: `metaservice.log`

4. **Stopping the Cluster**:
   - To stop all nodes:
     ```sh
     pkill amberdb-node
     pkill amberdb-metaservice
     ```

5. **Client Usage**:
   - (If client binary is present) You can run the client to interact with the cluster:
     ```sh
     ./amberdb-client --server=localhost:50051
     ```

## Customization
- Edit `internal/raftstore/raft_config.json` to change node addresses or cluster size.
- Edit `internal/metastore/shard_config.json` for sharding configuration (if using metaservice).

## License
MIT License (or specify your license here)
