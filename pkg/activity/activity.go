// Package activity projects canonical events into the human-facing
// activity narrative: "What is Ghost doing for me?"
//
// Borrowed pattern (OpenMausBot): streaming replies with tool-run
// activity chips — compact cards (◌ running / ✓ done / ! waiting /
// × failed) that expand for detail. Ghost derives chips from SAFE
// canonical events only: raw tool names, manifests, schemas, prompts,
// and secrets can never reach a chip (structural, not prompt-based).
//
// Three detail layers:
//  1. Chip:  "Checked the weather"
//  2. Expanded: "Weather · Open-Meteo · updated 2 minutes ago"
//  3. Diagnostics (explicit opt-in only): provider, latency, request_id
package activity

import (
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/utils"
)

// State is the chip lifecycle.
type State string

const (
	StateRunning   State = "running"
	StateWaiting   State = "waiting"
	StateSuccess   State = "success"
	StateFailed    State = "failed"
	StateCancelled State = "cancelled"
	StatePaused    State = "paused"
)

// Chip is the UI-safe activity card.
type Chip struct {
	ID        string    `json:"id"`
	EventID   string    `json:"event_id"`
	Seq       int64     `json:"seq"`
	Title     string    `json:"title"`
	Kind      string    `json:"kind"`
	State     State     `json:"state"`
	Timestamp time.Time `json:"timestamp"`
	Summary   string    `json:"summary,omitempty"`
	Detail    string    `json:"detail,omitempty"` // layer 2: safe expanded text
	// Diagnostic is the raw technical text (tool error, query, JSON) that
	// used to be shown as the summary. It stays available behind a
	// "Details" disclosure for people who want it; Summary never carries it.
	Diagnostic string `json:"diagnostic,omitempty"`
	// Why explains, in owner language, why Ghost acted or asked. It is the
	// revelation layer: the owner should never have to wonder why a
	// consequential action happened. Empty for events that need no
	// justification (routine housekeeping, reads).
	Why string `json:"why,omitempty"`
}

// humanTitles maps event types to product-narrative titles. Unknown types
// deliberately yield "" (no chip) rather than leaking the raw type name.
var humanTitles = map[cevents.Type]string{
	cevents.MessageReceived: "New message",
	cevents.MessageCreated:  "Reply sent",
	cevents.AgentStarted:    "Working on it",
	cevents.AgentProgress:   "Working on it",
	cevents.AgentWaiting:    "Waiting for you",
	// AgentCompleted is deliberately absent: a turn ending is a fact about
	// the runtime, never about what Ghost did for the owner. It carries no
	// payload, so it can only ever render as a bare "Done" — and one of
	// those per turn drowned every real row in the feed.
	cevents.AgentFailed:         "Couldn't finish that",
	cevents.CapabilityStarted:   "Starting",
	cevents.CapabilityCompleted: "Finished a step",
	cevents.CapabilityFailed:    "Unavailable right now",
	cevents.ToolCompleted:       "Finished a step",
	cevents.ToolFailed:          "Step failed",
	// A decision is only readable once the thing decided on is named:
	// "Approved" begs the question; "You approved: Send email" answers it.
	// Project appends the subject from the capability registry.
	cevents.PermissionRequested:     "Waiting for approval",
	cevents.PermissionApproved:      "You approved",
	cevents.PermissionDenied:        "You declined",
	cevents.PermissionExpired:       "Approval expired",
	cevents.MemoryCreated:           "Remembered",
	cevents.MemoryUpdated:           "Memory updated",
	cevents.MemoryDeleted:           "Memory removed",
	cevents.IntegrationConnected:    "Connected",
	cevents.IntegrationDisconnected: "Disconnected",
	cevents.IntegrationExpired:      "Needs reconnecting",
	cevents.IntegrationFailed:       "Connection failed",
	cevents.RoutineCreated:          "Routine scheduled",
	cevents.RoutineStarted:          "Routine running",
	cevents.RoutineWaiting:          "Routine waiting",
	cevents.RoutineCompleted:        "Routine done",
	cevents.RoutineFailed:           "Routine failed",
	cevents.GhostReady:              "Ghost is ready",
	cevents.GhostDegraded:           "Running with limits",
	cevents.GhostOffline:            "Ghost is offline",
	cevents.GhostRecovering:         "Ghost is recovering",
	cevents.OperationFailed:         "Couldn't complete that",
	cevents.SkillInstalled:          "Installed skill",
	cevents.SkillEnabled:            "Enabled skill",
	cevents.SkillDisabled:           "Disabled skill",
	cevents.SkillUpdated:            "Updated skill",
	cevents.SkillRemoved:            "Removed skill",
	cevents.ProactivePresented:      "Ghost noticed something",
	cevents.ProactiveApproved:       "You approved it",
	cevents.ProactiveDenied:         "You declined it",
	cevents.ProactiveDismissed:      "You dismissed it",
	cevents.ProactiveSnoozed:        "Snoozed",
	cevents.ProactiveExpired:        "Suggestion expired",
	cevents.ProactiveSuperseded:     "Situation changed",
	cevents.ProactiveCompleted:      "Handled it",
	cevents.ProactiveFailed:         "Couldn't handle it",
	cevents.CommitmentCreated:       "You promised",
	cevents.CommitmentCompleted:     "Promise kept",
	cevents.CommitmentFailed:        "Promise still open",
	cevents.CommitmentBlocked:       "Promise blocked",
	cevents.ReminderDelivered:       "Reminded you",
	cevents.ReminderMissed:          "Missed a reminder",
	cevents.ReminderFailed:          "Couldn't send a reminder",
	cevents.ReminderDone:            "Done",
	cevents.ReminderSnoozed:         "Snoozed",
	cevents.ReminderDismissed:       "Put away",
	cevents.DigestDelivered:         "Morning summary",
}

// skillNameTitle refines skill lifecycle chips with the skill name when the
// publisher provided one (user-safe text only; never paths).
func skillNameTitle(e *cevents.Event, base string) string {
	name, _ := e.Payload["name"].(string)
	if name == "" {
		return base
	}
	return base + ": " + name
}

// capabilityIDOf reads the canonical capability identity off an event. It
// tolerates the two key spellings publishers have used, and then — because a
// tool row usually records only the tool it ran — resolves that tool back to
// the capability which owns it. That last step is what lets a bare
// tool.completed row name its subject ("Searched the web") instead of having
// no subject at all. The tool identifier itself never leaves this function.
func capabilityIDOf(e *cevents.Event) string {
	if cap, _ := e.Payload["capability"].(string); cap != "" {
		return cap
	}
	if cap, _ := e.Payload["capability_id"].(string); cap != "" {
		return cap
	}
	if tool, _ := e.Payload["tool"].(string); tool != "" {
		if spec, ok := capability.ForTool(tool); ok {
			return spec.ID
		}
	}
	return ""
}

// activityPhrase is how one domain reads in owner language, per phase.
type activityPhrase struct {
	doing string // the work started
	done  string // the work finished
	fail  string // the work did not finish
}

// capabilityPhrases is the owner-language title table, keyed by substring
// of the capability id. Substring matching is deliberate: the runtime has
// used weather.current while the registry says weather.get, and the owner
// reads domains ("your weather", "a web page"), never identifiers. Ordered
// most specific first — the first match wins.
//
// This table exists so no capability can ever fall through to a bare "Done".
// Anything it does not know falls back to the capability registry's own
// owner-facing title, and only then to a neutral phrase.
var capabilityPhrases = []struct {
	match string
	p     activityPhrase
}{
	// Reads that were already phrased before this table existed — these
	// exact strings are load-bearing (tests pin them).
	{"weather", activityPhrase{"Checking the weather", "Weather checked", "Weather unavailable"}},
	// calendar.modify is deliberately ahead of the generic calendar row: it
	// CHANGES the owner's calendar, and "Calendar checked" would be a factual
	// misstatement of what Ghost did to their day.
	{"calendar.modify", activityPhrase{"Changing your calendar", "Calendar updated", "Couldn't update your calendar"}},
	{"calendar", activityPhrase{"Checking your calendar", "Calendar checked", "Calendar unavailable"}},
	// memory.remember and memory.forget come before the generic memory row:
	// they write, they do not search, and "Memory searched" for a fact Ghost
	// just stored would name the wrong action.
	{"memory.remember", activityPhrase{"Remembering that", "Remembered it", "Couldn't remember that"}},
	{"memory.forget", activityPhrase{"Forgetting that", "Forgot it", "Couldn't forget that"}},
	{"memory.summarize", activityPhrase{"Condensing a conversation", "Conversation condensed", "Couldn't condense that"}},
	{"memory", activityPhrase{"Searching your memory", "Memory searched", "Couldn't search your memory"}},
	{"reminder", activityPhrase{"Setting a reminder", "Reminder created", "Couldn't set that reminder"}},
	{"flight", activityPhrase{"Checking your flight", "Flight checked", "Flight data unavailable"}},

	// Online.
	{"web.search", activityPhrase{"Searching the web", "Searched the web", "Web search didn't finish"}},
	{"web.fetch", activityPhrase{"Reading a web page", "Read a web page", "Couldn't read that page"}},
	{"scraper", activityPhrase{"Reading a web page", "Read a web page", "Couldn't read that page"}},
	{"web", activityPhrase{"Looking something up online", "Looked something up online", "Online lookup didn't finish"}},

	// Files and code.
	{"file.read", activityPhrase{"Reading your files", "Read a file", "Couldn't read that file"}},
	{"file.write", activityPhrase{"Saving a file", "Saved a file", "Couldn't save that file"}},
	{"file", activityPhrase{"Looking at your files", "Looked at your files", "Couldn't read that file"}},
	{"docs", activityPhrase{"Searching your docs", "Searched your docs", "Docs search didn't finish"}},
	{"repository", activityPhrase{"Searching your code", "Searched your code", "Code search didn't finish"}},
	{"code", activityPhrase{"Reading your code", "Read your code", "Couldn't read that code"}},

	// Executing. exec.sandbox and mcp.execute sit ahead of the generic exec
	// row because both contain the letters "exec" and would otherwise inherit
	// "ran a command" for work that is not a shell command.
	{"exec.sandbox", activityPhrase{"Running some code", "Ran some code", "Code didn't finish"}},
	{"mcp.execute", activityPhrase{"Using an outside tool", "Used an outside tool", "Outside tool didn't finish"}},
	{"exec", activityPhrase{"Running a command", "Ran a command", "Command didn't finish"}},
	{"sandbox", activityPhrase{"Running some code", "Ran some code", "Code didn't finish"}},
	{"system.update", activityPhrase{"Updating your Ghost", "Ghost updated", "Update didn't finish"}},

	// Scheduling and routines.
	{"schedule.cancel", activityPhrase{"Cancelling it", "Cancelled it", "Couldn't cancel that"}},
	{"schedule.modify", activityPhrase{"Changing it", "Changed it", "Couldn't change that"}},
	{"schedule", activityPhrase{"Scheduling it", "Scheduled it", "Couldn't schedule that"}},
	{"routine.cancel", activityPhrase{"Stopping a routine", "Routine stopped", "Couldn't stop that routine"}},
	{"routine", activityPhrase{"Working on a routine", "Routine updated", "Routine step didn't finish"}},
	{"goal", activityPhrase{"Updating your goals", "Goal updated", "Couldn't update that goal"}},

	// Lookups.
	{"aqi", activityPhrase{"Checking the air quality", "Air quality checked", "Couldn't check the air quality"}},
	{"air", activityPhrase{"Checking the air quality", "Air quality checked", "Couldn't check the air quality"}},
	{"places", activityPhrase{"Looking up nearby places", "Nearby places found", "Couldn't look that up"}},
	{"nearby", activityPhrase{"Looking up nearby places", "Nearby places found", "Couldn't look that up"}},
	{"currency", activityPhrase{"Checking the exchange rate", "Exchange rate checked", "Couldn't check the rate"}},
	{"crypto", activityPhrase{"Checking the crypto price", "Crypto price checked", "Couldn't check the price"}},

	// Outbound.
	{"email.send", activityPhrase{"Sending an email", "Sent an email", "Email wasn't sent"}},
	{"email", activityPhrase{"Checking your email", "Checked your email", "Couldn't read your email"}},
	{"message", activityPhrase{"Sending a message", "Sent a message", "Message wasn't sent"}},
	{"telegram", activityPhrase{"Sending a message", "Sent a message", "Message wasn't sent"}},
	{"whatsapp", activityPhrase{"Sending a message", "Sent a message", "Message wasn't sent"}},

	// The world around the owner. device.io is ahead of the generic device
	// row because it writes to hardware — "Checked your devices" would report
	// a read for work that changed something.
	{"device.io", activityPhrase{"Using a device", "Used a device", "Device didn't respond"}},
	{"device.control", activityPhrase{"Changing a device", "Changed a device", "Device didn't respond"}},
	{"device", activityPhrase{"Checking your devices", "Checked your devices", "Couldn't reach your devices"}},
	{"hass", activityPhrase{"Checking your devices", "Checked your devices", "Couldn't reach your devices"}},
	{"browser", activityPhrase{"Using the browser", "Used the browser", "Browser step didn't finish"}},
	{"computer", activityPhrase{"Looking at your screen", "Looked at your screen", "Couldn't read your screen"}},

	// Money and media.
	{"payment", activityPhrase{"Making a payment", "Made a payment", "Payment didn't go through"}},
	{"wallet", activityPhrase{"Making a payment", "Made a payment", "Payment didn't go through"}},
	{"charge", activityPhrase{"Making a payment", "Made a payment", "Payment didn't go through"}},
	{"media", activityPhrase{"Playing media", "Played media", "Couldn't play that"}},
	{"playback", activityPhrase{"Playing media", "Played media", "Couldn't play that"}},
	{"artifact", activityPhrase{"Publishing something for you", "Published something for you", "Couldn't publish that"}},

	// Housekeeping and extensions.
	{"skill", activityPhrase{"Updating your skills", "Skill updated", "Couldn't update that skill"}},
	{"mcp", activityPhrase{"Using an outside tool", "Used an outside tool", "Outside tool didn't finish"}},
}

// phraseFor looks up the owner-language phrasing for a capability id.
func phraseFor(cap string) (activityPhrase, bool) {
	for _, entry := range capabilityPhrases {
		if strings.Contains(cap, entry.match) {
			return entry.p, true
		}
	}
	return activityPhrase{}, false
}

// capabilityTitle turns a capability id into the owner-language row title
// for this event's phase. Layer 1 is the phrase table above; layer 2 is the
// capability registry's own curated title (single source of truth, so an
// unphrased capability still reads as a noun phrase the owner recognises);
// layer 3 is the neutral base. There is no path that returns a bare "Done".
//
// The second return reports whether layer 1 answered — layer 2 then knows not
// to repeat itself in the detail line.
func capabilityTitle(e *cevents.Event, base string) (string, bool) {
	cap := capabilityIDOf(e)
	if cap == "" {
		return base, false
	}
	if p, ok := phraseFor(cap); ok {
		switch e.Type {
		case cevents.CapabilityStarted:
			return p.doing, true
		case cevents.CapabilityFailed, cevents.ToolFailed:
			// A failed run must be worded as the failure it is: the pill
			// says "Failed", so a done-form title reads as a contradiction.
			return p.fail, true
		default:
			return p.done, true
		}
	}
	if spec, ok := capability.Get(cap); ok && spec.Title != "" {
		return spec.Title, false
	}
	return base, false
}

// permissionSubject names the thing the owner decided on, in owner language.
// The capability registry's title is imperative ("Send email", "Read calendar"),
// which is exactly the right shape for a decision line; the phrase table's
// doing-form covers capabilities the registry does not carry. The payload's
// own "summary" is never used here — publishers write plumbing into it
// ("exec.shell via exec"), which is not a sentence for the owner.
func permissionSubject(e *cevents.Event) string {
	cap := capabilityIDOf(e)
	if cap == "" {
		return ""
	}
	if spec, ok := capability.Get(cap); ok && spec.Title != "" {
		return spec.Title
	}
	if p, ok := phraseFor(cap); ok {
		return p.doing
	}
	return ""
}

// WithoutAnsweredApprovals drops permission.asked events whose request the
// owner has already settled, given the set of requests which are still open
// (permissions.OpenRequests). Passing nil means the answer is unknown — nothing
// is filtered then, because a stale row is wrong but guessing is worse.
//
// An activity row is a claim about the present as much as the past: "Waiting
// for approval" is only true while the request is open. The canonical event
// records that Ghost asked — the answer is a separate event — so replaying the
// ask afterwards as a live status tells the owner eight things need them when
// none do. The answer event carries the decision instead.
//
// A request with no id cannot be checked either, so it is kept rather than
// silently deleted.
func WithoutAnsweredApprovals(events []*cevents.Event, openIDs map[string]bool) []*cevents.Event {
	if openIDs == nil {
		return events
	}
	out := make([]*cevents.Event, 0, len(events))
	for _, e := range events {
		if e == nil {
			continue
		}
		if e.Type == cevents.PermissionRequested && e.RequestID != "" && !openIDs[e.RequestID] {
			continue
		}
		out = append(out, e)
	}
	return out
}

// stateFor maps event types to chip states.
func stateFor(t cevents.Type, status string) State {
	switch t {
	case cevents.AgentWaiting, cevents.PermissionRequested, cevents.RoutineWaiting,
		cevents.ProactivePresented, cevents.ProactiveSnoozed, cevents.ProactiveExpired:
		return StateWaiting
	case cevents.ProactiveDenied, cevents.ProactiveDismissed, cevents.ProactiveSuperseded,
		cevents.ReminderDismissed:
		return StateCancelled
	// A refusal and an expiry both end the request; neither is still waiting
	// on anybody, and a "waiting" pill for a settled decision is a false
	// claim about the present.
	case cevents.PermissionDenied, cevents.PermissionExpired:
		return StateCancelled
	case cevents.CommitmentCreated, cevents.CommitmentFailed:
		return StateWaiting
	case cevents.CommitmentBlocked:
		return StateFailed
	case cevents.AgentCompleted, cevents.MessageCreated, cevents.CapabilityCompleted,
		cevents.ToolCompleted, cevents.PermissionApproved, cevents.MemoryCreated,
		cevents.MemoryUpdated, cevents.IntegrationConnected, cevents.RoutineCreated,
		cevents.RoutineCompleted, cevents.GhostReady,
		cevents.SkillInstalled, cevents.SkillUpdated, cevents.SkillEnabled,
		cevents.SkillDisabled, cevents.SkillRemoved,
		cevents.ProactiveApproved, cevents.ProactiveCompleted,
		cevents.CommitmentCompleted, cevents.ReminderDelivered,
		cevents.ReminderDone, cevents.ReminderSnoozed, cevents.DigestDelivered:
		return StateSuccess
	case cevents.AgentFailed, cevents.CapabilityFailed, cevents.ToolFailed,
		cevents.IntegrationFailed, cevents.RoutineFailed,
		cevents.OperationFailed, cevents.GhostOffline, cevents.ProactiveFailed,
		cevents.ReminderMissed, cevents.ReminderFailed:
		return StateFailed
	case cevents.IntegrationExpired:
		return StateWaiting
	case cevents.MessageReceived, cevents.AgentStarted, cevents.CapabilityStarted,
		cevents.RoutineStarted, cevents.GhostRecovering, cevents.GhostStarted:
		return StateRunning
	default:
		return StateRunning
	}
}

// Project converts one canonical event to a chip. It returns ok=false for
// events with no human narrative (raw tool internals, debug traces) —
// those never reach the activity UI.
func Project(e *cevents.Event) (*Chip, bool) {
	if e == nil || !e.Visibility.UserVisible() {
		return nil, false
	}
	title, ok := humanTitles[e.Type]
	if !ok || title == "" {
		return nil, false
	}
	// fromPhrase records that the owner-language table named this row, so the
	// detail layer knows the capability has already been said out loud.
	fromPhrase := false
	switch e.Type {
	case cevents.CapabilityStarted, cevents.CapabilityCompleted, cevents.CapabilityFailed,
		cevents.ToolCompleted, cevents.ToolFailed:
		// These rows exist to say WHAT Ghost did. With no subject there is
		// nothing to say, and an empty row is worse for the owner than no
		// row at all — which is how the feed used to fill with "Done".
		if capabilityIDOf(e) == "" {
			return nil, false
		}
		title, fromPhrase = capabilityTitle(e, title)
	case cevents.PermissionRequested, cevents.PermissionApproved,
		cevents.PermissionDenied, cevents.PermissionExpired:
		if subject := permissionSubject(e); subject != "" {
			title = title + ": " + subject
		}
	case cevents.MemoryCreated:
		if summary, _ := e.Payload["title"].(string); summary != "" {
			title = "Remembered: " + truncate(summary, 80)
		}
	case cevents.MemoryDeleted:
		if label, _ := e.Payload["title"].(string); label != "" {
			title = "Forgot: " + truncate(label, 80)
		}
	case cevents.IntegrationConnected:
		if name, _ := e.Payload["integration"].(string); name != "" {
			title = humanizeIntegration(name) + " connected"
		}
	case cevents.SkillInstalled, cevents.SkillEnabled, cevents.SkillDisabled,
		cevents.SkillUpdated, cevents.SkillRemoved:
		title = skillNameTitle(e, title)
	}
	chip := &Chip{
		ID: e.ID, EventID: e.ID, Seq: e.Seq, Title: title,
		Kind: string(e.Type), State: stateFor(e.Type, e.Status),
		Timestamp: e.Timestamp,
	}
	// A permission's recorded summary is publisher plumbing
	// ("exec.shell via exec"), not a sentence for the owner: the title and
	// the why-line already say what was asked, so it stays off the row.
	switch e.Type {
	case cevents.PermissionRequested, cevents.PermissionApproved,
		cevents.PermissionDenied, cevents.PermissionExpired:
	default:
		if s, _ := e.Payload["summary"].(string); s != "" {
			switch {
			case chip.State == StateFailed:
				chip.Summary = humanizeFailure(s)
				chip.Diagnostic = truncate(s, 400)
			case looksTechnical(s):
				chip.Diagnostic = truncate(s, 400)
			default:
				chip.Summary = truncate(s, 160)
			}
		}
	}
	chip.Detail = expandDetail(e, title, fromPhrase)
	chip.Why = whyFor(e)
	return chip, true
}

// whyFor renders the revelation line: why Ghost asked or acted. It uses only
// runtime-owned facts (capability, risk, status) and never model prose. It is
// deliberately quiet: a reason only appears when there IS a reason the owner
// would want (a consequential action, an approval, a denial). Routine reads
// and housekeeping get no line.
func whyFor(e *cevents.Event) string {
	risk, _ := e.Payload["risk"].(string)
	risk = strings.ToLower(strings.TrimSpace(risk))

	switch e.Type {
	case cevents.PermissionRequested:
		return "This changes something, so Ghost asked first."
	case cevents.PermissionApproved:
		if risk == "high_impact" {
			return "You approved this high-impact action; it may be hard to undo."
		}
		return "You said yes, so Ghost went ahead."
	case cevents.PermissionDenied:
		return "You declined this action, so Ghost did not do it."
	case cevents.PermissionExpired:
		return "The approval expired before it was used, so nothing ran."
	case cevents.CapabilityCompleted, cevents.ToolCompleted:
		// Only justify consequential work; reads need no explanation.
		switch risk {
		case "consequential":
			return "Done under your approval."
		case "high_impact":
			return "High-impact action, completed under your approval."
		}
		return ""
	case cevents.CapabilityFailed, cevents.ToolFailed:
		if risk == "consequential" || risk == "high_impact" {
			return "The action did not complete; nothing was changed."
		}
		return ""
	}
	return ""
}

// expandDetail builds layer-2 text: safe provenance, never internals.
// It deliberately carries no outcome word — the chip already has a state —
// and no raw capability id, because "Exec.shell · success" is plumbing the
// owner never asked to read.
//
// titleSaysCapability reports that the owner-language table already named the
// domain. When it did, repeating the registry title would say the same thing
// twice — "Read a file · Read files", "You approved: Shell command · Shell
// command" — so layer 2 stays quiet.
func expandDetail(e *cevents.Event, title string, titleSaysCapability bool) string {
	parts := []string{}
	if prov, _ := e.Payload["provider"].(string); prov != "" {
		parts = append(parts, utils.Prettify(prov))
	}
	// Sources are provenance, not a limitation: they belong on the activity
	// entry so the owner can see where something came from, without the reply
	// having to explain how it was retrieved.
	if srcs := payloadStrings(e.Payload["sources"]); len(srcs) > 0 {
		parts = append(parts, "sources: "+strings.Join(srcs, ", "))
	}
	// Only the registry's own owner-facing title may name the capability,
	// and only when the title did not already; an identifier we cannot
	// translate simply stays off the row.
	if !titleSaysCapability {
		if cap := capabilityIDOf(e); cap != "" {
			if spec, ok := capability.Get(cap); ok && spec.Title != "" &&
				!strings.Contains(strings.ToLower(title), strings.ToLower(spec.Title)) {
				parts = append(parts, spec.Title)
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

// payloadStrings reads a bounded string list out of an event payload, which
// may have been through JSON ([]interface{}) or not ([]string).
func payloadStrings(v interface{}) []string {
	switch tv := v.(type) {
	case []string:
		return tv
	case []interface{}:
		out := make([]string, 0, len(tv))
		for _, item := range tv {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, strings.TrimSpace(s))
			}
		}
		return out
	}
	return nil
}

func humanizeIntegration(name string) string {
	switch strings.ToLower(name) {
	case "calendar":
		return "Calendar"
	case "flight":
		return "Flight tracking"
	case "telegram":
		return "Telegram"
	default:
		if name == "" {
			return "Integration"
		}
		return strings.ToUpper(name[:1]) + name[1:]
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

// Diagnostics renders the layer-3 technical projection for explicit
// diagnostics views. Safe fields only; secrets were redacted at publish.
func Diagnostics(e *cevents.Event) map[string]interface{} {
	out := map[string]interface{}{
		"event_id": e.ID, "type": string(e.Type),
		"request_id": e.RequestID, "timestamp": e.Timestamp.Format(time.RFC3339),
	}
	if v, ok := e.Payload["provider"]; ok {
		out["provider"] = v
	}
	if v, ok := e.Payload["duration_ms"]; ok {
		out["duration_ms"] = v
	}
	if v, ok := e.Payload["attempt"]; ok {
		out["attempt"] = v
	}
	if e.Status != "" {
		out["status"] = e.Status
	}
	return out
}

// looksTechnical reports whether text reads like machine output (JSON, a
// stack, a query, a path, an error code) rather than a sentence for the owner.
func looksTechnical(s string) bool {
	if strings.ContainsAny(s, "{}[]<>|\\") || strings.Contains(s, "://") || strings.Contains(s, "::") {
		return true
	}
	l := strings.ToLower(s)
	for _, m := range []string{"select ", " from ", "exit status", "panic", "goroutine", "traceback",
		"nil pointer", "errno", "json:", "unmarshal", "sqlstate", "stderr", "/var/", "/usr/", "/home/", "0x"} {
		if strings.Contains(l, m) {
			return true
		}
	}
	// snake_case or dotted identifiers (exec.shell, web_search) are plumbing.
	for _, w := range strings.Fields(s) {
		if strings.Contains(w, "_") && len(w) > 3 {
			return true
		}
	}
	return false
}

// humanizeFailure turns a raw tool error into one plain sentence. The raw
// text is kept in Chip.Diagnostic; this is only what the owner reads first.
func humanizeFailure(raw string) string {
	l := strings.ToLower(raw)
	has := func(subs ...string) bool {
		for _, x := range subs {
			if strings.Contains(l, x) {
				return true
			}
		}
		return false
	}
	switch {
	// A source declining a date it does not cover is an answer, not a fault:
	// "Something went wrong" sent the owner to Details for "covers 3 to 17
	// October only".
	case has("forecast") && has("out of reach", "out of range", "covers", "only goes"):
		return "The forecast doesn't reach that date yet."
	case has("429", "rate limit", "too many requests", "quota"):
		return "The service is busy right now. Ghost can try again in a moment."
	case has("401", "403", "unauthorized", "forbidden", "api key", "invalid key", "authentication"):
		return "The service refused access. Its key may be missing or expired."
	case has("permission denied", "not permitted", "not allowed"):
		return "Ghost wasn't allowed to do that."
	case has("timeout", "timed out", "deadline exceeded"):
		return "It took too long to answer."
	case has("connection refused", "no such host", "dial tcp", "network", "unreachable", "eof", "connection reset"):
		return "Ghost couldn't reach the service."
	case has("404", "not found", "no such file", "does not exist"):
		return "Ghost couldn't find what it was looking for."
	case has("required", "missing", "invalid", "must be", "cannot be empty", "unknown field"):
		return "Ghost didn't give the tool everything it needed."
	}
	return "Something went wrong. Open Details for the technical reason."
}
