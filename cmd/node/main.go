// cmd/node/main.go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	"github.com/dishankoza/amberdb/internal/kvstore"
	"github.com/dishankoza/amberdb/internal/raftstore"
	"github.com/dishankoza/amberdb/internal/rpc"
	"github.com/hashicorp/raft"
)

type PeerConfig struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

func main() {
	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "data.db"
	}

	nodeID := os.Getenv("NODE_ID")
	if nodeID == "" {
		log.Fatal("NODE_ID env variable is required")
	}

	// Raft advertise address for other nodes to connect
	raftAdvertise := os.Getenv("RAFT_ADDR")
	if raftAdvertise == "" {
		log.Fatal("RAFT_ADDR env variable is required, e.g. node1:9001")
	}
	// Raft bind address for listening; default to 0.0.0.0:<port>
	raftBind := os.Getenv("RAFT_BIND_ADDR")
	log.Printf("RAFT_BIND_ADDR: %s", raftBind)
	if raftBind == "" {
		// extract port from advertise address
		_, port, err := net.SplitHostPort(raftAdvertise)
		if err != nil {
			log.Fatalf("invalid RAFT_ADDR: %v", err)
		}
		raftBind = "0.0.0.0:" + port
	}

	store, err := kvstore.NewStore(dbPath)
	if err != nil {
		log.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	fsm := raftstore.NewFSM(store)

	// Load Raft peers configuration
	configPath := os.Getenv("RAFT_CONFIG_PATH")
	if configPath == "" {
		configPath = "./internal/raftstore/raft_config.json"
	}
	peers := loadPeers(configPath)
	for _, p := range peers {
		log.Printf("Loaded peer: %s at %s", p.ID, p.Address)
	}
	raftServers := make([]raft.Server, 0, len(peers))
	for _, p := range peers {
		log.Printf("Configuring peer %s at %s", p.ID, p.Address)
		raftServers = append(raftServers, raft.Server{
			ID:       raft.ServerID(p.ID),
			Address:  raft.ServerAddress(p.Address),
			Suffrage: raft.Voter, // <-- ensure all peers are voters
		})
	}

	raftDataDir := filepath.Join("./raft-data", nodeID)
	raftNode, err := raftstore.NewRaftNode(raftDataDir, nodeID, raftAdvertise, raftBind, raftServers, fsm)
	if err != nil {
		log.Fatalf("failed to start raft node: %v", err)
	}
	_ = raftNode // currently unused, but will gate write logic later

	grpcServer := grpc.NewServer()
	rpc.RegisterAmberService(grpcServer, store, raftNode)
	reflection.Register(grpcServer)

	// configure gRPC port from environment
	port := os.Getenv("PORT")
	if port == "" {
		port = "50051"
	}

	lis, err := net.Listen("tcp", ":"+port)
	if err != nil {
		log.Fatalf("failed to listen: %v", err)
	}
	fmt.Printf("AmberDB Node %s running on port %s\n", nodeID, port)
	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("failed to serve: %v", err)
	}
}

func loadPeers(filename string) []PeerConfig {
	data, err := os.ReadFile(filename)
	if err != nil {
		log.Fatalf("failed to read raft config: %v", err)
	}
	var peers []PeerConfig
	if err := json.Unmarshal(data, &peers); err != nil {
		log.Fatalf("invalid raft config: %v", err)
	}
	return peers
}
