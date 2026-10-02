package agent

import (
	"context"
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
)

func TestRecentTurnsKeepsOnlyTheConversation(t *testing.T) {
	h := []providers.Message{
		{Role: "system", Content: "you are ghost"},
		{Role: "user", Content: "hi"},
		{Role: "tool", Content: "{...}"},
		{Role: "assistant", Content: ""},
		{Role: "assistant", Content: "Hello"},
		{Role: "user", Content: "weather?"},
		{Role: "assistant", Content: "Sunny."},
	}
	got := recentTurns(h, 3)
	if len(got) != 3 || got[0].Text != "Hello" || got[2].Text != "Sunny." {
		t.Fatalf("want the last three real messages, got %+v", got)
	}
}

func TestSuggestNextNeedsGhostToBeWaiting(t *testing.T) {
	var nilLoop *AgentLoop
	if s := nilLoop.SuggestNext(context.Background(), "main"); s.Text != "" {
		t.Fatalf("a nil loop suggests nothing, got %+v", s)
	}
	t.Setenv("GHOST_SUGGEST", "off")
	al := &AgentLoop{}
	if s := al.SuggestNext(context.Background(), "main"); s.Text != "" {
		t.Fatalf("off means off, got %+v", s)
	}
}

func TestSuggestNextRulesThenModelThenCache(t *testing.T) {
	t.Setenv("GHOST_SUGGEST", "")
	al := newTestAgentLoop(t, t.TempDir())
	const sess = "main"

	// Ghost made an offer: a rule answers it, without the model.
	al.sessions.AddMessage(sess, "user", "any reminders?")
	al.sessions.AddMessage(sess, "assistant", `"Pay rent" is the next one up. Want me to move it, or add a nudge before it?`)
	s := al.SuggestNext(context.Background(), sess)
	if s.Text != "Yes, move it" || s.Source != "rule" || s.For == "" {
		t.Fatalf("want the rule's answer to the offer, got %+v", s)
	}

	// Same state, same answer (and the key says it is the same state).
	if again := al.SuggestNext(context.Background(), sess); again != s {
		t.Fatalf("an unchanged conversation must give the same suggestion: %+v vs %+v", again, s)
	}

	// The owner replied and Ghost answered with something no rule covers: the
	// model is asked, and its short line is used.
	al.sessions.AddMessage(sess, "user", "yes")
	al.sessions.AddMessage(sess, "assistant", "Done, I moved it to 7 PM.")
	m := al.SuggestNext(context.Background(), sess)
	if m.Source != "model" || m.Text == "" || m.For == s.For {
		t.Fatalf("want a model suggestion for the new state, got %+v", m)
	}

	// With the model switched off, the same state yields only rules.
	t.Setenv("GHOST_SUGGEST", "rules")
	al.sessions.AddMessage(sess, "user", "thanks")
	al.sessions.AddMessage(sess, "assistant", "Anytime.")
	if r := al.SuggestNext(context.Background(), sess); r.Text != "" {
		t.Fatalf("rules mode must not call the model, got %+v", r)
	}
}

func TestSuggestNextStaysQuietWhenTheOwnerHasTheFloor(t *testing.T) {
	al := newTestAgentLoop(t, t.TempDir())
	al.sessions.AddMessage("main", "user", "remind me tomorrow")
	if s := al.SuggestNext(context.Background(), "main"); s.Text != "" {
		t.Fatalf("when the last message is the owner's, there is nothing to suggest, got %+v", s)
	}
	if s := al.SuggestNext(context.Background(), "routine:abc"); s.Text != "" {
		t.Fatalf("a timer's conversation never gets suggestions, got %+v", s)
	}
}
