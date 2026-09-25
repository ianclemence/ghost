package agent

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/tools"
)

// The broker refuses empty request ids, so a heartbeat that touches a
// governed capability can only open a durable approval if the turn has
// its own request identity. Regression: heartbeat maintenance approvals
// failed for days with "I couldn't prepare the approval request" because
// ProcessHeartbeat passed no RequestID.
func TestHeartbeatOptionsCarryRequestID(t *testing.T) {
	opts := heartbeatOptions("run maintenance", "cli", "direct")
	if opts.RequestID == "" {
		t.Fatal("heartbeat turn has no request id — the broker will refuse the approval request")
	}
	if !strings.HasPrefix(opts.RequestID, "req-hb-") {
		t.Fatalf("request id = %q, want req-hb- prefix", opts.RequestID)
	}
}

// The heartbeat turn shape itself must not drift: history-free, no chat
// echo, heartbeat-safe tools only.
func TestHeartbeatOptionsShape(t *testing.T) {
	opts := heartbeatOptions("ping", "cli", "direct")
	if opts.SessionKey != "heartbeat" {
		t.Fatalf("session = %q, want heartbeat", opts.SessionKey)
	}
	if !opts.NoHistory {
		t.Fatal("heartbeat must stay history-free")
	}
	if opts.SendResponse {
		t.Fatal("heartbeat must not echo a response to chat")
	}
	if opts.ToolProfile != tools.ProfileHeartbeatSafe {
		t.Fatalf("tool profile = %v, want heartbeat-safe", opts.ToolProfile)
	}
	if opts.UserMessage != "ping" {
		t.Fatalf("user message = %q", opts.UserMessage)
	}
}
