package agent

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
)

// droppingStreamProvider streams half an answer and then loses the
// connection on its first call; the retry answers in full.
type droppingStreamProvider struct{ calls int }

func (p *droppingStreamProvider) Chat(ctx context.Context, m []providers.Message, t []providers.ToolDefinition, model string, o map[string]interface{}) (*providers.LLMResponse, error) {
	return p.StreamChat(ctx, m, t, model, o, func(string) {})
}

func (p *droppingStreamProvider) StreamChat(_ context.Context, _ []providers.Message, _ []providers.ToolDefinition, _ string, _ map[string]interface{}, onChunk func(string)) (*providers.LLMResponse, error) {
	p.calls++
	if p.calls == 1 {
		onChunk("Here are three names for your ca")
		return nil, errors.New("read tcp: connection reset by peer")
	}
	answer := "Here are three names for your cat: Miso, Pepper, and Juniper."
	onChunk(answer)
	return &providers.LLMResponse{Content: answer, FinishReason: "stop"}, nil
}

func (p *droppingStreamProvider) GetDefaultModel() string { return "test-model" }

// A connection that drops mid-stream is retried; the owner's live stream
// must carry the retry's answer once — never the broken fragment glued to
// the front of it.
func TestStreamRetryDoesNotConcatenateAttempts(t *testing.T) {
	prov := &droppingStreamProvider{}
	al := newTestAgentLoopWithProvider(t, t.TempDir(), prov)
	var streamed strings.Builder
	reply, err := al.ProcessDirectWithChannel(context.Background(), "Suggest names for my cat.", "stream-restart", "web", "chat", nil,
		func(s string) { streamed.WriteString(s) }, nil)
	if err != nil {
		t.Fatalf("turn failed: %v", err)
	}
	if prov.calls < 2 {
		t.Fatalf("expected a retry, got %d calls", prov.calls)
	}
	if !strings.Contains(reply, "Juniper") {
		t.Fatalf("reply = %q", reply)
	}
	if got := streamed.String(); strings.Count(got, "Here are three names") != 1 {
		t.Fatalf("live stream repeated the broken attempt: %q", got)
	}
}
