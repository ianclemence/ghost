package personalcontext

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// "Forget my dentist" after a correction: both versions are retracted and
// Ghost's own notes stop carrying either, while unrelated lines survive.
func TestForgetPipelineForgetsEverywhere(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	src := []Source{{Type: SourceConversation, Kind: SourceUserDeclared, Ref: "m1", Timestamp: time.Now()}}
	old, err := s.Create(Entry{ID: newEntryID(), Kind: KindFact, Subject: "user", Predicate: "fact/dentist", Status: StatusCurrent,
		Value: json.RawMessage(`"Their dentist is Dr. Somchai at Bumrungrad Hospital"`), Confidence: 0.9, Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	cur, err := s.Supersede("user", "fact/dentist", Entry{ID: newEntryID(), Kind: KindFact, Subject: "user", Predicate: "fact/dentist", Status: StatusCurrent,
		Value: json.RawMessage(`"Their dentist is Dr. Lee at Samitivej"`), Confidence: 0.9, Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(ws, "memory", "2026-10-03.md")
	_ = os.MkdirAll(filepath.Dir(notes), 0755)
	_ = os.WriteFile(notes, []byte("# Day\n- the user corrected their dentist from Dr. Somchai at Bumrungrad Hospital to Dr. Lee at Samitivej\n- flights from Bangkok to Shenzhen\n"), 0644)

	var hooked []string
	OnForget = func(values, names []string) { hooked = append(hooked, names...) }
	defer func() { OnForget = nil }()

	if _, err := ForgetPipelineWith(s, ws, cur.ID, "the owner asked"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{old.ID, cur.ID} {
		if e, _ := s.Get(id); e.Status != StatusRejected {
			t.Errorf("%s is %s, want rejected", id, e.Status)
		}
	}
	data, _ := os.ReadFile(notes)
	if strings.Contains(string(data), "Samitivej") || strings.Contains(string(data), "Somchai") {
		t.Errorf("notes still carry the forgotten dentist:\n%s", data)
	}
	if !strings.Contains(string(data), "Bangkok to Shenzhen") {
		t.Errorf("an unrelated line was removed:\n%s", data)
	}
	if len(hooked) == 0 {
		t.Error("the search index was not told")
	}
}

// A short value is still a value. The scrubber used to match only values of
// six runes or more, so forgetting a colour or a city left it sitting in the
// journal — exactly the kind of thing the owner asked to remove.
func TestForgetScrubsShortValues(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	src := []Source{{Type: SourceConversation, Kind: SourceUserDeclared, Ref: "m1", Timestamp: time.Now()}}
	cur, err := s.Create(Entry{ID: newEntryID(), Kind: KindPreference, Subject: "user", Predicate: "preference/favorite_color", Status: StatusCurrent,
		Value: json.RawMessage(`"green"`), Confidence: 0.9, Sources: src})
	if err != nil {
		t.Fatal(err)
	}
	notes := filepath.Join(ws, "memory", "2026-01-01.md")
	_ = os.MkdirAll(filepath.Dir(notes), 0755)
	_ = os.WriteFile(notes, []byte("# Day\n- Their favorite color is green.\n- The greenery outside is nice.\n"), 0644)

	if _, err := ForgetPipelineWith(s, ws, cur.ID, OwnerForgetReason); err != nil {
		t.Fatal(err)
	}

	data, _ := os.ReadFile(notes)
	if strings.Contains(strings.ToLower(string(data)), "color is green") {
		t.Errorf("a short forgotten value survived in the notes:\n%s", data)
	}
	if !strings.Contains(string(data), "greenery") {
		t.Errorf("word-boundary matching removed a different word:\n%s", data)
	}
}

// The runtime's audit hook is told after the rebuilds, with the claim's
// label and reason — and never with the value, because a receipt for
// forgetting something must not be another copy of it.
func TestForgetReceiptIsRedacted(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	src := []Source{{Type: SourceConversation, Kind: SourceUserDeclared, Ref: "m1", Timestamp: time.Now()}}
	cur, err := s.Create(Entry{ID: newEntryID(), Kind: KindFact, Subject: "user", Predicate: "fact/dentist", Status: StatusCurrent,
		Value: json.RawMessage(`"Their dentist is Dr. Lee at Samitivej"`), Confidence: 0.9, Sources: src})
	if err != nil {
		t.Fatal(err)
	}

	var got ForgetReport
	OnForgetReceipt = func(r ForgetReport) { got = r }
	defer func() { OnForgetReceipt = nil }()

	if _, err := ForgetPipelineWith(s, ws, cur.ID, "the owner asked"); err != nil {
		t.Fatal(err)
	}

	if got.ClaimID != cur.ID {
		t.Errorf("claim id = %q, want %q", got.ClaimID, cur.ID)
	}
	if got.Predicate != "fact/dentist" {
		t.Errorf("predicate = %q, want the belief's label", got.Predicate)
	}
	if got.Reason != "the owner asked" {
		t.Errorf("reason = %q, want the recorded reason", got.Reason)
	}
	if !got.Forgotten || !got.Tombstoned {
		t.Errorf("report = %+v, want forgotten and tombstoned", got)
	}
	if got.At == 0 {
		t.Error("report carries no timestamp")
	}
	// The report has no value field by design; this guards the intent so a
	// future field cannot quietly start carrying the forgotten content.
	if strings.Contains(got.Predicate+got.Subject+got.Reason, "Samitivej") {
		t.Fatal("the receipt leaked the forgotten value")
	}
}

// Two sentences under one catch-all key are two facts: neither overwrites
// the other and they are never declared a conflict.
func TestCatchAllKeysHoldManyFacts(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	src := []Source{{Type: SourceConversation, Kind: SourceInferred, Ref: "m", Timestamp: time.Now()}}
	for _, v := range []string{`"Works as an ESL teacher"`, `"Works on Applied AI Engineering"`} {
		if _, err := s.Create(Entry{ID: newEntryID(), Kind: KindFact, Subject: "user", Predicate: "fact/work", Status: StatusCurrent,
			Value: json.RawMessage(v), Confidence: 0.9, Sources: src}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Consolidate(time.Now()); err != nil {
		t.Fatal(err)
	}
	if n := len(s.Current()); n != 2 {
		t.Fatalf("current = %d, want both work facts", n)
	}
	if !HoldsMany(Entry{Kind: KindFact, Predicate: "fact/work", Value: json.RawMessage(`"Works as an ESL teacher"`)}) {
		t.Error("a sentence under fact/work holds many")
	}
	if HoldsMany(Entry{Kind: KindFact, Predicate: "fact/work", Value: json.RawMessage(`"acme"`)}) {
		t.Error("a bare value under fact/work stays single-valued")
	}
}

// The same sentence learned twice is one memory.
func TestFoldDuplicates(t *testing.T) {
	s := mustOpen(t, t.TempDir())
	src := []Source{{Type: SourceConversation, Kind: SourceInferred, Ref: "m", Timestamp: time.Now()}}
	for i := 0; i < 2; i++ {
		if _, err := s.Create(Entry{ID: newEntryID(), Kind: KindProject, Subject: "user", Predicate: "project/current", Status: StatusCurrent,
			Value: json.RawMessage(`"Is designing a Ghost Pod"`), Confidence: 0.9, Sources: src}); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := s.FoldDuplicates(); n != 1 {
		t.Fatalf("folded %d, want 1", n)
	}
	if n := len(s.Current()); n != 1 {
		t.Fatalf("current = %d, want 1", n)
	}
}

// A line written after the forget (the turn's own journal summary) is kept
// out, and telling Ghost again brings the fact back into notes.
func TestDropForgottenAndRelearn(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	src := []Source{{Type: SourceConversation, Kind: SourceUserDeclared, Ref: "m", Timestamp: time.Now()}}
	e, _ := s.Create(Entry{ID: newEntryID(), Kind: KindPerson, Subject: "user", Predicate: "person/barber", Status: StatusCurrent,
		Value: json.RawMessage(`"Somsak at Thonglor Cuts is their barber"`), Confidence: 0.9, Sources: src})
	if _, err := ForgetPipelineWith(s, ws, e.ID, ""); err != nil {
		t.Fatal(err)
	}
	line := "- [08:37] (journal) the user stated their barber is Somsak at Thonglor Cuts, then asked to forget it\n- booked flights"
	if got := DropForgotten(ws, line); strings.Contains(got, "Thonglor") || !strings.Contains(got, "booked flights") {
		t.Fatalf("DropForgotten = %q", got)
	}
	if _, err := s.Create(Entry{ID: newEntryID(), Kind: KindPerson, Subject: "user", Predicate: "person/barber", Status: StatusCurrent,
		Value: json.RawMessage(`"Somsak at Thonglor Cuts is their barber"`), Confidence: 0.9, Sources: src}); err != nil {
		t.Fatal(err)
	}
	if got := DropForgotten(ws, line); !strings.Contains(got, "Thonglor") {
		t.Fatal("a fact told again stays out of notes")
	}
}

func TestSourceKindFor(t *testing.T) {
	if got := sourceKindFor("By the way, my dentist is Dr. Somchai.", "my dentist is Dr. Somchai"); got != SourceUserDeclared {
		t.Errorf("a direct statement is %s", got)
	}
	if got := sourceKindFor("look for flights from bangkok", "look for flights from bangkok"); got != SourceInferred {
		t.Errorf("a request is %s", got)
	}
	if got := sourceKindFor("is my dentist Dr. Lee?", "my dentist Dr. Lee"); got != SourceInferred {
		t.Errorf("a question is %s", got)
	}
}

func TestForgetNameResetsUserDoc(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)
	_ = os.WriteFile(filepath.Join(ws, "USER.md"), []byte("## Identity\n\n- **Name**: Ian\n- **Location**: bangkok\n"), 0644)
	src := []Source{{Type: SourceConversation, Kind: SourceUserDeclared, Ref: "m", Timestamp: time.Now()}}
	e, _ := s.Create(Entry{ID: newEntryID(), Kind: KindIdentity, Subject: "user", Predicate: "identity/name", Status: StatusCurrent,
		Value: json.RawMessage(`"Ian"`), Confidence: 0.9, Sources: src})
	if _, err := ForgetPipelineWith(s, ws, e.ID, ""); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(ws, "USER.md"))
	if strings.Contains(string(data), "Name**: Ian") || !strings.Contains(string(data), "Location**: bangkok") {
		t.Fatalf("USER.md = %s", data)
	}
}
