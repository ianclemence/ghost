package personalcontext

import (
	"bufio"
	"bytes"
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

// Errors returned by store operations. Callers distinguish them with errors.Is.
var (
	// ErrNotFound is returned when an operation references a missing entry.
	ErrNotFound = errors.New("entry not found")
	// ErrDuplicateID is returned when Create is given an id that already exists
	// in the log. Existing ids can only be changed by appending a revision.
	ErrDuplicateID = errors.New("entry id already exists")
	// ErrNoCurrentEntry is returned when Supersede has no current entry for the
	// requested subject/predicate to replace.
	ErrNoCurrentEntry = errors.New("no current entry for subject/predicate")
	// ErrNotCurrent is returned when an operation requires an entry to be
	// current but it is not.
	ErrNotCurrent = errors.New("entry is not current")
)

// EntriesDir is the workspace-relative directory that holds Personal Context.
const EntriesDir = "personal-context"

// EntriesFile is the append-only JSONL log of entry records.
const EntriesFile = "entries.jsonl"

// EntriesPath returns the location of the entries log for a workspace.
func EntriesPath(workspace string) string {
	return filepath.Join(workspace, EntriesDir, EntriesFile)
}

// Store is an append-only JSONL-backed Personal Context store. It holds the
// full log in memory plus the reconstructed final state of every entry. All
// mutations append a new record and never rewrite a previously written line.
type Store struct {
	path string
	mu   sync.RWMutex
	log  []Entry           // every record in file order
	byID map[string]*Entry // last record per entry id (final state)
}

// Open opens (creating if necessary) the entries log for a workspace and
// reconstructs the current state from it. A missing or empty log is a valid,
// empty store; a malformed line is an error so corrupted canonical state is
// never silently dropped.
func Open(workspace string) (*Store, error) {
	path := EntriesPath(workspace)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create %s: %w", EntriesDir, err)
	}
	s := &Store{path: path, byID: make(map[string]*Entry)}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
}

// Path returns the entries log location.
func (s *Store) Path() string {
	return s.path
}

// ValidateEntries checks that data is a well-formed Personal Context entries
// log: every non-empty line must parse as an Entry, exactly as the store's
// load step requires. It is the integrity gate Ghost State import uses so a
// malformed portable log fails loudly instead of being silently accepted as
// an empty context. An empty log is valid (it is an empty store), and no
// record is ever dropped.
func ValidateEntries(data []byte) error {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			return fmt.Errorf("parse entries line %d: %w", line, err)
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("scan entries: %w", err)
	}
	return nil
}

// load reconstructs state from the append-only log. The last record for an id
// wins; every record is kept in order so history stays inspectable.
func (s *Store) load() error {
	f, err := os.Open(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("open entries log: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 64*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		raw := strings.TrimSpace(sc.Text())
		if raw == "" {
			continue
		}
		var e Entry
		if err := json.Unmarshal([]byte(raw), &e); err != nil {
			return fmt.Errorf("parse %s line %d: %w", s.path, line, err)
		}
		s.apply(e)
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("read %s: %w", s.path, err)
	}
	return nil
}

// apply records one log record: it appends to the log and, for the final-state
// index, replaces the entry with that id.
func (s *Store) apply(e Entry) {
	s.log = append(s.log, e)
	s.byID[e.ID] = &e
}

// Create appends a new entry. The entry is validated, its value is compacted
// to canonical JSON, missing timestamps default to now (UTC), and an empty
// status defaults to current. The id is required and must not already exist in
// the log: existing entries change only by appending a revision.
func (s *Store) Create(e Entry) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.createLocked(e)
}

func (s *Store) createLocked(e Entry) (Entry, error) {
	if _, exists := s.byID[e.ID]; exists {
		return Entry{}, fmt.Errorf("%w: %s", ErrDuplicateID, e.ID)
	}
	now := time.Now().UTC()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = now
	}
	if e.UpdatedAt.IsZero() {
		e.UpdatedAt = now
	}
	if e.Status == "" {
		e.Status = StatusCurrent
	}
	if e.Sources == nil {
		e.Sources = []Source{}
	}
	value, err := compactJSON(e.Value)
	if err != nil {
		return Entry{}, fmt.Errorf("entry %s: %w", e.ID, err)
	}
	e.Value = value
	if err := e.Validate(); err != nil {
		return Entry{}, fmt.Errorf("create entry: %w", err)
	}
	if err := s.append(e); err != nil {
		return Entry{}, err
	}
	return e, nil
}

// Supersede records that a new value for (subject, predicate) replaces the
// current value. The current entry is marked superseded with superseded_by set
// to the new entry's id, and the new entry becomes current. Both records are
// appended; the old entry and all its sources are never deleted.
func (s *Store) Supersede(subject, predicate string, e Entry) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.supersedeLocked(subject, predicate, e)
}

// supersedeLocked is Supersede without taking the lock; callers must hold it.
func (s *Store) supersedeLocked(subject, predicate string, e Entry) (Entry, error) {
	cur := s.currentEntry(subject, predicate)
	if cur == nil {
		return Entry{}, fmt.Errorf("%w: %s/%s", ErrNoCurrentEntry, subject, predicate)
	}
	if e.Status != "" && e.Status != StatusCurrent {
		return Entry{}, fmt.Errorf("superseding entry for %s/%s must be current, got %q", subject, predicate, e.Status)
	}
	created, err := s.createLocked(e)
	if err != nil {
		return Entry{}, err
	}

	rev := *cur
	rev.Status = StatusSuperseded
	rev.SupersededBy = &created.ID
	rev.UpdatedAt = time.Now().UTC()
	if err := s.append(rev); err != nil {
		return Entry{}, err
	}
	return created, nil
}

// MaxReinforceCount caps how many times a single belief can be reinforced, so
// repeated confirmation cannot grow the counter without bound. The counter
// saturates at this value; ReinforcedAt is still refreshed so "last confirmed"
// stays accurate.
const MaxReinforceCount = 20

// ReinforceDecayWindow is how long since the last reinforcement before the
// count is allowed to decay during a consolidation. ReinforcedAt is cleared
// once the count reaches zero, so a genuinely idle fact loses its emphasis.
const ReinforceDecayWindow = 90 * 24 * time.Hour

// reinforceLocked records that a current belief was restated: it bumps the
// reinforcement count (saturating at MaxReinforceCount) and "last reinforced"
// time without changing the value, sources, or status. Provenance is preserved
// (the original sources stay) and no new fact is created. The caller must hold
// the write lock.
func (s *Store) reinforceLocked(cur *Entry) error {
	rev := *cur
	if rev.ReinforceCount < MaxReinforceCount {
		rev.ReinforceCount++
	} else {
		rev.ReinforceCount = MaxReinforceCount
	}
	now := time.Now().UTC()
	rev.ReinforcedAt = &now
	rev.UpdatedAt = now
	return s.append(rev)
}

// DecayReinforcement steps down the ReinforceCount of current entries whose
// last reinforcement is older than ReinforceDecayWindow, clearing ReinforcedAt
// when the count reaches zero. This keeps the reported strength reflecting
// recent emphasis rather than a lifetime count. It only changes the
// reinforcement bookkeeping, never the value or provenance. Returns the number
// of entries decayed. It is safe to call concurrently.
func (s *Store) DecayReinforcement() (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	decayed := 0
	for _, e := range s.byID {
		if e.Status != StatusCurrent || e.ReinforceCount <= 0 || e.ReinforcedAt == nil {
			continue
		}
		if now.Sub(e.ReinforcedAt.UTC()) < ReinforceDecayWindow {
			continue
		}
		rev := *e
		rev.ReinforceCount = rev.ReinforceCount / 2
		if rev.ReinforceCount == 0 {
			rev.ReinforcedAt = nil
		}
		rev.UpdatedAt = now
		if err := s.append(rev); err != nil {
			return decayed, err
		}
		decayed++
	}
	return decayed, nil
}

// applyActions persists extraction actions atomically with respect to each
// other and to concurrent writers. The caller's Extract decision was made
// against a snapshot of the current context; by the time persistence runs,
// another message may have already created an entry for the same subject and
// predicate (a concurrent session, or an earlier declaration in the same
// message resolved to the same predicate by a different grammar rule). Each
// action is therefore re-resolved against the live state under the store lock:
//
//   - no current entry: the action is a create (including a correction whose
//     entry disappeared, which becomes a new declaration as documented);
//   - a restatement of the current value: nothing is written;
//   - a differing non-additive value: the current entry is superseded;
//   - additive likes: a new entry is appended (a duplicate of an existing or
//     already-appended like value is skipped).
//
// This keeps the invariant that a single belief (subject + predicate) has at
// most one current entry, while likes remain additive.
func (s *Store) applyActions(actions []Action) ([]Action, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Action
	for _, a := range actions {
		e := a.Entry
		cur := s.currentEntry(e.Subject, e.Predicate)

		switch {
		case cur == nil:
			created, err := s.createLocked(e)
			if err != nil {
				return out, err
			}
			a.Mode = ActionCreate
			a.Entry = created
			out = append(out, a)
		case a.Mode == ActionReinforce:
			if err := s.reinforceLocked(cur); err != nil {
				return out, err
			}
			// reflect the reinforced state (append updated byID[id])
			if refreshed, ok := s.byID[cur.ID]; ok {
				a.Entry = *refreshed
			} else {
				a.Entry = *cur
			}
			out = append(out, a)
		case a.Rule == likesRuleName:
			// Additive: a like never supersedes. A duplicate value (existing
			// or just appended earlier in this batch) is handled as a reinforce
			// action above; otherwise a genuinely new like is appended.
			if entryValueString(*cur) == entryValueString(e) {
				continue
			}
			created, err := s.createLocked(e)
			if err != nil {
				return out, err
			}
			a.Mode = ActionCreate
			a.Entry = created
			out = append(out, a)
		case entryValueString(*cur) == entryValueString(e):
			// Restating the current belief is handled as a reinforce action
			// above (decided before building actions), so this branch is a
			// safety-net no-op.
		default:
			created, err := s.supersedeLocked(e.Subject, e.Predicate, e)
			if err != nil {
				return out, err
			}
			a.Mode = ActionSupersede
			a.Entry = created
			out = append(out, a)
		}
	}
	return out, nil
}

// DeclareConflict represents two unresolved values for the same belief as
// conflicting. Both entries must exist, match (subject, predicate), and be
// current. Each gets a conflicting revision appended; neither is ever silently
// merged or selected as the current value.
func (s *Store) DeclareConflict(subject, predicate string, idA, idB string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	a, okA := s.byID[idA]
	b, okB := s.byID[idB]
	if !okA {
		return fmt.Errorf("%w: %s", ErrNotFound, idA)
	}
	if !okB {
		return fmt.Errorf("%w: %s", ErrNotFound, idB)
	}
	if a.Subject != subject || a.Predicate != predicate {
		return fmt.Errorf("entry %s does not match %s/%s", a.ID, subject, predicate)
	}
	if b.Subject != subject || b.Predicate != predicate {
		return fmt.Errorf("entry %s does not match %s/%s", b.ID, subject, predicate)
	}
	if a.Status != StatusCurrent || b.Status != StatusCurrent {
		return fmt.Errorf("%w: %s and %s are not both current", ErrNotCurrent, a.ID, b.ID)
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

// Forget retires an entry: it appends a rejected revision so the entry stops
// appearing as current context while its record and provenance remain
// inspectable. This is the storage side of "forget this personal-context
// entry". It never touches conversation evidence; deleting conversations is a
// separate, later slice.
func (s *Store) Forget(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.byID[id]
	if !ok {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	rev := *e
	rev.Status = StatusRejected
	rev.UpdatedAt = time.Now().UTC()
	return s.append(rev)
}

// Get returns the final state of an entry by id.
func (s *Store) Get(id string) (Entry, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.byID[id]
	if !ok {
		return Entry{}, false
	}
	return *e, true
}

// All returns the final state of every entry, including superseded,
// conflicting, uncertain, and rejected ones, sorted by id. It is the explicit
// escape hatch for inspecting the full context store.
func (s *Store) All() []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return sortedByID(allFinal(s.byID))
}

// Current returns the current context as of now: entries with status current
// that fall within their temporal validity, sorted deterministically.
func (s *Store) Current() []Entry {
	return s.CurrentAt(time.Now())
}

// CurrentAt returns the current context as of a reference time. Entries with
// a valid_until in the past (or a valid_from in the future) are excluded, as
// are entries that are not status current. Rejected, superseded, conflicting,
// and uncertain entries never leak into this query.
func (s *Store) CurrentAt(t time.Time) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Entry
	for _, e := range s.byID {
		if e.Status != StatusCurrent {
			continue
		}
		if !withinValidity(*e, t) {
			continue
		}
		out = append(out, *e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Subject != out[j].Subject {
			return out[i].Subject < out[j].Subject
		}
		if out[i].Predicate != out[j].Predicate {
			return out[i].Predicate < out[j].Predicate
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// VisibleTo reports whether an entry is visible to the given scopes:
// global (untagged) entries are visible everywhere; scoped entries only
// where a scope matches. Single predicate behind every retrieval path.
func VisibleTo(e Entry, scopes []string) bool {
	if len(e.Scopes) == 0 {
		return true
	}
	for _, sc := range e.Scopes {
		for _, mine := range scopes {
			if sc == mine {
				return true
			}
		}
	}
	return false
}

// CurrentInScope returns current entries visible to the given scopes:
// global (untagged) entries plus entries tagged with any of the scopes.
// Precedence is explicit: a scoped fact overrides the global fact for
// the same subject+predicate (context wins, global never leaks through
// as a competing answer). Context isolation for retrieval: a context
// sees its own + global memories, never another context's scoped facts.
func (s *Store) CurrentInScope(scopes []string) []Entry {
	all := s.Current()
	visible := make([]Entry, 0, len(all))
	for _, e := range all {
		if VisibleTo(e, scopes) {
			visible = append(visible, e)
		}
	}
	// Scoped entries shadow global ones on subject+predicate.
	shadowed := map[string]bool{}
	for _, e := range visible {
		if len(e.Scopes) > 0 {
			shadowed[e.Subject+"\x00"+e.Predicate] = true
		}
	}
	if len(shadowed) == 0 {
		return visible
	}
	result := make([]Entry, 0, len(visible))
	for _, e := range visible {
		if len(e.Scopes) == 0 && shadowed[e.Subject+"\x00"+e.Predicate] {
			continue
		}
		result = append(result, e)
	}
	return result
}

// BySubject returns the final state of every entry with the given subject,
// regardless of status, sorted by id.
func (s *Store) BySubject(subject string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Entry
	for _, e := range allFinal(s.byID) {
		if e.Subject == subject {
			out = append(out, e)
		}
	}
	return sortedByID(out)
}

// ByPredicate returns the final state of every entry with the given predicate,
// regardless of status, sorted by id.
func (s *Store) ByPredicate(predicate string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Entry
	for _, e := range allFinal(s.byID) {
		if e.Predicate == predicate {
			out = append(out, e)
		}
	}
	return sortedByID(out)
}

// ByKind returns the final state of every entry of the given kind, regardless
// of status, sorted by id.
func (s *Store) ByKind(k Kind) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Entry
	for _, e := range allFinal(s.byID) {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	return sortedByID(out)
}

// History returns every record appended for an entry id, in log order. It is
// the explicit way to inspect rejected, superseded, or conflicting entries and
// how a belief changed over time.
func (s *Store) History(id string) []Entry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var out []Entry
	for _, e := range s.log {
		if e.ID == id {
			out = append(out, e)
		}
	}
	return out
}

// Reset clears both the in-memory state and the on-disk log, returning the
// store to a fresh-install state. Truncating the file alone is NOT enough:
// the running process serves Current() from memory, so a file-only wipe
// leaves stale beliefs visible until restart (ghost knew deleted facts).
func (s *Store) Reset() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.log = nil
	s.byID = make(map[string]*Entry)
	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return fmt.Errorf("create %s: %w", EntriesDir, err)
	}
	if err := os.WriteFile(s.path, []byte{}, 0644); err != nil {
		return fmt.Errorf("truncate entries log: %w", err)
	}
	return nil
}

// append writes one record to the log and updates the in-memory state. The
// caller must hold the write lock.
func (s *Store) append(e Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal entry %s: %w", e.ID, err)
	}
	line = append(line, '\n')

	f, err := os.OpenFile(s.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open entries log: %w", err)
	}
	if _, err := f.Write(line); err != nil {
		f.Close()
		return fmt.Errorf("append entry %s: %w", e.ID, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("close entries log: %w", err)
	}
	s.apply(e)
	return nil
}

// currentEntry returns the final state of the current entry for a subject and
// predicate, or nil.
func (s *Store) currentEntry(subject, predicate string) *Entry {
	for _, e := range s.byID {
		if e.Status == StatusCurrent && e.Subject == subject && e.Predicate == predicate {
			return e
		}
	}
	return nil
}

// withinValidity reports whether an entry is valid at time t. Boundaries are
// inclusive.
func withinValidity(e Entry, t time.Time) bool {
	if e.ValidFrom != nil && t.Before(*e.ValidFrom) {
		return false
	}
	if e.ValidUntil != nil && t.After(*e.ValidUntil) {
		return false
	}
	return true
}

func allFinal(byID map[string]*Entry) []Entry {
	out := make([]Entry, 0, len(byID))
	for _, e := range byID {
		out = append(out, *e)
	}
	return out
}

func sortedByID(entries []Entry) []Entry {
	sort.Slice(entries, func(i, j int) bool { return entries[i].ID < entries[j].ID })
	return entries
}

// compactJSON canonicalizes a raw JSON value so serialization is deterministic
// regardless of the caller's whitespace.
func compactJSON(raw json.RawMessage) (json.RawMessage, error) {
	if len(raw) == 0 {
		return nil, fmt.Errorf("value is required")
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, fmt.Errorf("value is not valid JSON: %w", err)
	}
	return buf.Bytes(), nil
}

// HasCurrent reports whether entries already contains a CURRENT entry
// with the same kind, subject, predicate, and value as want — i.e. the
// candidate is a restatement of a memory Ghost already holds. Used as a
// deterministic dedup guard on semantic extraction writes.
func HasCurrent(entries []Entry, want Entry) bool {
	for _, e := range entries {
		if e.Status != StatusCurrent || e.Kind != want.Kind || e.Subject != want.Subject || e.Predicate != want.Predicate {
			continue
		}
		if bytes.Equal(e.Value, want.Value) {
			return true
		}
	}
	return false
}
