package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/cards"
)

// A wedged browser must tell the owner it was reset. The card is emitted
// through the same publisher the registry wires, and is informational only.
func TestBrowserRecoveryPublishesCard(t *testing.T) {
	bt := NewBrowserTool(t.TempDir(), "navigate")
	bt.SetContext("mobile", "chat-1")

	var got cards.Card
	var gotChannel, gotChat string
	bt.SetPublisher(func(channel, chatID, sessionID string, c cards.Card) {
		gotChannel, gotChat, got = channel, chatID, c
	})

	bt.publishRecovery(context.Background())

	if got.Kind != cards.KindBrowserRecovery {
		t.Fatalf("card kind = %q, want browser_recovery", got.Kind)
	}
	if gotChannel != "mobile" || gotChat != "chat-1" {
		t.Fatalf("card routed to %q/%q, want mobile/chat-1", gotChannel, gotChat)
	}
	if !strings.Contains(strings.ToLower(got.Title), "stuck") {
		t.Errorf("title should say the browser got stuck, got %q", got.Title)
	}
	if len(got.Actions) != 0 {
		t.Error("a recovery notice carries no actions")
	}
	if got.TextFallback() == "" {
		t.Error("card must carry a plain-text fallback")
	}
}
