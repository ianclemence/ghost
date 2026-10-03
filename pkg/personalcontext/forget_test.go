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
