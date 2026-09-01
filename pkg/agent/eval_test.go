package agent

import (
	"context"
	"testing"

	"github.com/ianclemence/ghost/pkg/tools"
)

// —— Evaluation harness: encodes the representative behaviours Ghost must meet —
// each scenario asserts a deterministic decision (effort tier / tool gating)
// so regressions in routing are caught without needing a live model.

func TestEvalScenarioTriage(t *testing.T) {
	cases := []struct {
		name       string
		prompt     string
		wantEffort Effort
	}{
		{"simple-existing-fact", "what is my name", EffortFast},
		{"simple-who-am-i", "who am i", EffortFast},
		{"simple-location", "where do i live", EffortFast},
		{"skill-recipe", "find me a chicken pasta recipe", EffortDeliberate},
		{"shopping", "add milk and eggs to my shopping list", EffortDeliberate},
		{"preference-declaration", "I prefer tea over coffee", EffortDeliberate},
		{"identity-declaration", "my name is Sam and I live in Bangkok", EffortDeliberate},
		{"weather", "what is the weather in Bangkok", EffortDeliberate},
		{"research", "compare the energy efficiency of two heat pumps", EffortDeliberate},
		{"empty", "", EffortUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := classifyEffort(c.prompt); got != c.wantEffort {
				t.Errorf("classifyEffort(%q) = %v, want %v", c.prompt, got, c.wantEffort)
			}
		})
	}
}

// gatingTestTool is a minimal Tool for gating tests.
type gatingTestTool struct {
	name string
}

func (g gatingTestTool) Name() string        { return g.name }
func (g gatingTestTool) Description() string { return "stub" }
func (g gatingTestTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (g gatingTestTool) Execute(ctx context.Context, args map[string]interface{}) *tools.ToolResult {
	return tools.NewToolResult("ok")
}

// registerNames builds a registry containing tools under the given names, the
// way the agent's full registry does (subset for gating assertions).
func registerNames(names ...string) *tools.ToolRegistry {
	reg := tools.NewToolRegistry()
	for _, n := range names {
		reg.Register(gatingTestTool{name: n})
	}
	return reg
}

func TestEvalGatingAddsIntentTools(t *testing.T) {
	reg := registerNames("canvas", "vision", "image_generate", "web_search", "read_file", "session_search")

	// Media present → vision/image tools are available even without a keyword.
	withMedia := tools.FilterToolsForTurn(reg, tools.ProfileFull, "what color is this", true)
	if !containsTool(withMedia, "vision") || !containsTool(withMedia, "image_generate") {
		t.Fatalf("expected vision/image tools with media, got %v", withMedia.List())
	}

	// No media, no keyword → vision/image absent (no wasteful surface).
	noMedia := tools.FilterToolsForTurn(reg, tools.ProfileFull, "what color is this", false)
	if containsTool(noMedia, "vision") {
		t.Fatalf("expected no vision tool without media/keyword, got %v", noMedia.List())
	}

	// Explicit "draw" → canvas included.
	draw := tools.FilterToolsForTurn(reg, tools.ProfileFull, "can you draw a diagram", false)
	if !containsTool(draw, "canvas") {
		t.Fatalf("expected canvas for a draw request, got %v", draw.List())
	}
}

func TestEvalGatingKeepsCoreTools(t *testing.T) {
	reg := registerNames("exec", "read_file", "web_search", "session_search", "tts", "canvas")
	gated := tools.FilterToolsForTurn(reg, tools.ProfileFull, "hello", false)
	// Core tools always present.
	for _, core := range []string{"exec", "read_file", "web_search", "session_search"} {
		if !containsTool(gated, core) {
			t.Fatalf("expected core tool %q, got %v", core, gated.List())
		}
	}
	// Niche tool (tts) absent unless its keyword appears.
	if containsTool(gated, "tts") {
		t.Fatalf("expected no tts tool for 'hello', got %v", gated.List())
	}
	if containsTool(gated, "canvas") {
		t.Fatalf("expected no canvas tool for 'hello', got %v", gated.List())
	}
}

func containsTool(reg *tools.ToolRegistry, name string) bool {
	for _, n := range reg.List() {
		if n == name {
			return true
		}
	}
	return false
}
