package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ianclemence/ghost/pkg/skills"
)

// tryDeterministicTurn handles local ops without any LLM call.
// Intent -> Capability -> execute. Returns (answer, handled).
// This keeps shopping/reminders working even when the cloud provider is
// rate-limited and cuts provider cost for trivial turns.
func (al *AgentLoop) tryDeterministicTurn(msg, session string) (string, bool) {
	trimmed := strings.TrimSpace(msg)
	lower := strings.ToLower(trimmed)

	// shopping.add: "add milk and eggs to my shopping list" / "add milk to shopping list"
	if strings.Contains(lower, "shopping list") || strings.Contains(lower, "shoppinglist") {
		if isShoppingAdd(lower) {
			items := parseShoppingItems(trimmed)
			if len(items) > 0 {
				added := al.shoppingAdd(items)
				return "Added to your shopping list: " + strings.Join(added, ", ") + ".", true
			}
		}
		// shopping.list: "what's on my shopping list" / "show shopping list"
		if isShoppingList(lower) {
			return al.shoppingList(), true
		}
	}

	return "", false
}

// trySkillToggleFastPath handles "disable X" / "enable X" deterministically
// (zero LLM calls), mirroring the Web Console toggle exactly. Works even
// when the provider is rate-limited.
func (al *AgentLoop) trySkillToggleFastPath(msg string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(msg))
	var enable *bool
	var rest string
	for _, prefix := range []string{"disable ", "turn off ", "switch off ", "deactivate "} {
		if strings.HasPrefix(lower, prefix) {
			b := false
			enable = &b
			rest = strings.TrimSpace(msg[len(prefix):])
			break
		}
	}
	if enable == nil {
		for _, prefix := range []string{"enable ", "turn on ", "switch on ", "activate "} {
			if strings.HasPrefix(lower, prefix) {
				b := true
				enable = &b
				rest = strings.TrimSpace(msg[len(prefix):])
				break
			}
		}
	}
	if enable == nil {
		return "", false
	}
	// Strip trailing "skill" word: "disable the weather skill".
	rest = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(rest)), " skill")
	rest = strings.TrimPrefix(rest, "the ")
	rest = strings.TrimSpace(strings.TrimSuffix(rest, " skill"))
	name := skillToggleName(al.workspace, rest)
	if name == "" {
		return "", false
	}
	ok, msgOut := setSkillEnabledLocal(al.workspace, name, *enable)
	if !ok {
		return "", false
	}
	return msgOut, true
}

// skillToggleName resolves free text to an installed skill name (hyphens ≈
// spaces, case-insensitive, leading "the" and trailing "skill" stripped).
// Empty when no installed skill matches — the turn then falls through to
// the normal loop instead of guessing.
func skillToggleName(workspace, text string) string {
	clean := strings.TrimSpace(strings.ToLower(text))
	clean = strings.TrimSuffix(clean, " skill")
	clean = strings.TrimPrefix(clean, "the ")
	clean = strings.TrimSpace(strings.TrimSuffix(clean, " skill"))
	norm := strings.NewReplacer("-", " ", "_", " ").Replace(clean)
	dirs, err := os.ReadDir(workspace + "/skills")
	if err != nil {
		return ""
	}
	for _, e := range dirs {
		if !e.IsDir() {
			continue
		}
		n := strings.ToLower(e.Name())
		if norm == n || norm == strings.ReplaceAll(n, "-", " ") {
			return e.Name()
		}
	}
	// Also match without the trailing "s"? No — exact only, never guess.
	return ""
}

// setSkillEnabledLocal mirrors SkillManageTool.setSkillEnabled without a
// tool call: rename + manifest user_modified flag.
func setSkillEnabledLocal(workspace, name string, enabled bool) (bool, string) {
	skillDir := workspace + "/skills/" + name
	src := skillDir + "/SKILL.md"
	dst := skillDir + "/SKILL.md.disabled"
	if enabled {
		if _, err := os.Stat(dst); err != nil {
			return false, ""
		}
		if err := os.Rename(dst, src); err != nil {
			return false, ""
		}
	} else {
		if _, err := os.Stat(src); err != nil {
			return false, ""
		}
		if err := os.Rename(src, dst); err != nil {
			return false, ""
		}
	}
	if manifest, err := skills.LoadManifest(workspace + "/skills"); err == nil {
		if entry, ok := manifest.Skills[name]; ok {
			entry.UserModified = true
			manifest.Skills[name] = entry
			_ = manifest.SaveManifest(workspace + "/skills")
		}
	}
	if enabled {
		return true, "Done — " + name + " is enabled."
	}
	return true, "Done — " + name + " is off. Say enable to bring it back."
}

// maybeSetPendingFromAnswer detects when the assistant just asked a
// natural follow-up for a known missing input and records a pending
// continuation so the next short reply resumes. Generic: driven by
// question patterns, not per-skill branches.
func maybeSetPendingFromAnswer(session, userMsg, answer string) {
	lowerAns := strings.ToLower(answer)
	lowerMsg := strings.ToLower(userMsg)
	var field, skill, capID string
	switch {
	case strings.Contains(lowerAns, "which flight number"):
		field, skill, capID = "flight_number", "flight", "flight.status"
	case strings.Contains(lowerAns, "which city should i check") && (strings.Contains(lowerMsg, "weather") || strings.Contains(lowerMsg, "aqi") || strings.Contains(lowerMsg, "air quality")):
		field, skill, capID = "location", "weather", "weather.current"
	case strings.Contains(lowerAns, "which location should"):
		field, skill, capID = "location", "find-nearby", "nearby.search"
		if strings.Contains(lowerMsg, "travel") || strings.Contains(lowerMsg, "directions") {
			skill, capID = "travel", "travel.route"
		}
	default:
		return
	}
	skills.SetPending(session, skills.PendingContinuation{
		CapabilityID: capID, Skill: skill,
		MissingField: field, Question: strings.TrimSpace(answer), OriginalTask: strings.TrimSpace(userMsg),
	})
}

var shoppingAddRE = regexp.MustCompile(`(?i)add\s+(.+?)\s+to\s+(?:my\s+)?shopping\s*list`)

func isShoppingAdd(lower string) bool {
	return strings.Contains(lower, "add ") && strings.Contains(lower, "shopping")
}

func isShoppingList(lower string) bool {
	return strings.Contains(lower, "what") && strings.Contains(lower, "shopping") ||
		strings.Contains(lower, "show") && strings.Contains(lower, "shopping") ||
		strings.Contains(lower, "shopping list") && (strings.Contains(lower, "what") || strings.Contains(lower, "show") || strings.Contains(lower, "list"))
}

func parseShoppingItems(msg string) []string {
	m := shoppingAddRE.FindStringSubmatch(msg)
	if m == nil {
		return nil
	}
	raw := strings.TrimSpace(m[1])
	// Split on " and " / commas.
	raw = strings.ReplaceAll(raw, ", and ", ",")
	raw = strings.ReplaceAll(raw, " and ", ",")
	var out []string
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(strings.Trim(part, "\"' ."))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (al *AgentLoop) shoppingPath() string {
	return filepath.Join(al.workspace, "data", "shopping_list.txt")
}

func (al *AgentLoop) shoppingAdd(items []string) []string {
	path := al.shoppingPath()
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return items
	}
	defer f.Close()
	for _, it := range items {
		_, _ = f.WriteString(strings.TrimSpace(it) + "\n")
	}
	return items
}

func (al *AgentLoop) shoppingList() string {
	data, err := os.ReadFile(al.shoppingPath())
	if err != nil || strings.TrimSpace(string(data)) == "" {
		return "Your shopping list is empty."
	}
	var items []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			items = append(items, "- "+line)
		}
	}
	if len(items) == 0 {
		return "Your shopping list is empty."
	}
	return "Your shopping list:\n" + strings.Join(items, "\n")
}

// resolvePendingResume checks for a natural clarification continuation.
// If the session has a pending missing input and the message looks like a
// short answer (not a new task), it rewrites the effective user message to
// carry the original task forward. Returns (effectiveMessage, resumed).
func resolvePendingResume(session, msg string) (string, bool) {
	pending, ok := skills.GetPending(session)
	if !ok {
		return msg, false
	}
	trimmed := strings.TrimSpace(msg)
	// Only treat short, non-command replies as answers. A full new request
	// (long, or with its own intent verbs) starts a fresh task.
	if len(trimmed) == 0 || len(trimmed) > 80 || strings.HasPrefix(trimmed, "/") {
		return msg, false
	}
	lower := strings.ToLower(trimmed)
	// If it looks like a brand-new task, don't hijack it.
	newTaskMarkers := []string{"remind me", "what's the weather", "what is the weather", "find ", "add ", "schedule ", "remember "}
	for _, m := range newTaskMarkers {
		if strings.Contains(lower, m) && len(trimmed) > 25 {
			return msg, false
		}
	}
	// Build resumed task.
	var resumed string
	switch pending.MissingField {
	case "flight_number":
		resumed = "Check flight status for " + trimmed + " (answering: " + pending.Question + "; original task: " + pending.OriginalTask + ")"
	case "location":
		resumed = pending.OriginalTask + " Location answer: " + trimmed
	default:
		resumed = pending.OriginalTask + " Answer: " + trimmed
	}
	skills.ClearPending(session)
	return resumed, true
}

// isSecurityProbe reports adversarial attempts to reveal internals.
// These must never be hijacked by capability fast-paths (e.g. a prompt
// containing "skills/weather/SKILL.md" is not a weather request).
func isSecurityProbe(msg string) bool {
	lower := strings.ToLower(msg)
	for _, marker := range []string{
		"skill.md", ".bundled", "dir:", "file:", "/var/lib",
		"api_key", "api key", "secret", "tool instructions",
		"internal prompt", "show me the contents of skills",
		"bundled manifest",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// tryDisabledSkillFastPath answers requests for a disabled skill honestly
// (zero LLM calls) instead of letting the model improvise around the toggle.
func (al *AgentLoop) tryDisabledSkillFastPath(msg, session string) (string, bool) {
	_ = session
	if name := skills.MatchDisabledSkill(al.workspace, msg); name != "" {
		r := skills.CheckReadiness(name, al.workspace, nil)
		if r.Message != "" {
			return r.Message, true
		}
		return "The " + name + " skill is currently disabled. Enable it in Ghost's Skills settings to use it.", true
	}
	return "", false
}

// tryReadinessFastPath handles missing-input / not-configured cases
// deterministically (zero LLM calls) via the generic readiness model:
// Intent -> Capability -> Readiness -> ask / setup message + pending.
// Returns (answer, handled).
func (al *AgentLoop) tryReadinessFastPath(msg, session string, metadata map[string]string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(msg))
	if lower == "" {
		return "", false
	}
	// Security probes bypass all capability fast-paths; the normal LLM +
	// output filters handle them without leaking.
	if isSecurityProbe(msg) {
		return "", false
	}

	// Flight: missing number -> ask + pending. With number but no key ->
	// needs_configuration (no fake live data).
	if isFlightIntent(lower) {
		inputs := capabilityInputsFromMessage(msg, metadata)
		r := skills.CheckReadiness("flight", al.workspace, inputs)
		// Missing number takes precedence: ask naturally, no clarify tool.
		if r.Status == skills.StatusNeedsUserInput && r.Requirement == "flight_number" {
			skills.SetPending(session, skills.PendingContinuation{
				CapabilityID: "flight.status", Skill: "flight",
				MissingField: "flight_number", Question: r.Question, OriginalTask: strings.TrimSpace(msg),
			})
			return r.Question, true
		}
		if r.Status == skills.StatusNeedsConfiguration || r.Status == skills.StatusUnavailable || r.Status == skills.StatusTemporarilyUnavailable {
			return r.Message, true
		}
		// Aviation key check is generic (no per-skill env branches elsewhere).
		if skills.AviationKey(nil) == "" {
			return "Flight tracking isn't connected yet. Add your flight data key in Ghost settings under Integrations, then try again — I won't guess flight data.", true
		}
		return "", false
	}

	// Nearby / travel: missing location.
	if isNearbyIntent(lower) || isTravelIntent(lower) {
		inputs := capabilityInputsFromMessage(msg, metadata)
		if strings.TrimSpace(inputs["location"]) == "" {
			skill := "find-nearby"
			if isTravelIntent(lower) {
				skill = "travel"
			}
			r := skills.CheckReadiness(skill, al.workspace, inputs)
			// Only fast-path the missing-input case; otherwise let LLM use device context.
			if r.Status == skills.StatusNeedsUserInput {
				skills.SetPending(session, skills.PendingContinuation{
					CapabilityID: r.Requirement, Skill: skill,
					MissingField: "location", Question: r.Question, OriginalTask: strings.TrimSpace(msg),
				})
				return r.Question, true
			}
		}
		return "", false
	}

	// Weather without any location: ask rather than wander.
	if isWeatherIntent(lower) {
		inputs := capabilityInputsFromMessage(msg, metadata)
		if strings.TrimSpace(inputs["location"]) == "" {
			skills.SetPending(session, skills.PendingContinuation{
				CapabilityID: "weather.current", Skill: "weather",
				MissingField: "location", Question: "Which city should I check?",
				OriginalTask: strings.TrimSpace(msg),
			})
			return "Which city should I check?", true
		}
		return "", false
	}

	// Calendar: enabled != configured != ready. Product message, no gcalcli leak.
	if isCalendarIntent(lower) {
		r := skills.CheckReadiness("calendar", al.workspace, nil)
		if r.Status != skills.StatusReady {
			return r.Message, true
		}
		return "", false
	}

	// Home Assistant: product message, no env leak.
	if isHassIntent(lower) {
		r := skills.CheckReadiness("homeassistant", al.workspace, nil)
		if r.Status != skills.StatusReady {
			return r.Message, true
		}
		return "", false
	}

	return "", false
}

func isFlightIntent(lower string) bool {
	return strings.Contains(lower, "flight")
}

func isNearbyIntent(lower string) bool {
	for _, k := range []string{"nearby", "near me", "coffee shop", "cafes", "cafe", "restaurant nearby"} {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

func isTravelIntent(lower string) bool {
	for _, k := range []string{"directions to", "how do i get to", "how long to get to", "travel time", "fastest route"} {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

func isWeatherIntent(lower string) bool {
	for _, k := range []string{"weather", "temperature", "is it going to rain", "will i need an umbrella", "how hot"} {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

func isCalendarIntent(lower string) bool {
	for _, k := range []string{"calendar", "meetings today", "do i have meetings", "schedule a meeting", "add an event", "is tomorrow free"} {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

func isHassIntent(lower string) bool {
	for _, k := range []string{"turn on the lights", "thermostat", "front door locked", "trigger scene", "home assistant"} {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

func hasAviationKey() bool {
	// Deprecated shim: use skills.AviationKey (secrets-first). Kept for
	// existing callers/tests; new code should call skills directly.
	return skills.AviationKey(nil) != ""
}

// capabilityInputsFromMessage extracts known inputs for readiness checks
// from the user message and request metadata (device location).
func capabilityInputsFromMessage(msg string, metadata map[string]string) map[string]string {
	out := map[string]string{}
	// Flight number: 2-letter airline + 1-4 digits, e.g. TG123, UA1234.
	if m := flightNumberRE.FindString(msg); m != "" {
		out["flight_number"] = strings.ToUpper(strings.TrimSpace(m))
	}
	// Location: explicit device metadata wins, else "in <Place>" heuristic.
	if metadata != nil {
		if city := strings.TrimSpace(metadata["city"]); city != "" {
			out["location"] = city
		} else if lat, lon := strings.TrimSpace(metadata["latitude"]), strings.TrimSpace(metadata["longitude"]); lat != "" && lon != "" {
			out["location"] = lat + "," + lon
		}
	}
	if _, ok := out["location"]; !ok {
		if loc := locationFromText(msg); loc != "" {
			out["location"] = loc
		}
	}
	return out
}

var flightNumberRE = regexp.MustCompile(`(?i)\b([A-Z]{2}\s?\d{1,4})\b`)

var locationFromTextRE = regexp.MustCompile(`(?i)\bin\s+([A-Z][a-zA-Z\s\-]{2,30})(?:\?|\.|$)`)

func locationFromText(msg string) string {
	m := locationFromTextRE.FindStringSubmatch(msg)
	if m == nil {
		return ""
	}
	loc := strings.TrimSpace(m[1])
	// Trim trailing verbs that leaked in.
	for _, stop := range []string{" today", " now", " tomorrow", " right now"} {
		if idx := strings.Index(strings.ToLower(loc), stop); idx >= 0 {
			loc = strings.TrimSpace(loc[:idx])
		}
	}
	if len(loc) < 3 || len(loc) > 40 {
		return ""
	}
	return loc
}
