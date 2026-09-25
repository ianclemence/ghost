package agent

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/tools"
)

// failTool simulates a provider-backed tool failure whose result carries
// model-only guidance (exactly as providerError builds it).
type failTool struct{}

func (failTool) Name() string        { return "fake_fail" }
func (failTool) Description() string { return "test failure tool" }
func (failTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{},
	}
}
func (failTool) Execute(context.Context, map[string]interface{}) *tools.ToolResult {
	return &tools.ToolResult{
		ForLLM:  "That data source is temporarily unavailable. I'll try again shortly. (completion: failed; do not present fabricated data)",
		IsError: true,
	}
}

// The deterministic fast path answers the owner with NO model in between,
// so raw ForLLM — which may carry instructions addressed to the model —
// must never be shown. Regression: on weather failures the suffix
// "(completion: failed; do not present fabricated data)" was printed to
// the chat verbatim, twice, in under a second per turn.
func TestDeterministicToolErrorStripsModelGuidance(t *testing.T) {
	reg := tools.NewToolRegistry()
	reg.Register(failTool{})
	al := &AgentLoop{tools: reg}

	ans, ok := al.execDeterministicTool("fake_fail", nil, "s")
	if !ok {
		t.Fatalf("an error result must still be handled (honest failure ends the turn)")
	}
	if strings.Contains(ans, "fabricated") || strings.Contains(ans, "completion: failed") {
		t.Fatalf("model guidance leaked to the owner: %q", ans)
	}
	if !strings.Contains(ans, "temporarily unavailable") {
		t.Fatalf("product-language failure missing: %q", ans)
	}
}
