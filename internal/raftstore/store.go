// internal/raftstore/store.go
package raftstore

import (
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/raft"
	raftboltdb "github.com/hashicorp/raft-boltdb"
)

type Store struct {
	raft *raft.Raft
}

func (s *Store) IsLeader() bool {
	return s.raft.State() == raft.Leader
}

func (s *Store) Apply(data []byte, timeout time.Duration) raft.ApplyFuture {
	return s.raft.Apply(data, timeout)
}

// NewRaftNode creates and starts a Raft node.
// NewRaftNode(dataDir, nodeID, bindAddr string, peers []raft.Server, fsm raft.FSM)
func NewRaftNode(dataDir, nodeID, advertiseAddr, bindAddr string, peers []raft.Server, fsm raft.FSM) (*Store, error) {
	// Create raft config
	raftConfig := raft.DefaultConfig()
	raftConfig.LocalID = raft.ServerID(nodeID)

	// Create a proper logger for Raft
	logger := hclog.New(&hclog.LoggerOptions{
		Name:   "raft-transport",
		Level:  hclog.Info,
		Output: os.Stderr,
	})

	// Resolve the advertise address
	advertiseTCP, err := net.ResolveTCPAddr("tcp", advertiseAddr)
	if err != nil {
		return nil, err
	}

	// Create transport: use bindAddr for listening and advertiseTCP for advertising
	transport, err := raft.NewTCPTransportWithLogger(
		bindAddr,      // Address to bind to for listening
		advertiseTCP,  // Address to advertise to other nodes
		3,             // Max pool size
		raftTimeout(), // Timeout
		logger,        // Logger
	)
	if err != nil {
		return nil, err
	}

	// Create a snapshot store with the appropriate logger
	snapshots, err := raft.NewFileSnapshotStoreWithLogger(
		dataDir,
		2,
		logger,
	)
	if err != nil {
		return nil, err
	}

	// Fix peer addresses before creating the Raft instance
	// This ensures we use container hostnames rather than localhost
	// (Make sure the addresses passed in are correct, e.g., node1:9001, not localhost:9001)
	fixedPeers := make([]raft.Server, 0, len(peers))
	for _, p := range peers {
		logger.Info("configuring peer", "id", p.ID, "address", p.Address)
		// Extract the node ID (like "node1", "node2", etc.)
		id := string(p.ID)
		// Create a properly formatted address with the container hostname
		fixedPeers = append(fixedPeers, raft.Server{
			ID:       p.ID,
			Address:  p.Address,
			Suffrage: p.Suffrage,
		})
		logger.Info("configured peer", "id", id, "address", p.Address)
	}

	logStore, err := raftboltdb.NewBoltStore(filepath.Join(dataDir, "raft-log.bolt"))
	if err != nil {
		return nil, err
	}

	stableStore, err := raftboltdb.NewBoltStore(filepath.Join(dataDir, "raft-stable.bolt"))
	if err != nil {
		return nil, err
	}

	r, err := raft.NewRaft(raftConfig, fsm, logStore, stableStore, snapshots, transport)
	if err != nil {
		return nil, err
	}

	store := &Store{raft: r}

	// Bootstrap the cluster if necessary
	hasState, err := raft.HasExistingState(logStore, stableStore, snapshots)
	if err != nil {
		return nil, err
	}
	if !hasState {
		config := raft.Configuration{Servers: fixedPeers}
		logger.Info("Bootstrapping cluster with peers", "peers", fixedPeers)
		r.BootstrapCluster(config)
	}

	return store, nil
}

func raftTimeout() time.Duration {
	return 10 * time.Second
}
