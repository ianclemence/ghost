package agent

import (
	"errors"
	"testing"

	"github.com/ianclemence/ghost/pkg/tools"
)

func TestBrowserStepSummary(t *testing.T) {
	ok := &tools.ToolResult{Evidence: map[string]interface{}{"domain": "www.google.com", "text": "- generic"}}
	if got := browserStepSummary("browser_navigate", ok); got != "Opened www.google.com" {
		t.Errorf("navigate = %q", got)
	}
	if got := browserStepSummary("browser_snapshot", ok); got != "" {
		t.Errorf("a snapshot is bookkeeping and has no row, got %q", got)
	}
	fail := &tools.ToolResult{IsError: true, Err: errors.New(`tool "browser_navigate" timed out after 1m30s`)}
	if got := browserStepSummary("browser_navigate", fail); got == "" {
		t.Error("a failure must carry its reason")
	}
}

func TestActivityWorthy(t *testing.T) {
	for _, c := range []string{"web.search", "web.fetch", "email.send"} {
		if !activityWorthy(c) {
			t.Errorf("%s should be shown in Activity", c)
		}
	}
	for _, c := range []string{"memory.recall", "file.read", "exec.shell", ""} {
		if activityWorthy(c) {
			t.Errorf("%s is bookkeeping and should stay out of Activity", c)
		}
	}
}
