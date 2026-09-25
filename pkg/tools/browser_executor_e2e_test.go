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
		// data: URLs are refused by the navigation policy (SSRF posture);
		// a public http(s) page is the only honest e2e target.
		url = "https://example.com"
	}
	nav := NewBrowserTool("", "navigate")
	res := nav.executeBare(context.Background(), map[string]interface{}{"url": url})
	if res.IsError {
		t.Fatalf("navigate failed: %s", res.ForLLM)
	}
	// navigate merges identity + accessibility tree in one result.
	if !strings.Contains(res.ForLLM, "http") {
		t.Fatalf("navigate did not return page identity: %.200s", res.ForLLM)
	}
	snap := NewBrowserTool("", "snapshot")
	s := snap.executeBare(context.Background(), map[string]interface{}{})
	if s.IsError {
		t.Fatalf("snapshot failed: %s", s.ForLLM)
	}
	if !strings.Contains(s.ForLLM, "snapshot") && !strings.Contains(s.ForLLM, "ref=e") {
		t.Fatalf("snapshot missing accessibility tree: %.200s", s.ForLLM)
	}
	// Screens: Ghost's real capture path must produce a valid PNG from the
	// live page, using the same executable steering as navigation.
	shot, ok := captureBrowserShotWith(context.Background(), "e2e", defaultBrowserShotDir(), runBrowserScreenshot)
	if !ok {
		t.Fatalf("screenshot capture failed for %s", url)
	}
	if fi, err := os.Stat(shot); err != nil || fi.Size() <= 0 {
		t.Fatalf("screenshot missing or empty: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(shot) })
}
