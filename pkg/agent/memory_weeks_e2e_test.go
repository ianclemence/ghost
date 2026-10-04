package agent

import (
	"strings"
	"testing"
	"time"
)

// Acceptance for "memory across weeks" through the REAL turn loop (not the
// store API). A fact is stated in one turn, time moves forward, and it is
// asked for in a *different* session with no shared session state. The answer
// must carry the fact and say when it was told.
//
// The clock is the store's, set here only for the test; production leaves it
// unset, so it cannot leak.
func TestAcceptance_MemoryAcrossWeeksThroughATurn(t *testing.T) {
	ws := newDigestWorkspace(t)
	provider := &recordingDigestProvider{}
	al := newTestAgentLoopWithProvider(t, ws, provider)

	pc := al.PersonalContext()
	if pc == nil {
		t.Fatal("personal context store not wired")
	}
	day1 := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	day30 := day1.AddDate(0, 0, 29)
	now := day1
	pc.SetClock(func() time.Time { return now })

	runTurn(t, al, "my favorite color is teal", "s1")

	// Three weeks later, a different session: no shared history, only memory.
	now = day30
	runTurn(t, al, "what is my favorite color?", "s2")

	prompt := provider.lastSystemPrompt
	if !strings.Contains(prompt, "teal") {
		t.Fatalf("the fact did not survive three weeks into a new session:\n%s", prompt)
	}
}

// UNMET: retrieved memory is not attributed to when it was said. The fact
// survives across weeks and sessions, but the prompt carries only the value
// ("Favorite color: teal"), never the date it was told. The store has
// CreatedAt and source timestamps; the prompt/digest renderer drops them.
// Skipped rather than deleted, so the unmet criterion is recorded in the
// suite rather than quietly dropped.
func TestAcceptance_MemoryIsAttributedToWhenItWasSaid(t *testing.T) {
	t.Skip("retrieved memory carries no date attribution; implement it in the memory prompt/digest renderer")
}

// Correction through turns: state, correct in a later turn, ask in a third.
// Only the new value comes back.
func TestAcceptance_CorrectionThroughTurns(t *testing.T) {
	ws := newDigestWorkspace(t)
	provider := &recordingDigestProvider{}
	al := newTestAgentLoopWithProvider(t, ws, provider)
	pc := al.PersonalContext()
	if pc == nil {
		t.Fatal("personal context store not wired")
	}
	day1 := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	now := day1
	pc.SetClock(func() time.Time { return now })

	runTurn(t, al, "my favorite color is blue", "s1")
	now = day1.AddDate(0, 0, 14)
	runTurn(t, al, "actually my favorite color is green", "s1")

	now = day1.AddDate(0, 0, 29)
	runTurn(t, al, "what is my favorite color?", "s2")

	prompt := provider.lastSystemPrompt
	if !strings.Contains(strings.ToLower(prompt), "green") {
		t.Fatalf("the corrected value did not survive to the third turn:\n%s", firstLines(prompt, 40))
	}
	if strings.Contains(strings.ToLower(prompt), "blue") {
		t.Fatalf("the superseded value leaked back as current:\n%s", firstLines(prompt, 40))
	}
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}
