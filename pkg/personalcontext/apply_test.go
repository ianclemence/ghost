package personalcontext

import (
	"fmt"
	"sync"
	"testing"
)

// applyText runs Apply over a message and returns the actions, failing on
// error.
func applyText(t *testing.T, s *Store, text string) []Action {
	t.Helper()
	actions, err := Apply(s, Input{
		SessionID: "s1",
		MessageID: "m1",
		Text:      text,
		Timestamp: fixedTime,
	})
	if err != nil {
		t.Fatalf("Apply(%q): %v", text, err)
	}
	return actions
}

// Two grammar rules resolve to the same predicate in one message (goal/primary
// via "my goal is to X" and "I want to build X"). The later declaration must
// supersede the earlier one, never producing two current entries for one
// belief: persistence re-resolves against the live state, so the second action
// becomes a supersede instead of a second create.
func TestApplySameMessageSamePredicateSupersedes(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)

	actions := applyText(t, s, "my goal is to launch X and I want to build Y")
	if len(actions) != 2 {
		t.Fatalf("actions = %d, want 2", len(actions))
	}
	if actions[0].Mode != ActionCreate {
		t.Errorf("action 0 mode = %q, want create", actions[0].Mode)
	}
	if actions[1].Mode != ActionSupersede {
		t.Fatalf("action 1 mode = %q, want supersede (later declaration wins)", actions[1].Mode)
	}

	cur := s.Current()
	if len(cur) != 1 {
		t.Fatalf("current entries = %d, want exactly 1 for goal/primary", len(cur))
	}
	if entryString(cur[0]) != "build Y" {
		t.Errorf("current value = %q, want the later declaration build Y", entryString(cur[0]))
	}
	if cur[0].SupersededBy != nil {
		t.Errorf("current entry must not carry superseded_by: %+v", cur[0])
	}

	var superseded *Entry
	for i := range s.All() {
		e := s.All()[i]
		if e.Status == StatusSuperseded {
			superseded = &e
		}
	}
	if superseded == nil {
		t.Fatal("no superseded entry; the earlier declaration should be retired")
	}
	if entryString(*superseded) != "launch X" {
		t.Errorf("superseded value = %q, want launch X", entryString(*superseded))
	}
	if superseded.SupersededBy == nil || *superseded.SupersededBy != cur[0].ID {
		t.Errorf("superseded_by = %v, want %s", superseded.SupersededBy, cur[0].ID)
	}

	// The whole chain is preserved: one create + one supersede-revision.
	if hist := s.History(superseded.ID); len(hist) != 2 {
		t.Errorf("earlier entry history = %d records, want 2", len(hist))
	}
}

// Concurrent declarations of the same predicate (two sessions sharing one
// store) never create two current entries: persistence re-resolves under the
// store lock, so the second writer sees the first's entry and treats the
// duplicate as a restatement.
func TestApplyConcurrentSamePredicateNoDuplicate(t *testing.T) {
	for iter := 0; iter < 25; iter++ {
		t.Run(fmt.Sprintf("iter-%d", iter), func(t *testing.T) {
			ws := t.TempDir()
			s := mustOpen(t, ws)
			var wg sync.WaitGroup
			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func(msg string) {
					defer wg.Done()
					_, _ = Apply(s, Input{
						SessionID: "s1",
						MessageID: msg,
						Text:      "my favorite color is blue",
						Timestamp: fixedTime,
					})
				}(fmt.Sprintf("m%d", i))
			}
			wg.Wait()

			cur := s.Current()
			if len(cur) != 1 {
				t.Fatalf("current entries = %d, want exactly 1 (no duplicate belief)", len(cur))
			}
			if entryString(cur[0]) != "blue" {
				t.Errorf("current value = %q, want blue", entryString(cur[0]))
			}
		})
	}
}

// Additive likes keep working through the re-resolved persistence path: a
// second like is a new current entry, and a restated like is skipped.
func TestApplyLikesStillAdditive(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)

	applyText(t, s, "I like dark chocolate")
	applyText(t, s, "I like milk chocolate")

	likes := s.ByPredicate("preference/likes")
	if len(likes) != 2 {
		t.Fatalf("likes = %d, want 2", len(likes))
	}
	for _, e := range likes {
		if e.Status != StatusCurrent {
			t.Errorf("like %q status = %q, want current (additive)", entryString(e), e.Status)
		}
	}

	if actions := applyText(t, s, "I like dark chocolate"); len(actions) != 0 {
		t.Fatalf("restated like produced %d actions, want 0", len(actions))
	}
}

// A correction whose current entry disappeared between extraction and
// persistence falls back to a create through the re-resolved path.
func TestApplyCorrectionFallsBackToCreateWhenNoCurrent(t *testing.T) {
	ws := t.TempDir()
	s := mustOpen(t, ws)

	actions := applyText(t, s, "actually, my favorite color is green")
	if len(actions) != 1 {
		t.Fatalf("actions = %d, want 1", len(actions))
	}
	if actions[0].Mode != ActionCreate {
		t.Fatalf("mode = %q, want create (no entry to correct)", actions[0].Mode)
	}
	if actions[0].Entry.Sources[0].Kind != SourceUserDeclared {
		t.Errorf("source kind = %q, want user_declared fallback", actions[0].Entry.Sources[0].Kind)
	}
}
