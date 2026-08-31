package tools

import "sort"

type ToolProfile string

const (
	ProfileFull          ToolProfile = "full"
	ProfileMobileSafe    ToolProfile = "mobile-safe"
	ProfileHeartbeatSafe ToolProfile = "heartbeat-safe"
	ProfileCoding        ToolProfile = "coding"
	ProfileResearch      ToolProfile = "research"
	ProfileMinimal       ToolProfile = "minimal"
	ProfileAdmin         ToolProfile = "admin"
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
		"write_file", "append_file", "remember",
	},
	ProfileCoding: {
		"read_file", "write_file", "list_dir", "edit_file", "append_file",
		"search_files", "grep_search",
		"exec", "sandbox",
		"web_search", "web_fetch",
		"remember", "session_search",
		"spawn", "subagent",
	},
	ProfileResearch: {
		"read_file", "list_dir", "search_files", "grep_search",
		"web_search", "web_fetch",
		"browser_navigate", "browser_snapshot", "browser_click", "browser_type",
		"vision", "image_generate", "video_frames",
		"remember", "session_search",
	},
	ProfileMinimal: {
		"read_file", "list_dir",
		"web_search", "web_fetch",
		"remember",
	},
	ProfileAdmin: {
		"read_file", "write_file", "list_dir", "edit_file", "append_file",
		"search_files", "grep_search",
		"exec", "sandbox",
		"web_search", "web_fetch",
		"cron", "remember",
		"session_search",
		"spawn", "subagent", "batch_delegate",
		"skill_manage",
		"vision", "image_generate",
		"i2c", "spi",
		"compaction", "todo",
	},
	ProfileFull: nil,
}

var ProfileDescriptions = map[ToolProfile]string{
	ProfileFull:          "All tools available",
	ProfileMobileSafe:    "Safe subset for mobile access",
	ProfileHeartbeatSafe: "Minimal set for background tasks",
	ProfileCoding:        "File, shell, and search tools for coding",
	ProfileResearch:      "Web, browser, and media tools for research",
	ProfileMinimal:       "Basic read-only and search tools",
	ProfileAdmin:         "Full admin tools including hardware and delegation",
}

func ListProfiles() []ToolProfile {
	profiles := make([]ToolProfile, 0, len(ProfileAllowlists))
	for p := range ProfileAllowlists {
		profiles = append(profiles, p)
	}
	sort.Slice(profiles, func(i, j int) bool {
		return string(profiles[i]) < string(profiles[j])
	})
	return profiles
}

func GetProfileDescription(p ToolProfile) string {
	if desc, ok := ProfileDescriptions[p]; ok {
		return desc
	}
	return "Unknown profile"
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
