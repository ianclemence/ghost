package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/providers"
)

// A model's preamble before it reaches for a tool is not the answer, and it is
// not in the stored reply either — so it must not reach the owner live.
// These tests hold the boundary both ways: the throat-clear is dropped, the
// answer is never lost.

func TestNarrationHoldDropsShortPreambleOnDiscard(t *testing.T) {
	var got []string
	h := newNarrationHold(func(s string) { got = append(got, s) })

	h.feed("I don't have a live news feed wired up for this, so let me search.")
	if len(got) != 0 {
		t.Fatalf("preamble emitted before the iteration settled: %q", got)
	}

	h.discard()
	if len(got) != 0 {
		t.Fatalf("preamble survived discard: %q", got)
	}
}

func TestNarrationHoldFlushesAnswerWhenThereWereNoTools(t *testing.T) {
	var got []string
	h := newNarrationHold(func(s string) { got = append(got, s) })

	const answer = "Kenya's headlines today are mostly politics."
	h.feed(answer)
	if len(got) != 0 {
		t.Fatalf("answer emitted before the iteration settled: %q", got)
	}

	h.flush()
	if len(got) != 1 || got[0] != answer {
		t.Fatalf("flushed = %#v, want exactly %q", got, answer)
	}

	// Flushing again must not duplicate what was already released.
	h.flush()
	if len(got) != 1 {
		t.Fatalf("second flush duplicated the answer: %#v", got)
	}
}

func TestNarrationHoldReleasesLongContentSoAnswerCanStream(t *testing.T) {
	var got []string
	h := newNarrationHold(func(s string) { got = append(got, s) })

	long := strings.Repeat("The Treasury put inflation at 4.2% for September. ", 6)
	h.feed(long)
	if len(got) == 0 {
		t.Fatal("content past the hold bound was never released; the answer would not stream")
	}
	if !strings.HasPrefix(strings.Join(got, ""), "The Treasury put") {
		t.Fatalf("released prefix = %q", strings.Join(got, ""))
	}

	// Already-sent text cannot be taken back, and must not be re-sent.
	h.discard()
	after := strings.Join(got, "")
	if strings.Count(after, "The Treasury put") != strings.Count(long, "The Treasury put") {
		t.Fatalf("discard altered already-released content")
	}
}

func TestNarrationHoldIsSafeWhenUnwired(t *testing.T) {
	var h *narrationHold
	h.feed("anything")
	h.flush()
	h.discard() // nil receiver: a turn with no stream must not panic
}

// scriptedNarratingProvider speaks the way a small model does: it announces
// what it is about to do, calls a tool, and only then answers.
type scriptedNarratingProvider struct {
	calls     int
	preamble  string
	answer    string
	toolCall  providers.ToolCall
	gotSystem bool
}

func (m *scriptedNarratingProvider) Chat(ctx context.Context, messages []providers.Message, tools []providers.ToolDefinition, model string, opts map[string]interface{}) (*providers.LLMResponse, error) {
	m.gotSystem = false
	for _, msg := range messages {
		if msg.Role == "system" {
			m.gotSystem = true
			break
		}
	}
	// Only conversation turns count: the agent's async journaling also calls
	// the provider, and letting it advance the script would clobber the turn.
	if !m.gotSystem {
		return &providers.LLMResponse{Content: "noted", ToolCalls: []providers.ToolCall{}}, nil
	}
	m.calls++
	if m.calls == 1 {
		return &providers.LLMResponse{
			Content:   m.preamble,
			ToolCalls: []providers.ToolCall{m.toolCall},
		}, nil
	}
	return &providers.LLMResponse{Content: m.answer, ToolCalls: []providers.ToolCall{}}, nil
}

func (m *scriptedNarratingProvider) GetDefaultModel() string { return "mock-model" }

func TestProcessMessageDoesNotStreamThePreambleBeforeAToolCall(t *testing.T) {
	ws := t.TempDir()
	const preamble = "I don't have a live news feed wired up for this, so let me search."
	const answer = "Kenya's headlines today are mostly politics, money and security."

	provider := &scriptedNarratingProvider{
		preamble: preamble,
		answer:   answer,
		toolCall: providers.ToolCall{
			ID:   "call_1",
			Name: "context_get",
			Arguments: map[string]interface{}{
				"predicate": "fact/work",
			},
		},
	}
	cfg := &config.Config{
		Agents: config.AgentsConfig{
			Defaults: config.AgentDefaults{
				Workspace:         ws,
				Model:             "test-model",
				MaxTokens:         4096,
				MaxToolIterations: 10,
			},
		},
	}
	al, _ := NewAgentLoop(cfg, bus.NewMessageBus(), provider)
	t.Cleanup(func() { al.Stop() })

	var streamed []string
	msg := bus.InboundMessage{
		Channel:    "test",
		SenderID:   "user1",
		ChatID:     "chat1",
		Content:    "What is happening in Kenya?",
		SessionKey: "narration-hold-1",
	}
	resp, err := al.processMessage(context.Background(), msg,
		func(chunk string) { streamed = append(streamed, chunk) }, nil)
	if err != nil {
		t.Fatalf("processMessage: %v", err)
	}
	if provider.calls < 2 {
		t.Fatalf("scripted provider calls = %d, want the tool path to run", provider.calls)
	}

	live := strings.Join(streamed, "")
	if strings.Contains(live, "live news feed") {
		t.Errorf("preamble before a tool call reached the owner live: %q", live)
	}
	if !strings.Contains(live, "Kenya's headlines today") {
		t.Errorf("answer missing from the live stream: %q", live)
	}
	if resp != answer {
		t.Errorf("reply = %q, want %q", resp, answer)
	}
}

// A failed attempt's text must never be glued onto the retry's answer.
func TestNarrationHoldAbandonAttempt(t *testing.T) {
	var out strings.Builder
	h := newNarrationHold(func(s string) { out.WriteString(s) })
	h.feed("Sure, here are some cat na") // attempt 1, still held
	h.abandonAttempt()
	h.feed("Sure, here are some cat names: Miso, Pepper.")
	h.flush()
	if got := out.String(); got != "Sure, here are some cat names: Miso, Pepper." {
		t.Fatalf("held partial leaked into the retry: %q", got)
	}

	out.Reset()
	h = newNarrationHold(func(s string) { out.WriteString(s) })
	long := strings.Repeat("word ", 40) // past the flush threshold: owner saw it
	h.feed(long)
	h.abandonAttempt()
	h.feed("Fresh answer.")
	if got := out.String(); !strings.Contains(got, attemptRestartNotice) || !strings.HasSuffix(got, "Fresh answer.") {
		t.Fatalf("released partial must be marked before the restart: %q", got)
	}
}
