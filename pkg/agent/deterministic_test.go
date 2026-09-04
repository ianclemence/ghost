package agent

import (
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/providers"
	"github.com/ianclemence/ghost/pkg/skills"
)

func testMessagesWithSkillRead(args string) []providers.Message {
	return []providers.Message{
		{
			Role: "assistant",
			ToolCalls: []providers.ToolCall{
				{ID: "call_1", Type: "function", Function: &providers.FunctionCall{Name: "read_file", Arguments: args}},
			},
		},
	}
}

func TestParseShoppingItems(t *testing.T) {
	items := parseShoppingItems("Add milk and eggs to my shopping list")
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %v", items)
	}
}

func TestResolvePendingResume(t *testing.T) {
	skills.SetPending("sess-resume", skills.PendingContinuation{
		CapabilityID: "flight.status", Skill: "flight",
		MissingField: "flight_number", Question: "Which flight number?",
		OriginalTask: "What's my flight status?",
	})
	defer skills.ClearPending("sess-resume")

	resumed, ok := resolvePendingResume("sess-resume", "TG123")
	if !ok {
		t.Fatalf("expected resume for TG123")
	}
	if !strings.Contains(resumed, "TG123") || !strings.Contains(resumed, "flight") {
		t.Fatalf("resumed message missing context: %q", resumed)
	}

	// Full new task must not hijack.
	skills.SetPending("sess-resume2", skills.PendingContinuation{
		CapabilityID: "flight.status", Skill: "flight",
		MissingField: "flight_number", Question: "Which flight number?",
		OriginalTask: "What's my flight status?",
	})
	defer skills.ClearPending("sess-resume2")
	if _, ok := resolvePendingResume("sess-resume2", "Remind me tomorrow at 9 AM to call John about the quarterly report"); ok {
		t.Fatalf("long new task should not resume")
	}
	skills.ClearPending("sess-resume2")
}

func TestCapabilityInputs(t *testing.T) {
	inputs := capabilityInputsFromMessage("status of flight TG123?", nil)
	if inputs["flight_number"] == "" {
		t.Fatalf("expected flight_number extracted")
	}
	inputs = capabilityInputsFromMessage("weather in Bangkok?", nil)
	if inputs["location"] == "" {
		t.Fatalf("expected location extracted")
	}
}

func TestCommittedSkillGeneric(t *testing.T) {
	// No per-skill branches: any skills/<name>/SKILL.md path works.
	msgs := testMessagesWithSkillRead(`{"path":"skills/my-new-skill/SKILL.md"}`)
	if got := committedSkill(msgs); got != "my-new-skill" {
		t.Fatalf("expected my-new-skill, got %q", got)
	}
}
