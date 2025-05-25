// internal/raftstore/store.go
package raftstore

import (
	"net"
	"os"
	"path/filepath"
	"time"

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

func NewRaftNode(dataDir, nodeID, bindAddr string, peers []raft.Server, fsm raft.FSM) (*Store, error) {
	// Create raft config
	raftConfig := raft.DefaultConfig()
	raftConfig.LocalID = raft.ServerID(nodeID)

	raftBind, err := net.ResolveTCPAddr("tcp", bindAddr)
	if err != nil {
		return nil, err
	}
	transport, err := raft.NewTCPTransport(bindAddr, raftBind, 3, raftTimeout(), os.Stderr)
	if err != nil {
		return nil, err
	}

	snapshots, err := raft.NewFileSnapshotStore(dataDir, 2, os.Stderr)
	if err != nil {
		return nil, err
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
		config := raft.Configuration{Servers: peers}
		r.BootstrapCluster(config)
	}

	return store, nil
}

func raftTimeout() time.Duration {
	return 10 * time.Second
}
