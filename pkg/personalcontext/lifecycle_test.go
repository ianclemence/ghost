package personalcontext

import (
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// E. Forget: retiring an entry removes it from current context, keeps it in
// history with provenance, and never touches the conversation it came from.
func TestForget(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)

	src := declaredSource()
	green := mkEntry("pc-green", "user", "favorite_color", "green")
	green.Sources = []Source{src}
	if _, err := s.Create(green); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := s.Forget("pc-green"); err != nil {
		t.Fatalf("Forget: %v", err)
	}

	if cur := s.Current(); len(cur) != 0 {
		t.Fatalf("Current after forget = %+v, want nothing", cur)
	}

	got, ok := s.Get("pc-green")
	if !ok {
		t.Fatal("forgotten entry must remain inspectable")
	}
	if got.Status != StatusRejected {
		t.Fatalf("forgotten entry status = %q, want rejected", got.Status)
	}
	if len(got.Sources) != 1 || got.Sources[0].Ref != src.Ref {
		t.Fatalf("provenance not preserved after forget: %+v", got.Sources)
	}

	hist := s.History("pc-green")
	if len(hist) != 2 || hist[1].Status != StatusRejected {
		t.Fatalf("history after forget = %+v, want original + rejected revision", hist)
	}
	if got := lineCount(t, s.Path()); got != 2 {
		t.Fatalf("log lines = %d, want 2 (original + rejected revision, nothing deleted)", got)
	}

	// Forgetting an unknown entry is an explicit error.
	if err := s.Forget("pc-nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Forget(missing) = %v, want ErrNotFound", err)
	}
}

// F. Conflict: two unresolved inferred values are represented as conflicting;
// neither is silently selected as current.
func TestConflict(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)

	a := mkEntry("pc-a", "user", "height", "tall")
	a.Kind = KindFact
	a.Confidence = 0.5
	a.Sources = []Source{inferredSource()}
	b := mkEntry("pc-b", "user", "height", "short")
	b.Kind = KindFact
	b.Confidence = 0.5
	b.Sources = []Source{inferredSource()}
	if _, err := s.Create(a); err != nil {
		t.Fatalf("create a: %v", err)
	}
	if _, err := s.Create(b); err != nil {
		t.Fatalf("create b: %v", err)
	}

	if err := s.DeclareConflict("user", "height", "pc-a", "pc-b"); err != nil {
		t.Fatalf("DeclareConflict: %v", err)
	}

	if cur := s.Current(); len(cur) != 0 {
		t.Fatalf("Current after conflict = %+v, want nothing silently selected", cur)
	}
	for _, id := range []string{"pc-a", "pc-b"} {
		got, _ := s.Get(id)
		if got.Status != StatusConflicting {
			t.Fatalf("%s status = %q, want conflicting", id, got.Status)
		}
		if len(got.Sources) != 1 || got.Sources[0].Kind != SourceInferred {
			t.Fatalf("%s provenance lost: %+v", id, got.Sources)
		}
		if hist := s.History(id); len(hist) != 2 {
			t.Fatalf("%s history = %d records, want 2", id, len(hist))
		}
	}

	// Conflict marking is guarded: both entries must exist, be current, and
	// match the subject/predicate.
	if err := s.DeclareConflict("user", "height", "pc-a", "pc-nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("DeclareConflict(missing) = %v, want ErrNotFound", err)
	}
	if err := s.DeclareConflict("user", "other", "pc-a", "pc-b"); err == nil {
		t.Fatal("DeclareConflict(mismatched predicate) should fail")
	}
	if err := s.DeclareConflict("user", "height", "pc-a", "pc-a"); err == nil {
		t.Fatal("DeclareConflict(same entry) should fail")
	}
}

// G. Temporal validity: entries outside their validity window are excluded
// from the current-context query at the reference time.
func TestTemporalValidity(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)

	seasonal := mkEntry("pc-seasonal", "user", "favorite_drink", "iced_coffee")
	vFrom := mustTime(t, "2026-06-01T00:00:00Z")
	vUntil := mustTime(t, "2026-09-01T00:00:00Z")
	seasonal.ValidFrom = &vFrom
	seasonal.ValidUntil = &vUntil
	if _, err := s.Create(seasonal); err != nil {
		t.Fatalf("create seasonal: %v", err)
	}

	expired := mkEntry("pc-expired", "user", "old_phone", "nokia")
	old := mustTime(t, "2020-01-01T00:00:00Z")
	expired.ValidUntil = &old
	if _, err := s.Create(expired); err != nil {
		t.Fatalf("create expired: %v", err)
	}

	if cur := s.CurrentAt(mustTime(t, "2026-07-01T00:00:00Z")); len(cur) != 1 || cur[0].ID != "pc-seasonal" {
		t.Fatalf("CurrentAt(summer) = %+v, want only seasonal", cur)
	}
	if cur := s.CurrentAt(mustTime(t, "2026-05-01T00:00:00Z")); len(cur) != 0 {
		t.Fatalf("CurrentAt(before valid_from) = %+v, want nothing", cur)
	}
	if cur := s.CurrentAt(mustTime(t, "2026-10-01T00:00:00Z")); len(cur) != 0 {
		t.Fatalf("CurrentAt(after valid_until) = %+v, want nothing", cur)
	}
	if cur := s.CurrentAt(mustTime(t, "2026-07-01T00:00:00Z")); hasID(cur, "pc-expired") {
		t.Fatal("expired entry leaked into current context")
	}

	// The entry still exists and is inspectable; it is only hidden from the
	// current query.
	if got, ok := s.Get("pc-seasonal"); !ok || got.Status != StatusCurrent {
		t.Fatalf("expired-but-current entry should still be queryable: %+v", got)
	}
}

func hasID(entries []Entry, id string) bool {
	for _, e := range entries {
		if e.ID == id {
			return true
		}
	}
	return false
}

// H. Invalid data: malformed entries are rejected rather than appended.
func TestInvalidDataRejected(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)

	invalidFrom := mustTime(t, "2026-02-01T00:00:00Z")
	invalidUntil := mustTime(t, "2026-01-01T00:00:00Z")

	cases := []struct {
		name string
		mut  func(e *Entry)
	}{
		{"empty id", func(e *Entry) { e.ID = "" }},
		{"empty subject", func(e *Entry) { e.Subject = "" }},
		{"empty predicate", func(e *Entry) { e.Predicate = "" }},
		{"invalid kind", func(e *Entry) { e.Kind = "bogus" }},
		{"invalid status", func(e *Entry) { e.Status = "bogus" }},
		{"negative confidence", func(e *Entry) { e.Confidence = -0.1 }},
		{"confidence over 1", func(e *Entry) { e.Confidence = 1.1 }},
		{"missing value", func(e *Entry) { e.Value = nil }},
		{"invalid JSON value", func(e *Entry) { e.Value = json.RawMessage(`{"a":`) }},
		{"inverted validity", func(e *Entry) {
			e.ValidFrom = &invalidFrom
			e.ValidUntil = &invalidUntil
		}},
		{"invalid source type", func(e *Entry) { e.Sources[0].Type = "bogus" }},
		{"invalid source kind", func(e *Entry) { e.Sources[0].Kind = "bogus" }},
		{"source without timestamp", func(e *Entry) { e.Sources[0].Timestamp = time.Time{} }},
	}
	for _, tc := range cases {
		e := mkEntry("pc-"+tc.name, "user", "predicate", "v")
		tc.mut(&e)
		if _, err := s.Create(e); err == nil {
			t.Errorf("%s: Create should have failed", tc.name)
		}
	}

	// A duplicate id is a revision attempt, not a create, and is rejected.
	if _, err := s.Create(mkEntry("pc-dup", "user", "p", "v")); err != nil {
		t.Fatalf("create pc-dup: %v", err)
	}
	if _, err := s.Create(mkEntry("pc-dup", "user", "p2", "v2")); !errors.Is(err, ErrDuplicateID) {
		t.Fatalf("Create(duplicate) = %v, want ErrDuplicateID", err)
	}

	// The log is unchanged by all the failed writes.
	if got := lineCount(t, s.Path()); got != 1 {
		t.Fatalf("log lines = %d, want 1 (only pc-dup written)", got)
	}
}

// I. Round-trip: all fields, including JSON values, timestamps, sources,
// status, temporal validity, and supersession, survive write/load.
func TestRoundTrip(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)

	// A supersession pair with rich fields.
	decision := mkEntry("pc-d1", "user", "coffee_roast", "light")
	decision.Kind = KindDecision
	decision.Confidence = 0.8
	vf := mustTime(t, "2026-01-15T00:00:00Z")
	decision.ValidFrom = &vf
	decision.Sources = []Source{
		{Type: SourceConversation, Kind: SourceUserDeclared, Ref: "telegram:42:msg-1", Timestamp: mustTime(t, "2026-01-15T00:00:00Z")},
		{Type: SourceConversation, Kind: SourceInferred, Ref: "telegram:42:msg-2", Timestamp: mustTime(t, "2026-01-16T00:00:00Z")},
	}
	if _, err := s.Create(decision); err != nil {
		t.Fatalf("create decision: %v", err)
	}
	replacement := mkEntry("pc-d2", "user", "coffee_roast", "dark")
	replacement.Kind = KindDecision
	replacement.Confidence = 0.9
	if _, err := s.Supersede("user", "coffee_roast", replacement); err != nil {
		t.Fatalf("supersede: %v", err)
	}

	// A conflicting pair and an expired entry.
	a := mkEntry("pc-c1", "user", "height", "tall")
	a.Kind = KindFact
	a.Confidence = 0.5
	a.Sources = []Source{inferredSource()}
	b := mkEntry("pc-c2", "user", "height", "short")
	b.Kind = KindFact
	b.Confidence = 0.5
	b.Sources = []Source{inferredSource()}
	if _, err := s.Create(a); err != nil {
		t.Fatalf("create c1: %v", err)
	}
	if _, err := s.Create(b); err != nil {
		t.Fatalf("create c2: %v", err)
	}
	if err := s.DeclareConflict("user", "height", "pc-c1", "pc-c2"); err != nil {
		t.Fatalf("conflict: %v", err)
	}

	expired := mkEntry("pc-e1", "user", "old_phone", "nokia")
	until := mustTime(t, "2020-01-01T00:00:00Z")
	expired.ValidUntil = &until
	if _, err := s.Create(expired); err != nil {
		t.Fatalf("create expired: %v", err)
	}
	if err := s.Forget("pc-e1"); err != nil {
		t.Fatalf("forget: %v", err)
	}

	reloaded := mustOpen(t, ws)
	ids := []string{"pc-d1", "pc-d2", "pc-c1", "pc-c2", "pc-e1"}
	for _, id := range ids {
		want, ok := s.Get(id)
		if !ok {
			t.Fatalf("%s missing from source store", id)
		}
		got, ok := reloaded.Get(id)
		if !ok {
			t.Fatalf("%s missing after reload", id)
		}
		if !marshalEqual(t, got, want) {
			t.Fatalf("%s changed across reload:\n got %s\nwant %s", id, marshalJSON(got), marshalJSON(want))
		}
		wantHist := s.History(id)
		gotHist := reloaded.History(id)
		if len(gotHist) != len(wantHist) {
			t.Fatalf("%s history length = %d, want %d", id, len(gotHist), len(wantHist))
		}
		for i := range wantHist {
			if !marshalEqual(t, gotHist[i], wantHist[i]) {
				t.Fatalf("%s history[%d] changed across reload", id, i)
			}
		}
	}

	// Status invariants survive reload.
	if got, _ := reloaded.Get("pc-d1"); got.Status != StatusSuperseded || got.SupersededBy == nil || *got.SupersededBy != "pc-d2" {
		t.Fatalf("pc-d1 after reload = %+v, want superseded by pc-d2", got)
	}
	if got, _ := reloaded.Get("pc-e1"); got.Status != StatusRejected {
		t.Fatalf("pc-e1 after reload = %+v, want rejected", got)
	}
	if cur := reloaded.CurrentAt(mustTime(t, "2026-03-01T00:00:00Z")); len(cur) != 1 || cur[0].ID != "pc-d2" {
		t.Fatalf("current after reload = %+v, want only pc-d2", cur)
	}
}
