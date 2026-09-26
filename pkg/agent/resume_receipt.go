package agent

import (
	"fmt"
	"strings"

	"github.com/ianclemence/ghost/pkg/tools"
)

// receiptMaxChars bounds a receipt that reaches the owner verbatim. The
// model normally writes the reply; this text only fires when no model
// turn is available, and a payload pasted at the owner as Ghost's words
// is the exact failure this bound exists to stop.
const receiptMaxChars = 600

// resumeContinuationLabel names an approval continuation's turn. It feeds
// the turn's effort, affect and journal bookkeeping, so it must be a short
// honest phrase — never the payload.
func resumeContinuationLabel(tool string) string {
	if tool == "" {
		return "approved run finished"
	}
	return tool + " approved run finished"
}

// resumeContinuationPrompt frames a resumed execution's raw output as the
// evidence for the one model turn that owes the owner an answer. The
// payload travels as the current message and nowhere else: never
// persisted as speech, never streamed as Ghost's reply — the owner reads
// the model's words, not the tool's bytes.
func resumeContinuationPrompt(label, output string) string {
	body := strings.TrimSpace(output)
	if body == "" {
		body = "(no output)"
	}
	return label + "\n\n" +
		"You asked the owner to approve this run and promised the answer once it " +
		"ran. It ran. Read the output below and give that answer in your own " +
		"words — a raw payload is not a reply.\n\n" +
		"--- approved run output ---\n" + body + "\n--- end output ---"
}

// canContinueResume reports whether this loop can run the model turn that
// answers a resumed execution. A loop built without a provider or session
// store has no conversation to continue: there the bounded receipt is the
// reply.
func (al *AgentLoop) canContinueResume() bool {
	return al.provider != nil && al.sessions != nil && al.contextBuilder != nil
}

// resumeReceiptText returns the user-facing text for a resumed execution
// when the model could not write the reply: the tool's own words, or an
// evidence-grounded fallback — never silence, and never an unbounded
// payload pasted at the owner as something Ghost said.
func resumeReceiptText(toolResult *tools.ToolResult) string {
	text := strings.TrimSpace(toolResult.ForLLM)
	if toolResult.IsError {
		if text == "" {
			return "That didn't work."
		}
		return clipReceipt("That didn't work: " + text)
	}
	if text == "" {
		return "Done."
	}
	return clipReceipt(text)
}

// clipReceipt bounds a verbatim receipt on a rune boundary, so a
// multi-byte character is never split in half.
func clipReceipt(s string) string {
	runes := []rune(s)
	if len(runes) <= receiptMaxChars {
		return s
	}
	kept := strings.TrimSpace(string(runes[:receiptMaxChars]))
	return kept + fmt.Sprintf("\n… (%d more characters)", len(runes)-receiptMaxChars)
}
