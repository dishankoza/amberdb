package kvstore

import (
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db    *sql.DB
	mutex sync.RWMutex
}

func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Validate connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	s := &Store{db: db}

	// Now safely initialize schema
	if err := s.initSchema(); err != nil {
		return nil, fmt.Errorf("failed to init schema: %w", err)
	}

	return s, nil
}

func (s *Store) initSchema() error {
	if s.db == nil {
		return fmt.Errorf("db connection is nil in initSchema")
	}

	query := `
	CREATE TABLE IF NOT EXISTS kv_store (
		key TEXT,
		value TEXT,
		timestamp TEXT,
		tx_id TEXT,
		is_committed BOOLEAN
	);
	CREATE INDEX IF NOT EXISTS idx_timestamp ON kv_store(timestamp);
	CREATE INDEX IF NOT EXISTS idx_txid ON kv_store(tx_id);
	`
	_, err := s.db.Exec(query)
	return err
}

func (s *Store) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

func (s *Store) BeginTransaction() string {
	return uuid.New().String()
}

// WriteWithTimestamp writes a versioned value using the provided timestamp (for HLC ordering)
func (s *Store) WriteWithTimestamp(key, value, txID, timestamp string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	_, err := s.db.Exec(
		"INSERT INTO kv_store (key, value, timestamp, tx_id, is_committed) VALUES (?, ?, ?, ?, false)",
		key, value, timestamp, txID,
	)
	if err != nil {
		return fmt.Errorf("failed to write: %w", err)
	}
	return nil
}

// Write is maintained for compatibility but uses system time
func (s *Store) Write(key, value, txID string) error {
	now := time.Now().Format(time.RFC3339Nano)
	return s.WriteWithTimestamp(key, value, txID, now)
}

func (s *Store) Read(key, readTimestamp string) (string, error) {
	query := `SELECT value FROM kv_store WHERE key = ? AND timestamp <= ? AND is_committed = true ORDER BY timestamp DESC LIMIT 1`
	row := s.db.QueryRow(query, key, readTimestamp)
	var value string
	err := row.Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (s *Store) ReadWithTimestamp(key, readTimestamp string) (string, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	var value string
	err := s.db.QueryRow(`
		SELECT value FROM kv_store 
		WHERE key = ? 
		AND timestamp <= ? 
		AND is_committed = true
		ORDER BY timestamp DESC 
		LIMIT 1`,
		key, readTimestamp,
	).Scan(&value)

	if err == sql.ErrNoRows {
		return "", fmt.Errorf("key not found: %s", key)
	}
	if err != nil {
		return "", fmt.Errorf("failed to read: %w", err)
	}
	return value, nil
}

func (s *Store) Commit(txID string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	_, err := s.db.Exec(
		"UPDATE kv_store SET is_committed = true WHERE tx_id = ?",
		txID,
	)
	if err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}
	return nil
}

func (s *Store) Abort(txID string) error {
	query := `DELETE FROM kv_store WHERE tx_id = ? AND is_committed = false`
	_, err := s.db.Exec(query, txID)
	return err
}

// GetDB returns the underlying database connection
func (s *Store) GetDB() *sql.DB {
	return s.db
}

// Lock acquires the store's write lock
func (s *Store) Lock() {
	s.mutex.Lock()
}

// Unlock releases the store's write lock
func (s *Store) Unlock() {
	s.mutex.Unlock()
}

// RLock acquires the store's read lock
func (s *Store) RLock() {
	s.mutex.RLock()
}

// RUnlock releases the store's read lock
func (s *Store) RUnlock() {
	s.mutex.RUnlock()
}
