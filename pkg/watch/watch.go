// Package watch is Ghost's durable observation of external mutable state.
//
// A watch is not a reminder, not a routine, and not memory. A reminder fires
// once at a stated time; a routine re-runs a command on a schedule; memory
// holds facts about the owner. A watch is the third thing: the runtime
// noticed that some thing in the world outside Ghost (a flight, an
// appointment, a delivery) will change on its own, and Ghost committed to
// checking it and telling the owner when the world actually moved.
//
// Every rule here is deterministic:
//
//   - Creation comes from a pattern over the owner's own words (detect.go),
//     gated by a fixed policy (policy.go) — the model never decides that a
//     watch exists.
//   - Polling is a clock function (cadence.go) over a bounded budget
//     (budget.go) — the model never decides when or whether to check.
//   - Change is a diff of two state maps (diff.go) — the model never
//     decides that something changed.
//   - Notification text is a template over observed values (render.go), and
//     every notice carries the evidence that produced it.
//
// The store persists at <workspace>/watches/watches.json with atomic writes,
// mirroring commitments.Store: small file, single writer, correctness over
// per-row speed.
package watch

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

// Kind is the closed vocabulary of watchable things. Each kind names an
// external object whose state changes without Ghost's involvement.
type Kind string

const (
	KindFlight      Kind = "flight"
	KindAppointment Kind = "appointment"
	KindReservation Kind = "reservation"
	KindDelivery    Kind = "delivery"
	KindEvent       Kind = "event"
)

// ValidKind reports whether k is in the closed vocabulary.
func ValidKind(k Kind) bool {
	switch k {
	case KindFlight, KindAppointment, KindReservation, KindDelivery, KindEvent, KindPage:
		return true
	}
	return false
}

// Status is a watch's lifecycle state. Only Active and Snoozed are polled.
type Status string

const (
	// StatusActive is polling normally.
	StatusActive Status = "active"
	// StatusTriggered fired a notice for its first meaningful change and
	// keeps polling (a gate change does not end a flight watch).
	StatusTriggered Status = "triggered"
	// StatusSnoozed is held back until a stated instant (quiet hours, or
	// the owner asking Ghost to stop nagging).
	StatusSnoozed Status = "snoozed"
	// StatusExpired passed its horizon without the owner closing it.
	StatusExpired Status = "expired"
	// StatusCompleted reached a natural end (the flight landed).
	StatusCompleted Status = "completed"
	// StatusFailed exhausted its probe retries; it says so rather than
	// pretending to keep watching.
	StatusFailed Status = "failed"
	// StatusDisabled was turned off by the owner.
	StatusDisabled Status = "disabled"
)

// Polled reports whether the watch may be probed at all.
func (s Status) Polled() bool {
	return s == StatusActive || s == StatusTriggered || s == StatusSnoozed
}

// Settled reports whether the watch needs no further attention.
func (s Status) Settled() bool {
	switch s {
	case StatusExpired, StatusCompleted, StatusFailed, StatusDisabled:
		return true
	}
	return false
}

// Provenance ties a watch back to the words that created it. The Quote is
// required: a watch Ghost cannot show the origin of is one it may not run.
type Provenance struct {
	Session   string    `json:"session"`
	MessageID string    `json:"message_id"`
	Quote     string    `json:"quote"`
	At        time.Time `json:"at"`
}

// Evidence is one runtime observation backing a notice. Everything a
// notification claims is reproducible from the evidence list.
type Evidence struct {
	At     time.Time `json:"at"`
	Field  string    `json:"field"`
	From   string    `json:"from,omitempty"`
	To     string    `json:"to,omitempty"`
	Source string    `json:"source"`
	Detail string    `json:"detail,omitempty"`
}

// DefaultMaxFailures is how many consecutive probe failures turn a watch
// StatusFailed. Below that, failures are retried with backoff and reported
// honestly (offline stays offline).
const DefaultMaxFailures = 5

// Watch is one durable observation of external mutable state.
type Watch struct {
	ID     string `json:"id"`
	Kind   Kind   `json:"kind"`
	Entity string `json:"entity"` // flight number, "dentist", order id…
	Label  string `json:"label"`  // owner-facing name
	// Source names the probe consulted for this watch (flight, page, sandbox).
	Source string `json:"source"`
	// URL and Rule belong to a page watch: the page, and what about it the
	// owner asked to hear (see page.go).
	URL  string    `json:"url,omitempty"`
	Rule *PageRule `json:"rule,omitempty"`

	// EventAt is the instant the watched thing happens, when the owner's
	// words carried one. It drives the polling cadence, never a guess.
	EventAt     *time.Time `json:"event_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	Status      Status     `json:"status"`
	Explicit    bool       `json:"explicit,omitempty"` // owner asked for it by name
	SnoozeUntil *time.Time `json:"snooze_until,omitempty"`

	// Baseline is the state at creation (or first successful probe);
	// Current is the latest observed state. Change is the diff between
	// them — never a model opinion.
	Baseline map[string]string `json:"baseline,omitempty"`
	Current  map[string]string `json:"current,omitempty"`

	// Evidence accumulates the observations that justify notices.
	Evidence []Evidence `json:"evidence,omitempty"`
	// Notified holds fingerprints already delivered (watch:<id>:<field>:
	// <from>→<to>), so a crash between change and delivery cannot re-send.
	Notified []string `json:"notified,omitempty"`
	// Suppressed counts notices held back per reason (quiet_hours,
	// cooldown, dedupe, budget).
	Suppressed map[string]int `json:"suppressed,omitempty"`

	Failures    int        `json:"failures,omitempty"`
	LastFailure string     `json:"last_failure,omitempty"`
	LastCheckAt *time.Time `json:"last_check_at,omitempty"`
	NextCheck   *time.Time `json:"next_check,omitempty"`

	Provenance Provenance `json:"provenance"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// DedupeKey is the stable identity of a watch: the same thing restated does
// not become a second watcher of the same state.
func (w Watch) DedupeKey() string {
	return string(w.Kind) + "|" + normalize(w.Entity)
}

// Live reports whether the watch is still being polled.
func (w Watch) Live() bool { return w.Status.Polled() }

// Due reports whether the watch wants a probe at now.
func (w Watch) Due(now time.Time) bool {
	if !w.Live() {
		return false
	}
	if w.SnoozeUntil != nil && now.Before(*w.SnoozeUntil) {
		return false
	}
	return w.NextCheck == nil || !now.Before(*w.NextCheck)
}

// Expired reports whether the watch's horizon has passed.
func (w Watch) Expired(now time.Time) bool {
	return w.ExpiresAt != nil && !now.Before(*w.ExpiresAt)
}

// Store persists watches in the workspace. Writes are atomic.
type Store struct {
	path string
	mu   sync.Mutex
}

// Dir is the workspace-relative directory watches live in.
const Dir = "watches"

// New opens (creating if needed) the watch ledger for a workspace.
func New(workspace string) (*Store, error) {
	if strings.TrimSpace(workspace) == "" {
		return nil, errors.New("workspace is required")
	}
	dir := filepath.Join(workspace, Dir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	return &Store{path: filepath.Join(dir, "watches.json")}, nil
}

func newID() string {
	return fmt.Sprintf("wt-%d", time.Now().UTC().UnixNano())
}

func normalize(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

func (s *Store) loadLocked() ([]Watch, error) {
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
	var out []Watch
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) saveLocked(list []Watch) error {
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

// ErrNoWatch is returned when the id does not exist.
var ErrNoWatch = errors.New("watch not found")

// Create stores a new watch after refusing anything that could not be
// honest: no entity, no provenance quote, unknown kind. Restating the same
// thing (same dedupe key) while it is still live is a no-op returning the
// existing watch — one thing in the world is watched once.
func (s *Store) Create(w Watch) (Watch, error) {
	if strings.TrimSpace(w.Entity) == "" {
		return Watch{}, errors.New("watch entity is required")
	}
	if strings.TrimSpace(w.Provenance.Quote) == "" {
		return Watch{}, errors.New("watch requires the owner's own words as provenance")
	}
	if !ValidKind(w.Kind) {
		return Watch{}, fmt.Errorf("unknown watch kind %q", w.Kind)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.loadLocked()
	if err != nil {
		return Watch{}, err
	}
	key := w.DedupeKey()
	for i := range list {
		if list[i].DedupeKey() == key && list[i].Live() {
			return list[i], nil
		}
	}
	now := time.Now().UTC()
	w.ID = newID()
	w.CreatedAt = now
	w.UpdatedAt = now
	if w.Status == "" {
		w.Status = StatusActive
	}
	if w.Label == "" {
		w.Label = w.Entity
	}
	if w.ExpiresAt == nil {
		// A watch expires on its own: a flight watch ends a day after the
		// flight, anything else after two idle weeks.
		if w.EventAt != nil {
			exp := w.EventAt.Add(24 * time.Hour)
			w.ExpiresAt = &exp
		} else {
			exp := now.Add(14 * 24 * time.Hour)
			w.ExpiresAt = &exp
		}
	}
	list = append(list, w)
	if err := s.saveLocked(list); err != nil {
		return Watch{}, err
	}
	return w, nil
}

// Get returns one watch by exact id.
func (s *Store) Get(id string) (Watch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.loadLocked()
	if err != nil {
		return Watch{}, err
	}
	for _, w := range list {
		if w.ID == id {
			return w, nil
		}
	}
	return Watch{}, fmt.Errorf("%w: %q", ErrNoWatch, id)
}

// List returns every watch, newest first (what is on my plate).
func (s *Store) List() ([]Watch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].CreatedAt.After(list[j].CreatedAt) })
	return list, nil
}

// Active returns the watches that may be polled.
func (s *Store) Active() ([]Watch, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var out []Watch
	for _, w := range all {
		if w.Live() {
			out = append(out, w)
		}
	}
	return out, nil
}

// Due returns the watches that want a probe at now, soonest first. Expired
// watches are returned separately by Sweep; this never includes them.
func (s *Store) Due(now time.Time) ([]Watch, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	var out []Watch
	for _, w := range all {
		if w.Due(now) && !w.Expired(now) {
			out = append(out, w)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].NextCheck == nil || out[j].NextCheck == nil {
			return out[i].NextCheck == nil
		}
		return out[i].NextCheck.Before(*out[j].NextCheck)
	})
	return out, nil
}

// mutate applies fn to one watch under the store lock and persists.
func (s *Store) mutate(id string, fn func(*Watch) error) (Watch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.loadLocked()
	if err != nil {
		return Watch{}, err
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
			return list[i], err
		}
		return list[i], nil
	}
	return Watch{}, fmt.Errorf("%w: %q", ErrNoWatch, id)
}

// ResetBaseline seeds the baseline on the first successful probe, so the
// observation that established "what the world looks like" is never itself
// reported as a change.
func (s *Store) ResetBaseline(id string, state map[string]string) (Watch, error) {
	return s.mutate(id, func(w *Watch) error {
		w.Baseline = copyState(state)
		w.Current = copyState(state)
		return nil
	})
}

// Observe records one probe result: current state, evidence, the schedule
// for the next check, and the reset of the failure counter. It returns the
// updated watch and the meaningful changes against the baseline.
func (s *Store) Observe(id string, state map[string]string, ev Evidence, nextCheck time.Time) (Watch, []Change, error) {
	var changes []Change
	updated, err := s.mutate(id, func(w *Watch) error {
		base := w.Baseline
		if base == nil {
			base = state
			w.Baseline = copyState(state)
		}
		changes = Diff(base, state)
		w.Current = copyState(state)
		w.Failures = 0
		w.LastFailure = ""
		now := time.Now().UTC()
		w.LastCheckAt = &now
		nc := nextCheck
		w.NextCheck = &nc
		if ev.At.IsZero() {
			ev.At = now
		}
		w.Evidence = append(w.Evidence, ev)
		// Bounded: evidence is the justification for recent notices, not
		// an unbounded log.
		if len(w.Evidence) > 20 {
			w.Evidence = w.Evidence[len(w.Evidence)-20:]
		}
		if w.Status == StatusActive && len(w.Notified) > 0 {
			w.Status = StatusTriggered
		}
		return nil
	})
	if err != nil {
		return Watch{}, nil, err
	}
	return updated, changes, nil
}

// RecordFailure counts a failed probe and pushes the next attempt back. It
// returns the status the watch ended up in: StatusFailed once the failure
// budget is exhausted — an honest "I couldn't watch this" rather than a
// silent watch that never fires.
func (s *Store) RecordFailure(id, reason string, nextCheck time.Time) (Watch, error) {
	return s.mutate(id, func(w *Watch) error {
		w.Failures++
		w.LastFailure = truncate(reason, 240)
		nc := nextCheck
		w.NextCheck = &nc
		now := time.Now().UTC()
		w.LastCheckAt = &now
		if w.Failures >= DefaultMaxFailures {
			w.Status = StatusFailed
		}
		return nil
	})
}

// MarkNotified records that a change fingerprint reached the owner. It
// returns false when the fingerprint was already delivered (dedupe across
// restarts — the store, not memory, is the record).
func (s *Store) MarkNotified(id, fingerprint string) (bool, Watch, error) {
	sent := false
	w, err := s.mutate(id, func(w *Watch) error {
		for _, f := range w.Notified {
			if f == fingerprint {
				return nil
			}
		}
		sent = true
		w.Notified = append(w.Notified, fingerprint)
		if len(w.Notified) > 40 {
			w.Notified = w.Notified[len(w.Notified)-40:]
		}
		return nil
	})
	return sent, w, err
}

// AlreadyNotified reports whether a fingerprint was delivered, without
// recording it.
func (w Watch) AlreadyNotified(fingerprint string) bool {
	for _, f := range w.Notified {
		if f == fingerprint {
			return true
		}
	}
	return false
}

// RecordSuppressed counts a notice that was held back, per reason, so the
// owner (and diagnostics) can see that Ghost noticed but stayed quiet — and
// why.
func (s *Store) RecordSuppressed(id, reason string) (Watch, error) {
	return s.mutate(id, func(w *Watch) error {
		if w.Suppressed == nil {
			w.Suppressed = map[string]int{}
		}
		w.Suppressed[reason]++
		return nil
	})
}

// Cancel closes a watch by owner decision.
func (s *Store) Cancel(id, note string) (Watch, error) {
	return s.mutate(id, func(w *Watch) error {
		if w.Status.Settled() {
			return fmt.Errorf("watch is already %s", w.Status)
		}
		w.Status = StatusDisabled
		w.NextCheck = nil
		if note != "" {
			w.LastFailure = "" // owner's note, not a failure
		}
		return nil
	})
}

// Snooze holds a watch back until a stated instant.
func (s *Store) Snooze(id string, until time.Time) (Watch, error) {
	return s.mutate(id, func(w *Watch) error {
		if !w.Live() {
			return fmt.Errorf("watch is %s", w.Status)
		}
		u := until.UTC()
		w.SnoozeUntil = &u
		w.Status = StatusSnoozed
		nc := until
		w.NextCheck = &nc
		return nil
	})
}

// Wake resumes a snoozed watch immediately: no next check is scheduled, so
// the very next poll picks it up.
func (s *Store) Wake(id string) (Watch, error) {
	return s.mutate(id, func(w *Watch) error {
		w.SnoozeUntil = nil
		w.Status = StatusActive
		w.NextCheck = nil
		return nil
	})
}

// Complete settles a watch because the world reached a natural end (the
// flight landed, the delivery arrived).
func (s *Store) Complete(id, note string) (Watch, error) {
	return s.mutate(id, func(w *Watch) error {
		w.Status = StatusCompleted
		w.NextCheck = nil
		if note != "" {
			w.LastFailure = truncate(note, 240)
		}
		return nil
	})
}

// Sweep expires watches whose horizon passed and returns them, so the
// caller can announce each expiry once.
func (s *Store) Sweep(now time.Time) ([]Watch, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	list, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	var expired []Watch
	changed := false
	for i := range list {
		if !list[i].Expired(now) || !list[i].Live() {
			continue
		}
		list[i].Status = StatusExpired
		list[i].NextCheck = nil
		list[i].UpdatedAt = now
		expired = append(expired, list[i])
		changed = true
	}
	if changed {
		if err := s.saveLocked(list); err != nil {
			return nil, err
		}
	}
	return expired, nil
}

// Counts reports how many watches sit in each status, for owner surfaces.
func (s *Store) Counts() (map[Status]int, error) {
	all, err := s.List()
	if err != nil {
		return nil, err
	}
	out := map[Status]int{}
	for _, w := range all {
		out[w.Status]++
	}
	return out, nil
}

func copyState(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
