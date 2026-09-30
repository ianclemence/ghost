package tools

import (
	"context"
	"testing"
	"time"
)

type testTool struct {
	name string
}

func (t testTool) Name() string        { return t.name }
func (t testTool) Description() string { return "test tool" }
func (t testTool) Parameters() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
	}
}
func (t testTool) Execute(ctx context.Context, args map[string]interface{}) *ToolResult {
	return UserResult("ok")
}

func TestToolProfileAllows(t *testing.T) {
	tests := []struct {
		name     string
		profile  ToolProfile
		toolName string
		allow    bool
	}{
		{"full allows any", ProfileFull, "shell", true},
		{"mobile allows read", ProfileMobileSafe, "read_file", true},
		{"mobile blocks shell", ProfileMobileSafe, "shell", false},
		{"heartbeat allows session_search", ProfileHeartbeatSafe, "session_search", true},
		{"heartbeat blocks grep_search", ProfileHeartbeatSafe, "grep_search", false},
		// Skill docs name these as the preferred path; the model must see
		// them on mobile or every skill call fails with "not available".
		{"mobile allows weather_now", ProfileMobileSafe, "weather_now", true},
		{"mobile allows places_nearby", ProfileMobileSafe, "places_nearby", true},
		{"mobile allows aqi_now", ProfileMobileSafe, "aqi_now", true},
		{"mobile allows crypto_price", ProfileMobileSafe, "crypto_price", true},
		{"mobile allows currency_convert", ProfileMobileSafe, "currency_convert", true},
		{"mobile allows flight_status", ProfileMobileSafe, "flight_status", true},
		{"mobile allows memory_recall", ProfileMobileSafe, "memory_recall", true},
		{"mobile allows context_get", ProfileMobileSafe, "context_get", true},
		{"mobile allows clarify", ProfileMobileSafe, "clarify", true},
		// Status questions (free disk space, device health) are answerable
		// on every surface without shell.
		{"mobile allows system_status", ProfileMobileSafe, "system_status", true},
		{"heartbeat allows system_status", ProfileHeartbeatSafe, "system_status", true},
		{"minimal allows system_status", ProfileMinimal, "system_status", true},
		{"mobile allows todo", ProfileMobileSafe, "todo", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.profile.Allows(tt.toolName); got != tt.allow {
				t.Fatalf("Allows(%q) = %v, want %v", tt.toolName, got, tt.allow)
			}
		})
	}
}

func TestFilterRegistryByProfile(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(testTool{name: "read_file"})
	reg.Register(testTool{name: "session_search"})
	reg.Register(testTool{name: "shell"})

	mobile := FilterRegistryByProfile(reg, ProfileMobileSafe)
	if mobile.Count() != 2 {
		t.Fatalf("expected 2 tools in mobile profile, got %d", mobile.Count())
	}
	if _, ok := mobile.Get("shell"); ok {
		t.Fatalf("shell should be filtered out for mobile profile")
	}

	full := FilterRegistryByProfile(reg, ProfileFull)
	if full.Count() != 3 {
		t.Fatalf("expected 3 tools in full profile, got %d", full.Count())
	}
}

// Hidden tools are never advertised: execution refuses them, so offering one
// invites a call that can only fail. A committed capability promotes its tools,
// which is what makes them both visible and executable.
func TestHiddenToolsAreNotOffered(t *testing.T) {
	reg := NewToolRegistry()
	reg.Register(testTool{name: "web_search"})
	reg.RegisterHidden(testTool{name: "exec"}, time.Hour)

	for _, p := range []ToolProfile{ProfileMobileSafe, ProfileFull} {
		got := FilterToolsForTurn(reg, p, "check imdb for a thriller", false)
		if _, ok := got.Get("exec"); ok {
			t.Fatalf("hidden exec must not be offered under profile %q", p)
		}
		if _, ok := got.Get("web_search"); !ok {
			t.Fatalf("web_search must still be offered under profile %q", p)
		}
	}

	reg.Promote("exec")
	got := FilterToolsForTurn(reg, ProfileMobileSafe, "", false)
	if _, ok := got.Get("exec"); !ok {
		t.Fatal("a promoted tool must be offered again")
	}
	if !reg.AllowedFor("exec", "mobile", "s1") {
		t.Fatal("a promoted tool must be executable, not just visible")
	}
}

func TestRegistryAllowedFor(t *testing.T) {
	r := NewToolRegistry()
	if !r.AllowedFor("exec", "terminal", "s1") {
		t.Error("with no policy, a tool is allowed by default")
	}
	r.SetToolEnabledForChannel("mobile", "exec", false)
	if r.AllowedFor("exec", "mobile", "s1") {
		t.Error("channel policy must be honored")
	}
	r.SetToolEnabledForSession("s2", "exec", false)
	if r.AllowedFor("exec", "terminal", "s2") {
		t.Error("session policy must be honored")
	}
}

// "Search Amazon for a keyboard" names no address and none of the old browser
// words; Ghost must still be handed a browser, or it tells the owner it can't
// browse. High-impact submit stays behind explicit checkout language.
func TestBrowserIsOfferedForNamedSitesAndShopping(t *testing.T) {
	reg := NewToolRegistry()
	for _, n := range append(browserMobileToolNames(), "browser_submit", "weather_now") {
		reg.Register(testTool{name: n})
	}
	offered := func(msg, tool string) bool {
		_, ok := FilterToolsForTurn(reg, ProfileFull, msg, false).Get(tool)
		return ok
	}
	for _, msg := range []string{
		"I want to buy a mechanical keyboard on amazon. search amazon for one under $100",
		"find me a cheap flight on google flights",
		"can you shop for running shoes",
		"check the price on ebay",
		"log in to my bank and tell me the balance",
	} {
		if !offered(msg, "browser_navigate") || !offered(msg, "browser_click") {
			t.Errorf("%q must be offered the browser", msg)
		}
		if offered(msg, "browser_submit") {
			t.Errorf("%q must NOT expose purchase-class submit without checkout language", msg)
		}
	}
	if !offered("place order for the keyboard on amazon", "browser_submit") {
		t.Error("explicit checkout language should expose submit (the broker still gates it)")
	}
	if offered("what is the weather like", "browser_navigate") {
		t.Error("an unrelated question must not be handed a browser")
	}
}
