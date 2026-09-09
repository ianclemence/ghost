package tools

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Real agent-browser executor E2E. Opt-in via GHOST_E2E_BROWSER=1 and an
// installed `agent-browser` (CDP wrapper). Skipped otherwise — this is the
// actual external browser executor Ghost shells out to, never a fake.
func TestBrowserExecutorRealE2E(t *testing.T) {
	if os.Getenv("GHOST_E2E_BROWSER") == "" {
		t.Skip("set GHOST_E2E_BROWSER=1 with agent-browser installed to run the real executor E2E")
	}
	if _, err := exec.LookPath("agent-browser"); err != nil {
		t.Skip("agent-browser not installed")
	}
	url := os.Getenv("GHOST_E2E_URL")
	if url == "" {
		url = "data:text/html,<title>Ghost E2E</title><h1>hi</h1>"
	}
	nav := NewBrowserTool("", "navigate")
	res := nav.executeBare(context.Background(), map[string]interface{}{"url": url})
	if res.IsError {
		t.Fatalf("navigate failed: %s", res.ForLLM)
	}
	if !strings.Contains(res.ForLLM, "Ghost") {
		t.Fatalf("navigate did not load expected page: %.200s", res.ForLLM)
	}
	snap := NewBrowserTool("", "snapshot")
	s := snap.executeBare(context.Background(), map[string]interface{}{})
	if s.IsError {
		t.Fatalf("snapshot failed: %s", s.ForLLM)
	}
	if !strings.Contains(s.ForLLM, "Ghost") {
		t.Fatalf("snapshot missing page title: %.200s", s.ForLLM)
	}
}
