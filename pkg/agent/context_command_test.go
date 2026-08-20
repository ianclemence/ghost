package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
)

// countingProvider records how many times the model was invoked so tests can
// prove /context never calls the LLM.
type countingProvider struct {
	calls int
}

func (m *countingProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, opts map[string]interface{}) (*providers.LLMResponse, error) {
	m.calls++
	return &providers.LLMResponse{Content: "ok", ToolCalls: []providers.ToolCall{}}, nil
}

func (m *countingProvider) GetDefaultModel() string {
	return "mock-model"
}

// /context is registered as a normal slash command and available through the
// real agent command executor.
func TestContextCommandRegistered(t *testing.T) {
	ws := newDigestWorkspace(t)
	al := newTestAgentLoopWithProvider(t, ws, &countingProvider{})

	if _, ok := al.commands.Lookup("/context"); !ok {
		t.Fatal("/context not registered in the agent command registry")
	}

	resp := runTurn(t, al, "/context", "s1")
	if !strings.Contains(resp, "Personal Context is empty.") {
		t.Fatalf("unexpected /context response: %q", resp)
	}
}

// Full path: a declared belief flows into /context, a correction updates it,
// and the superseded value never reappears. No model is invoked by the command.
func TestContextCommandEndToEnd(t *testing.T) {
	ws := newDigestWorkspace(t)
	provider := &countingProvider{}
	al := newTestAgentLoopWithProvider(t, ws, provider)

	runTurn(t, al, "my favorite color is blue", "s1")
	resp := runTurn(t, al, "/context", "s1")
	if !strings.Contains(resp, "- favorite_color: blue") {
		t.Fatalf("after declaration, /context missing blue:\n%s", resp)
	}
	if strings.Contains(resp, "green") {
		t.Fatalf("green leaked before the correction:\n%s", resp)
	}

	runTurn(t, al, "actually, it's green", "s1")
	resp = runTurn(t, al, "/context", "s1")
	if !strings.Contains(resp, "- favorite_color: green") {
		t.Fatalf("after correction, /context missing green:\n%s", resp)
	}
	if strings.Contains(resp, "- favorite_color: blue") {
		t.Fatalf("superseded blue still presented as current:\n%s", resp)
	}
}

// /context executes without invoking an LLM: the provider call count is
// unchanged by the command turn.
func TestContextCommandNoLLM(t *testing.T) {
	ws := newDigestWorkspace(t)
	provider := &countingProvider{}
	al := newTestAgentLoopWithProvider(t, ws, provider)

	// One normal turn seeds context and invokes the provider once.
	runTurn(t, al, "my favorite color is blue", "s1")
	callsAfterTurn := provider.calls
	if callsAfterTurn != 1 {
		t.Fatalf("provider calls after turn = %d, want 1", callsAfterTurn)
	}

	resp := runTurn(t, al, "/context", "s1")
	if provider.calls != callsAfterTurn {
		t.Fatalf("provider called %d times during /context, want %d (no LLM)", provider.calls, callsAfterTurn)
	}
	if !strings.Contains(resp, "- favorite_color: blue") {
		t.Fatalf("response missing context:\n%s", resp)
	}
}
