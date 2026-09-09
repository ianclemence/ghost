package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/computer"
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
