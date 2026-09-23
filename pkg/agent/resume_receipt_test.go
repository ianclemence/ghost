package agent

import (
	"testing"

	"github.com/ianclemence/ghost/pkg/tools"
)

func TestResumeReceiptText(t *testing.T) {
	if got := resumeReceiptText(&tools.ToolResult{ForLLM: "All set."}); got != "All set." {
		t.Fatalf("tool prose must win, got %q", got)
	}
	if got := resumeReceiptText(&tools.ToolResult{ForLLM: "boom", IsError: true}); got != "That didn't work: boom" {
		t.Fatalf("errors must prefix, got %q", got)
	}
	// The reported defect: a successful tool with no text persisted
	// silence, which renders as a blank reply.
	if got := resumeReceiptText(&tools.ToolResult{}); got != "Done." {
		t.Fatalf("empty success must receipt honestly, got %q", got)
	}
	if got := resumeReceiptText(&tools.ToolResult{ForLLM: "  ", IsError: true}); got != "That didn't work." {
		t.Fatalf("empty failure must not mislead, got %q", got)
	}
}
