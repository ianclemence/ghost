package tools

type ToolProfile string

const (
	ProfileFull          ToolProfile = "full"
	ProfileMobileSafe    ToolProfile = "mobile-safe"
	ProfileHeartbeatSafe ToolProfile = "heartbeat-safe"
)

var ProfileAllowlists = map[ToolProfile][]string{
	ProfileMobileSafe: {
		"read_file", "search_files", "session_search",
		"view", "read", "grep_search",
		"web_search", "web_fetch",
		"sandbox", "exec",
	},
	ProfileHeartbeatSafe: {
		"read_file", "view", "session_search",
	},
	ProfileFull: nil,
}

func (p ToolProfile) Allows(toolName string) bool {
	allowlist, exists := ProfileAllowlists[p]
	if !exists || allowlist == nil {
		return true
	}
	for _, allowed := range allowlist {
		if allowed == toolName {
			return true
		}
	}
	return false
}

func FilterRegistryByProfile(registry *ToolRegistry, profile ToolProfile) *ToolRegistry {
	if registry == nil {
		return NewToolRegistry()
	}
	if profile == "" || profile == ProfileFull {
		return registry
	}
	filtered := NewToolRegistry()
	for _, name := range registry.List() {
		if !profile.Allows(name) {
			continue
		}
		if tool, ok := registry.Get(name); ok {
			filtered.Register(tool)
		}
	}
	return filtered
}
