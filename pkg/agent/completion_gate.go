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

// unearnedAttestationRE matches a capability phrased as a present
// confirmation of a completed act ("I can confirm the message was sent").
// With no consequential execution this turn, the runtime rewrites it into an
// honest future frame: the sentence reads as a success claim, and a model's
// wording must not outrun the runtime's evidence.
var unearnedAttestationRE = regexp.MustCompile(`(?i)\b(i|we)\s+can\s+(confirm|verify|guarantee|assure|certify)\s+(that\s+)?`)

// repairUnearnedAttestation rewrites present-tense confirmations of actions
// into future frames when nothing consequential succeeded this turn. It never
// touches a reply whose action was actually executed (hadSuccess), and it
// never matches a denial ("I can't confirm", "I cannot confirm").
func repairUnearnedAttestation(reply string, hadSuccess bool) (string, string) {
	if hadSuccess || reply == "" {
		return reply, ""
	}
	if !unearnedAttestationRE.MatchString(reply) {
		return reply, ""
	}
	repaired := unearnedAttestationRE.ReplaceAllString(reply, `${1}'ll be able to ${2} whether `)
	if repaired == reply {
		return reply, ""
	}
	return repaired, "present-tense confirmation of an action with no execution evidence"
}

// turnOutcome in outcome.go supersedes the earlier boolean scan.
