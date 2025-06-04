package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/dishankoza/amberdb/internal/coordinator"
	"github.com/dishankoza/amberdb/internal/metastore"
	amberpb "github.com/dishankoza/amberdb/proto"
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

	// Initialize coordinator with 5 second timeout
	coordinator := coordinator.NewCoordinator(5 * time.Second)

	mux := http.NewServeMux()

	// Add coordinator endpoints
	mux.HandleFunc("/transaction/begin", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		txID := coordinator.BeginTransaction()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"transaction_id": txID})
	})

	mux.HandleFunc("/transaction/prepare", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			TransactionID string                           `json:"transaction_id"`
			Writes        map[string]*amberpb.WriteRequest `json:"writes"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, fmt.Sprintf("invalid request: %v", err), http.StatusBadRequest)
			return
		}

		if req.TransactionID == "" {
			http.Error(w, "transaction_id is required", http.StatusBadRequest)
			return
		}

		if len(req.Writes) == 0 {
			http.Error(w, "writes are required", http.StatusBadRequest)
			return
		}

		if err := coordinator.PrepareTransaction(r.Context(), req.TransactionID, req.Writes); err != nil {
			if err.Error() == "transaction not found" {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/transaction/commit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			TransactionID string `json:"transaction_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		if err := coordinator.CommitTransaction(r.Context(), req.TransactionID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		defer coordinator.CleanupTransaction(req.TransactionID)
	})

	mux.HandleFunc("/transaction/abort", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			TransactionID string `json:"transaction_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}

		if err := coordinator.AbortTransaction(r.Context(), req.TransactionID); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		defer coordinator.CleanupTransaction(req.TransactionID)
	})

	mux.HandleFunc("/transaction/status", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		txID := r.URL.Query().Get("transaction_id")
		if txID == "" {
			http.Error(w, "transaction_id required", http.StatusBadRequest)
			return
		}

		state, err := coordinator.GetTransactionState(txID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}

		json.NewEncoder(w).Encode(map[string]interface{}{
			"transaction_id": txID,
			"state":          state,
		})
	})

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
			shards, err := metastore.LoadShards()
			if err != nil {
				http.Error(w, fmt.Sprintf("failed to load shards: %v", err), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(shards); err != nil {
				log.Printf("Error encoding shards response: %v", err)
			}

		case http.MethodPost:
			var shards []metastore.Shard
			if err := json.NewDecoder(r.Body).Decode(&shards); err != nil {
				http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
				return
			}

			// Validate shards
			for _, shard := range shards {
				if shard.ID == "" {
					http.Error(w, "shard ID is required", http.StatusBadRequest)
					return
				}
				if len(shard.Nodes) == 0 {
					http.Error(w, fmt.Sprintf("shard %s must have at least one node", shard.ID), http.StatusBadRequest)
					return
				}
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
			http.Error(w, "key parameter is required", http.StatusBadRequest)
			return
		}

		shards, err := metastore.LoadShards()
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to load shards: %v", err), http.StatusInternalServerError)
			return
		}

		var found metastore.Shard
		for _, s := range shards {
			if s.MinKey <= key && (s.MaxKey == "" || key < s.MaxKey) {
				found = s
				break
			}
		}

		if found.ID == "" {
			http.Error(w, fmt.Sprintf("no shard found for key: %s", key), http.StatusNotFound)
			return
		}

		if len(found.Nodes) == 0 {
			http.Error(w, fmt.Sprintf("shard %s has no nodes", found.ID), http.StatusInternalServerError)
			return
		}

		resp := RouteResponse{ShardID: found.ID, Nodes: found.Nodes}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("Error encoding route response: %v", err)
		}
	})

	log.Printf("MetaService running on port %s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
