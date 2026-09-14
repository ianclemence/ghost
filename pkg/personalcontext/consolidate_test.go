package personalcontext

import (
	"testing"
	"time"
)

func testConsolidateStore(t *testing.T) *Store {
	t.Helper()
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func mustCreate(t *testing.T, st *Store, subject, predicate, value string) Entry {
	t.Helper()
	v, err := RawValue(value)
	if err != nil {
		t.Fatal(err)
	}
	e, err := st.Create(Entry{
		ID: newEntryID(), Kind: KindFact, Subject: subject, Predicate: predicate,
		Value: v, Status: StatusCurrent, Confidence: 0.8,
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestDetectConflictsDeclares(t *testing.T) {
	st := testConsolidateStore(t)
	a := mustCreate(t, st, "sahara", "lives_in", "Cairo")
	b := mustCreate(t, st, "Sahara", "Lives_In", "Alexandria")
	_ = a
	_ = b
	n, err := st.DetectConflicts()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 conflict, got %d", n)
	}
	// Second pass is a no-op (both now conflicting, not current).
	if n, _ := st.DetectConflicts(); n != 0 {
		t.Fatalf("expected idempotent no-op, got %d", n)
	}
}

func TestDetectConflictsIgnoresAgreement(t *testing.T) {
	st := testConsolidateStore(t)
	mustCreate(t, st, "sahara", "likes", "Tea")
	mustCreate(t, st, "Sahara", "Likes", "tea ")
	if n, _ := st.DetectConflicts(); n != 0 {
		t.Fatalf("agreement declared conflict: %d", n)
	}
}

func TestExpireDueRetires(t *testing.T) {
	st := testConsolidateStore(t)
	past := time.Now().UTC().Add(-time.Hour)
	v, _ := RawValue("old news")
	e, err := st.Create(Entry{
		ID: newEntryID(), Kind: KindFact, Subject: "promo", Predicate: "code",
		Value: v, Status: StatusCurrent, Confidence: 0.5, ValidUntil: &past,
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := st.ExpireDue(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if m != 1 {
		t.Fatalf("expected 1 expiry, got %d", m)
	}
	// Expired entry no longer current, provenance retained in history.
	for _, c := range st.Current() {
		if c.ID == e.ID {
			t.Fatal("expired entry still current")
		}
	}
	if len(st.History(e.ID)) < 2 {
		t.Fatal("expiry must append a revision, not delete")
	}
}

func TestConsolidateRunsAllPasses(t *testing.T) {
	st := testConsolidateStore(t)
	mustCreate(t, st, "a", "p", "one")
	mustCreate(t, st, "A", "P", "two")
	c, err := st.Consolidate(time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if c.ConflictsDeclared != 1 {
		t.Fatalf("expected 1 conflict, got %+v", c)
	}
}
