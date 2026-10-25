package personalcontext

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func eraseSrc() []Source {
	return []Source{{Type: SourceConversation, Kind: SourceUserDeclared, Ref: "main", Timestamp: time.Now()}}
}

func rawFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Forgetting appended a "rejected" line beside the original line, which still
// held the value and the verbatim quote. The owner's forgotten dentist was
// still readable in the file.
func TestForgettingErasesTheContentFromTheLog(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	e, err := s.Create(Entry{ID: newEntryID(), Kind: KindFact, Subject: "user", Predicate: "fact/dentist", Status: StatusCurrent,
		Value: json.RawMessage(`"Their dentist is Dr. Lee at Samitivej"`), Quote: "my dentist is Dr. Lee at Samitivej",
		Confidence: 0.9, Sources: eraseSrc()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ForgetPipelineWith(s, ws, e.ID, ""); err != nil {
		t.Fatal(err)
	}

	for _, f := range []string{s.Path(), filepath.Join(filepath.Dir(s.Path()), TombstonesFile)} {
		if got := rawFile(t, f); strings.Contains(got, "Samitivej") || strings.Contains(got, "dentist is Dr") {
			t.Fatalf("%s still holds what was forgotten:\n%s", filepath.Base(f), got)
		}
	}
	got, ok := s.Get(e.ID)
	if !ok || got.Status != StatusRejected || got.RetractedAt == nil || string(got.Value) != string(erasedValue) || got.Quote != "" {
		t.Fatalf("the record must remain as a receipt without its content: %+v", got)
	}
	if reopened := mustOpen(t, ws); len(reopened.Current()) != 0 {
		t.Fatal("a forgotten belief came back")
	}
	if _, ok := s.IsTombstoned("user", "fact/dentist", json.RawMessage(`"Their dentist is Dr. Lee at Samitivej"`)); !ok {
		t.Fatal("the tombstone must still stop the same belief being re-derived")
	}
}

// Keeping the fact out of the notes must not need the fact: the hashed words
// do the matching.
func TestForgottenFactStaysOutOfNotesWithoutBeingKept(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	e, _ := s.Create(Entry{ID: newEntryID(), Kind: KindFact, Subject: "user", Predicate: "fact/dentist", Status: StatusCurrent,
		Value: json.RawMessage(`"Their dentist is Dr. Lee at Samitivej"`), Confidence: 0.9, Sources: eraseSrc()})
	if _, err := ForgetPipelineWith(s, ws, e.ID, ""); err != nil {
		t.Fatal(err)
	}
	tomb := rawFile(t, filepath.Join(ws, "personal-context", TombstonesFile))
	if strings.Contains(strings.ToLower(tomb), "samitivej") {
		t.Fatalf("the tombstone holds the word: %s", tomb)
	}
	if got := DropForgotten(ws, "- the user's dentist is at Samitivej\n- booked flights"); strings.Contains(got, "Samitivej") || !strings.Contains(got, "booked flights") {
		t.Fatalf("a note repeating the forgotten fact survived: %q", got)
	}
	if got := DropForgotten(ws, "- lunch near Samitivej tomorrow"); !strings.Contains(got, "lunch") {
		t.Fatalf("one name alone, without the topic, must not erase an unrelated note: %q", got)
	}
}

// Beliefs forgotten before forgetting erased them are cleaned when the store
// opens, and still kept out of the notes.
func TestEarlierForgettingIsErasedOnOpen(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, EntriesDir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	legacy := []Entry{
		{ID: "ec_old", Kind: KindFact, Subject: "user", Predicate: "fact/dentist", Status: StatusCurrent,
			Value: json.RawMessage(`"Their dentist is Dr. Somchai at Bumrungrad"`), Quote: "my dentist is Dr. Somchai at Bumrungrad",
			Confidence: 0.9, Sources: eraseSrc(), CreatedAt: now, UpdatedAt: now},
		{ID: "ec_old", Kind: KindFact, Subject: "user", Predicate: "fact/dentist", Status: StatusRejected, RetractReason: OwnerForgetReason, RetractedAt: &now,
			Value: json.RawMessage(`"Their dentist is Dr. Somchai at Bumrungrad"`), Quote: "my dentist is Dr. Somchai at Bumrungrad",
			Confidence: 0.9, Sources: eraseSrc(), CreatedAt: now, UpdatedAt: now},
		{ID: "ec_kept", Kind: KindFact, Subject: "user", Predicate: "fact/city", Status: StatusCurrent,
			Value: json.RawMessage(`"Lives in Bangkok"`), Confidence: 0.9, Sources: eraseSrc(), CreatedAt: now, UpdatedAt: now},
	}
	var b strings.Builder
	for _, e := range legacy {
		line, _ := json.Marshal(e)
		b.Write(line)
		b.WriteByte('\n')
	}
	path := filepath.Join(dir, "entries.jsonl")
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	s := mustOpen(t, ws)
	if got := rawFile(t, path); strings.Contains(got, "Bumrungrad") {
		t.Fatalf("an earlier forgetting still holds its content:\n%s", got)
	}
	if got := rawFile(t, path); !strings.Contains(got, "Lives in Bangkok") {
		t.Fatal("a belief that was not forgotten must be untouched")
	}
	if cur := s.Current(); len(cur) != 1 || cur[0].ID != "ec_kept" {
		t.Fatalf("current beliefs changed: %+v", cur)
	}
	if got := DropForgotten(ws, "- the dentist is Dr. Somchai at Bumrungrad\n- other"); strings.Contains(got, "Bumrungrad") {
		t.Fatalf("the notes guard was lost in the migration: %q", got)
	}
}

// Retiring an entry without a retraction (a correction, an extractor rejecting
// its own mistake) keeps its record readable, as documented.
func TestRetiringWithoutARetractionKeepsTheRecord(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	e, _ := s.Create(Entry{ID: newEntryID(), Kind: KindFact, Subject: "user", Predicate: "fact/city", Status: StatusCurrent,
		Value: json.RawMessage(`"Lives in Bangkok"`), Confidence: 0.9, Sources: eraseSrc()})
	if err := s.Forget(e.ID); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rawFile(t, s.Path()), "Lives in Bangkok") {
		t.Fatal("a plain retire must keep the record inspectable")
	}
}
