package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/computer"
)

// New reach ops plumb model args to executor args through the same
// binding rules: unbound calls refuse, control needs permission plus
// authority, and a verified screenshot carries its path for vision.
func TestComputerReachArgPlumbing(t *testing.T) {
	bound := func(op, perm string) context.Context {
		return WithComputerCall(context.Background(), ComputerCall{
			Owner: "o", ContextID: "c", TaskID: "t", SessionKey: "s",
			Op: op, Permission: perm, ControlAuthority: true,
		})
	}
	run := func(action string, args map[string]interface{}) *ToolResult {
		tool := NewComputerTool(action)
		tool.Exec = func() (computer.Computer, error) { return computer.NewVirtualUI("plumb"), nil }
		return tool.Execute(bound(action, "allow"), args)
	}
	// Unbound: refuse before touching anything.
	tool := NewComputerTool("scroll")
	tool.Exec = func() (computer.Computer, error) { return computer.NewVirtualUI("plumb"), nil }
	if res := tool.Execute(context.Background(), map[string]interface{}{"direction": "down"}); !res.IsError {
		t.Fatal("unbound scroll must refuse")
	}
	// Scroll/drag/clipboard flow with evidence.
	if res := run("scroll", map[string]interface{}{"direction": "up", "amount": float64(2)}); res.IsError {
		t.Fatalf("scroll failed: %s", res.ForLLM)
	} else if res.Evidence["amount"] != "2" {
		t.Fatalf("scroll evidence wrong: %+v", res.Evidence)
	}
	if res := run("drag", map[string]interface{}{"x1": float64(1), "y1": float64(1), "x2": float64(9), "y2": float64(9)}); res.IsError {
		t.Fatalf("drag failed: %s", res.ForLLM)
	} else if res.Evidence["x2"] != "9" {
		t.Fatalf("drag evidence wrong: %+v", res.Evidence)
	}
	if res := run("clipboard_set", map[string]interface{}{"text": "abc"}); res.IsError {
		t.Fatalf("clipboard_set failed: %s", res.ForLLM)
	}
	// Click by ref resolves; stale refs fail with an instruction.
	v := computer.NewVirtualUI("refs")
	refTool := NewComputerTool("click")
	refTool.Exec = func() (computer.Computer, error) { return v, nil }
	refCtx := bound("click", "allow")
	if _, err := v.Do(context.Background(), computer.OpInspectUI, computer.Args{}); err != nil {
		t.Fatal(err)
	}
	if res := refTool.Execute(refCtx, map[string]interface{}{"element": "e1"}); res.IsError {
		t.Fatalf("ref click failed: %s", res.ForLLM)
	} else if res.Evidence["ref"] != "e1" {
		t.Fatalf("ref click evidence wrong: %+v", res.Evidence)
	}
	if res := refTool.Execute(refCtx, map[string]interface{}{"element": "e1"}); !res.IsError ||
		!strings.Contains(res.ForLLM, "expired") {
		t.Fatalf("stale ref must fail expired, got: %s", res.ForLLM)
	}
	// Screenshot carries its path for the loop's vision injection.
	shot := NewComputerTool("screenshot")
	shot.Exec = func() (computer.Computer, error) { return computer.NewVirtualUI("shot"), nil }
	res := shot.Execute(bound("screenshot", ""), map[string]interface{}{"path": t.TempDir() + "/s.png"})
	if res.IsError {
		t.Fatalf("screenshot failed: %s", res.ForLLM)
	}
	if res.ScreenshotPath == "" || !strings.HasSuffix(res.ScreenshotPath, "s.png") {
		t.Fatalf("screenshot must carry its path for vision: %+v", res.Evidence)
	}
}
