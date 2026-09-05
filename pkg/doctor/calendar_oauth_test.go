package doctor

import (
	"context"
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

func TestDoctorRunAllIncludesCalendarOAuth(t *testing.T) {
	d := &Doctor{}
	found := false
	for _, r := range d.RunAll(context.Background()) {
		if r.Name == "calendar_oauth" {
			found = true
		}
	}
	if !found {
		t.Fatal("RunAll must include the calendar_oauth check")
	}
}
