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

// The retrieved fact must carry WHEN it was said, not just what. The date
// is the entry's own (CreatedAt), rendered into the prompt by the digest.
func TestAcceptance_MemoryIsAttributedToWhenItWasSaid(t *testing.T) {
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

	runTurn(t, al, "my favorite color is teal", "s1")
	now = day1.AddDate(0, 0, 29)
	runTurn(t, al, "what is my favorite color?", "s2")

	prompt := provider.lastSystemPrompt
	if !strings.Contains(prompt, "teal") {
		t.Fatalf("the fact is missing:\n%s", firstLines(prompt, 40))
	}
	if !strings.Contains(prompt, "2026-01-01") {
		t.Fatalf("the retrieved fact carries no date it was said:\n%s", firstLines(prompt, 40))
	}
}

// A correction renders the NEW date, not the original: the corrected entry is
// a new entry and carries the correction's timestamp.
func TestAcceptance_CorrectionRendersNewDate(t *testing.T) {
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
	if !strings.Contains(prompt, "green") {
		t.Fatalf("corrected value missing:\n%s", firstLines(prompt, 40))
	}
	if !strings.Contains(prompt, "2026-01-15") {
		t.Fatalf("corrected fact does not carry the correction date:\n%s", firstLines(prompt, 40))
	}
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
