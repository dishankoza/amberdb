// internal/rpc/server.go
package rpc

import (
	"bytes"
	"context"
	"encoding/gob"
	"log"
	"time"

	"github.com/dishankoza/amberdb/internal/kvstore"
	"github.com/dishankoza/amberdb/internal/raftstore"
	amberpb "github.com/dishankoza/amberdb/proto"
	"google.golang.org/grpc"
)

type server struct {
	amberpb.UnimplementedAmberServiceServer
	store     *kvstore.Store
	raftStore *raftstore.Store
}

func RegisterAmberService(grpcServer *grpc.Server, store *kvstore.Store, raftStore *raftstore.Store) {
	amberpb.RegisterAmberServiceServer(grpcServer, &server{store: store, raftStore: raftStore})
}

func (s *server) BeginTransaction(ctx context.Context, _ *amberpb.Empty) (*amberpb.TxnID, error) {
	txID := s.store.BeginTransaction()
	return &amberpb.TxnID{Id: txID}, nil
}

func (s *server) Write(ctx context.Context, req *amberpb.WriteRequest) (*amberpb.Status, error) {
	if !s.raftStore.IsLeader() {
		log.Printf("Write rejected: not the leader")
		return &amberpb.Status{Success: false, Message: "not the leader"}, nil
	}

	cmd := raftstore.Command{
		Key:   req.Key,
		Value: req.Value,
		TxID:  req.TxId,
	}

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(cmd); err != nil {
		log.Printf("Encode error: %v", err)
		return &amberpb.Status{Success: false, Message: "encoding failed"}, nil
	}

	applyFuture := s.raftStore.Apply(buf.Bytes(), 5*time.Second)
	if err := applyFuture.Error(); err != nil {
		log.Printf("Raft apply error: %v", err)
		return &amberpb.Status{Success: false, Message: "raft apply failed"}, nil
	}

	return &amberpb.Status{Success: true, Message: "OK"}, nil
}

func (s *server) Read(ctx context.Context, req *amberpb.ReadRequest) (*amberpb.ReadResponse, error) {
	val, err := s.store.Read(req.Key, req.ReadTimestamp)
	if err != nil {
		log.Printf("Read error: %v", err)
		return nil, err
	}
	return &amberpb.ReadResponse{Value: val}, nil
}

func (s *server) Commit(ctx context.Context, req *amberpb.TxnID) (*amberpb.Status, error) {
	err := s.store.Commit(req.Id)
	if err != nil {
		log.Printf("Commit error: %v", err)
		return &amberpb.Status{Success: false, Message: err.Error()}, nil
	}
	return &amberpb.Status{Success: true, Message: "Committed"}, nil
}

func (s *server) Abort(ctx context.Context, req *amberpb.TxnID) (*amberpb.Status, error) {
	err := s.store.Abort(req.Id)
	if err != nil {
		log.Printf("Abort error: %v", err)
		return &amberpb.Status{Success: false, Message: err.Error()}, nil
	}
	return &amberpb.Status{Success: true, Message: "Aborted"}, nil
}
