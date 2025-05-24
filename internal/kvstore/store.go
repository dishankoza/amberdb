// internal/kvstore/store.go
package kvstore

import (
	"database/sql"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

func NewStore(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	s := &Store{db: db}
	if err := s.initSchema(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) initSchema() error {
	query := `CREATE TABLE IF NOT EXISTS kv (
		key TEXT,
		value TEXT,
		timestamp TEXT,
		tx_id TEXT,
		is_committed BOOLEAN
	);`
	_, err := s.db.Exec(query)
	return err
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) BeginTransaction() string {
	return uuid.New().String()
}

func (s *Store) Write(key, value, txID string) error {
	timestamp := time.Now().Format(time.RFC3339Nano)
	query := `INSERT INTO kv (key, value, timestamp, tx_id, is_committed) VALUES (?, ?, ?, ?, false)`
	_, err := s.db.Exec(query, key, value, timestamp, txID)
	return err
}

func (s *Store) Read(key, readTimestamp string) (string, error) {
	query := `SELECT value FROM kv WHERE key = ? AND timestamp <= ? AND is_committed = true ORDER BY timestamp DESC LIMIT 1`
	row := s.db.QueryRow(query, key, readTimestamp)
	var value string
	err := row.Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

func (s *Store) Commit(txID string) error {
	query := `UPDATE kv SET is_committed = true WHERE tx_id = ?`
	_, err := s.db.Exec(query, txID)
	return err
}

func (s *Store) Abort(txID string) error {
	query := `DELETE FROM kv WHERE tx_id = ? AND is_committed = false`
	_, err := s.db.Exec(query, txID)
	return err
}
