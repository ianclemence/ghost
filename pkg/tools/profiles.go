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
		"read_file", "write_file", "list_dir", "edit_file", "append_file", "delete_file", "move_file",
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
		"memory_recall", "context_get", "memory_correct", "clarify", "todo",
		// Device control and handoff are legitimate mobile actions; the
		// broker still governs the consequential ones.
		"device", "calendar", "publish_artifact", "doc_parser",
		// Showing the owner what was built, running in their chat. Sandboxed on
		// the phone and unable to reach the network, so nothing to gate.
		"canvas",
		// Cards that show or ask, and drafts the owner sends themselves: none
		// of them can run, open or send anything on their own.
		"present_card", "draft",
		// Documents are laid out on the Pod and shown; nothing is sent.
		"document",
		// The owner's own records on their Pod: people, documents, money.
		"people", "vault", "money", "trips", "phone", "knowledge",
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
		"read_file", "write_file", "list_dir", "edit_file", "append_file", "delete_file", "move_file",
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
		"read_file", "write_file", "list_dir", "edit_file", "append_file", "delete_file", "move_file",
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
		"browser_drag", "browser_dialog", "browser_download", "browser_login",
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
	// Tidying its own workspace (deleting and moving files) is a basic ability,
	// like writing one. Deleting still asks the owner every time it is not
	// already allowed.
	"delete_file": true, "move_file": true,
	"web_search": true, "web_fetch": true, "session_search": true,
	"remember": true, "context_get": true, "memory_curate": true, "memory_correct": true,
	"memory_recall": true,
	// Read-only skill reference tools: always visible so skill docs that name
	// them as the preferred path actually work. Cheap, no side effects.
	"weather_now": true, "places_nearby": true, "aqi_now": true,
	"crypto_price": true, "currency_convert": true, "flight_status": true,
	"clarify": true, "todo": true,
	"schedule": true,
	// Showing an answer as a card (or asking with one) is how Ghost talks on
	// the phone; it has to be there whatever the words of the message were.
	"present_card": true,
	"spawn":        true, "subagent": true,
	// Status phrasings vary too much to keyword-gate ("is the device
	// healthy", "how much space is left", "what's the temperature") and a
	// missed offer is a refused question — always present, read-only,
	// one small schema.
	"system_status": true,
}

// turnIntentTools maps message keyword signals to niche tools to include so a
// turn only pays for tools it might actually use. A tool not in coreToolNames
// and not matched here is dropped for the turn, shrinking the surface the model
// reasons over (higher specificity = better selection + cheaper turns).
var turnIntentTools = []struct {
	keywords []string
	tools    []string
}{
	{[]string{"draw", "diagram", "flowchart", "mindmap", "canvas",
		// Building something to look at and use: a page, a tool, a game, a
		// mock-up. "Show me what that code does" and "make me a ..." are the
		// ways people ask.
		"html", "web page", "webpage", "landing page", "website", "web app", "mockup", "mock-up", "mock up",
		"prototype", "wireframe", "visualize", "visualise", "visualization", "visualisation", "dashboard",
		"chart", "graph", "infographic", "animation", "animate", "calculator", "stopwatch", "timer app", "widget",
		"game", "quiz", "to-do app", "todo app", "counter", "interactive", "ui for", "page that", "page with",
		"build me", "make me a", "make me an", "create a page", "show it to me", "display it", "see it", "preview", "render"},
		[]string{"canvas"}},
	// Writing something for the owner to send: they see it in full and send it.
	{[]string{"email", "e-mail", "mail ", "reply to", "write to", "write back", "text ", "sms", "message to", "send a message",
		"invite", "calendar", "schedule a meeting", "set up a meeting", "book a meeting", "add to my calendar", "put it in my calendar",
		"draft", "let them know", "tell her", "tell him", "tell them"},
		[]string{"draft", "email_search"}},
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
	// The people in the owner's life. Mentioning someone is enough: "my
	// sister", "Sam's birthday", "text Mum" all need to know who.
	{[]string{"mum", "mom", "dad", "mother", "father", "sister", "brother", "wife", "husband", "partner", "girlfriend", "boyfriend",
		"son", "daughter", "kids", "aunt", "uncle", "cousin", "grandma", "grandpa", "friend", "colleague", "boss", "neighbour", "neighbor",
		"birthday", "anniversary", "who is", "who's", "gift for", "present for", "get her", "get him", "call ", "text ", "phone number",
		"remember that", "keep in touch", "haven't spoken", "people"},
		[]string{"people"}},
	// Important documents and their dates.
	{[]string{"passport", "visa", "id card", "national id", "driving licence", "driving license", "licence", "license", "insurance",
		"policy", "warranty", "guarantee", "lease", "tenancy", "contract", "logbook", "registration", "certificate", "expires", "expiry",
		"renewal", "renew", "vault", "my documents"},
		[]string{"vault", "vision", "doc_parser"}},
	// The phone: notifications it shared, health totals, place reminders, alarms.
	{[]string{"notification", "message from", "messaged me", "texted me", "whatsapp", "did anyone", "missed call", "steps", "sleep", "slept",
		"heart rate", "health", "walked", "exercise", "when i get to", "when i arrive", "when i leave", "when i'm at", "when i am at", "arrive at",
		"get home", "alarm", "wake me", "wake up at"},
		[]string{"phone", "draft", "places_nearby"}},
	// Knowledge: what the owner reads and studies.
	{[]string{"reading", "i read", "finished reading", "book", "chapter", "page ", "studying", "study", "revise", "revision", "exam", "course",
		"lesson", "module", "lecture", "learning", "learn ", "podcast", "article", "highlight", "quote", "flashcard", "quiz me", "test me"},
		[]string{"knowledge", "present_card", "canvas"}},
	// Trips: journeys, bookings, flights and stays.
	{[]string{"trip", "travel", "travelling", "traveling", "holiday", "vacation", "flight", "flying", "booking", "booked", "itinerary",
		"hotel", "airbnb", "check in", "check-in", "boarding", "airport", "train to", "bus to", "going to ", "visiting"},
		[]string{"trips", "email_search"}},
	// Money: spending, receipts, subscriptions, bills, statements.
	{[]string{"spent", "spend", "spending", "paid", "pay ", "bought", "cost", "receipt", "expense", "budget", "money", "salary", "income",
		"earned", "subscription", "subscribed", "netflix", "spotify", "bill", "rent", "electricity", "kplc", "water bill", "statement", "m-pesa",
		"mpesa", "bank", "renews", "this month", "how much"},
		[]string{"money", "vision"}},
	// Making something to keep, print, send or sign.
	{[]string{"cv", "resume", "résumé", "cover letter", "letter", "invoice", "quote for", "receipt", "itinerary",
		"one-pager", "one pager", "report", "proposal", "contract", "agenda", "minutes", "meeting notes", "recipe card",
		"document", "pdf", "word file", "word doc", "docx", "print", "printable", "write up", "write-up", "handout", "flyer", "menu"},
		[]string{"document"}},
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
	// Well-known sites and the verbs of doing things on the web. "Search Amazon
	// for a keyboard" names no address and none of the words below, so the
	// model was never handed a browser and told the owner it could not browse.
	{[]string{"amazon", "ebay", "etsy", "walmart", "aliexpress", "lazada", "shopee", "best buy", "bestbuy",
		"youtube", "reddit", "wikipedia", "linkedin", "airbnb", "google flights", "google maps", "google shopping",
		"shop for", "shopping", "add to cart", "add to my cart", "to my cart", "compare prices", "price check",
		"search online", "look up online", "look it up online", "find online", "on the web",
		"log in to", "login to", "sign in to", "sign up for", "book a ", "reserve a ",
		// Travel and price shopping live on the web: "cheapest one-way Bangkok
		// to Shenzhen" names no site, and used to get no browser at all.
		"flights", "airfare", "fares", "one-way", "one way", "round trip", "round-trip",
		"hotel", "hotels", "tickets", "cheapest", "price of", "prices for", "how much is", "in stock"},
		browserIntentToolNames()},
	{[]string{"browser", "website", "web site", "open page", "open url", "open a page",
		"webpage", "web page", "navigate to", "fill in", "fill out", "form",
		"site", "visit", "landing page", "scroll", "click on", "dropdown",
		"checkbox", "the page", "this page", "that page", "results page", "screenshot of", "web app"},
		browserIntentToolNames()},
	{[]string{"checkout", "place order", "submit order", "buy now", "pay for"}, []string{"browser_submit"}},
	{[]string{"upload", "attach a file", "file input", "attach a document"}, []string{"browser_upload"}},
	{[]string{"computer", "desktop", "screen", "on the computer", "on the desktop", "computer screen", "settings window", "ui", "interface"}, []string{"computer_inspect_ui", "computer_screenshot", "computer_click", "computer_type", "computer_press_key"}},
}

// BrowserFollowUpToolNames is the browser surface a follow-up in a
// conversation that was just using the browser keeps ("send me a screenshot
// of that", "now sort by price"): observe + act, never submit or upload.
func BrowserFollowUpToolNames() []string {
	return browserIntentToolNames()
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

	// High-confidence intent: prefer the purpose-built capability and suppress
	// generic overlap (shell/sandbox) for this turn. Ambiguous input routes
	// nothing, so the model keeps the safe fallback.
	route, routed := PreferredIntentRoute(userMsg)
	excluded := map[string]bool{}
	if routed {
		for _, n := range route.Exclude {
			excluded[n] = true
		}
	}

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
		if excluded[name] {
			continue
		}
		if include(name) || (routed && stringInSlice(route.Tools, name)) {
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
