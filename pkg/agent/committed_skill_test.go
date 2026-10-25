package agent

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
)

func readSkillCall(path string) providers.Message {
	return providers.Message{
		Role: "assistant",
		ToolCalls: []providers.ToolCall{{
			ID: "c1", Type: "function",
			Function: &providers.FunctionCall{Name: "read_file", Arguments: `{"path":"` + path + `"}`},
		}},
	}
}

// A skill committed earlier in the SAME turn must remain committed even
// after many intervening messages (regression: the old fixed 6-message
// window let the restriction expire mid-turn).
func TestCommittedSkillIsTurnScoped(t *testing.T) {
	msgs := []providers.Message{{Role: "user", Content: "check the weather"}}
	msgs = append(msgs, readSkillCall("skills/weather/SKILL.md"))
	for i := 0; i < 12; i++ {
		msgs = append(msgs, providers.Message{Role: "tool", Content: "result"})
		msgs = append(msgs, providers.Message{Role: "assistant", Content: "ok"})
	}
	if got := committedSkill(msgs); got != "weather" {
		t.Fatalf("turn-scoped commitment must persist, got %q", got)
	}
}

// A skill read in a PREVIOUS turn must not remain committed in a new turn.
func TestCommittedSkillDoesNotLeakAcrossTurns(t *testing.T) {
	msgs := []providers.Message{
		{Role: "user", Content: "weather please"},
		readSkillCall("skills/weather/SKILL.md"),
		{Role: "assistant", Content: "done"},
		{Role: "user", Content: "now something else entirely"},
		{Role: "assistant", Content: "sure"},
	}
	if got := committedSkill(msgs); got != "" {
		t.Fatalf("commitment must not leak across turns, got %q", got)
	}
}

// With no user message at all, scanning is bounded to the whole slice.
func TestCommittedSkillNoUserMessage(t *testing.T) {
	msgs := []providers.Message{readSkillCall("skills/aqi/SKILL.md")}
	if got := committedSkill(msgs); got != "aqi" {
		t.Fatalf("expected aqi, got %q", got)
	}
}

// The live failure: while writing a Go test Ghost read the MCP SDK's examples,
// which say Name: "greet". The turn was committed to a skill called greet, and
// every later exec was rated high-impact under it and held for approval.
func TestSourceCodeThatSaysNameDoesNotCommitASkill(t *testing.T) {
	goSource := "func main() {\n\ts := mcp.NewServer(&mcp.Implementation{Name: \"greet\"}, nil)\n\tname: greet\n}\n"
	msgs := []providers.Message{
		{Role: "user", Content: "add a test for pkg/mcpclient"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "x1", Type: "function", Function: &providers.FunctionCall{Name: "exec", Arguments: `{"command":"sed -n 1,40p server.go"}`}}}},
		{Role: "tool", ToolCallID: "x1", Content: goSource},
	}
	if got := committedSkill(msgs); got != "" {
		t.Fatalf("source code committed the turn to a skill: %q", got)
	}
	// Even when it is a file read, only a real frontmatter block counts.
	msgs = []providers.Message{
		{Role: "user", Content: "add a test"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "r1", Type: "function", Function: &providers.FunctionCall{Name: "read_file", Arguments: `{"path":"projects/scout/server.go"}`}}}},
		{Role: "tool", ToolCallID: "r1", Content: goSource},
	}
	if got := committedSkill(msgs); got != "" {
		t.Fatalf("a source file read committed the turn to a skill: %q", got)
	}
}

func TestARealSkillReadStillCommits(t *testing.T) {
	skill := "---\nname: weather\ndescription: Weather lookups\n---\n# Weather\n"
	msgs := []providers.Message{
		{Role: "user", Content: "weather in Bangkok"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "r1", Type: "function", Function: &providers.FunctionCall{Name: "read_file", Arguments: `{"path":"/opt/other/location.md"}`}}}},
		{Role: "tool", ToolCallID: "r1", Content: skill},
	}
	if got := committedSkill(msgs); got != "weather" {
		t.Fatalf("a SKILL.md frontmatter read through read_file must commit, got %q", got)
	}
	// A YAML file shown by exec is not a skill.
	msgs = []providers.Message{
		{Role: "user", Content: "show the config"},
		{Role: "assistant", ToolCalls: []providers.ToolCall{{ID: "e1", Type: "function", Function: &providers.FunctionCall{Name: "exec", Arguments: `{"command":"cat config.yaml"}`}}}},
		{Role: "tool", ToolCallID: "e1", Content: "---\nname: myapp\nreplicas: 3\n---\n"},
	}
	if got := committedSkill(msgs); got != "" {
		t.Fatalf("exec output committed the turn to a skill: %q", got)
	}
}
