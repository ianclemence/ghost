package tools

import (
	"regexp"
	"strings"
)

// IntentRoute is a deterministic, high-confidence routing preference. When the
// owner's words unambiguously name a purpose-built capability, the runtime
// offers that capability and suppresses generic overlap (shell/sandbox) for the
// turn. Ambiguous input returns no route, so the model keeps the safe generic
// fallback and no incorrect narrow route is forced.
//
// This is a preference layer, never an authority: capability checks,
// permissions, governance, evidence and verification are unchanged.
type IntentRoute struct {
	Name    string
	Tools   []string // purpose-built tools to offer
	Exclude []string // generic/overlapping tools to suppress for this turn
}

// intentRoutes is deliberately small and phrase-specific. A keyword that also
// appears in ordinary prose would force a narrow route on a message that
// needed the fallback, which is worse than no preference at all.
var intentRoutes = []struct {
	route    IntentRoute
	keywords []string
}{
	{
		route: IntentRoute{
			Name:    "schedule",
			Tools:   []string{"schedule", "tasks"},
			Exclude: []string{"exec", "sandbox"},
		},
		keywords: []string{
			"what's scheduled", "what is scheduled", "my schedule", "what's on my schedule",
			"upcoming reminders", "my reminders", "my routines", "scheduled items",
			"what routines", "my upcoming",
		},
	},
	{
		route: IntentRoute{
			Name:    "runtime_health",
			Tools:   []string{"system_status"},
			Exclude: []string{"exec", "sandbox"},
		},
		keywords: []string{
			"system health", "device health", "memory pressure", "disk space", "free space",
			"how's the system", "system status", "storage left", "how are you doing",
		},
	},
	{
		route: IntentRoute{
			Name:    "personal_memory",
			Tools:   []string{"memory_recall"},
			Exclude: []string{"web_search", "web_fetch", "exec"},
		},
		keywords: []string{
			"what do you remember", "do you remember", "my preferences", "what do i like",
			"what do you know about me", "what did i say",
		},
	},
	{
		route: IntentRoute{
			Name:    "recent_activity",
			Tools:   []string{"system_status"},
			Exclude: []string{"exec", "web_search"},
		},
		keywords: []string{
			"what did you just do", "recent activity", "what have you been doing",
		},
	},
}

// shellCommandRE recognises an explicit shell/destructive command. Such a
// message is never re-routed to a purpose-built capability: it belongs to the
// governed shell path, and re-routing it would hide the destructive intent from
// the approval gate. Kept to destructive/authority verbs so ordinary prose
// ("go ahead", "list") is unaffected.
var shellCommandRE = regexp.MustCompile(`(?i)(^|\s)(rm|rmdir|mv|cp|dd|truncate|shred|chmod|chown|sudo|systemctl|apt|dpkg|docker|kubectl|iptables)\b`)

// PreferredIntentRoute returns the high-confidence route for a message, or
// ok=false when the intent is not unambiguous.
func PreferredIntentRoute(userMsg string) (IntentRoute, bool) {
	lower := strings.ToLower(strings.TrimSpace(userMsg))
	if lower == "" {
		return IntentRoute{}, false
	}
	// An explicit shell command keeps the generic governed path; a purpose-built
	// route must never mask it.
	if shellCommandRE.MatchString(lower) {
		return IntentRoute{}, false
	}
	for _, r := range intentRoutes {
		for _, kw := range r.keywords {
			if strings.Contains(lower, kw) {
				return r.route, true
			}
		}
	}
	return IntentRoute{}, false
}
