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
	configPath string
	config     *ShardConfig
	once       sync.Once
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
