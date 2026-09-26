package ideas

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

// ErrStale reports that a transition's precondition no longer holds — the
// idea moved on (answered, expired, superseded) between read and write. It is
// the persistence half of stale-approval protection: the caller must not
// execute anything when it sees this.
var ErrStale = errors.New("idea state has changed")

func newID() string {
	return fmt.Sprintf("idea-%d", time.Now().UTC().UnixNano())
}

// Add stores new ideas, filling IDs and timestamps.
func (s *Store) Add(ideas []Idea) error {
	if s == nil || len(ideas) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, idea := range ideas {
		if idea.ID == "" {
			idea.ID = newID()
		}
		if idea.CreatedAt.IsZero() {
			idea.CreatedAt = time.Now().UTC()
		}
		if idea.Status == "" {
			idea.Status = StatusPending
		}
		raw, err := json.Marshal(idea)
		if err != nil {
			return err
		}
		if _, err := f.Write(append(raw, '\n')); err != nil {
			return err
		}
	}
	return nil
}

// all reads every idea, newest last.
func (s *Store) all() ([]Idea, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	var out []Idea
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var idea Idea
		if err := json.Unmarshal(sc.Bytes(), &idea); err != nil {
			continue
		}
		out = append(out, idea)
	}
	return out, sc.Err()
}

// List returns ideas, newest first, optionally filtered by status (""
// means all), capped by limit (<=0 means all).
func (s *Store) List(status Status, limit int) ([]Idea, error) {
	all, err := s.all()
	if err != nil {
		return nil, err
	}
	var out []Idea
	for i := len(all) - 1; i >= 0; i-- {
		if status != "" && all[i].Status != status {
			continue
		}
		out = append(out, all[i])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

// Get returns one idea by id or unique prefix.
func (s *Store) Get(ref string) (Idea, error) {
	all, err := s.all()
	if err != nil {
		return Idea{}, err
	}
	var hit Idea
	matches := 0
	for _, idea := range all {
		if idea.ID == ref || (len(ref) >= 4 && len(idea.ID) >= len(ref) && idea.ID[:len(ref)] == ref) {
			hit = idea
			matches++
		}
	}
	if matches != 1 {
		return Idea{}, fmt.Errorf("no single idea matches %q", ref)
	}
	return hit, nil
}

// Decide records accept (true) or dismiss (false) with a timestamp. It
// rewrites the store file; only status/decided fields change.
func (s *Store) Decide(id string, accept bool) (Idea, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.path)
	if err != nil {
		return Idea{}, err
	}
	var all []Idea
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var idea Idea
		if err := json.Unmarshal(sc.Bytes(), &idea); err != nil {
			continue
		}
		all = append(all, idea)
	}
	f.Close()
	if err := sc.Err(); err != nil {
		return Idea{}, err
	}
	var decided Idea
	found := false
	now := time.Now().UTC()
	for i := range all {
		if all[i].ID == id {
			if accept {
				all[i].Status = StatusAccepted
			} else {
				all[i].Status = StatusDismissed
			}
			all[i].DecidedAt = &now
			decided = all[i]
			found = true
		}
	}
	if !found {
		return Idea{}, fmt.Errorf("no idea %q", id)
	}
	tmp := s.path + ".tmp"
	w, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return Idea{}, err
	}
	for _, idea := range all {
		raw, err := json.Marshal(idea)
		if err != nil {
			w.Close()
			return Idea{}, err
		}
		if _, err := w.Write(append(raw, '\n')); err != nil {
			w.Close()
			return Idea{}, err
		}
	}
	w.Close()
	if err := os.Rename(tmp, s.path); err != nil {
		return Idea{}, err
	}
	return decided, nil
}

// Transition atomically rewrites the store, applying mutate to the idea with
// the given id. from lists the statuses the transition accepts; an empty list
// means "any". It returns ErrStale when the current status is not accepted, so
// a proposal that was dismissed, expired or superseded in another process can
// never be executed from a stale read.
//
// The whole file is rewritten under the store mutex, matching Decide: the
// store is a small append-mostly JSONL log and single-writer correctness
// matters more here than per-row updates.
func (s *Store) Transition(id string, from []Status, mutate func(*Idea) error) (Idea, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s == nil {
		return Idea{}, errors.New("ideas store is nil")
	}
	f, err := os.Open(s.path)
	if err != nil {
		return Idea{}, err
	}
	var all []Idea
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1<<20)
	for sc.Scan() {
		var idea Idea
		if err := json.Unmarshal(sc.Bytes(), &idea); err != nil {
			continue
		}
		all = append(all, idea)
	}
	f.Close()
	if err := sc.Err(); err != nil {
		return Idea{}, err
	}
	idx := -1
	for i := range all {
		if all[i].ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Idea{}, fmt.Errorf("no idea %q", id)
	}
	if len(from) > 0 {
		ok := false
		for _, st := range from {
			if all[idx].Status == st {
				ok = true
				break
			}
		}
		if !ok {
			return all[idx], ErrStale
		}
	}
	if err := mutate(&all[idx]); err != nil {
		return all[idx], err
	}
	tmp := s.path + ".tmp"
	w, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
	if err != nil {
		return Idea{}, err
	}
	for _, idea := range all {
		raw, err := json.Marshal(idea)
		if err != nil {
			w.Close()
			return Idea{}, err
		}
		if _, err := w.Write(append(raw, '\n')); err != nil {
			w.Close()
			return Idea{}, err
		}
	}
	w.Close()
	if err := os.Rename(tmp, s.path); err != nil {
		return Idea{}, err
	}
	return all[idx], nil
}

// MarkPresented records that a candidate reached the owner. It is idempotent:
// presenting an already-presented idea is a no-op, not an error.
func (s *Store) MarkPresented(id string, now time.Time) (Idea, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return s.Transition(id, []Status{StatusPending, StatusPresented}, func(i *Idea) error {
		i.Status = StatusPresented
		if i.PresentedAt == nil {
			t := now.UTC()
			i.PresentedAt = &t
		}
		return nil
	})
}

// Snooze defers an idea until the given time. Snoozing is an owner decision,
// so it closes the current proposal and lets a fresh one form later.
func (s *Store) Snooze(id string, until time.Time, now time.Time) (Idea, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return s.Transition(id, []Status{StatusPending, StatusPresented}, func(i *Idea) error {
		i.Status = StatusSnoozed
		u := until.UTC()
		i.SnoozedUntil = &u
		t := now.UTC()
		i.DecidedAt = &t
		return nil
	})
}

// Expire closes an unanswered proposal whose window has passed. It never
// touches an answered or in-flight idea.
func (s *Store) Expire(id string, now time.Time) (Idea, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return s.Transition(id, []Status{StatusPending, StatusPresented}, func(i *Idea) error {
		i.Status = StatusExpired
		t := now.UTC()
		i.DecidedAt = &t
		return nil
	})
}

// Supersede voids a proposal because the state it was built from changed. The
// owner's approval, if any, must not execute: superseding is how the runtime
// refuses to act on stale information.
func (s *Store) Supersede(id, reason string, now time.Time) (Idea, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return s.Transition(id, []Status{StatusPending, StatusPresented, StatusAccepted, StatusSnoozed}, func(i *Idea) error {
		i.Status = StatusSuperseded
		i.Outcome = "superseded"
		i.Result = reason
		t := now.UTC()
		i.DecidedAt = &t
		return nil
	})
}

// Live reports the ideas that still represent an open opportunity, for the
// dedupe check.
func (s *Store) Live(limit int) ([]Idea, error) {
	all, err := s.all()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 500
	}
	var out []Idea
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, all[i])
	}
	return out, nil
}

// Open reports the ideas the owner can still act on, newest first.
func (s *Store) Open(limit int) ([]Idea, error) {
	return s.listStatuses(nil, true, limit)
}

// Resolved reports the ideas that already reached a terminal state, newest
// first — the receipts the owner can look back at.
func (s *Store) Resolved(limit int) ([]Idea, error) {
	return s.listStatuses(nil, false, limit)
}

func (s *Store) listStatuses(_ []Status, open bool, limit int) ([]Idea, error) {
	all, err := s.all()
	if err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 50
	}
	var out []Idea
	for i := len(all) - 1; i >= 0 && len(out) < limit; i-- {
		if all[i].Status.Undecided() == open {
			out = append(out, all[i])
		}
	}
	return out, nil
}

// BeginExecution claims an approved idea for exactly one run. It accepts
// pending or presented (an approval may resolve before the presented marker is
// written) and returns ErrStale for anything already answered, so a duplicate
// approval can never execute twice.
func (s *Store) BeginExecution(id string, now time.Time) (Idea, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return s.Transition(id, []Status{StatusPending, StatusPresented}, func(i *Idea) error {
		i.Status = StatusExecuting
		return nil
	})
}

// Complete records a verified successful outcome. Only an executing idea can
// complete; the Result text must come from runtime evidence, never a model
// claim.
func (s *Store) Complete(id, outcome, result string, now time.Time) (Idea, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if outcome == "" {
		outcome = "succeeded"
	}
	return s.Transition(id, []Status{StatusExecuting, StatusAccepted, StatusPresented, StatusPending}, func(i *Idea) error {
		i.Status = StatusCompleted
		i.Outcome = outcome
		i.Result = result
		t := now.UTC()
		i.DecidedAt = &t
		return nil
	})
}

// Fail records a real failure (or a failed verification). The message is the
// runtime's honest statement of what happened.
func (s *Store) Fail(id, result string, now time.Time) (Idea, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return s.Transition(id, []Status{StatusExecuting, StatusAccepted, StatusPending, StatusPresented}, func(i *Idea) error {
		i.Status = StatusFailed
		i.Outcome = "failed"
		i.Result = result
		t := now.UTC()
		i.DecidedAt = &t
		return nil
	})
}
