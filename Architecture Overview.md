
# AmberDB Architecture Overview

## Overall Architecture
AmberDB is a distributed key-value database that implements:
- **Raft consensus** for replication
- **Two-Phase Commit (2PC)** for distributed transactions
- **Dynamic sharding** for scalability
- **Hybrid Logical Clock (HLC)** for consistent reads

## Main Components

### a) Node Component (`cmd/node/`)
- Main database node that handles data storage and replication
- Each node can be either a leader or follower in the Raft cluster
- Handles read/write operations for its assigned shards

### b) Metaservice (`cmd/metaservice/`)
- Manages cluster metadata
- Handles shard management and distribution
- Coordinates distributed transactions
- Entry point for client operations

### c) Client (`cmd/client/`)
- Provides interface for interacting with the cluster
- Handles transaction management and data operations

## Core Components (in `internal/`)

### a) `raftstore/`
- Implements Raft consensus algorithm
- Handles leader election, log replication
- Ensures consistency across the cluster

### b) `kvstore/`
- Core key-value storage engine
- Handles local data storage and retrieval
- Manages data persistence

### c) `metastore/`
- Manages sharding configuration
- Handles shard allocation and rebalancing
- Stores cluster metadata

### d) `coordinator/`
- Implements Two-Phase Commit protocol
- Coordinates distributed transactions
- Ensures transaction atomicity across shards

### e) `rpc/`
- gRPC server implementation
- Handles network communication between nodes
- Defines service interfaces

### f) `hlc/`
- Hybrid Logical Clock implementation
- Ensures consistent ordering of operations
- Handles timestamp management

## Data Flow

### a) Write Operation Flow
1. Client initiates a transaction through metaservice
2. Metaservice determines which shards are involved
3. Coordinator starts 2PC process
4. Affected nodes prepare changes
5. If all nodes are ready, changes are committed
6. Raft ensures changes are replicated to followers

### b) Read Operation Flow
1. Client sends read request
2. Request routed to appropriate shard
3. Node uses HLC to ensure consistent reads
4. Data retrieved from local storage
5. Response sent back to client
