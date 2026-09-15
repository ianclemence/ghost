package browser

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Snapshot + ephemeral-ref observation contract.
//
// The model observes pages as accessibility snapshots whose element
// references (@e5) are valid for exactly one epoch: any snapshot or
// navigation opens a new epoch, and any mutating operation closes it.
// Acting on a ref from a closed epoch is a stale-ref error with a
// re-snapshot instruction — never a blind click at whatever now sits
// under that ID. The ledger lives on the SessionStore (per agent loop,
// like sessions themselves), keyed by session ID with TTL-bounded
// entries swept alongside expired sessions.

// refEpoch is one observation generation for one session.
type refEpoch struct {
	epoch   int64
	refs    map[string]bool
	expires time.Time
}

// RefLedger tracks live ref sets per browser session.
type RefLedger struct {
	mu     sync.Mutex
	epochs map[string]*refEpoch
}

// TaintSpan records one page whose content entered model context as
// untrusted data. The broker consumes tainted domains when scoping
// consequential approvals: a submit whose target was shaped by
// attacker-influenced content deserves a narrower scope.
type TaintSpan struct {
	SessionID string
	URL       string
	Domain    string
	At        time.Time
}

// maxTaintSpans bounds per-session taint memory.
const maxTaintSpans = 50

// refTTL bounds ledger memory for sessions that vanish without Close.
const refTTL = 2 * time.Hour

var (
	refAtPattern  = regexp.MustCompile(`@e\d+`)
	refEqPattern  = regexp.MustCompile(`\bref=e\d+\b`)
	refKeyPattern = regexp.MustCompile(`"e\d+"`)
)

// ParseRefs extracts element references from snapshot output in any of
// the wire forms backends emit, normalized to the canonical @eN form
// the model uses:
//
//	@e5              Ghost tool descriptions / model convention
//	ref=e5           agent-browser snapshot tree text
//	"e5"             refs-map keys in snapshot JSON
//
// All three denote the same element; the canonical form is what act
// arguments carry, so the epoch set stores canonical refs.
func ParseRefs(text string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(ref string) {
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	for _, r := range refAtPattern.FindAllString(text, -1) {
		add(r)
	}
	for _, m := range refEqPattern.FindAllString(text, -1) {
		add("@" + strings.TrimPrefix(m[strings.Index(m, "="):], "="))
	}
	// Refs-map keys only: require the "refs" object nearby to avoid
	// matching unrelated quoted strings.
	if idx := strings.Index(text, `"refs"`); idx >= 0 {
		for _, k := range refKeyPattern.FindAllString(text[idx:], -1) {
			add("@" + strings.Trim(k, `"`))
		}
	}
	return out
}

// Observe opens a new epoch for sessionID with refs as the live set.
// Called after every successful snapshot or navigation.
func (s *SessionStore) Observe(sessionID string, refs []string) int64 {
	s.refsMu.Lock()
	defer s.refsMu.Unlock()
	if s.refLedger == nil {
		s.refLedger = map[string]*refEpoch{}
	}
	prev := int64(0)
	if e, ok := s.refLedger[sessionID]; ok {
		prev = e.epoch
	}
	set := make(map[string]bool, len(refs))
	for _, r := range refs {
		set[r] = true
	}
	s.refLedger[sessionID] = &refEpoch{epoch: prev + 1, refs: set, expires: time.Now().UTC().Add(refTTL)}
	return prev + 1
}

// StaleRefError is returned when an act-class operation names a ref
// outside the live epoch. The model must re-snapshot; the runtime must
// not guess.
type StaleRefError struct {
	Ref   string
	Hint  string
	Epoch int64
}

func (e *StaleRefError) Error() string {
	return fmt.Sprintf("browser: stale element ref %q (epoch %d): %s", e.Ref, e.Epoch, e.Hint)
}

// CheckRef validates a ref against the live epoch. Unknown sessions,
// expired epochs, and refs outside the live set all fail closed with a
// re-snapshot instruction.
func (s *SessionStore) CheckRef(sessionID, ref string) error {
	s.refsMu.Lock()
	defer s.refsMu.Unlock()
	e, ok := s.refLedger[sessionID]
	if !ok || time.Now().UTC().After(e.expires) {
		return &StaleRefError{Ref: ref, Epoch: -1, Hint: "no live snapshot for this session; take a snapshot first"}
	}
	if !e.refs[ref] {
		return &StaleRefError{Ref: ref, Epoch: e.epoch, Hint: "ref is from an older snapshot or another page; re-snapshot and use a fresh ref"}
	}
	return nil
}

// Mutate closes the live epoch after a state-changing operation. The
// next act requires a fresh snapshot; observations after the mutation
// open their own epoch via Observe.
func (s *SessionStore) Mutate(sessionID string) {
	s.refsMu.Lock()
	defer s.refsMu.Unlock()
	if s.refLedger == nil {
		return
	}
	if e, ok := s.refLedger[sessionID]; ok {
		e.refs = map[string]bool{}
		e.epoch++
	}
}

// RefEpoch reports the live epoch for introspection (-1 when none).
func (s *SessionStore) RefEpoch(sessionID string) int64 {
	s.refsMu.Lock()
	defer s.refsMu.Unlock()
	if e, ok := s.refLedger[sessionID]; ok && time.Now().UTC().Before(e.expires) {
		return e.epoch
	}
	return -1
}

// RecordTaint appends an untrusted-content span for a session, bounded.
func (s *SessionStore) RecordTaint(span TaintSpan) {
	if span.SessionID == "" {
		return
	}
	if span.At.IsZero() {
		span.At = time.Now().UTC()
	}
	s.refsMu.Lock()
	defer s.refsMu.Unlock()
	spans := append(s.taintLog[span.SessionID], span)
	if len(spans) > maxTaintSpans {
		spans = spans[len(spans)-maxTaintSpans:]
	}
	if s.taintLog == nil {
		s.taintLog = map[string][]TaintSpan{}
	}
	s.taintLog[span.SessionID] = spans
}

// TaintedDomains returns distinct domains whose content entered model
// context through this session, oldest first. Empty when clean.
func (s *SessionStore) TaintedDomains(sessionID string) []string {
	s.refsMu.Lock()
	defer s.refsMu.Unlock()
	var out []string
	seen := map[string]bool{}
	for _, sp := range s.taintLog[sessionID] {
		if sp.Domain != "" && !seen[sp.Domain] {
			seen[sp.Domain] = true
			out = append(out, sp.Domain)
		}
	}
	return out
}

// Revalidate is the explicit restore-check gate for resume paths: the
// stored row must still belong to this owner/context/task and still be
// live. Anything else fails closed — no silent rebinding to whatever
// session happens to exist.
func (s *SessionStore) Revalidate(id, owner, contextID, taskID string) (*Session, error) {
	row, err := s.Get(id)
	if err != nil {
		return nil, fmt.Errorf("browser session unavailable: %w", err)
	}
	if row == nil {
		return nil, fmt.Errorf("browser session not found")
	}
	if row.Owner != owner || row.ContextID != contextID || row.TaskID != taskID {
		return nil, fmt.Errorf("browser session belongs to a different owner, context, or task")
	}
	if time.Now().UTC().After(row.ExpiresAt) {
		return nil, fmt.Errorf("browser session expired; ask again to start a fresh one")
	}
	return row, nil
}

// SweepRefs drops ledger and taint state for sessions gone from the
// table. Called alongside ExpireSweep so in-memory state cannot outlive
// the sessions it describes.
func (s *SessionStore) SweepRefs() {
	s.refsMu.Lock()
	defer s.refsMu.Unlock()
	live := map[string]bool{}
	if s.db != nil {
		rows, err := s.db.Query(`SELECT id FROM browser_sessions`)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var id string
				if rows.Scan(&id) == nil {
					live[id] = true
				}
			}
		}
	}
	now := time.Now().UTC()
	for id, e := range s.refLedger {
		if !live[id] || now.After(e.expires) {
			delete(s.refLedger, id)
		}
	}
	for id := range s.taintLog {
		if !live[id] {
			delete(s.taintLog, id)
		}
	}
}
