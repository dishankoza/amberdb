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
	Op        string // "WRITE", "COMMIT", or "ABORT"
	Key       string
	Value     string
	TxID      string
	Timestamp string // HLC or system timestamp for versioning
}

func (f *FSM) Apply(log *raft.Log) interface{} {
	var cmd Command
	decoder := gob.NewDecoder(bytes.NewReader(log.Data))
	if err := decoder.Decode(&cmd); err != nil {
		return fmt.Errorf("failed to decode command: %w", err)
	}
	// Dispatch based on operation
	switch cmd.Op {
	case "WRITE":
		// Use timestamp-aware write
		return f.store.WriteWithTimestamp(cmd.Key, cmd.Value, cmd.TxID, cmd.Timestamp)
	case "COMMIT":
		return f.store.Commit(cmd.TxID)
	case "ABORT":
		return f.store.Abort(cmd.TxID)
	default:
		return fmt.Errorf("unknown command operation: %s", cmd.Op)
	}
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
