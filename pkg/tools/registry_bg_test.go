package tools

import (
	"context"
	"strings"
	"testing"
)

// bgStubTool is an async tool that captures its injected callback so the
// test fires completion manually, like a subagent finishing after the turn.
type bgStubTool struct {
	cb   AsyncCallback
	sync bool
}

func (s *bgStubTool) Name() string        { return "bgstub" }
func (s *bgStubTool) Description() string { return "test async tool" }
func (s *bgStubTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (s *bgStubTool) SetCallback(cb AsyncCallback) { s.cb = cb }
func (s *bgStubTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	if s.sync {
		return &ToolResult{ForLLM: "inline"}
	}
	return AsyncResult("started in background")
}

func TestRegistryTracksAsyncCompletion(t *testing.T) {
	r := NewToolRegistry()
	stub := &bgStubTool{}
	r.Register(stub)
	ctx := context.Background()
	res := r.ExecuteWithContext(ctx, "bgstub", map[string]interface{}{"label": "dig"}, "cli", "c", "sess", nil)
	if res == nil || !res.Async {
		t.Fatalf("stub must detach, got %+v", res)
	}
	// Gap-1 proof: no caller callback was passed, yet the registry installed
	// its recording wrapper — the tool got a callback anyway.
	if stub.cb == nil {
		t.Fatal("registry must inject a recording callback even when caller passes nil")
	}
	if running := r.BackgroundRunning("sess"); len(running) != 1 || running[0].Label != "dig" {
		t.Fatalf("running wrong: %+v", running)
	}
	stub.cb(ctx, &ToolResult{ForLLM: "dug up", ForUser: "dug up"})
	if len(r.BackgroundRunning("sess")) != 0 {
		t.Fatal("completion must clear running")
	}
	done := r.DrainBackgroundDone("sess")
	if len(done) != 1 || !done[0].OK || done[0].Result != "dug up" {
		t.Fatalf("done wrong: %+v", done)
	}
	if len(r.DrainBackgroundDone("sess")) != 0 {
		t.Fatal("drain must be exactly-once")
	}
}

func TestRegistryUntracksInlineAsyncTool(t *testing.T) {
	r := NewToolRegistry()
	stub := &bgStubTool{sync: true}
	r.Register(stub)
	res := r.ExecuteWithContext(context.Background(), "bgstub", nil, "cli", "c", "sess", nil)
	if res == nil || res.Async {
		t.Fatalf("stub must run inline, got %+v", res)
	}
	if len(r.BackgroundRunning("sess")) != 0 {
		t.Fatal("inline-finishing async-capable tools must not stick in running")
	}
}

func TestRegistryForwardsOuterCallback(t *testing.T) {
	r := NewToolRegistry()
	stub := &bgStubTool{}
	r.Register(stub)
	var outer []*ToolResult
	res := r.ExecuteWithContext(context.Background(), "bgstub", nil, "cli", "c", "sess",
		func(ctx context.Context, result *ToolResult) { outer = append(outer, result) })
	if res == nil || !res.Async || stub.cb == nil {
		t.Fatalf("stub must detach with wrapped callback, got %+v", res)
	}
	got := &ToolResult{ForLLM: "x"}
	stub.cb(context.Background(), got)
	if len(outer) != 1 || outer[0] != got {
		t.Fatal("caller callback must still run after recording")
	}
	if len(r.DrainBackgroundDone("sess")) != 1 {
		t.Fatal("recording must happen alongside the outer callback")
	}
}

// The lifecycle sink observes starts and finishes even though no caller
// callback was passed: the recording wrapper feeds it.
func TestRegistryEmitsBackgroundEvents(t *testing.T) {
	r := NewToolRegistry()
	var events []BackgroundEvent
	r.SetEventSink(func(ev BackgroundEvent) { events = append(events, ev) })
	stub := &bgStubTool{}
	r.Register(stub)
	res := r.ExecuteWithContext(context.Background(), "bgstub", map[string]interface{}{"label": "dig"}, "cli", "c", "sess", nil)
	if res == nil || !res.Async || stub.cb == nil {
		t.Fatalf("stub must detach, got %+v", res)
	}
	if len(events) != 1 || events[0].Type != BackgroundEventStarted || events[0].Label != "dig" || events[0].Session != "sess" {
		t.Fatalf("start event wrong: %+v", events)
	}
	stub.cb(context.Background(), &ToolResult{ForLLM: "dug up", ForUser: "dug up"})
	if len(events) != 2 || events[1].Type != BackgroundEventDone || !strings.Contains(events[1].Result, "dug up") {
		t.Fatalf("done event wrong: %+v", events)
	}
}
