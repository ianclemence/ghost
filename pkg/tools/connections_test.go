package tools

import (
	"context"
	"strings"
	"testing"
)

func TestConnectionEntriesCoverCatalog(t *testing.T) {
	entries := connectionEntries()
	if len(entries) < 8 {
		t.Fatalf("expected the first-party catalog, got %d entries", len(entries))
	}
	want := map[string]bool{
		"gmail": false, "google-calendar": false, "home-assistant": false,
		"github": false, "notion": false, "spotify": false, "outlook": false,
	}
	for _, e := range entries {
		if _, ok := want[e.id]; ok {
			want[e.id] = true
		}
	}
	for id, seen := range want {
		if !seen {
			t.Errorf("catalog entry missing from connections: %s", id)
		}
	}
}

func TestSetupStepsNeverCarrySecrets(t *testing.T) {
	for _, e := range connectionEntries() {
		steps := setupSteps(e)
		if steps == "" {
			t.Errorf("%s: empty setup steps", e.id)
		}
		for _, bad := range []string{"/var/", "/home/", "Bearer ", "API_KEY=", "sk-", ".env"} {
			if strings.Contains(steps, bad) {
				t.Errorf("%s: setup steps leak %q: %s", e.id, bad, steps)
			}
		}
		if !strings.Contains(steps, "Apps") {
			t.Errorf("%s: setup steps must point at the Apps screen: %s", e.id, steps)
		}
	}
}

func TestConnectionsExecute(t *testing.T) {
	tool := NewConnectionsTool()

	list := tool.Execute(context.Background(), map[string]interface{}{"action": "list"})
	if list == nil || list.IsError || !strings.Contains(list.ForLLM, "Connected apps") {
		t.Fatalf("list should return the app table: %+v", list)
	}

	// Unknown app: honest error that names the options.
	bad := tool.Execute(context.Background(), map[string]interface{}{"action": "status", "app": "myspace"})
	if bad == nil || !bad.IsError {
		t.Fatal("unknown app should be an honest error")
	}

	// Known app: connected or setup guidance, never a secret.
	one := tool.Execute(context.Background(), map[string]interface{}{"action": "begin", "app": "Notion"})
	if one == nil || one.IsError {
		t.Fatalf("notion begin should succeed: %+v", one)
	}
	if !strings.Contains(one.ForLLM, "Notion") {
		t.Fatalf("expected the app name in the answer: %s", one.ForLLM)
	}

	badAction := tool.Execute(context.Background(), map[string]interface{}{"action": "nope"})
	if badAction == nil || !badAction.IsError {
		t.Fatal("unsupported action should be refused")
	}
}
