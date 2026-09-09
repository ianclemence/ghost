package tools

import (
	"testing"
)

// BenchmarkCapabilityDiscovery measures the per-turn tool-surface narrowing
// (profile + intent keyword filtering) the runtime applies before a model
// sees its capabilities. Discovery must stay cheap enough to run every turn.
func BenchmarkCapabilityDiscovery(b *testing.B) {
	reg := NewToolRegistry()
	for _, name := range []string{
		"exec", "read_file", "write_file", "list_dir", "append_file", "edit_file",
		"web_search", "web_fetch", "session_search", "remember", "context_get",
		"memory_curate", "message", "skill_manage", "todo", "cron", "schedule",
		"spawn", "subagent", "clarify", "memory_recall", "vision", "image_generate",
		"computer_inspect_ui", "computer_screenshot", "computer_click",
		"computer_type", "computer_press_key",
		"browser_navigate", "browser_snapshot", "browser_click", "browser_type", "browser_press",
	} {
		reg.Register(&stubTool{name: name})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		got := FilterToolsForTurn(reg, ProfileFull, "open the settings window on the computer and change the name", false)
		if got == nil || len(got.List()) == 0 {
			b.Fatal("empty discovery")
		}
	}
}
