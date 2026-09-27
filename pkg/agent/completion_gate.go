package agent

// Evidence-gated completion: a runtime safety net, not the grader.
//
// The authoritative success/evidence invariant is graded by pkg/golden's
// semantic claim extractor (the NoFalseSuccess gate). That evaluator cannot
// be imported here — golden imports agent — so the runtime carries a
// deliberately narrow check for the one shape that must never reach the
// owner unchallenged:
//
//	the turn performed a consequential action that FAILED (or was denied),
//	nothing consequential succeeded, and the reply still reports completion.
//
// In that case the runtime appends its own authoritative correction and
// records a verification.failed event. This is not a general claim
// classifier: it only fires when the runtime holds positive evidence of
// non-completion, so it can never rewrite a reply about verified work. The
// full linguistic grader stays the authority for grading; this is the live
// floor beneath it.

import (
	"regexp"
	"strings"

	"github.com/ianclemence/ghost/pkg/capability"
	"github.com/ianclemence/ghost/pkg/cevents"
)

// completionClaimRE matches prose that asserts Ghost completed an action:
// first-person perfect/simple past over a consequential verb, and task
// completion frames. Prospective/conditional phrasing ("I will send it",
// "I'll schedule it") deliberately never matches.
var completionClaimRE = regexp.MustCompile(`(?i)(` +
	`\b(i|we)\s*(?:'ve|have|already)\s+(sent|scheduled|created|deleted|removed|uploaded|submitted|published|saved|posted|booked|ordered|paid|updated|enabled|disabled|cancelled|canceled|set up)\b` +
	`|\b(i|we)\s+(sent|scheduled|created|deleted|removed|uploaded|submitted|published|saved|posted|booked|ordered|paid|updated|enabled|disabled|cancelled|canceled|set up)\b` +
	`|\b(it'?s|it is|that'?s|all|everything)\s+(done|finished|complete|completed)\b` +
	`|\btask\s+(complete|completed|done)\b` +
	`|\bconsider it done\b` +
	`)`)

// bareCompletionRE matches a reply that OPENS with a standalone completion
// frame ("Done.", "All done — …", "It's finished."). Kept separate because
// the word "done" anywhere would otherwise match praise ("well done") and
// other people's work.
var bareCompletionRE = regexp.MustCompile(`(?i)^\s*(all\s+|it'?s\s+|that'?s\s+|everything'?s\s+)?(done|finished|complete|completed)\b`)

// completionSignal reports whether reply asserts a completed action.
func completionSignal(reply string) bool {
	if strings.TrimSpace(reply) == "" {
		return false
	}
	return completionClaimRE.MatchString(reply) || bareCompletionRE.MatchString(reply)
}

// applyCompletionGate returns the reply, corrected when it claims completion
// after a failed/denied consequential action with no consequential success,
// plus a non-empty reason when it corrected. A turn where nothing
// consequential was attempted, or where something consequential succeeded,
// is never touched.
func applyCompletionGate(reply string, hadConsequentialFailure, hadConsequentialSuccess bool) (string, string) {
	if hadConsequentialSuccess || !hadConsequentialFailure {
		return reply, ""
	}
	if !completionSignal(reply) {
		return reply, ""
	}
	correction := "Runtime check: that action did not complete — the runtime recorded a failure, so treat it as not done."
	return strings.TrimRight(reply, "\n") + "\n\n" + correction,
		"completion claimed after a failed or denied consequential action"
}

// toolIsConsequential reports whether a tool resolves to a capability whose
// outcome requires runtime evidence. A turn that ran one must not stream
// completion language without the evidence gate.
func toolIsConsequential(tool string) bool {
	spec, ok := capability.ForTool(tool)
	return ok && spec.RequiresEvidence()
}

// turnConsequentialOutcomes scans this turn's canonical events for
// consequential (evidence-requiring) tool outcomes. succeeded is true only
// for a successful completed call; failed is true for a failed call or a
// permission denial.
func (al *AgentLoop) turnConsequentialOutcomes(requestID string) (succeeded, failed bool) {
	if al == nil || al.governance == nil || al.governance.Events == nil || requestID == "" {
		return false, false
	}
	for _, e := range al.governance.Events.ByRequest(requestID) {
		if e == nil || e.Payload == nil {
			continue
		}
		capID, _ := e.Payload["capability"].(string)
		if capID == "" {
			continue
		}
		spec, ok := capability.Get(capID)
		if !ok || !spec.RequiresEvidence() {
			continue
		}
		switch e.Type {
		case cevents.ToolCompleted:
			if e.Status == "success" {
				succeeded = true
			}
		case cevents.ToolFailed, cevents.PermissionDenied:
			failed = true
		}
	}
	return succeeded, failed
}
