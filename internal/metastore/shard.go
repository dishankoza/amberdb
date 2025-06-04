package metastore

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
)

// Shard represents a range of keys and their assigned nodes
type Shard struct {
	ID      string   `json:"id"`
	MinKey  string   `json:"min_key"`
	MaxKey  string   `json:"max_key"`
	Nodes   []string `json:"nodes"`
	Primary string   `json:"primary"`
}

// ShardConfig represents the complete sharding configuration
type ShardConfig struct {
	Shards []Shard `json:"shards"`
	mu     sync.RWMutex
}

var (
	configPath = func() string {
		if p := os.Getenv("SHARD_CONFIG_PATH"); p != "" {
			return p
		}
		return "internal/metastore/shard_config.json"
	}()
	config *ShardConfig
	once   sync.Once
)

// InitShardConfig initializes the shard configuration
func InitShardConfig(path string) error {
	configPath = path
	return loadConfig()
}

// LoadShards returns the current shard configuration
func LoadShards() ([]Shard, error) {
	if config == nil {
		if err := loadConfig(); err != nil {
			return nil, err
		}
	}
	config.mu.RLock()
	defer config.mu.RUnlock()

	// If no shards exist, create a default shard
	if len(config.Shards) == 0 {
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
		if len(nodes) > 0 {
			defaultShard := Shard{
				ID:      "shard1",
				MinKey:  "",
				MaxKey:  "",
				Nodes:   nodes,
				Primary: nodes[0],
			}
			config.Shards = []Shard{defaultShard}
			if err := saveConfig(); err != nil {
				return nil, err
			}
		}
	}

	return config.Shards, nil
}

// GetShardForKey returns the shard responsible for a given key
func GetShardForKey(key string) (*Shard, error) {
	shards, err := LoadShards()
	if err != nil {
		return nil, err
	}

	// Find the shard containing this key
	for _, shard := range shards {
		if shard.MinKey <= key && (shard.MaxKey == "" || key < shard.MaxKey) {
			return &shard, nil
		}
	}

	return nil, fmt.Errorf("no shard found for key: %s", key)
}

// AddShard adds a new shard to the configuration
func AddShard(shard Shard) error {
	if config == nil {
		if err := loadConfig(); err != nil {
			return err
		}
	}

	config.mu.Lock()
	defer config.mu.Unlock()

	// Validate shard
	if shard.ID == "" {
		return fmt.Errorf("shard ID is required")
	}
	if len(shard.Nodes) == 0 {
		return fmt.Errorf("shard must have at least one node")
	}
	if shard.Primary == "" {
		shard.Primary = shard.Nodes[0]
	}

	// Validate shard boundaries
	for _, s := range config.Shards {
		if overlaps(s, shard) {
			return fmt.Errorf("new shard overlaps with existing shard %s", s.ID)
		}
	}

	config.Shards = append(config.Shards, shard)
	sort.Slice(config.Shards, func(i, j int) bool {
		return config.Shards[i].MinKey < config.Shards[j].MinKey
	})

	return saveConfig()
}

// RemoveShard removes a shard from the configuration
func RemoveShard(shardID string) error {
	if config == nil {
		if err := loadConfig(); err != nil {
			return err
		}
	}

	config.mu.Lock()
	defer config.mu.Unlock()

	for i, s := range config.Shards {
		if s.ID == shardID {
			config.Shards = append(config.Shards[:i], config.Shards[i+1:]...)
			return saveConfig()
		}
	}

	return fmt.Errorf("shard not found: %s", shardID)
}

// UpdateShard updates an existing shard's configuration
func UpdateShard(shard Shard) error {
	if config == nil {
		if err := loadConfig(); err != nil {
			return err
		}
	}

	config.mu.Lock()
	defer config.mu.Unlock()

	for i, s := range config.Shards {
		if s.ID == shard.ID {
			// Check for overlaps with other shards
			for j, other := range config.Shards {
				if i != j && overlaps(other, shard) {
					return fmt.Errorf("updated shard would overlap with shard %s", other.ID)
				}
			}
			config.Shards[i] = shard
			return saveConfig()
		}
	}

	return fmt.Errorf("shard not found: %s", shard.ID)
}

// SplitShard splits the given shard at splitKey into two new shards
func SplitShard(id, splitKey string) ([]Shard, error) {
	if config == nil {
		if err := loadConfig(); err != nil {
			return nil, err
		}
	}

	config.mu.Lock()
	defer config.mu.Unlock()

	var found *Shard
	var foundIdx int
	for i, s := range config.Shards {
		if s.ID == id {
			found = &s
			foundIdx = i
			break
		}
	}

	if found == nil {
		return nil, fmt.Errorf("shard not found: %s", id)
	}

	// Validate splitKey in range
	if !(found.MinKey <= splitKey && (found.MaxKey == "" || splitKey < found.MaxKey)) {
		return nil, fmt.Errorf("splitKey %s out of range [%s, %s)", splitKey, found.MinKey, found.MaxKey)
	}

	// Create two halves
	s1 := Shard{
		ID:      id + "_a",
		MinKey:  found.MinKey,
		MaxKey:  splitKey,
		Nodes:   found.Nodes,
		Primary: found.Primary,
	}
	s2 := Shard{
		ID:      id + "_b",
		MinKey:  splitKey,
		MaxKey:  found.MaxKey,
		Nodes:   found.Nodes,
		Primary: found.Primary,
	}

	// Replace the original shard with the two new ones
	newShards := make([]Shard, 0, len(config.Shards)+1)
	newShards = append(newShards, config.Shards[:foundIdx]...)
	newShards = append(newShards, s1, s2)
	newShards = append(newShards, config.Shards[foundIdx+1:]...)
	config.Shards = newShards

	if err := saveConfig(); err != nil {
		return nil, err
	}

	return []Shard{s1, s2}, nil
}

// SaveShards writes the shard configuration to disk
func SaveShards(shards []Shard) error {
	if config == nil {
		if err := loadConfig(); err != nil {
			return err
		}
	}

	config.mu.Lock()
	defer config.mu.Unlock()

	// Validate all shards
	for _, shard := range shards {
		if shard.ID == "" {
			return fmt.Errorf("shard ID is required")
		}
		if len(shard.Nodes) == 0 {
			return fmt.Errorf("shard %s must have at least one node", shard.ID)
		}
		if shard.Primary == "" {
			shard.Primary = shard.Nodes[0]
		}
	}

	// Check for overlaps
	for i, a := range shards {
		for j, b := range shards {
			if i != j && overlaps(a, b) {
				return fmt.Errorf("shard %s overlaps with shard %s", a.ID, b.ID)
			}
		}
	}

	config.Shards = shards
	return saveConfig()
}

// Helper functions

func loadConfig() error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Initialize empty config if file doesn't exist
			config = &ShardConfig{Shards: make([]Shard, 0)}
			return saveConfig()
		}
		return fmt.Errorf("failed to read config: %w", err)
	}

	var cfg ShardConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	config = &cfg
	return nil
}

func saveConfig() error {
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config: %w", err)
	}

	return nil
}

func overlaps(a, b Shard) bool {
	// Check if shard ranges overlap
	if a.MaxKey == "" {
		if b.MaxKey == "" {
			return a.MinKey <= b.MinKey
		}
		return a.MinKey < b.MaxKey
	}
	if b.MaxKey == "" {
		return b.MinKey < a.MaxKey
	}
	return a.MinKey < b.MaxKey && b.MinKey < a.MaxKey
}
