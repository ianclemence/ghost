// Package turnlog gives every user chat turn a durable identity so that a
// mobile reconnect can NEVER re-submit the same intent and duplicate a
// consequential side effect.
//
// A turn is keyed by (conversation session, request_id) — the client's
// stable correlation identity. Claim is create-once and atomic: the second
// identical request attaches to the existing record instead of starting a
// second execution. Reconnect is observation/resumption, not re-submission.
package turnlog

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Status is the durable lifecycle of one turn.
type Status string

const (
	StatusPending     Status = "pending"
	StatusRunning     Status = "running"
	StatusWaiting     Status = "waiting" // waiting_for_permission / waiting_for_user
	StatusCompleted   Status = "completed"
	StatusFailed      Status = "failed"
	StatusInterrupted Status = "interrupted" // process died mid-turn
)

// Turn is one durable user turn record. TrajectoryID is the execution
// trace identity for the turn: one ID connects the session turn, model
// calls, memory retrieval, tool execution, verification, and recovery.
// A repeat claim (mobile reconnect) returns the SAME trajectory ID —
// reconnect observes the existing execution, it never forks a new trace.
type Turn struct {
	SessionID    string    `json:"session_id"`
	RequestID    string    `json:"request_id"`
	TrajectoryID string    `json:"trajectory_id,omitempty"`
	Status       Status    `json:"status"`
	Outcome      string    `json:"outcome,omitempty"` // product outcome when terminal
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`

	// Partial is the reply text written so far, checkpointed while the
	// model streams. The reply used to live only in memory, so a restart
	// mid-sentence lost every word already spoken; the transcript ended
	// with an answered question and no answer. Checkpointed here, the
	// process can put it back — a crash reads as a pause.
	Partial   string    `json:"partial,omitempty"`
	PartialAt time.Time `json:"partial_at,omitempty"`

	// Origin is the surface the turn arrived on, recorded once at claim so
	// a resumed turn answers where the owner actually asked.
	UserText string `json:"user_text,omitempty"`
	Channel  string `json:"channel,omitempty"`
	ChatID   string `json:"chat_id,omitempty"`

	// InterruptedAt is the last moment the turn was known to be alive (the
	// last checkpoint), not the moment recovery noticed — so a Pod that was
	// off all night does not present yesterday's dead turn as fresh.
	InterruptedAt time.Time `json:"interrupted_at,omitempty"`
	// Materialized marks the partial already written into the transcript,
	// so recovery can never append the same half-reply twice.
	Materialized bool `json:"materialized,omitempty"`
	// ResumedBy is the request id of the turn that finished the work.
	ResumedBy string `json:"resumed_by,omitempty"`
}

// trajectoryIDContextKey carries a turn's trajectory ID through the agent
// loop, tool execution, and verification without threading it through every
// function signature (same pattern as tools.WithSessionKey).
type trajectoryIDContextKey struct{}

// NewTrajectoryID mints a trajectory identity: "trj_" + 16 hex chars from
// crypto entropy. Collisions are not retried — 64 bits per turn is enough
// that a birthday collision is not a planning input.
func NewTrajectoryID() string {
	var buf [8]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("trj-%d", time.Now().UTC().UnixNano())
	}
	return "trj_" + hex.EncodeToString(buf[:])
}

// WithTrajectoryID attaches the turn's trajectory ID to ctx. Empty IDs are
// ignored so background work without a turn keeps a clean context.
func WithTrajectoryID(ctx context.Context, trajectoryID string) context.Context {
	if ctx == nil || trajectoryID == "" {
		return ctx
	}
	return context.WithValue(ctx, trajectoryIDContextKey{}, trajectoryID)
}

// TrajectoryIDFromContext returns the turn's trajectory ID, or "" when the
// work has no turn (startup, cron without a turn, tests).
func TrajectoryIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(trajectoryIDContextKey{}).(string)
	return id
}

// ClaimResult reports the outcome of attempting to claim a turn.
type ClaimResult struct {
	Created bool   // true => this request won the right to execute
	Status  Status // existing status when not created
	Turn    *Turn
}

// Store persists turn records as atomic JSON files under a state dir.
type Store struct {
	dir string
	mu  sync.Mutex
	// lastCk is the throttle stamp per live turn, so a chunk-by-chunk
	// checkpoint does not turn every token into a disk write.
	lastCk map[string]checkpointMark
}

// checkpointInterval and checkpointByteStep bound how often a streaming
// reply is flushed: at most one write every interval, unless the reply has
// grown by a whole step, which is when a lost chunk would actually hurt.
const (
	checkpointInterval = 400 * time.Millisecond
	checkpointByteStep = 2048
)

type checkpointMark struct {
	at time.Time
	n  int
}

// New creates the store under dir (created if absent).
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir, lastCk: map[string]checkpointMark{}}, nil
}

func key(sessionID, requestID string) string {
	h := sha256.Sum256([]byte(sessionID + "\x00" + requestID))
	return hex.EncodeToString(h[:12])
}

func (s *Store) path(k string) string { return filepath.Join(s.dir, k+".json") }

// Claim atomically claims the right to execute a turn. On first request it
// creates a pending record and returns Created=true. On an identical repeat
// it returns the existing status so the caller can attach/replay and must
// NOT execute again.
func (s *Store) Claim(sessionID, requestID string) (ClaimResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.path(key(sessionID, requestID))
	now := time.Now().UTC()
	rec := &Turn{SessionID: sessionID, RequestID: requestID, Status: StatusPending,
		TrajectoryID: NewTrajectoryID(), CreatedAt: now, UpdatedAt: now}
	data, _ := json.Marshal(rec)
	// create-once: O_EXCL makes two racing identical requests resolve to one
	// winner deterministically.
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if !os.IsExist(err) {
			return ClaimResult{}, err
		}
		existing, err := s.loadLocked(sessionID, requestID)
		if err != nil {
			return ClaimResult{}, err
		}
		return ClaimResult{Created: false, Status: existing.Status, Turn: existing}, nil
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		return ClaimResult{}, fmt.Errorf("claim persist failed")
	}
	return ClaimResult{Created: true, Status: StatusPending, Turn: rec}, nil
}

// Set transitions a turn record. Terminal states are immutable.
func (s *Store) Set(sessionID, requestID string, st Status, outcome string) (*Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.loadLocked(sessionID, requestID)
	if err != nil || t == nil {
		return nil, fmt.Errorf("turn not found")
	}
	if t.Status == StatusCompleted || t.Status == StatusFailed || t.Status == StatusInterrupted {
		return nil, fmt.Errorf("turn %s is already terminal (%s)", requestID, t.Status)
	}
	t.Status = st
	if outcome != "" {
		t.Outcome = outcome
	}
	// A reply that finished is in the transcript, so the checkpoint is
	// retired — recovering it later would append the same words twice. A
	// reply that failed or was cut off keeps its partial: that text is
	// nowhere else, and losing it is the amnesia recovery exists to stop.
	if st == StatusCompleted {
		t.Partial = ""
		t.PartialAt = time.Time{}
		t.Materialized = true
		delete(s.lastCk, key(sessionID, requestID))
	}
	t.UpdatedAt = time.Now().UTC()
	if err := s.writeLocked(t); err != nil {
		return nil, err
	}
	cp := *t
	return &cp, nil
}

// SetOrigin records the surface a turn arrived on. Called once, right after
// the claim wins, so a later resume knows what was asked and where from
// without re-reading the transcript.
func (s *Store) SetOrigin(sessionID, requestID, userText, channel, chatID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.loadLocked(sessionID, requestID)
	if err != nil || t == nil {
		return err
	}
	if t.UserText == userText && t.Channel == channel && t.ChatID == chatID {
		return nil
	}
	t.UserText = userText
	t.Channel = channel
	t.ChatID = chatID
	return s.writeLocked(t)
}

// Checkpoint durably records the reply text written so far. It is throttled
// by default (a token stream must not become a token stream of fsyncs) and
// refuses terminal turns, whose partial has already been settled one way or
// the other.
func (s *Store) Checkpoint(sessionID, requestID, text string, force bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	k := key(sessionID, requestID)
	now := time.Now()
	if !force {
		if m, ok := s.lastCk[k]; ok {
			if now.Sub(m.at) < checkpointInterval && len(text)-m.n < checkpointByteStep {
				return nil
			}
		}
	}
	t, err := s.loadLocked(sessionID, requestID)
	if err != nil || t == nil {
		return err
	}
	switch t.Status {
	case StatusCompleted, StatusFailed, StatusInterrupted:
		return nil // terminal: already settled
	}
	t.Partial = text
	t.PartialAt = now.UTC()
	t.UpdatedAt = t.PartialAt
	if err := s.writeLocked(t); err != nil {
		return err
	}
	s.lastCk[k] = checkpointMark{at: now, n: len(text)}
	return nil
}

// PendingPartials returns terminal turns whose partial reply has not yet
// reached the transcript. Recovery writes each one in exactly once.
func (s *Store) PendingPartials() ([]*Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Turn
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		t, err := s.loadNameLocked(e.Name())
		if err != nil || t == nil || t.Materialized || t.Partial == "" {
			continue
		}
		switch t.Status {
		case StatusInterrupted, StatusFailed:
			cp := *t
			out = append(out, &cp)
		}
	}
	return out, nil
}

// MarkMaterialized records that a turn's partial reply is now a row in the
// transcript, so it can never be appended a second time.
func (s *Store) MarkMaterialized(sessionID, requestID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.loadLocked(sessionID, requestID)
	if err != nil || t == nil {
		return err
	}
	t.Materialized = true
	t.Partial = ""
	t.PartialAt = time.Time{}
	delete(s.lastCk, key(sessionID, requestID))
	return s.writeLocked(t)
}

// MarkResumed links an interrupted turn to the turn that finished its work.
func (s *Store) MarkResumed(sessionID, requestID, resumedBy string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, err := s.loadLocked(sessionID, requestID)
	if err != nil || t == nil {
		return err
	}
	t.ResumedBy = resumedBy
	return s.writeLocked(t)
}

// Resumable returns interrupted turns that died within window and have not
// already been picked back up. The window is measured from InterruptedAt —
// the last moment the turn was demonstrably alive — so a Pod that was off
// overnight does not resurrect a turn the owner has long stopped expecting.
func (s *Store) Resumable(now time.Time, window time.Duration) ([]*Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []*Turn
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		t, err := s.loadNameLocked(e.Name())
		if err != nil || t == nil || t.Status != StatusInterrupted || t.ResumedBy != "" {
			continue
		}
		dead := t.InterruptedAt
		if dead.IsZero() {
			dead = t.UpdatedAt
		}
		if dead.IsZero() || now.Sub(dead) > window || now.Sub(dead) < 0 {
			continue
		}
		cp := *t
		out = append(out, &cp)
	}
	return out, nil
}

// HasNewerTurn reports whether the session has a turn that started after
// this one. When the owner already moved on, finishing the old reply would
// inject it out of order, so recovery must leave it alone.
func (s *Store) HasNewerTurn(t *Turn) (bool, error) {
	if t == nil {
		return false, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return false, err
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		o, err := s.loadNameLocked(e.Name())
		if err != nil || o == nil || o.SessionID != t.SessionID {
			continue
		}
		if o.RequestID == t.RequestID {
			continue
		}
		if o.CreatedAt.After(t.CreatedAt) {
			return true, nil
		}
	}
	return false, nil
}

// Recover marks turns that were running/pending at a crash as interrupted so
// a later identical request is told the truth instead of being refused
// forever. Call once at startup with the process start time.
func (s *Store) Recover(processStart time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		t, err := s.loadNameLocked(e.Name())
		if err != nil || t == nil {
			continue
		}
		if t.CreatedAt.After(processStart) {
			continue
		}
		if t.Status == StatusRunning || t.Status == StatusPending || t.Status == StatusWaiting {
			// InterruptedAt is when the turn was last seen alive (its last
			// checkpoint), not now: freshness must be measured from the
			// crash, so an overnight outage does not look like a turn that
			// just stopped.
			if !t.PartialAt.IsZero() {
				t.InterruptedAt = t.PartialAt
			} else if !t.UpdatedAt.IsZero() {
				t.InterruptedAt = t.UpdatedAt
			} else {
				t.InterruptedAt = t.CreatedAt
			}
			t.Status = StatusInterrupted
			t.UpdatedAt = time.Now().UTC()
			if err := s.writeLocked(t); err == nil {
				n++
			}
		}
	}
	return n, nil
}

// Get returns one turn record (or nil).
func (s *Store) Get(sessionID, requestID string) (*Turn, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked(sessionID, requestID)
}

func (s *Store) loadLocked(sessionID, requestID string) (*Turn, error) {
	return s.loadNameLocked(key(sessionID, requestID) + ".json")
}

func (s *Store) loadNameLocked(name string) (*Turn, error) {
	data, err := os.ReadFile(filepath.Join(s.dir, name))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var t Turn
	if err := json.Unmarshal(data, &t); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) writeLocked(t *Turn) error {
	data, err := json.Marshal(t)
	if err != nil {
		return err
	}
	p := s.path(key(t.SessionID, t.RequestID))
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// PruneTerminal deletes turn records that reached a terminal state before the
// cutoff. Terminal turns exist only so a reconnecting client can observe the
// outcome; after the retention window they carry no authority. Live turns
// (pending/running/waiting) are never pruned. Returns the number removed.
func (s *Store) PruneTerminal(before time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, de := range entries {
		if de.IsDir() || filepath.Ext(de.Name()) != ".json" {
			continue
		}
		p := filepath.Join(s.dir, de.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var t Turn
		if json.Unmarshal(data, &t) != nil {
			continue
		}
		switch t.Status {
		case StatusCompleted, StatusFailed, StatusInterrupted:
		default:
			continue // live turn: never prune
		}
		if t.UpdatedAt.Before(before) {
			if os.Remove(p) == nil {
				removed++
			}
		}
	}
	return removed, nil
}
