package bus

import (
	"testing"
	"time"
)

func TestPublishOutbound_DoesNotBlockOnSlowBestEffortSubscriber(t *testing.T) {
	mb := NewMessageBus()
	reliableCh, cleanupReliable := mb.SubscribeOutbound("reliable-test", true, 8)
	defer cleanupReliable()
	_, cleanupSlow := mb.SubscribeOutbound("slow-test", false, 1)
	defer cleanupSlow()

	received := make(chan OutboundMessage, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 2; i++ {
			msg, ok := <-reliableCh
			if !ok {
				return
			}
			received <- msg
		}
	}()

	start := time.Now()
	mb.PublishOutbound(OutboundMessage{Channel: "telegram", ChatID: "1", Content: "a"})
	mb.PublishOutbound(OutboundMessage{Channel: "telegram", ChatID: "1", Content: "b"})
	if time.Since(start) > 200*time.Millisecond {
		t.Fatalf("publish blocked too long with slow best-effort subscriber")
	}

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("reliable subscriber did not receive messages")
	}

	if len(received) != 2 {
		t.Fatalf("expected 2 messages for reliable subscriber, got %d", len(received))
	}
}
