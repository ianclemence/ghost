package personalcontext

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
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

// Ghost's service runs as root. An atomic rewrite through a new file must not
// hand the owner's memory file to root. Skipped unless run as root.
func TestRewriteKeepsTheFileOwner(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("ownership only changes when run as root")
	}
	ws := t.TempDir()
	s := mustOpen(t, ws)
	e, _ := s.Create(Entry{ID: newEntryID(), Kind: KindFact, Subject: "user", Predicate: "fact/dentist", Status: StatusCurrent,
		Value: json.RawMessage(`"Their dentist is Dr. Lee at Samitivej"`), Confidence: 0.9, Sources: eraseSrc()})
	const owner = 1234
	if err := os.Chown(s.Path(), owner, owner); err != nil {
		t.Fatal(err)
	}
	if _, err := ForgetPipelineWith(s, ws, e.ID, ""); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != owner || int(st.Gid) != owner {
		t.Fatalf("the rewrite changed the file's owner: %+v", info.Sys())
	}
}

func TestForeignCurrentFindsWhatOtherSessionsFiled(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	mk := func(pred, val, ref string, scopes []string) {
		t.Helper()
		if _, err := s.Create(Entry{ID: newEntryID(), Kind: KindFact, Subject: "user", Predicate: pred, Status: StatusCurrent, Scopes: scopes,
			Value: json.RawMessage(`"` + val + `"`), Confidence: 0.9,
			Sources: []Source{{Type: SourceConversation, Kind: SourceInferred, Ref: ref, Timestamp: time.Now()}}}); err != nil {
			t.Fatal(err)
		}
	}
	mk("fact/city", "Lives in Bangkok", "main:req-1", nil)
	mk("fact/dog", "Has a golden retriever", "memtest:req-2", nil)
	mk("fact/job", "Is a pentester", "research-live-100e", nil)
	mk("fact/club", "Supports a club", "work-session:req-3", []string{"context:work"})

	got := s.ForeignCurrent(func(session string) bool { return session == "main" })
	if len(got) != 2 {
		t.Fatalf("want the two probe-filed facts, got %d: %+v", len(got), got)
	}
	for _, e := range got {
		if SessionOf(e) == "main" || len(e.Scopes) > 0 {
			t.Fatalf("an owner or scoped entry was listed: %+v", e)
		}
	}
}

func TestHousekeepingForgetIsNotRecordedAsTheOwnerAsking(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	e, _ := s.Create(Entry{ID: newEntryID(), Kind: KindFact, Subject: "user", Predicate: "fact/dog", Status: StatusCurrent,
		Value: json.RawMessage(`"Has a golden retriever named Biscuit"`), Confidence: 0.9, Sources: eraseSrc()})
	if _, err := ForgetPipelineWith(s, ws, e.ID, HousekeepingReasonPrefix+"filed by a probe"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(e.ID)
	if strings.Contains(strings.ToLower(got.RetractReason), "owner asked") || !strings.HasPrefix(got.RetractReason, HousekeepingReasonPrefix) {
		t.Fatalf("housekeeping was recorded as the owner's wish: %q", got.RetractReason)
	}
	if strings.Contains(rawFile(t, s.Path()), "Biscuit") {
		t.Fatal("the content must still be erased")
	}
	if ForgottenMatcher(ws) != nil {
		t.Fatal("housekeeping must not keep a subject out of the owner's notes as though they had asked")
	}
}
