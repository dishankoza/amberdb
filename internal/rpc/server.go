// internal/rpc/server.go
package rpc

import (
	"bytes"
	"context"
	"encoding/gob"
	"log"
	"time"

	"github.com/dishankoza/amberdb/internal/hlc"
	"github.com/dishankoza/amberdb/internal/kvstore"
	"github.com/dishankoza/amberdb/internal/raftstore"
	amberpb "github.com/dishankoza/amberdb/proto"
	"google.golang.org/grpc"
)

type server struct {
	amberpb.UnimplementedAmberServiceServer
	store     *kvstore.Store
	raftStore *raftstore.Store
	clock     *hlc.Clock
}

func RegisterAmberService(grpcServer *grpc.Server, store *kvstore.Store, raftStore *raftstore.Store) {
	// Initialize HLC clock for reads
	clock := hlc.NewClock()
	amberpb.RegisterAmberServiceServer(grpcServer, &server{store: store, raftStore: raftStore, clock: clock})
}

func (s *server) BeginTransaction(ctx context.Context, _ *amberpb.Empty) (*amberpb.TxnID, error) {
	txID := s.store.BeginTransaction()
	return &amberpb.TxnID{Id: txID}, nil
}

func (s *server) Write(ctx context.Context, req *amberpb.WriteRequest) (*amberpb.Status, error) {
	if !s.raftStore.IsLeader() {
		log.Printf("[WRITE-REJECTED] Not the leader - Key: %s, Value: %s, TxID: %s", req.Key, req.Value, req.TxId)
		return &amberpb.Status{Success: false, Message: "not the leader"}, nil
	}

	// Use HLC timestamp for ordering
	ts := s.clock.Now()
	log.Printf("[WRITE-REQUEST] Key: %s, Value: %s, TxID: %s, Timestamp: %s", req.Key, req.Value, req.TxId, ts)

	cmd := raftstore.Command{
		Op:        "WRITE",
		Key:       req.Key,
		Value:     req.Value,
		TxID:      req.TxId,
		Timestamp: ts,
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(cmd); err != nil {
		log.Printf("[WRITE-ERROR] Failed to encode command - Key: %s, Error: %v", req.Key, err)
		return &amberpb.Status{Success: false, Message: "encoding failed"}, nil
	}

	applyFuture := s.raftStore.Apply(buf.Bytes(), 5*time.Second)
	if err := applyFuture.Error(); err != nil {
		log.Printf("[WRITE-ERROR] Raft apply failed - Key: %s, Error: %v", req.Key, err)
		return &amberpb.Status{Success: false, Message: "raft apply failed"}, nil
	}

	log.Printf("[WRITE-SUCCESS] Key: %s, Value: %s, TxID: %s", req.Key, req.Value, req.TxId)
	return &amberpb.Status{Success: true, Message: "OK"}, nil
}

func (s *server) Read(ctx context.Context, req *amberpb.ReadRequest) (*amberpb.ReadResponse, error) {
	// Get current HLC timestamp if none provided
	readTS := req.ReadTimestamp
	if readTS == "" {
		readTS = s.clock.Now()
	}

	// Read from store with timestamp
	value, err := s.store.ReadWithTimestamp(req.Key, readTS)
	if err != nil {
		log.Printf("Read error: %v", err)
		return nil, err
	}

	return &amberpb.ReadResponse{Value: value}, nil
}

func (s *server) Commit(ctx context.Context, req *amberpb.TxnID) (*amberpb.Status, error) {
	if !s.raftStore.IsLeader() {
		log.Printf("Commit rejected: not the leader")
		return &amberpb.Status{Success: false, Message: "not the leader"}, nil
	}

	// Replicate commit via Raft
	cmd := raftstore.Command{
		Op:   "COMMIT",
		TxID: req.Id,
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(cmd); err != nil {
		log.Printf("Encode commit error: %v", err)
		return &amberpb.Status{Success: false, Message: "encoding failed"}, nil
	}

	applyFuture := s.raftStore.Apply(buf.Bytes(), 5*time.Second)
	if err := applyFuture.Error(); err != nil {
		log.Printf("Raft commit error: %v", err)
		return &amberpb.Status{Success: false, Message: "raft apply failed"}, nil
	}

	return &amberpb.Status{Success: true, Message: "Committed"}, nil
}

func (s *server) Abort(ctx context.Context, req *amberpb.TxnID) (*amberpb.Status, error) {
	if !s.raftStore.IsLeader() {
		log.Printf("Abort rejected: not the leader")
		return &amberpb.Status{Success: false, Message: "not the leader"}, nil
	}
	// Replicate abort via Raft
	cmd := raftstore.Command{Op: "ABORT", TxID: req.Id}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(cmd); err != nil {
		log.Printf("Encode abort error: %v", err)
		return &amberpb.Status{Success: false, Message: "encoding failed"}, nil
	}
	applyFuture := s.raftStore.Apply(buf.Bytes(), 5*time.Second)
	if err := applyFuture.Error(); err != nil {
		log.Printf("Raft abort error: %v", err)
		return &amberpb.Status{Success: false, Message: "raft apply failed"}, nil
	}
	return &amberpb.Status{Success: true, Message: "Aborted"}, nil
}
