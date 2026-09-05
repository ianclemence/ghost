package permissions

// Natural-language standing permissions: "Always let Ghost add calendar
// events." The LLM may PROPOSE a scope, but the runtime DECIDES: every
// grant is validated against declared capabilities, reduced or rejected
// when overbroad, and confirmed before persistence. The model is never
// the authorization authority.

import (
	"regexp"
	"strings"

	"github.com/ianclemence/ghost/pkg/skills"
)

// StandingGrant is one validated grant line.
type StandingGrant struct {
	Capability string `json:"capability"`
	Action     string `json:"action"`
	Scope      string `json:"scope"`
}

// StandingProposal is a runtime-validated proposal awaiting confirmation.
type StandingProposal struct {
	Grants  []StandingGrant `json:"grants"`
	Summary string          `json:"summary"`
	Deny    bool            `json:"deny,omitempty"`
}

// StandingRejection explains why no grant was proposed.
type StandingRejection struct {
	Reason  string   `json:"reason"`
	Options []string `json:"options,omitempty"`
}

var standingAllowRE = regexp.MustCompile(`(?i)(^\s*(always\s+(let|allow)\s+ghost|let\s+ghost\s+always|allow\s+ghost\s+to\s+always|you can always|i can always|you (can|may) (let|allow) (ghost|me))\b)|(\b(your|always)\s*:\s*(let|allow))`)
var standingDenyRE = regexp.MustCompile(`(?i)^\s*(never\s+(let|allow)\s+ghost|don't\s+(ever\s+)?let\s+ghost|stop\s+asking\s+me\s+about)\b`)

// scopePhrase maps user language to narrow capability grants. Deliberately
// small: unknown phrasing rejects with guidance instead of guessing.
var scopePhraseTable = []struct {
	match []string
	gives []StandingGrant
}{
	{[]string{"add", "calendar", "event"}, []StandingGrant{{"calendar.create", "create", "owner"}}},
	{[]string{"calendar", "event"}, []StandingGrant{{"calendar.create", "create", "owner"}}},
	{[]string{"read", "calendar"}, []StandingGrant{{"calendar.read", "read", "owner"}}},
	{[]string{"calendar"}, []StandingGrant{{"calendar.read", "read", "owner"}}},
	{[]string{"reminder"}, []StandingGrant{{"reminder.create", "create", "owner"}}},
	{[]string{"weather"}, []StandingGrant{{"weather.current", "read", "owner"}}},
	{[]string{"remember", "things"}, []StandingGrant{{"memory.write", "write", "owner"}}},
	{[]string{"send", "message"}, []StandingGrant{{"telegram.send", "send", "owner"}}},
	{[]string{"telegram"}, []StandingGrant{{"telegram.send", "send", "owner"}}},
	{[]string{"control", "lights"}, []StandingGrant{{"hass.control", "toggle", "home"}}},
	{[]string{"lights"}, []StandingGrant{{"hass.control", "toggle", "home"}}},
	{[]string{"home", "assistant"}, []StandingGrant{{"hass.control", "toggle", "home"}}},
}

// ProposeStanding parses a standing-permission request deterministically.
// Returns proposal or rejection — never a grant. Grants persist only
// after explicit confirmation through ConfirmStanding.
// ProposeStanding parses a standing-permission request deterministically.
// Broad-account language is recognized and rejected FIRST (handled=true,
// no grant), so the model never gets a chance to "note" an all-powerful
// permission the runtime would not honor. Narrow proposals persist only
// after explicit confirmation.
func ProposeStanding(text string) (StandingProposal, StandingRejection, bool) {
	trimmed := strings.TrimSpace(text)
	lower := strings.ToLower(trimmed)
	// Broad/anything phrasing is a standing-permission intent that must be
	// rejected deterministically — never routed to the model as ordinary
	// chat, and never converted into an all-powerful grant.
	if isBroadScope(lower) || broadAccountRE.MatchString(trimmed) {
		return StandingProposal{}, StandingRejection{
			Reason: "I can't grant access to an entire account. Grant one capability at a time instead.",
			Options: []string{
				"Always let Ghost read calendar events",
				"Always let Ghost add calendar events",
			},
		}, true
	}
	deny := standingDenyRE.MatchString(trimmed)
	if !standingAllowRE.MatchString(trimmed) && !deny {
		return StandingProposal{}, StandingRejection{}, false
	}
	for _, row := range scopePhraseTable {
		if containsAll(lower, row.match) {
			grants := validatedGrants(row.gives)
			if len(grants) == 0 {
				continue
			}
			summary := summarizeGrants(grants, deny)
			return StandingProposal{Grants: grants, Summary: summary, Deny: deny}, StandingRejection{}, true
		}
	}
	return StandingProposal{}, StandingRejection{
		Reason: "I couldn't tell exactly what to allow. Name one thing, like calendar events or reminders.",
		Options: []string{
			"Always let Ghost add calendar events",
			"Always let Ghost create reminders",
		},
	}, true
}

func isBroadScope(lower string) bool {
	for _, phrase := range []string{
		"entire", "whole account", "everything", "all my data",
		"full access", "anything", "whatever it wants",
	} {
		if strings.Contains(lower, phrase) {
			return true
		}
	}
	return false
}

// broadAccountRE catches "do anything on my account"-style phrasing even
// when isBroadScope's substrings don't appear verbatim.
var broadAccountRE = regexp.MustCompile(`(?i)\b(do|access|control|manage|use|change|touch)\s+(anything|whatever|everything|all)\b.*\b(account|google|data|things)\b`)

func containsAll(lower string, words []string) bool {
	for _, w := range words {
		if !strings.Contains(lower, w) {
			return false
		}
	}
	return true
}

// validatedGrants keeps only grants whose capability the runtime
// declares. Unknown capabilities reject the whole proposal (fail closed
// rather than storing half a grant).
func validatedGrants(gives []StandingGrant) []StandingGrant {
	var out []StandingGrant
	for _, g := range gives {
		if !skills.HasCapability(g.Capability) {
			return nil
		}
		if strings.TrimSpace(g.Action) == "" || strings.TrimSpace(g.Scope) == "" {
			return nil
		}
		// Cross-context grants are not expressible here: standing scopes
		// are owner/session/contact only.
		if strings.Contains(g.Scope, "context:") {
			return nil
		}
		out = append(out, g)
	}
	return out
}

func summarizeGrants(grants []StandingGrant, deny bool) string {
	parts := make([]string, 0, len(grants))
	for _, g := range grants {
		parts = append(parts, describeGrant(g))
	}
	verb := "Ghost will be allowed to "
	if deny {
		verb = "Ghost will never be allowed to "
	}
	return verb + strings.Join(parts, " and ") + ". Nothing else changes."
}

func describeGrant(g StandingGrant) string {
	switch g.Capability {
	case "calendar.create":
		return "add calendar events for you"
	case "calendar.read":
		return "read your calendar"
	case "reminder.create":
		return "create reminders for you"
	case "weather.current":
		return "check the weather without asking"
	case "telegram.send":
		return "send messages for you"
	case "hass.control":
		return "control home devices"
	default:
		return "use " + g.Capability
	}
}
