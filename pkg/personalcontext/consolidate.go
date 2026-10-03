package personalcontext

import (
	"strings"
	"time"
)

// Consolidation result counts. Consolidate is the periodic grooming pass:
// detect contradictions, retire expired beliefs, decay idle reinforcement.
// Every transition appends a revision — records and provenance are never
// deleted.
type Consolidation struct {
	ConflictsDeclared int
	Expired           int
	Decayed           int
	At                time.Time
}

// Consolidate runs one grooming pass over the store. Safe to call on every
// heartbeat tick: each step is idempotent (repeats are no-ops).
func (s *Store) Consolidate(now time.Time) (Consolidation, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	out := Consolidation{At: now}
	if _, err := s.ReleaseMultiValuedConflicts(); err != nil {
		return out, err
	}
	if _, err := s.FoldDuplicates(); err != nil {
		return out, err
	}
	n, err := s.DetectConflicts()
	if err != nil {
		return out, err
	}
	out.ConflictsDeclared = n
	m, err := s.ExpireDue(now)
	if err != nil {
		return out, err
	}
	out.Expired = m
	d, err := s.DecayReinforcement()
	if err != nil {
		return out, err
	}
	out.Decayed = d
	return out, nil
}

// DetectConflicts finds current entries sharing (subject, predicate) with
// differing values and declares them conflicting. Neither value is selected:
// resolution stays explicit via ResolveConflict. Bounded: one declaration
// per conflicting group per pass.
func (s *Store) DetectConflicts() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	groups := map[string][]Entry{}
	for _, e := range s.byID {
		if e.Status != StatusCurrent || HoldsMany(*e) {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(e.Subject)) + "\x00" + strings.ToLower(strings.TrimSpace(e.Predicate))
		groups[key] = append(groups[key], *e)
	}
	declared := 0
	for key, members := range groups {
		if len(members) < 2 {
			continue
		}
		seen := map[string]string{}
		for _, m := range members {
			v := normConflictValue(string(m.Value))
			seen[v] = m.ID
		}
		if len(seen) < 2 {
			continue
		}
		ids := make([]string, 0, 2)
		for _, id := range seen {
			ids = append(ids, id)
			if len(ids) == 2 {
				break
			}
		}
		parts := strings.SplitN(key, "\x00", 2)
		if err := s.declareConflictLocked(parts[0], parts[1], ids[0], ids[1]); err != nil {
			continue
		}
		declared++
	}
	return declared, nil
}

// declareConflictLocked is DeclareConflict assuming the write lock is held
// and matching case-insensitively on subject/predicate (detector groups
// normalized keys; stored cases may differ).
func (s *Store) declareConflictLocked(subject, predicate, idA, idB string) error {
	a, okA := s.byID[idA]
	b, okB := s.byID[idB]
	if !okA || !okB {
		return ErrNotFound
	}
	if a.Status != StatusCurrent || b.Status != StatusCurrent {
		return ErrNotCurrent
	}
	now := time.Now().UTC()
	ra := *a
	ra.Status = StatusConflicting
	ra.UpdatedAt = now
	rb := *b
	rb.Status = StatusConflicting
	rb.UpdatedAt = now
	if err := s.append(ra); err != nil {
		return err
	}
	return s.append(rb)
}

// MultiValued reports whether a predicate is a catch-all that holds many
// unrelated beliefs ("fact/general", "project/current", "goal/primary",
// "preference/favorite"). Two values under one of these are two facts, not a
// contradiction: treating them as one belief hid "Works as an ESL teacher"
// and "Is originally from Tanzania" as conflicting, and let a favourite
// football club overwrite a favourite programming language.
func MultiValued(predicate string) bool {
	p := strings.ToLower(strings.TrimSpace(predicate))
	i := strings.LastIndex(p, "/")
	if i < 0 {
		return false
	}
	switch p[i+1:] {
	case "general", "current", "favorite", "prefers", "likes", "technology", "other":
		return true
	}
	return false
}

// HoldsMany reports whether an entry lives under a key that can hold several
// beliefs at once. "goal/primary" and "fact/work" are single-valued for the
// grammar rules ("my goal is to…" replaces the last one), but the model-written
// extractor files whole sentences under them ("Targets a $349 retail price",
// "Works on Applied AI Engineering"); those are separate facts.
func HoldsMany(e Entry) bool {
	if MultiValued(e.Predicate) {
		return true
	}
	p := strings.ToLower(e.Predicate)
	return SentenceValue(e) && (strings.HasSuffix(p, "/primary") || strings.HasSuffix(p, "/work"))
}

// ReleaseMultiValuedConflicts returns entries that were declared conflicting
// only because they shared a catch-all predicate to current. Nothing chose
// between them, so none was ever wrong. Idempotent.
func (s *Store) ReleaseMultiValuedConflicts() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	released := 0
	now := time.Now().UTC()
	for _, e := range s.byID {
		if e.Status != StatusConflicting || !HoldsMany(*e) {
			continue
		}
		rev := *e
		rev.Status = StatusCurrent
		rev.UpdatedAt = now
		if err := s.append(rev); err != nil {
			return released, err
		}
		released++
	}
	return released, nil
}

func normConflictValue(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = v[1 : len(v)-1]
	}
	return strings.ToLower(strings.Join(strings.Fields(v), " "))
}

// ExpireDue retires current entries whose ValidUntil has passed. Each
// expiry appends a superseded revision (SupersededBy nil = time, not
// replacement) — the record and its sources stay inspectable.
func (s *Store) ExpireDue(now time.Time) (int, error) {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	expired := 0
	for _, e := range s.byID {
		if e.Status != StatusCurrent || e.ValidUntil == nil {
			continue
		}
		if !now.After(*e.ValidUntil) {
			continue
		}
		rev := *e
		rev.Status = StatusSuperseded
		rev.SupersededBy = nil
		rev.UpdatedAt = now
		if err := s.append(rev); err != nil {
			return expired, err
		}
		expired++
	}
	return expired, nil
}

// FoldDuplicates retires current entries that say exactly what another
// current entry under the same key already says, keeping the one the owner
// stated (else the oldest). The same sentence learned twice is one memory,
// not two rows. Idempotent; returns how many it folded.
func (s *Store) FoldDuplicates() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	groups := map[string][]*Entry{}
	for _, e := range s.byID {
		if e.Status != StatusCurrent {
			continue
		}
		k := strings.ToLower(e.Subject) + "\x00" + strings.ToLower(e.Predicate) + "\x00" + normConflictValue(string(e.Value))
		groups[k] = append(groups[k], e)
	}
	folded := 0
	now := time.Now().UTC()
	for _, g := range groups {
		if len(g) < 2 {
			continue
		}
		keep := g[0]
		for _, e := range g[1:] {
			if better(e, keep) {
				keep = e
			}
		}
		for _, e := range g {
			if e == keep {
				continue
			}
			rev := *e
			rev.Status = StatusSuperseded
			id := keep.ID
			rev.SupersededBy = &id
			rev.UpdatedAt = now
			if err := s.append(rev); err != nil {
				return folded, err
			}
			folded++
		}
	}
	return folded, nil
}

// better prefers what the owner said over what Ghost inferred, then the
// older entry (it carries the original receipt).
func better(a, b *Entry) bool {
	da, db := ownerStated(*a), ownerStated(*b)
	if da != db {
		return da
	}
	return a.CreatedAt.Before(b.CreatedAt)
}

func ownerStated(e Entry) bool {
	for _, src := range e.Sources {
		if src.Kind == SourceUserDeclared || src.Kind == SourceUserCorrected {
			return true
		}
	}
	return false
}
