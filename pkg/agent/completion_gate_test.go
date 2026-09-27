package agent

import (
	"strings"
	"testing"
)

// The completion gate fires only on the unambiguous false-completion shape:
// a failed/denied consequential action, no consequential success, and a
// success claim in the reply.
func TestCompletionGateRejectsUnbackedSuccess(t *testing.T) {
	reply := "Done — I've sent the email to Sarah."
	got, reason := applyCompletionGate(reply, true, false)
	if reason == "" {
		t.Fatal("a success claim after a failed consequential action must be corrected")
	}
	if !strings.Contains(got, "did not complete") {
		t.Fatalf("correction must state non-completion, got %q", got)
	}
	if !strings.Contains(got, reply) {
		t.Fatalf("correction must preserve the original reply, got %q", got)
	}
}

// Verified work is never touched: a consequential success makes the claim
// backed, so the reply passes through unchanged.
func TestCompletionGateLeavesVerifiedSuccessAlone(t *testing.T) {
	reply := "Done — I've sent the email."
	if got, reason := applyCompletionGate(reply, false, true); reason != "" || got != reply {
		t.Fatalf("verified success must pass through, got %q reason %q", got, reason)
	}
	// A retry that failed then succeeded still counts as success.
	if got, reason := applyCompletionGate(reply, true, true); reason != "" || got != reply {
		t.Fatalf("success after failure must pass through, got %q reason %q", got, reason)
	}
}

// A turn with no consequential action is never rewritten, even if the reply
// uses completion language about something else.
func TestCompletionGateIgnoresNonActionTurns(t *testing.T) {
	for _, reply := range []string{
		"The document is done.",
		"I've finished reading the thread.",
		"All done with the summary.",
	} {
		if got, reason := applyCompletionGate(reply, false, false); reason != "" || got != reply {
			t.Fatalf("non-action turn must not be rewritten: %q", got)
		}
	}
}

// A failure with no completion language is left alone: the model already
// reported the truth.
func TestCompletionGateIgnoresHonestFailureReport(t *testing.T) {
	reply := "I couldn't send it — the mail service rejected the message. Nothing was sent."
	if got, reason := applyCompletionGate(reply, true, false); reason != "" || got != reply {
		t.Fatalf("honest failure report must not be rewritten: %q", got)
	}
}

func TestCompletionSignal(t *testing.T) {
	claims := []string{
		"Done.",
		"It's done.",
		"All done.",
		"Task complete.",
		"Consider it done.",
		"I've sent the email.",
		"I have scheduled the reminder.",
		"I uploaded the file.",
		"We created the event.",
	}
	for _, s := range claims {
		if !completionSignal(s) {
			t.Errorf("completionSignal(%q) = false, want true", s)
		}
	}
	nonClaims := []string{
		"",
		"I'll send it shortly.",
		"I will schedule that for tomorrow.",
		"Let me know when you're done.",
		"I could send it if you want.",
		"The report reads well.",
	}
	for _, s := range nonClaims {
		if completionSignal(s) {
			t.Errorf("completionSignal(%q) = true, want false", s)
		}
	}
}

// A gated hold must never stream: once a turn has run a consequential action,
// completion language is withheld until the evidence gate has run.
func TestGatedHoldWithholdsCompletionFromStream(t *testing.T) {
	var emitted []string
	h := newNarrationHold(func(s string) { emitted = append(emitted, s) })
	h.gate = true
	h.feed("Done — I've sent the email to Sarah and everything is confirmed.")
	h.flush()
	if len(emitted) != 0 {
		t.Fatalf("gated hold streamed %d chunks, want 0: %v", len(emitted), emitted)
	}

	// Control: an ungated hold still flushes the answer.
	emitted = nil
	u := newNarrationHold(func(s string) { emitted = append(emitted, s) })
	u.feed("Here is your answer.")
	u.flush()
	if len(emitted) == 0 {
		t.Fatal("ungated hold must still stream the answer")
	}
}

// A capability phrased as a completed confirmation, with no execution, is
// rewritten into a future frame; denials and earned confirmations are not.
func TestRepairUnearnedAttestation(t *testing.T) {
	got, reason := repairUnearnedAttestation(
		"I can confirm the message was sent and report the channel's delivery status.", false)
	if reason == "" {
		t.Fatal("uneared attestation must be repaired")
	}
	if strings.Contains(strings.ToLower(got), "i can confirm") {
		t.Fatalf("present-tense confirmation survived: %q", got)
	}
	if !strings.Contains(got, "be able to confirm whether the message was sent") {
		t.Fatalf("repair must become a future frame, got %q", got)
	}

	// An executed action keeps its confirmation.
	earned := "I can confirm the message was sent."
	if got, reason := repairUnearnedAttestation(earned, true); reason != "" || got != earned {
		t.Fatalf("earned confirmation must pass through, got %q reason %q", got, reason)
	}

	// Denials never match.
	for _, s := range []string{
		"I can't confirm the message was sent.",
		"I cannot guarantee it went out.",
		"Here is your answer.",
	} {
		if got, reason := repairUnearnedAttestation(s, false); reason != "" || got != s {
			t.Fatalf("must not rewrite %q, got %q reason %q", s, got, reason)
		}
	}
}
