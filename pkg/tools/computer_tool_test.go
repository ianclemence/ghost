package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/computer"
)

type stubComputer struct{}

func (s *stubComputer) ID() string                    { return "stub" }
func (s *stubComputer) Placement() computer.Placement { return computer.PlacementLocal }
func (s *stubComputer) DisplayName() string           { return "stub" }
func (s *stubComputer) SupportedOps() []computer.Op {
	return []computer.Op{computer.OpScreenshot, computer.OpClick}
}
func (s *stubComputer) Do(ctx context.Context, op computer.Op, args computer.Args) (computer.Result, error) {
	if op == computer.OpScreenshot {
		return computer.Result{Output: "/tmp/shot.png", Verified: true, Evidence: map[string]string{"bytes": "42"}}, nil
	}
	return computer.Result{Output: "dispatched", Verified: false, Evidence: map[string]string{"x": args["x"], "y": args["y"]}}, nil
}

func TestComputerToolUnboundRefuses(t *testing.T) {
	ct := NewComputerTool("screenshot")
	ct.Exec = func() (computer.Computer, error) { return &stubComputer{}, nil }
	res := ct.Execute(context.Background(), map[string]interface{}{"path": "/tmp/x.png"})
	if !res.IsError || !strings.Contains(res.ForLLM, "not bound") {
		t.Fatalf("unbound computer call must refuse: %+v", res)
	}
}

func TestComputerToolForcedControlsRefuse(t *testing.T) {
	ct := NewComputerTool("click")
	ct.Exec = func() (computer.Computer, error) { return &stubComputer{}, nil }
	call := ComputerCall{Op: "click", TaskID: "t", Owner: "o"}
	// Missing permission
	res := ct.Execute(WithComputerCall(context.Background(), call), map[string]interface{}{"x": "1", "y": "1"})
	if !res.IsError {
		t.Fatal("click without broker permission must refuse")
	}
	// Missing control authority
	call.Permission = "allow"
	res = ct.Execute(WithComputerCall(context.Background(), call), map[string]interface{}{"x": "1", "y": "1"})
	if !res.IsError || !strings.Contains(res.ForLLM, "control authority") {
		t.Fatalf("click without control authority must refuse: %+v", res)
	}
	// Operation binding mismatch: authorized screenshot drives a click tool
	call.Permission, call.ControlAuthority = "allow", true
	call.Op = "screenshot"
	res = ct.Execute(WithComputerCall(context.Background(), call), map[string]interface{}{"x": "1", "y": "1"})
	if !res.IsError || !strings.Contains(res.ForLLM, "binding mismatch") {
		t.Fatalf("op mismatch must refuse: %+v", res)
	}
}

func TestComputerToolObserveReturnsEvidence(t *testing.T) {
	ct := NewComputerTool("screenshot")
	var got string
	ct.Exec = func() (computer.Computer, error) { return &stubComputer{}, nil }
	call := ComputerCall{Op: "screenshot", Permission: "allow", TaskID: "t", Owner: "o",
		OnEvidence: func(taskID string, ev computer.Evidence) { got = taskID }}
	res := ct.Execute(WithComputerCall(context.Background(), call), map[string]interface{}{"path": "/tmp/x.png"})
	if res.IsError {
		t.Fatalf("observe failed: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "verified") {
		t.Fatalf("observe result must be evidence-backed: %s", res.ForLLM)
	}
	if res.Evidence["op"] != "computer.screenshot" || res.Evidence["verified"] != true {
		t.Fatalf("evidence wrong: %+v", res.Evidence)
	}
	if got != "t" {
		t.Fatalf("onEvidence task = %q", got)
	}
}

func TestComputerToolControlDoesNotOverclaim(t *testing.T) {
	ct := NewComputerTool("click")
	ct.Exec = func() (computer.Computer, error) { return &stubComputer{}, nil }
	call := ComputerCall{Op: "click", Permission: "once", ControlAuthority: true, TaskID: "t", Owner: "o"}
	res := ct.Execute(WithComputerCall(context.Background(), call), map[string]interface{}{"x": "12", "y": "34"})
	if res.IsError {
		t.Fatalf("click failed: %s", res.ForLLM)
	}
	if strings.Contains(strings.ToLower(res.ForLLM), "completed and verified") {
		t.Fatalf("dispatched input must not claim verified completion: %s", res.ForLLM)
	}
	if !strings.Contains(strings.ToLower(res.ForLLM), "not independently screen-verified") {
		t.Fatalf("result must state dispatch honestly: %s", res.ForLLM)
	}
	if res.Evidence["x"] != "12" {
		t.Fatalf("evidence missing coords: %+v", res.Evidence)
	}
}
