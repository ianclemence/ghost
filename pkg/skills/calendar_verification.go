package skills

import (
	"strings"
)

// Google verification readiness: machine-readable scope justification.
//
// Google's OAuth verification cannot be performed programmatically — a
// human submits the consent-screen review. What Ghost CAN do in code:
// (1) request the narrowest scopes for the capability actually used,
// (2) document why each scope exists, (3) expose a readiness checklist
// the deployer completes before requesting verification, and
// (4) guarantee at runtime that broader scopes are never requested
// silently (ScopesFor is the single choke point, covered by tests).

// ScopeInfo justifies one Google scope for verification review.
type ScopeInfo struct {
	Scope       string `json:"scope"`
	Purpose     string `json:"purpose"`
	WhyNarrow   string `json:"why_narrow"`
	Sensitivity string `json:"sensitivity"` // "non-sensitive" | "sensitive"
}

// CalendarScopeJustification returns the verification submission content:
// every scope Ghost may request, what it is for, and why nothing broader
// is used. Full https://www.googleapis.com/auth/calendar access is never
// requested — state that explicitly for the reviewer.
func CalendarScopeJustification() []ScopeInfo {
	return []ScopeInfo{
		{
			Scope:       ScopeCalendarReadonly,
			Purpose:     "Read the owner's upcoming events to answer 'what's on my calendar'.",
			WhyNarrow:   "Read-only: Ghost cannot create, modify, or delete anything with this scope.",
			Sensitivity: "non-sensitive",
		},
		{
			Scope:       ScopeCalendarEvents,
			Purpose:     "Create events the owner explicitly asks for ('add lunch with Maria at 1 PM tomorrow'). Requested only when the write capability is used.",
			WhyNarrow:   "Limited to events (not settings, sharing, or full calendar access). The default flow requests readonly only; events scope is an explicit escalation.",
			Sensitivity: "sensitive",
		},
	}
}

// VerificationItem is one deployer-side checklist step.
type VerificationItem struct {
	ID          string `json:"id"`
	Step        string `json:"step"`
	Detail      string `json:"detail"`
	Automatable bool   `json:"automatable"`
}

// CalendarVerificationChecklist is the deployment path to production
// verification. Non-automatable steps are human actions in the Google
// Cloud Console; automatable ones are enforced by this package's tests.
func CalendarVerificationChecklist() []VerificationItem {
	return []VerificationItem{
		{ID: "scopes", Step: "Request only the scopes below", Detail: "readonly by default; events scope only for the write capability.", Automatable: true},
		{ID: "consent-screen", Step: "Configure the OAuth consent screen", Detail: "App name, support email, and authorized domains matching the relay callback host.", Automatable: false},
		{ID: "redirects", Step: "Register exact redirect URIs", Detail: "Relay callback https://<relay>/oauth/calendar/callback and LAN callback; no wildcards.", Automatable: false},
		{ID: "test-users", Step: "Use test users until verified", Detail: "Unverified apps are limited to 100 test users; production rollout waits for approval.", Automatable: false},
		{ID: "sensitive-review", Step: "Submit the events scope for sensitive-scope review", Detail: "Attach the justification from CalendarScopeJustification; readonly-only deployments skip this.", Automatable: false},
		{ID: "no-full-scope", Step: "Never request full calendar scope", Detail: "Enforced by ScopesFor + TestScopesNarrowest.", Automatable: true},
	}
}

// RequestedScopesNeverBroad asserts the runtime invariant the checklist
// depends on: no code path may request anything beyond the two justified
// scopes.
func RequestedScopesNeverBroad(scopes []string) bool {
	allowed := map[string]bool{ScopeCalendarReadonly: true, ScopeCalendarEvents: true}
	for _, s := range scopes {
		if !allowed[strings.TrimSpace(s)] {
			return false
		}
	}
	return true
}
