package agent

import (
	"os"
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

	resumed, ok, _, _ := resolvePendingResume("", "sess-resume", "TG123")
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
	if _, ok, _, _ := resolvePendingResume("", "sess-resume2", "Remind me tomorrow at 9 AM to call John about the quarterly report"); ok {
		t.Fatalf("long new task should not resume")
	}
	skills.ClearPending("sess-resume2")
}

func TestResolvePendingDurableAcrossProcess(t *testing.T) {
	ws := t.TempDir()
	// Process A: deterministic weather fast-path sets a durable pending.
	skills.SetPendingDurable(ws, "sess-dup", skills.PendingContinuation{
		CapabilityID: "weather.current", Skill: "weather",
		MissingField: "location", Question: "Which city should I check?",
		OriginalTask: "What's the weather like?",
	})
	// Process B (fresh, empty in-memory map): the short answer resumes.
	resumed, ok, field, answer := resolvePendingResume(ws, "sess-dup", "Bangkok")
	if !ok {
		t.Fatalf("expected durable resume for Bangkok")
	}
	if field != "location" || answer != "Bangkok" {
		t.Fatalf("structured resume wrong: field=%q answer=%q", field, answer)
	}
	if !strings.Contains(resumed, "Bangkok") || !strings.Contains(resumed, "weather") {
		t.Fatalf("resumed message missing context: %q", resumed)
	}
	// Exact-once: after completion the durable request is consumed, so a
	// second short answer must NOT resume the same original task.
	if _, ok, _, _ := resolvePendingResume(ws, "sess-dup", "Phuket"); ok {
		t.Fatalf("durable pending must resume exactly once")
	}
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
	if got := capabilityInputsFromMessage("What is the weather in Bangkok right now?", nil); got["location"] != "Bangkok" {
		t.Fatalf("'right now' must trim cleanly, got %q", got["location"])
	}
	if got := locationFromText("what's the weather in chiang mai tomorrow?"); got != "chiang mai" {
		t.Fatalf("'tomorrow' must trim, got %q", got)
	}
}

func TestCommittedSkillGeneric(t *testing.T) {
	// No per-skill branches: any skills/<name>/SKILL.md path works.
	msgs := testMessagesWithSkillRead(`{"path":"skills/my-new-skill/SKILL.md"}`)
	if got := committedSkill(msgs); got != "my-new-skill" {
		t.Fatalf("expected my-new-skill, got %q", got)
	}
}

func TestSkillToggleName(t *testing.T) {
	// Uses a temp workspace with fake skills.
	dir := t.TempDir()
	for _, n := range []string{"ascii-art", "weather"} {
		os.MkdirAll(dir+"/skills/"+n, 0755)
		os.WriteFile(dir+"/skills/"+n+"/SKILL.md", []byte("x"), 0644)
	}
	if got := skillToggleName(dir, "ascii art"); got != "ascii-art" {
		t.Fatalf("expected ascii-art, got %q", got)
	}
	if got := skillToggleName(dir, "the weather skill"); got != "weather" {
		t.Fatalf("expected weather, got %q", got)
	}
	if got := skillToggleName(dir, "the lights"); got != "" {
		t.Fatalf("must not guess, got %q", got)
	}
}

func TestSetSkillEnabledLocal(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(dir+"/skills/demo", 0755)
	os.WriteFile(dir+"/skills/demo/SKILL.md", []byte("x"), 0644)
	ok, msg := setSkillEnabledLocal(dir, "demo", false)
	if !ok || msg == "" {
		t.Fatalf("disable failed: %v %q", ok, msg)
	}
	if _, err := os.Stat(dir + "/skills/demo/SKILL.md"); err == nil {
		t.Fatalf("SKILL.md should be renamed")
	}
	ok, _ = setSkillEnabledLocal(dir, "demo", true)
	if !ok {
		t.Fatalf("re-enable failed")
	}
	if _, err := os.Stat(dir + "/skills/demo/SKILL.md"); err != nil {
		t.Fatalf("SKILL.md should be back")
	}
}

// TestResumeNotReasked guards the routing fix: after a durable
// clarification resume rewrites the message to carry the answer, the
// readiness fast-path must NOT re-ask (which would mint a second
// pending and lose the answer).
func TestResumeNotReaskedByReadiness(t *testing.T) {
	ws := t.TempDir()
	skills.SetPendingDurable(ws, "sess-w2", skills.PendingContinuation{
		CapabilityID: "weather.current", Skill: "weather",
		MissingField: "location", Question: "Which city should I check?",
		OriginalTask: "What's the weather?",
	})
	resumed, ok, field, answer := resolvePendingResume(ws, "sess-w2", "Bangkok")
	if !ok {
		t.Fatalf("expected resume")
	}
	// The rewritten message must not re-trigger the weather re-ask.
	if strings.Contains(strings.ToLower(resumed), "which city should i check") {
		t.Fatalf("resumed message still asks for the city: %q", resumed)
	}
	if field != "location" || answer != "Bangkok" {
		t.Fatalf("structured resume wrong: field=%q answer=%q", field, answer)
	}
	// And the durable pending is consumed exactly once.
	if _, ok, _, _ := resolvePendingResume(ws, "sess-w2", "Tokyo"); ok {
		t.Fatalf("resume must happen exactly once")
	}
}

func TestNetworkDispatchHonestForecast(t *testing.T) {
	al := &AgentLoop{}
	// Forecast ask must be answered honestly, never by the model inventing
	// numbers, and never dispatched to the current-conditions tool.
	ans, ok := al.tryDeterministicNetworkDispatch("What's the weather tomorrow in Bangkok?", "s", nil)
	if !ok {
		t.Fatalf("forecast ask must be handled deterministically")
	}
	if !strings.Contains(ans, "forecast") {
		t.Fatalf("honest limitation missing: %q", ans)
	}
}

func TestNetworkDispatchUnwiredFallsThrough(t *testing.T) {
	// No tools registry (unit-test loop): the dispatch must NOT panic and
	// must NOT claim success; it falls through to the agent.
	al := &AgentLoop{}
	ans, ok := al.tryDeterministicNetworkDispatch("What's the current weather in Bangkok?", "s", nil)
	if ok {
		t.Fatalf("unwired loop must not handle: %q", ans)
	}
}

func TestCapabilityInputsReadsResumeAnswer(t *testing.T) {
	md := map[string]string{"resume_field": "location", "resume_answer": "Bangkok"}
	inputs := capabilityInputsFromMessage("What's the weather?", md)
	if inputs["location"] != "Bangkok" {
		t.Fatalf("resume answer must feed location: %+v", inputs)
	}
	// Real device location still wins over resume.
	md2 := map[string]string{"city": "Chiang Mai", "resume_field": "location", "resume_answer": "Bangkok"}
	if got := capabilityInputsFromMessage("w", md2); got["location"] != "Chiang Mai" {
		t.Fatalf("device location must win: %+v", got)
	}
}

// TestResumeDoesNotHijackProposals guards the shared-durable-store bug:
// routine/standing proposals wait on "yes"/"no" and must be confirmed by
// their own fast-paths, never rewritten as clarification answers.
func TestResumeDoesNotHijackProposals(t *testing.T) {
	ws := t.TempDir()
	// A standing-permission proposal (no MissingField set).
	skills.SetPendingDurable(ws, "sess-g", skills.PendingContinuation{
		CapabilityID: "permission.standing", Skill: "permissions",
		MissingField: "", Question: "Ghost will be allowed to add calendar events for you. Nothing else changes. Say yes to confirm.",
		OriginalTask: "You can always add calendar events for me",
	})
	if _, ok, _, _ := resolvePendingResume(ws, "sess-g", "yes"); ok {
		t.Fatal("standing proposal must not be hijacked as a clarification")
	}
	// A routine proposal likewise.
	skills.SetPendingDurable(ws, "sess-g2", skills.PendingContinuation{
		CapabilityID: "routine.create", Skill: "routines",
		MissingField: "", Question: "I'll remind you to review finances every Monday morning. Say yes to confirm.",
		OriginalTask: "Every Monday morning remind me to review my finances",
	})
	if _, ok, _, _ := resolvePendingResume(ws, "sess-g2", "yes"); ok {
		t.Fatal("routine proposal must not be hijacked as a clarification")
	}
}
