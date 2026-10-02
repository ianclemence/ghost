package agent

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/ianclemence/ghost/pkg/credentials"
	"github.com/ianclemence/ghost/pkg/logger"
	"github.com/ianclemence/ghost/pkg/personalcontext"
	"github.com/ianclemence/ghost/pkg/skills"
)

// askFiller is the politeness and sentence scaffolding that may surround a
// command without changing what it asks.
var askFiller = map[string]bool{
	"please": true, "pls": true, "can": true, "could": true, "would": true, "will": true,
	"you": true, "hey": true, "hi": true, "hello": true, "ghost": true, "me": true,
	"tell": true, "show": true, "give": true, "thanks": true, "thank": true,
	"the": true, "a": true, "an": true, "are": true, "is": true, "there": true,
	"do": true, "does": true, "i": true, "we": true, "have": true, "got": true,
	"what": true, "whats": true, "how": true, "which": true, "any": true, "all": true,
	"my": true, "current": true, "right": true, "now": true, "ok": true, "okay": true,
	"so": true, "just": true, "quick": true, "quickly": true, "of": true, "s": true,
}

// wholeAsk reports whether re accounts for the entire message. A shortcut
// that answers from stored state or a fixed tool must only fire on a request
// it fully understands: a keyword found inside a longer sentence says nothing
// about what that sentence asks ("are there any reminders I forgot to look
// at" contains "any reminders" and is not a request to list reminders). Any
// words left over once the match and the filler are removed mean the message
// carries something the shortcut cannot see, so the model reads it instead.
func wholeAsk(lower string, re *regexp.Regexp) bool {
	return wholeAskWith(lower, re, "")
}

// slotFiller is what may sit around a request that carries a place: the
// prepositions that introduce it and the time words that qualify "now".
var slotFiller = map[string]bool{
	"in": true, "at": true, "of": true, "for": true, "near": true, "around": true,
	"like": true, "today": true, "outside": true, "currently": true, "rn": true,
	"there": true,
}

// wholeAskWith is wholeAsk for a request that carries a slot (a place). The
// slot's own words are removed first, then the prepositions and time words in
// slotFiller are tolerated, so "what's the weather like in nairobi today"
// passes and "what did you do wrong when I asked about the weather in
// nairobi" does not.
func wholeAskWith(lower string, re *regexp.Regexp, slot string) bool {
	loc := re.FindStringIndex(lower)
	if loc == nil {
		return false
	}
	rest := lower[:loc[0]] + " " + lower[loc[1]:]
	if slot != "" {
		rest = strings.Replace(rest, strings.ToLower(slot), " ", 1)
	}
	for _, w := range strings.FieldsFunc(rest, func(r rune) bool {
		return !(unicode.IsLetter(r) || unicode.IsDigit(r))
	}) {
		if !askFiller[w] && !(slot != "" && slotFiller[w]) {
			return false
		}
	}
	return true
}

// currentConditionsAsk reports whether the weather or air-quality request
// accounts for the whole message once its place is set aside. A bare "weather"
// always does.
func currentConditionsAsk(lower, loc string) bool {
	for _, phrase := range []string{"weather", "aqi", "air quality"} {
		if skills.IsBareUtterance(lower, phrase) {
			return true
		}
	}
	return wholeAskWith(lower, weatherAskRE, loc) || wholeAskWith(lower, aqiAskRE, loc)
}

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
// question patterns, not per-skill branches. workspace persists the
// continuation so a fresh process (single-shot CLI) can resume it.
func maybeSetPendingFromAnswer(workspace, session, userMsg, answer string) {
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
	skills.SetPendingDurable(workspace, session, skills.PendingContinuation{
		CapabilityID: capID, Skill: skill,
		MissingField: field, Question: strings.TrimSpace(answer), OriginalTask: strings.TrimSpace(userMsg),
	})
}

var shoppingAddRE = regexp.MustCompile(`(?i)add\s+(.+?)\s+to\s+(?:my\s+)?shopping\s*list`)

func isShoppingAdd(lower string) bool {
	return strings.Contains(lower, "add ") && strings.Contains(lower, "shopping")
}

// shoppingListAskRE matches a request to hear the list. The old guard was
// `contains("shopping list") && (contains("what") || contains("show") ||
// contains("list"))` — "shopping list" itself contains "list", so any sentence
// with the phrase satisfied it and read the list back instead of answering
// ("how do i add shopping list export to my notes app" → "Your shopping
// list: …").
var shoppingListAskRE = regexp.MustCompile(`(?i)(?:what'?s?\s+(?:on|is\s+on)\s+(?:my|the)\s+shopping|show\s+(?:me\s+)?(?:my|the\s+)?\s*shopping\s+list|read\s+(?:me\s+)?(?:my\s+)?shopping\s+list|check\s+(?:my|the)\s+shopping\s+list|shopping\s+list\s+please)`)

func isShoppingList(lower string) bool {
	return shoppingListAskRE.MatchString(lower) || skills.IsBareUtterance(lower, "shopping list")
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
// carry the original task forward. Returns (effectiveMessage, resumed,
// field, answer) where field/answer let the runtime dispatch the resumed
// capability deterministically (never fabricated). workspace lets a fresh
// process (single-shot CLI) resume a durable continuation written earlier.
func resolvePendingResume(workspace, session, msg string) (string, bool, string, string) {
	pending, ok := skills.GetPendingDurable(workspace, session)
	if !ok {
		return msg, false, "", ""
	}
	// Only clarify-style continuations (missing a short input) are resumable
	// this way. Routine and standing-permission proposals also live in the
	// durable store and must be confirmed by their own fast-paths — never
	// hijacked into a clarification rewrite.
	if pending.MissingField != "location" && pending.MissingField != "flight_number" {
		return msg, false, "", ""
	}
	trimmed := strings.TrimSpace(msg)
	// Only treat short, non-command replies as answers. A full new request
	// (long, or with its own intent verbs) starts a fresh task.
	if len(trimmed) == 0 || len(trimmed) > 80 || strings.HasPrefix(trimmed, "/") {
		return msg, false, "", ""
	}
	lower := strings.ToLower(trimmed)
	// If it looks like a brand-new task, don't hijack it.
	newTaskMarkers := []string{"remind me", "what's the weather", "what is the weather", "find ", "add ", "schedule ", "remember "}
	for _, m := range newTaskMarkers {
		if strings.Contains(lower, m) && len(trimmed) > 25 {
			return msg, false, "", ""
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
	// Complete both memory and durable (if any) so one answer resumes
	// exactly once across processes/restarts.
	skills.ClearPending(session)
	skills.CompleteDurable(workspace, session)
	return resumed, true, pending.MissingField, trimmed
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
			skills.SetPendingDurable(al.workspace, session, skills.PendingContinuation{
				CapabilityID: "flight.status", Skill: "flight",
				MissingField: "flight_number", Question: r.Question, OriginalTask: strings.TrimSpace(msg),
			})
			return r.Question, true
		}
		if r.Status == skills.StatusNeedsConfiguration || r.Status == skills.StatusUnavailable || r.Status == skills.StatusTemporarilyUnavailable {
			return r.Message, true
		}
		// Aviation key check is generic (no per-skill env branches elsewhere).
		if credentials.AviationKey(nil) == "" {
			return "Flight tracking isn't connected yet. Add your flight data key in Ghost settings under Integrations, then try again — I won't guess flight data.", true
		}
		return "", false
	}

	// Nearby / travel: missing location.
	if isNearbyIntent(lower) || isTravelIntent(lower) {
		inputs := al.locationWithMemoryFallback(msg, session, capabilityInputsFromMessage(msg, metadata))
		if strings.TrimSpace(inputs["location"]) == "" {
			// Same as weather: an anaphoric reference ("near that city")
			// is answered from the conversation, not re-asked.
			if locationRefersBack(msg) {
				return "", false
			}
			skill := "find-nearby"
			if isTravelIntent(lower) {
				skill = "travel"
			}
			r := skills.CheckReadiness(skill, al.workspace, inputs)
			// Only fast-path the missing-input case; otherwise let LLM use device context.
			if r.Status == skills.StatusNeedsUserInput {
				skills.SetPendingDurable(al.workspace, session, skills.PendingContinuation{
					CapabilityID: r.Requirement, Skill: skill,
					MissingField: "location", Question: r.Question, OriginalTask: strings.TrimSpace(msg),
				})
				return r.Question, true
			}
		}
		return "", false
	}

	// Weather without any location: resolve "here" from stored place, else
	// ask rather than wander.
	if isWeatherIntent(lower) {
		inputs := al.locationWithMemoryFallback(msg, session, capabilityInputsFromMessage(msg, metadata))
		if strings.TrimSpace(inputs["location"]) == "" {
			// "…like there" points back at a place this conversation just
			// named. Nothing here can resolve it, but asking again ignores
			// the turn we are in — hand it to the model, which has the
			// history and can use the antecedent.
			if locationRefersBack(msg) {
				return "", false
			}
			// The runtime may only ask when it is sure nothing was said. A
			// message that names something after a place word, or carries a
			// second ask, failed extraction — it did not omit the place.
			if !isSingleAsk(lower) || namesAPlace(lower) || !currentConditionsAsk(lower, "") {
				return "", false
			}
			skills.SetPendingDurable(al.workspace, session, skills.PendingContinuation{
				CapabilityID: "weather.current", Skill: "weather",
				MissingField: "location", Question: "Which city should I check?",
				OriginalTask: strings.TrimSpace(msg),
			})
			return "Which city should I check?", true
		}
		if strings.TrimSpace(metadata["resume_field"]) == "location" {
			al.rememberHomeLocation(session, inputs["location"], msg)
		}
		return "", false
	}

	// Calendar: enabled != configured != ready. Product message, no gcalcli leak.
	if isCalendarIntent(lower) && isSingleAsk(lower) {
		r := skills.CheckReadiness("calendar", al.workspace, nil)
		if r.Status != skills.StatusReady {
			return r.Message, true
		}
		return "", false
	}

	// Home Assistant: product message, no env leak.
	if isHassIntent(lower) && isSingleAsk(lower) {
		r := skills.CheckReadiness("homeassistant", al.workspace, nil)
		if r.Status != skills.StatusReady {
			return r.Message, true
		}
		return "", false
	}

	return "", false
}

// flightAskRE matches an explicit request to check a flight's status. A bare
// "flight" anywhere in a sentence is a topic — a comparison of booking sites
// or a drone flight controller is not a flight-status request, and firing the
// fast path on those answered with a key-setup message (or, once configured,
// stored a flight-number continuation that swallowed the next message).
var flightAskRE = regexp.MustCompile(`(?i)(?:flight\s+status|status\s+of\s+(?:the\s+|my\s+)?flight|track\s+(?:my\s+|the\s+)?flight|flight\s+number|where(?:'s| is)\s+(?:my\s+|the\s+)?flight|(?:is|has)\s+(?:my|the)\s+flight|my\s+flight\s+(?:status|number|landed|delayed|boarding|arrived|departed|on\s+time)|flight\s+(?:landed|delayed|boarding|arrived|departed|on\s+time))`)

func isFlightIntent(lower string) bool {
	return flightAskRE.MatchString(lower) || skills.IsBareUtterance(lower, "flight")
}

// nearbyAskRE matches a request for places around a location. The original
// list matched the bare nouns ("cafe", "coffee shop", "cafes"), so "i want to
// open a cafe, what equipment do i need" was treated as a nearby search —
// which stored a location continuation and turned the owner's next message
// into "… Location answer: …". The noun only counts with a proximity term or
// an explicit find/search frame.
var nearbyAskRE = regexp.MustCompile(`(?i)(?:nearby|near\s+me|around\s+me|close\s+to\s+me|near\s+here|around\s+here|(?:caf[eé]s?|coffee\s+shops?|restaurants?|places?|shops?|stores?|gas\s+stations?|bars?|pharmac(?:y|ies))\s+(?:near|around|close\s+to|nearby)|(?:find|search\s+(?:for\s+)?)\s+(?:a\s+|an\s+|some\s+|me\s+(?:a\s+)?)?(?:caf[eé]s?|coffee|restaurants?|places|shops|stores))`)

func isNearbyIntent(lower string) bool {
	return nearbyAskRE.MatchString(lower) ||
		skills.IsBareUtterance(lower, "cafe") ||
		skills.IsBareUtterance(lower, "cafes") ||
		skills.IsBareUtterance(lower, "coffee shop")
}

func isTravelIntent(lower string) bool {
	for _, k := range []string{"directions to", "how do i get to", "how long to get to", "travel time", "fastest route"} {
		if strings.Contains(lower, k) {
			return true
		}
	}
	return false
}

// The bare-capability half of every matcher below lives in one place:
// skills.IsBareUtterance — a capability word standing alone ("weather",
// "flight", "calendar") is unambiguous, because there is no other subject in
// the sentence for it to belong to. The ask-frame regexes handle everything
// longer.

// weatherAskRE matches an explicit request for current conditions.
//
// It exists because "temperature" and "weather" are also ordinary words in
// other people's sentences. Matching them anywhere sent a product-spec ask
// ("…read room temperature and humidity … manufacture it in China") to
// weather_now for China, and sent the owner's complaint about that answer
// ("why are you telling me the weather, I did not ask you about it") into the
// readiness fast-path — which asked "Which city should I check?", stored a
// location continuation, and then rewrote the owner's next message into
// "… Location answer: are you dumb" and geocoded it. The deterministic path
// exists to save an LLM call on an unambiguous ask, so the bar is an ask, not
// the presence of a topic word: when in doubt the turn belongs to the model.
var weatherAskRE = regexp.MustCompile(`(?i)(?:what(?:'?s| is)?\s+(?:the\s+|some\s+)?(?:current\s+)?(?:weather|temperature)|how(?:'?s| is)?\s+the\s+weather|weather\s+(?:in|at|for|like|today|now|report)|check\s+(?:the\s+)?weather|current\s+temperature|temperature\s+(?:in|at|outside|now|today)|how\s+hot|how\s+cold|is\s+it\s+going\s+to\s+rain|will\s+i\s+need\s+an\s+umbrella|degrees\s+(?:outside|now))`)

// isWeatherIntent reports whether the message is actually asking for current
// conditions — see weatherAskRE for why a bare keyword match was not enough.
func isWeatherIntent(lower string) bool {
	if skills.IsBareUtterance(lower, "weather") ||
		skills.IsBareUtterance(lower, "temperature") ||
		skills.IsBareUtterance(lower, "temp") {
		return true
	}
	return weatherAskRE.MatchString(lower)
}

// aqiAskRE matches an explicit air-quality request. The AQI branch of the
// dispatch path sat beside the weather branch with the same bare-word test
// ("air quality", " aqi"), so a sensor-spec sentence geocoded whatever
// followed "in" — "what does an aqi sensor cost in bulk" queried aqi_now for
// a place called "bulk" — the exact defect class the weather fix closed.
var aqiAskRE = regexp.MustCompile(`(?i)(?:air\s+quality\s+(?:in|at|near|index|today|now)|(?:what'?s|how(?:'s| is)|check|current)\s+(?:the\s+)?(?:air\s+quality|aqi)|(?:air\s+quality|aqi)\s+(?:in|at|near|now|today|for|of)|aqi\s+(?:is|reading))`)

// isAQIIntent reports whether the message is actually asking for air quality.
func isAQIIntent(lower string) bool {
	return aqiAskRE.MatchString(lower) ||
		skills.IsBareUtterance(lower, "aqi") ||
		skills.IsBareUtterance(lower, "air quality")
}

// calendarAskRE matches a request about the owner's schedule. The bare word
// "calendar" is a common product topic — "design a calendar app for doctors"
// got answered with the calendar-connection setup message instead.
var calendarAskRE = regexp.MustCompile(`(?i)(?:what'?s?\s+(?:on|in)\s+(?:my|the)\s+calendar|on\s+(?:my|the)\s+calendar|in\s+(?:my|the)\s+calendar|check\s+(?:my|the)\s+calendar|show\s+(?:my|the)\s+calendar|my\s+calendar\s+(?:for|today|tomorrow)|calendar\s+(?:today|tomorrow)|meetings?\s+(?:today|tomorrow)|do\s+i\s+have\s+(?:any\s+)?meetings|schedule\s+(?:a|an|the)\s+meeting|add\s+(?:an?\s+)?(?:event|meeting)|is\s+tomorrow\s+free|am\s+i\s+free)`)

func isCalendarIntent(lower string) bool {
	return calendarAskRE.MatchString(lower) || skills.IsBareUtterance(lower, "calendar")
}

// hassAskRE matches a home-control request: an imperative device action or a
// device-state question. Bare "thermostat" and "home assistant" were shopping
// and explanation topics too — "which thermostat should i buy" and "explain
// how home assistant works" each got answered with the Home Assistant
// connection setup message.
var hassAskRE = regexp.MustCompile(`(?i)(?:turn\s+(?:on|off)\s+(?:the\s+)?(?:lights?|lamps?|fans?)|set\s+(?:the\s+)?thermostat|thermostat\s+(?:to|at|set)|what'?s\s+(?:the\s+)?thermostat|turn\s+(?:up|down)\s+(?:the\s+)?thermostat|front\s+door\s+locked|lock\s+(?:the\s+)?(?:front\s+)?door|unlock\s+(?:the\s+)?(?:front\s+)?door|trigger\s+(?:the\s+)?scene|activate\s+(?:the\s+)?scene|home\s+assistant\s+(?:is|status|not))`)

func isHassIntent(lower string) bool {
	return hassAskRE.MatchString(lower) || skills.IsBareUtterance(lower, "thermostat")
}

// capabilityInputsFromMessage extracts known inputs for readiness checks
// from the user message and request metadata (device location).
func capabilityInputsFromMessage(msg string, metadata map[string]string) map[string]string {
	out := map[string]string{}
	// Flight number: 2-letter airline + 1-4 digits, e.g. TG123, UA1234.
	if m := flightNumberRE.FindString(msg); m != "" {
		out["flight_number"] = strings.ToUpper(strings.TrimSpace(m))
	}
	// Location: explicit device metadata wins, else "in <Place>" heuristic,
	// else a structured clarification answer ("resume_answer") provided by
	// the durable-continuation resume path.
	if metadata != nil {
		if city := strings.TrimSpace(metadata["city"]); city != "" {
			out["location"] = city
		} else if lat, lon := strings.TrimSpace(metadata["latitude"]), strings.TrimSpace(metadata["longitude"]); lat != "" && lon != "" {
			out["location"] = lat + "," + lon
		}
		if out["location"] == "" && metadata["resume_field"] == "location" {
			if ans := strings.TrimSpace(metadata["resume_answer"]); ans != "" {
				out["location"] = ans
			}
		}
	}
	if _, ok := out["location"]; !ok {
		if loc := locationFromText(msg); loc != "" {
			out["location"] = loc
		}
	}
	return out
}

// hereRefRE matches location references that mean "where I am" rather than a
// named place: "here", "my location", "near me", "around here", a bare
// "nearby", or "local" attached to a place noun. When one matches and no
// explicit place is present, the runtime resolves it against device metadata
// first, then the user's stored location, before ever asking.
var hereRefRE = regexp.MustCompile(`(?i)\b(here|my location|my city|my town|my area|my place|where i am|where i live|near me|around here|around me|close by|local weather|local aqi|weather here|here\?)\b|\bnearby\s*[?.!]?$`)

// locationRefersHere reports whether the message asks about the user's own
// location without naming a place.
func locationRefersHere(msg string) bool {
	return hereRefRE.MatchString(strings.TrimSpace(msg))
}

// backRefRE matches an anaphoric place reference: the owner is pointing back
// at something the conversation just established — "weather like there",
// "parking near that city". The message names no place, so no stored or
// device location can resolve it, and substituting one would answer a
// different place than the one on the table.
var backRefRE = regexp.MustCompile(`(?i)\b(?:there|over\s+there|that\s+(?:one|city|place|town|country|area|region|spot)|same\s+(?:place|city|town))\b`)

// locationRefersBack reports whether the message locates itself by pointing
// at prior conversation rather than naming a place. Such a request must
// reach the model, which holds the history: the deterministic fast-path
// would otherwise ask a question the previous turn already answered.
func locationRefersBack(msg string) bool {
	return backRefRE.MatchString(strings.TrimSpace(msg))
}

// knownLocation returns the user's stored city/place (personal context,
// current, in-scope), or "". It lets "here" resolve without another
// round-trip: the user already told Ghost where they are.
func (al *AgentLoop) knownLocation(session string) string {
	if al == nil || al.pcStore == nil {
		return ""
	}
	var general []string
	for _, e := range al.pcStore.CurrentInScope(al.sessionScopes(session)) {
		if e.Status != "" && e.Status != personalcontext.StatusCurrent {
			continue
		}
		switch e.Predicate {
		case "fact/location", "identity/location", "fact/city", "identity/city",
			"fact/home", "identity/home", "fact/country", "identity/country":
			if v := strings.TrimSpace(personalcontext.Value(e)); v != "" {
				return v
			}
		case "fact/general":
			if v := strings.TrimSpace(personalcontext.Value(e)); v != "" {
				general = append(general, v)
			}
		}
	}
	// General facts sometimes hold the location inline ("I am currently in
	// Phang-Nga"). The "in <Place>" heuristic extracts it.
	for _, g := range general {
		if loc := locationFromText(g); loc != "" {
			return loc
		}
	}
	return ""
}

// supersedeSemanticCorrection retires a same-belief current entry when a new
// current-status semantic extraction contradicts it, returning true when it
// did. Guards: the conflicting row must be visible in the caller's scopes
// AND be the unambiguous store-wide current, so a personal turn can never
// retire a work-scoped fact it cannot see. Uncertain extractions never
// supersede — a candidate must not evict a belief.
func (al *AgentLoop) supersedeSemanticCorrection(current []personalcontext.Entry, entry personalcontext.Entry) bool {
	if al == nil || al.pcStore == nil {
		return false
	}
	var conflict *personalcontext.Entry
	for i := range current {
		ce := &current[i]
		if ce.Status == personalcontext.StatusCurrent && ce.Subject == entry.Subject &&
			ce.Predicate == entry.Predicate &&
			personalcontext.Value(*ce) != personalcontext.Value(entry) {
			conflict = ce
			break
		}
	}
	if conflict == nil {
		return false
	}
	for _, ce := range al.pcStore.Current() {
		if ce.Subject == entry.Subject && ce.Predicate == entry.Predicate &&
			ce.Status == personalcontext.StatusCurrent && ce.ID != conflict.ID {
			return false
		}
	}
	if _, err := al.pcStore.Supersede(entry.Subject, entry.Predicate, entry); err != nil {
		logger.WarnCF("agent", "semantic correction supersede failed", map[string]interface{}{
			"predicate": entry.Predicate, "error": err.Error(),
		})
		return false
	}
	logger.InfoCF("agent", "semantic correction superseded prior belief", map[string]interface{}{
		"predicate": entry.Predicate, "retired": conflict.ID,
	})
	return true
}

// locationWithMemoryFallback fills a missing location from a "here"-style
// reference resolved against the user's stored place. It returns the inputs
// unchanged when an explicit location is already present.
func (al *AgentLoop) locationWithMemoryFallback(msg, session string, inputs map[string]string) map[string]string {
	if strings.TrimSpace(inputs["location"]) != "" {
		return inputs
	}
	if !locationRefersHere(msg) {
		return inputs
	}
	if loc := al.knownLocation(session); loc != "" {
		inputs["location"] = loc
	}
	return inputs
}

var flightNumberRE = regexp.MustCompile(`\b([A-Za-z]{2}\d{1,4}|[A-Z]{2}\s\d{1,4})\b`)

// futureIntentWords mark forecast-style asks. Ghost's provider-backed
// capability is CURRENT conditions; a forecast ask must never be answered
// with fabricated numbers, so it returns an honest product limitation.
var futureIntentWords = []string{"tomorrow", "forecast", "next week", "this weekend", "weekend", "on friday", "on saturday", "on sunday", "on monday", "later today", "tonight"}

// hasFutureIntent reports whether the request asks about a time the
// current-conditions capability cannot truthfully answer.
func hasFutureIntent(lower string) bool {
	for _, w := range futureIntentWords {
		if strings.Contains(lower, w) {
			return true
		}
	}
	return false
}

// tryDeterministicNetworkDispatch enforces evidence-based answers for
// unambiguous network capabilities with complete inputs: the runtime
// executes the provider-backed tool (validated output) instead of letting
// the model invent live data. Returns (answer, handled). "handled" means
// the turn is done — either with a validated result or an honest
// product-language failure/limitation, never a fabricated claim.
func (al *AgentLoop) tryDeterministicNetworkDispatch(msg, session string, metadata map[string]string) (string, bool) {
	lower := strings.ToLower(strings.TrimSpace(msg))
	if lower == "" || isSecurityProbe(msg) {
		return "", false
	}
	// The quick path exists for a single, plain ask. A message with several
	// asks ("check the weather there and the aqi, also send me the news about
	// bangkok") went here, took the last place named as the city, answered one
	// of the three, and left the owner to notice and ask again. Anything longer
	// or wider than one ask belongs to the model.
	if !isSingleAsk(lower) {
		return "", false
	}
	inputs := al.locationWithMemoryFallback(msg, session, capabilityInputsFromMessage(msg, metadata))
	loc := strings.TrimSpace(inputs["location"])

	// Weather / AQI current conditions with a location -> dispatch the
	// provider tool. Missing location is handled by the readiness
	// fast-path (it asks, durably). Forecast-style asks are out of the
	// capability's scope: answer honestly, never fabricate.
	if isWeatherIntent(lower) || isAQIIntent(lower) {
		isAQI := isAQIIntent(lower)
		if isAQI && hasFutureIntent(lower) {
			return "I can check current air quality, not a forecast for it yet.", true
		}
		if isWeatherIntent(lower) && hasFutureIntent(lower) {
			// weather_now reaches 16 days ahead; the model picks the date.
			return "", false
		}
		if loc == "" {
			return "", false // readiness fast-path owns the "which city" ask
		}
		if !currentConditionsAsk(lower, loc) {
			return "", false
		}
		tool := "weather_now"
		if isAQI {
			tool = "aqi_now"
		}
		ans, handled := al.execDeterministicTool(tool, map[string]interface{}{"location": loc}, session)
		// The place was guessed from the sentence, not given. A miss means the
		// guess may be wrong, so the model (with history and its own reading of
		// the sentence) gets the turn. A place the owner typed in answer to our
		// question is final.
		if handled && metadata["resume_field"] == "" && strings.HasPrefix(ans, "I couldn't find a place called") {
			return "", false
		}
		return ans, handled
	}

	// Flight status with a number + configured provider -> dispatch.
	if isFlightIntent(lower) {
		fn := strings.TrimSpace(inputs["flight_number"])
		if fn == "" {
			return "", false // readiness fast-path owns the ask
		}
		if credentials.AviationKey(nil) == "" && credentials.AeroDataBoxKey() == "" {
			return "Flight tracking isn't connected yet. Add your flight data key in Ghost settings under Integrations, then try again — I won't guess flight data.", true
		}
		return al.execDeterministicTool("flight_status", map[string]interface{}{"flight_number": fn}, session)
	}

	return "", false
}

// execDeterministicTool runs a registered provider-backed tool with the
// given args and returns its text (validated data, or an honest
// product-language error with model-only guidance stripped). handled=true
// means the runtime produced the answer — never fabricated.
func (al *AgentLoop) execDeterministicTool(name string, args map[string]interface{}, session string) (string, bool) {
	if al.tools == nil {
		return "", false
	}
	if _, ok := al.tools.Get(name); !ok {
		return "", false
	}
	res := al.tools.Execute(context.Background(), name, args)
	failed := res == nil || res.IsError
	// Emit canonical capability events so runtime evidence matches what the
	// user is told, exactly like the model-driven tool path does.
	if al.governance != nil && al.governance.Events != nil {
		capID := deterministicCapability(name)
		al.governance.ToolRan("", session, name, "", failed, nil)
		al.governance.CapabilityDone("", session, capID, "", failed)
	}
	if res == nil {
		return "I couldn't get that right now. Please try again in a bit.", true
	}
	if res.IsError {
		// The tool returns product-language errors; never pass a raw
		// stack through. This path answers the user directly — there is
		// no model to interpret guidance aimed at one — so strip any
		// model-only suffix (UserFacing) instead of showing ForLLM.
		if text := res.UserFacing(); text != "" {
			return text, true
		}
		return "I couldn't get that right now. Please try again in a bit.", true
	}
	text := strings.TrimSpace(res.ForLLM)
	if text == "" && res.Err != nil {
		text = res.Err.Error()
	}
	if text == "" {
		return "I couldn't get that right now. Please try again in a bit.", true
	}
	return text, true
}

var locationFromTextRE = regexp.MustCompile(`(?i)\b(?:in|near|around|close to|next to|of|for|at)\s+([A-Z][a-zA-Z'’\-]*(?:[\s\-][A-Z][a-zA-Z'’\-]*){0,3})`)

// herePhraseRE matches captures that reference the user's OWN location rather
// than a named place. The preposition regex is case-insensitive, so "weather
// in my city" captures the phrase "my city" — sending that to a geocoder
// guarantees a no-results failure and never the user's actual location. Such
// captures return "" so locationWithMemoryFallback resolves them through the
// stored location (knownLocation), or the readiness fast-path asks once.
var herePhraseRE = regexp.MustCompile(`(?i)^(?:me|here|my\s+(?:city|town|location|area|place)|this\s+(?:city|place)|where\s+i\s+(?:am|live))$`)

// placeFrameRE finds a place word and what follows it. namesAPlace uses it to
// tell "weather" (nothing named) from "weather in <something we could not
// parse>" (something named).
var placeFrameRE = regexp.MustCompile(`(?i)\b(?:in|at|of|for|near|around|to|from)\s+([\p{L}'’\-]+)`)

// namesAPlace reports whether the message points at a named thing after a
// place word. Self-references ("near me", "here", "my city") do not count:
// those resolve against the stored location.
func namesAPlace(lower string) bool {
	for _, m := range placeFrameRE.FindAllStringSubmatch(lower, -1) {
		switch m[1] {
		case "me", "here", "my", "this", "today", "now", "tonight", "tomorrow", "a", "an":
			continue
		}
		return true
	}
	return false
}

var locationStopRE = regexp.MustCompile(`(?i)\s(?:and|its|it's|with|plus|also|then|or|aqi|aqo|weather|temperature|air|please|pls|right|today|now|tonight|tomorrow)\b`)

func locationFromText(msg string) string {
	m := locationFromTextRE.FindStringSubmatch(msg)
	if m == nil {
		return ""
	}
	loc := strings.TrimSpace(m[1])
	// The pattern is case-insensitive, so the capture runs on into whatever
	// follows the place ("nairobi and its aqi"). Cut at the first word that
	// cannot be part of a place name.
	if cut := locationStopRE.FindStringIndex(" " + loc); cut != nil && cut[0] > 0 {
		loc = strings.TrimSpace(loc[:cut[0]-1])
	}
	// Trim trailing verbs that leaked in. Longest phrases first so
	// " right now" wins over " now", and repeat until stable.
	stops := []string{" right now", " this afternoon", " this morning", " tomorrow", " today", " now", " later", " please", " one line", " asap"}
	for {
		trimmed := loc
		for _, stop := range stops {
			if idx := strings.Index(strings.ToLower(trimmed), stop); idx >= 0 {
				candidate := strings.TrimSpace(trimmed[:idx])
				if len(candidate) >= 3 {
					trimmed = candidate
				}
			}
		}
		if trimmed == loc {
			break
		}
		loc = trimmed
	}
	loc = strings.TrimRight(loc, " .,!?;:")
	if len(loc) < 3 || len(loc) > 40 {
		return ""
	}
	if herePhraseRE.MatchString(loc) {
		return ""
	}
	return loc
}

// deterministicCapability maps a deterministic tool to its capability id.
func deterministicCapability(tool string) string {
	switch tool {
	case "weather_now":
		return "weather.current"
	case "aqi_now":
		return "aqi.current"
	case "flight_status":
		return "flight.status"
	default:
		return tool
	}
}

// rememberHomeLocation records the place the owner just gave us so "here"
// resolves on later turns instead of asking again. Best-effort and quiet:
// failing to remember must never break the answer.
func (al *AgentLoop) rememberHomeLocation(session, loc, message string) {
	if al == nil || al.pcStore == nil {
		return
	}
	loc = strings.TrimSpace(loc)
	if loc == "" {
		return
	}
	value, err := personalcontext.RawValue(loc)
	if err != nil {
		return
	}
	_, err = al.pcStore.Supersede("user", "fact/location", personalcontext.Entry{
		ID:         personalcontext.NewEntryID(),
		Kind:       personalcontext.KindFact,
		Subject:    "user",
		Predicate:  "fact/location",
		Value:      value,
		Status:     personalcontext.StatusCurrent,
		Scopes:     al.sessionScopes(session),
		Confidence: 0.9,
		Sources: []personalcontext.Source{{
			Type: personalcontext.SourceConversation, Kind: personalcontext.SourceUserDeclared,
			Ref: session, Timestamp: time.Now().UTC(),
		}},
		Quote: strings.TrimSpace(message),
	})
	if err != nil {
		// First time there is nothing to supersede: create the belief.
		_, _ = al.pcStore.Create(personalcontext.Entry{
			ID:         personalcontext.NewEntryID(),
			Kind:       personalcontext.KindFact,
			Subject:    "user",
			Predicate:  "fact/location",
			Value:      value,
			Status:     personalcontext.StatusCurrent,
			Scopes:     al.sessionScopes(session),
			Confidence: 0.9,
			Sources: []personalcontext.Source{{
				Type: personalcontext.SourceConversation, Kind: personalcontext.SourceUserDeclared,
				Ref: session, Timestamp: time.Now().UTC(),
			}},
			Quote: strings.TrimSpace(message),
		})
	}
}

// multiAskRE marks a second ask riding along with the first.
var multiAskRE = regexp.MustCompile(`(?i)\b(also|and then|plus|as well|on top of that|after that|then)\b`)

// isSingleAsk reports whether a message is one plain request that a
// deterministic tool can answer completely: one sentence, short, no second
// ask attached, and not two capabilities at once (weather and air quality
// together would answer only one).
func isSingleAsk(lower string) bool {
	lower = strings.TrimSpace(lower)
	if len(strings.Fields(lower)) > 16 || multiAskRE.MatchString(lower) {
		return false
	}
	// More than one sentence means more than one thing was said.
	sentences := 0
	for _, part := range regexp.MustCompile(`[.!?]+(?:\s|$)`).Split(lower, -1) {
		if strings.TrimSpace(part) != "" {
			sentences++
		}
	}
	if sentences > 1 {
		return false
	}
	mentionsWeather := strings.Contains(lower, "weather") || strings.Contains(lower, "temperature")
	mentionsAir := strings.Contains(lower, "aqi") || strings.Contains(lower, "air quality")
	return !(mentionsWeather && mentionsAir)
}
