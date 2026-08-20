package personalcontext

import (
	"strings"
	"testing"
	"time"
)

// dEntry builds a current entry for digest tests. Sources use fixedTime so
// stores are byte-comparable.
func dEntry(id string, kind Kind, predicate, value string) Entry {
	raw, _ := RawValue(value)
	return Entry{
		ID:         id,
		Kind:       kind,
		Subject:    "user",
		Predicate:  predicate,
		Value:      raw,
		Status:     StatusCurrent,
		Confidence: 1,
		Sources:    []Source{declaredSource()},
		CreatedAt:  fixedTime,
		UpdatedAt:  fixedTime,
	}
}

func digestLines(t *testing.T, digest string) []string {
	t.Helper()
	if !strings.HasPrefix(digest, digestOpen) || !strings.HasSuffix(digest, digestClose) {
		t.Fatalf("digest not properly delimited:\n%s", digest)
	}
	body := strings.TrimSuffix(strings.TrimPrefix(digest, digestOpen), digestClose)
	return strings.Split(strings.TrimRight(body, "\n"), "\n")
}

// Empty Personal Context produces no digest.
func TestBuildDigestEmpty(t *testing.T) {
	if d := BuildDigest(nil, 0); d != "" {
		t.Fatalf("nil current produced digest %q", d)
	}
	if d := BuildDigest([]Entry{}, DigestBudget); d != "" {
		t.Fatalf("empty current produced digest %q", d)
	}
}

// A current identity entry appears in the digest.
func TestBuildDigestIncludesIdentity(t *testing.T) {
	d := BuildDigest([]Entry{dEntry("1", KindIdentity, "identity/name", "Ian")}, 0)
	if !strings.Contains(d, "- Name: Ian") {
		t.Fatalf("digest missing identity:\n%s", d)
	}
}

// Current high-priority preferences appear.
func TestBuildDigestIncludesPreferences(t *testing.T) {
	d := BuildDigest([]Entry{
		dEntry("1", KindPreference, "preference/favorite_color", "blue"),
	}, 0)
	if !strings.Contains(d, "- Favorite color: blue") {
		t.Fatalf("digest missing preference:\n%s", d)
	}
}

// Communication preferences appear with their label.
func TestBuildDigestIncludesCommunicationStyle(t *testing.T) {
	d := BuildDigest([]Entry{
		dEntry("1", KindPreference, "preference/communication.style", "concise, direct"),
	}, 0)
	if !strings.Contains(d, "- Communication style: concise, direct") {
		t.Fatalf("digest missing communication style:\n%s", d)
	}
}

// Active goals appear.
func TestBuildDigestIncludesGoals(t *testing.T) {
	d := BuildDigest([]Entry{
		dEntry("1", KindGoal, "goal/primary", "build something people use"),
	}, 0)
	if !strings.Contains(d, "- Goal: build something people use") {
		t.Fatalf("digest missing goal:\n%s", d)
	}
}

// Priority order is deterministic and value-based: identity, then preference,
// then goal, then fact, regardless of input order.
func TestBuildDigestPriorityOrder(t *testing.T) {
	entries := []Entry{
		dEntry("f1", KindFact, "fact/location", "Bangkok"),
		dEntry("g1", KindGoal, "goal/primary", "build x"),
		dEntry("n1", KindIdentity, "identity/name", "Ian"),
		dEntry("p1", KindPreference, "preference/favorite_color", "blue"),
	}
	d := BuildDigest(entries, 0)
	lines := digestLines(t, d)
	wantOrder := []string{"Name: Ian", "Favorite color: blue", "Goal: build x", "Location: Bangkok"}
	var got []string
	for _, want := range wantOrder {
		found := -1
		for i, line := range lines {
			if strings.Contains(line, want) {
				found = i
				break
			}
		}
		if found < 0 {
			t.Fatalf("digest missing %q in:\n%s", want, d)
		}
		got = append(got, lines[found])
	}
	for i := 1; i < len(got); i++ {
		if strings.Index(d, got[i]) < strings.Index(d, got[i-1]) {
			t.Fatalf("entries out of priority order:\n%s", d)
		}
	}
}

// Lower-priority entries are excluded when the size cap is reached;
// the highest-priority entries always win.
func TestBuildDigestExcludesLowerPriorityAtCap(t *testing.T) {
	entries := []Entry{
		dEntry("n1", KindIdentity, "identity/name", "Ian"),
		dEntry("l1", KindPreference, "preference/likes", "coffee"),
		dEntry("l2", KindPreference, "preference/likes", "tea"),
		dEntry("l3", KindPreference, "preference/likes", "books"),
	}
	// A budget that fits the identity but not all the likes.
	d := BuildDigest(entries, 90)
	if !strings.Contains(d, "Name: Ian") {
		t.Fatalf("top-priority entry excluded:\n%s", d)
	}
	if len(digestLines(t, d)) > 3 {
		t.Fatalf("digest included more than the budget allows:\n%s", d)
	}
}

// The digest never exceeds the hard size limit, no matter how many entries.
func TestBuildDigestNeverExceedsHardLimit(t *testing.T) {
	var entries []Entry
	for i := 0; i < 300; i++ {
		entries = append(entries, dEntry(
			"id"+string(rune('a'+i%26))+string(rune('a'+(i/26)%26)),
			KindPreference, "preference/likes", "item "+string(rune('a'+i%26))))
	}
	d := BuildDigest(entries, DigestBudget)
	if d == "" {
		t.Fatal("digest unexpectedly empty")
	}
	if len(d) > DigestBudget {
		t.Fatalf("digest length %d exceeds hard cap %d", len(d), DigestBudget)
	}
}

// A single enormous value is truncated, never allowed to blow the cap.
func TestBuildDigestTruncatesHugeValue(t *testing.T) {
	huge := strings.Repeat("x", 10000)
	d := BuildDigest([]Entry{dEntry("1", KindIdentity, "identity/name", huge)}, DigestBudget)
	if d == "" {
		t.Fatal("digest empty even though a value existed")
	}
	if len(d) > DigestBudget {
		t.Fatalf("digest length %d exceeds hard cap %d", len(d), DigestBudget)
	}
}

// Ordering is deterministic: the same entries in any input order produce
// byte-identical output.
func TestBuildDigestDeterministicOrdering(t *testing.T) {
	entries := []Entry{
		dEntry("f1", KindFact, "fact/location", "Bangkok"),
		dEntry("n1", KindIdentity, "identity/name", "Ian"),
		dEntry("p1", KindPreference, "preference/favorite_color", "blue"),
	}
	shuffled := []Entry{entries[2], entries[0], entries[1]}
	a := BuildDigest(entries, 0)
	b := BuildDigest(shuffled, 0)
	if a != b {
		t.Fatalf("digest depends on input order:\n%q\nvs\n%q", a, b)
	}
	if BuildDigest(entries, 0) != BuildDigest(entries, 0) {
		t.Fatal("digest not stable across calls")
	}
}

// Unknown predicates fall back to a readable label derived from the predicate.
func TestBuildDigestLabelFallback(t *testing.T) {
	d := BuildDigest([]Entry{
		dEntry("1", KindFact, "fact/works_in_city", "Oslo"),
	}, 0)
	if !strings.Contains(d, "- works in city: Oslo") {
		t.Fatalf("digest label fallback wrong:\n%s", d)
	}
}

// A stored string value appears unquoted, not as raw JSON.
func TestBuildDigestValueUnquoted(t *testing.T) {
	d := BuildDigest([]Entry{dEntry("1", KindFact, "fact/location", "Bangkok")}, 0)
	if strings.Contains(d, `"Bangkok"`) {
		t.Fatalf("digest contains quoted value:\n%s", d)
	}
	if !strings.Contains(d, "- Location: Bangkok") {
		t.Fatalf("digest missing location:\n%s", d)
	}
}

// Superseded entries are excluded because they are not current.
func TestBuildDigestSupersededExcluded(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	s.Create(dEntry("1", KindPreference, "preference/favorite_color", "blue"))
	s.Supersede("user", "preference/favorite_color", dEntry("2", KindPreference, "preference/favorite_color", "green"))

	d := BuildDigest(s.Current(), 0)
	if strings.Contains(d, "blue") {
		t.Fatalf("superseded value in digest:\n%s", d)
	}
	if !strings.Contains(d, "- Favorite color: green") {
		t.Fatalf("digest missing current value:\n%s", d)
	}
}

// Rejected/forgotten entries are excluded.
func TestBuildDigestRejectedExcluded(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	s.Create(dEntry("1", KindFact, "fact/location", "Bangkok"))
	s.Forget("1")

	d := BuildDigest(s.Current(), 0)
	if d != "" {
		t.Fatalf("forgotten entry leaked into digest:\n%s", d)
	}
}

// Expired entries are excluded.
func TestBuildDigestExpiredExcluded(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	past := time.Now().UTC().Add(-24 * time.Hour)
	e := dEntry("1", KindFact, "fact/location", "Tokyo")
	e.ValidUntil = &past
	s.Create(e)

	d := BuildDigest(s.Current(), 0)
	if d != "" {
		t.Fatalf("expired entry leaked into digest:\n%s", d)
	}
}

// Future-valid entries are excluded.
func TestBuildDigestFutureValidExcluded(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	future := time.Now().UTC().Add(24 * time.Hour)
	e := dEntry("1", KindFact, "fact/location", "London")
	e.ValidFrom = &future
	s.Create(e)

	d := BuildDigest(s.Current(), 0)
	if d != "" {
		t.Fatalf("future-valid entry leaked into digest:\n%s", d)
	}
}

// Conflicting entries are excluded from the normal digest output.
func TestBuildDigestConflictingExcluded(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	s.Create(dEntry("1", KindPreference, "preference/foo", "A"))
	s.Create(dEntry("2", KindPreference, "preference/foo", "B"))
	s.DeclareConflict("user", "preference/foo", "1", "2")

	d := BuildDigest(s.Current(), 0)
	if d != "" {
		t.Fatalf("conflicting entries leaked into digest:\n%s", d)
	}
}

// Uncertain entries are excluded.
func TestBuildDigestUncertainExcluded(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	e := dEntry("1", KindFact, "fact/location", "Bangkok")
	e.Status = StatusUncertain
	s.Create(e)

	d := BuildDigest(s.Current(), 0)
	if d != "" {
		t.Fatalf("uncertain entry leaked into digest:\n%s", d)
	}
}

// Adding many entries never causes unbounded output.
func TestBuildDigestManyEntriesBounded(t *testing.T) {
	var entries []Entry
	for i := 0; i < 200; i++ {
		entries = append(entries, dEntry(
			"id"+string(rune('a'+i%26)),
			KindFact, "fact/extra", "value"))
	}
	d := BuildDigest(entries, DigestBudget)
	if len(d) > DigestBudget {
		t.Fatalf("digest length %d exceeds cap %d", len(d), DigestBudget)
	}
}

// Two equivalent stores produce byte-identical digest output.
func TestBuildDigestTwoStoresByteIdentical(t *testing.T) {
	build := func() string {
		s := mustOpen(t, t.TempDir())
		s.Create(dEntry("1", KindIdentity, "identity/name", "Ian"))
		s.Create(dEntry("2", KindPreference, "preference/favorite_color", "blue"))
		s.Create(dEntry("3", KindGoal, "goal/primary", "build x"))
		return BuildDigest(s.Current(), 0)
	}
	a, b := build(), build()
	if a != b {
		t.Fatalf("two equivalent stores produced different digests:\n%q\nvs\n%q", a, b)
	}
}

// Updating a Personal Context entry makes the digest reflect the new value.
func TestBuildDigestReflectsUpdate(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	s.Create(dEntry("1", KindPreference, "preference/favorite_color", "blue"))
	if d := BuildDigest(s.Current(), 0); !strings.Contains(d, "- Favorite color: blue") {
		t.Fatalf("digest missing blue:\n%s", d)
	}
	s.Supersede("user", "preference/favorite_color", dEntry("2", KindPreference, "preference/favorite_color", "green"))
	d := BuildDigest(s.Current(), 0)
	if strings.Contains(d, "blue") {
		t.Fatalf("stale value in digest after update:\n%s", d)
	}
	if !strings.Contains(d, "- Favorite color: green") {
		t.Fatalf("digest missing new value:\n%s", d)
	}
}
