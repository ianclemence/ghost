package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCalendarOAuthUnconfigured(t *testing.T) {
	t.Setenv("GHOST_GOOGLE_CLIENT_ID", "")
	t.Setenv("GHOST_GOOGLE_CLIENT_SECRET", "")
	t.Setenv("GHOST_CALENDAR_REDIRECT_URL", "")
	d := &Doctor{}
	res := d.checkCalendarOAuth(context.Background())
	if res.Status != "warning" {
		t.Fatalf("unconfigured must warn (progressive setup), got %s", res.Status)
	}
}

func TestCalendarOAuthBadClientID(t *testing.T) {
	t.Setenv("GHOST_GOOGLE_CLIENT_ID", "not-a-client-id")
	t.Setenv("GHOST_GOOGLE_CLIENT_SECRET", "s")
	t.Setenv("GHOST_CALENDAR_REDIRECT_URL", "https://relay.example.com/oauth/calendar/callback")
	d := &Doctor{}
	res := d.checkCalendarOAuth(context.Background())
	if res.Status != "error" {
		t.Fatalf("bad client id must error, got %s", res.Status)
	}
}

func TestCalendarOAuthNonHTTPSWarns(t *testing.T) {
	t.Setenv("GHOST_GOOGLE_CLIENT_ID", "x.apps.googleusercontent.com")
	t.Setenv("GHOST_GOOGLE_CLIENT_SECRET", "s")
	t.Setenv("GHOST_CALENDAR_REDIRECT_URL", "http://192.168.1.10/oauth/calendar/callback")
	d := &Doctor{}
	res := d.checkCalendarOAuth(context.Background())
	if res.Status != "warning" {
		t.Fatalf("LAN http must warn (not error), got %s: %s", res.Status, res.Message)
	}
}

func TestDoctorRunAllAggregatesServices(t *testing.T) {
	d := &Doctor{}
	found := false
	for _, r := range d.RunAll(context.Background()) {
		if r.Name == "calendar_oauth" || r.Name == "gmail_oauth" || r.Name == "github_token" {
			t.Fatalf("per-service row %q must not appear; services aggregate", r.Name)
		}
		if r.Name == "connected_services" {
			found = true
			if r.Status != "warning" {
				t.Fatalf("unconfigured services must warn, got %s: %s", r.Status, r.Message)
			}
		}
	}
	if !found {
		t.Fatal("RunAll must include the connected_services aggregate")
	}
}

// A disabled calendar skill must not nag the owner to configure sign-in.
func TestCalendarOAuthSuppressedWhenSkillDisabled(t *testing.T) {
	t.Setenv("GHOST_GOOGLE_CLIENT_ID", "")
	t.Setenv("GHOST_GOOGLE_CLIENT_SECRET", "")
	t.Setenv("GHOST_CALENDAR_REDIRECT_URL", "")
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "skills", "calendar"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "skills", "calendar", "SKILL.md.disabled"), []byte("disabled"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Doctor{workspace: ws}
	res := d.checkCalendarOAuth(context.Background())
	if res.Status != "ok" {
		t.Fatalf("disabled calendar skill must not warn, got %s: %s", res.Status, res.Message)
	}
}

// When the calendar skill IS enabled but unconfigured, the warning stays.
func TestCalendarOAuthWarnsWhenSkillEnabled(t *testing.T) {
	t.Setenv("GHOST_GOOGLE_CLIENT_ID", "")
	t.Setenv("GHOST_GOOGLE_CLIENT_SECRET", "")
	t.Setenv("GHOST_CALENDAR_REDIRECT_URL", "")
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "skills", "calendar"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "skills", "calendar", "SKILL.md"), []byte("---\nname: calendar\n---\n\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Doctor{workspace: ws}
	res := d.checkCalendarOAuth(context.Background())
	if res.Status != "warning" {
		t.Fatalf("enabled-but-unconfigured calendar must still warn, got %s", res.Status)
	}
}

// The aggregate must not mention calendar at all when the skill is off —
// a disabled capability has nothing to diagnose. With only the calendar
// skill dir present-but-disabled, every service is excluded, so the
// aggregate itself is info and omitted from RunAll.
func TestRunAllOmitsCalendarCheckWhenSkillDisabled(t *testing.T) {
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "skills", "calendar"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "skills", "calendar", "SKILL.md.disabled"), []byte("disabled"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Doctor{workspace: ws}
	for _, r := range d.RunAll(context.Background()) {
		if r.Name == "calendar_oauth" {
			t.Fatal("disabled skill must not appear in diagnostics")
		}
		if r.Name == "connected_services" {
			t.Fatal("aggregate with zero applicable services must be omitted")
		}
	}
}

// ...but an enabled-but-unconfigured calendar surfaces inside the aggregate.
func TestRunAllIncludesCalendarCheckWhenSkillEnabled(t *testing.T) {
	t.Setenv("GHOST_GOOGLE_CLIENT_ID", "")
	t.Setenv("GHOST_GOOGLE_CLIENT_SECRET", "")
	t.Setenv("GHOST_CALENDAR_REDIRECT_URL", "")
	ws := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws, "skills", "calendar"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "skills", "calendar", "SKILL.md"), []byte("---\nname: calendar\n---\n\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := &Doctor{workspace: ws}
	found := false
	for _, r := range d.RunAll(context.Background()) {
		if r.Name == "connected_services" {
			found = true
			if r.Status != "warning" {
				t.Fatalf("unconfigured calendar must warn, got %s: %s", r.Status, r.Message)
			}
			if !strings.Contains(r.Message, "Calendar") {
				t.Fatalf("aggregate must name the missing service: %q", r.Message)
			}
		}
	}
	if !found {
		t.Fatal("enabled skill must appear in diagnostics via the aggregate")
	}
}
