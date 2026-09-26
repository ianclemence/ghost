package agent

import (
	"strings"
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

// A receipt that reaches the owner verbatim must stay readable: pasting a
// payload at the owner as Ghost's words is the defect this bound exists
// to stop, so even the no-model fallback can never dump a page at them.
func TestResumeReceiptBoundsPayload(t *testing.T) {
	big := strings.Repeat(`{"@context":"https://schema.org"}`, 500)
	got := resumeReceiptText(&tools.ToolResult{ForLLM: big})
	if n := len([]rune(got)); n > receiptMaxChars+80 {
		t.Fatalf("receipt must stay bounded, got %d runes", n)
	}
	if !strings.Contains(got, "more characters") {
		t.Fatalf("a clipped receipt must say what was left out, got %q", got)
	}
	if n := len([]rune(resumeReceiptText(&tools.ToolResult{ForLLM: big, IsError: true}))); n > receiptMaxChars+80 {
		t.Fatalf("error receipt must stay bounded, got %d runes", n)
	}
}

// The continuation hands the payload to the model as evidence: it has to
// arrive in full and be framed as something to answer, never as something
// to repeat back.
func TestResumeContinuationFramesPayload(t *testing.T) {
	prompt := resumeContinuationPrompt("exec approved run finished", "  raw bytes  ")
	if !strings.Contains(prompt, "raw bytes") {
		t.Fatalf("payload must reach the model, got %q", prompt)
	}
	if !strings.Contains(prompt, "own words") {
		t.Fatalf("payload must be framed as evidence to answer, got %q", prompt)
	}
	if empty := resumeContinuationPrompt("exec", "   "); !strings.Contains(empty, "(no output)") {
		t.Fatalf("an empty run must still say so, got %q", empty)
	}
	if got := resumeContinuationLabel("exec"); got != "exec approved run finished" {
		t.Fatalf("label must name the run, got %q", got)
	}
}
