// internal/rpc/server.go
package rpc

import (
	"context"
	"log"

	"github.com/dishankoza/amberdb/internal/kvstore"
	"github.com/dishankoza/amberdb/proto"
	"google.golang.org/grpc"
)

type server struct {
	proto.UnimplementedAmberServiceServer
	store *kvstore.Store
}

func RegisterAmberService(grpcServer *grpc.Server, store *kvstore.Store) {
	proto.RegisterAmberServiceServer(grpcServer, &server{store: store})
}

func (s *server) BeginTransaction(ctx context.Context, _ *proto.Empty) (*proto.TxnID, error) {
	txID := s.store.BeginTransaction()
	return &proto.TxnID{Id: txID}, nil
}

func (s *server) Write(ctx context.Context, req *proto.WriteRequest) (*proto.Status, error) {
	err := s.store.Write(req.Key, req.Value, req.TxId)
	if err != nil {
		log.Printf("Write error: %v", err)
		return &proto.Status{Success: false, Message: err.Error()}, nil
	}
	return &proto.Status{Success: true, Message: "OK"}, nil
}

func (s *server) Read(ctx context.Context, req *proto.ReadRequest) (*proto.ReadResponse, error) {
	val, err := s.store.Read(req.Key, req.ReadTimestamp)
	if err != nil {
		log.Printf("Read error: %v", err)
		return nil, err
	}
	return &proto.ReadResponse{Value: val}, nil
}

func (s *server) Commit(ctx context.Context, req *proto.TxnID) (*proto.Status, error) {
	err := s.store.Commit(req.Id)
	if err != nil {
		log.Printf("Commit error: %v", err)
		return &proto.Status{Success: false, Message: err.Error()}, nil
	}
	return &proto.Status{Success: true, Message: "Committed"}, nil
}

func (s *server) Abort(ctx context.Context, req *proto.TxnID) (*proto.Status, error) {
	err := s.store.Abort(req.Id)
	if err != nil {
		log.Printf("Abort error: %v", err)
		return &proto.Status{Success: false, Message: err.Error()}, nil
	}
	return &proto.Status{Success: true, Message: "Aborted"}, nil
}
