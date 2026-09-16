package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func enableSkill(t *testing.T, ws, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(ws, "skills", name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(ws, "skills", name, "SKILL.md"), []byte("---\nname: "+name+"\n---\n\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// All services unconfigured: one warning row naming every missing service,
// not six rows.
func TestConnectedServicesAllMissing(t *testing.T) {
	for _, kv := range [][2]string{
		{"GHOST_GOOGLE_CLIENT_ID", ""}, {"GHOST_GOOGLE_CLIENT_SECRET", ""},
		{"GHOST_CALENDAR_REDIRECT_URL", ""}, {"GHOST_GMAIL_REDIRECT_URL", ""},
		{"GHOST_OUTLOOK_CLIENT_ID", ""}, {"GHOST_OUTLOOK_CLIENT_SECRET", ""},
		{"GHOST_OUTLOOK_REDIRECT_URL", ""}, {"GHOST_SPOTIFY_CLIENT_ID", ""},
		{"GHOST_SPOTIFY_CLIENT_SECRET", ""}, {"GHOST_SPOTIFY_REDIRECT_URL", ""},
	} {
		t.Setenv(kv[0], kv[1])
	}
	ws := t.TempDir()
	for _, s := range []string{"calendar", "email", "spotify", "github", "notion"} {
		enableSkill(t, ws, s)
	}
	d := &Doctor{workspace: ws}
	res := d.checkConnectedServices(context.Background())
	if res.Status != "warning" {
		t.Fatalf("all-missing must warn, got %s: %s", res.Status, res.Message)
	}
	for _, want := range []string{"Calendar", "Gmail", "Outlook", "Spotify", "GitHub", "Notion"} {
		if !strings.Contains(res.Message, want) {
			t.Fatalf("aggregate must name %q: %q", want, res.Message)
		}
	}
	if !strings.Contains(res.Message, "Connected Apps") {
		t.Fatalf("aggregate must point at Connected Apps: %q", res.Message)
	}
}

// A misconfigured service (malformed redirect) escalates the aggregate to
// error instead of hiding behind a warning.
func TestConnectedServicesErrorPropagates(t *testing.T) {
	t.Setenv("GHOST_GOOGLE_CLIENT_ID", "x.apps.googleusercontent.com")
	t.Setenv("GHOST_GOOGLE_CLIENT_SECRET", "s")
	t.Setenv("GHOST_CALENDAR_REDIRECT_URL", "://bad-url")
	ws := t.TempDir()
	enableSkill(t, ws, "calendar")
	d := &Doctor{workspace: ws}
	res := d.checkConnectedServices(context.Background())
	if res.Status != "error" {
		t.Fatalf("malformed redirect must error, got %s: %s", res.Status, res.Message)
	}
	if !strings.Contains(res.Message, "Calendar") {
		t.Fatalf("error must name the service: %q", res.Message)
	}
}

// Resources reports RAM only: disk has its own authority and must not be
// repeated here.
func TestResourcesIsRAMOnly(t *testing.T) {
	d := &Doctor{workspace: t.TempDir()}
	res := d.checkResources(context.Background())
	if strings.Contains(strings.ToLower(res.Message), "disk") {
		t.Fatalf("resources must not mention disk: %q", res.Message)
	}
	if !strings.Contains(res.Message, "RAM") {
		t.Fatalf("resources must report RAM: %q", res.Message)
	}
}
