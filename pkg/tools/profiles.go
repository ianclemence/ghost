package tools

import (
	"regexp"
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
		"system_status",
		"read_file", "write_file", "list_dir", "edit_file", "append_file",
		"search_files", "session_search", "grep_search",
		"view", "read",
		"web_search", "web_fetch",
		"sandbox", "exec",
		"schedule", "remember",
		"vision", "image_generate",
		// Read-only skill primaries: the skill docs tell the model to call
		// these first. Without them the model follows the doc, calls the
		// tool, and gets "not available in profile" — then falls back to
		// heavier paths or fails. All are read-only; the broker still
		// governs anything consequential.
		"weather_now", "places_nearby", "aqi_now", "crypto_price",
		"currency_convert", "flight_status",
		// Connected-app read primaries (email_search, code_search,
		// docs_search): read-only, refuse honestly when unconnected.
		// email_send stays out of the mobile profile (explicit approval
		// on full profile). media_play is in: phone playback is the point,
		// and the broker still governs it.
		"email_search", "code_search", "docs_search", "media_play",
		// Standing goals are user-manageable local state.
		"goal",
		// Read-only memory + conversation tools.
		"memory_recall", "context_get", "clarify", "todo",
		// Device control and handoff are legitimate mobile actions; the
		// broker still governs the consequential ones.
		"device", "calendar", "publish_artifact", "doc_parser",
		// Browser surface on mobile: observe + act (broker-governed like
		// exec, which is already here). Purchase-class submit and
		// file-egress upload stay desktop/admin posture.
	},
	ProfileHeartbeatSafe: {
		"system_status",
		"read_file", "view", "session_search", "exec",
		"write_file", "append_file", "remember",
	},
	ProfileCoding: {
		"system_status",
		"read_file", "write_file", "list_dir", "edit_file", "append_file",
		"search_files", "grep_search",
		"exec", "sandbox",
		"web_search", "web_fetch",
		"remember", "session_search",
		"spawn", "subagent",
		// Browser for local/dev page debugging: "why is my page broken"
		// answers come from console + network + a11y without new
		// authority (observe-class) and broker-governed interaction.
		// No submit: purchases are not a coding action.
	},
	ProfileResearch: {
		"system_status",
		"read_file", "list_dir", "search_files", "grep_search",
		"web_search", "web_fetch",
		"vision", "image_generate", "video_frames",
		"remember", "session_search",
		// Browser set registered below (browserToolNames minus submit).
	},
	ProfileMinimal: {
		"system_status",
		"read_file", "list_dir",
		"web_search", "web_fetch",
		"remember",
	},
	ProfileAdmin: {
		"system_status",
		"read_file", "write_file", "list_dir", "edit_file", "append_file",
		"search_files", "grep_search",
		"exec", "sandbox",
		"web_search", "web_fetch",
		"schedule", "remember",
		"session_search",
		"spawn", "subagent", "batch_delegate",
		"skill_manage", "browser_submit",
		"vision", "image_generate",
		"i2c", "spi", "device", "publish_artifact",
		"compaction", "compact_context", "todo", "doc_parser",
	},
	ProfileFull: nil,
}

// Browser tool surfaces by class. pkg/agent/browser_gate.go (whitelist
// + risk) must agree with these sets; TestBrowserGovernanceConsistency
// pins the two together so a tool can never ship visible-but-ungated.
var (
	// browserObserveToolNames read the page (or move the viewport) and
	// change nothing the broker cares about.
	browserObserveToolNames = []string{
		"browser_navigate", "browser_snapshot", "browser_wait", "browser_find",
		"browser_screenshot", "browser_scroll", "browser_console",
		"browser_network", "browser_a11y",
	}
	// browserActToolNames drive the page; the broker decides each call.
	browserActToolNames = []string{
		"browser_click", "browser_type", "browser_press", "browser_fill",
		"browser_fill_form", "browser_select", "browser_check", "browser_hover",
		"browser_drag", "browser_dialog", "browser_download",
	}
	// browserHighToolNames are high impact (never auto-authorized):
	// purchase-class submit, and upload (local files leave the device).
	browserHighToolNames = []string{"browser_submit", "browser_upload"}
)

func concatNames(groups ...[]string) []string {
	var out []string
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// browserMobileToolNames: the mobile-safe browser posture — observe +
// act under the broker, no submit/upload.
func browserMobileToolNames() []string {
	return concatNames(browserObserveToolNames, browserActToolNames)
}

// Profile membership for the shared browser sets: mobile and research
// get observe+act (broker-governed), coding additionally gets upload
// (no new authority beside the exec it already has; submit stays
// admin/full posture). Heartbeat and minimal stay browser-free.
func init() {
	ProfileAllowlists[ProfileMobileSafe] = append(ProfileAllowlists[ProfileMobileSafe], browserMobileToolNames()...)
	codingBrowser := concatNames(browserMobileToolNames(), []string{"browser_upload"})
	ProfileAllowlists[ProfileCoding] = append(ProfileAllowlists[ProfileCoding], codingBrowser...)
	ProfileAllowlists[ProfileResearch] = append(ProfileAllowlists[ProfileResearch], codingBrowser...)
}

var ProfileDescriptions = map[ToolProfile]string{
	ProfileFull:          "All tools available",
	ProfileMobileSafe:    "Safe subset for mobile access",
	ProfileHeartbeatSafe: "Minimal set for background tasks",
	ProfileCoding:        "File, shell, search, and browser tools for coding",
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
	filtered := NewToolRegistry()
	for _, name := range registry.List() {
		// Hidden tools are never offered: execution refuses them (isAllowed),
		// so advertising one invites a call that can only fail — the
		// broken-promise bug where an approval was granted, then refused.
		if !registry.visibleNow(name) {
			continue
		}
		if profile != "" && profile != ProfileFull && !profile.Allows(name) {
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
	"memory_recall": true,
	// Read-only skill reference tools: always visible so skill docs that name
	// them as the preferred path actually work. Cheap, no side effects.
	"weather_now": true, "places_nearby": true, "aqi_now": true,
	"crypto_price": true, "currency_convert": true, "flight_status": true,
	"clarify": true, "todo": true,
	"message": true, "skill_manage": true,
	"schedule": true,
	"spawn":    true, "subagent": true,
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
	{[]string{"wake word", "voice wake", "listen"}, []string{"voice_wake"}},
	{[]string{"sandbox", "isolate", "container"}, []string{"sandbox"}},
	{[]string{"network", "ping", "port", "dns", "wifi"}, []string{"networking"}},
	{[]string{"i2c", "spi", "gpio", "sensor", "pins", "hardware"}, []string{"spi", "i2c"}},
	{[]string{"oracle", "ask oracle"}, []string{"oracle"}},
	{[]string{"lane", "template", "route"}, []string{"switch_lane"}},
	{[]string{"merge", "parallel", "batch"}, []string{"batch_delegate"}},
	{[]string{"compact", "summarize history", "context full"}, []string{"compact_context"}},
	{[]string{"update ghost", "upgrade ghost", "self-update"}, []string{"update"}},
	{[]string{"pdf", "word", "excel", "document", "docx", "pptx"}, []string{"doc_parser"}},
	// Home Assistant device control (broker-gated, consequential).
	{[]string{"light", "lights", "thermostat", "home assistant", "smart home", "turn on", "turn off", "turn the", "device", "scene"}, []string{"device"}},
	// Durable handoff artifacts (low risk, evidence-backed).
	{[]string{"artifact", "hand off", "handoff", "publish", "report", "deliverable"}, []string{"publish_artifact"}},
	// Browser/computer tools are discovered by EXPLICIT exact tool names
	// plus intent keywords (never brittle token overlap). The governing
	// gates remain the authority; this only controls what the model sees.
	// Submit stays checkout-keyword-gated, upload its own entry (file
	// egress visibility is deliberate), everything else rides the base
	// set — which bare web addresses also unlock (see webAddressPattern).
	{[]string{"browser", "website", "web site", "open page", "open url", "open a page",
		"webpage", "web page", "navigate to", "fill in", "fill out", "form",
		"site", "visit", "landing page", "scroll", "click on", "dropdown",
		"checkbox", "the page", "this page", "web app"},
		browserIntentToolNames()},
	{[]string{"checkout", "place order", "submit order", "buy now", "pay for"}, []string{"browser_submit"}},
	{[]string{"upload", "attach a file", "file input", "attach a document"}, []string{"browser_upload"}},
	{[]string{"computer", "desktop", "screen", "on the computer", "on the desktop", "computer screen", "settings window", "ui", "interface"}, []string{"computer_inspect_ui", "computer_screenshot", "computer_click", "computer_type", "computer_press_key"}},
}

// browserIntentToolNames is the browser surface any browser-intent
// signal (keyword or bare web address) may expose: observe + act.
// High-impact submit/upload need their own explicit intent keywords.
func browserIntentToolNames() []string {
	return browserMobileToolNames()
}

// webAddressPattern matches an explicit web address in a message —
// scheme URLs, www hosts, and bare domains by TLD (nairobiunwind.com).
// A shared address is a browser-intent signal by itself: the model
// cannot be handed the page tools for "check out this site" while a
// literal URL falls through to web_fetch.
var webAddressPattern = regexp.MustCompile(
	`(?:https?://|www\.)[^\s]+|\b[a-z0-9][a-z0-9-]*(?:\.[a-z0-9-]+)*\.(?:com|org|net|io|ke|co|uk|us|ca|de|fr|jp|au|nz|in|br|za|ng|gh|eg|eu|dev|app|site|ai|gov|edu|xyz|me|info|blog|shop|store|tech|online|cloud)\b`)

// FilterToolsForTurn narrows the tool surface to a core set plus any tools whose
// intent keywords appear in the user message (media present always allows vision).
func FilterToolsForTurn(registry *ToolRegistry, profile ToolProfile, userMsg string, hasMedia bool) *ToolRegistry {
	base := FilterRegistryByProfile(registry, profile)
	if base == nil {
		return NewToolRegistry()
	}
	lower := strings.ToLower(userMsg)
	// A literal web address ("nairobiunwind.com", "https://…") is
	// browser intent by itself — see webAddressPattern.
	webAddress := webAddressPattern.MatchString(lower)
	highImpact := map[string]bool{"browser_submit": true, "browser_upload": true}

	include := func(name string) bool {
		if coreToolNames[name] {
			return true
		}
		if webAddress && strings.HasPrefix(name, "browser_") && !highImpact[name] {
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

// FilterNamesByProfile returns the names a profile allows, order preserved.
// Used when a capability lists the tools it needs: the surface profile still
// decides which of them may be offered.
func FilterNamesByProfile(p ToolProfile, names []string) []string {
	if p == "" || p == ProfileFull {
		return names
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if p.Allows(n) {
			out = append(out, n)
		}
	}
	return out
}
