package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/dishankoza/amberdb/internal/metastore"
	amberpb "github.com/dishankoza/amberdb/proto"
	"google.golang.org/grpc"
)

type PeerConfig struct {
	ID      string `json:"id"`
	Address string `json:"address"`
}

// RouteResponse gives shard and node addresses for a key
type RouteResponse struct {
	ShardID string   `json:"shard_id"`
	Nodes   []string `json:"nodes"`
}

var (
	configFile = "internal/raftstore/raft_config.json"
	mu         sync.Mutex
)

func loadPeers() ([]PeerConfig, error) {
	data, err := os.ReadFile(configFile)
	if err != nil {
		return nil, err
	}
	var peers []PeerConfig
	if err := json.Unmarshal(data, &peers); err != nil {
		return nil, err
	}
	return peers, nil
}

func savePeers(peers []PeerConfig) error {
	data, err := json.MarshalIndent(peers, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configFile, data, 0644)
}

func getPeersHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()
	peers, err := loadPeers()
	if err != nil {
		http.Error(w, "failed to load peers", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(peers)
}

func updatePeersHandler(w http.ResponseWriter, r *http.Request) {
	mu.Lock()
	defer mu.Unlock()
	var peers []PeerConfig
	if err := json.NewDecoder(r.Body).Decode(&peers); err != nil {
		http.Error(w, "invalid payload", http.StatusBadRequest)
		return
	}
	if err := savePeers(peers); err != nil {
		http.Error(w, "failed to save peers", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func main() {
	port := os.Getenv("META_PORT")
	if port == "" {
		port = "8080"
	}
	mux := http.NewServeMux()

	// Existing peers handler
	mux.HandleFunc("/peers", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			getPeersHandler(w, r)
		case http.MethodPost:
			updatePeersHandler(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Shards: list and update
	mux.HandleFunc("/shards", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			// List shards
			shards, err := metastore.LoadShards()
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to load shards: %v", err), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(shards)
		case http.MethodPost:
			// Update entire shard list
			var shards []metastore.Shard
			if err := json.NewDecoder(r.Body).Decode(&shards); err != nil {
				http.Error(w, "invalid payload", http.StatusBadRequest)
				return
			}
			if err := metastore.SaveShards(shards); err != nil {
				http.Error(w, fmt.Sprintf("failed to save shards: %v", err), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	// Split a shard at given key: POST /shards/split
	mux.HandleFunc("/shards/split", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			ID       string `json:"id"`
			SplitKey string `json:"split_key"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		newShards, err := metastore.SplitShard(req.ID, req.SplitKey)
		if err != nil {
			http.Error(w, fmt.Sprintf("split error: %v", err), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(newShards)
	})

	// Routing: map key to shard
	mux.HandleFunc("/route", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		key := r.URL.Query().Get("key")
		if key == "" {
			http.Error(w, "missing key parameter", http.StatusBadRequest)
			return
		}
		// Load shards
		shards, err := metastore.LoadShards()
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to load shards: %v", err), http.StatusInternalServerError)
			return
		}
		// Find shard range
		var found metastore.Shard
		for _, s := range shards {
			if s.MinKey <= key && (s.MaxKey == "" || key < s.MaxKey) {
				found = s
				break
			}
		}
		if found.ID == "" {
			http.Error(w, "no shard for key", http.StatusNotFound)
			return
		}
		// Return shard and nodes
		resp := RouteResponse{ShardID: found.ID, Nodes: found.Nodes}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(resp)
	})

	// 2PC: cross-shard atomic writes
	mux.HandleFunc("/2pc", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct{ Writes []struct{ Key, Value string } }
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid payload", http.StatusBadRequest)
			return
		}
		// Map writes per node
		shards, _ := metastore.LoadShards()
		writesByNode := make(map[string][]struct{ Key, Value string })
		for _, wreq := range req.Writes {
			for _, s := range shards {
				if s.MinKey <= wreq.Key && (s.MaxKey == "" || wreq.Key < s.MaxKey) {
					writesByNode[s.Nodes[0]] = append(writesByNode[s.Nodes[0]], struct{ Key, Value string }{wreq.Key, wreq.Value})
					break
				}
			}
		}
		// Dial and begin tx per node
		txnIDs := make(map[string]string)
		dialConns := make(map[string]*grpc.ClientConn)
		for addr := range writesByNode {
			conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithBlock(), grpc.WithTimeout(3*time.Second))
			if err != nil {
				http.Error(w, fmt.Sprintf("dial error %s: %v", addr, err), http.StatusInternalServerError)
				return
			}
			dialConns[addr] = conn
			client := amberpb.NewAmberServiceClient(conn)
			// Prepare phase: begin tx and writes
			resp, err := client.BeginTransaction(context.Background(), &amberpb.Empty{})
			if err != nil {
				http.Error(w, fmt.Sprintf("begin tx failed %s: %v", addr, err), http.StatusInternalServerError)
				return
			}
			txnIDs[addr] = resp.Id
		}
		// Fan-out prepare (writes)
		for addr, writes := range writesByNode {
			client := amberpb.NewAmberServiceClient(dialConns[addr])
			for _, wr := range writes {
				st, err := client.Write(context.Background(), &amberpb.WriteRequest{Key: wr.Key, Value: wr.Value, TxId: txnIDs[addr]})
				if err != nil || !st.Success {
					// Abort on all nodes
					for a, tx := range txnIDs {
						rpcClient := amberpb.NewAmberServiceClient(dialConns[a])
						rpcClient.Abort(context.Background(), &amberpb.TxnID{Id: tx})
					}
					http.Error(w, fmt.Sprintf("prepare failed on %s: %v %v", addr, err, st), http.StatusInternalServerError)
					return
				}
			}
		}
		// Commit phase
		for addr, tx := range txnIDs {
			client := amberpb.NewAmberServiceClient(dialConns[addr])
			st, err := client.Commit(context.Background(), &amberpb.TxnID{Id: tx})
			if err != nil || !st.Success {
				http.Error(w, fmt.Sprintf("commit failed on %s: %v %v", addr, err, st), http.StatusInternalServerError)
				return
			}
		}
		// Cleanup
		for _, conn := range dialConns {
			conn.Close()
		}
		json.NewEncoder(w).Encode(map[string]bool{"success": true})
	})

	log.Printf("MetaService running on port %s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
