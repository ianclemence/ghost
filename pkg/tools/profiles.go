package tools

import (
	"sort"
	"strings"
)

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

// coreToolNames are always available — the generic fallback + file + memory +
// system tools a turn can't run without. Keeping them always present means tool
// gating never starves a turn of the essentials.
var coreToolNames = map[string]bool{
	"exec": true, "read_file": true, "write_file": true, "append_file": true,
	"list_dir": true, "edit_file": true,
	"web_search": true, "web_fetch": true, "session_search": true,
	"remember": true, "context_get": true, "memory_curate": true,
	"message": true, "skill_manage": true, "todo": true, "cron": true,
	"spawn": true, "subagent": true, "clarify": true,
}

// turnIntentTools maps message keyword signals to niche tools to include so a
// turn only pays for tools it might actually use. A tool not in coreToolNames
// and not matched here is dropped for the turn, shrinking the surface the model
// reasons over (higher specificity = better selection + cheaper turns).
var turnIntentTools = []struct {
	keywords []string
	tools    []string
}{
	{[]string{"draw", "diagram", "flowchart", "mindmap", "canvas"}, []string{"canvas"}},
	{[]string{"image", "picture", "photo", "screenshot", "draw something"}, []string{"image_generate", "vision"}},
	{[]string{"video", "clip", "frames"}, []string{"video_frames"}},
	{[]string{"speak", "tts", "read aloud", "say this", "audio"}, []string{"tts"}},
	{[]string{"wake word", "voice wake", "listen"}, []string{"voicewake"}},
	{[]string{"sandbox", "isolate", "container"}, []string{"sandbox"}},
	{[]string{"network", "ping", "port", "dns", "wifi"}, []string{"networking"}},
	{[]string{"i2c", "spi", "gpio", "sensor", "pins", "hardware"}, []string{"spi"}},
	{[]string{"oracle", "ask oracle"}, []string{"oracle"}},
	{[]string{"mcp"}, []string{"mcp"}},
	{[]string{"lane", "template", "route"}, []string{"lanes"}},
	{[]string{"merge", "parallel", "batch"}, []string{"delegate_batch"}},
	{[]string{"compact", "summarize history", "context full"}, []string{"compaction"}},
	{[]string{"update ghost", "upgrade ghost", "self-update"}, []string{"update"}},
	{[]string{"pdf", "word", "excel", "document", "docx", "pptx"}, []string{"docparser"}},
	{[]string{"browser", "open page", "open url", "webpage"}, []string{"browser"}},
}

// FilterToolsForTurn narrows the tool surface to a core set plus any tools whose
// intent keywords appear in the user message (media present always allows vision).
func FilterToolsForTurn(registry *ToolRegistry, profile ToolProfile, userMsg string, hasMedia bool) *ToolRegistry {
	base := FilterRegistryByProfile(registry, profile)
	if base == nil {
		return NewToolRegistry()
	}
	lower := strings.ToLower(userMsg)

	include := func(name string) bool {
		if coreToolNames[name] {
			return true
		}
		for _, it := range turnIntentTools {
			if !stringInSlice(it.tools, name) {
				continue
			}
			for _, kw := range it.keywords {
				if strings.Contains(lower, kw) {
					return true
				}
			}
			// Vision tools are implied by media even without a keyword.
			if hasMedia && (name == "vision" || name == "image_generate") {
				return true
			}
		}
		return false
	}

	out := NewToolRegistry()
	for _, name := range base.List() {
		if include(name) {
			if tool, ok := base.Get(name); ok {
				out.Register(tool)
			}
		}
	}
	return out
}

func stringInSlice(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
