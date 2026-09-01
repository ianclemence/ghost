package personalcontext

import (
	"encoding/json"
	"testing"
	"time"
)

func compactEntry(id, predicate, value string, at time.Time, ref string) Entry {
	v, _ := json.Marshal(value)
	return Entry{
		ID:         id,
		Kind:       KindPreference,
		Subject:    entrySubjectUser,
		Predicate:  predicate,
		Value:      v,
		Status:     StatusCurrent,
		Confidence: 0.95,
		Sources: []Source{{
			Type:      SourceConversation,
			Kind:      SourceUserDeclared,
			Ref:       ref,
			Timestamp: at,
		}},
		CreatedAt: at,
		UpdatedAt: at,
	}
}

func TestCompactRejectsExactDuplicates(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now().UTC()
	if _, err := store.Create(compactEntry("dup-a", "preference/communication.style", "concise", now, "s1:msg1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	// Same subject/predicate/value, later timestamp — an exact duplicate.
	if _, err := store.Create(compactEntry("dup-b", "preference/communication.style", "concise", now.Add(time.Hour), "s2:msg2")); err != nil {
		t.Fatalf("create: %v", err)
	}

	n, _, err := Compact(store)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 rejected, got %d", n)
	}
	cur := store.Current()
	if len(cur) != 1 {
		t.Fatalf("expected 1 current entry, got %d", len(cur))
	}
	if cur[0].ID != "dup-b" {
		t.Fatalf("expected the most recent entry to survive, got %s", cur[0].ID)
	}
	// Provenance preserved: the surviving entry keeps its sources.
	if len(cur[0].Sources) == 0 {
		t.Fatal("expected provenance (sources) preserved")
	}
}

func TestCompactKeepsDistinctPreferences(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now().UTC()
	if _, err := store.Create(compactEntry("coffee", "preference/prefers", "coffee", now, "s1:m1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.Create(compactEntry("tea", "preference/prefers", "tea", now.Add(time.Minute), "s1:m2")); err != nil {
		t.Fatalf("create: %v", err)
	}

	n, _, err := Compact(store)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 rejected for distinct values, got %d", n)
	}
	if len(store.Current()) != 2 {
		t.Fatalf("expected both distinct preferences to remain, got %d", len(store.Current()))
	}
}

func TestCompactOnEmptyStore(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	n, _, err := Compact(store)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

func TestReinforceCountSaturates(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now().UTC()
	if _, err := store.Create(compactEntry("e", "preference/prefers", "tea", now, "s:m1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	for i := 0; i < MaxReinforceCount+50; i++ {
		if err := store.reinforceLocked(store.currentEntry(entrySubjectUser, "preference/prefers")); err != nil {
			t.Fatalf("reinforce %d: %v", i, err)
		}
	}
	cur := store.currentEntry(entrySubjectUser, "preference/prefers")
	if cur == nil {
		t.Fatal("expected current entry")
	}
	if cur.ReinforceCount > MaxReinforceCount {
		t.Fatalf("expected ReinforceCount to saturate at %d, got %d", MaxReinforceCount, cur.ReinforceCount)
	}
	if cur.ReinforceCount != MaxReinforceCount {
		t.Fatalf("expected ReinforceCount=%d, got %d", MaxReinforceCount, cur.ReinforceCount)
	}
}

func TestDecayReinforcementAfterWindow(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	old := time.Now().UTC().Add(-ReinforceDecayWindow - time.Hour)
	entry := compactEntry("e", "preference/prefers", "tea", old, "s:m1")
	entry.ReinforceCount = 8
	entry.ReinforcedAt = &old
	if _, err := store.Create(entry); err != nil {
		t.Fatalf("create: %v", err)
	}
	decayed, err := store.DecayReinforcement()
	if err != nil {
		t.Fatalf("decay: %v", err)
	}
	if decayed != 1 {
		t.Fatalf("expected 1 decayed, got %d", decayed)
	}
	cur := store.currentEntry(entrySubjectUser, "preference/prefers")
	if cur.ReinforceCount != 4 {
		t.Fatalf("expected ReinforceCount=4 (8/2), got %d", cur.ReinforceCount)
	}
	if cur.ReinforcedAt == nil {
		t.Fatal("expected ReinforcedAt preserved while count > 0")
	}
}

func TestDecayToZeroClearsReinforcedAt(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	old := time.Now().UTC().Add(-ReinforceDecayWindow - time.Hour)
	entry := compactEntry("e", "preference/prefers", "tea", old, "s:m1")
	entry.ReinforceCount = 1
	entry.ReinforcedAt = &old
	if _, err := store.Create(entry); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := store.DecayReinforcement(); err != nil {
		t.Fatalf("decay: %v", err)
	}
	cur := store.currentEntry(entrySubjectUser, "preference/prefers")
	if cur.ReinforceCount != 0 {
		t.Fatalf("expected ReinforceCount=0, got %d", cur.ReinforceCount)
	}
	if cur.ReinforcedAt != nil {
		t.Fatal("expected ReinforcedAt cleared when count reaches 0")
	}
}
