package agent

import (
	"context"
	"testing"

	"github.com/ianclemence/ghost/pkg/cevents"
)

// Approval wait → process death → restart → approve → resume continues the
// SAME logical work item with the SAME pinned browser session.
func TestGateApprovalRestartSafeResume(t *testing.T) {
	ws := t.TempDir()
	h1 := newGateHarnessOnWS(t, ws)
	if err := h1.contexts.SetSessionContext("sess-rs", h1.workCtx); err != nil {
		t.Fatal(err)
	}
	// Turn 1 under Work context: consequential click waits durably.
	al1 := h1.loop
	d := al1.authorizeBrowserCall("req-rs-1", "sess-rs", "browser_click", map[string]interface{}{})
	if d.decision != "wait" {
		t.Fatalf("expected wait, got %s", d.decision)
	}
	if _, ok := h1.broker.PendingForSession("sess-rs"); !ok {
		t.Fatal("no durable pending request")
	}
	pinnedSess := ""

	// Simulate process death: nothing else is needed — the request, the
	// browser session row, and the context-session mapping all live in
	// the workspace's SQLite/contexts.json. A fresh gateway harness over
	// the SAME workspace is the restart.
	h2 := newGateHarnessOnWS(t, ws)
	// The restarted context store carries the session→Work mapping.
	if got := h2.contexts.SessionContext("sess-rs"); got != h2.workCtx && got != h1.workCtx {
		t.Fatalf("context mapping lost across restart: %q", got)
	}
	// The pinned browser session minted at wait time is still there.
	pending2, ok := h2.broker.PendingForSession("sess-rs")
	if !ok {
		t.Fatal("pending approval must survive restart (SQLite)")
	}
	pinnedSess = pending2.Continuation[contBrowserSession]
	if pinnedSess == "" {
		t.Fatal("continuation lost its pinned session across restart")
	}

	// Owner approves after restart; the reply resolves and resumes.
	resume := h2.loop.governance.CheckApprovalReply("sess-rs", "allow once")
	if !resume.Resumed {
		t.Fatalf("approval reply after restart must resume: %+v", resume)
	}
	call, refuse := h2.loop.resumeBrowserCall(resume, "sess-rs", "req-rs-1")
	if refuse != nil {
		t.Fatalf("post-restart resume refused: %s", refuse.ForLLM)
	}
	if call.SessionID != pinnedSess {
		t.Fatalf("resume must reuse the pinned session: %q != %q", call.SessionID, pinnedSess)
	}
	res := h2.loop.runBrowserTool(context.Background(), call, resume.Tool, resume.Args, "test", "chat", "sess-rs")
	if res.IsError {
		t.Fatalf("post-restart execution failed: %s", res.ForLLM)
	}
	if len(h2.stubs["browser_click"].bags) != 1 {
		t.Fatal("post-restart execution must run exactly once with a binding")
	}
	if got := h2.stubs["browser_click"].bags[0].SessionID; got != pinnedSess {
		t.Fatalf("bound session %q != pinned %q", got, pinnedSess)
	}
}

// K. Replay cannot re-execute: after a one-shot approval is consumed, a
// fresh turn issuing the same model call must ask again — never silently
// reuse the spent approval.
func TestGateFreshTurnDoesNotReuseSpentApproval(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	resume := approveOnce(t, h, "req-k-1", "sess-k")
	call, refuse := al.resumeBrowserCall(resume, "sess-k", "req-k-1")
	if refuse != nil {
		t.Fatalf("resume refused: %s", refuse.ForLLM)
	}
	res := al.runBrowserTool(h.toolCtx(), call, resume.Tool, resume.Args, "test", "chat", "sess-k")
	if res.IsError {
		t.Fatalf("first execution failed: %s", res.ForLLM)
	}
	executions := h.stubs["browser_click"].calls

	// The model re-issues the identical call in a NEW turn. No standing
	// grant exists, so it must wait again — the spent approval cannot
	// auto-authorize a replay.
	d2 := al.authorizeBrowserCall("req-k-2", "sess-k", "browser_click", map[string]interface{}{})
	if d2.decision != "wait" {
		t.Fatalf("replay without grant must wait, got %s", d2.decision)
	}
	if h.stubs["browser_click"].calls != executions {
		t.Fatal("replayed call executed without fresh approval")
	}
}

// L. The canonical event carries the execution's own evidence (operation,
// owner, context, task, session, permission, outcome) — not a fabricated
// success. A failed execution emits a failure event with the same shape.
func TestGateEventCarriesExecutionEvidence(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	// Allowed observe.
	res, governed, _ := al.maybeRunBrowserTool(h.toolCtx(), al.tools, fakeToolCall("browser_snapshot", nil), h.opts("sess-l"), nil)
	if !governed || res.IsError {
		t.Fatalf("observe must execute: %v %v", governed, res)
	}
	var ev *cevents.Event
	for _, e := range h.events.ByRequest("req-sess-l-1") {
		if e.Type == cevents.ToolCompleted {
			ev = e
		}
	}
	if ev == nil {
		t.Fatal("missing tool.completed event")
	}
	if ev.Status != "success" {
		t.Fatalf("status = %q", ev.Status)
	}
	for _, k := range []string{"owner", "context", "task", "permission", "outcome", "session"} {
		if ev.Payload[k] == nil || ev.Payload[k] == "" {
			t.Fatalf("event payload missing %q: %+v", k, ev.Payload)
		}
	}
	if ev.Payload["owner"] != h.ghostID || ev.Payload["task"] != "sess-l" || ev.Payload["permission"] != "allow" {
		t.Fatalf("event evidence wrong: %+v", ev.Payload)
	}

	// The failure path emits tool.failed with the same evidence shape.
	h2 := newGateHarness(t)
	al2 := h2.loop
	h2.stubs["browser_click"].failWith = true
	if err := h2.broker.GrantStanding("browser", "browser_click", "session:sess-l2", false); err != nil {
		t.Fatal(err)
	}
	res2, governed2, stop2 := al2.maybeRunBrowserTool(h2.toolCtx(), al2.tools, fakeToolCall("browser_click", map[string]interface{}{"ref": "@e"}), h2.opts("sess-l2"), nil)
	if !governed2 || stop2 || res2 == nil {
		t.Fatalf("click must run, governed=%v stop=%v", governed2, stop2)
	}
	if !res2.IsError {
		t.Fatalf("expected failing result, got %+v", res2)
	}
	foundFail := false
	for _, e := range h2.events.ByRequest("req-sess-l2-1") {
		if e.Type == cevents.ToolFailed {
			foundFail = true
			if e.Status != "failed" || e.Payload["outcome"] != "error" {
				t.Fatalf("failed event wrong: %+v", e.Payload)
			}
		}
	}
	if !foundFail {
		t.Fatal("missing tool.failed event for errored execution")
	}
}
