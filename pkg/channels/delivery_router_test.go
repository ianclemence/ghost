package channels

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/bus"
)

func TestDeliveryRouterResolveTarget(t *testing.T) {
	r := NewDeliveryRouter()
	msg := bus.OutboundMessage{
		Channel: "telegram",
		Metadata: map[string]interface{}{
			"delivery_mode": "home",
			"home_channel":  "email",
		},
	}
	if got := r.ResolveTarget(msg); got != "email" {
		t.Fatalf("expected email target, got %s", got)
	}
	msg.Metadata["delivery_mode"] = "explicit"
	msg.Metadata["explicit_channel"] = "slack"
	if got := r.ResolveTarget(msg); got != "slack" {
		t.Fatalf("expected slack target, got %s", got)
	}
}
