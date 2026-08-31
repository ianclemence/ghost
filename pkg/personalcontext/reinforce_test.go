package personalcontext

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReinforceOnRestatement(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now().UTC()
	in := func(text, ref string) Input {
		return Input{SessionID: "s", MessageID: ref, Text: text, Timestamp: now}
	}

	// First declaration creates the belief.
	if _, err := Apply(store, in("I like coffee", "m1")); err != nil {
		t.Fatalf("apply1: %v", err)
	}
	// Restating it reinforces (never duplicates).
	if _, err := Apply(store, in("I like coffee", "m2")); err != nil {
		t.Fatalf("apply2: %v", err)
	}

	cur := store.Current()
	if len(cur) != 1 {
		t.Fatalf("expected 1 current entry, got %d", len(cur))
	}
	if cur[0].ReinforceCount != 1 {
		t.Fatalf("expected ReinforceCount=1, got %d", cur[0].ReinforceCount)
	}
	if cur[0].ReinforcedAt == nil {
		t.Fatal("expected ReinforcedAt set")
	}
	// Provenance (original sources) preserved.
	if len(cur[0].Sources) == 0 {
		t.Fatal("expected sources preserved")
	}
}

func TestReinforceDoesNotMergeDistinctLikes(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now().UTC()
	in := func(text, ref string) Input {
		return Input{SessionID: "s", MessageID: ref, Text: text, Timestamp: now}
	}
	if _, err := Apply(store, in("I like coffee", "m1")); err != nil {
		t.Fatalf("apply1: %v", err)
	}
	if _, err := Apply(store, in("I like tea", "m2")); err != nil {
		t.Fatalf("apply2: %v", err)
	}
	if len(store.Current()) != 2 {
		t.Fatalf("expected 2 distinct likes, got %d", len(store.Current()))
	}
}

func TestReinforcePreservesConflictFreeSupersede(t *testing.T) {
	store, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	now := time.Now().UTC()
	in := func(text, ref string) Input {
		return Input{SessionID: "s", MessageID: ref, Text: text, Timestamp: now}
	}
	// A non-additive belief (communication style) supersedes on change.
	if _, err := Apply(store, in("I prefer concise answers", "m1")); err != nil {
		t.Fatalf("apply1: %v", err)
	}
	if _, err := Apply(store, in("I prefer detailed answers", "m2")); err != nil {
		t.Fatalf("apply2: %v", err)
	}
	cur := store.Current()
	if len(cur) != 1 {
		t.Fatalf("expected 1 current entry, got %d", len(cur))
	}
	if v := Value(cur[0]); v != "detailed" {
		t.Fatalf("expected superseded to 'detailed', got %q", v)
	}
	// The superseded value remains inspectable in the full store (provenance).
	all := store.All()
	if len(all) != 2 {
		t.Fatalf("expected 2 records total (superseded + current), got %d", len(all))
	}
}

func TestExtractPartnerWorkJob(t *testing.T) {
	store, _ := Open(t.TempDir())
	now := time.Now().UTC()
	in := func(text, ref string) Input { return Input{SessionID: "s", MessageID: ref, Text: text, Timestamp: now} }

	if _, err := Apply(store, in("my partner's name is Alex", "m1")); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(store, in("I work as an engineer", "m2")); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(store, in("my job is a teacher", "m3")); err != nil {
		t.Fatal(err)
	}

	cur := store.Current()
	found := map[string]string{}
	for _, e := range cur {
		found[e.Predicate] = Value(e)
	}
	if found["relationship/partner"] != "Alex" {
		t.Fatalf("partner = %q, want Alex", found["relationship/partner"])
	}
	if found["fact/work"] != "an engineer" {
		t.Fatalf("work = %q", found["fact/work"])
	}
	if found["fact/job"] != "a teacher" {
		t.Fatalf("job = %q", found["fact/job"])
	}
}

func TestMaterializeCuratedProfile(t *testing.T) {
	dir := t.TempDir()
	store, _ := Open(dir)
	now := time.Now().UTC()
	in := func(text, ref string) Input { return Input{SessionID: "s", MessageID: ref, Text: text, Timestamp: now} }
	if _, err := Apply(store, in("my name is Sam", "m1")); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(store, in("I live in Bangkok", "m2")); err != nil {
		t.Fatal(err)
	}

	n, err := MaterializeCuratedProfile(dir, store)
	if err != nil {
		t.Fatal(err)
	}
	if n < 2 {
		t.Fatalf("expected >=2 facts materialized, got %d", n)
	}
	b, err := os.ReadFile(filepath.Join(dir, "knowledge", "self", "user-profile.md"))
	if err != nil {
		t.Fatalf("profile file: %v", err)
	}
	s := string(b)
	if !strings.Contains(s, "Name: Sam") || !strings.Contains(s, "Location: Bangkok") {
		t.Fatalf("profile unexpected: %s", s)
	}
}
