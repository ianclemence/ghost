package main

import (
	"testing"
	"time"
)

// The same content, resubmitted with a new request id inside the window, is a
// resend. The same request id is a reconnect and is handled by the durable
// turn log, not here.
func TestDuplicateChatRequestFindsAResend(t *testing.T) {
	key := chatContentFingerprint("main", "mobile", "default", "draw a fox in a snowy forest at dusk")
	noteChatRequest(key, "req-1")

	if prior, ok := duplicateChatRequest(key, "req-2"); !ok || prior != "req-1" {
		t.Fatalf("expected req-1 to be recognised as the original, got %q %v", prior, ok)
	}
	if _, ok := duplicateChatRequest(key, "req-1"); ok {
		t.Fatal("a reconnect with the same request id is not a duplicate")
	}

	other := chatContentFingerprint("main", "mobile", "default", "draw a boat")
	if _, ok := duplicateChatRequest(other, "req-3"); ok {
		t.Fatal("different text must not be treated as a duplicate")
	}

	recentChatMu.Lock()
	recentChat[key] = recentChatTurn{requestID: "req-1", at: time.Now().Add(-duplicateWindow - time.Second)}
	recentChatMu.Unlock()
	if _, ok := duplicateChatRequest(key, "req-4"); ok {
		t.Fatal("a resend after the window is a new request")
	}
}
