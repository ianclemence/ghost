package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/computer"
	"github.com/ianclemence/ghost/pkg/db"
	"github.com/ianclemence/ghost/pkg/live"
)

// Screenshot (observation) is a broker read; control ops (click, type,
// press_key) must obtain a broker decision before any executor runs.
func TestComputerGateRiskAndBroker(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	al.setTestComputer(computer.LocalComputerConfigured("local", "/bin/true", "", ":99"))

	snap := al.authorizeComputerCall("req-c1", "sess-c", "computer_screenshot", map[string]interface{}{"path": "/tmp/x.png"})
	if snap.decision != "allow" {
		t.Fatalf("read-only screenshot must allow under broker read, got %s: %s", snap.decision, snap.message)
	}

	for _, tool := range []string{"computer_click", "computer_type", "computer_press_key"} {
		args := map[string]interface{}{}
		switch tool {
		case "computer_click":
			args = map[string]interface{}{"x": "10", "y": "10"}
		case "computer_type":
			args = map[string]interface{}{"text": "Ghost"}
		case "computer_press_key":
			args = map[string]interface{}{"key": "Return"}
		}
		d := al.authorizeComputerCall("req-"+tool, "sess-c", tool, args)
		if d.decision == "allow" {
			t.Fatalf("%s must not auto-allow without a grant", tool)
		}
		if d.pending == "" {
			t.Fatalf("%s must leave a durable pending request or explicit denial", tool)
		}
	}
}

// A denied control operation must never be allowed.
func TestComputerGateDenyNeverRuns(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	d := al.authorizeComputerCall("req-d1", "sess-cd", "computer_click", map[string]interface{}{"x": "1", "y": "1"})
	if d.decision == "allow" {
		t.Fatal("must not allow ungranted control")
	}
	if err := denyPending(h, "req-d1", "sess-cd"); err != nil {
		t.Fatal(err)
	}
	d2 := al.authorizeComputerCall("req-d2", "sess-cd", "computer_click", map[string]interface{}{"x": "1", "y": "1"})
	if d2.decision == "allow" {
		t.Fatalf("denied computer click must stay denied: %+v", d2)
	}
}

// With an explicit broker grant the control call becomes allowed; the
// binding carries server-resolved owner/context/task, never arguments.
func TestComputerGateGrantedBindingServerResolved(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	al.setTestComputer(computer.LocalComputerConfigured("local", "/bin/true", "", ":99"))
	if err := h.broker.GrantStanding("computer", "computer_click", "session:sess-cg", false); err != nil {
		t.Fatal(err)
	}
	d := al.authorizeComputerCall("req-g1", "sess-cg", "computer_click", map[string]interface{}{"x": "1", "y": "1", "owner": "mallory", "task": "evil"})
	if d.decision != "allow" {
		t.Fatalf("granted click must allow: %+v", d)
	}
	if d.call.Owner != h.ghostID || d.call.ContextID != h.contexts.SessionContext("sess-cg") {
		t.Fatalf("forged owner/context leaked into binding: %+v", d.call)
	}
	if d.call.TaskID != "sess-cg" {
		t.Fatalf("forged task leaked into binding: %+v", d.call)
	}
}

// A stale task generation refuses an approval resume for computer control.
func TestComputerResumeStaleGenerationRefuses(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	al.setTestComputer(computer.LocalComputerConfigured("local", "/bin/true", "", ":99"))
	job, err := h.jobs.Create("computer", "sess-cs", nil)
	if err != nil {
		t.Fatal(err)
	}
	if d := al.authorizeComputerCall("req-cs1", "sess-cs", "computer_click", map[string]interface{}{"x": "1", "y": "1"}); d.decision == "allow" {
		t.Fatalf("control must ask: %+v", d)
	}
	resume := al.governance.CheckApprovalReply("sess-cs", "allow once")
	if !resume.Resumed {
		t.Fatalf("approval reply must resume: %+v", resume)
	}
	if _, ok := resume.Args[contTask].(string); !ok {
		t.Fatalf("continuation must carry task: %+v", resume.Args)
	}
	if _, err := h.jobs.RotateGeneration(job.ID); err != nil {
		t.Fatal(err)
	}
	_, refuse := al.resumeComputerCall(resume, "sess-cs", "req-cs1")
	if refuse == nil || !strings.Contains(refuse.ForLLM, "stale generation") {
		t.Fatalf("stale generation must refuse resume: %+v", refuse)
	}
}

// Forged operation names are not part of the taxonomy and never map to an
// executor.
func TestComputerUnknownOpsFailClosed(t *testing.T) {
	for _, tool := range []string{"computer_run", "computer_shell", "computer_transact", "computer --flag"} {
		if isComputerTool(tool) {
			t.Fatalf("forged op %q must not be recognized", tool)
		}
	}
	// The taxonomy surface is exactly the bounded set; generic execution is
	// structurally unreachable under the computer capability.
	for _, want := range []string{"computer_inspect_ui", "computer_screenshot", "computer_click", "computer_type", "computer_press_key"} {
		if !isComputerTool(want) {
			t.Fatalf("governed op %q missing from taxonomy", want)
		}
	}
	if isComputerTool("computer_exec") || isComputerTool("exec") || isComputerTool("computer_shell") {
		t.Fatal("generic execution must never be a model-reachable computer op")
	}
}

// Observation (screenshot, inspect_ui) is available under view-only
// authority; control ops are refused until the executor has control.
func TestComputerObservationViewOnlyControlRefused(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	// shot tool present, no input executor => view-only.
	al.setTestComputer(computer.LocalComputerConfigured("local", "", "/bin/true", ":99"))

	if d := al.authorizeComputerCall("req-v1", "sess-v", "computer_inspect_ui", map[string]interface{}{}); d.decision != "allow" {
		t.Fatalf("inspect_ui must allow under view-only authority: %+v", d)
	}
	if d := al.authorizeComputerCall("req-v2", "sess-v", "computer_screenshot", map[string]interface{}{"path": "/tmp/x.png"}); d.decision != "allow" {
		t.Fatalf("screenshot must allow under view-only authority: %+v", d)
	}
	for _, tool := range []string{"computer_click", "computer_type", "computer_press_key"} {
		d := al.authorizeComputerCall("req-v-"+tool, "sess-v", tool, map[string]interface{}{"x": "1", "y": "1"})
		if d.decision == "allow" {
			t.Fatalf("%s must be refused under view-only authority", tool)
		}
		if !strings.Contains(d.message, "view-only") {
			t.Fatalf("%s denial must cite view-only authority: %s", tool, d.message)
		}
	}
}

// An ungoverned loop refuses computer operations outright.
func TestComputerUnwiredLoopRefuses(t *testing.T) {
	al := newTestAgentLoopWithProvider(t, t.TempDir(), &countingProvider{})
	res, governed, stop := al.maybeRunComputerTool(context.Background(), nil,
		fakeToolCall("computer_screenshot", map[string]interface{}{"path": "/tmp/x.png"}),
		processOptions{SessionKey: "s", RequestID: "r"}, nil)
	if !governed || !stop || res == nil || !strings.Contains(res.ForLLM, "not governed") {
		t.Fatalf("unwired loop must refuse computer: governed=%v stop=%v res=%+v", governed, stop, res)
	}
}

// Live Surface plane integration: a human takeover of the computer pauses
// Ghost (the gate refuses further ops), release does NOT auto-resume, and
// only a revalidating resume lets Ghost act again. This is the "takeover
// pauses autonomous control" invariant enforced at the real gate. Uses the
// observation path (no durable lease) so the test is hermetic; the pause
// check runs before the observation/control branch, so it covers control
// ops identically.
func TestComputerGatePausedByLiveSurfaceTakeover(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	al.setTestComputer(computer.NewVirtualUI("settings"))
	plane := live.NewRegistry("ghost-test-1")
	plane.Register("local", live.KindComputer)
	al.SetLivePlane(plane)

	allow := func() bool {
		d := al.authorizeComputerCall("req-live", "sess-live", "computer_screenshot",
			map[string]interface{}{"path": "/tmp/x.png"})
		return d.decision == "allow"
	}
	if !allow() {
		t.Fatal("ghost may observe the computer before takeover")
	}

	if _, err := plane.Takeover("local", "device-mob", time.Minute); err != nil {
		t.Fatal(err)
	}
	if allow() {
		t.Fatal("ghost must be paused while the user controls the computer")
	}

	if err := plane.Release("local", "device-mob", false); err != nil {
		t.Fatal(err)
	}
	if allow() {
		t.Fatal("release must NOT auto-resume Ghost; revalidation required")
	}
	if err := plane.Resume("local"); err != nil {
		t.Fatal(err)
	}
	if !allow() {
		t.Fatal("after revalidated resume ghost may act again")
	}
}

// Browser gate honors the same Live Surface pause: a human takeover of the
// browser session pauses Ghost; release requires a revalidating resume.
func TestBrowserGatePausedByLiveSurfaceTakeover(t *testing.T) {
	h := newGateHarness(t)
	al := h.loop
	plane := live.NewRegistry("ghost-test-1")
	al.SetLivePlane(plane)

	allow := func() bool {
		d := al.authorizeBrowserCall("req-bp", "sess-bp", "browser_snapshot", map[string]interface{}{})
		return d.decision == "allow"
	}
	if !allow() {
		t.Fatal("ghost may observe the browser before takeover")
	}
	// The surface id is the deterministic browser session for owner+context+task.
	ledger, err := al.browserSessionLedger()
	if err != nil {
		t.Fatal(err)
	}
	sess, err := ledger.GetOrCreate("ghost-test-1", "personal", "sess-bp", "default", 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := plane.Takeover(sess.ID, "device-mob", time.Minute); err != nil {
		t.Fatal(err)
	}
	if allow() {
		t.Fatal("ghost must be paused while the user controls the browser")
	}
	if err := plane.Release(sess.ID, "device-mob", false); err != nil {
		t.Fatal(err)
	}
	if allow() {
		t.Fatal("browser release must NOT auto-resume Ghost")
	}
	if err := plane.Resume(sess.ID); err != nil {
		t.Fatal(err)
	}
	if !allow() {
		t.Fatal("after revalidated resume ghost may observe the browser again")
	}
}

// TestComputerLeaseStorePerLoop proves lease ledgers are memoized per
// AgentLoop, not shared process-wide: a lease acquired through one loop's
// store must never block an unrelated loop (this leaked golden computer
// cases into each other, failing G062 with "locked by another task").
func TestComputerLeaseStorePerLoop(t *testing.T) {
	mkLoop := func() *AgentLoop {
		t.Helper()
		database, err := db.NewDB(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { database.Close() })
		return &AgentLoop{db: database}
	}
	al1, al2 := mkLoop(), mkLoop()
	s1, err := al1.computerLeaseStore()
	if err != nil {
		t.Fatal(err)
	}
	s2, err := al2.computerLeaseStore()
	if err != nil {
		t.Fatal(err)
	}
	if s1 == s2 {
		t.Fatal("loops must not share a lease store instance")
	}
	if _, err := s1.Acquire("res-1", "o1", "task-1", "sess-1", "ctx", time.Minute); err != nil {
		t.Fatal(err)
	}
	// Same resource through the other loop's store must be free.
	if _, err := s2.Acquire("res-1", "o2", "task-2", "sess-2", "ctx", time.Minute); err != nil {
		t.Fatalf("second loop must not see the first loop's lease: %v", err)
	}
}
