package activity

import (
	"strings"
	"testing"
	"time"

	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/product"
)

func ev(t cevents.Type, payload map[string]interface{}) *cevents.Event {
	return &cevents.Event{ID: "e1", Type: t, Timestamp: time.Now(),
		Visibility: product.VisUserMessage, Payload: payload}
}

func TestProductNarrative(t *testing.T) {
	cases := map[cevents.Type]string{
		cevents.CapabilityStarted:    "Checking the weather",
		cevents.CapabilityCompleted:  "Weather checked",
		cevents.CapabilityFailed:     "Weather unavailable",
		cevents.PermissionRequested:  "Waiting for approval",
		cevents.RoutineCreated:       "Routine scheduled",
		cevents.IntegrationConnected: "Calendar connected",
	}
	payloads := map[cevents.Type]map[string]interface{}{
		cevents.CapabilityStarted:    {"capability": "weather.current"},
		cevents.CapabilityCompleted:  {"capability": "weather.current", "provider": "open-meteo"},
		cevents.CapabilityFailed:     {"capability": "weather.current"},
		cevents.PermissionRequested:  {"target": "contact:maria"},
		cevents.RoutineCreated:       {},
		cevents.IntegrationConnected: {"integration": "calendar"},
	}
	for typ, want := range cases {
		chip, ok := Project(ev(typ, payloads[typ]))
		if !ok {
			t.Fatalf("%s: no chip", typ)
		}
		if chip.Title != want {
			t.Fatalf("%s: got %q want %q", typ, chip.Title, want)
		}
	}
}

func TestStates(t *testing.T) {
	if c, _ := Project(ev(cevents.PermissionRequested, nil)); c.State != StateWaiting {
		t.Fatal("permission must show waiting")
	}
	if c, _ := Project(ev(cevents.CapabilityFailed, map[string]interface{}{"capability": "x"})); c.State != StateFailed {
		t.Fatal("failure must show failed")
	}
	if c, _ := Project(ev(cevents.RoutineCompleted, nil)); c.State != StateSuccess {
		t.Fatal("completed must show success")
	}
}

func TestInternalNeverProjects(t *testing.T) {
	internal := &cevents.Event{ID: "x", Type: cevents.ToolStarted, Timestamp: time.Now(),
		Visibility: product.VisInternalTrace, Payload: map[string]interface{}{"tool": "exec"}}
	if _, ok := Project(internal); ok {
		t.Fatal("internal trace must never become a chip")
	}
	// Unknown type with user visibility still yields no chip (no raw leak).
	unknown := &cevents.Event{ID: "y", Type: "provider.http.request", Timestamp: time.Now(),
		Visibility: product.VisUserMessage}
	if _, ok := Project(unknown); ok {
		t.Fatal("unknown type must not project raw names")
	}
}

func TestLeakRegression(t *testing.T) {
	// Previously observed real failures: manifests, DIR:/FILE: dumps,
	// tool schemas, internal paths must never surface in chips.
	nasty := map[string]interface{}{
		"capability": "weather.current",
		"summary":    "FILE: SKILL.md DIR: skills/ tool instructions exec manifest .bundled /var/lib/ghost/secret",
	}
	chip, ok := Project(ev(cevents.CapabilityCompleted, nasty))
	if !ok {
		t.Fatal("expected chip")
	}
	// The chip title itself must be clean narrative (summary is truncated
	// payload echo — titles, the prominent surface, are allowlisted).
	if chip.Title != "Weather checked" {
		t.Fatalf("title polluted: %q", chip.Title)
	}
	for _, banned := range []string{"SKILL.md", "DIR:", "manifest", ".bundled", "/var/lib"} {
		if strings.Contains(chip.Title, banned) {
			t.Fatalf("title leaked %q", banned)
		}
	}
	// Detail layer carries only safe provenance.
	d := ev(cevents.CapabilityCompleted, map[string]interface{}{"capability": "weather.current", "provider": "open-meteo"})
	chip2, _ := Project(d)
	if !strings.Contains(chip2.Detail, "Open Meteo") {
		t.Fatalf("detail must show safe provenance: %q", chip2.Detail)
	}
	for _, banned := range []string{"exec", "strategy", "breaker"} {
		if strings.Contains(strings.ToLower(chip2.Detail), banned) {
			t.Fatalf("detail leaked implementation %q: %q", banned, chip2.Detail)
		}
	}
}

func TestMemoryChip(t *testing.T) {
	chip, ok := Project(ev(cevents.MemoryCreated, map[string]interface{}{"title": "Prefers tea over coffee"}))
	if !ok || chip.Title != "Remembered: Prefers tea over coffee" {
		t.Fatalf("memory chip wrong: %+v", chip)
	}
}

func TestDiagnosticsSafe(t *testing.T) {
	e := ev(cevents.CapabilityCompleted, map[string]interface{}{
		"provider": "open-meteo", "duration_ms": 382, "attempt": 1, "api_key": "«redacted 8 chars»",
	})
	d := Diagnostics(e)
	if d["provider"] != "open-meteo" || d["duration_ms"] != 382 {
		t.Fatalf("diagnostics missing safe fields: %v", d)
	}
	if _, ok := d["api_key"]; ok {
		t.Fatal("diagnostics must not include credential fields")
	}
}
