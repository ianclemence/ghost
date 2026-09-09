package agent

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/permissions"
)

// The strongest invariant: a standalone consequential tool without an
// authorized broker decision never returns Allowed — exec, device I/O,
// scheduling, updates, image generation, and messaging are all governed.
func TestStandaloneConsequentialAskOrDeny(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	for _, tool := range []string{"exec", "sandbox", "i2c", "spi", "hass", "schedule", "cron", "update", "image_generate", "message"} {
		decision, handled := al.authorizeStandaloneTool("req-"+tool, "sess-gov", tool, map[string]interface{}{})
		if !handled {
			t.Fatalf("%s must be handled by the gate", tool)
		}
		if decision.Allowed {
			t.Fatalf("%s must not be Allowed without a grant", tool)
		}
		// Leave a durable pending request (ask) or explicit denial.
		if decision.PendingID == "" && decision.AskMessage == "" {
			t.Fatalf("%s decision must carry a pending id or denial", tool)
		}
	}
}

// A read-only / internal tool is not on the consequential table and is
// therefore not gated as a standalone (it has no side-effect class).
func TestStandaloneNonConsequentialNotGated(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	for _, tool := range []string{"read_file", "context_get", "remember", "memory_recall"} {
		if _, handled := al.authorizeStandaloneTool("req-x", "sess-x", tool, map[string]interface{}{}); handled {
			t.Fatalf("%s must not be a standalone-consequential tool", tool)
		}
	}
}

// Explicit deny on exec stops the call; the decision message reflects it
// and no executor is reachable (gate is before dispatch).
func TestStandaloneExecDenyNeverRuns(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	// Ask first so a pending request exists, then deny it.
	first, handled := al.authorizeStandaloneTool("req-exec-1", "sess-exec", "exec", map[string]interface{}{"command": "rm -rf ~"})
	if !handled || first.Allowed {
		t.Fatalf("exec must ask: %+v", first)
	}
	if _, ok := h.broker.PendingForRequest("req-exec-1"); !ok {
		t.Fatal("ask must leave a durable pending request")
	}
	// Deny at the broker.
	if err := denyPending(h, "req-exec-1", "sess-exec"); err != nil {
		t.Fatal(err)
	}
	second, handled := al.authorizeStandaloneTool("req-exec-2", "sess-exec", "exec", map[string]interface{}{"command": "rm -rf ~"})
	if !handled || second.Allowed {
		t.Fatalf("denied exec must not allow: %+v", second)
	}
}

func denyPending(h *gateHarness, requestID, session string) error {
	if p, ok := h.broker.PendingForRequest(requestID); ok {
		_, err := h.broker.Resolve(p.ID, permissions.GrantDeny, "session:"+session)
		return err
	}
	if p, ok := h.broker.PendingForSession(session); ok {
		_, err := h.broker.Resolve(p.ID, permissions.GrantDeny, "session:"+session)
		return err
	}
	return nil
}

// A standing grant for exec (owner-visible, scoped) makes the same call
// Allowed; the narrow capability identity is exec.shell.
func TestStandaloneExecAllowedWithGrant(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	if err := h.broker.GrantStanding("exec.shell", "exec", "session:sess-x", false); err != nil {
		t.Fatal(err)
	}
	decision, handled := al.authorizeStandaloneTool("req-e1", "sess-x", "exec", map[string]interface{}{})
	if !handled || !decision.Allowed {
		t.Fatalf("granted exec must allow: %+v", decision)
	}
}

// Unknown/consequential-looking names that are NOT declared fail closed
// too: they are simply not dispatchable by this gate, and any executor
// surface must validate against the registry (covered elsewhere). A forged
// operation name is never silently mapped onto a real capability.
func TestStandaloneUnknownNameNotMapped(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	for _, tool := range []string{"exec --flag", "shell", "run_command", "browser_transact", "device_write"} {
		if _, handled := al.authorizeStandaloneTool("req-"+tool, "sess-y", tool, map[string]interface{}{}); handled {
			t.Fatalf("forged/unknown tool %q must not be auto-governed into a capability", tool)
		}
	}
}
