package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/skills"
	"github.com/ianclemence/ghost/pkg/tools"
)

// The deterministic fast paths exist to save an LLM call on an unambiguous
// ask. Every matcher below was a bare substring test, so a capability word
// appearing as a *topic* in someone else's sentence fired the fast path and
// answered a question nobody asked — the same defect that made a product-spec
// question ("...read room temperature ... in China") come back as a weather
// report. These tests pin both directions: topic mentions must fall through
// to the model, genuine asks must keep firing.

// --- stub tooling for the dispatch path -------------------------------

type aqiStubTool struct{ calls *[]map[string]interface{} }

func (a *aqiStubTool) Name() string        { return "aqi_now" }
func (a *aqiStubTool) Description() string { return "aqi stub for fast-path tests" }
func (a *aqiStubTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
}
func (a *aqiStubTool) Execute(_ context.Context, args map[string]interface{}) *tools.ToolResult {
	*a.calls = append(*a.calls, args)
	return tools.NewToolResult("AQI 42 Good (stub).")
}

// --- workspace fixtures -------------------------------------------------

// writeSkill creates a minimal installed skill so CheckReadiness can find it.
func writeSkill(t *testing.T, workspace, name string) {
	t.Helper()
	dir := filepath.Join(workspace, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	front := "---\nname: " + name + "\ndescription: fixture skill\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(front), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeDisabledSkill creates a skill in the disabled state.
func writeDisabledSkill(t *testing.T, workspace, name string) {
	t.Helper()
	dir := filepath.Join(workspace, "skills", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	front := "---\nname: " + name + "\ndescription: fixture skill\n---\n\n# " + name + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md.disabled"), []byte(front), 0o644); err != nil {
		t.Fatal(err)
	}
}

// --- the table: topic mentions must not classify as requests ------------

func TestFastPathsRejectTopicMentions(t *testing.T) {
	notRequests := []struct {
		capability string
		msg        string
		match      func(string) bool
	}{
		// isFlightIntent: bare "flight" inside a comparison/review sentence.
		{"flight", "i'm writing a blog post comparing flight booking sites, which one is cheapest", isFlightIntent},
		{"flight", "my friend books flight tickets for a living, is it a good job", isFlightIntent},
		{"flight", "design a flight controller board for a small drone, what specs", isFlightIntent},
		// isNearbyIntent: bare place nouns with no proximity ask.
		{"nearby", "i want to open a cafe, what equipment do i need to buy", isNearbyIntent},
		{"nearby", "i'm designing a restaurant brand identity, what colors work", isNearbyIntent},
		// isCalendarIntent: bare "calendar" as a product topic.
		{"calendar", "design a calendar app for doctors that shows shift schedules", isCalendarIntent},
		{"calendar", "how does a calendar app sync events in the background", isCalendarIntent},
		// isHassIntent: bare device nouns as shopping/explanation topics.
		{"hass", "which thermostat should i buy for the server rack and how do i wire it", isHassIntent},
		{"hass", "explain how home assistant works under the hood", isHassIntent},
		// AQI: same shape as the weather bug — sensor/spec sentences.
		{"aqi", "i'm building an air quality monitor for the pod, what sensor specs do i need in China", isAQIIntent},
		{"aqi", "what does an aqi sensor cost in bulk", isAQIIntent},
		// Shopping: a "how do I" question about the feature itself.
		{"shopping", "how do i add shopping list export to my notes app", isShoppingList},
		{"shopping", "why is my shopping list not syncing between devices", isShoppingList},
	}
	for _, c := range notRequests {
		if c.match(strings.ToLower(c.msg)) {
			t.Errorf("%s: is match on a topic mention: %q", c.capability, c.msg)
		}
	}

	requests := []struct {
		capability string
		msg        string
		match      func(string) bool
	}{
		// Genuine asks must keep firing: this trades nothing away.
		{"flight", "what's the flight status of TG123", isFlightIntent},
		{"flight", "status of flight TG123?", isFlightIntent},
		{"flight", "track my flight TG123", isFlightIntent},
		{"flight", "is my flight on time", isFlightIntent},
		{"flight", "flight", isFlightIntent},
		{"nearby", "coffee shops near me", isNearbyIntent},
		{"nearby", "find a coffee shop near Cebu City", isNearbyIntent},
		{"nearby", "cafes around New York today", isNearbyIntent},
		{"nearby", "restaurants near me", isNearbyIntent},
		{"nearby", "nearby", isNearbyIntent},
		{"calendar", "what's on my calendar today", isCalendarIntent},
		{"calendar", "do i have meetings", isCalendarIntent},
		{"calendar", "schedule a meeting for tomorrow", isCalendarIntent},
		{"calendar", "add an event to my calendar", isCalendarIntent},
		{"calendar", "calendar", isCalendarIntent},
		{"hass", "turn on the lights in the kitchen", isHassIntent},
		{"hass", "set the thermostat to 21 degrees", isHassIntent},
		{"hass", "is the front door locked", isHassIntent},
		{"hass", "trigger scene movie night", isHassIntent},
		{"aqi", "what's the air quality today", isAQIIntent},
		{"aqi", "air quality in Bangkok", isAQIIntent},
		{"aqi", "how's the air quality in Delhi right now", isAQIIntent},
		{"aqi", "aqi", isAQIIntent},
		{"shopping", "what's on my shopping list", isShoppingList},
		{"shopping", "show me my shopping list", isShoppingList},
		{"shopping", "read my shopping list", isShoppingList},
		{"shopping", "shopping list", isShoppingList},
	}
	for _, c := range requests {
		if !c.match(strings.ToLower(c.msg)) {
			t.Errorf("%s: genuine ask not matched: %q", c.capability, c.msg)
		}
	}
}

// The AQI branch of the dispatch path sat beside the weather branch with the
// same bare-word test. With a location phrase anywhere in the sentence it
// called aqi_now — geocoding "in bulk" as if it were a city, exactly like the
// weather bug geocoded "are you dumb".
func TestDispatchNeverCallsAQIOnSpecMention(t *testing.T) {
	var calls []map[string]interface{}
	reg := tools.NewToolRegistry()
	reg.Register(&aqiStubTool{calls: &calls})
	al := &AgentLoop{tools: reg, workspace: t.TempDir()}

	for _, msg := range []string{
		"i'm building an air quality monitor for the pod, what sensor specs do i need in China",
		"what does an aqi sensor cost in bulk",
	} {
		if ans, ok := al.tryDeterministicNetworkDispatch(msg, "s-aqi", nil); ok {
			t.Fatalf("spec mention dispatched deterministically: %q", ans)
		}
	}
	if len(calls) != 0 {
		t.Fatalf("aqi_now invoked %d time(s) for spec mentions: %+v", len(calls), calls)
	}

	if _, ok := al.tryDeterministicNetworkDispatch("what's the air quality in bangkok", "s-aqi-ok", nil); !ok {
		t.Fatal("a genuine air-quality ask must still take the fast path")
	}
	if len(calls) != 1 {
		t.Fatalf("genuine AQI ask must invoke the tool once, got %d", len(calls))
	}
}

// Readiness is the path that stores pending continuations — the mechanism
// that rewrote the owner's message into "... Location answer: are you dumb".
// A topic mention must neither answer nor leave a continuation behind.
func TestReadinessNeverAnswersTopicMentions(t *testing.T) {
	ws := t.TempDir()
	writeSkill(t, ws, "find-nearby")
	writeSkill(t, ws, "flight")
	writeSkill(t, ws, "calendar")
	al := &AgentLoop{workspace: ws}

	type cse struct{ tag, msg, session string }
	notRequests := []cse{
		{"nearby", "i want to open a cafe, what equipment do i need to buy", "s-nb"},
		{"flight", "i'm writing a blog post comparing flight booking sites, which one is cheapest", "s-fl"},
		{"calendar", "design a calendar app for doctors that shows shift schedules", "s-ca"},
	}
	for _, c := range notRequests {
		if ans, handled := al.tryReadinessFastPath(c.msg, c.session, map[string]string{}); handled {
			t.Errorf("%s: topic mention answered by readiness: %q", c.tag, ans)
		}
		if p, ok := skills.GetPendingDurable(ws, c.session); ok {
			t.Errorf("%s: topic mention stored a pending continuation: %+v", c.tag, p)
		}
	}

	// Positive control: the genuine ask still asks, durably, exactly once.
	ans, handled := al.tryReadinessFastPath("find a coffee shop near me", "s-nb-ok", map[string]string{})
	if !handled || ans == "" {
		t.Fatalf("genuine nearby ask must still be handled, got (%q, %v)", ans, handled)
	}
	if _, ok := skills.GetPendingDurable(ws, "s-nb-ok"); !ok {
		t.Fatal("genuine nearby ask must store its location continuation")
	}
}

// A disabled skill's name is an ordinary word ("recipe", "system", "network").
// Mentioning one inside a sentence about something else must not answer
// "The X skill is currently disabled" — only a message actually asking to use
// or about the skill may.
func TestDisabledSkillFastPathNeedsARequest(t *testing.T) {
	ws := t.TempDir()
	writeDisabledSkill(t, ws, "recipe")
	al := &AgentLoop{workspace: ws}

	mentions := []string{
		"what's your recipe for success with hardware projects",
		"i'm writing a blog post about recipe apps, what features matter",
		"design a recipe sharing app for my portfolio",
	}
	for _, m := range mentions {
		if ans, handled := al.tryDisabledSkillFastPath(m, "s-dis"); handled {
			t.Errorf("topic mention answered with the disabled-skill message: %q (msg=%q)", ans, m)
		}
	}

	// A genuine attempt to use it still gets the honest answer.
	if _, handled := al.tryDisabledSkillFastPath("use the recipe skill", "s-dis-ok"); !handled {
		t.Fatal("an actual request for a disabled skill must be answered")
	}
}

// tryDeterministicTurn answers with the shopping list whenever the message
// contains "shopping list" — its own guard ("...or the word 'list'") was
// always true inside that exact phrase, so any sentence containing it read the
// list back instead of answering.
func TestShoppingFastPathNeedsAnAsk(t *testing.T) {
	al := &AgentLoop{workspace: t.TempDir()}
	if ans, handled := al.tryDeterministicTurn("how do i add shopping list export to my notes app", "s-shop"); handled {
		t.Fatalf("a how-do-I question was answered with: %q", ans)
	}
	// The genuine list ask still answers instantly.
	if _, handled := al.tryDeterministicTurn("what's on my shopping list", "s-shop-ok"); !handled {
		t.Fatal("a genuine shopping-list ask must still be answered deterministically")
	}
}
