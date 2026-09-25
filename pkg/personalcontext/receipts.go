package personalcontext

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrTombstoned reports that a belief was forgotten by its owner and the write
// would resurrect it from evidence older than the forgetting. A declaration
// from a newer message is allowed — the owner can always change their mind.
var ErrTombstoned = fmt.Errorf("belief was forgotten by the owner")

// TombstonesFile is the append-only record of owner-forgotten beliefs.
const TombstonesFile = "tombstones.jsonl"

// TombstonesPath returns the tombstone log for a workspace.
func TombstonesPath(workspace string) string {
	return filepath.Join(workspace, EntriesDir, TombstonesFile)
}

// Tombstone is the durable receipt of a forgetting. It exists so that
// background derivation can never quietly restore a belief the owner removed:
// a matching subject+predicate+value is refused unless a newer message
// declares it again. Tombstones hold no personal content beyond the key.
type Tombstone struct {
	ID        string    `json:"id"`
	Subject   string    `json:"subject"`
	Predicate string    `json:"predicate"`
	ValueHash string    `json:"value_hash"`
	Reason    string    `json:"reason,omitempty"`
	At        time.Time `json:"at"`
}

func (s *Store) tombstonesPath() string {
	return filepath.Join(filepath.Dir(s.path), TombstonesFile)
}

func (s *Store) appendTombstone(t Tombstone) error {
	line, err := json.Marshal(t)
	if err != nil {
		return fmt.Errorf("marshal tombstone %s: %w", t.ID, err)
	}
	line = append(line, '\n')
	f, err := os.OpenFile(s.tombstonesPath(), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open tombstones log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(line); err != nil {
		return fmt.Errorf("append tombstone %s: %w", t.ID, err)
	}
	return nil
}

// Tombstones returns every recorded forgetting, oldest first. A missing log is
// an empty list, never an error.
func (s *Store) Tombstones() []Tombstone {
	f, err := os.Open(s.tombstonesPath())
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []Tombstone
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var t Tombstone
		if err := json.Unmarshal([]byte(line), &t); err != nil {
			continue // a malformed tombstone must never block reading the rest
		}
		out = append(out, t)
	}
	return out
}

// IsTombstoned reports whether the given belief was forgotten, returning the
// tombstone so callers can compare its timestamp against new evidence.
func (s *Store) IsTombstoned(subject, predicate string, value json.RawMessage) (Tombstone, bool) {
	want := valueHash(value)
	for _, t := range s.Tombstones() {
		if t.Subject == subject && t.Predicate == predicate && t.ValueHash == want {
			return t, true
		}
	}
	return Tombstone{}, false
}

// ForgetWithReason retracts an entry, records why, and writes the tombstone
// that keeps it from being re-derived. It returns the retracted revision.
func (s *Store) ForgetWithReason(id, reason string) (Entry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	e, ok := s.byID[id]
	if !ok {
		return Entry{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	now := time.Now().UTC()
	rev := *e
	rev.Status = StatusRejected
	rev.RetractReason = strings.TrimSpace(reason)
	rev.RetractedAt = &now
	rev.UpdatedAt = now
	if err := s.append(rev); err != nil {
		return Entry{}, err
	}
	if err := s.appendTombstone(Tombstone{
		ID:        id,
		Subject:   e.Subject,
		Predicate: e.Predicate,
		ValueHash: valueHash(e.Value),
		Reason:    strings.TrimSpace(reason),
		At:        now,
	}); err != nil {
		return rev, err
	}
	return rev, nil
}

// ForgetReport is what the owner (and the audit trail) sees after a
// forgetting: what was retired and which derived artifacts were rebuilt so
// nothing can serve it again.
type ForgetReport struct {
	ClaimID            string   `json:"claim_id"`
	Forgotten          bool     `json:"forgotten"`
	Tombstoned         bool     `json:"tombstoned"`
	DerivativesRebuilt []string `json:"derivatives_rebuilt,omitempty"`
}

// ForgetPipeline runs the full forgetting: retract the claim, write the
// tombstone, then rebuild the derived artifacts that could otherwise still
// serve it (the digest and the curated profile). This is the difference
// between deleting a line and actually forgetting.
func ForgetPipeline(workspace, id, reason string) (ForgetReport, error) {
	report := ForgetReport{ClaimID: id}
	store, err := Open(workspace)
	if err != nil {
		return report, err
	}
	if _, err := store.ForgetWithReason(id, reason); err != nil {
		return report, err
	}
	report.Forgotten = true
	report.Tombstoned = true

	if _, _, err := Compact(store); err == nil {
		report.DerivativesRebuilt = append(report.DerivativesRebuilt, "digest")
	}
	if _, err := MaterializeCuratedProfile(workspace, store); err == nil {
		report.DerivativesRebuilt = append(report.DerivativesRebuilt, "profile")
	}
	return report, nil
}

// Explanation is the receipt for one belief: the current state, the revisions
// that produced it, the message ids it came from, and whether it was later
// superseded or forgotten. It is deliberately plain data — the console, the
// phone, and the model all render the same facts.
type Explanation struct {
	Entry           Entry      `json:"entry"`
	Kind            string     `json:"kind"`
	Title           string     `json:"title"`
	Summary         string     `json:"summary"`
	Value           string     `json:"value"`
	History         []Entry    `json:"history,omitempty"`
	SupersededBy    *Entry     `json:"superseded_by,omitempty"`
	MessageIDs      []string   `json:"message_ids,omitempty"`
	Quote           string     `json:"quote,omitempty"`
	Confidence      float64    `json:"confidence"`
	ForgottenAt     *time.Time `json:"forgotten_at,omitempty"`
	ForgottenReason string     `json:"forgotten_reason,omitempty"`
	Tombstoned      bool       `json:"tombstoned"`
}

// Explain returns the full receipt for an entry id. It never fails for a
// forgotten entry: a forgotten belief must stay explainable, or forgetting
// would be indistinguishable from corruption.
func (s *Store) Explain(id string) (Explanation, error) {
	e, ok := s.Get(id)
	if !ok {
		return Explanation{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	ex := Explanation{
		Entry:      e,
		Kind:       string(e.Kind),
		Title:      Title(e),
		Summary:    Summary(e),
		Value:      Value(e),
		History:    s.History(id),
		MessageIDs: MessageIDsFromSources(e.Sources),
		Quote:      e.Quote,
		Confidence: e.Confidence,
	}
	if e.SupersededBy != nil {
		if by, ok := s.Get(*e.SupersededBy); ok {
			ex.SupersededBy = &by
		}
	}
	if e.RetractedAt != nil {
		ex.ForgottenAt = e.RetractedAt
		ex.ForgottenReason = e.RetractReason
	}
	if _, ok := s.IsTombstoned(e.Subject, e.Predicate, e.Value); ok {
		ex.Tombstoned = true
	}
	return ex, nil
}

// MessageIDsFromSources extracts the conversation message ids recorded in a
// belief's sources. Source refs are "session_id:message_id"; only the message
// id is returned because that is what a person can look up.
func MessageIDsFromSources(sources []Source) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range sources {
		ref := strings.TrimSpace(s.Ref)
		if ref == "" {
			continue
		}
		msg := ref
		if i := strings.LastIndex(ref, ":"); i >= 0 {
			msg = ref[i+1:]
		}
		msg = strings.TrimSpace(msg)
		if msg == "" || seen[msg] {
			continue
		}
		seen[msg] = true
		out = append(out, msg)
	}
	return out
}

// newestSourceTime is the most recent source timestamp on an entry, used to
// decide whether new evidence postdates a forgetting.
func (e Entry) newestSourceTime() time.Time {
	var newest time.Time
	for _, s := range e.Sources {
		if s.Timestamp.After(newest) {
			newest = s.Timestamp
		}
	}
	if newest.IsZero() {
		newest = e.CreatedAt
	}
	return newest
}

func valueHash(value json.RawMessage) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(string(value)))))
	return hex.EncodeToString(sum[:8])
}
