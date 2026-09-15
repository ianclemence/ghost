package permissions

import "fmt"

// Denial vocabulary. Four verbs, exact meanings, used everywhere a
// refusal reaches a human or a model:
//
//	denied       policy said no (broker deny, explicit rule)
//	refused      tool-level precondition failed (bad merchant quote,
//	             unreadable page, missing binding) — fix the input
//	unavailable  infrastructure missing (no executor, ungoverned
//	             runtime, expired session) — fix the setup
//	unauthorized subagent or binding violation — rebind, never retry
//
// Every user-visible denial is a Denial: a stable machine code, one
// human sentence naming the rule or capability, and one next action.
// Chat renders reason + remedy; the API adds the code; logs keep all
// three plus the full context.
type DenialCode string

const (
	CodePolicyDenied         DenialCode = "policy.denied"
	CodeApprovalRequired     DenialCode = "approval.required"
	CodeScopeRoutine         DenialCode = "scope.routine"
	CodeScopeContext         DenialCode = "scope.context"
	CodeSessionExpired       DenialCode = "session.expired"
	CodeRefStale             DenialCode = "ref.stale"
	CodeEvidenceAbsent       DenialCode = "evidence.absent"
	CodeGrantRevoked         DenialCode = "grant.revoked"
	CodeSubagentUnauthorized DenialCode = "subagent.unauthorized"
	CodeBindingMismatch      DenialCode = "binding.mismatch"
	CodeUnavailable          DenialCode = "unavailable"
	CodePreconditionFailed   DenialCode = "precondition.failed"
)

// Denial is the user-visible refusal envelope.
type Denial struct {
	Code   DenialCode
	Reason string // one human sentence naming the rule/capability
	Remedy string // one next action
}

// Deny builds the envelope. Empty reason or remedy is a programmer
// error: callers must say what and what-next, never just "no".
func Deny(code DenialCode, reason, remedy string) Denial {
	if reason == "" {
		reason = "That action isn't allowed."
	}
	if remedy == "" {
		remedy = "Ask again with more detail about what you want."
	}
	return Denial{Code: code, Reason: reason, Remedy: remedy}
}

// Chat renders reason + remedy for conversational surfaces.
func (d Denial) Chat() string {
	return d.Reason + " " + d.Remedy
}

// String renders code + reason + remedy for logs and tool results.
func (d Denial) String() string {
	return fmt.Sprintf("[%s] %s %s", d.Code, d.Reason, d.Remedy)
}
