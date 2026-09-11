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
