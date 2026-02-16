package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps the SQL database connection
type DB struct {
	*sql.DB
}

// KeyValue represents a key-value pair in the store
type KeyValue struct {
	Key       string          `json:"key"`
	Value     json.RawMessage `json:"value"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// Message represents a chat message
type Message struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	Meta      json.RawMessage `json:"meta,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// MemoryChunk represents a chunk of text with its embedding
type MemoryChunk struct {
	ID        string          `json:"id"`
	Content   string          `json:"content"`
	Embedding []float32       `json:"embedding"` // Stored as JSON
	CreatedAt time.Time       `json:"created_at"`
	Source    string          `json:"source,omitempty"` // e.g., "conversation", "file:..."
}

// NewDB creates or opens the SQLite database
func NewDB(workspace string) (*DB, error) {
	dbPath := filepath.Join(workspace, "ghost.db")
	
	// Ensure workspace exists
	if err := os.MkdirAll(workspace, 0755); err != nil {
		return nil, fmt.Errorf("failed to create workspace: %w", err)
	}

	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)", dbPath)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := conn.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	db := &DB{conn}
	if err := db.initSchema(); err != nil {
		conn.Close()
		return nil, err
	}

	return db, nil
}

func (db *DB) initSchema() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS kv_store (
			key TEXT PRIMARY KEY,
			value JSON,
			updated_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS messages (
			id TEXT PRIMARY KEY,
			session_id TEXT,
			role TEXT,
			content TEXT,
			meta JSON,
			archived BOOLEAN DEFAULT FALSE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_session_id ON messages(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_messages_created_at ON messages(created_at)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			summary TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE TABLE IF NOT EXISTS memory_chunks (
			id TEXT PRIMARY KEY,
			content TEXT,
			embedding JSON,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			source TEXT
		)`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("failed to execute query %q: %w", query, err)
		}
	}

	return nil
}

// Close closes the database connection
func (db *DB) Close() error {
	return db.DB.Close()
}
