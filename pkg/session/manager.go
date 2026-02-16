package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/sipeed/picoclaw/pkg/db"
	"github.com/sipeed/picoclaw/pkg/providers"
	"github.com/sipeed/picoclaw/pkg/rag"
)

type Session struct {
	Key      string              `json:"key"`
	Messages []providers.Message `json:"messages"`
	Summary  string              `json:"summary,omitempty"`
	Created  time.Time           `json:"created"`
	Updated  time.Time           `json:"updated"`
}

type SessionManager struct {
	db  *db.DB
	rag *rag.Store
	mu  sync.RWMutex
}

func NewSessionManager(database *db.DB, ragStore *rag.Store) *SessionManager {
	return &SessionManager{
		db:  database,
		rag: ragStore,
	}
}

// ensureSession makes sure the session record exists
func (sm *SessionManager) ensureSession(key string) {
	sm.db.Exec(`INSERT OR IGNORE INTO sessions (id, created_at, updated_at) VALUES (?, ?, ?)`, key, time.Now(), time.Now())
}

func (sm *SessionManager) AddMessage(sessionKey, role, content string) {
	sm.AddFullMessage(sessionKey, providers.Message{
		Role:    role,
		Content: content,
	})
}

// AddFullMessage adds a complete message with tool calls and tool call ID to the session.
func (sm *SessionManager) AddFullMessage(sessionKey string, msg providers.Message) {
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

	sm.ensureSession(sessionKey)

	_, err := sm.db.Exec(`
		INSERT INTO messages (id, session_id, role, content, meta, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, id, sessionKey, msg.Role, content, metaJSON, time.Now())

	if err != nil {
		// In a real app, log this error
	}

	sm.db.Exec(`UPDATE sessions SET updated_at = ? WHERE id = ?`, time.Now(), sessionKey)
}

func (sm *SessionManager) GetHistory(key string) []providers.Message {
	rows, err := sm.db.Query(`
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

		// Handle multi-content
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

func (sm *SessionManager) GetSummary(key string) string {
	var summary sql.NullString
	err := sm.db.QueryRow(`SELECT summary FROM sessions WHERE id = ?`, key).Scan(&summary)
	if err != nil {
		return ""
	}
	if summary.Valid {
		return summary.String
	}
	return ""
}

func (sm *SessionManager) SetSummary(key string, summary string) {
	sm.ensureSession(key)
	sm.db.Exec(`UPDATE sessions SET summary = ?, updated_at = ? WHERE id = ?`, summary, time.Now(), key)
}

// TruncateHistory archives older messages, keeping the last `keepLast` messages.
func (sm *SessionManager) TruncateHistory(key string, keepLast int) {
	if keepLast <= 0 {
		// Archive all
		sm.db.Exec(`UPDATE messages SET archived = 1 WHERE session_id = ?`, key)
		return
	}

	// Find the timestamp of the Nth oldest message from the end
	// We want to keep `keepLast` messages, so we skip `keepLast` from the end and mark everything before that as archived.
	// SQLite: UPDATE messages SET archived=1 WHERE session_id=? AND id NOT IN (SELECT id FROM messages WHERE session_id=? ORDER BY created_at DESC LIMIT ?)
	
	sm.db.Exec(`
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

func (sm *SessionManager) Save(key string) error {
	// No-op for DB
	return nil
}

// GetContext retrieves relevant context for the current turn (RAG)
// This can be used by ContextBuilder to inject RAG context
func (sm *SessionManager) GetContext(ctx context.Context, userQuery string) string {
	if sm.rag == nil || userQuery == "" {
		return ""
	}
	results, err := sm.rag.Retrieve(ctx, userQuery, 3) // Top 3
	if err != nil || len(results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Relevant Context from Memory:\n")
	for _, r := range results {
		sb.WriteString("- " + r.Content + " (Source: " + r.Source + ")\n")
	}
	return sb.String()
}
