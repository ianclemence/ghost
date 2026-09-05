package interaction

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/bus"
)

func TestEnvelope(t *testing.T) {
	msg := bus.InboundMessage{Channel: "telegram", SenderID: "u123", ChatID: "c1", Content: "hi", SessionKey: "s"}
	r, err := FromInbound(msg, "req-1", "conv-1", "", "owner-1", "ghost-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if r.Channel != "telegram" || r.ChannelIdentity != "u123" || r.ContextID != "personal" || r.AgentID != "agent-main" {
		t.Fatalf("bad envelope: %+v", r)
	}
	if r.ChannelOf() != "telegram" {
		t.Fatal("channel identity wrong")
	}
}

func TestEnvelopeFailsClosed(t *testing.T) {
	msg := bus.InboundMessage{Channel: "", Content: "hi"}
	if _, err := FromInbound(msg, "r", "c", "", "o", "g", "a"); err == nil {
		t.Fatal("empty channel must fail")
	}
	msg = bus.InboundMessage{Channel: "web", Content: ""}
	if _, err := FromInbound(msg, "r", "c", "", "o", "g", "a"); err == nil {
		t.Fatal("empty content must fail")
	}
	msg = bus.InboundMessage{Channel: "web", Content: "hi"}
	if _, err := FromInbound(msg, "", "c", "", "o", "g", "a"); err == nil {
		t.Fatal("missing request id must fail")
	}
}
