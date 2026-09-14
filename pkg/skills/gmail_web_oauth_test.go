package skills

import (
	"context"
	"strings"
	"testing"
)

func testGmailOAuthConfig() GmailOAuthConfig {
	return GmailOAuthConfig{
		ClientID:     "test-client-id.apps.googleusercontent.com",
		ClientSecret: "test-secret",
		RedirectURL:  "https://relay.example.com/oauth/gmail/callback",
	}
}

func TestGmailScopesNarrowest(t *testing.T) {
	ro := GmailScopesFor(false)
	if len(ro) != 1 || ro[0] != ScopeGmailReadonly {
		t.Fatalf("read must be readonly-only: %v", ro)
	}
	w := GmailScopesFor(true)
	if len(w) != 1 || w[0] != ScopeGmailSend {
		t.Fatalf("write must be send scope: %v", w)
	}
	for _, s := range append(ro, w...) {
		if s == "https://mail.google.com/" {
			t.Fatalf("never request full mailbox scope: %s", s)
		}
	}
}

func TestGmailBeginRequiresConfig(t *testing.T) {
	if _, _, err := GmailOAuthBegin(GmailOAuthConfig{}, "sess", "", false); err == nil {
		t.Fatal("begin without client config must fail")
	}
	if _, _, err := GmailOAuthBegin(testGmailOAuthConfig(), "", "", false); err == nil {
		t.Fatal("begin without session must fail")
	}
}

func TestGmailBeginAndCompleteRoundtrip(t *testing.T) {
	t.Setenv("GHOST_CREDENTIALS_DIR", t.TempDir())
	url, state, err := GmailOAuthBegin(testGmailOAuthConfig(), "sess-1", "pending-123", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, "accounts.google.com") {
		t.Fatalf("auth URL must point at Google: %s", url)
	}
	if strings.Contains(url, "test-secret") {
		t.Fatal("auth URL must never contain client secret")
	}
	if !strings.Contains(url, "gmail.readonly") {
		t.Fatalf("read flow must request readonly scope: %s", url)
	}
	exch := func(ctx context.Context, cfg GmailOAuthConfig, needWrite bool, code string) (*GmailToken, error) {
		if code != "auth-code-abc" {
			t.Fatalf("unexpected code %q", code)
		}
		return &GmailToken{RefreshToken: "refresh-xyz", AccessToken: "access-xyz"}, nil
	}
	valid := func(ctx context.Context, tok *GmailToken) error { return nil }
	pendingID, err := GmailOAuthComplete(testGmailOAuthConfig(), state, "auth-code-abc", exch, valid)
	if err != nil {
		t.Fatal(err)
	}
	if pendingID != "pending-123" {
		t.Fatalf("must return pending ID to resume, got %q", pendingID)
	}
	st := GmailWebStatus()
	if st.Status != CalendarReady || !st.Connected {
		t.Fatalf("must be ready after complete: %+v", st)
	}
	if _, err := GmailOAuthComplete(testGmailOAuthConfig(), state, "auth-code-abc", exch, valid); err == nil {
		t.Fatal("state replay must fail (CSRF)")
	}
	if err := GmailWebDisconnect(); err != nil {
		t.Fatal(err)
	}
	if st := GmailWebStatus(); st.Connected {
		t.Fatal("must need setup after disconnect")
	}
}
