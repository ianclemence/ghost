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

// Turn is one durable user turn record.
type Turn struct {
	SessionID string    `json:"session_id"`
	RequestID string    `json:"request_id"`
	Status    Status    `json:"status"`
	Outcome   string    `json:"outcome,omitempty"` // product outcome when terminal
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
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
}

// New creates the store under dir (created if absent).
func New(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
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
		CreatedAt: now, UpdatedAt: now}
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
	t.UpdatedAt = time.Now().UTC()
	if err := s.writeLocked(t); err != nil {
		return nil, err
	}
	cp := *t
	return &cp, nil
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
