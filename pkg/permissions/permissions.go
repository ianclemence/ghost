// Package permissions is Ghost's central permission broker.
//
// Borrowed pattern (OpenMausBot): a broker turns every risky action into
// an inline decision — allow / deny in chat — with per-bot approval modes
// (ask/auto/full/custom) and persistent scoped grants. Ghost adapts it to
// the appliance: capabilities declare risk, the broker sits between
// capability resolution and consequential execution, and NOTHING
// consequential runs on LLM authority alone.
//
// Pipeline: Intent → Capability → Risk → Broker → ALLOW / ASK / DENY →
// Execution. ASK pauses durably (SQLite requests + continuation payload);
// approval resumes the exact request without restarting the conversation.
//
// Rules enforced here: scoped grants (never broadened silently),
// expiry (expired approvals are unusable), restart survival, and
// fail-closed evaluation (unknown capability/action → ASK).
package permissions

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/skills"
)

// RiskOf returns the declared risk for a capability ID (e.g.
// "weather.current"). Unknown capabilities fail closed as consequential.
func RiskOf(capabilityID string) Risk {
	// The registry is keyed by skill name; match capability IDs by scan.
	for _, skill := range []string{
		"weather", "aqi", "currency", "crypto", "recipe", "flight",
		"find-nearby", "travel", "scraper", "calendar", "shopping",
		"reminders", "daily-briefing", "hardware", "homeassistant",
		"unit-converter", "world-clock", "calculator", "dictionary",
		"translate", "timer",
	} {
		if cap := skills.GetCapability(skill); cap.ID == capabilityID && cap.Risk != "" {
			return Risk(cap.Risk)
		}
	}
	return RiskConsequential
}

// Risk classifies what an action can do. Declared by the capability,
// never inferred from tool names by the model.
type Risk string

const (
	RiskReadOnly      Risk = "read_only"
	RiskLow           Risk = "low_risk"
	RiskConsequential Risk = "consequential"
	RiskHighImpact    Risk = "high_impact"
)

// Mode is the standing approval posture (adapted from OpenMausBot's
// approval modes; legacy autoApprove=true maps to auto, everything else
// fails closed to ask).
type Mode string

const (
	ModeAsk    Mode = "ask"
	ModeAuto   Mode = "auto"
	ModeFull   Mode = "full"
	ModeCustom Mode = "custom"
)

// Verdict is the broker's decision.
type Verdict string

const (
	VerdictAllow Verdict = "allow"
	VerdictAsk   Verdict = "ask"
	VerdictDeny  Verdict = "deny"
)

// GrantType is what the user approved.
type GrantType string

const (
	GrantOnce   GrantType = "allow_once"
	GrantAlways GrantType = "allow_always"
	GrantDeny   GrantType = "deny"
)

// RequestStatus tracks a permission request's lifecycle.
type RequestStatus string

const (
	StatusPending   RequestStatus = "pending"
	StatusApproved  RequestStatus = "approved"
	StatusDenied    RequestStatus = "denied"
	StatusExpired   RequestStatus = "expired"
	StatusCancelled RequestStatus = "cancelled"
	StatusConsumed  RequestStatus = "consumed"
)

// Request is a first-class durable permission request.
type Request struct {
	ID           string            `json:"id"`
	RequestID    string            `json:"request_id"`
	SessionKey   string            `json:"session_key,omitempty"`
	AgentID      string            `json:"agent_id"`
	Capability   string            `json:"capability"`
	Action       string            `json:"action"`
	Target       string            `json:"target,omitempty"`
	Reason       string            `json:"reason,omitempty"`
	Risk         Risk              `json:"risk"`
	Status       RequestStatus     `json:"status"`
	Continuation map[string]string `json:"continuation,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	ExpiresAt    time.Time         `json:"expires_at"`
	ResolvedAt   *time.Time        `json:"resolved_at,omitempty"`
	Grant        GrantType         `json:"grant,omitempty"`
}

// Grant is a persisted standing permission. Scope is explicit and narrow:
// capability + action + scope (e.g. "contact:maria" or "owner"). A grant
// never widens beyond what was approved.
type Grant struct {
	Capability string    `json:"capability"`
	Action     string    `json:"action"`
	Scope      string    `json:"scope"`
	CreatedAt  time.Time `json:"created_at"`
}

// Emitter receives broker lifecycle events (wired to the canonical event
// stream; nil-safe). Defined here to avoid import cycles.
type Emitter func(eventType string, req *Request)

// Broker is the central permission authority.
type Broker struct {
	mu      sync.RWMutex
	db      *sql.DB
	mode    Mode
	emit    Emitter
	ttl     time.Duration
	nowFunc func() time.Time
}

func newID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))[:32]
	}
	return hex.EncodeToString(buf)
}

// Open creates the broker over db (SQLite). Tables are created if absent.
// ttl bounds how long an approval request stays answerable.
func Open(db *sql.DB, mode Mode, ttl time.Duration) (*Broker, error) {
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	b := &Broker{db: db, mode: mode, ttl: ttl, nowFunc: time.Now}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS permission_requests (
			id TEXT PRIMARY KEY, request_id TEXT, agent_id TEXT,
			capability TEXT, action TEXT, target TEXT, reason TEXT,
			risk TEXT, status TEXT, continuation TEXT,
			created_at TEXT, expires_at TEXT, resolved_at TEXT,
			grant TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_perm_req_status ON permission_requests(status)`,
		`CREATE TABLE IF NOT EXISTS permission_grants (
			capability TEXT, action TEXT, scope TEXT,
			created_at TEXT,
			PRIMARY KEY (capability, action, scope)
		)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return nil, err
		}
	}
	// Migration for databases created before session linkage existed
	// (must precede the session index below).
	_, _ = db.Exec(`ALTER TABLE permission_requests ADD COLUMN session_key TEXT`)
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_perm_req_session ON permission_requests(session_key)`); err != nil {
		return nil, err
	}
	return b, nil
}

// SetEmitter wires lifecycle events (permission.requested/approved/...).
func (b *Broker) SetEmitter(e Emitter) { b.emit = e }

func (b *Broker) now() time.Time {
	if b.nowFunc != nil {
		return b.nowFunc()
	}
	return time.Now()
}

// SetMode updates the standing posture (ask/auto/full/custom).
func (b *Broker) SetMode(m Mode) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.mode = m
}

// Evaluate decides ALLOW / ASK / DENY for a capability action WITHOUT
// creating a request. Pure policy:
//   - explicit deny grant → DENY
//   - matching always-grant (exact scope) → ALLOW
//   - read_only risk → ALLOW (no approval)
//   - low_risk → ALLOW unless mode is ask? No: low-risk creation acts
//     (reminders, drafts, saved memories) auto-allow in auto/full,
//     ask in ask/custom. Read-only always allows.
//   - consequential/high_impact → ASK, except full mode allows
//     consequential (never high_impact).
func (b *Broker) Evaluate(capability, action, scope string, risk Risk) Verdict {
	b.mu.RLock()
	mode := b.mode
	b.mu.RUnlock()
	capability = strings.TrimSpace(capability)
	action = strings.TrimSpace(action)
	if capability == "" || action == "" {
		return VerdictAsk // fail closed on malformed input
	}
	if b.denied(capability, action, scope) {
		return VerdictDeny
	}
	if b.granted(capability, action, scope) {
		return VerdictAllow
	}
	switch risk {
	case RiskReadOnly:
		return VerdictAllow
	case RiskLow:
		if mode == ModeAsk || mode == ModeCustom {
			return VerdictAsk
		}
		return VerdictAllow
	case RiskConsequential:
		if mode == ModeFull {
			return VerdictAllow
		}
		return VerdictAsk
	case RiskHighImpact:
		return VerdictAsk // never auto-authorized
	default:
		return VerdictAsk // unknown risk fails closed
	}
}

// Require creates (or reuses) a durable PENDING request for an ASK
// verdict and returns it. Idempotent per request_id: the same interrupted
// turn resumes the same request instead of duplicating approval cards.
func (b *Broker) Require(requestID, sessionKey, agentID, capability, action, target, reason string, risk Risk, continuation map[string]string) (*Request, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	if strings.TrimSpace(requestID) == "" {
		return nil, errors.New("request_id required")
	}
	if existing, ok := b.byRequestID(requestID); ok && existing.Status == StatusPending && now.Before(existing.ExpiresAt) {
		return existing, nil
	}
	r := &Request{
		ID: newID(), RequestID: requestID, SessionKey: sessionKey, AgentID: agentID,
		Capability: capability, Action: action, Target: target, Reason: reason,
		Risk: risk, Status: StatusPending, Continuation: continuation,
		CreatedAt: now, ExpiresAt: now.Add(b.ttl),
	}
	if err := b.insert(r); err != nil {
		return nil, err
	}
	b.emitEvent("permission.requested", r)
	return r, nil
}

// Resolve applies allow_once / allow_always / deny to a pending request.
// Expired requests cannot be resolved (fail closed). allow_always stores
// a scoped grant; deny stores a scoped denial.
func (b *Broker) Resolve(id string, grant GrantType, scope string) (*Request, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.byID(id)
	if !ok {
		return nil, errors.New("permission request not found")
	}
	if r.Status != StatusPending {
		return nil, errors.New("permission request is not pending")
	}
	now := b.now()
	if now.After(r.ExpiresAt) {
		r.Status = StatusExpired
		r.ResolvedAt = &now
		_ = b.update(r)
		b.emitEvent("permission.expired", r)
		return r, errors.New("permission request expired")
	}
	r.ResolvedAt = &now
	r.Grant = grant
	switch grant {
	case GrantOnce:
		r.Status = StatusApproved
		b.emitEvent("permission.approved", r)
	case GrantAlways:
		r.Status = StatusApproved
		_ = b.storeGrant(Grant{Capability: r.Capability, Action: r.Action, Scope: scope, CreatedAt: now})
		b.emitEvent("permission.approved", r)
	case GrantDeny:
		r.Status = StatusDenied
		_ = b.storeGrant(Grant{Capability: r.Capability, Action: "deny:" + r.Action, Scope: scope, CreatedAt: now})
		b.emitEvent("permission.denied", r)
	default:
		return nil, errors.New("unknown grant type")
	}
	_ = b.update(r)
	return r, nil
}

// Cancel marks a pending request cancelled (superseded turns, user abort).
func (b *Broker) Cancel(id string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	r, ok := b.byID(id)
	if !ok {
		return errors.New("permission request not found")
	}
	if r.Status != StatusPending {
		return nil
	}
	now := b.now()
	r.Status = StatusCancelled
	r.ResolvedAt = &now
	return b.update(r)
}

// ConsumeApproved atomically claims an approved allow_once request for
// one resumed turn: exactly one execution per approval. allow_always
// grants never consult this path (Evaluate allows them directly).
// Returns the request when consumed, ok=false otherwise.
func (b *Broker) ConsumeApproved(requestID string) (*Request, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.now()
	r, ok := b.byRequestID(requestID)
	if !ok || r.Status != StatusApproved || r.Grant != GrantOnce {
		return nil, false
	}
	if now.After(r.ExpiresAt) {
		return nil, false
	}
	r.Status = StatusConsumed
	r.ResolvedAt = &now
	_ = b.update(r)
	return r, true
}

// PendingForSession returns the newest live pending request for a
// session — the approval-continuation lookup behind "allow once" chat
// replies and approval cards. Restart-safe via SQLite.
func (b *Broker) PendingForSession(sessionKey string) (*Request, bool) {
	if strings.TrimSpace(sessionKey) == "" {
		return nil, false
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	r, ok := scanRequest(b.db.QueryRow(`SELECT id, request_id, session_key, agent_id, capability, action, target, reason, risk, status, continuation, created_at, expires_at, resolved_at, grant FROM permission_requests WHERE session_key=? AND status=? ORDER BY created_at DESC LIMIT 1`, sessionKey, StatusPending))
	if !ok {
		return nil, false
	}
	if b.now().After(r.ExpiresAt) {
		return nil, false
	}
	return r, true
}

// PendingForRequest returns the live pending request for a turn, if any.
// This is the approval-continuation lookup: restart-safe via SQLite.
func (b *Broker) PendingForRequest(requestID string) (*Request, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	r, ok := b.byRequestID(requestID)
	if !ok || r.Status != StatusPending {
		return nil, false
	}
	if b.now().After(r.ExpiresAt) {
		return nil, false
	}
	return r, true
}

// Requests lists permission requests, newest first, optionally filtered
// by status. Payloads are safe (continuations exclude secrets).
func (b *Broker) Requests(status RequestStatus, limit int) []*Request {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	q := `SELECT id, request_id, session_key, agent_id, capability, action, target, reason, risk, status, continuation, created_at, expires_at, resolved_at, grant FROM permission_requests`
	args := []interface{}{}
	if status != "" {
		q += ` WHERE status=?`
		args = append(args, string(status))
	}
	q += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := b.db.Query(q, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []*Request
	for rows.Next() {
		var r Request
		var risk, st, cont, created, expires string
		var session sql.NullString
		var resolved, grant sql.NullString
		if err := rows.Scan(&r.ID, &r.RequestID, &session, &r.AgentID, &r.Capability, &r.Action,
			&r.Target, &r.Reason, &risk, &st, &cont, &created, &expires, &resolved, &grant); err != nil {
			continue
		}
		if session.Valid {
			r.SessionKey = session.String
		}
		r.Risk = Risk(risk)
		r.Status = RequestStatus(st)
		_ = json.Unmarshal([]byte(cont), &r.Continuation)
		if t, ok := parseTime(created); ok {
			r.CreatedAt = t
		}
		if t, ok := parseTime(expires); ok {
			r.ExpiresAt = t
		}
		if resolved.Valid {
			if t, ok := parseTime(resolved.String); ok {
				r.ResolvedAt = &t
			}
		}
		if grant.Valid {
			r.Grant = GrantType(grant.String)
		}
		out = append(out, &r)
	}
	return out
}

// Grants lists standing grants for UI/revocation (no secret content).
func (b *Broker) Grants() []Grant {
	rows, err := b.db.Query(`SELECT capability, action, scope, created_at FROM permission_grants ORDER BY created_at`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []Grant
	for rows.Next() {
		var g Grant
		var ts string
		if err := rows.Scan(&g.Capability, &g.Action, &g.Scope, &ts); err != nil {
			continue
		}
		if t, ok := parseTime(ts); ok {
			g.CreatedAt = t
		}
		out = append(out, g)
	}
	return out
}

// GrantStanding persists one validated standing grant (or denial).
// Callers must validate the capability via skills.HasCapability first;
// this method re-checks action/scope shape but trusts capability IDs
// only from runtime-validated proposals.
func (b *Broker) GrantStanding(capability, action, scope string, deny bool) error {
	if strings.TrimSpace(capability) == "" || strings.TrimSpace(action) == "" || strings.TrimSpace(scope) == "" {
		return errors.New("capability, action, and scope required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	act := action
	if deny {
		act = "deny:" + action
	}
	return b.storeGrant(Grant{Capability: capability, Action: act, Scope: scope, CreatedAt: b.now()})
}

// Revoke removes one standing grant (or denial).
func (b *Broker) Revoke(capability, action, scope string) error {
	_, err := b.db.Exec(`DELETE FROM permission_grants WHERE capability=? AND action=? AND scope=?`, capability, action, scope)
	return err
}

// parseTime accepts the layouts SQLite may hand back (TEXT storage plus
// driver-normalized DATETIME strings).
func parseTime(s string) (time.Time, bool) {
	for _, layout := range []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.999999999 -0700 MST",
		"2006-01-02 15:04:05 -0700 MST",
		time.RFC3339,
		"2006-01-02T15:04:05Z07:00",
	} {
		if t, err := time.Parse(layout, strings.TrimSpace(s)); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// SweepExpires marks overdue pending requests expired (called on tick).
func (b *Broker) SweepExpires() int {
	res, err := b.db.Exec(`UPDATE permission_requests SET status=? WHERE status=? AND expires_at < ?`,
		StatusExpired, StatusPending, b.now().Format(time.RFC3339))
	if err != nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return int(n)
}

func (b *Broker) emitEvent(t string, r *Request) {
	if b.emit != nil {
		cp := *r
		b.emit(t, &cp)
	}
}

func (b *Broker) granted(capability, action, scope string) bool {
	var n int
	_ = b.db.QueryRow(`SELECT COUNT(*) FROM permission_grants WHERE capability=? AND action=? AND scope=?`,
		capability, action, scope).Scan(&n)
	return n > 0
}

func (b *Broker) denied(capability, action, scope string) bool {
	var n int
	_ = b.db.QueryRow(`SELECT COUNT(*) FROM permission_grants WHERE capability=? AND action=? AND scope=?`,
		capability, "deny:"+action, scope).Scan(&n)
	return n > 0
}

func (b *Broker) insert(r *Request) error {
	// Defense in depth: continuations must never carry secrets, no matter
	// which layer constructed them. The governance boundary strips first;
	// the durable store refuses second.
	r.Continuation = stripSecretValues(r.Continuation)
	cont, _ := json.Marshal(r.Continuation)
	_, err := b.db.Exec(`INSERT INTO permission_requests
		(id, request_id, session_key, agent_id, capability, action, target, reason, risk, status, continuation, created_at, expires_at, resolved_at, grant)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.RequestID, r.SessionKey, r.AgentID, r.Capability, r.Action, r.Target, r.Reason,
		string(r.Risk), string(r.Status), string(cont),
		r.CreatedAt.Format(time.RFC3339), r.ExpiresAt.Format(time.RFC3339), nil, "")
	return err
}

// stripSecretValues drops secret-shaped keys from continuations. Values
// are never masked-in-place (a masked secret is still a secret-shaped
// liability); the key is removed and intent stays intact.
func stripSecretValues(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "key") || strings.Contains(lk, "token") ||
			strings.Contains(lk, "secret") || strings.Contains(lk, "password") ||
			strings.Contains(lk, "credential") {
			continue
		}
		out[k] = v
	}
	return out
}

func (b *Broker) update(r *Request) error {
	var res interface{}
	if r.ResolvedAt != nil {
		res = r.ResolvedAt.Format(time.RFC3339)
	}
	_, err := b.db.Exec(`UPDATE permission_requests SET status=?, resolved_at=?, grant=? WHERE id=?`,
		string(r.Status), res, string(r.Grant), r.ID)
	return err
}

func (b *Broker) storeGrant(g Grant) error {
	_, err := b.db.Exec(`INSERT OR REPLACE INTO permission_grants (capability, action, scope, created_at) VALUES (?,?,?,?)`,
		g.Capability, g.Action, g.Scope, g.CreatedAt.Format(time.RFC3339))
	return err
}

func scanRequest(row *sql.Row) (*Request, bool) {
	var r Request
	var risk, status, cont, created, expires string
	var resolved, grant sql.NullString
	var session sql.NullString
	if err := row.Scan(&r.ID, &r.RequestID, &session, &r.AgentID, &r.Capability, &r.Action,
		&r.Target, &r.Reason, &risk, &status, &cont, &created, &expires, &resolved, &grant); err != nil {
		return nil, false
	}
	r.Risk = Risk(risk)
	if session.Valid {
		r.SessionKey = session.String
	}
	r.Status = RequestStatus(status)
	_ = json.Unmarshal([]byte(cont), &r.Continuation)
	if t, ok := parseTime(created); ok {
		r.CreatedAt = t
	}
	if t, ok := parseTime(expires); ok {
		r.ExpiresAt = t
	}
	if resolved.Valid {
		if t, ok := parseTime(resolved.String); ok {
			r.ResolvedAt = &t
		}
	}
	if grant.Valid {
		r.Grant = GrantType(grant.String)
	}
	return &r, true
}

func (b *Broker) byID(id string) (*Request, bool) {
	return scanRequest(b.db.QueryRow(`SELECT id, request_id, session_key, agent_id, capability, action, target, reason, risk, status, continuation, created_at, expires_at, resolved_at, grant FROM permission_requests WHERE id=?`, id))
}

func (b *Broker) byRequestID(requestID string) (*Request, bool) {
	return scanRequest(b.db.QueryRow(`SELECT id, request_id, session_key, agent_id, capability, action, target, reason, risk, status, continuation, created_at, expires_at, resolved_at, grant FROM permission_requests WHERE request_id=? ORDER BY created_at DESC LIMIT 1`, requestID))
}

// CardAction is one native approval action.
type CardAction struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Style string `json:"style"` // "primary" | "secondary" | "danger"
}

// ApprovalCard is the product-level representation for native mobile
// (and web) approval cards. It carries human context — action, target,
// reason, risk — and the allowed actions. It NEVER carries raw tool
// arguments, secrets, schemas, provider details, paths, or
// model-generated reasoning.
type ApprovalCard struct {
	RequestID   string       `json:"request_id"`
	AgentID     string       `json:"agent_id"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Risk        Risk         `json:"risk"`
	ExpiresAt   string       `json:"expires_at"`
	Actions     []CardAction `json:"actions"`
}

// Card projects a pending request to its native card. Nil-safe: returns
// ok=false for non-pending requests (only pending cards are actionable).
func (r *Request) Card() (ApprovalCard, bool) {
	if r == nil || r.Status != StatusPending {
		return ApprovalCard{}, false
	}
	title := cardTitle(r.Capability, r.Action)
	desc := strings.TrimSpace(r.Reason)
	if desc == "" {
		desc = "Ghost needs your approval to continue."
	}
	return ApprovalCard{
		RequestID:   r.ID,
		AgentID:     r.AgentID,
		Title:       title,
		Description: desc,
		Risk:        r.Risk,
		ExpiresAt:   r.ExpiresAt.Format(time.RFC3339),
		Actions: []CardAction{
			{ID: "allow_once", Label: "Allow once", Style: "primary"},
			{ID: "allow_always", Label: "Always allow", Style: "secondary"},
			{ID: "deny", Label: "Deny", Style: "danger"},
		},
	}, true
}

func cardTitle(capability, action string) string {
	act := action
	if idx := strings.Index(act, ":"); idx >= 0 {
		act = act[idx+1:]
	}
	switch {
	case strings.Contains(capability, "calendar"):
		if strings.Contains(act, "create") || strings.Contains(act, "add") {
			return "Add calendar event?"
		}
		return "Use your calendar?"
	case strings.Contains(capability, "telegram") || strings.Contains(capability, "message"):
		return "Send this message?"
	case strings.Contains(capability, "mail") || strings.Contains(capability, "email"):
		return "Send this email?"
	case strings.Contains(capability, "hass") || strings.Contains(capability, "home"):
		return "Control a home device?"
	case strings.Contains(capability, "file") || strings.Contains(capability, "delete"):
		return "Change files?"
	default:
		if act != "" && act != capability {
			return "Allow " + prettifyAction(act) + "?"
		}
		return "Allow this action?"
	}
}

func prettifyAction(act string) string {
	act = strings.ReplaceAll(act, "_", " ")
	act = strings.ReplaceAll(act, ".", " ")
	return strings.TrimSpace(act)
}
