package channels

import (
	"context"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
)

// One conversation, many surfaces: an allowlisted sender's message enters the
// shared conversation regardless of channel, while the channel, chat and
// sender ride along as provenance so the reply still routes correctly.
func TestChannelMessageUsesSharedConversation(t *testing.T) {
	b := bus.NewMessageBus()
	ch := NewBaseChannel("telegram", nil, b, nil)

	ch.HandleMessage("123456|alice", "chat-42", "hi from telegram", nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	msg, ok := b.ConsumeInbound(ctx)
	if !ok {
		t.Fatal("expected an inbound message")
	}
	if msg.SessionKey != SharedConversationKey {
		t.Errorf("channel message must join the shared conversation %q, got %q", SharedConversationKey, msg.SessionKey)
	}
	// Provenance survives so the reply goes back to the right chat.
	if msg.Channel != "telegram" || msg.ChatID != "chat-42" || msg.SenderID != "123456|alice" {
		t.Errorf("channel/chat/sender must be preserved, got %+v", msg)
	}
	if msg.Metadata["source_channel"] != "telegram" {
		t.Errorf("source_channel provenance must be recorded, got %q", msg.Metadata["source_channel"])
	}
}

// A disallowed sender never reaches the conversation at all.
func TestChannelMessageDeniedSenderNeverPublishes(t *testing.T) {
	b := bus.NewMessageBus()
	ch := NewBaseChannel("telegram", nil, b, []string{"123456"})

	ch.HandleMessage("999", "chat-42", "not allowed", nil, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, ok := b.ConsumeInbound(ctx); ok {
		t.Fatal("a denied sender must not publish an inbound message")
	}
}

func TestBaseChannelIsAllowed(t *testing.T) {
	tests := []struct {
		name      string
		allowList []string
		senderID  string
		want      bool
	}{
		{
			name:      "empty allowlist allows all",
			allowList: nil,
			senderID:  "anyone",
			want:      true,
		},
		{
			name:      "compound sender matches numeric allowlist",
			allowList: []string{"123456"},
			senderID:  "123456|alice",
			want:      true,
		},
		{
			name:      "compound sender matches username allowlist",
			allowList: []string{"@alice"},
			senderID:  "123456|alice",
			want:      true,
		},
		{
			name:      "numeric sender matches legacy compound allowlist",
			allowList: []string{"123456|alice"},
			senderID:  "123456",
			want:      true,
		},
		{
			name:      "non matching sender is denied",
			allowList: []string{"123456"},
			senderID:  "654321|bob",
			want:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := NewBaseChannel("test", nil, nil, tt.allowList)
			if got := ch.IsAllowed(tt.senderID); got != tt.want {
				t.Fatalf("IsAllowed(%q) = %v, want %v", tt.senderID, got, tt.want)
			}
		})
	}
}
