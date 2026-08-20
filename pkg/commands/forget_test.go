package commands

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/session"
)

func newForgetStore(t *testing.T) *personalcontext.Store {
	t.Helper()
	s, err := personalcontext.Open(t.TempDir())
	if err != nil {
		t.Fatalf("open personal context: %v", err)
	}
	return s
}

func runForgetHandler(t *testing.T, store *personalcontext.Store, sessions *session.SessionManager, text string) string {
	t.Helper()
	var out string
	rt := &Runtime{PersonalContext: store, Sessions: sessions}
	req := Request{
		Text:       text,
		Channel:    "cli",
		ChatID:     "direct",
		SessionKey: "s1",
		Reply: func(s string) error {
			out = s
			return nil
		},
	}
	if err := forgetHandler(context.Background(), req, rt); err != nil {
		t.Fatalf("forgetHandler(%q): %v", text, err)
	}
	return out
}

// forgetSource builds a provenance source referencing a session for entry
// deletion tests.
func forgetSource(ref string) personalcontext.Source {
	return personalcontext.Source{
		Type:      personalcontext.SourceConversation,
		Kind:      personalcontext.SourceUserDeclared,
		Ref:       ref,
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// forgetEntry builds a current entry with deterministic provenance.
func forgetEntry(id, kind, subject, predicate string, value interface{}, ref string) personalcontext.Entry {
	e := ctxEntry(id, kind, subject, predicate, value)
	e.Sources = []personalcontext.Source{forgetSource(ref)}
	return e
}

// A. Forgetting a current entry retires it; provenance is preserved.
func TestForgetCurrentEntry(t *testing.T) {
	store := newForgetStore(t)
	mustCreate(t, store, forgetEntry("e1", "preference", "user", "preference/favorite_color", "green", "s1:m1"))

	out := runForgetHandler(t, store, nil, "/forget favorite_color")
	if !strings.Contains(out, "Forgotten: preference/favorite_color") {
		t.Fatalf("unexpected response: %q", out)
	}

	e, ok := store.Get("e1")
	if !ok {
		t.Fatal("entry e1 missing")
	}
	if e.Status != personalcontext.StatusRejected {
		t.Fatalf("status = %s, want rejected", e.Status)
	}
	if len(e.Sources) != 1 || e.Sources[0].Ref != "s1:m1" {
		t.Fatalf("provenance changed after forget: %+v", e.Sources)
	}
}

// C. History records the retirement as a second revision; the original record
// stays intact.
func TestForgetKeepsHistory(t *testing.T) {
	store := newForgetStore(t)
	mustCreate(t, store, forgetEntry("e1", "preference", "user", "preference/favorite_color", "green", "s1:m1"))

	runForgetHandler(t, store, nil, "/forget favorite_color")

	hist := store.History("e1")
	if len(hist) != 2 {
		t.Fatalf("history length = %d, want 2", len(hist))
	}
	if hist[0].Status != personalcontext.StatusCurrent {
		t.Fatalf("first revision status = %s, want current", hist[0].Status)
	}
	if hist[1].Status != personalcontext.StatusRejected {
		t.Fatalf("second revision status = %s, want rejected", hist[1].Status)
	}
}

// D. After a supersession, /forget retires the current value; the superseded
// one stays superseded (not rejected).
func TestForgetAfterSupersession(t *testing.T) {
	store := newForgetStore(t)
	mustCreate(t, store, forgetEntry("e1", "preference", "user", "preference/favorite_color", "blue", "s1:m1"))
	green := forgetEntry("e2", "preference", "user", "preference/favorite_color", "green", "s1:m2")
	if _, err := store.Supersede("user", "preference/favorite_color", green); err != nil {
		t.Fatalf("Supersede: %v", err)
	}

	runForgetHandler(t, store, nil, "/forget favorite_color")

	e1, _ := store.Get("e1")
	if e1.Status != personalcontext.StatusSuperseded {
		t.Fatalf("superseded entry status = %s, want superseded", e1.Status)
	}
	e2, _ := store.Get("e2")
	if e2.Status != personalcontext.StatusRejected {
		t.Fatalf("current entry status = %s, want rejected", e2.Status)
	}
}

// E. A repeated /forget reports "already forgotten" and appends no extra
// revision.
func TestForgetAlreadyForgotten(t *testing.T) {
	store := newForgetStore(t)
	mustCreate(t, store, forgetEntry("e1", "preference", "user", "preference/favorite_color", "green", "s1:m1"))

	runForgetHandler(t, store, nil, "/forget favorite_color")
	out := runForgetHandler(t, store, nil, "/forget favorite_color")

	if !strings.Contains(out, "already forgotten") {
		t.Fatalf("unexpected response: %q", out)
	}
	if hist := store.History("e1"); len(hist) != 2 {
		t.Fatalf("history length = %d, want 2 (no duplicate revision)", len(hist))
	}
}

// F. An unknown target is reported clearly and mutates nothing.
func TestForgetNoMatch(t *testing.T) {
	store := newForgetStore(t)
	mustCreate(t, store, forgetEntry("e1", "preference", "user", "preference/favorite_color", "green", "s1:m1"))

	before := len(store.All())
	out := runForgetHandler(t, store, nil, "/forget favorite_food")

	if !strings.Contains(out, "No current Personal Context entry matches") {
		t.Fatalf("unexpected response: %q", out)
	}
	if len(store.All()) != before {
		t.Fatalf("store mutated on no match")
	}
}

// G. An ambiguous target spanning distinct predicates is refused and nothing
// is retired.
func TestForgetAmbiguousDistinctPredicates(t *testing.T) {
	store := newForgetStore(t)
	mustCreate(t, store, forgetEntry("e1", "fact", "user", "fact/location", "Bangkok", "s1:m1"))
	mustCreate(t, store, forgetEntry("e2", "preference", "user", "preference/location_format", "city, country", "s1:m2"))

	out := runForgetHandler(t, store, nil, "/forget location")

	for _, want := range []string{
		"Multiple Personal Context entries match",
		"fact/location",
		"preference/location_format",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if e1, _ := store.Get("e1"); e1.Status != personalcontext.StatusCurrent {
		t.Fatalf("e1 retired despite ambiguity: %s", e1.Status)
	}
	if e2, _ := store.Get("e2"); e2.Status != personalcontext.StatusCurrent {
		t.Fatalf("e2 retired despite ambiguity: %s", e2.Status)
	}
}

// G2. A bare kind with parallel current entries (same predicate, different
// values) is ambiguous and refused — never a silent mass delete.
func TestForgetBareKindParallelValuesAmbiguous(t *testing.T) {
	store := newForgetStore(t)
	mustCreate(t, store, forgetEntry("e1", "relationship", "user", "relationship/partner", "Jane", "s1:m1"))
	mustCreate(t, store, forgetEntry("e2", "relationship", "user", "relationship/partner", "Bob", "s1:m2"))

	out := runForgetHandler(t, store, nil, "/forget relationship")

	if !strings.Contains(out, "Multiple Personal Context entries match") {
		t.Fatalf("expected ambiguity, got: %q", out)
	}
	if !strings.Contains(out, "Jane") || !strings.Contains(out, "Bob") {
		t.Fatalf("ambiguity listing should show both values:\n%s", out)
	}
	if e1, _ := store.Get("e1"); e1.Status != personalcontext.StatusCurrent {
		t.Fatalf("e1 retired despite ambiguity: %s", e1.Status)
	}
	if e2, _ := store.Get("e2"); e2.Status != personalcontext.StatusCurrent {
		t.Fatalf("e2 retired despite ambiguity: %s", e2.Status)
	}
}

// H. A conflicting pair for one belief is retired together; /context then
// reports no unresolved state.
func TestForgetConflictPairRetiredTogether(t *testing.T) {
	store := newForgetStore(t)
	mustCreate(t, store, forgetEntry("e1", "preference", "user", "preference/favorite_color", "green", "s1:m1"))
	mustCreate(t, store, forgetEntry("e2", "preference", "user", "preference/favorite_color", "blue", "s1:m2"))
	if err := store.DeclareConflict("user", "preference/favorite_color", "e1", "e2"); err != nil {
		t.Fatalf("DeclareConflict: %v", err)
	}

	out := runForgetHandler(t, store, nil, "/forget favorite_color")
	if !strings.Contains(out, "Forgotten 2 Personal Context entries for preference/favorite_color.") {
		t.Fatalf("unexpected response: %q", out)
	}

	for _, id := range []string{"e1", "e2"} {
		if e, _ := store.Get(id); e.Status != personalcontext.StatusRejected {
			t.Fatalf("%s status = %s, want rejected", id, e.Status)
		}
	}
	ctxOut := runContextHandler(t, store, "/context")
	if strings.Contains(ctxOut, "Unresolved") {
		t.Fatalf("/context still shows unresolved after forgetting conflict:\n%s", ctxOut)
	}
}

// I. Expired and future-valid entries are not current context and are never
// retired by /forget.
func TestForgetIgnoresExpiredAndFuture(t *testing.T) {
	store := newForgetStore(t)
	now := time.Now()
	expired := forgetEntry("e1", "fact", "user", "fact/location", "Old", "s1:m1")
	until := now.Add(-time.Hour)
	expired.ValidUntil = &until
	mustCreate(t, store, expired)
	future := forgetEntry("e2", "preference", "user", "preference/language", "French", "s1:m2")
	from := now.Add(time.Hour)
	future.ValidFrom = &from
	mustCreate(t, store, future)

	out := runForgetHandler(t, store, nil, "/forget location")
	if !strings.Contains(out, "No current Personal Context entry matches") {
		t.Fatalf("expired entry should not be forgotten: %q", out)
	}
	out = runForgetHandler(t, store, nil, "/forget language")
	if !strings.Contains(out, "No current Personal Context entry matches") {
		t.Fatalf("future-valid entry should not be forgotten: %q", out)
	}
	if e1, _ := store.Get("e1"); e1.Status != personalcontext.StatusCurrent {
		t.Fatalf("expired entry retired: %s", e1.Status)
	}
	if e2, _ := store.Get("e2"); e2.Status != personalcontext.StatusCurrent {
		t.Fatalf("future-valid entry retired: %s", e2.Status)
	}
}

// Everything-about: a bare /forget everything is refused, never a full wipe.
func TestForgetEverythingRefused(t *testing.T) {
	store := newForgetStore(t)
	mustCreate(t, store, forgetEntry("e1", "preference", "user", "preference/favorite_color", "green", "s1:m1"))

	out := runForgetHandler(t, store, nil, "/forget everything")
	if !strings.Contains(out, "Refusing") {
		t.Fatalf("expected refusal, got: %q", out)
	}
	if e1, _ := store.Get("e1"); e1.Status != personalcontext.StatusCurrent {
		t.Fatalf("e1 retired by bare /forget everything: %s", e1.Status)
	}
}

// Everything-about: topic-scoped retirement retires the matching entries.
func TestForgetEverythingAboutTopic(t *testing.T) {
	store := newForgetStore(t)
	mustCreate(t, store, forgetEntry("e1", "fact", "user", "fact/location", "Bangkok", "s1:m1"))
	mustCreate(t, store, forgetEntry("e2", "preference", "user", "preference/location_format", "city, country", "s1:m2"))

	out := runForgetHandler(t, store, nil, "/forget everything about location")
	if !strings.Contains(out, "Forgotten 2 Personal Context entries related to") {
		t.Fatalf("unexpected response: %q", out)
	}
	if e1, _ := store.Get("e1"); e1.Status != personalcontext.StatusRejected {
		t.Fatalf("e1 not retired: %s", e1.Status)
	}
	if e2, _ := store.Get("e2"); e2.Status != personalcontext.StatusRejected {
		t.Fatalf("e2 not retired: %s", e2.Status)
	}
}

// Everything-about: a named relationship partner retires only that partner's
// entries, leaving other relationship entries intact.
func TestForgetEverythingAboutRelationshipPartner(t *testing.T) {
	store := newForgetStore(t)
	mustCreate(t, store, forgetEntry("e1", "relationship", "user", "relationship/partner", "Jane", "s1:m1"))
	mustCreate(t, store, forgetEntry("e2", "relationship", "user", "relationship/partner", "Bob", "s1:m2"))

	out := runForgetHandler(t, store, nil, "/forget everything about my relationship with Jane")
	if !strings.Contains(out, "Forgotten 1 Personal Context entry related to") {
		t.Fatalf("unexpected response: %q", out)
	}
	if e1, _ := store.Get("e1"); e1.Status != personalcontext.StatusRejected {
		t.Fatalf("Jane entry not retired: %s", e1.Status)
	}
	if e2, _ := store.Get("e2"); e2.Status != personalcontext.StatusCurrent {
		t.Fatalf("Bob entry wrongly retired: %s", e2.Status)
	}
}

// Everything-about: generic self-referential topics are refused.
func TestForgetEverythingAboutSelfRefused(t *testing.T) {
	store := newForgetStore(t)
	mustCreate(t, store, forgetEntry("e1", "identity", "user", "identity/name", "Ian", "s1:m1"))

	out := runForgetHandler(t, store, nil, "/forget everything about me")
	if !strings.Contains(out, "Refusing") {
		t.Fatalf("expected refusal, got: %q", out)
	}
}

// J. /forget session deletes the conversation evidence and retires only the
// Personal Context entries whose provenance references that session.
func TestForgetSessionDeletesEvidence(t *testing.T) {
	store := newForgetStore(t)
	sm := session.NewSessionManager(session.NewJSONLStore(t.TempDir()), nil)
	sm.AddMessage("s1", "user", "my favorite color is green")
	sm.AddMessage("s1", "assistant", "noted")
	sm.AddMessage("s2", "user", "unrelated")
	mustCreate(t, store, forgetEntry("e1", "preference", "user", "preference/favorite_color", "green", "s1:m1"))
	mustCreate(t, store, forgetEntry("e2", "fact", "user", "fact/location", "Bangkok", "s2:m1"))

	out := runForgetHandler(t, store, sm, "/forget session s1")
	if !strings.Contains(out, `Deleted session "s1" and retired 1 dependent Personal Context entry.`) {
		t.Fatalf("unexpected response: %q", out)
	}
	if hist := sm.GetHistory("s1"); len(hist) != 0 {
		t.Fatalf("session s1 evidence not deleted, %d messages remain", len(hist))
	}
	if e1, _ := store.Get("e1"); e1.Status != personalcontext.StatusRejected {
		t.Fatalf("dependent entry not retired: %s", e1.Status)
	}
	if e2, _ := store.Get("e2"); e2.Status != personalcontext.StatusCurrent {
		t.Fatalf("unrelated entry retired: %s", e2.Status)
	}
	if hist := sm.GetHistory("s2"); len(hist) != 1 {
		t.Fatalf("session s2 evidence deleted: %d messages", len(hist))
	}
}

// J2. /forget session with no dependent entries still deletes the evidence.
func TestForgetSessionNoDependents(t *testing.T) {
	store := newForgetStore(t)
	sm := session.NewSessionManager(session.NewJSONLStore(t.TempDir()), nil)
	sm.AddMessage("s1", "user", "hello")

	out := runForgetHandler(t, store, sm, "/forget session s1")
	if !strings.Contains(out, `Deleted session "s1".`) {
		t.Fatalf("unexpected response: %q", out)
	}
	if hist := sm.GetHistory("s1"); len(hist) != 0 {
		t.Fatalf("session evidence not deleted")
	}
}

// K. An unknown session id is reported clearly and mutates nothing.
func TestForgetSessionNotFound(t *testing.T) {
	store := newForgetStore(t)
	sm := session.NewSessionManager(session.NewJSONLStore(t.TempDir()), nil)
	sm.AddMessage("s1", "user", "hello")
	mustCreate(t, store, forgetEntry("e1", "preference", "user", "preference/favorite_color", "green", "s1:m1"))

	out := runForgetHandler(t, store, sm, "/forget session nope")
	if !strings.Contains(out, `No session found with id "nope".`) {
		t.Fatalf("unexpected response: %q", out)
	}
	if hist := sm.GetHistory("s1"); len(hist) != 1 {
		t.Fatalf("existing session mutated: %d messages", len(hist))
	}
	if e1, _ := store.Get("e1"); e1.Status != personalcontext.StatusCurrent {
		t.Fatalf("entry retired for nonexistent session: %s", e1.Status)
	}
}

// K2. /forget session with no id reports usage.
func TestForgetSessionNoID(t *testing.T) {
	store := newForgetStore(t)
	sm := session.NewSessionManager(session.NewJSONLStore(t.TempDir()), nil)

	out := runForgetHandler(t, store, sm, "/forget session")
	if !strings.Contains(out, "Usage: /forget session <session-id>") {
		t.Fatalf("unexpected response: %q", out)
	}
}

// Plain /forget with no target reports usage and never wipes anything.
func TestForgetNoTarget(t *testing.T) {
	store := newForgetStore(t)
	mustCreate(t, store, forgetEntry("e1", "preference", "user", "preference/favorite_color", "green", "s1:m1"))

	out := runForgetHandler(t, store, nil, "/forget")
	if !strings.Contains(out, "Usage: /forget") {
		t.Fatalf("unexpected response: %q", out)
	}
	if e1, _ := store.Get("e1"); e1.Status != personalcontext.StatusCurrent {
		t.Fatalf("e1 retired by bare /forget: %s", e1.Status)
	}
}

// The predicate-suffix and "my " phrase forms resolve to the same entry.
func TestForgetPhraseForms(t *testing.T) {
	for _, text := range []string{
		"/forget preference/favorite_color",
		"/forget favorite_color",
		"/forget my favorite color",
		"/forget favorite color",
	} {
		store := newForgetStore(t)
		mustCreate(t, store, forgetEntry("e1", "preference", "user", "preference/favorite_color", "green", "s1:m1"))
		out := runForgetHandler(t, store, nil, text)
		if !strings.Contains(out, "Forgotten: preference/favorite_color") {
			t.Fatalf("form %q: unexpected response: %q", text, out)
		}
		if e1, _ := store.Get("e1"); e1.Status != personalcontext.StatusRejected {
			t.Fatalf("form %q did not retire entry: %s", text, e1.Status)
		}
	}
}
