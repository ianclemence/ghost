package tools

import (
	"context"
	"testing"
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
