// Package cevents is Ghost's canonical event stream — the architectural
// backbone everything meaningful flows through.
//
// Borrowed pattern (OpenMausBot harness bus): one fan-in bus; every
// event redacted at the boundary BEFORE persistence/fan-out; per-day
// NDJSON log (0600); write-failure marker that never recurses; a faulty
// listener never breaks publish. Ghost adapts it local-first: SQLite is
// the durable store, the relay only transports, and projections (SSE,
// activity, diagnostics) derive from the same canonical rows.
//
// Invariants (mandatory):
//   - Internal events never become user-facing (enforced by SSEForm).
//   - Replay describes; it never re-executes (events carry no executors).
//   - Secrets never enter payloads (redact.Any at Publish).
//   - Ordering is deterministic (timestamp, seq).
package cevents

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/product"
	"github.com/ianclemence/ghost/pkg/redact"
)

// Type is the canonical event taxonomy (deliberately small).
type Type string

const (
	// Conversation
	MessageReceived Type = "message.received"
	MessageCreated  Type = "message.created"
	MessageUpdated  Type = "message.updated"
	// Reasoning / execution (product-level, never chain-of-thought)
	AgentStarted   Type = "agent.started"
	AgentProgress  Type = "agent.progress"
	AgentWaiting   Type = "agent.waiting"
	AgentCompleted Type = "agent.completed"
	AgentFailed    Type = "agent.failed"
	// Capability
	CapabilityStarted   Type = "capability.started"
	CapabilityCompleted Type = "capability.completed"
	CapabilityFailed    Type = "capability.failed"
	// Tools
	ToolStarted   Type = "tool.started"
	ToolCompleted Type = "tool.completed"
	ToolFailed    Type = "tool.failed"
	// Permissions
	PermissionRequested Type = "permission.requested"
	PermissionApproved  Type = "permission.approved"
	PermissionDenied    Type = "permission.denied"
	PermissionExpired   Type = "permission.expired"
	// Memory
	MemoryCreated   Type = "memory.created"
	MemoryUpdated   Type = "memory.updated"
	MemoryRetrieved Type = "memory.retrieved"
	MemoryDeleted   Type = "memory.deleted"
	// Integrations
	IntegrationConnected    Type = "integration.connected"
	IntegrationDisconnected Type = "integration.disconnected"
	IntegrationExpired      Type = "integration.expired"
	IntegrationFailed       Type = "integration.failed"
	// Scheduling / routines
	RoutineCreated   Type = "routine.created"
	RoutineStarted   Type = "routine.started"
	RoutineWaiting   Type = "routine.waiting"
	RoutineCompleted Type = "routine.completed"
	RoutineFailed    Type = "routine.failed"
	// System
	GhostStarted    Type = "ghost.started"
	GhostReady      Type = "ghost.ready"
	GhostDegraded   Type = "ghost.degraded"
	GhostOffline    Type = "ghost.offline"
	GhostRecovering Type = "ghost.recovering"
	// Errors
	OperationFailed Type = "operation.failed"
)

// Durable reports whether the type is persisted long-term. Transient
// types (progress heartbeats) live in NDJSON + memory only, never the
// warehouse.
func (t Type) Durable() bool {
	switch t {
	case AgentProgress, ToolStarted, ToolCompleted, MemoryRetrieved:
		return false
	default:
		return true
	}
}

// DefaultVisibility assigns the safe visibility when publishers omit it.
// Anything not explicitly user-facing stays internal.
func (t Type) DefaultVisibility() product.Visibility {
	switch t {
	case MessageCreated, MessageUpdated,
		AgentWaiting, AgentCompleted, AgentFailed,
		CapabilityCompleted, CapabilityFailed,
		ToolFailed,
		PermissionRequested, PermissionApproved, PermissionDenied, PermissionExpired,
		MemoryCreated, MemoryUpdated, MemoryDeleted,
		IntegrationConnected, IntegrationDisconnected, IntegrationExpired, IntegrationFailed,
		RoutineCreated, RoutineWaiting, RoutineCompleted, RoutineFailed,
		GhostReady, GhostDegraded, GhostOffline, GhostRecovering,
		OperationFailed:
		return product.VisUserMessage
	default:
		return product.VisInternalTrace
	}
}

// Event is the canonical runtime event.
type Event struct {
	ID             string                 `json:"id"`
	Type           Type                   `json:"type"`
	RequestID      string                 `json:"request_id,omitempty"`
	SessionID      string                 `json:"session_id,omitempty"`
	ConversationID string                 `json:"conversation_id,omitempty"`
	GhostID        string                 `json:"ghost_id,omitempty"`
	AgentID        string                 `json:"agent_id,omitempty"`
	RoutineID      string                 `json:"routine_id,omitempty"`
	Timestamp      time.Time              `json:"timestamp"`
	Seq            int64                  `json:"seq"`
	Visibility     product.Visibility     `json:"visibility"`
	Status         string                 `json:"status,omitempty"`
	Payload        map[string]interface{} `json:"payload,omitempty"`
}

func newID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))[:32]
	}
	return hex.EncodeToString(buf)
}

// Filter scopes subscriptions and queries.
type Filter struct {
	GhostID         string
	RequestID       string
	SessionID       string
	ConversationID  string
	RoutineID       string
	Types           []Type
	UserVisibleOnly bool
}

func (f Filter) matches(e *Event) bool {
	if f.GhostID != "" && e.GhostID != f.GhostID {
		return false
	}
	if f.RequestID != "" && e.RequestID != f.RequestID {
		return false
	}
	if f.SessionID != "" && e.SessionID != f.SessionID {
		return false
	}
	if f.ConversationID != "" && e.ConversationID != f.ConversationID {
		return false
	}
	if f.RoutineID != "" && e.RoutineID != f.RoutineID {
		return false
	}
	if f.UserVisibleOnly && !e.Visibility.UserVisible() {
		return false
	}
	if len(f.Types) > 0 {
		for _, t := range f.Types {
			if t == e.Type {
				return true
			}
		}
		return false
	}
	return true
}

// Stream is the canonical bus + durable store.
type Stream struct {
	mu        sync.RWMutex
	db        *sql.DB
	logDir    string
	listeners map[int]struct {
		filter Filter
		fn     func(*Event)
	}
	nextListener int
	logFailed    bool
}

// Open creates the stream over db with NDJSON logs under logDir.
func Open(db *sql.DB, logDir string) (*Stream, error) {
	s := &Stream{db: db, logDir: logDir, listeners: map[int]struct {
		filter Filter
		fn     func(*Event)
	}{}}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS canonical_events (
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			id TEXT UNIQUE, type TEXT, request_id TEXT, session_id TEXT,
			conversation_id TEXT, ghost_id TEXT, agent_id TEXT, routine_id TEXT,
			timestamp TEXT, visibility TEXT, status TEXT, payload TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cevents_request ON canonical_events(request_id, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_cevents_conversation ON canonical_events(conversation_id, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_cevents_ghost ON canonical_events(ghost_id, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_cevents_time ON canonical_events(timestamp)`,
	}
	for _, st := range stmts {
		if _, err := db.Exec(st); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(logDir, 0700); err != nil {
		return nil, err
	}
	return s, nil
}

// Publish redacts, persists (durable types), appends NDJSON, and fans out.
// A failing listener or log write never breaks publish or other listeners.
func (s *Stream) Publish(e *Event) *Event {
	if e.ID == "" {
		e.ID = newID()
	}
	if e.Timestamp.IsZero() {
		e.Timestamp = time.Now()
	}
	if e.Visibility == "" {
		e.Visibility = e.Type.DefaultVisibility()
	}
	if e.Payload != nil {
		if red, ok := redact.Any(e.Payload).(map[string]interface{}); ok {
			e.Payload = red
		}
	}
	if e.Type.Durable() {
		if err := s.insert(e); err != nil {
			// Persistence failure degrades to memory-only delivery;
			// the NDJSON marker records the gap without recursion.
			s.appendLog(map[string]interface{}{
				"type":    "runtime.error",
				"message": "Canonical event history is incomplete: Ghost could not write one or more events to disk. Live updates will continue.",
			})
		}
	}
	s.appendLog(eventToLog(e))
	s.deliver(e)
	return e
}

func eventToLog(e *Event) map[string]interface{} {
	m := map[string]interface{}{
		"id": e.ID, "type": string(e.Type), "timestamp": e.Timestamp.Format(time.RFC3339),
		"seq": e.Seq, "visibility": string(e.Visibility),
	}
	for k, v := range map[string]string{
		"request_id": e.RequestID, "session_id": e.SessionID,
		"conversation_id": e.ConversationID, "ghost_id": e.GhostID,
		"agent_id": e.AgentID, "routine_id": e.RoutineID, "status": e.Status,
	} {
		if v != "" {
			m[k] = v
		}
	}
	if e.Payload != nil {
		m["payload"] = e.Payload
	}
	return m
}

func (s *Stream) insert(e *Event) error {
	payload, _ := json.Marshal(e.Payload)
	res, err := s.db.Exec(`INSERT INTO canonical_events
		(id, type, request_id, session_id, conversation_id, ghost_id, agent_id, routine_id, timestamp, visibility, status, payload)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, string(e.Type), e.RequestID, e.SessionID, e.ConversationID,
		e.GhostID, e.AgentID, e.RoutineID, e.Timestamp.Format(time.RFC3339),
		string(e.Visibility), e.Status, string(payload))
	if err != nil {
		return err
	}
	if seq, err := res.LastInsertId(); err == nil {
		e.Seq = seq
	}
	return nil
}

func (s *Stream) appendLog(entry map[string]interface{}) {
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	name := filepath.Join(s.logDir, time.Now().Format("2006-01-02")+".ndjson")
	f, err := os.OpenFile(name, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

func (s *Stream) deliver(e *Event) {
	s.mu.RLock()
	calls := make([]func(*Event), 0)
	for _, l := range s.listeners {
		if l.filter.matches(e) {
			calls = append(calls, l.fn)
		}
	}
	s.mu.RUnlock()
	cp := *e
	for _, fn := range calls {
		func() {
			defer func() { _ = recover() }()
			fn(&cp)
		}()
	}
}

// Subscribe registers a filtered listener; returns unsubscribe.
func (s *Stream) Subscribe(f Filter, fn func(*Event)) func() {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.nextListener
	s.nextListener++
	s.listeners[id] = struct {
		filter Filter
		fn     func(*Event)
	}{f, fn}
	return func() {
		s.mu.Lock()
		delete(s.listeners, id)
		s.mu.Unlock()
	}
}

// ByRequest returns a request's events in deterministic order — the full
// message→capability→permission→execution→result trace without guessing.
func (s *Stream) ByRequest(requestID string) []*Event {
	rows, err := s.db.Query(`SELECT seq, id, type, request_id, session_id, conversation_id, ghost_id, agent_id, routine_id, timestamp, visibility, status, payload
		FROM canonical_events WHERE request_id=? ORDER BY seq`, requestID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanAll(rows)
}

// Recent returns the latest events for UI/activity, newest first.
func (s *Stream) Recent(limit int, f Filter) []*Event {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	clauses := []string{}
	args := []interface{}{}
	if f.GhostID != "" {
		clauses = append(clauses, "ghost_id=?")
		args = append(args, f.GhostID)
	}
	if f.ConversationID != "" {
		clauses = append(clauses, "conversation_id=?")
		args = append(args, f.ConversationID)
	}
	if f.UserVisibleOnly {
		clauses = append(clauses, "(visibility='user_visible_message' OR visibility='user_visible_error')")
	}
	q := `SELECT seq, id, type, request_id, session_id, conversation_id, ghost_id, agent_id, routine_id, timestamp, visibility, status, payload FROM canonical_events`
	if len(clauses) > 0 {
		q += " WHERE " + strings.Join(clauses, " AND ")
	}
	q += " ORDER BY seq DESC LIMIT ?"
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := scanAll(rows)
	// scanAll returns seq-ascending per query; Recent asked DESC — reverse.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Since returns events after a sequence cursor for resumable
// subscriptions: the client passes the last seen seq and receives only
// newer rows, newest last. Replay is read-only — it never executes.
func (s *Stream) Since(seq int64, limit int, f Filter) []*Event {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	clauses := []string{"seq > ?"}
	args := []interface{}{seq}
	if f.GhostID != "" {
		clauses = append(clauses, "ghost_id=?")
		args = append(args, f.GhostID)
	}
	if f.UserVisibleOnly {
		clauses = append(clauses, "(visibility='user_visible_message' OR visibility='user_visible_error')")
	}
	q := `SELECT seq, id, type, request_id, session_id, conversation_id, ghost_id, agent_id, routine_id, timestamp, visibility, status, payload FROM canonical_events WHERE ` +
		strings.Join(clauses, " AND ") + ` ORDER BY seq ASC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	return scanAll(rows)
}

// Prune deletes transient-scope rows older than maxAge, keeping durable
// history bounded. Durable types are never pruned by age here (explicit
// retention policy lives with the caller).
func (s *Stream) Prune(maxAge time.Duration) int {
	cutoff := time.Now().Add(-maxAge).Format(time.RFC3339)
	res, err := s.db.Exec(`DELETE FROM canonical_events WHERE timestamp < ? AND
		type IN ('agent.progress','tool.started','tool.completed','memory.retrieved')`, cutoff)
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}

func scanAll(rows *sql.Rows) []*Event {
	var out []*Event
	for rows.Next() {
		var e Event
		var typ, vis, ts, payload string
		if err := rows.Scan(&e.Seq, &e.ID, &typ, &e.RequestID, &e.SessionID,
			&e.ConversationID, &e.GhostID, &e.AgentID, &e.RoutineID, &ts, &vis, &e.Status, &payload); err != nil {
			continue
		}
		e.Type = Type(typ)
		e.Visibility = product.Visibility(vis)
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			e.Timestamp = t
		}
		if payload != "" {
			_ = json.Unmarshal([]byte(payload), &e.Payload)
		}
		out = append(out, &e)
	}
	return out
}

// SSEForm projects an event onto the SSE wire format. It returns ok=false
// for anything not user-visible: internal events can never accidentally
// become client events. Payloads are pre-redacted at Publish.
func (e *Event) SSEForm() (typ, data string, ok bool) {
	if !e.Visibility.UserVisible() {
		return "", "", false
	}
	body := map[string]interface{}{"id": e.ID, "timestamp": e.Timestamp.Format(time.RFC3339)}
	if e.RequestID != "" {
		body["request_id"] = e.RequestID
	}
	if e.Status != "" {
		body["status"] = e.Status
	}
	if e.Payload != nil {
		body["payload"] = e.Payload
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", "", false
	}
	return string(e.Type), string(raw), true
}
