package tools

import (
	"context"
	"testing"
)

// routeStubTool is a minimal Tool for routing tests.
type routeStubTool struct{ name string }

func (s routeStubTool) Name() string        { return s.name }
func (s routeStubTool) Description() string { return "stub" }
func (s routeStubTool) Parameters() map[string]interface{} {
	return map[string]interface{}{"type": "object"}
}
func (s routeStubTool) Execute(context.Context, map[string]interface{}) *ToolResult {
	return NewToolResult("ok")
}

// The routing preference must select the purpose-built capability and suppress
// the generic shell shortcut only when intent is unambiguous, and must never
// force a narrow route on ambiguous input or an unsafe phrasing.
func TestPreferredIntentRoute(t *testing.T) {
	clear := []struct {
		msg  string
		name string
		tool string
	}{
		{"what's on my schedule today?", "schedule", "schedule"},
		{"show me my upcoming reminders", "schedule", "schedule"},
		{"how much disk space is left?", "runtime_health", "system_status"},
		{"what do you remember about my sister?", "personal_memory", "memory_recall"},
		{"what did you just do?", "recent_activity", "system_status"},
	}
	for _, c := range clear {
		route, ok := PreferredIntentRoute(c.msg)
		if !ok {
			t.Errorf("%q must route to %s", c.msg, c.name)
			continue
		}
		if route.Name != c.name {
			t.Errorf("%q routed to %s, want %s", c.msg, route.Name, c.name)
		}
		if !stringInSlice(route.Tools, c.tool) {
			t.Errorf("%q route must offer %s, got %v", c.msg, c.tool, route.Tools)
		}
		if !stringInSlice(route.Exclude, "exec") {
			t.Errorf("%q must exclude the generic shell, got %v", c.msg, route.Exclude)
		}
	}

	ambiguous := []string{
		"help me plan my day",
		"can you take a look at this for me",
		"what should I do next",
		"summarise this document",
		"rm -rf the old logs in my schedule folder", // destructive phrasing, no clean route phrase
	}
	for _, msg := range ambiguous {
		if route, ok := PreferredIntentRoute(msg); ok {
			t.Errorf("%q must not force a narrow route, got %s", msg, route.Name)
		}
	}
}

// FilterToolsForTurn suppresses the generic shell for a high-confidence route
// and keeps the general surface for ambiguous input.
func TestFilterToolsForTurnSuppressesGenericForClearIntent(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(NewExecTool("", false))
	reg.Register(routeStubTool{name: "system_status"})
	reg.Register(routeStubTool{name: "web_search"})

	out := FilterToolsForTurn(reg, ProfileFull, "how much disk space is left?", false)
	if _, ok := out.Get("exec"); ok {
		t.Fatal("generic shell must be suppressed for an unambiguous runtime-health question")
	}
	if _, ok := out.Get("system_status"); !ok {
		t.Fatal("purpose-built system_status must be offered")
	}

	fallback := FilterToolsForTurn(reg, ProfileFull, "help me plan my day", false)
	if _, ok := fallback.Get("web_search"); !ok {
		t.Fatal("ambiguous input must retain the general tool surface")
	}
}
