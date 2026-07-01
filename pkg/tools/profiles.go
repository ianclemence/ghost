package tools

type ToolProfile string

const (
	ProfileFull          ToolProfile = "full"
	ProfileMobileSafe    ToolProfile = "mobile-safe"
	ProfileHeartbeatSafe ToolProfile = "heartbeat-safe"
)

var ProfileAllowlists = map[ToolProfile][]string{
	ProfileMobileSafe: {
		"read_file", "write_file", "list_dir", "edit_file", "append_file",
		"search_files", "session_search", "grep_search",
		"view", "read",
		"web_search", "web_fetch",
		"sandbox", "exec",
		"cron", "remember",
		"vision", "image_generate",
	},
	ProfileHeartbeatSafe: {
		"read_file", "view", "session_search", "exec",
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
