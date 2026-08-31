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

	n, err := Compact(store)
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

	n, err := Compact(store)
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
	n, err := Compact(store)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}
