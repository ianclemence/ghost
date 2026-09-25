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
	now := time.Now().Format(time.RFC3339Nano)
	s.db.Exec(`INSERT OR IGNORE INTO sessions (id, created_at, updated_at) VALUES (?, ?, ?)`, key, now, now)
}

func (s *SQLiteStore) AddFullMessage(sessionKey string, msg providers.Message) {
	id := uuid.New().String()
	meta := map[string]interface{}{
		"tool_calls":   msg.ToolCalls,
		"tool_call_id": msg.ToolCallID,
	}
	if msg.SourceChannel != "" {
		meta["source_channel"] = msg.SourceChannel
	}
	metaJSON, _ := json.Marshal(meta)

	content := msg.Content
	if len(msg.MultiContent) > 0 {
		contentJSON, _ := json.Marshal(msg.MultiContent)
		content = string(contentJSON)
	}

	s.EnsureSession(sessionKey)
	now := time.Now().Format(time.RFC3339Nano)
	s.db.Exec(`
		INSERT INTO messages (id, session_id, role, content, meta, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, sessionKey, msg.Role, content, metaJSON, now)
	s.db.Exec(`UPDATE sessions SET updated_at = ? WHERE id = ?`, now, sessionKey)
}

// GetHistory returns the messages that go into the model's context. Rows
// compacted out (summarized away) and rows the owner deleted are excluded —
// this is what keeps the context window bounded.
func (s *SQLiteStore) GetHistory(key string) []providers.Message {
	return s.queryHistory(key, true)
}

// GetDisplayHistory returns the messages the owner sees in the transcript:
// deleted rows are excluded, but compacted rows are kept. Context compaction
// must never hide earlier turns from the owner, so the transcript stays whole
// even after the model's context has been summarized.
func (s *SQLiteStore) GetDisplayHistory(key string) []providers.Message {
	return s.queryHistory(key, false)
}

func (s *SQLiteStore) queryHistory(key string, excludeCompacted bool) []providers.Message {
	where := `session_id = ? AND (archived IS NULL OR archived = 0)`
	if excludeCompacted {
		where += ` AND (compacted IS NULL OR compacted = 0)`
	}
	rows, err := s.db.Query(`
		SELECT role, content, meta, created_at FROM messages 
		WHERE `+where+`
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
		var createdAt sql.NullString
		if err := rows.Scan(&role, &content, &metaJSON, &createdAt); err != nil {
			continue
		}

		var meta map[string]interface{}
		json.Unmarshal(metaJSON, &meta)

		msg := providers.Message{
			Role:    role,
			Content: content,
		}
		// When the row was written, so context building can date-stamp
		// history for the model (relative words like "tomorrow" are only
		// resolvable against the moment they were said). Unparseable or
		// missing timestamps leave the field zero — the stamp is then
		// simply skipped.
		if createdAt.Valid {
			if t, err := time.Parse(time.RFC3339Nano, createdAt.String); err == nil {
				msg.CreatedAt = t
			}
		}

		if val, ok := meta["tool_call_id"].(string); ok {
			msg.ToolCallID = val
		}
		if val, ok := meta["source_channel"].(string); ok {
			msg.SourceChannel = val
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
	now := time.Now().Format(time.RFC3339Nano)
	s.db.Exec(`UPDATE sessions SET summary = ?, updated_at = ? WHERE id = ?`, summary, now, key)
}

func (s *SQLiteStore) GetTitle(key string) string {
	var title sql.NullString
	err := s.db.QueryRow(`SELECT title FROM sessions WHERE id = ?`, key).Scan(&title)
	if err != nil {
		return ""
	}
	if title.Valid {
		return title.String
	}
	return ""
}

func (s *SQLiteStore) SetTitle(key string, title string) {
	s.EnsureSession(key)
	now := time.Now().Format(time.RFC3339Nano)
	s.db.Exec(`UPDATE sessions SET title = ?, updated_at = ? WHERE id = ?`, title, now, key)
}

// TruncateHistory drops all but the last keepLast messages from the model's
// context by marking them compacted. They are NOT archived: the owner's
// transcript keeps showing them, so compaction never makes earlier turns
// disappear from the terminal or the app.
func (s *SQLiteStore) TruncateHistory(key string, keepLast int) {
	if keepLast <= 0 {
		s.db.Exec(`UPDATE messages SET compacted = 1 WHERE session_id = ?`, key)
		return
	}
	s.db.Exec(`
		UPDATE messages 
		SET compacted = 1 
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

// DeleteSession removes all messages and the session row for key, including
// archived messages and the FTS index (via the messages_ad trigger).
func (s *SQLiteStore) DeleteSession(key string) error {
	if _, err := s.db.Exec(`DELETE FROM messages WHERE session_id = ?`, key); err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, key); err != nil {
		return err
	}
	return nil
}
