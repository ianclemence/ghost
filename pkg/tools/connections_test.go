package tools

import (
	"context"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/credentials"
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

// A sealed website login is visible to the model as a host, never as a
// secret — so "is my X login set up?" is answered by checking, not by
// doubting the owner or by claiming the vault unlocks nothing.
func TestConnectionsSeesSealedWebLogins(t *testing.T) {
	t.Setenv("GHOST_CONFIG_DIR", t.TempDir())
	if err := credentials.SaveWebLogin(credentials.WebLogin{
		URL: "https://x.com/login", Username: "icmosha", Password: "hunter2",
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	tool := NewConnectionsTool()

	list := tool.Execute(context.Background(), map[string]interface{}{"action": "list"})
	if list == nil || list.IsError {
		t.Fatalf("list should succeed: %+v", list)
	}
	if !strings.Contains(list.ForLLM, "x.com — login saved") {
		t.Fatalf("list must show the sealed host: %s", list.ForLLM)
	}
	for _, secret := range []string{"hunter2", "icmosha"} {
		if strings.Contains(list.ForLLM, secret) {
			t.Fatalf("list must never carry secret material %q", secret)
		}
	}

	st := tool.Execute(context.Background(), map[string]interface{}{"action": "status", "app": "x.com"})
	if st == nil || st.IsError {
		t.Fatalf("status of a sealed host should succeed: %+v", st)
	}
	if !strings.Contains(st.ForLLM, "sealed in the vault") {
		t.Fatalf("status must confirm the sealed login: %s", st.ForLLM)
	}
	for _, secret := range []string{"hunter2", "icmosha"} {
		if strings.Contains(st.ForLLM, secret) {
			t.Fatalf("status must never carry secret material %q", secret)
		}
	}
}
