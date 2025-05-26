package metastore

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

type Shard struct {
	ID     string   `json:"id"`
	MinKey string   `json:"min_key"`
	MaxKey string   `json:"max_key"`
	Nodes  []string `json:"nodes"`
}

var (
	// configFile path can be overridden via SHARD_CONFIG_PATH env var
	configFile = func() string {
		if p := os.Getenv("SHARD_CONFIG_PATH"); p != "" {
			return p
		}
		return "internal/metastore/shard_config.json"
	}()
	mu sync.Mutex
)

// LoadShards reads the shard directory, initializing a default shard if none exists.
func LoadShards() ([]Shard, error) {
	mu.Lock()
	defer mu.Unlock()
	data, err := os.ReadFile(configFile)
	if err != nil {
		if os.IsNotExist(err) {
			// Initialize with one default shard covering all keys
			// Load peer addresses for nodes assignment
			peersPath := os.Getenv("RAFT_CONFIG_PATH")
			if peersPath == "" {
				peersPath = "internal/raftstore/raft_config.json"
			}
			peerData, pErr := os.ReadFile(peersPath)
			var nodes []string
			if pErr == nil {
				var peerCfg []struct {
					Address string `json:"address"`
				}
				_ = json.Unmarshal(peerData, &peerCfg)
				for _, pc := range peerCfg {
					nodes = append(nodes, pc.Address)
				}
			}
			defaultShard := Shard{ID: "shard1", MinKey: "", MaxKey: "", Nodes: nodes}
			shards := []Shard{defaultShard}
			if err := SaveShards(shards); err != nil {
				return nil, err
			}
			return shards, nil
		}
		return nil, err
	}
	var shards []Shard
	if err := json.Unmarshal(data, &shards); err != nil {
		return nil, err
	}
	// Populate missing Nodes from peer config
	for i, s := range shards {
		if len(s.Nodes) == 0 {
			// default to all peer addresses
			peersPath := os.Getenv("RAFT_CONFIG_PATH")
			if peersPath == "" {
				peersPath = "internal/raftstore/raft_config.json"
			}
			peerData, pErr := os.ReadFile(peersPath)
			if pErr == nil {
				var peerCfg []struct {
					Address string `json:"address"`
				}
				_ = json.Unmarshal(peerData, &peerCfg)
				for _, pc := range peerCfg {
					shards[i].Nodes = append(shards[i].Nodes, pc.Address)
				}
			}
		}
	}
	return shards, nil
}

// SaveShards writes the shard configuration to disk.
func SaveShards(shards []Shard) error {
	mu.Lock()
	defer mu.Unlock()
	data, err := json.MarshalIndent(shards, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configFile, data, 0644)
}

// SplitShard splits the given shard at splitKey into two new shards.
func SplitShard(id, splitKey string) ([]Shard, error) {
	shards, err := LoadShards()
	if err != nil {
		return nil, err
	}
	var newShards []Shard
	for _, s := range shards {
		if s.ID == id {
			// Validate splitKey in range
			if !(s.MinKey <= splitKey && (s.MaxKey == "" || splitKey < s.MaxKey)) {
				return nil, fmt.Errorf("splitKey %s out of range [%s, %s)", splitKey, s.MinKey, s.MaxKey)
			}
			// Create two halves
			s1 := Shard{ID: id + "_a", MinKey: s.MinKey, MaxKey: splitKey, Nodes: s.Nodes}
			s2 := Shard{ID: id + "_b", MinKey: splitKey, MaxKey: s.MaxKey, Nodes: s.Nodes}
			newShards = append(newShards, s1, s2)
		} else {
			newShards = append(newShards, s)
		}
	}
	if err := SaveShards(newShards); err != nil {
		return nil, err
	}
	return newShards, nil
}
