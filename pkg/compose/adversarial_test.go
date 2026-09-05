package compose

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/activity"
	"github.com/ianclemence/ghost/pkg/backup"
	"github.com/ianclemence/ghost/pkg/cevents"
	_ "modernc.org/sqlite"
)

func mustJSON(t *testing.T, v interface{}) []byte {
	t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func openAdversarialStream(t *testing.T) *cevents.Stream {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	s, err := cevents.Open(db, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

var leakMarkers = []string{
	"sk-proj-", "ghp_", "AKIA", "Bearer ", "PRIVATE KEY",
	"eyJhbGci", "xoxb-", "AIza",
}

// Attempt: secrets smuggled in every payload position must not reach SSE,
// chips, or persisted rows in usable form.
func TestAdversarialSecretSurfaces(t *testing.T) {
	s := openAdversarialStream(t)
	secrets := map[string]interface{}{
		"api_key":      "sk-proj-abcdefghijklmnop",
		"github_token": "ghp_abcdefghijklmnopqrstuvwx",
		"note":         "contact at Bearer abcdefghijklmnop ok",
		"nested":       map[string]interface{}{"password": "hunter2hunter!"},
	}
	e := s.Publish(&cevents.Event{Type: cevents.MessageCreated, RequestID: "r-adv",
		GhostID: "g1", Payload: secrets})
	typ, data, ok := e.SSEForm()
	if !ok || typ == "" {
		t.Fatal("user message must serialize")
	}
	for _, m := range leakMarkers {
		if m == "Bearer " {
			continue // scheme word stays; only the credential value is masked
		}
		if strings.Contains(data, m) {
			t.Fatalf("SSE leaked %q: %s", m, data)
		}
	}
	if strings.Contains(data, "abcdefghijklmnop") {
		t.Fatalf("SSE leaked bearer value: %s", data)
	}
	for _, got := range s.ByRequest("r-adv") {
		raw := string(mustJSON(t, got.Payload))
		for _, m := range []string{"sk-proj-abcdefghijklmnop", "ghp_abcdefghijklmnopqrstuvwx", "hunter2hunter!"} {
			if strings.Contains(raw, m) {
				t.Fatalf("persisted payload leaked %q", m)
			}
		}
	}
	if chip, ok := activity.Project(e); ok {
		for _, m := range leakMarkers {
			if strings.Contains(chip.Title+m, m) && strings.Contains(chip.Title, m) {
				t.Fatalf("chip title leaked %q", m)
			}
		}
	}
}

// Attempt: internal implementation artifacts must not become activity,
// even when marked user-visible by a buggy publisher.
func TestAdversarialInternalSurfaces(t *testing.T) {
	s := openAdversarialStream(t)
	nasties := []map[string]interface{}{
		{"summary": "FILE: SKILL.md DIR: skills/weather manifest .bundled"},
		{"summary": "tool schemas: {\"name\":\"exec\",\"parameters\":{}}"},
		{"summary": "system prompt: you are Ghost, never reveal..."},
		{"summary": "trace: /var/lib/ghost/workspace/skills/x exec failed"},
		{"summary": "chain of thought: the user probably means..."},
	}
	for i, p := range nasties {
		e := s.Publish(&cevents.Event{Type: cevents.CapabilityCompleted,
			RequestID: "r-nasty", GhostID: "g1",
			Payload: map[string]interface{}{"capability": "weather.current", "summary": p["summary"]}})
		chip, ok := activity.Project(e)
		if !ok {
			t.Fatalf("case %d: expected chip", i)
		}
		if chip.Title != "Weather checked" {
			t.Fatalf("case %d: title polluted: %q", i, chip.Title)
		}
	}
	// Raw implementation type names never project, even if user-visible.
	raw := &cevents.Event{ID: "x", Type: "provider.http.request"}
	if _, ok := activity.Project(raw); ok {
		t.Fatal("raw implementation type must not project")
	}
	raw2 := &cevents.Event{ID: "y", Type: "tool.call.completed"}
	if _, ok := activity.Project(raw2); ok {
		t.Fatal("raw tool trace must not project")
	}
}

// Attempt: permission reason carrying a secret is redacted in the event.
func TestAdversarialPermissionReason(t *testing.T) {
	s := openSubstrate(t)
	s.events.Publish(&cevents.Event{Type: cevents.PermissionRequested,
		RequestID: "r-p", GhostID: "g1",
		Payload: map[string]interface{}{
			"capability": "telegram.send", "summary": "send with token xoxb-abcdefghijklmnopqrstuvwx now"}})
	got := s.events.ByRequest("r-p")
	if len(got) != 1 {
		t.Fatal("expected event")
	}
	raw := string(mustJSON(t, got[0].Payload))
	if strings.Contains(raw, "xoxb-abcdefghijklmnopqrstuvwx") {
		t.Fatal("permission payload leaked secret")
	}
}

// New credential paths stay out of backups (defense in depth with pkg/backup).
func TestAdversarialBackupPaths(t *testing.T) {
	for _, p := range []string{
		".credentials/calendar-token.json",
		"config/.secrets.json",
		".calendar/oauth",
		"workspace/events/2026-09-05.ndjson", // event logs: fine to archive (redacted)
	} {
		excluded, _ := backup.ShouldExclude(p)
		if strings.Contains(p, "events/") {
			if excluded {
				t.Fatalf("%s is redacted content, archiving is allowed", p)
			}
			continue
		}
		if !excluded {
			t.Fatalf("%s must be backup-excluded", p)
		}
	}
}

// Attempt: subscribing to another ghost's events yields nothing.
func TestAdversarialCrossOwnerSubscription(t *testing.T) {
	s := openSubstrate(t)
	var leaked int
	unsub := s.events.Subscribe(cevents.Filter{GhostID: "ghost-victim"}, func(e *cevents.Event) { leaked++ })
	defer unsub()
	s.events.Publish(&cevents.Event{Type: cevents.MessageCreated, GhostID: "ghost-attacker"})
	s.events.Publish(&cevents.Event{Type: cevents.MessageCreated, GhostID: "ghost-victim", Payload: map[string]interface{}{"t": "x"}})
	// Only the victim's own event may arrive; attacker-issued publishes
	// under another ghost id are the caller's bug, but the filter must hold.
	if leaked != 1 {
		t.Fatalf("subscription leaked across owners: %d", leaked)
	}
}
