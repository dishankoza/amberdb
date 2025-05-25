// internal/raftstore/fsm.go
package raftstore

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"

	"github.com/dishankoza/amberdb/internal/kvstore"
	"github.com/hashicorp/raft"
)

type FSM struct {
	store *kvstore.Store
}

func NewFSM(store *kvstore.Store) *FSM {
	return &FSM{store: store}
}

// Command represents a Raft log entry
type Command struct {
	Key   string
	Value string
	TxID  string
}

func (f *FSM) Apply(log *raft.Log) interface{} {
	var cmd Command
	decoder := gob.NewDecoder(bytes.NewReader(log.Data))
	if err := decoder.Decode(&cmd); err != nil {
		return fmt.Errorf("failed to decode command: %w", err)
	}
	// Apply write to kvstore
	return f.store.Write(cmd.Key, cmd.Value, cmd.TxID)
}

func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	// Not implemented for now
	return &noopSnapshot{}, nil
}

func (f *FSM) Restore(snapshot io.ReadCloser) error {
	// No-op for now
	return nil
}

// noopSnapshot implements raft.FSMSnapshot but does nothing
type noopSnapshot struct{}

func (n *noopSnapshot) Persist(sink raft.SnapshotSink) error {
	return sink.Close()
}

func (n *noopSnapshot) Release() {}
