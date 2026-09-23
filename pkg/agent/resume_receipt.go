package agent

import (
	"strings"

	"github.com/ianclemence/ghost/pkg/tools"
)

// resumeReceiptText returns the user-facing text for a resumed execution:
// the tool's own words, or an evidence-grounded fallback — never silence.
// A tool that succeeds with no text must still leave a truthful receipt:
// the governed execution just recorded is the evidence. Persisting silence
// shows as a blank reply and teaches the owner Ghost did nothing.
func resumeReceiptText(toolResult *tools.ToolResult) string {
	text := strings.TrimSpace(toolResult.ForLLM)
	if toolResult.IsError {
		if text == "" {
			return "That didn't work."
		}
		return "That didn't work: " + toolResult.ForLLM
	}
	if text == "" {
		return "Done."
	}
	return toolResult.ForLLM
}
