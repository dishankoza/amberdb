package metastore_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/dishankoza/amberdb/internal/metastore"
)

// writePeerConfig writes a raft peer config to a temp file and returns its path
func writePeerConfig(t *testing.T, peers interface{}) string {
	t.Helper()
	f, err := os.CreateTemp("", "peers-*.json")
	if err != nil {
		t.Fatalf("failed to create temp peers file: %v", err)
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	if err := enc.Encode(peers); err != nil {
		t.Fatalf("failed to write peers: %v", err)
	}
	return f.Name()
}

func TestLoadDefaultShard(t *testing.T) {
	// Setup temp shard config and peer config
	shardFile := filepath.Join(os.TempDir(), "shard_config.json")
	os.Remove(shardFile)
	// Mock peer config with two nodes
	peers := []struct {
		ID      string `json:"id"`
		Address string `json:"address"`
	}{
		{ID: "n1", Address: "addr1"},
		{ID: "n2", Address: "addr2"},
	}
	peerFile := writePeerConfig(t, peers)
	// Set env vars
	os.Setenv("SHARD_CONFIG_PATH", shardFile)
	os.Setenv("RAFT_CONFIG_PATH", peerFile)
	defer os.Remove(shardFile)
	// Load shards: should create default shard
	shards, err := metastore.LoadShards()
	if err != nil {
		t.Fatalf("LoadShards error: %v", err)
	}
	if len(shards) != 1 {
		t.Fatalf("expected 1 shard, got %d", len(shards))
	}
	s := shards[0]
	if s.ID != "shard1" {
		t.Errorf("expected shard ID shard1, got %s", s.ID)
	}
	if len(s.Nodes) != 2 || s.Nodes[0] != "addr1" || s.Nodes[1] != "addr2" {
		t.Errorf("expected nodes [addr1 addr2], got %v", s.Nodes)
	}
}

func TestSplitShard(t *testing.T) {
	// Setup initial shards config
	shardFile := filepath.Join(os.TempDir(), "shard_config.json")
	os.Remove(shardFile)
	s0 := metastore.Shard{ID: "s0", MinKey: "a", MaxKey: "z", Nodes: []string{"n1"}}
	// Save initial
	err := metastore.SaveShards([]metastore.Shard{s0})
	if err != nil {
		t.Fatalf("SaveShards error: %v", err)
	}
	os.Setenv("SHARD_CONFIG_PATH", shardFile)
	defer os.Remove(shardFile)

	// Split at key 'm'
	newShards, err := metastore.SplitShard("s0", "m")
	if err != nil {
		t.Fatalf("SplitShard error: %v", err)
	}
	// Expect two shards
	if len(newShards) != 2 {
		t.Fatalf("expected 2 shards after split, got %d", len(newShards))
	}
	// Check ranges
	if newShards[0].MinKey != "a" || newShards[0].MaxKey != "m" {
		t.Errorf("first shard range expected [a,m), got [%s,%s)", newShards[0].MinKey, newShards[0].MaxKey)
	}
	if newShards[1].MinKey != "m" || newShards[1].MaxKey != "z" {
		t.Errorf("second shard range expected [m,z), got [%s,%s)", newShards[1].MinKey, newShards[1].MaxKey)
	}
	// Nodes preserved
	for _, sh := range newShards {
		if len(sh.Nodes) != 1 || sh.Nodes[0] != "n1" {
			t.Errorf("expected nodes [n1], got %v for shard %s", sh.Nodes, sh.ID)
		}
	}
}
