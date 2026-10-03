package agent

import (
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/session"
	"github.com/ianclemence/ghost/pkg/turnlog"
)

// The model is handed its own cut-off reply and must come back with one
// whole answer — not a sentence that continues mid-air, and not an account
// of the restart the owner never saw happen.
func TestResumePromptAsksForOneWholeReply(t *testing.T) {
	prompt := threadResumePrompt("summarise my week", "You shipped three things and ")

	for _, want := range []string{
		"You shipped three things and ",
		"summarise my week",
		"one complete answer",
		"reads as one whole message",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q\n---\n%s", want, prompt)
		}
	}
	// The owner must never learn that anything was cut, or that a restart
	// happened: they should just read a finished reply.
	if !strings.Contains(prompt, "never mention the restart") {
		t.Errorf("prompt must keep the machinery out of the reply:\n%s", prompt)
	}
}

// Nothing had been written when the process died: the turn still owes an
// answer, and the prompt must not pretend there was a draft.
func TestResumePromptWhenNothingWasWritten(t *testing.T) {
	prompt := threadResumePrompt("what's the weather", "   ")
	if !strings.Contains(prompt, "stopped before anything was written") {
		t.Errorf("expected the empty-draft framing:\n%s", prompt)
	}
	if strings.Contains(prompt, "--- reply so far ---") {
		t.Errorf("an empty draft must not be presented as one:\n%s", prompt)
	}
}

// A missing ask (an origin that was never recorded) must still leave a
// usable prompt rather than an empty instruction.
func TestResumePromptToleratesAMissingAsk(t *testing.T) {
	prompt := threadResumePrompt("", "half")
	if !strings.Contains(prompt, "conversation above") {
		t.Errorf("expected a fallback ask:\n%s", prompt)
	}
}

// The transcript holds a reply that stops mid-sentence. The model needs to
// be told, or "continue" lands on a message that merely looks finished and
// the thread gets rebuilt from scratch instead of picked up.
func TestHistoryMarksAReplyThatStopped(t *testing.T) {
	history := []providers.Message{
		{Role: "user", Content: "summarise my week", CreatedAt: time.Now()},
		{Role: "assistant", Content: "You shipped three things and", Interrupted: true, CreatedAt: time.Now()},
	}
	stamped := stampHistory(history)
	got := stamped[1].Content
	if !strings.Contains(got, "stopped mid-sentence") {
		t.Fatalf("cut-off reply was not marked for the model: %q", got)
	}
	if !strings.HasPrefix(got, "[") {
		t.Fatalf("the wall-clock stamp must survive: %q", got)
	}
}

// An ordinary reply is left exactly as it was: marking every message would
// teach the model to hedge on everything it says.
func TestHistoryLeavesOrdinaryRepliesAlone(t *testing.T) {
	history := []providers.Message{
		{Role: "assistant", Content: "You shipped three things.", CreatedAt: time.Now()},
	}
	if got := stampHistory(history)[0].Content; strings.Contains(got, "mid-sentence") {
		t.Fatalf("ordinary reply was marked as cut off: %q", got)
	}
}

// A reply a restart cut off is written back into the transcript, marked, and
// can never be written twice.
func TestMaterializeInterruptedReplyIsOnceOnly(t *testing.T) {
	al := &AgentLoop{sessions: session.NewSessionManager(session.NewJSONLStore(t.TempDir()), nil)}
	turn := &turnlog.Turn{
		SessionID:     "main",
		RequestID:     "req-1",
		Partial:       "You shipped three things and ",
		Status:        turnlog.StatusInterrupted,
		InterruptedAt: time.Now(),
	}

	if !al.MaterializeInterruptedReply(turn) {
		t.Fatal("the cut-off reply must reach the transcript")
	}
	history := al.Sessions().GetDisplayHistory("main")
	if len(history) != 1 {
		t.Fatalf("expected the recovered reply alone, got %d rows", len(history))
	}
	if history[0].Role != "assistant" || history[0].Content != "You shipped three things and " {
		t.Fatalf("recovered reply stored wrong: %+v", history[0])
	}
	if !history[0].Interrupted {
		t.Fatal("the reply must carry its cut-short marker")
	}

	// The caller marks it materialized once it is on disk. A turn already
	// written must be refused, or a second recovery appends it again.
	turn.Materialized = true
	if al.MaterializeInterruptedReply(turn) {
		t.Fatal("a reply already in the transcript was written a second time")
	}
	if n := len(al.Sessions().GetDisplayHistory("main")); n != 1 {
		t.Fatalf("history grew to %d rows", n)
	}
}

// Nothing written means nothing to preserve: the guard must not insert an
// empty message into the conversation.
func TestMaterializeRefusesAnEmptyPartial(t *testing.T) {
	al := &AgentLoop{sessions: session.NewSessionManager(session.NewJSONLStore(t.TempDir()), nil)}
	if al.MaterializeInterruptedReply(&turnlog.Turn{SessionID: "main", RequestID: "r", Partial: "  "}) {
		t.Fatal("an empty draft must not become a message")
	}
	if n := len(al.Sessions().GetDisplayHistory("main")); n != 0 {
		t.Fatalf("expected an empty transcript, got %d rows", n)
	}
}
