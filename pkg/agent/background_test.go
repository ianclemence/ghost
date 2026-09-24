package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/session"
	"github.com/ianclemence/ghost/pkg/tools"
)

// bgTestAsyncTool captures its injected callback so the test completes it
// manually, like a subagent finishing after the turn.
type bgTestAsyncTool struct{ cb tools.AsyncCallback }

func (s *bgTestAsyncTool) Name() string        { return "bgtest" }
func (s *bgTestAsyncTool) Description() string { return "test async tool" }
func (s *bgTestAsyncTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (s *bgTestAsyncTool) SetCallback(cb tools.AsyncCallback) { s.cb = cb }
func (s *bgTestAsyncTool) Execute(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	return tools.AsyncResult("started in background")
}

func testBackgroundLoop(t *testing.T) *AgentLoop {
	t.Helper()
	al := testRoutineLoop(t)
	al.tools = tools.NewToolRegistry()
	al.sessions = session.NewSessionManager(session.NewJSONLStore(t.TempDir()), nil)
	return al
}

// PollBackground drains completions exactly once and files a system note
// per completion so the model continues from results on its next turn.
func TestPollBackgroundDeliversAndNotes(t *testing.T) {
	al := testBackgroundLoop(t)
	// Simulate a spawn lifecycle through the public registry surface.
	r := al.tools
	stub := &bgTestAsyncTool{}
	r.Register(stub)
	res := r.ExecuteWithContext(t.Context(), "bgtest", map[string]interface{}{"label": "dig"}, "cli", "c", "sess", nil)
	if res == nil || !res.Async || stub.cb == nil {
		t.Fatalf("stub must detach, got %+v", res)
	}
	running, done := al.PollBackground("sess")
	if len(running) != 1 || running[0].Label != "dig" {
		t.Fatalf("running wrong: %+v", running)
	}
	if len(done) != 0 {
		t.Fatalf("nothing finished yet: %+v", done)
	}
	stub.cb(t.Context(), &tools.ToolResult{ForLLM: "dug up", ForUser: "dug up"})
	running, done = al.PollBackground("sess")
	if len(running) != 0 {
		t.Fatal("completion must clear running")
	}
	if len(done) != 1 || !done[0].OK || done[0].Result != "dug up" {
		t.Fatalf("done wrong: %+v", done)
	}
	// Second poll: no double-report, but the note persists in history.
	if _, done := al.PollBackground("sess"); len(done) != 0 {
		t.Fatal("drain must be exactly-once")
	}
	found := false
	for _, m := range al.sessions.GetHistory("sess") {
		if m.Role == "system" && strings.Contains(m.Content, "dig") && strings.Contains(m.Content, "finished") {
			found = true
		}
	}
	if !found {
		t.Fatal("completion must persist a system history note for the model's next turn")
	}
}

// Failures are stated plainly in the note so the next turn recovers.
func TestPollBackgroundNotesFailure(t *testing.T) {
	al := testBackgroundLoop(t)
	r := al.tools
	stub := &bgTestAsyncTool{}
	r.Register(stub)
	r.ExecuteWithContext(t.Context(), "bgtest", map[string]interface{}{"label": "dig"}, "cli", "c", "sess", nil)
	stub.cb(t.Context(), &tools.ToolResult{ForLLM: "boom", IsError: true})
	_, done := al.PollBackground("sess")
	if len(done) != 1 || done[0].OK {
		t.Fatalf("failure must surface as not-OK: %+v", done)
	}
	found := false
	for _, m := range al.sessions.GetHistory("sess") {
		if m.Role == "system" && strings.Contains(m.Content, "failed") {
			found = true
		}
	}
	if !found {
		t.Fatal("failure must persist an honest history note")
	}
}
