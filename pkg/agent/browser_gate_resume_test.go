package agent

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/contexts"
	"github.com/ianclemence/ghost/pkg/permissions"
)

// approveOnce drives a click through wait → chat approval → resume args,
// in production order: the user replies first, the reply resolves and
// consumes the pending request, and the continuation resumes.
func approveOnce(t *testing.T, h *gateHarness, requestID, session string) ResumeOutcome {
	t.Helper()
	al := h.loop
	d := al.authorizeBrowserCall(requestID, session, "browser_click", map[string]interface{}{})
	if d.decision != "wait" {
		t.Fatalf("expected wait, got %s", d.decision)
	}
	if _, ok := h.broker.PendingForSession(session); !ok {
		t.Fatal("no pending request")
	}
	resume := al.governance.CheckApprovalReply(session, "allow once")
	if !resume.Resumed {
		t.Fatalf("chat approval must resume: %+v", resume)
	}
	if resume.Grant != permissions.GrantOnce {
		t.Fatalf("grant = %q", resume.Grant)
	}
	return resume
}

// D. Approval resumes the SAME logical work item, then executes once.
func TestGateApprovalResumesSameWork(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	resume := approveOnce(t, h, "req-r-1", "sess-r")
	if resume.Tool != "browser_click" {
		t.Fatalf("resume tool = %q", resume.Tool)
	}
	call, refuse := al.resumeBrowserCall(resume, "sess-r", "req-r-1")
	if refuse != nil {
		t.Fatalf("resume refused: %s", refuse.ForLLM)
	}
	if call.Owner != h.ghostID || call.TaskID != "sess-r" || call.SessionID == "" || call.Permission != "once" {
		t.Fatalf("resume binding wrong: %+v", call)
	}
	res := al.runBrowserTool(h.toolCtx(), call, resume.Tool, resume.Args, "test", "chat-1", "sess-r")
	if res.IsError {
		t.Fatalf("resumed execution failed: %s", res.ForLLM)
	}
	if h.stubs["browser_click"].calls != 1 {
		t.Fatal("resumed work must execute exactly once")
	}
	// Duplicate approval reply finds nothing to consume.
	again := al.governance.CheckApprovalReply("sess-r", "allow once")
	if again.Resumed {
		t.Fatal("second approval reply must not resume again")
	}
}

// D2. Always-allow approvals resume the paused call (previously dropped).
func TestGateAlwaysAllowResumes(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	d := al.authorizeBrowserCall("req-a-1", "sess-a2", "browser_click", map[string]interface{}{})
	if d.decision != "wait" {
		t.Fatalf("expected wait, got %s", d.decision)
	}
	// The user answers "always allow" in chat: the reply stores the
	// standing grant AND resumes the paused call (previously it stored
	// the grant but dropped the resume).
	resume := al.governance.CheckApprovalReply("sess-a2", "always allow")
	if !resume.Resumed {
		t.Fatalf("always-allow must resume the paused call: %+v", resume)
	}
	if resume.Grant != permissions.GrantAlways {
		t.Fatalf("grant = %q", resume.Grant)
	}
	call, refuse := al.resumeBrowserCall(resume, "sess-a2", "req-a-1")
	if refuse != nil {
		t.Fatalf("resume refused: %s", refuse.ForLLM)
	}
	if call.Permission != "always" {
		t.Fatalf("permission = %q", call.Permission)
	}
	// A later identical call is allowed directly by the standing grant.
	d2 := al.authorizeBrowserCall("req-a-2", "sess-a2", "browser_click", map[string]interface{}{})
	if d2.decision != "allow" {
		t.Fatalf("standing grant must allow, got %s", d2.decision)
	}
}

// Revocation takes effect immediately, even mid-flow: a standing grant
// removed between approval and execution stops the resume.
func TestGateRevocationStopsResume(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	resume := approveOnce(t, h, "req-v-1", "sess-v")
	// Standing grant lands after the one-shot approval (owner upgrades).
	if err := h.broker.GrantStanding("browser", "browser_click", "session:sess-v", false); err != nil {
		t.Fatal(err)
	}
	// Owner revokes before the resume executes.
	if err := h.broker.Revoke("browser", "browser_click", "session:sess-v"); err != nil {
		t.Fatal(err)
	}
	// A fresh identical call now waits instead of sailing through.
	d := al.authorizeBrowserCall("req-v-2", "sess-v", "browser_click", map[string]interface{}{})
	if d.decision != "wait" {
		t.Fatalf("revoked grant must ask again, got %s", d.decision)
	}
	// And the pre-revocation one-shot resume still executes: the approval
	// was consumed before revocation, and binding checks pass. Revocation
	// governs future authorizations, not consumed approvals.
	call, refuse := al.resumeBrowserCall(resume, "sess-v", "req-v-1")
	if refuse != nil {
		t.Fatalf("consumed one-shot approval must still resume: %s", refuse.ForLLM)
	}
	_ = call
	// A standing-grant resume after revocation refuses via re-evaluation.
	resume2 := approveOnce(t, h, "req-v-3", "sess-v")
	resume2.Grant = permissions.GrantAlways
	if err := h.broker.GrantStanding("browser", "browser_click", "session:sess-v", false); err != nil {
		t.Fatal(err)
	}
	if err := h.broker.Revoke("browser", "browser_click", "session:sess-v"); err != nil {
		t.Fatal(err)
	}
	_, refuse2 := al.resumeBrowserCall(resume2, "sess-v", "req-v-3")
	if refuse2 == nil || !strings.Contains(refuse2.ForLLM, "revoked") {
		t.Fatalf("revoked standing grant must refuse resume, got %+v", refuse2)
	}
}

// E. Context change between approval and resume refuses.
func TestGateContextChangeRefusesResume(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	if err := h.contexts.SetSessionContext("sess-e", h.workCtx); err != nil {
		t.Fatal(err)
	}
	resume := approveOnce(t, h, "req-e-1", "sess-e")
	if got, _ := resume.Args[contContext].(string); got != h.workCtx {
		t.Fatalf("approval must be bound to work context, got %q", got)
	}
	// The session moves to personal before the user replies.
	var personalID string
	for _, c := range h.contexts.List() {
		if c.Kind == contexts.KindPersonal {
			personalID = c.ID
		}
	}
	if personalID == "" {
		t.Fatal("no personal context")
	}
	if err := h.contexts.SetSessionContext("sess-e", personalID); err != nil {
		t.Fatal(err)
	}
	_, refuse := al.resumeBrowserCall(resume, "sess-e", "req-e-1")
	if refuse == nil || !strings.Contains(refuse.ForLLM, "Context changed") {
		t.Fatalf("must refuse on context change, got %+v", refuse)
	}
	if h.stubs["browser_click"].calls != 0 {
		t.Fatal("refused resume must not execute")
	}
}

// E2. A context that disallows the browser capability denies fresh calls.
func TestGateContextCapabilityDeny(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	if err := h.contexts.SetSessionContext("sess-e2", h.workCtx); err != nil {
		t.Fatal(err)
	}
	if err := h.contexts.SetCapabilities(h.workCtx, []string{"calendar.read"}); err != nil {
		t.Fatal(err)
	}
	d := al.authorizeBrowserCall("req-e2-1", "sess-e2", "browser_navigate", map[string]interface{}{"url": "https://example.com"})
	if d.decision != "deny" {
		t.Fatalf("disallowed context must deny, got %s", d.decision)
	}
}

// F. Owner mismatch refuses (forged or migrated approval).
func TestGateOwnerMismatchRefuses(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	resume := approveOnce(t, h, "req-f-1", "sess-f")
	resume.Args[contOwner] = "ghost-impostor"
	_, refuse := al.resumeBrowserCall(resume, "sess-f", "req-f-1")
	if refuse == nil || !strings.Contains(refuse.ForLLM, "Owner mismatch") {
		t.Fatalf("must refuse on owner mismatch, got %+v", refuse)
	}
}

// I. Stale task generation refuses; current generation resumes.
func TestGateStaleGenerationRefuses(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	job, err := h.jobs.Create("browse", "sess-i", map[string]interface{}{"url": "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	resume := approveOnce(t, h, "req-i-1", "sess-i")
	if got, _ := resume.Args[contTask].(string); got != job.ID {
		t.Fatalf("approval must bind the job, got %q", got)
	}
	gen1, _ := resume.Args[contGeneration].(string)
	if gen1 == "" || gen1 != job.Generation {
		t.Fatalf("approval must pin the generation, got %q want %q", gen1, job.Generation)
	}
	// The task moved on (retry/restart rotated the generation).
	if _, err := h.jobs.RotateGeneration(job.ID); err != nil {
		t.Fatal(err)
	}
	_, refuse := al.resumeBrowserCall(resume, "sess-i", "req-i-1")
	if refuse == nil || !strings.Contains(refuse.ForLLM, "stale generation") {
		t.Fatalf("must refuse stale generation, got %+v", refuse)
	}
	if h.stubs["browser_click"].calls != 0 {
		t.Fatal("stale resume must not execute")
	}
}

// J. Forged arguments cannot widen authority; binding is server-side.
func TestGateForgedArgsIgnored(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	d := al.authorizeBrowserCall("req-j-1", "sess-j", "browser_navigate", map[string]interface{}{
		"url": "https://example.com", "owner": "mallory",
		"context": "evil", "task": "task-evil", "session_key": "sess-evil",
		"browser_session": "sess-evil", "generation": "gen-evil",
	})
	if d.decision != "allow" {
		t.Fatalf("observe must allow, got %s", d.decision)
	}
	wantCtx := h.contexts.SessionContext("sess-j")
	if d.call.Owner != h.ghostID || d.call.ContextID != wantCtx || d.call.TaskID != "sess-j" || d.call.SessionID != "" || d.call.Generation != "" {
		t.Fatalf("forged args leaked into binding: %+v", d.call)
	}
	for _, forged := range []string{"mallory", "evil", "task-evil", "sess-evil", "gen-evil"} {
		if strings.Contains(d.call.Owner, forged) || strings.Contains(d.call.ContextID, forged) || strings.Contains(d.call.TaskID, forged) {
			t.Fatalf("forged value %q in binding: %+v", forged, d.call)
		}
	}
	// Forged operation swap in a resumed continuation refuses.
	resume := approveOnce(t, h, "req-j-2", "sess-j2")
	resume.Args[contBrowserOp] = "navigate"
	resume.Args[contBrowserTool] = "browser_navigate"
	_, refuse := al.resumeBrowserCall(resume, "sess-j2", "req-j-2")
	if refuse == nil {
		t.Fatal("operation swap in continuation must refuse")
	}
}

// Task binding: a session running a job binds the job; other sessions
// stay interactive; ambiguity fails closed.
func TestGateTaskBinding(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	job, err := h.jobs.Create("browse", "sess-t", nil)
	if err != nil {
		t.Fatal(err)
	}
	d := al.authorizeBrowserCall("req-t-1", "sess-t", "browser_navigate", map[string]interface{}{"url": "https://example.com"})
	if d.decision != "allow" {
		t.Fatalf("expected allow, got %s", d.decision)
	}
	if d.call.TaskID != job.ID || d.call.Generation != job.Generation {
		t.Fatalf("must bind the live job: %+v", d.call)
	}
	other := al.authorizeBrowserCall("req-t-2", "sess-other", "browser_navigate", map[string]interface{}{"url": "https://example.com"})
	if other.decision != "allow" || other.call.TaskID != "sess-other" || other.call.Generation != "" {
		t.Fatalf("unrelated session must stay interactive: %+v", other.call)
	}
	// Two live jobs on one session is ambiguous: fail closed.
	if _, err := h.jobs.Create("browse", "sess-t", nil); err != nil {
		t.Fatal(err)
	}
	amb := al.authorizeBrowserCall("req-t-3", "sess-t", "browser_navigate", map[string]interface{}{"url": "https://example.com"})
	if amb.decision != "deny" {
		t.Fatalf("ambiguous work items must deny, got %s", amb.decision)
	}
}

// M. Success without evidence is reported unverified, never as done.
func TestGateNoEvidenceNoSuccessClaim(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	h.stubs["browser_click"].noEvidence = true
	// Grant first so the call is allowed.
	d := al.authorizeBrowserCall("req-m-1", "sess-m", "browser_click", map[string]interface{}{})
	if d.decision != "wait" {
		t.Fatalf("expected wait, got %s", d.decision)
	}
	pending, _ := h.broker.PendingForSession("sess-m")
	if _, err := h.broker.Resolve(pending.ID, permissions.GrantAlways, "session:sess-m"); err != nil {
		t.Fatal(err)
	}
	d2 := al.authorizeBrowserCall("req-m-2", "sess-m", "browser_click", map[string]interface{}{})
	if d2.decision != "allow" {
		t.Fatalf("expected allow, got %s", d2.decision)
	}
	res := al.runBrowserTool(h.toolCtx(), d2.call, "browser_click", map[string]interface{}{}, "test", "chat-1", "sess-m")
	if !res.IsError || !strings.Contains(res.ForLLM, "no runtime evidence") {
		t.Fatalf("must report unverified, got %+v", res)
	}
	al.publishBrowserEvidence("req-m-2", "sess-m", "browser_click", res)
	failed := false
	for _, e := range h.events.ByRequest("req-m-2") {
		if e.Type == cevents.ToolFailed {
			failed = true
		}
		if e.Type == cevents.ToolCompleted {
			t.Fatal("unverified execution must not emit a success event")
		}
	}
	if !failed {
		t.Fatal("unverified execution must emit a failure event")
	}
}

// Secrets never persist into approval continuations.
func TestGateContinuationStripsSecrets(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	d := al.authorizeBrowserCall("req-s-1", "sess-s", "browser_type", map[string]interface{}{
		"ref": "@e1", "text": "hello", "password": "s3cret", "api_token": "tok",
	})
	if d.decision != "wait" {
		t.Fatalf("expected wait, got %s", d.decision)
	}
	pending, _ := h.broker.PendingForSession("sess-s")
	for k := range pending.Continuation {
		lk := strings.ToLower(k)
		if strings.Contains(lk, "password") || strings.Contains(lk, "token") {
			t.Fatalf("continuation carries secret key %q", k)
		}
	}
}
