package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/providers"
)

// recordingDigestProvider records every system prompt it receives so tests can
// assert what was injected into the model and how often.
//
// It counts "turn calls" (Chat invocations that carry a system message) rather
// than every Chat invocation: the agent's async autoJournal goroutine also
// calls the provider for summarization, and those calls carry no system message
// and must not be mistaken for model turns. It optionally requests a tool call
// on a specific turn call.
type recordingDigestProvider struct {
	turnCalls        int
	systemPrompts    []string
	lastSystemPrompt string
	systemPerCall    []int
	toolCallOnTurn   int // trigger a tool call on this turn call (1-based); 0 = never
	toolArgs         map[string]interface{}
}

func (m *recordingDigestProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, opts map[string]interface{}) (*providers.LLMResponse, error) {
	hasSystem := false
	sysCount := 0
	for _, msg := range messages {
		if msg.Role != "system" {
			continue
		}
		hasSystem = true
		sysCount++
		m.systemPrompts = append(m.systemPrompts, msg.Content)
		m.lastSystemPrompt = msg.Content
	}
	if !hasSystem {
		return &providers.LLMResponse{Content: "ok", ToolCalls: []providers.ToolCall{}}, nil
	}
	m.turnCalls++
	m.systemPerCall = append(m.systemPerCall, sysCount)
	if m.toolCallOnTurn > 0 && m.turnCalls == m.toolCallOnTurn {
		return &providers.LLMResponse{
			Content:   "",
			ToolCalls: []providers.ToolCall{{ID: "call_1", Name: "mock_custom", Arguments: m.toolArgs}},
		}, nil
	}
	return &providers.LLMResponse{Content: "ok", ToolCalls: []providers.ToolCall{}}, nil
}

func (m *recordingDigestProvider) GetDefaultModel() string {
	return "mock-model"
}

func countDigestSections(prompt string) int {
	return strings.Count(prompt, "<personal_context>")
}

// newDigestWorkspace returns a temp workspace whose cleanup retries removal:
// the agent's async autoJournal goroutine can append to daily notes while the
// test finishes, so a single RemoveAll can race it.
func newDigestWorkspace(t *testing.T) string {
	t.Helper()
	ws, err := os.MkdirTemp("", "digest-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() {
		for i := 0; i < 100; i++ {
			if err := os.RemoveAll(ws); err == nil {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		os.RemoveAll(ws)
	})
	return ws
}

func digestBody(t *testing.T, prompt string) string {
	t.Helper()
	start := strings.Index(prompt, "<personal_context>")
	end := strings.Index(prompt, "</personal_context>")
	if start < 0 || end < start {
		t.Fatalf("digest section missing:\n%s", prompt)
	}
	return prompt[start+len("<personal_context>") : end]
}

// The Active Context Digest is injected exactly once into a normal model turn,
// after a declaration has been extracted in a previous turn.
func TestDigestInjectedOncePerTurn(t *testing.T) {
	ws := newDigestWorkspace(t)
	provider := &recordingDigestProvider{}
	al := newTestAgentLoopWithProvider(t, ws, provider)

	runTurn(t, al, "my favorite color is blue", "s1")
	runTurn(t, al, "what's my favorite color?", "s1")

	if provider.turnCalls != 2 {
		t.Fatalf("model turn calls = %d, want 2", provider.turnCalls)
	}
	prompt := provider.lastSystemPrompt
	if count := countDigestSections(prompt); count != 1 {
		t.Fatalf("digest injected %d times in turn 2 system prompt, want exactly 1", count)
	}
	if !strings.Contains(prompt, "- Favorite color: blue") {
		t.Fatalf("turn 2 digest missing favorite color:\n%s", prompt)
	}
}

// Empty Personal Context produces no digest section.
func TestDigestEmptyWhenNoContext(t *testing.T) {
	ws := newDigestWorkspace(t)
	provider := &recordingDigestProvider{}
	al := newTestAgentLoopWithProvider(t, ws, provider)

	runTurn(t, al, "hello there", "s1")

	if count := countDigestSections(provider.lastSystemPrompt); count != 0 {
		t.Fatalf("digest section present without any personal context:\n%s", provider.lastSystemPrompt)
	}
}

// The digest is not duplicated during tool iterations: one system message per
// model turn, one digest section in it, identical across the tool loop.
func TestDigestNotDuplicatedDuringToolIterations(t *testing.T) {
	ws := newDigestWorkspace(t)
	provider := &recordingDigestProvider{toolCallOnTurn: 2}
	al := newTestAgentLoopWithProvider(t, ws, provider)
	al.RegisterTool(&mockCustomTool{})

	runTurn(t, al, "my favorite color is blue", "s1")
	runTurn(t, al, "hello", "s1")

	if provider.turnCalls != 3 {
		t.Fatalf("model turn calls = %d, want 3 (tool request + tool result + final)", provider.turnCalls)
	}
	for i, count := range provider.systemPerCall {
		if count != 1 {
			t.Fatalf("turn call %d had %d system messages, want exactly 1", i+1, count)
		}
	}
	if len(provider.systemPrompts) != 3 {
		t.Fatalf("recorded %d system prompts, want 3", len(provider.systemPrompts))
	}
	// The digest is only present from the second turn onward (the first turn
	// extracts, the prompt is built before extraction runs).
	if count := countDigestSections(provider.systemPrompts[0]); count != 0 {
		t.Fatalf("turn 1 system prompt unexpectedly contains a digest")
	}
	if provider.systemPrompts[1] != provider.systemPrompts[2] {
		t.Fatal("system prompt changed across tool iterations")
	}
	for _, p := range provider.systemPrompts[1:] {
		if count := countDigestSections(p); count != 1 {
			t.Fatalf("digest injected %d times during tool iteration, want exactly 1", count)
		}
		if !strings.Contains(p, "- Favorite color: blue") {
			t.Fatalf("tool iteration prompt missing digest content:\n%s", p)
		}
	}
}

// The old unbounded MEMORY.md (and daily notes) content is no longer injected
// into the prompt.
func TestMemoryMdNoLongerInjected(t *testing.T) {
	ws := newDigestWorkspace(t)
	if err := os.MkdirAll(filepath.Join(ws, "memory"), 0755); err != nil {
		t.Fatalf("mkdir memory: %v", err)
	}
	legacyMarker := "SECRET_LEGACY_MEMORY_MARKER"
	dailyMarker := "SECRET_DAILY_NOTE_MARKER"
	if err := os.WriteFile(filepath.Join(ws, "memory", "MEMORY.md"), []byte(legacyMarker), 0644); err != nil {
		t.Fatalf("write MEMORY.md: %v", err)
	}
	today := time.Now().Format("20060102")
	monthDir := today[:6]
	notePath := filepath.Join(ws, "memory", monthDir, today+".md")
	if err := os.MkdirAll(filepath.Dir(notePath), 0755); err != nil {
		t.Fatalf("mkdir notes: %v", err)
	}
	if err := os.WriteFile(notePath, []byte(dailyMarker), 0644); err != nil {
		t.Fatalf("write daily note: %v", err)
	}

	provider := &recordingDigestProvider{}
	al := newTestAgentLoopWithProvider(t, ws, provider)
	runTurn(t, al, "hello", "s1")

	if strings.Contains(provider.lastSystemPrompt, legacyMarker) {
		t.Error("MEMORY.md content was injected into the prompt; the unbounded dump must be gone")
	}
	if strings.Contains(provider.lastSystemPrompt, dailyMarker) {
		t.Error("daily note content was injected into the prompt")
	}
}

// Full path: a declared fact flows into the next turn's digest without any
// context_get call.
func TestDigestFullPathDeclaration(t *testing.T) {
	ws := newDigestWorkspace(t)
	provider := &recordingDigestProvider{}
	al := newTestAgentLoopWithProvider(t, ws, provider)

	runTurn(t, al, "my favorite color is blue", "s1")
	runTurn(t, al, "what's my favorite color?", "s1")

	if !strings.Contains(provider.lastSystemPrompt, "- Favorite color: blue") {
		t.Fatalf("next turn digest missing the declared fact:\n%s", provider.lastSystemPrompt)
	}
}

// Full path with a correction: the digest must show the corrected value and
// never the superseded one.
func TestDigestFullPathCorrection(t *testing.T) {
	ws := newDigestWorkspace(t)
	provider := &recordingDigestProvider{}
	al := newTestAgentLoopWithProvider(t, ws, provider)

	runTurn(t, al, "my favorite color is blue", "s1")
	runTurn(t, al, "actually, it's green", "s1")
	runTurn(t, al, "what's my favorite color?", "s1")

	// Turn 2's prompt was built before the correction was extracted, so it
	// still reflects blue.
	if !strings.Contains(provider.systemPrompts[1], "- Favorite color: blue") {
		t.Fatalf("pre-correction turn missing blue:\n%s", provider.systemPrompts[1])
	}
	if !strings.Contains(provider.lastSystemPrompt, "- Favorite color: green") {
		t.Fatalf("post-correction digest missing green:\n%s", provider.lastSystemPrompt)
	}
	if strings.Contains(provider.lastSystemPrompt, "- Favorite color: blue") {
		t.Fatalf("post-correction digest still shows superseded blue:\n%s", provider.lastSystemPrompt)
	}
}

// The digest does not bring machine/runtime state into the prompt; only
// Personal Context entries appear inside the delimited section.
func TestDigestContainsOnlyPersonalContext(t *testing.T) {
	ws := newDigestWorkspace(t)
	provider := &recordingDigestProvider{}
	al := newTestAgentLoopWithProvider(t, ws, provider)

	runTurn(t, al, "my name is Ian and I live in Bangkok", "s1")
	runTurn(t, al, "hello", "s1")

	body := digestBody(t, provider.lastSystemPrompt)
	for _, forbidden := range []string{"heartbeat", "Ollama", "Last channel", "Status Healthy", "created_at", "confidence", "sources"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(forbidden)) {
			t.Errorf("digest body contains non-personal or internal data %q:\n%s", forbidden, body)
		}
	}
	if !strings.Contains(body, "- Name: Ian") || !strings.Contains(body, "- Location: Bangkok") {
		t.Fatalf("digest body missing expected personal entries:\n%s", body)
	}
}
