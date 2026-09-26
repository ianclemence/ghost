package agent

import (
	"regexp"
	"strings"
)

// Memory retrieval gating.
//
// Measured on the live device: one local embedding call costs ~2.8 s and is
// input-independent (1 character and 200 words both cost the same), and the
// turn loop performed one on EVERY interactive message — including "hey
// Ghost". The vector search itself is sub-millisecond; the embedding is the
// entire cost.
//
// Ghost already exposes `memory_recall` and `session_search` as ordinary
// tools, so a turn that genuinely needs memory and does not get it
// auto-injected can still fetch it. That fallback is what makes a
// conservative gate safe: the worst case is one extra tool call on a rare
// message, against ~2.8 s saved on almost every message.
//
// The gate is deterministic, documented, and biased toward retrieving
// whenever the owner's words could plausibly depend on durable memory.

var (
	// Explicit recall intent: the owner is asking for something to be
	// remembered back to them.
	recallIntentRE = regexp.MustCompile(`(?i)\b(` +
		`do you (?:remember|know|recall)|you remember|you know about|` +
		`remind me (?:what|who|when|where|how|why)|` +
		`what did (?:i|we|you)|what was (?:i|the|our|my)|what were|` +
		`what have (?:i|we)|tell me (?:what|about)|` +
		`recall|remember when|previously|earlier|last (?:week|month|year|time|night|conversation)|` +
		`yesterday|the other day|we (?:discussed|decided|agreed|talked)|` +
		`i (?:told|said|mentioned|asked) you|have i (?:told|said|mentioned)|` +
		`did i (?:tell|say|mention)|back then|at the time|` +
		`what do i (?:usually|always|normally|like|prefer)|` +
		`my (?:notes|preferences|profile|history)` +
		`)\b`)

	// A possessive reference to something the owner owns and Ghost may have
	// remembered. The noun is checked against runtime state below, so "my
	// reminders" never reaches memory retrieval.
	myFactRE = regexp.MustCompile(`(?i)\bmy\s+([a-z][a-z'\-]*)`)

	// Runtime-state nouns: these are answered from authoritative state (the
	// deterministic state fast path) and must never trigger a memory lookup.
	stateNounWords = `reminders?|tasks?|todos?|schedules?|routines?|automations?|jobs?|` +
		`proposals?|suggestions?|approvals?|activity|status|health|disk|space|` +
		`model|version|uptime|permissions?|grants?|proactive|shopping|` +
		`list|screen|cart`
	// stateNounRE matches a single runtime-state noun on its own, used to test
	// the noun captured from "my <noun>".
	stateNounRE = regexp.MustCompile(`(?i)^(?:` + stateNounWords + `)$`)
	// stateNounAnywhereRE matches runtime-state vocabulary anywhere in the
	// message: naming live state means the answer comes from that state.
	stateNounAnywhereRE = regexp.MustCompile(`(?i)\b(?:` + stateNounWords + `)\b`)

	// A question about what Ghost is currently doing or just did. That is
	// runtime activity, which the state fast path answers from the event
	// stream — never from durable memory.
	activityProbeRE = regexp.MustCompile(`(?i)\b(` +
		`what did you (?:just )?do|what have you been (?:doing|up to)|` +
		`what are you doing|what did you just|what's happening` +
		`)\b`)

	// An imperative request is an action, not a question about the past.
	// Checked after explicit recall intent, so "remind me what we decided"
	// still retrieves.
	actionImperativeRE = regexp.MustCompile(`(?i)^\s*(` +
		`remind me|send|email|message|call|add|set|create|delete|remove|cancel|` +
		`schedule|move|reschedule|book|pay|buy|order|turn|enable|disable|` +
		`list|show|open|run|start|stop|pause|resume|update|write|save|make` +
		`)\b`)

	// Offline/greeting/acknowledgement traffic: never worth an embedding.
	trivialRE = regexp.MustCompile(`(?i)^\s*(` +
		`hi|hey|hello|yo|hiya|thanks|thank you|ta|ok|okay|cool|nice|great|` +
		`good morning|good evening|good night|bye|goodbye|cheers|got it|` +
		`yes|no|yep|nope|sure|please|stop|cancel` +
		`)[\s!.,]*$`)
)

// memoryRetrievalNeeded reports whether a turn should pay for a semantic
// memory lookup before answering.
//
// Retrieval is justified when the owner is explicitly asking Ghost to recall
// something, or when the message refers to something the owner owns that is
// not runtime state. Everything else — greetings, actions, tool requests, and
// questions whose answer is authoritative runtime state — proceeds without an
// embedding.
func memoryRetrievalNeeded(query string) bool {
	q := strings.TrimSpace(query)
	if q == "" {
		return false
	}
	lower := strings.ToLower(q)

	// A bare greeting or acknowledgement never needs durable memory.
	if trivialRE.MatchString(lower) {
		return false
	}

	// "What are you doing / what did you just do" is live runtime activity,
	// and outranks the recall patterns because it can only ever mean that.
	if activityProbeRE.MatchString(lower) {
		return false
	}

	// Explicit recall intent is always honoured.
	if recallIntentRE.MatchString(lower) {
		return true
	}

	// An imperative is an action Ghost performs, not a memory question.
	if actionImperativeRE.MatchString(lower) {
		return false
	}

	// A message that names runtime state anywhere is answered from that state.
	if stateNounAnywhereRE.MatchString(lower) {
		return false
	}

	// "my <thing>" is a reference to durable owner state unless the thing is
	// something Ghost answers from live runtime state.
	if m := myFactRE.FindStringSubmatch(lower); m != nil && !stateNounRE.MatchString(m[1]) {
		return true
	}
	return false
}
