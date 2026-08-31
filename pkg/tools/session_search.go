package tools

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
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

type BrowseResult struct {
	SessionID string `json:"session_id"`
	Summary   string `json:"summary,omitempty"`
	Preview   string `json:"preview,omitempty"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type ScrollResult struct {
	ID        int64  `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	Timestamp string `json:"timestamp"`
}

type ReadResult struct {
	ID        int64  `json:"id"`
	Role      string `json:"role"`
	Content   string `json:"content"`
	ToolCalls string `json:"tool_calls,omitempty"`
	Timestamp string `json:"timestamp"`
}

func NewSessionSearchTool(database *sql.DB) *SessionSearchTool {
	return &SessionSearchTool{db: database}
}

func (t *SessionSearchTool) Name() string {
	return "session_search"
}

func (t *SessionSearchTool) Description() string {
	return "Search Ghost's past conversation history. Use ONLY when the user asks about something from a previous conversation (\"what did I say about X\", \"what we discussed\"). Do NOT use it for general knowledge, facts, weather, currency, or skills — use a matching Ghost skill or the general search instead."
}

func (t *SessionSearchTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "Search query (required for discover mode)",
			},
			"mode": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"discover", "browse", "scroll", "read", "summarize"},
				"description": "Search mode (default: discover)",
				"default":     "discover",
			},
			"session_id": map[string]interface{}{
				"type":        "string",
				"description": "Session ID filter (discover, scroll, read modes)",
			},
			"around_message_id": map[string]interface{}{
				"type":        "integer",
				"description": "Message ID to anchor scroll mode (requires session_id)",
			},
			"window": map[string]interface{}{
				"type":        "integer",
				"description": "Messages before/after anchor in scroll mode (default 5, max 20)",
				"default":     5,
			},
			"limit": map[string]interface{}{
				"type":        "integer",
				"description": "Maximum results (default 10, max 50)",
				"default":     10,
			},
			"sort": map[string]interface{}{
				"type":        "string",
				"enum":        []string{"relevance", "newest", "oldest"},
				"description": "Sort order for discover mode (default: relevance)",
				"default":     "relevance",
			},
		},
		"required": []string{},
	}
}

func (t *SessionSearchTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	if t.db == nil {
		return ErrorResult("session_search unavailable: database not configured")
	}

	mode, _ := args["mode"].(string)
	if mode == "" {
		mode = "discover"
	}

	switch mode {
	case "browse":
		return t.browse(ctx, args)
	case "scroll":
		return t.scroll(ctx, args)
	case "read":
		return t.readSession(ctx, args)
	case "summarize":
		return t.summarize(ctx, args)
	case "discover":
		return t.discover(ctx, args)
	default:
		return ErrorResult(fmt.Sprintf("unknown mode: %s", mode))
	}
}

func (t *SessionSearchTool) discover(ctx context.Context, args map[string]interface{}) *ToolResult {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return ErrorResult("query is required for discover mode")
	}

	sessionID, _ := args["session_id"].(string)
	limit := 10
	if raw, ok := args["limit"].(float64); ok {
		limit = int(raw)
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	sort, _ := args["sort"].(string)
	orderClause := "ORDER BY rank"
	switch sort {
	case "newest":
		orderClause = "ORDER BY ts DESC, rank"
	case "oldest":
		orderClause = "ORDER BY ts ASC, rank"
	}

	sqlQuery := fmt.Sprintf(`
		SELECT
			m.session_id,
			snippet(messages_fts, 0, '[', ']', '...', 32) AS content,
			COALESCE(unixepoch(m.created_at), 0) AS ts,
			bm25(messages_fts) AS rank
		FROM messages_fts
		JOIN messages m ON m.rowid = messages_fts.rowid
		WHERE messages_fts MATCH ?
		  AND (m.archived IS NULL OR m.archived = 0)
		  AND (? = '' OR m.session_id = ?)
		%s
		LIMIT ?
	`, orderClause)

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
		"mode":       "discover",
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

// summarize groups FTS search results by session and returns a compact
// digest. The LLM reads this and produces a cross-session recall summary,
// mirroring Hermes's "search + LLM summarization for cross-session recall".
func (t *SessionSearchTool) summarize(ctx context.Context, args map[string]interface{}) *ToolResult {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return ErrorResult("query is required for summarize mode")
	}

	limit := 20
	if raw, ok := args["limit"].(float64); ok {
		limit = int(raw)
	}
	if limit <= 0 {
		limit = 20
	}
	if limit > 50 {
		limit = 50
	}

	rows, err := t.db.QueryContext(ctx, `
		SELECT m.session_id, m.id, m.role, m.content, COALESCE(unixepoch(m.created_at), 0)
		FROM messages_fts
		JOIN messages m ON m.rowid = messages_fts.rowid
		WHERE messages_fts MATCH ?
		  AND (m.archived IS NULL OR m.archived = 0)
		ORDER BY bm25(messages_fts)
		LIMIT ?
	`, query, limit)
	if err != nil {
		return ErrorResult(fmt.Sprintf("session_search summarize query failed: %v", err)).WithError(err)
	}
	defer rows.Close()

	// Group matches by session, keeping the best few per session.
	grouped := map[string][]string{}
	order := []string{}
	for rows.Next() {
		var sid, id, role, content string
		var ts int64
		if err := rows.Scan(&sid, &id, &role, &content, &ts); err != nil {
			return ErrorResult(fmt.Sprintf("session_search summarize scan failed: %v", err)).WithError(err)
		}
		if _, exists := grouped[sid]; !exists {
			order = append(order, sid)
		}
		if len(grouped[sid]) < 3 {
			grouped[sid] = append(grouped[sid], role+": "+strings.TrimSpace(content))
		}
	}
	if err := rows.Err(); err != nil {
		return ErrorResult(fmt.Sprintf("session_search summarize failed: %v", err)).WithError(err)
	}

	type sessionDigest struct {
		SessionID string   `json:"session_id"`
		Messages  []string `json:"messages"`
	}
	digests := make([]sessionDigest, 0, len(order))
	for _, sid := range order {
		digests = append(digests, sessionDigest{SessionID: sid, Messages: grouped[sid]})
	}

	payload := map[string]interface{}{
		"mode":        "summarize",
		"query":       query,
		"count":       len(digests),
		"sessions":    digests,
		"instruction": "Synthesize these matches into a concise recall summary answering the query.",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ErrorResult(fmt.Sprintf("session_search summarize marshal failed: %v", err)).WithError(err)
	}
	return UserResult(string(raw))
}

func (t *SessionSearchTool) browse(ctx context.Context, args map[string]interface{}) *ToolResult {
	limit := 10
	if raw, ok := args["limit"].(float64); ok {
		limit = int(raw)
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}

	sqlQuery := `
		SELECT
			s.id AS session_id,
			COALESCE(s.summary, '') AS summary,
			(SELECT content FROM messages WHERE session_id = s.id AND role = 'user' LIMIT 1) AS preview,
			COALESCE(s.created_at, '') AS created_at,
			COALESCE(s.updated_at, '') AS updated_at
		FROM sessions s
		ORDER BY s.updated_at DESC
		LIMIT ?
	`

	rows, err := t.db.QueryContext(ctx, sqlQuery, limit)
	if err != nil {
		return ErrorResult(fmt.Sprintf("session_search browse failed: %v", err)).WithError(err)
	}
	defer rows.Close()

	results := make([]BrowseResult, 0, limit)
	for rows.Next() {
		var r BrowseResult
		if err := rows.Scan(&r.SessionID, &r.Summary, &r.Preview, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return ErrorResult(fmt.Sprintf("session_search browse scan failed: %v", err)).WithError(err)
		}
		if len(r.Preview) > 100 {
			r.Preview = r.Preview[:100] + "..."
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return ErrorResult(fmt.Sprintf("session_search browse failed: %v", err)).WithError(err)
	}

	payload := map[string]interface{}{
		"mode":    "browse",
		"count":   len(results),
		"results": results,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ErrorResult(fmt.Sprintf("session_search marshal failed: %v", err)).WithError(err)
	}
	return UserResult(string(raw))
}

func (t *SessionSearchTool) scroll(ctx context.Context, args map[string]interface{}) *ToolResult {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return ErrorResult("session_id is required for scroll mode")
	}

	aroundMsgID, ok := args["around_message_id"].(float64)
	if !ok || aroundMsgID <= 0 {
		return ErrorResult("around_message_id is required for scroll mode")
	}
	msgID := int64(aroundMsgID)

	window := 5
	if raw, ok := args["window"].(float64); ok {
		window = int(raw)
	}
	if window <= 0 {
		window = 5
	}
	if window > 20 {
		window = 20
	}

	beforeQuery := `
		SELECT id, role, content, COALESCE(created_at, '') AS ts
		FROM messages
		WHERE session_id = ? AND id <= ?
		  AND (archived IS NULL OR archived = 0)
		ORDER BY id DESC
		LIMIT ?
	`
	afterQuery := `
		SELECT id, role, content, COALESCE(created_at, '') AS ts
		FROM messages
		WHERE session_id = ? AND id > ?
		  AND (archived IS NULL OR archived = 0)
		ORDER BY id ASC
		LIMIT ?
	`

	beforeRows, err := t.db.QueryContext(ctx, beforeQuery, sessionID, msgID, window+1)
	if err != nil {
		return ErrorResult(fmt.Sprintf("session_search scroll before failed: %v", err)).WithError(err)
	}
	defer beforeRows.Close()

	var before []ScrollResult
	for beforeRows.Next() {
		var r ScrollResult
		if err := beforeRows.Scan(&r.ID, &r.Role, &r.Content, &r.Timestamp); err != nil {
			return ErrorResult(fmt.Sprintf("session_search scroll scan failed: %v", err)).WithError(err)
		}
		before = append(before, r)
	}
	if err := beforeRows.Err(); err != nil {
		return ErrorResult(fmt.Sprintf("session_search scroll failed: %v", err)).WithError(err)
	}

	afterRows, err := t.db.QueryContext(ctx, afterQuery, sessionID, msgID, window)
	if err != nil {
		return ErrorResult(fmt.Sprintf("session_search scroll after failed: %v", err)).WithError(err)
	}
	defer afterRows.Close()

	var after []ScrollResult
	for afterRows.Next() {
		var r ScrollResult
		if err := afterRows.Scan(&r.ID, &r.Role, &r.Content, &r.Timestamp); err != nil {
			return ErrorResult(fmt.Sprintf("session_search scroll scan failed: %v", err)).WithError(err)
		}
		after = append(after, r)
	}
	if err := afterRows.Err(); err != nil {
		return ErrorResult(fmt.Sprintf("session_search scroll failed: %v", err)).WithError(err)
	}

	for i := 0; i < len(before)/2; i++ {
		j := len(before) - 1 - i
		before[i], before[j] = before[j], before[i]
	}

	results := append(before, after...)

	payload := map[string]interface{}{
		"mode":            "scroll",
		"session_id":      sessionID,
		"anchor_msg_id":   msgID,
		"window":          window,
		"messages_before": len(before) - 1,
		"messages_after":  len(after),
		"count":           len(results),
		"results":         results,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ErrorResult(fmt.Sprintf("session_search marshal failed: %v", err)).WithError(err)
	}
	return UserResult(string(raw))
}

func (t *SessionSearchTool) readSession(ctx context.Context, args map[string]interface{}) *ToolResult {
	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return ErrorResult("session_id is required for read mode")
	}

	sqlQuery := `
		SELECT id, role, content, COALESCE(tool_calls, '') AS tool_calls, COALESCE(created_at, '') AS ts
		FROM messages
		WHERE session_id = ?
		  AND (archived IS NULL OR archived = 0)
		ORDER BY id ASC
	`

	rows, err := t.db.QueryContext(ctx, sqlQuery, sessionID)
	if err != nil {
		return ErrorResult(fmt.Sprintf("session_search read failed: %v", err)).WithError(err)
	}
	defer rows.Close()

	var results []ReadResult
	for rows.Next() {
		var r ReadResult
		if err := rows.Scan(&r.ID, &r.Role, &r.Content, &r.ToolCalls, &r.Timestamp); err != nil {
			return ErrorResult(fmt.Sprintf("session_search read scan failed: %v", err)).WithError(err)
		}
		if r.Role == "assistant" && r.ToolCalls != "" {
			r.Content = truncateWithToolCalls(r.Content, r.ToolCalls)
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return ErrorResult(fmt.Sprintf("session_search read failed: %v", err)).WithError(err)
	}

	const maxHead = 20
	const maxTail = 10
	var truncated bool
	displayResults := results
	if len(results) > maxHead+maxTail {
		truncated = true
		displayResults = append(results[:maxHead], results[len(results)-maxTail:]...)
	}

	payload := map[string]interface{}{
		"mode":       "read",
		"session_id": sessionID,
		"total":      len(results),
		"count":      len(displayResults),
		"truncated":  truncated,
		"results":    displayResults,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return ErrorResult(fmt.Sprintf("session_search marshal failed: %v", err)).WithError(err)
	}
	return UserResult(string(raw))
}

func truncateWithToolCalls(content, toolCalls string) string {
	if content == "" {
		return fmt.Sprintf("[tool calls: %s]", toolCalls)
	}
	if len(content) > 200 {
		content = content[:200] + "..."
	}
	return content
}

func sanitizeFTS5Query(query string) string {
	var protected []string
	result := query
	for {
		start := strings.Index(result, "\"")
		if start == -1 {
			break
		}
		end := strings.Index(result[start+1:], "\"")
		if end == -1 {
			break
		}
		phrase := result[start : start+end+2]
		placeholder := fmt.Sprintf("___PHRASE%d___", len(protected))
		protected = append(protected, phrase)
		result = result[:start] + placeholder + result[start+end+2:]
	}

	replacer := strings.NewReplacer(
		"+", "",
		"{", "",
		"}", "",
		"(", "",
		")", "",
		":", "",
		"^", "",
	)
	result = replacer.Replace(result)

	for strings.Contains(result, "**") {
		result = strings.ReplaceAll(result, "**", "*")
	}
	for strings.HasPrefix(result, "*") {
		result = result[1:]
	}

	for _, phrase := range protected {
		result = strings.Replace(result, "___PHRASE"+fmt.Sprintf("%d", strings.Index(query, phrase)), phrase, 1)
	}

	return strings.TrimSpace(result)
}
