package computer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Without any executor tooling the local computer is an honest view-only/
// none computer: authority reports it, SupportedOps is empty, and every
// control/observe Do fails closed with a reason — never a fake success.
func TestLocalComputerNoExecutorIsTruthful(t *testing.T) {
	c := &LocalComputer{id: "local", controlTool: "", shotTool: "", display: ""}
	state, reason := c.Authority()
	if state != "none" || reason == "" {
		t.Fatalf("authority = %q %q", state, reason)
	}
	if len(c.SupportedOps()) != 0 {
		t.Fatalf("no-tool computer must support nothing, got %v", c.SupportedOps())
	}
	if _, err := c.Do(context.Background(), OpClick, Args{"x": "1", "y": "2"}); err == nil {
		t.Fatal("click without executor must fail closed")
	}
	if _, err := c.Do(context.Background(), OpScreenshot, Args{"path": "/tmp/x.png"}); err == nil {
		t.Fatal("screenshot without observation tool must fail closed")
	}
	if _, err := c.Do(context.Background(), OpExecute, nil); err == nil {
		t.Fatal("unsupported op must fail closed")
	}
}

// With only an observation tool and no display/input executor the computer
// reports view_only and refuses control while allowing nothing fabricated.
func TestLocalComputerViewOnlyRefusesControl(t *testing.T) {
	c := &LocalComputer{id: "local", controlTool: "", shotTool: "scrot", display: ":99"}
	if state, _ := c.Authority(); state != "view_only" {
		t.Fatalf("authority = %q, want view_only", state)
	}
	if _, err := c.Do(context.Background(), OpClick, Args{"x": "0", "y": "0"}); err == nil {
		t.Fatal("control op must be refused in view_only")
	}
}

// Arg validation is strict and server-side: bad coordinates, oversized
// text, and disallowed keys never reach the executor.
func TestLocalComputerArgValidation(t *testing.T) {
	c := &LocalComputer{id: "local", controlTool: "xdotool", display: ":99"}
	ctx := context.Background()
	if _, err := c.Do(ctx, OpClick, Args{"x": "abc", "y": "2"}); err == nil {
		t.Fatal("non-integer coordinate must fail")
	}
	if _, err := c.Do(ctx, OpClick, Args{"x": "-1", "y": "2"}); err == nil {
		t.Fatal("negative coordinate must fail")
	}
	if _, err := c.Do(ctx, OpClick, Args{"x": "99999999", "y": "2"}); err == nil {
		t.Fatal("out-of-range coordinate must fail")
	}
	if _, err := c.Do(ctx, OpType, Args{"text": strings.Repeat("x", maxTypeLen+1)}); err == nil {
		t.Fatal("oversized text must fail")
	}
	if _, err := c.Do(ctx, OpPressKey, Args{"key": "rm"}); err == nil {
		t.Fatal("disallowed key must fail")
	}
	if _, err := c.Do(ctx, OpScreenshot, Args{"path": "/etc/shadow"}); err == nil {
		t.Fatal("output path outside allowed dirs must fail")
	}
	if !isSafeOutputPath(filepath.Join(os.TempDir(), "shot.png")) {
		t.Fatal("temp-dir path must be allowed")
	}
}

// With a real stub executor present, bounded input ops dispatch through the
// actual exec boundary (a real subprocess), return evidence, and report
// "dispatched" rather than a fabricated confirmation.
func TestLocalComputerDispatchWithStubExecutor(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "xdotool")
	script := "#!/bin/sh\necho \"$@\" > \"" + dir + "/calls.txt\"\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	c := &LocalComputer{id: "local", controlTool: stub, display: ":99"}
	if state, _ := c.Authority(); state != "control" {
		t.Fatalf("authority = %q, want control", state)
	}
	res, err := c.Do(context.Background(), OpClick, Args{"x": "10", "y": "20"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Verified {
		t.Fatal("dispatched input must not claim independent verification")
	}
	if res.Evidence["op"] != "click" || res.Evidence["outcome"] != "dispatched" {
		t.Fatalf("evidence wrong: %+v", res.Evidence)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "calls.txt"))
	if !strings.Contains(string(data), "click") {
		t.Fatalf("executor stub not invoked with bounded args: %s", data)
	}
}

// Placement availability distinguishes preview from control truthfully.
func TestLocalAvailabilityPreviewVsControl(t *testing.T) {
	viewOnly := AvailabilityOf(Descriptor{Placement: PlacementLocal, Capabilities: nil}, time.Now())
	if !strings.Contains(viewOnly.Detail, "view only") {
		t.Fatalf("view-only detail missing: %+v", viewOnly)
	}
	control := AvailabilityOf(Descriptor{Placement: PlacementLocal, Capabilities: []Op{OpClick}}, time.Now())
	if !strings.Contains(control.Detail, "executor ready") {
		t.Fatalf("control detail missing: %+v", control)
	}
}
