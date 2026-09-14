// Package goals owns Ghost's standing goals: durable owner intents like
// "take care of school emails" that outlive any single turn. A goal is the
// *why*; routines/schedules linked to it are the *how*. The heartbeat and
// proactive engine evaluate goals on every tick; expiry auto-pauses.
//
// Authority: goals narrow action (capability allowlist can only shrink
// after creation). Creating a goal is low-risk local state; everything a
// goal triggers still passes the permission broker and capability evidence.
package goals

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// Status is the lifecycle state of a goal.
type Status string

const (
	StatusActive    Status = "active"
	StatusPaused    Status = "paused"
	StatusCompleted Status = "completed"
	StatusExpired   Status = "expired"
)

// ProgressNote is one timestamped progress entry on a goal.
type ProgressNote struct {
	At   time.Time `json:"at"`
	Text string    `json:"text"`
}

// Goal is one durable owner intent.
type Goal struct {
	// ID is stable (goal_<16 hex>), never reused.
	ID string `json:"id"`
	// Text is the owner's words, e.g. "take care of school emails".
	Text string `json:"text"`
	// Scope bounds what the goal may touch (e.g. "school.edu inbox").
	Scope string `json:"scope,omitempty"`
	// Success describes done in plain language.
	Success string `json:"success,omitempty"`
	// Capabilities narrows which capability IDs goal-triggered turns may
	// use. Narrowing-only after creation: updates can remove, never add.
	Capabilities []string `json:"capabilities,omitempty"`
	// RoutineIDs links schedules that serve this goal.
	RoutineIDs []string `json:"routine_ids,omitempty"`
	// Status lifecycle; ExpiresAt auto-pauses via Evaluate.
	Status Status `json:"status"`
	// ExpiresAt zero means no expiry.
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Progress  []ProgressNote `json:"progress,omitempty"`
}

// Usable reports whether the goal should be evaluated on ticks.
func (g Goal) Usable(now time.Time) bool {
	if g.Status != StatusActive {
		return false
	}
	if !g.ExpiresAt.IsZero() && !now.Before(g.ExpiresAt) {
		return false
	}
	return true
}

// Store persists goals in workspace/goals/goals.json (atomic writes).
type Store struct {
	mu   sync.Mutex
	dir  string
	path string
}

func NewStore(workspace string) *Store {
	dir := filepath.Join(workspace, "goals")
	return &Store{dir: dir, path: filepath.Join(dir, "goals.json")}
}

func newID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("goal_%d", time.Now().UnixNano())
	}
	return "goal_" + hex.EncodeToString(buf)
}

func (s *Store) loadLocked() ([]Goal, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Goal
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) saveLocked(goals []Goal) error {
	if err := os.MkdirAll(s.dir, 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(goals, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Create stores a new active goal.
func (s *Store) Create(text, scope, success string, capabilities []string, expiresAt time.Time) (Goal, error) {
	text = trim(text)
	if text == "" {
		return Goal{}, errors.New("goal text is required")
	}
	now := time.Now()
	g := Goal{
		ID: newID(), Text: text, Scope: trim(scope), Success: trim(success),
		Capabilities: append([]string(nil), capabilities...),
		Status:       StatusActive, CreatedAt: now, UpdatedAt: now, ExpiresAt: expiresAt,
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	goals, err := s.loadLocked()
	if err != nil {
		return Goal{}, err
	}
	goals = append(goals, g)
	if err := s.saveLocked(goals); err != nil {
		return Goal{}, err
	}
	return g, nil
}

// List returns all goals in creation order, auto-pausing expired actives.
func (s *Store) List(now time.Time) ([]Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	goals, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	dirty := false
	for i := range goals {
		if goals[i].Status == StatusActive && !goals[i].ExpiresAt.IsZero() && !now.Before(goals[i].ExpiresAt) {
			goals[i].Status = StatusExpired
			goals[i].UpdatedAt = now
			dirty = true
		}
	}
	if dirty {
		if err := s.saveLocked(goals); err != nil {
			return nil, err
		}
	}
	out := append([]Goal(nil), goals...)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out, nil
}

// Get returns one goal by ID.
func (s *Store) Get(id string) (Goal, error) {
	goals, err := s.List(time.Now())
	if err != nil {
		return Goal{}, err
	}
	for _, g := range goals {
		if g.ID == id {
			return g, nil
		}
	}
	return Goal{}, errors.New("goal not found")
}

func (s *Store) mutate(id string, fn func(*Goal) error) (Goal, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	goals, err := s.loadLocked()
	if err != nil {
		return Goal{}, err
	}
	for i := range goals {
		if goals[i].ID == id {
			if err := fn(&goals[i]); err != nil {
				return Goal{}, err
			}
			goals[i].UpdatedAt = time.Now()
			if err := s.saveLocked(goals); err != nil {
				return Goal{}, err
			}
			return goals[i], nil
		}
	}
	return Goal{}, errors.New("goal not found")
}

// Pause halts evaluation without deleting history.
func (s *Store) Pause(id string) (Goal, error) {
	return s.mutate(id, func(g *Goal) error {
		if g.Status == StatusCompleted {
			return errors.New("completed goal cannot be paused")
		}
		g.Status = StatusPaused
		return nil
	})
}

// Resume re-activates a paused (or expired-renewed) goal.
func (s *Store) Resume(id string) (Goal, error) {
	return s.mutate(id, func(g *Goal) error {
		if g.Status == StatusCompleted {
			return errors.New("completed goal cannot be resumed")
		}
		g.Status = StatusActive
		return nil
	})
}

// Complete marks the goal done (terminal).
func (s *Store) Complete(id string) (Goal, error) {
	return s.mutate(id, func(g *Goal) error {
		g.Status = StatusCompleted
		return nil
	})
}

// NarrowCapabilities replaces the allowlist with a subset of itself.
// Widening is refused: goal authority only ever shrinks.
func (s *Store) NarrowCapabilities(id string, next []string) (Goal, error) {
	return s.mutate(id, func(g *Goal) error {
		allowed := map[string]bool{}
		for _, c := range g.Capabilities {
			allowed[c] = true
		}
		for _, c := range next {
			if len(g.Capabilities) > 0 && !allowed[c] {
				return fmt.Errorf("cannot widen goal capabilities with %q", c)
			}
		}
		g.Capabilities = append([]string(nil), next...)
		return nil
	})
}

// LinkRoutine attaches a routine ID (idempotent).
func (s *Store) LinkRoutine(id, routineID string) (Goal, error) {
	return s.mutate(id, func(g *Goal) error {
		for _, r := range g.RoutineIDs {
			if r == routineID {
				return nil
			}
		}
		g.RoutineIDs = append(g.RoutineIDs, routineID)
		return nil
	})
}

// AppendProgress records a timestamped note (bounded to the last 200).
func (s *Store) AppendProgress(id, text string) (Goal, error) {
	text = trim(text)
	if text == "" {
		return Goal{}, errors.New("progress text is required")
	}
	return s.mutate(id, func(g *Goal) error {
		g.Progress = append(g.Progress, ProgressNote{At: time.Now(), Text: text})
		if len(g.Progress) > 200 {
			g.Progress = g.Progress[len(g.Progress)-200:]
		}
		return nil
	})
}

func trim(s string) string {
	if len(s) > 2000 {
		s = s[:2000]
	}
	return strings.TrimSpace(s)
}
