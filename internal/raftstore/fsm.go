// internal/raftstore/fsm.go
package raftstore

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
	"log"

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

func (f *FSM) Apply(logEntry *raft.Log) interface{} {
	var cmd Command
	decoder := gob.NewDecoder(bytes.NewReader(logEntry.Data))
	if err := decoder.Decode(&cmd); err != nil {
		return fmt.Errorf("failed to decode command: %w", err)
	}
	// Dispatch based on operation
	switch cmd.Op {
	case "WRITE":
		log.Printf("[FSM-WRITE] Applying write - Key: %s, Value: %s, TxID: %s, Timestamp: %s", cmd.Key, cmd.Value, cmd.TxID, cmd.Timestamp)
		err := f.store.WriteWithTimestamp(cmd.Key, cmd.Value, cmd.TxID, cmd.Timestamp)
		if err != nil {
			log.Printf("[FSM-WRITE-ERROR] Failed to apply write - Key: %s, Error: %v", cmd.Key, err)
			return err
		}
		log.Printf("[FSM-WRITE-SUCCESS] Write applied - Key: %s", cmd.Key)
		return nil
	case "COMMIT":
		log.Printf("[FSM-COMMIT] Applying commit - TxID: %s", cmd.TxID)
		return f.store.Commit(cmd.TxID)
	case "ABORT":
		log.Printf("[FSM-ABORT] Applying abort - TxID: %s", cmd.TxID)
		return f.store.Abort(cmd.TxID)
	default:
		return fmt.Errorf("unknown command operation: %s", cmd.Op)
	}
}

// Snapshot creates a snapshot of the FSM's state
func (f *FSM) Snapshot() (raft.FSMSnapshot, error) {
	f.store.RLock()
	defer f.store.RUnlock()

	// Query all committed data
	rows, err := f.store.GetDB().Query(`
		SELECT key, value, timestamp, tx_id 
		FROM kv_store 
		WHERE committed = 1
		ORDER BY timestamp ASC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to query data for snapshot: %w", err)
	}
	defer rows.Close()

	// Build snapshot data
	var snapshot []Command
	for rows.Next() {
		var cmd Command
		if err := rows.Scan(&cmd.Key, &cmd.Value, &cmd.Timestamp, &cmd.TxID); err != nil {
			return nil, fmt.Errorf("failed to scan row: %w", err)
		}
		cmd.Op = "WRITE"
		snapshot = append(snapshot, cmd)
	}

	return &FSMSnapshot{commands: snapshot}, nil
}

// Restore restores the FSM's state from a snapshot
func (f *FSM) Restore(rc io.ReadCloser) error {
	defer rc.Close()

	f.store.Lock()
	defer f.store.Unlock()

	// Clear existing data
	if _, err := f.store.GetDB().Exec("DELETE FROM kv_store"); err != nil {
		return fmt.Errorf("failed to clear existing data: %w", err)
	}

	// Decode snapshot data
	var snapshot []Command
	if err := gob.NewDecoder(rc).Decode(&snapshot); err != nil {
		return fmt.Errorf("failed to decode snapshot: %w", err)
	}

	// Restore each command
	for _, cmd := range snapshot {
		if err := f.store.WriteWithTimestamp(cmd.Key, cmd.Value, cmd.TxID, cmd.Timestamp); err != nil {
			return fmt.Errorf("failed to restore command: %w", err)
		}
		if err := f.store.Commit(cmd.TxID); err != nil {
			return fmt.Errorf("failed to commit restored command: %w", err)
		}
	}

	return nil
}

// FSMSnapshot implements the raft.FSMSnapshot interface
type FSMSnapshot struct {
	commands []Command
}

func (f *FSMSnapshot) Persist(sink raft.SnapshotSink) error {
	err := func() error {
		// Encode snapshot data
		var buf bytes.Buffer
		if err := gob.NewEncoder(&buf).Encode(f.commands); err != nil {
			return fmt.Errorf("failed to encode commands: %w", err)
		}

		// Write to sink
		if _, err := sink.Write(buf.Bytes()); err != nil {
			return fmt.Errorf("failed to write to sink: %w", err)
		}

		return sink.Close()
	}()

	if err != nil {
		sink.Cancel()
		return err
	}

	return nil
}

func (f *FSMSnapshot) Release() {
	f.commands = nil
}
