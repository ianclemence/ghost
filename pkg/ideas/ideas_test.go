package ideas

import (
	"strings"
	"testing"
	"time"
)

func TestGenerateRoutineFailure(t *testing.T) {
	now := time.Now().UTC()
	runs := []RoutineRun{
		{RunID: "run-2", RoutineID: "r1", RoutineName: "Water", Status: "error", Error: "timeout", At: now.Add(-time.Hour)},
		{RunID: "run-1", RoutineID: "r1", RoutineName: "Water", Status: "error", Error: "timeout", At: now.Add(-2 * time.Hour)},
		{RunID: "run-0", RoutineID: "r1", RoutineName: "Water", Status: "ok", At: now.Add(-25 * time.Hour)},
	}
	got := Generate(Signals{Runs: runs, Now: now}, nil)
	if len(got) != 1 {
		t.Fatalf("failing routine must yield one idea, got %v", got)
	}
	if got[0].Action != "pause:r1" {
		t.Fatalf("failure idea must carry a safe pause action, got %+v", got[0])
	}
	if len(got[0].Sources) != 1 || got[0].Sources[0].Ref != "run-2" {
		t.Fatalf("idea must cite the latest failing run, got %+v", got[0].Sources)
	}
	// A second generation with the idea pending must not duplicate it.
	again := Generate(Signals{Runs: runs, Now: now}, got)
	if len(again) != 0 {
		t.Fatalf("cited sources must dedupe, got %v", again)
	}
}

func TestGenerateHealthyRoutineSilent(t *testing.T) {
	now := time.Now().UTC()
	runs := []RoutineRun{{RunID: "run-1", RoutineID: "r1", Status: "ok", At: now.Add(-time.Hour)}}
	if got := Generate(Signals{Runs: runs, Now: now}, nil); len(got) != 0 {
		t.Fatalf("healthy routines must stay silent, got %v", got)
	}
}

func TestGenerateGoalCheckins(t *testing.T) {
	now := time.Now().UTC()
	mem := []MemoryFact{{ID: "m1", Predicate: "goal/primary", Value: "launch Ghost", UpdatedAt: now}}
	got := Generate(Signals{Memories: mem, Now: now}, nil)
	if len(got) != 1 || got[0].Action != "" {
		t.Fatalf("goal idea must propose without automating, got %+v", got)
	}
}

func TestGenerateStaleRoutine(t *testing.T) {
	now := time.Now().UTC()
	old := now.Add(-40 * 24 * time.Hour)
	next := now.Add(24 * time.Hour)
	routines := []RoutineMeta{{ID: "r9", Name: "Old", Status: "active", LastRun: &old, NextRun: &next}}
	got := Generate(Signals{Routines: routines, Now: now}, nil)
	if len(got) != 1 || got[0].Action != "pause:r9" {
		t.Fatalf("stale routine must yield a pause idea, got %+v", got)
	}
}

func TestStoreRoundtripDecide(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Add([]Idea{{Title: "T", Body: "B", Sources: []Source{{Kind: SourceMemory, Ref: "m1"}}}}); err != nil {
		t.Fatal(err)
	}
	list, err := st.List(StatusPending, 10)
	if err != nil || len(list) != 1 {
		t.Fatalf("pending list wrong: %v %v", list, err)
	}
	decided, err := st.Decide(list[0].ID, true)
	if err != nil || decided.Status != StatusAccepted || decided.DecidedAt == nil {
		t.Fatalf("accept must receipt: %+v %v", decided, err)
	}
	rest, _ := st.List(StatusPending, 10)
	if len(rest) != 0 {
		t.Fatalf("accepted idea must leave pending: %v", rest)
	}
	if _, err := st.Decide("missing", true); err == nil {
		t.Fatal("deciding a missing idea must fail")
	}
}

func TestParseDrafts(t *testing.T) {
	if got := ParseDrafts("NONE"); len(got) != 0 {
		t.Fatalf("NONE must yield nothing, got %v", got)
	}
	got := ParseDrafts("Title: Water reminder\nWhy: You drink tea [memory:m1] daily.\nExtra line here.")
	if len(got) != 1 || got[0].Title != "Water reminder" {
		t.Fatalf("draft parse wrong: %+v", got)
	}
	if !strings.Contains(got[0].Body, "[memory:m1]") {
		t.Fatalf("body must keep markers for verification: %q", got[0].Body)
	}
}

func TestVerifyDraftResolvesAndStrips(t *testing.T) {
	ev := Evidence{
		Memories: map[string]MemoryFact{"m1": {ID: "m1", Predicate: "preference/prefers", Value: "green tea"}},
		Runs:     map[string]RoutineRun{"r9": {RunID: "r9", Status: "error"}},
	}
	idea := VerifyDraft(Draft{Title: "Tea [memory:m1]", Body: "You prefer tea [memory:m1] and the run failed [routine:r9]."}, ev, time.Now().UTC())
	if idea.Unverified {
		t.Fatalf("resolvable citations must verify: %+v", idea)
	}
	if len(idea.Sources) != 2 {
		t.Fatalf("both citations must become sources: %+v", idea.Sources)
	}
	if strings.Contains(idea.Body, "[memory:m1]") {
		t.Fatalf("markers must strip from user prose: %q", idea.Body)
	}
}

func TestVerifyDraftUnresolvableMarksUnverified(t *testing.T) {
	idea := VerifyDraft(Draft{Title: "X", Body: "You prefer tea [memory:ghost]."}, Evidence{}, time.Now().UTC())
	if !idea.Unverified {
		t.Fatalf("phantom citations must mark unverified: %+v", idea)
	}
	if len(idea.Sources) != 0 {
		t.Fatalf("phantom citations must yield no sources: %+v", idea.Sources)
	}
}

func TestVerifyDraftUncitedFactualMarksUnverified(t *testing.T) {
	idea := VerifyDraft(Draft{Title: "X", Body: "You prefer green tea daily."}, Evidence{}, time.Now().UTC())
	if !idea.Unverified {
		t.Fatalf("uncited factual claims must mark unverified: %+v", idea)
	}
}
