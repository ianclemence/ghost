// Package deviceops is a tiny durable store for appliance operations
// (restart, update). Operations survive process death so a mobile client can
// request a reboot, lose the connection, reconnect, and read the final state.
// File-backed under the workspace state dir (no new database). Never accepts
// commands from callers: an operation is a fixed, server-defined action.
package deviceops

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// State is the durable lifecycle of one device operation.
type State string

const (
	StateScheduled State = "scheduled"
	StateStarting  State = "starting"
	StateRunning   State = "running"
	StateRebooting State = "rebooting"
	StateVerifying State = "verifying"
	StateCompleted State = "completed"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
)

// Operation is one durable device operation.
type Operation struct {
	ID          string    `json:"id"`
	Action      string    `json:"action"` // restart | update
	State       State     `json:"state"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	RequestedBy string    `json:"requested_by"` // authenticated device id
	Detail      string    `json:"detail,omitempty"`
}

// Store persists operations as atomic JSON files.
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

func (s *Store) path(id string) string { return filepath.Join(s.dir, id+".json") }

func validID(id string) bool {
	if id == "" || len(id) > 80 {
		return false
	}
	for _, r := range id {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-') {
			return false
		}
	}
	return true
}

// Create persists a new scheduled operation and returns it.
func (s *Store) Create(action, requestedBy string) (*Operation, error) {
	if action != "restart" && action != "update" {
		return nil, fmt.Errorf("unknown device operation %q", action)
	}
	id := fmt.Sprintf("devop-%d", time.Now().UnixNano())
	op := &Operation{
		ID: id, Action: action, State: StateScheduled,
		CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		RequestedBy: requestedBy,
	}
	if err := s.save(op); err != nil {
		return nil, err
	}
	return op, nil
}

// Transition moves an operation to a new state. Unknown ids and forged ids
// fail closed. Returns a copy.
func (s *Store) Transition(id string, st State, detail string) (*Operation, error) {
	if !validID(id) {
		return nil, fmt.Errorf("invalid operation id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	op, err := s.loadLocked(id)
	if err != nil || op == nil {
		return nil, fmt.Errorf("operation not found")
	}
	if op.State == StateCompleted || op.State == StateFailed || op.State == StateCancelled {
		return nil, fmt.Errorf("operation %s is already terminal (%s)", id, op.State)
	}
	op.State = st
	op.UpdatedAt = time.Now().UTC()
	if detail != "" {
		op.Detail = detail
	}
	if err := s.writeLocked(op); err != nil {
		return nil, err
	}
	cp := *op
	return &cp, nil
}

// Get returns a copy of one operation. A forged/unknown id fails closed.
func (s *Store) Get(id string) (*Operation, error) {
	if !validID(id) {
		return nil, fmt.Errorf("invalid operation id")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	op, err := s.loadLocked(id)
	if err != nil || op == nil {
		return nil, fmt.Errorf("operation not found")
	}
	cp := *op
	return &cp, nil
}

// List returns operations newest first.
func (s *Store) List() ([]Operation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, err
	}
	out := make([]Operation, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		op, err := s.loadLocked(strings.TrimSuffix(e.Name(), ".json"))
		if err == nil && op != nil {
			out = append(out, *op)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Reconcile marks pre-restart in-flight operations as completed/failed once
// the device is back. Only operations created before processStart and still
// non-terminal are touched; a genuine new request is never auto-completed.
// Returns number reconciled.
func (s *Store) Reconcile(processStart time.Time, now time.Time) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		op, err := s.loadLocked(strings.TrimSuffix(e.Name(), ".json"))
		if err != nil || op == nil {
			continue
		}
		if op.CreatedAt.After(processStart) {
			continue // a request made after this boot is still live
		}
		if op.State == StateCompleted || op.State == StateFailed || op.State == StateCancelled {
			continue
		}
		// The device returned; the pre-restart operation is complete.
		st := StateCompleted
		if now.Sub(op.CreatedAt) > 2*time.Hour {
			st = StateFailed // took far too long to come back
		}
		op.State = st
		op.UpdatedAt = now
		op.Detail = "device returned; operation reconciled at boot"
		if err := s.writeLocked(op); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func (s *Store) save(op *Operation) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writeLocked(op)
}

func (s *Store) writeLocked(op *Operation) error {
	data, err := json.MarshalIndent(op, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path(op.ID) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path(op.ID))
}

func (s *Store) loadLocked(id string) (*Operation, error) {
	if !validID(id) {
		return nil, nil
	}
	data, err := os.ReadFile(s.path(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var op Operation
	if err := json.Unmarshal(data, &op); err != nil {
		return nil, err
	}
	return &op, nil
}
