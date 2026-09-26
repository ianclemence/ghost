// Package commitments is the durable obligation ledger: things the owner
// said they intend to do, with the provenance and the time they attach to.
//
// It is deliberately not a second memory system. Memory holds facts about the
// owner ("my gym is Downtown Fitness"); a commitment is a piece of work with a
// lifecycle — open, blocked, completed, cancelled — that the proactive runtime
// can watch, propose action for, and settle from real execution results. The
// owner's exact words remain the record (Text), and the originating session,
// message id and verbatim quote stay attached, so nothing here is an
// interpretation that cannot be traced back to something the owner said.
//
// Commitments never execute anything and never grant authority. They are
// state the runtime observes.
package commitments

import (
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

// Status is a commitment's lifecycle state. Only Open and Blocked can produce
// a proactive opportunity; the rest are settled and stay quiet.
type Status string

const (
	StatusOpen      Status = "open"
	StatusBlocked   Status = "blocked"
	StatusCompleted Status = "completed"
	StatusCancelled Status = "cancelled"
	StatusExpired   Status = "expired"
)

// Settled reports whether the commitment needs no further attention.
func (s Status) Settled() bool {
	switch s {
	case StatusCompleted, StatusCancelled, StatusExpired:
		return true
	}
	return false
}

// Kind is the closed vocabulary of obligation shapes. It exists so the runtime
// can reason deterministically about what an obligation would need; it is
// never used to invent arguments (a recipient address, a file path) that the
// owner did not supply.
type Kind string

const (
	KindSend   Kind = "send"
	KindEmail  Kind = "email"
	KindCall   Kind = "call"
	KindBuy    Kind = "buy"
	KindPay    Kind = "pay"
	KindBook   Kind = "book"
	KindWrite  Kind = "write"
	KindSubmit Kind = "submit"
	KindOther  Kind = "other"
)

// ValidKind reports whether k is in the closed vocabulary.
func ValidKind(k Kind) bool {
	switch k {
	case KindSend, KindEmail, KindCall, KindBuy, KindPay, KindBook, KindWrite, KindSubmit, KindOther:
		return true
	}
	return false
}

// Messaging reports whether the obligation is fulfilled by communicating
// something to someone. Those need a live channel to be worth offering as an
// action; without one the honest offer is a reminder instead.
func (k Kind) Messaging() bool {
	return k == KindSend || k == KindEmail || k == KindCall
}

// Action reports whether the obligation names something the world outside
// Ghost must do (as opposed to a purely personal task).
func (k Kind) Action() string {
	switch k {
	case KindSend:
		return "send it"
	case KindEmail:
		return "send that email"
	case KindCall:
		return "make that call"
	case KindBuy:
		return "get it sorted"
	case KindPay:
		return "pay it"
	case KindBook:
		return "book it"
	case KindWrite:
		return "write it"
	case KindSubmit:
		return "send it off"
	default:
		return "take care of it"
	}
}

// Provenance ties a commitment back to the words that created it. Every field
// is required for a commitment to exist: an obligation the owner cannot be
// shown the origin of is not one Ghost may act on.
type Provenance struct {
	Session   string    `json:"session"`
	MessageID string    `json:"message_id"`
	Quote     string    `json:"quote"`
	At        time.Time `json:"at"`
}

// Commitment is one durable obligation.
type Commitment struct {
	ID      string `json:"id"`
	Text    string `json:"text"`              // the obligation, in the owner's words
	Subject string `json:"subject,omitempty"` // entity involved (a person, a project), when the owner named one
	Kind    Kind   `json:"kind"`

	// DueAt is set only when a time phrase in the owner's words resolved
	// through the scheduler's existing natural-language parser. The phrase
	// itself is kept so the resolution is auditable.
	DueAt     *time.Time `json:"due_at,omitempty"`
	DueSource string     `json:"due_source,omitempty"`

	Status     Status  `json:"status"`
	Confidence float64 `json:"confidence"`
	// Origin is "deterministic" or "model" — how the commitment was derived,
	// so a weak derivation can be told apart from a stated one.
	Origin     string     `json:"origin"`
	Provenance Provenance `json:"provenance"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// Attempts and the outcome fields are written only from runtime results.
	Attempts      int        `json:"attempts,omitempty"`
	LastAttemptAt *time.Time `json:"last_attempt_at,omitempty"`
	Outcome       string     `json:"outcome,omitempty"` // completed | failed | blocked | cancelled | dismissed
	OutcomeNote   string     `json:"outcome_note,omitempty"`

	// DedupeKey is the stable identity of the obligation: the same promise
	// restated does not become a second commitment.
	DedupeKey string `json:"dedupe_key,omitempty"`
}

// Overdue reports whether an open commitment's time has passed.
func (c Commitment) Overdue(now time.Time) bool {
	return c.Status == StatusOpen && c.DueAt != nil && !now.Before(*c.DueAt)
}

// Stalled reports whether an open commitment with no time on it has been
// waiting longer than the given window.
func (c Commitment) Stalled(now time.Time, after time.Duration) bool {
	if c.Status != StatusOpen || c.DueAt != nil {
		return false
	}
	return now.Sub(c.CreatedAt) >= after
}

// Store persists commitments in the workspace. Writes are atomic, matching
// the goal store: the file is small and single-writer correctness matters
// more than per-row updates.
type Store struct {
	path string
	mu   sync.Mutex
}

// Dir is the workspace-relative directory commitments live in.
const Dir = "commitments"

// New opens (creating if needed) the commitment ledger for a workspace.
func New(workspace string) (*Store, error) {
	if strings.TrimSpace(workspace) == "" {
		return nil, errors.New("workspace is required")
	}
	dir := filepath.Join(workspace, Dir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(dir, "commitments.json")}, nil
}

func newID() string {
	return fmt.Sprintf("cm-%d", time.Now().UTC().UnixNano())
}

func (s *Store) loadLocked() ([]Commitment, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(raw) == 0 {
		return nil, nil
	}
	var out []Commitment
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) saveLocked(list []Commitment) error {
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// Create stores a new commitment. It refuses records that could not be honest:
// no text, no provenance quote, no origin. A restatement of an identical open
// obligation (same dedupe key) is a no-op that returns the existing record.
func (s *Store) Create(c Commitment) (Commitment, error) {
	if strings.TrimSpace(c.Text) == "" {
		return Commitment{}, errors.New("commitment text is required")
	}
	if strings.TrimSpace(c.Provenance.Quote) == "" {
		return Commitment{}, errors.New("commitment requires the owner's own words as provenance")
	}
	if strings.TrimSpace(c.Origin) == "" {
		return Commitment{}, errors.New("commitment requires an origin")
	}
	if !ValidKind(c.Kind) {
		c.Kind = KindOther
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.loadLocked()
	if err != nil {
		return Commitment{}, err
	}
	if c.DedupeKey != "" {
		for i := range list {
			if list[i].DedupeKey == c.DedupeKey && !list[i].Status.Settled() {
				return list[i], nil
			}
		}
	}
	now := time.Now().UTC()
	c.ID = newID()
	c.CreatedAt = now
	c.UpdatedAt = now
	if c.Status == "" {
		c.Status = StatusOpen
	}
	list = append(list, c)
	if err := s.saveLocked(list); err != nil {
		return Commitment{}, err
	}
	return c, nil
}

// Get returns one commitment by exact id.
func (s *Store) Get(id string) (Commitment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.loadLocked()
	if err != nil {
		return Commitment{}, err
	}
	for _, c := range list {
		if c.ID == id {
			return c, nil
		}
	}
	return Commitment{}, fmt.Errorf("no commitment %q", id)
}

// List returns every commitment, oldest first.
func (s *Store) List() ([]Commitment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	sort.Slice(list, func(i, j int) bool { return list[i].CreatedAt.Before(list[j].CreatedAt) })
	return list, nil
}

// Attention returns the commitments that may produce a proactive
// opportunity: open, and either past due or waiting without a time.
func (s *Store) Attention(now time.Time, stallAfter time.Duration) ([]Commitment, error) {
	list, err := s.List()
	if err != nil {
		return nil, err
	}
	var out []Commitment
	for _, c := range list {
		if c.Overdue(now) || c.Stalled(now, stallAfter) {
			out = append(out, c)
		}
	}
	return out, nil
}

// Count reports how many commitments sit in each status, for owner surfaces.
func (s *Store) Count() (open, blocked, settled int, err error) {
	list, lerr := s.List()
	if lerr != nil {
		return 0, 0, 0, lerr
	}
	for _, c := range list {
		switch c.Status {
		case StatusOpen:
			open++
		case StatusBlocked:
			blocked++
		default:
			settled++
		}
	}
	return open, blocked, settled, nil
}

// mutate applies fn to one commitment under the store lock.
func (s *Store) mutate(id string, fn func(*Commitment) error) (Commitment, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.loadLocked()
	if err != nil {
		return Commitment{}, err
	}
	for i := range list {
		if list[i].ID != id {
			continue
		}
		if err := fn(&list[i]); err != nil {
			return list[i], err
		}
		list[i].UpdatedAt = time.Now().UTC()
		if err := s.saveLocked(list); err != nil {
			return Commitment{}, err
		}
		return list[i], nil
	}
	return Commitment{}, fmt.Errorf("no commitment %q", id)
}

// Settle records the real outcome of work Ghost did on this commitment. It is
// the memory half of the loop: a failed action must NOT complete the
// obligation, and a dismissal must not silently complete it either.
func (s *Store) Settle(id string, status Status, outcome, note string) (Commitment, error) {
	return s.mutate(id, func(c *Commitment) error {
		switch status {
		case StatusOpen, StatusBlocked, StatusCompleted, StatusCancelled, StatusExpired:
		default:
			return fmt.Errorf("unknown commitment status %q", status)
		}
		// A settled commitment stays settled: a late failure must not reopen
		// work the owner already closed, and a late success must not
		// re-complete a cancelled obligation.
		if c.Status.Settled() && status != c.Status {
			return fmt.Errorf("commitment is already %s", c.Status)
		}
		c.Status = status
		c.Outcome = outcome
		c.OutcomeNote = note
		return nil
	})
}

// MarkAttempt records that Ghost worked on the obligation, so a repeated
// failure does not look like a first try.
func (s *Store) MarkAttempt(id string) (Commitment, error) {
	return s.mutate(id, func(c *Commitment) error {
		now := time.Now().UTC()
		c.Attempts++
		c.LastAttemptAt = &now
		return nil
	})
}

// Cancel closes an obligation by owner decision.
func (s *Store) Cancel(id, note string) (Commitment, error) {
	return s.Settle(id, StatusCancelled, "cancelled", note)
}

// DueKey turns the owner's obligation into a stable identity. Time is included
// only to the day: "send Alex the photos Friday" restated is the same
// obligation, while the same words with a different day are a new one.
func DueKey(text, subject string, due *time.Time) string {
	key := normalize(text)
	if s := normalize(subject); s != "" {
		key += "|" + s
	}
	if due != nil {
		key += "|" + due.UTC().Format("2006-01-02")
	}
	return key
}

func normalize(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}
