package permissions

import (
	"time"
)

// Broker audit mode: record-without-enforce for safe policy development.
//
// In ModeAudit nothing is blocked: Evaluate allows everything while
// Audit/EvaluateAudit report the verdict the strict policy WOULD have
// returned. Operators develop standing grants against the audit stream
// instead of guessing, then switch to ask/auto/full with evidence.
// Audit mode is explicit and loud — it never applies silently.

// ModeAudit records without enforcing.
const ModeAudit Mode = "audit"

// evaluateWith runs the policy under an explicit mode so audit can ask
// "what would the strict posture do" independent of the live mode.
func (b *Broker) evaluateWith(mode Mode, capability, action, scope string, risk Risk) Verdict {
	return evaluatePolicy(b, mode, capability, action, scope, risk)
}

// AuditRecord is one would-be decision.
type AuditRecord struct {
	Capability string
	Action     string
	Scope      string
	Risk       Risk
	WouldBe    Verdict
	At         time.Time
}

// EvaluateAudit returns (enforced, wouldBe): enforced is always allow
// in audit semantics; wouldBe is the strict-policy verdict for logging.
func (b *Broker) EvaluateAudit(capability, action, scope string, risk Risk) (enforced, wouldBe Verdict) {
	return VerdictAllow, b.evaluateWith(ModeAsk, capability, action, scope, risk)
}

// Audit evaluates, emits a permission.audited event with the would-be
// verdict, and returns the enforced (allow) verdict. Unknown cost and
// malformed input are recorded with their would-be verdict, never
// dropped: an audit stream with holes teaches the wrong policy.
func (b *Broker) Audit(capability, action, scope string, risk Risk) Verdict {
	enforced, wouldBe := b.EvaluateAudit(capability, action, scope, risk)
	b.emitEvent("permission.audited", &Request{
		Capability: capability, Action: action,
		SessionKey: scope, Risk: risk, Status: RequestStatus("audited"),
		Reason: "would-be:" + string(wouldBe),
	})
	return enforced
}
