package agent

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/live"
	"github.com/ianclemence/ghost/pkg/tools"
)

// A site that asks for a human is the one browser failure Ghost cannot
// solve by trying harder. Handing off well means three things happening
// together: Ghost stops, the owner's card offers the button, and the
// owner is told to tap it. These tests pin each part.

func botCheckEvidence() map[string]interface{} {
	return map[string]interface{}{
		"type": "action", "op": "browser.snapshot", "session": "sess-hc",
		"outcome": "ok", "title": "Just a moment...",
		"text": "Verify you are human. This process is automatic. Your IP address has been flagged.",
	}
}

func TestHumanCheckHandoffTellsTheModelToStop(t *testing.T) {
	res := &tools.ToolResult{ForLLM: "page", Evidence: botCheckEvidence()}
	note, ok := humanCheckHandoff(res)
	if !ok {
		t.Fatal("a bot-check page must produce a hand-off")
	}
	// It must name the button that is actually on the card, or the owner
	// is sent to look for something that isn't there.
	for _, want := range []string{"Take over and steer", "Done", "Do not attempt"} {
		if !strings.Contains(note, want) {
			t.Fatalf("hand-off missing %q: %s", want, note)
		}
	}
	// Never blame the site for a check Ghost was never going to pass.
	if strings.Contains(strings.ToLower(note), "site is down") {
		t.Fatalf("hand-off must not call the site down: %s", note)
	}
}

func TestHumanCheckHandoffLeavesOrdinaryPagesAlone(t *testing.T) {
	for name, res := range map[string]*tools.ToolResult{
		"ordinary":    {ForLLM: "page", Evidence: map[string]interface{}{"title": "Hacker News", "text": "Top stories"}},
		"no evidence": {ForLLM: "page"},
		"nil":         nil,
		"empty text":  {ForLLM: "page", Evidence: map[string]interface{}{"title": "Sign in"}},
	} {
		if note, ok := humanCheckHandoff(res); ok {
			t.Fatalf("%s: unexpected hand-off: %s", name, note)
		}
	}
}

// handoffHarness wires a live plane onto the gate harness so a surface can
// be registered and observed exactly as the owner's app would see it.
func handoffHarness(t *testing.T) *gateHarness {
	t.Helper()
	h := newGateHarness(t)
	h.loop.livePlane = live.NewRegistry(h.ghostID)
	return h
}

func handoffCall(sessionID string) tools.BrowserCall {
	return tools.BrowserCall{
		Owner: "ian", ContextID: "personal", TaskID: "task-hc",
		SessionID: sessionID, Op: "browser_snapshot",
	}
}

// The step that found the check failed. The card must still offer the
// hand-off — the owner cannot tap a button that is only drawn on success.
func TestAFailedStepThatFoundABotCheckParksTheCard(t *testing.T) {
	h := handoffHarness(t)
	res := &tools.ToolResult{IsError: true, ForLLM: "refused", Evidence: botCheckEvidence()}
	h.loop.recordBrowserSurface(handoffCall("sess-fail-hc"), "browser_snapshot", res, "sess-hc")

	snap, ok := h.loop.livePlane.Snapshot("sess-fail-hc")
	if !ok {
		t.Fatal("surface was never registered")
	}
	if snap.State != live.StateWaiting || snap.WaitingFor != "human" {
		t.Fatalf("card must be waiting on the owner, got state=%s waiting_for=%q", snap.State, snap.WaitingFor)
	}
}

// An ordinary failure is still just a step that didn't work: Ghost tries
// another way, and the card must not claim the owner is needed.
func TestAFailedStepOnAnOrdinaryPageStaysActive(t *testing.T) {
	h := handoffHarness(t)
	res := &tools.ToolResult{IsError: true, ForLLM: "boom", Evidence: map[string]interface{}{
		"type": "action", "op": "browser.click", "outcome": "error",
		"title": "Hacker News", "text": "Top stories",
	}}
	h.loop.recordBrowserSurface(handoffCall("sess-fail-plain"), "browser_click", res, "sess-plain")

	snap, ok := h.loop.livePlane.Snapshot("sess-fail-plain")
	if !ok {
		t.Fatal("surface was never registered")
	}
	if snap.State != live.StateActive || snap.WaitingFor != "" {
		t.Fatalf("an ordinary failure must not demand the owner: state=%s waiting_for=%q", snap.State, snap.WaitingFor)
	}
}
