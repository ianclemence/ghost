package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// What the live Ghost ran while trying to build a Go project, and was refused
// for. None of it touches Ghost's estate.
func TestGuardLetsCodingWorkThrough(t *testing.T) {
	home, ws := projectsHome(t)
	proj := filepath.Join(home, "projects", "scout")
	cases := []struct{ name, cmd, cwd string }{
		{"test a package named memory", "go test ./pkg/memory/... 2>&1 | tail -20", proj},
		{"package named state, output to a file", "go build ./pkg/state ./pkg/sessions > build.log 2>&1", proj},
		{"README in the project", "cat README.md > /dev/null; head -5 README.md", proj},
		{"discarded stderr next to the workspace path", "ls " + ws + "/tmp/go/pkg/mod 2>/dev/null | head", ws},
		{"redirect into the scratch area", "go build ./... > " + ws + "/tmp/build.log 2>&1", proj},
		{"listing the cache while scratch is named", "ls " + ws + "/tmp/go/pkg/mod/github.com; echo done", ws},
	}
	for _, c := range cases {
		if msg, denied := execDeniedPrimaryFiles(c.cmd, c.cwd, ws); denied {
			t.Errorf("%s was refused: %s\n  command: %s", c.name, msg, c.cmd)
		}
	}
}

// The guard still stops the writes it exists for, from a project as well.
func TestGuardStillRefusesTheEstate(t *testing.T) {
	home, ws := projectsHome(t)
	proj := filepath.Join(home, "projects", "scout")
	cases := []struct{ name, cmd, cwd string }{
		{"remove the skills by absolute path from a project", "rm -rf " + ws + "/skills", proj},
		{"overwrite the contract from a project", "echo x > " + ws + "/GHOST.md", proj},
		{"move the memory digest from a project", "mv " + ws + "/memory/MEMORY.md /tmp/x", proj},
		{"remove the workspace itself", "rm -rf " + ws, proj},
		{"overwrite the installed binary", "cp ./ghost /usr/local/bin/ghost", proj},
		{"bare name from the workspace", "echo x > MEMORY.md", ws},
		{"bare name with a discarded stderr", "rm -rf skills 2>/dev/null", ws},
		{"scratch path does not excuse a real target", "cp " + ws + "/tmp/a " + ws + "/state/x", ws},
		{"the database", "cat ghost.db", proj},
	}
	for _, c := range cases {
		if _, denied := execDeniedPrimaryFiles(c.cmd, c.cwd, ws); !denied {
			t.Errorf("%s must be refused\n  command: %s", c.name, c.cmd)
		}
	}
}

func TestExecTimeoutIsPerCallAndBounded(t *testing.T) {
	tool := NewExecTool(t.TempDir(), true)
	cases := []struct {
		args map[string]interface{}
		want time.Duration
	}{
		{nil, 60 * time.Second},
		{map[string]interface{}{"timeout_seconds": float64(600)}, 600 * time.Second},
		{map[string]interface{}{"timeout_seconds": 90}, 90 * time.Second},
		{map[string]interface{}{"timeout_seconds": float64(999999)}, maxExecTimeout},
		{map[string]interface{}{"timeout_seconds": float64(0)}, 60 * time.Second},
		{map[string]interface{}{"timeout_seconds": "soon"}, 60 * time.Second},
	}
	for _, c := range cases {
		if got := tool.callTimeout(c.args); got != c.want {
			t.Errorf("callTimeout(%v) = %v, want %v", c.args, got, c.want)
		}
	}
	// The wrapper waits a little longer than the command, so the command says so first.
	if got := tool.TimeoutFor(map[string]interface{}{"timeout_seconds": float64(600)}); got != 600*time.Second+execGrace {
		t.Errorf("wrapper budget = %v", got)
	}
	// And the wrapper honours it: a 6-minute build is not cut at the 5-minute default.
	reg := NewToolRegistry()
	reg.Register(tool)
	if d := tool.TimeoutFor(map[string]interface{}{"timeout_seconds": float64(1200)}); d <= defaultToolTimeout {
		t.Errorf("a long command would still be cut at %v", defaultToolTimeout)
	}
}

// A timed-out command says what to do next, instead of leaving the model to
// invent a way to background it.
func TestExecTimeoutTellsTheModelHowToRecover(t *testing.T) {
	_, ws := projectsHome(t)
	res := NewExecTool(ws, true).Execute(context.Background(), map[string]interface{}{
		"command": "sleep 5", "timeout_seconds": float64(1),
	})
	if !res.IsError || !strings.Contains(res.ForLLM, "timeout_seconds") || !strings.Contains(res.ForLLM, "1800") {
		t.Fatalf("a timeout must explain timeout_seconds: %+v", res)
	}
}

// A caller that goes away is not the tool running long. The live run reported
// "timed out after 5m0s" twenty seconds after the client disconnected.
type slowTool struct{}

func (slowTool) Name() string                       { return "slow" }
func (slowTool) Description() string                { return "Waits." }
func (slowTool) Parameters() map[string]interface{} { return map[string]interface{}{"type": "object"} }
func (slowTool) Execute(ctx context.Context, _ map[string]interface{}) *ToolResult {
	<-ctx.Done()
	return &ToolResult{ForLLM: "interrupted", IsError: true}
}

func TestACancelledCallerIsNotReportedAsATimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(50 * time.Millisecond); cancel() }()
	res := executeWithReliability(ctx, slowTool{}, nil)
	if strings.Contains(res.ForLLM, "timed out after") || res.TimedOut {
		t.Fatalf("a cancelled request was reported as the tool running long: %+v", res)
	}
	if !strings.Contains(res.ForLLM, "cancelled") {
		t.Fatalf("it should say the request was cancelled: %q", res.ForLLM)
	}
}

// Everything Ghost's service starts runs in a sandbox that ends with the
// command, so a build run with & never finishes; the description says not to.
func TestExecDescriptionWarnsAgainstBackgrounding(t *testing.T) {
	d := NewExecTool(t.TempDir(), true).Description()
	for _, want := range []string{"timeout_seconds", "never background", "1800", "not bash"} {
		if !strings.Contains(strings.ToLower(d), strings.ToLower(want)) {
			t.Errorf("exec description must mention %q", want)
		}
	}
	_ = os.Getpid
}
