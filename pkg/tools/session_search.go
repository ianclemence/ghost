package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

type SessionSearchTool struct {
	db *sql.DB
}

type SessionSearchResult struct {
	SessionID string  `json:"session_id"`
	Content   string  `json:"content"`
	Timestamp int64   `json:"timestamp"`
	Rank      float64 `json:"rank"`
}

func NewSessionSearchTool(database *sql.DB) *SessionSearchTool {
	return &SessionSearchTool{db: database}
}

func (t *SessionSearchTool) Name() string {
	return "session_search"
}

func (t *SessionSearchTool) Description() string {
	return "Search ranked message history using full-text matching across sessions."
}

func (t *SessionSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query to match against message content",
			},
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Optional session filter; empty searches all sessions",
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum number of results (default 10, max 50)",
				"default":     10,
			},
		},
		"required": []string{"query"},
	}
}

func (t *SessionSearchTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	if t.db == nil {
		return ErrorResult("session_search unavailable: database not configured")
	}

	query, ok := args["query"].(string)
	if !ok || query == "" {
		return ErrorResult("query is required")
	}

	sessionID, _ := args["session_id"].(string)
	limit := 10
	if raw, ok := args["limit"].(float64); ok {
		limit = int(raw)
	}
	if raw, ok := args["limit"].(int); ok {
		limit = raw
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	sqlQuery := `
		SELECT
			m.session_id,
			snippet(messages_fts, 0, '[', ']', '…', 32) AS content,
			COALESCE(unixepoch(m.created_at), 0) AS ts,
			bm25(messages_fts) AS rank
		FROM messages_fts
		JOIN messages m ON m.rowid = messages_fts.rowid
		WHERE messages_fts MATCH ?
		  AND (m.archived IS NULL OR m.archived = 0)
		  AND (? = '' OR m.session_id = ?)
		ORDER BY rank
		LIMIT ?
	`

	rows, err := t.db.QueryContext(ctx, sqlQuery, query, sessionID, sessionID, limit)
	if err != nil {
		return ErrorResult(fmt.Sprintf("session_search query failed: %v", err)).WithError(err)
	}
	defer rows.Close()

	results := make([]SessionSearchResult, 0, limit)
	for rows.Next() {
		var r SessionSearchResult
		if err := rows.Scan(&r.SessionID, &r.Content, &r.Timestamp, &r.Rank); err != nil {
			return ErrorResult(fmt.Sprintf("session_search scan failed: %v", err)).WithError(err)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return ErrorResult(fmt.Sprintf("session_search failed: %v", err)).WithError(err)
	}

	payload := map[string]interface{}{
		"query":      query,
		"session_id": sessionID,
		"count":      len(results),
		"results":    results,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ErrorResult(fmt.Sprintf("session_search marshal failed: %v", err)).WithError(err)
	}

	return UserResult(string(raw))
}
