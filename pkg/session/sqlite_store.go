package session

import (
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/providers"
)

type SQLiteStore struct {
	db *db.DB
}

func NewSQLiteStore(database *db.DB) *SQLiteStore {
	return &SQLiteStore{db: database}
}

func (s *SQLiteStore) DB() *sql.DB {
	return s.db.DB
}

func (s *SQLiteStore) EnsureSession(key string) {
	s.db.Exec(`INSERT OR IGNORE INTO sessions (id, created_at, updated_at) VALUES (?, ?, ?)`, key, time.Now(), time.Now())
}

func (s *SQLiteStore) AddFullMessage(sessionKey string, msg providers.Message) {
	id := uuid.New().String()
	meta := map[string]interface{}{
		"tool_calls":   msg.ToolCalls,
		"tool_call_id": msg.ToolCallID,
	}
	metaJSON, _ := json.Marshal(meta)

	content := msg.Content
	if len(msg.MultiContent) > 0 {
		contentJSON, _ := json.Marshal(msg.MultiContent)
		content = string(contentJSON)
	}

	s.EnsureSession(sessionKey)
	s.db.Exec(`
		INSERT INTO messages (id, session_id, role, content, meta, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, sessionKey, msg.Role, content, metaJSON, time.Now())
	s.db.Exec(`UPDATE sessions SET updated_at = ? WHERE id = ?`, time.Now(), sessionKey)
}

func (s *SQLiteStore) GetHistory(key string) []providers.Message {
	rows, err := s.db.Query(`
		SELECT role, content, meta FROM messages 
		WHERE session_id = ? AND (archived IS NULL OR archived = 0)
		ORDER BY created_at ASC
	`, key)
	if err != nil {
		return []providers.Message{}
	}
	defer rows.Close()

	var history []providers.Message
	for rows.Next() {
		var role, content string
		var metaJSON []byte
		if err := rows.Scan(&role, &content, &metaJSON); err != nil {
			continue
		}

		var meta map[string]interface{}
		json.Unmarshal(metaJSON, &meta)

		msg := providers.Message{
			Role:    role,
			Content: content,
		}

		if val, ok := meta["tool_call_id"].(string); ok {
			msg.ToolCallID = val
		}

		if tc, ok := meta["tool_calls"]; ok {
			tcJSON, _ := json.Marshal(tc)
			var toolCalls []providers.ToolCall
			json.Unmarshal(tcJSON, &toolCalls)
			msg.ToolCalls = toolCalls
		}

		if strings.HasPrefix(content, "[") {
			var parts []providers.ContentPart
			if err := json.Unmarshal([]byte(content), &parts); err == nil {
				msg.MultiContent = parts
				msg.Content = ""
			}
		}

		history = append(history, msg)
	}
	return history
}

func (s *SQLiteStore) GetSummary(key string) string {
	var summary sql.NullString
	err := s.db.QueryRow(`SELECT summary FROM sessions WHERE id = ?`, key).Scan(&summary)
	if err != nil {
		return ""
	}
	if summary.Valid {
		return summary.String
	}
	return ""
}

func (s *SQLiteStore) SetSummary(key string, summary string) {
	s.EnsureSession(key)
	s.db.Exec(`UPDATE sessions SET summary = ?, updated_at = ? WHERE id = ?`, summary, time.Now(), key)
}

func (s *SQLiteStore) TruncateHistory(key string, keepLast int) {
	if keepLast <= 0 {
		s.db.Exec(`UPDATE messages SET archived = 1 WHERE session_id = ?`, key)
		return
	}
	s.db.Exec(`
		UPDATE messages 
		SET archived = 1 
		WHERE session_id = ? 
		AND id NOT IN (
			SELECT id FROM messages 
			WHERE session_id = ? 
			ORDER BY created_at DESC 
			LIMIT ?
		)
	`, key, key, keepLast)
}

func (s *SQLiteStore) SetHistory(key string, messages []providers.Message) {
	s.db.Exec(`DELETE FROM messages WHERE session_id = ?`, key)
	for _, msg := range messages {
		s.AddFullMessage(key, msg)
	}
}

func (s *SQLiteStore) Save(key string) error {
	return nil
}
