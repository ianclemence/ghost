package tools

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
)

// scriptedProvider answers each Chat call from a fixed script, recording
// every message list it was shown. It lets a test drive the tool loop by
// hand: turn one asks for a tool, turn two answers.
type scriptedProvider struct {
	mu    sync.Mutex
	turns []*providers.LLMResponse
	seen  [][]providers.Message
	calls int
}

func (p *scriptedProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, options map[string]interface{}) (*providers.LLMResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.seen = append(p.seen, messages)
	if len(p.turns) == 0 {
		return &providers.LLMResponse{Content: "done"}, nil
	}
	r := p.turns[0]
	p.turns = p.turns[1:]
	return r, nil
}

func (p *scriptedProvider) GetDefaultModel() string { return "test-model" }
func (p *scriptedProvider) SupportsTools() bool     { return true }
func (p *scriptedProvider) GetContextWindow() int   { return 65536 }

func (p *scriptedProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

func (p *scriptedProvider) lastMessages() []providers.Message {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.seen) == 0 {
		return nil
	}
	return p.seen[len(p.seen)-1]
}

// toolThenAnswer script: ask for `tool` once, then finish.
func toolThenAnswer(tool string) []*providers.LLMResponse {
	return []*providers.LLMResponse{
		{ToolCalls: []providers.ToolCall{{
			ID:        "call-1",
			Name:      tool,
			Arguments: map[string]interface{}{"when": "tomorrow"},
		}}},
		{Content: "finished"},
	}
}

func newDurableManager(p providers.LLMProvider) *SubagentManager {
	sm := NewSubagentManager(p, "test-model", "/tmp/test-durable", nil)
	reg := NewToolRegistry()
	reg.Register(namedTool{name: "schedule"})
	sm.SetTools(reg)
	return sm
}

// A durable spawn records the task and hands execution to the runner; it
// must not start a goroutine of its own, because two executors means two
// runs of the same task after a restart.
func TestSpawnWithDurableSpawnerRecordsInsteadOfRunning(t *testing.T) {
	p := &scriptedProvider{}
	sm := newDurableManager(p)

	var got SpawnRequest
	sm.SetDurableSpawner(func(req SpawnRequest) (string, error) {
		got = req
		return "job-77", nil
	})

	out, err := sm.Spawn(context.Background(), "send the weekly report", "weekly", "cli", "direct", nil)
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if !strings.Contains(out, "job-77") {
		t.Fatalf("spawn result %q should name the recorded job", out)
	}
	if got.Task != "send the weekly report" || got.Label != "weekly" || got.Channel != "cli" || got.ChatID != "direct" {
		t.Fatalf("recorded request = %+v, want the task and its origin", got)
	}
	if p.callCount() != 0 {
		t.Fatalf("provider ran %d times; a recorded spawn must not execute here", p.callCount())
	}
	if _, err := sm.GetTaskLogs("job-77"); err != nil {
		t.Fatalf("recorded task should be visible for logging: %v", err)
	}
}

// Reaching a wall the owner has to clear ends the attempt and comes back as
// an approval wait — not as a failure to retry, which would spend the
// budget stopping at the same wall.
func TestRunDurableStopsAtApprovalWall(t *testing.T) {
	p := &scriptedProvider{turns: toolThenAnswer("schedule")}
	sm := newDurableManager(p)
	sm.ConsequentialAuth = func(ctx context.Context, tool string, args map[string]interface{}) *ToolResult {
		return &ToolResult{ForLLM: "I need you to approve scheduling this.", Err: ErrApprovalNeeded}
	}

	err := sm.RunDurable(context.Background(), DurableAttempt{
		JobID: "job-approve", Task: "schedule it", Label: "scheduling", Channel: "cli", ChatID: "direct",
	})
	w, ok := AsApprovalWait(err)
	if !ok {
		t.Fatalf("err = %v, want an approval wait", err)
	}
	if !strings.Contains(w.Reason, "approve") {
		t.Fatalf("wait reason = %q, want the ask the owner must answer", w.Reason)
	}
	task, err := sm.GetTaskLogs("job-approve")
	if err != nil {
		t.Fatalf("task should exist: %v", err)
	}
	_ = task
}

// A completed step is recorded as it happens, so a restart resumes from
// what actually occurred rather than repeating it.
func TestRunDurableRecordsConfirmedSteps(t *testing.T) {
	p := &scriptedProvider{turns: toolThenAnswer("schedule")}
	sm := newDurableManager(p)
	sm.ConsequentialAuth = func(ctx context.Context, tool string, args map[string]interface{}) *ToolResult {
		return nil // allowed
	}

	var checkpoints []string
	err := sm.RunDurable(context.Background(), DurableAttempt{
		JobID: "job-steps", Task: "schedule it", Label: "scheduling", Channel: "cli", ChatID: "direct",
		Checkpoint: func(line string) { checkpoints = append(checkpoints, line) },
		Evidence:   func(text string) {},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(checkpoints) != 1 || !strings.HasPrefix(checkpoints[0], "schedule") {
		t.Fatalf("checkpoints = %v, want one line naming the tool that ran", checkpoints)
	}
}

// Resuming tells the run what earlier attempts already confirmed, so it
// does not quietly do the same work twice.
func TestRunDurableResumesFromConfirmedSteps(t *testing.T) {
	p := &scriptedProvider{turns: toolThenAnswer("schedule")}
	sm := newDurableManager(p)
	sm.ConsequentialAuth = func(ctx context.Context, tool string, args map[string]interface{}) *ToolResult {
		return nil
	}

	err := sm.RunDurable(context.Background(), DurableAttempt{
		JobID: "job-resume", Task: "finish the report", Label: "report", Channel: "cli", ChatID: "direct",
		Resume: "schedule: scheduled the 9am send",
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	var prompt string
	for _, m := range p.lastMessages() {
		if m.Role == "user" && strings.Contains(m.Content, "finish the report") {
			prompt = m.Content
		}
	}
	if !strings.Contains(prompt, "already confirmed done") || !strings.Contains(prompt, "scheduled the 9am send") {
		t.Fatalf("resume prompt did not carry confirmed progress: %q", prompt)
	}
}

// failingTool is a governed tool whose execution fails, used to prove the
// checkpoint recorder only counts steps that actually happened.
type failingTool struct{ name string }

func (f failingTool) Name() string        { return f.name }
func (f failingTool) Description() string { return "always fails" }
func (f failingTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (f failingTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	return ErrorResult("it did not work")
}

// An ordinary failure is still a failure: the runner retries it, and the
// checkpoint recorder must not claim a step that errored.
func TestRunDurableDoesNotCheckpointFailedSteps(t *testing.T) {
	p := &scriptedProvider{turns: toolThenAnswer("schedule")}
	sm := newDurableManager(p)
	sm.ConsequentialAuth = func(ctx context.Context, tool string, args map[string]interface{}) *ToolResult {
		return nil
	}
	reg := NewToolRegistry()
	reg.Register(failingTool{name: "schedule"})
	sm.SetTools(reg)

	var checkpoints int
	if err := sm.RunDurable(context.Background(), DurableAttempt{
		JobID: "job-fail", Task: "do it", Label: "x", Channel: "cli", ChatID: "direct",
		Checkpoint: func(line string) { checkpoints++ },
	}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if checkpoints != 0 {
		t.Fatalf("recorded %d checkpoints for a step that never succeeded", checkpoints)
	}
}
