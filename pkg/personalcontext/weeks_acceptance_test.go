package personalcontext

import (
	"encoding/json"
	"testing"
	"time"
)

// Acceptance criteria for "memory across weeks" (see the capability spec):
//
//  1. A fact filed on day 1 is retrievable on day 30 with its original
//     timestamp and provenance intact.
//  2. A correction on day 15 supersedes: day-30 retrieval returns only the
//     new value, and the old value is not reachable as current.
//  3. Forgetting is complete: the forgotten fact returns nothing as current
//     and leaves a tombstone, so the same value can never be re-learned from
//     old evidence.
//  4. Retrieval is scoped: facts filed under one context never surface in
//     another.
//
// These are the falsifiable form. Removing the timestamp, provenance, status
// or scope layer makes them fail.

func weeksEntry(id, predicate, value string, at time.Time, scopes []string) Entry {
	return Entry{
		ID: id, Kind: KindFact, Subject: "user", Predicate: predicate,
		Status: StatusCurrent, Value: json.RawMessage(`"` + value + `"`),
		Confidence: 0.9, CreatedAt: at, UpdatedAt: at, Scopes: scopes,
		Sources: []Source{{Type: SourceConversation, Kind: SourceUserDeclared, Ref: "s1:m1", Timestamp: at}},
	}
}

func TestAcceptance_MemorySurvivesThirtyDaysWithTimestampAndProvenance(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	day1 := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	day30 := day1.AddDate(0, 0, 29)

	if _, err := s.Create(weeksEntry("ec_week1", "fact/dentist", "Dr. Somchai", day1, nil)); err != nil {
		t.Fatal(err)
	}

	got := s.CurrentAt(day30)
	if len(got) != 1 {
		t.Fatalf("day-30 retrieval returned %d entries, want 1", len(got))
	}
	e := got[0]
	if eValue(t, e) != "Dr. Somchai" {
		t.Fatalf("value = %q, want the fact filed on day 1", eValue(t, e))
	}
	// Timestamp intact: it says when it was told, not when it was read.
	if !e.CreatedAt.Equal(day1) {
		t.Fatalf("created_at = %v, want %v (the day it was told)", e.CreatedAt, day1)
	}
	// Provenance intact: the message it came from.
	if len(e.Sources) != 1 || e.Sources[0].Ref != "s1:m1" {
		t.Fatalf("provenance = %+v, want the originating message ref", e.Sources)
	}
	if !e.Sources[0].Timestamp.Equal(day1) {
		t.Fatalf("source timestamp = %v, want %v", e.Sources[0].Timestamp, day1)
	}
}

func TestAcceptance_CorrectionSupersedesOldValue(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	day1 := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	day15 := day1.AddDate(0, 0, 14)
	day30 := day1.AddDate(0, 0, 29)

	if _, err := s.Create(weeksEntry("ec_old", "fact/dentist", "Dr. Somchai", day1, nil)); err != nil {
		t.Fatal(err)
	}
	old, next, err := s.Correct("ec_old", "Dr. Lee", "actually it's Dr. Lee now", "s1:m9")
	if err != nil {
		t.Fatalf("correct: %v", err)
	}
	// Correct returns the old entry as it was; the stored row is what must be
	// superseded.
	stored, ok := s.Get(old.ID)
	if !ok || stored.Status != StatusSuperseded {
		t.Fatalf("stored old status = %v (ok=%v), want superseded", stored.Status, ok)
	}
	_ = day15

	got := s.CurrentAt(day30)
	if len(got) != 1 || eValue(t, got[0]) != "Dr. Lee" {
		t.Fatalf("day-30 retrieval = %+v, want only the corrected value", got)
	}
	// The old value must not be reachable as current by any path.
	for _, e := range s.CurrentAt(day30) {
		if eValue(t, e) == "Dr. Somchai" {
			t.Fatal("superseded value is still returned as current")
		}
	}
	if next.Status != StatusCurrent {
		t.Fatalf("corrected entry status = %s, want current", next.Status)
	}
}

func TestAcceptance_ForgettingIsComplete(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	day1 := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	day30 := day1.AddDate(0, 0, 29)

	if _, err := s.Create(weeksEntry("ec_forget", "fact/dentist", "Dr. Somchai", day1, nil)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ForgetWithReason("ec_forget", "the owner asked"); err != nil {
		t.Fatalf("forget: %v", err)
	}

	for _, e := range s.CurrentAt(day30) {
		if e.ID == "ec_forget" {
			t.Fatal("a forgotten fact is still returned as current")
		}
	}
	// A tombstone must exist so the same value cannot be re-learned from
	// evidence older than the forgetting.
	tombs := s.Tombstones()
	if len(tombs) == 0 {
		t.Fatal("forgetting left no tombstone; old evidence could resurrect it")
	}
	if _, ok := s.IsTombstoned(tombs[0].Subject, tombs[0].Predicate, json.RawMessage(`"Dr. Somchai"`)); !ok {
		t.Fatal("the forgotten value is not recognized as tombstoned; old evidence could resurrect it")
	}
}

func TestAcceptance_RetrievalIsScoped(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	day1 := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)

	if _, err := s.Create(weeksEntry("ec_work", "fact/salary", "220000", day1, []string{"context:work"})); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(weeksEntry("ec_home", "fact/allergy", "peanuts", day1, []string{"context:home"})); err != nil {
		t.Fatal(err)
	}

	work := s.CurrentInScope([]string{"context:work"})
	for _, e := range work {
		if eValue(t, e) == "peanuts" {
			t.Fatal("a home-scoped fact surfaced in the work context")
		}
	}
	home := s.CurrentInScope([]string{"context:home"})
	for _, e := range home {
		if eValue(t, e) == "220000" {
			t.Fatal("a work-scoped fact surfaced in the home context")
		}
	}
}

func eValue(t *testing.T, e Entry) string {
	t.Helper()
	var s string
	if err := json.Unmarshal(e.Value, &s); err != nil {
		return string(e.Value)
	}
	return s
}
