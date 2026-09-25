package agent

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/credentials"
)

// A look-up must never open an approval card: listing schedules is a
// read of existing state. Regression — the standing grant's action
// "schedule" never matched the list request's "schedule:list", so every
// harmless "check my reminders" nagged the owner for permission.
func TestScheduleListLookupNeedsNoApproval(t *testing.T) {
	h := newGateHarness(t)

	decision, handled := h.loop.authorizeStandaloneTool("req-list", "sess-list", "schedule", map[string]interface{}{"action": "list"})
	if !handled {
		t.Fatal("schedule must be handled by the gate")
	}
	if !decision.Allowed {
		t.Fatalf("list lookup must be allowed without approval: %+v", decision)
	}
	if decision.PendingID != "" {
		t.Fatalf("a look-up must not create a pending request: %s", decision.PendingID)
	}
	if _, pending := h.broker.PendingForSession("sess-list"); pending {
		t.Fatal("a look-up must not leave a pending approval in the broker")
	}
}

// Creation still goes through the broker: only the read side is free.
func TestScheduleCreateStillGated(t *testing.T) {
	h := newGateHarness(t)

	decision, handled := h.loop.authorizeStandaloneTool("req-create", "sess-create", "schedule", map[string]interface{}{"message": "remind me tomorrow at 9"})
	if !handled {
		t.Fatal("schedule must be handled by the gate")
	}
	if decision.Allowed {
		t.Fatal("schedule creation must not be allowed without a grant")
	}
}

// Device state reads are lookups too — the health check's device:status
// used to open an approval whose only possible outcome was a dead end.
func TestDeviceStatusLookupNeedsNoApproval(t *testing.T) {
	h := newGateHarness(t)

	decision, handled := h.loop.authorizeStandaloneTool("req-dev", "sess-dev", "device", map[string]interface{}{"action": "status"})
	if !handled {
		t.Fatal("device must be handled by the gate")
	}
	if !decision.Allowed {
		t.Fatalf("status lookup must be allowed without approval: %+v", decision)
	}
	if _, pending := h.broker.PendingForSession("sess-dev"); pending {
		t.Fatal("a status look-up must not leave a pending approval")
	}
}

// Same rule on the committed-capability path: a health-check device
// status read is read-only no matter which capability the model
// committed to.
func TestAuthorizeToolStatusReadIsReadOnly(t *testing.T) {
	h := newGateHarness(t)

	res := h.loop.governance.AuthorizeTool("req-hc", "sess-hc", "healthcheck.default", "device", map[string]interface{}{"action": "status"})
	if !res.Allowed {
		t.Fatalf("status read must be allowed: %+v", res)
	}
}

// An unconnected integration refuses BEFORE an approval card exists: the
// owner's yes could not make it run, so the honest connect message must
// come first — never an approval whose only outcome is "isn't
// connected yet". Regression from the live health-check session.
func TestDeviceActuationRefusesWhenNotConnected(t *testing.T) {
	if credentials.HassConfigured() {
		t.Skip("Home Assistant is configured on this machine")
	}
	h := newGateHarness(t)

	decision, handled := h.loop.authorizeStandaloneTool("req-on", "sess-on", "device", map[string]interface{}{"action": "turn_on", "entity_id": "light.kitchen"})
	if !handled {
		t.Fatal("device must be handled by the gate")
	}
	if decision.Allowed {
		t.Fatalf("actuation must not be allowed: %+v", decision)
	}
	if decision.PendingID != "" {
		t.Fatalf("no approval may be opened for an unconnected integration: %s", decision.PendingID)
	}
	if !strings.Contains(strings.ToLower(decision.AskMessage), "connected") {
		t.Fatalf("want the connect message, got %q", decision.AskMessage)
	}
	if _, pending := h.broker.PendingForSession("sess-on"); pending {
		t.Fatal("no pending approval row may exist for a doomed call")
	}
}
