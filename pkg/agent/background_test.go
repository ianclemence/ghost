package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/bus"
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
	// Second poll: no double-report, and draining writes no history —
	// notes are filed once by the event sink at completion time.
	if _, done := al.PollBackground("sess"); len(done) != 0 {
		t.Fatal("drain must be exactly-once")
	}
	for _, m := range al.sessions.GetHistory("sess") {
		if m.Role == "system" {
			t.Fatalf("drain must not write history: %+v", m)
		}
	}
}

// The completion sink files one system note per finish so the model
// continues from results on its next turn, whichever surface drains.
func TestNoteBackgroundDone(t *testing.T) {
	al := testBackgroundLoop(t)
	al.noteBackgroundDone("sess", tools.BackgroundDone{
		BackgroundTask: tools.BackgroundTask{Label: "dig"},
		OK:             true, Result: "dug up", Elapsed: 5 * time.Second,
	})
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
	al.noteBackgroundDone("sess", done[0])
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

// Lifecycle events reach the bus with session routing so the WS bridge can
// forward them; findings travel in content, routing in metadata.
func TestPublishBackgroundEvent(t *testing.T) {
	mb := bus.NewMessageBus()
	ch, unsub := mb.SubscribeOutbound("test", false, 16)
	defer unsub()
	publishBackgroundEvent(mb, tools.BackgroundEvent{
		Type: tools.BackgroundEventDone, Session: "main",
		Label: "dig", Tool: "spawn", OK: true,
		Result: "dug up", ElapsedMs: 5000,
	})
	select {
	case msg := <-ch:
		if msg.Metadata["type"] != "background_done" {
			t.Fatalf("wrong event type: %+v", msg.Metadata)
		}
		if msg.Metadata["session_id"] != "main" || msg.Metadata["label"] != "dig" {
			t.Fatalf("routing wrong: %+v", msg.Metadata)
		}
		if msg.Content != "dug up" {
			t.Fatalf("findings must travel in content, got %q", msg.Content)
		}
	default:
		t.Fatal("no bus message published")
	}
	publishBackgroundEvent(nil, tools.BackgroundEvent{Type: tools.BackgroundEventStarted})
}
