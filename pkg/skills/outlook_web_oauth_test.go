package skills

import (
	"context"
	"strings"
	"testing"
)

func testOutlookOAuthConfig() OutlookOAuthConfig {
	return OutlookOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
		RedirectURL:  "https://relay.example.com/oauth/outlook/callback",
	}
}

func TestOutlookScopesNarrowest(t *testing.T) {
	ro := OutlookScopesFor(false)
	if len(ro) != 2 || ro[0] != ScopeOutlookMailRead || ro[1] != ScopeOutlookCalendarsRead {
		t.Fatalf("read must be mail+calendar read-only: %v", ro)
	}
	w := OutlookScopesFor(true)
	if len(w) != 4 {
		t.Fatalf("write must add send scopes: %v", w)
	}
}

func TestOutlookBeginRequiresConfig(t *testing.T) {
	if _, _, err := OutlookOAuthBegin(OutlookOAuthConfig{}, "sess", "", false); err == nil {
		t.Fatal("begin without client config must fail")
	}
	if _, _, err := OutlookOAuthBegin(testOutlookOAuthConfig(), "", "", false); err == nil {
		t.Fatal("begin without session must fail")
	}
}

func TestOutlookBeginAndCompleteRoundtrip(t *testing.T) {
	t.Setenv("GHOST_CREDENTIALS_DIR", t.TempDir())
	url, state, err := OutlookOAuthBegin(testOutlookOAuthConfig(), "sess-1", "pending-123", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, "login.microsoftonline.com") {
		t.Fatalf("auth URL must point at Microsoft: %s", url)
	}
	if strings.Contains(url, "test-secret") {
		t.Fatal("auth URL must never contain client secret")
	}
	exch := func(ctx context.Context, cfg OutlookOAuthConfig, needWrite bool, code string) (*OutlookToken, error) {
		if code != "auth-code-abc" {
			t.Fatalf("unexpected code %q", code)
		}
		return &OutlookToken{RefreshToken: "refresh-xyz", AccessToken: "access-xyz"}, nil
	}
	valid := func(ctx context.Context, tok *OutlookToken) error { return nil }
	pendingID, err := OutlookOAuthComplete(testOutlookOAuthConfig(), state, "auth-code-abc", exch, valid)
	if err != nil {
		t.Fatal(err)
	}
	if pendingID != "pending-123" {
		t.Fatalf("must return pending ID to resume, got %q", pendingID)
	}
	st := OutlookWebStatus()
	if st.Status != CalendarReady || !st.Connected {
		t.Fatalf("must be ready after complete: %+v", st)
	}
	if _, err := OutlookOAuthComplete(testOutlookOAuthConfig(), state, "auth-code-abc", exch, valid); err == nil {
		t.Fatal("state replay must fail (CSRF)")
	}
	if err := OutlookWebDisconnect(); err != nil {
		t.Fatal(err)
	}
	if st := OutlookWebStatus(); st.Connected {
		t.Fatal("must need setup after disconnect")
	}
}
