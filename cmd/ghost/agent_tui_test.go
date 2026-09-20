package main

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/agent"
)

// fakeRuntime records calls and simulates a runtime for TUI tests.
type fakeRuntime struct {
	model    string
	presets  []string
	steering *agent.SteeringManager
	turns    []string
	setCalls []string
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{model: "deepseek-flash", presets: []string{"fast", "deep"}, steering: agent.NewSteeringManager()}
}

func (f *fakeRuntime) GetCurrentModel() string { return f.model }
func (f *fakeRuntime) ModelPresets() []string  { return f.presets }
func (f *fakeRuntime) SetModel(t string) error {
	f.setCalls = append(f.setCalls, t)
	f.model = t
	return nil
}
func (f *fakeRuntime) Steering() *agent.SteeringManager { return f.steering }
func (f *fakeRuntime) ProcessDirectWithChannel(ctx context.Context, content, sessionKey, channel, chatID string, media []string, onChunk func(string), onToolCall func(string, string)) (string, error) {
	f.turns = append(f.turns, content)
	if onChunk != nil {
		onChunk("ok")
	}
	return "ok", nil
}

func readyForTest(m *agentTUI) *agentTUI {
	m.width, m.height, m.ready = 80, 24, true
	m.layout()
	return m
}

func TestTUISlashHelpDoesNotSendTurn(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.runCommand("/help")
	if len(f.turns) != 0 {
		t.Fatalf("/help must not start a turn, got %v", f.turns)
	}
	if !hasNotice(m, "Commands") {
		t.Errorf("/help should add a help notice")
	}
}

func TestTUIUnknownCommandIsHonest(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.runCommand("/frobnicate")
	if !hasError(m, "unknown command") {
		t.Errorf("unknown command must be reported")
	}
}

func TestTUIModelCycleAndSet(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.cycleModel() // deepseek-flash -> first preset "fast"
	if f.model != "fast" {
		t.Errorf("cycle should pick first preset, got %q", f.model)
	}
	m.runCommand("/model deep")
	if f.model != "deep" {
		t.Errorf("/model deep should switch, got %q", f.model)
	}
}

func TestTUIQueueWhileWorkingInjectsSteering(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.working = true
	m.send("change of plan")
	if len(m.queued) != 1 || m.queued[0] != "change of plan" {
		t.Fatalf("working send must queue, got %v", m.queued)
	}
	if f.steering.PendingCount("cli:test") != 1 {
		t.Errorf("working send must inject a steering message")
	}
	if len(f.turns) != 0 {
		t.Errorf("working send must not start a new turn")
	}
}

func TestTUISendWhenIdleStartsTurn(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.send("hello")
	if !m.working {
		t.Errorf("sending while idle must mark working")
	}
	if !hasUser(m, "hello") {
		t.Errorf("user message must appear in transcript")
	}
}

func TestTUIStatusLineShowsLocalityAndState(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	line := m.statusLine()
	for _, want := range []string{"deepseek-flash", "cloud", "cli:test", "ready"} {
		if !strings.Contains(line, want) {
			t.Errorf("status line %q must contain %q", line, want)
		}
	}
	m.working = true
	m.toolCount = 3
	if !strings.Contains(m.statusLine(), "3 tools") {
		t.Errorf("working status must show tool count")
	}
}

func TestProviderLocality(t *testing.T) {
	cases := map[string]string{
		"deepseek-flash": "cloud",
		"gpt-4o":         "cloud",
		"qwen3:8b":       "local",
		"llama3":         "local",
		"mystery-model":  "pod",
	}
	for model, want := range cases {
		if got := providerLocality(model); got != want {
			t.Errorf("providerLocality(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestTUIHistoryRecall(t *testing.T) {
	f := newFakeRuntime()
	m := readyForTest(newAgentTUI(f, "cli:test"))
	m.history = []string{"one", "two"}
	m.input.Reset()
	m.recallHistory(-1)
	if m.input.Value() != "two" {
		t.Errorf("first ↑ should recall the last message, got %q", m.input.Value())
	}
	m.recallHistory(-1)
	if m.input.Value() != "one" {
		t.Errorf("second ↑ should recall the earlier message, got %q", m.input.Value())
	}
}

// helpers
func hasNotice(m *agentTUI, sub string) bool {
	for _, e := range m.entries {
		if e.kind == entryNotice && strings.Contains(e.text, sub) {
			return true
		}
	}
	return false
}
func hasError(m *agentTUI, sub string) bool {
	for _, e := range m.entries {
		if e.kind == entryError && strings.Contains(e.text, sub) {
			return true
		}
	}
	return false
}
func hasUser(m *agentTUI, sub string) bool {
	for _, e := range m.entries {
		if e.kind == entryUser && strings.Contains(e.text, sub) {
			return true
		}
	}
	return false
}
