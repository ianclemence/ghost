package skills

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"time"
)

// Durable pending-intent store: requests waiting on clarification,
// configuration, authorization, or permission survive normal runtime
// conditions (including process restart). The existing in-memory fast
// path (pending.go) is unchanged; this layer adds identity, expiry,
// sanitized intent, continuation state, and completion tracking.
//
// Durability root: <workspace>/pending/requests.json (0600). No secrets
// are ever stored: intents pass through SanitizeIntent on write.

// PendingStatus tracks a durable request's lifecycle.
type PendingStatus string

const (
	PendingOpen      PendingStatus = "pending"
	PendingCompleted PendingStatus = "completed"
	PendingCancelled PendingStatus = "cancelled"
	PendingExpired   PendingStatus = "expired"
)

// PendingRequest is a durable continuation.
type PendingRequest struct {
	ID           string            `json:"id"`
	SessionID    string            `json:"session_id"`
	Capability   string            `json:"capability"`
	Skill        string            `json:"skill"`
	MissingField string            `json:"missing_field,omitempty"`
	Question     string            `json:"question,omitempty"`
	Intent       string            `json:"intent"` // sanitized, never secrets
	Continuation map[string]string `json:"continuation,omitempty"`
	Status       PendingStatus     `json:"status"`
	CreatedAt    time.Time         `json:"created_at"`
	ExpiresAt    time.Time         `json:"expires_at"`
	CompletedAt  *time.Time        `json:"completed_at,omitempty"`
}

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(api[_-]?key|apikey|client[_-]?secret|refresh[_-]?token|access[_-]?token|auth[_-]?code)\s*[:=]\s*\S+`),
	regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9\-._~+/]+=*`),
	regexp.MustCompile(`(?i)(password|passwd|secret|token)\s*[:=]\s*\S+`),
}

// SanitizeIntent strips secret-looking material from a stored intent.
// Structural rule (never store credential fields) comes first at the
// call sites; this pattern pass is defense-in-depth, not the only defense.
func SanitizeIntent(intent string) string {
	out := intent
	for _, re := range secretPatterns {
		out = re.ReplaceAllString(out, "$1=[redacted]")
	}
	return out
}

func newRequestID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().String()))[:32]
	}
	return hex.EncodeToString(buf)
}

// PendingStore is the durable JSON-backed store.
type PendingStore struct {
	mu   sync.Mutex
	path string
}

// NewPendingStore opens (or creates) the store under workspace.
func NewPendingStore(workspace string) *PendingStore {
	return &PendingStore{path: filepath.Join(workspace, "pending", "requests.json")}
}

func (s *PendingStore) load() map[string]*PendingRequest {
	out := map[string]*PendingRequest{}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return out
	}
	var list []*PendingRequest
	if err := json.Unmarshal(data, &list); err != nil {
		return out
	}
	for _, r := range list {
		if r != nil && r.ID != "" {
			out[r.ID] = r
		}
	}
	return out
}

func (s *PendingStore) saveAll(all map[string]*PendingRequest) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	list := make([]*PendingRequest, 0, len(all))
	for _, r := range all {
		list = append(list, r)
	}
	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Create stores a new pending request with a unique ID and TTL.
func (s *PendingStore) Create(sessionID, capability, skill, missingField, question, intent string, ttl time.Duration, continuation map[string]string) *PendingRequest {
	if ttl <= 0 {
		ttl = pendingTTL
	}
	now := time.Now()
	r := &PendingRequest{
		ID:        now.Format("20060102") + "-" + newRequestID()[:12],
		SessionID: sessionID, Capability: capability, Skill: skill,
		MissingField: missingField, Question: question,
		Intent:       SanitizeIntent(intent),
		Continuation: continuation,
		Status:       PendingOpen,
		CreatedAt:    now, ExpiresAt: now.Add(ttl),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	all[r.ID] = r
	_ = s.saveAll(all)
	return r
}

// OpenForSession returns the newest open, unexpired request for a session.
func (s *PendingStore) OpenForSession(sessionID string) (*PendingRequest, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	var best *PendingRequest
	changed := false
	now := time.Now()
	for _, r := range all {
		if r.SessionID != sessionID {
			continue
		}
		if r.Status == PendingOpen && now.After(r.ExpiresAt) {
			r.Status = PendingExpired
			changed = true
			continue
		}
		if r.Status != PendingOpen {
			continue
		}
		if best == nil || r.CreatedAt.After(best.CreatedAt) {
			best = r
		}
	}
	if changed {
		_ = s.saveAll(all)
	}
	if best == nil {
		return nil, false
	}
	return best, true
}

// Complete marks a request completed (resumed work finished).
func (s *PendingStore) Complete(id string) bool {
	return s.setStatus(id, PendingCompleted)
}

// Cancel marks a request cancelled.
func (s *PendingStore) Cancel(id string) bool {
	return s.setStatus(id, PendingCancelled)
}

func (s *PendingStore) setStatus(id string, st PendingStatus) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	all := s.load()
	r, ok := all[id]
	if !ok {
		return false
	}
	now := time.Now()
	r.Status = st
	r.CompletedAt = &now
	_ = s.saveAll(all)
	return true
}
