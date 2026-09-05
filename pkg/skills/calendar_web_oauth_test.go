package skills

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testOAuthConfig() CalendarOAuthConfig {
	return CalendarOAuthConfig{
		ClientID:     "test-client-id.apps.googleusercontent.com",
		ClientSecret: "test-secret",
		RedirectURL:  "https://relay.example.com/oauth/calendar/callback",
	}
}

func TestScopesNarrowest(t *testing.T) {
	ro := ScopesFor(false)
	if len(ro) != 1 || ro[0] != ScopeCalendarReadonly {
		t.Fatalf("read must be readonly-only: %v", ro)
	}
	w := ScopesFor(true)
	if len(w) != 1 || w[0] != ScopeCalendarEvents {
		t.Fatalf("write must be events scope: %v", w)
	}
	for _, s := range append(ro, w...) {
		if !strings.Contains(s, "calendar") || strings.HasSuffix(s, "calendar") {
			t.Fatalf("never request full calendar scope: %s", s)
		}
	}
}

func TestBeginRequiresConfig(t *testing.T) {
	if _, _, err := CalendarOAuthBegin(CalendarOAuthConfig{}, "sess", "", false); err == nil {
		t.Fatal("begin without client config must fail")
	}
	if _, _, err := CalendarOAuthBegin(testOAuthConfig(), "", "", false); err == nil {
		t.Fatal("begin without session must fail")
	}
}

func TestBeginAndCompleteRoundtrip(t *testing.T) {
	t.Setenv("GHOST_CREDENTIALS_DIR", t.TempDir())
	url, state, err := CalendarOAuthBegin(testOAuthConfig(), "sess-1", "pending-123", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, "accounts.google.com") {
		t.Fatalf("auth URL must point at Google: %s", url)
	}
	if strings.Contains(url, "test-secret") {
		t.Fatal("auth URL must never contain client secret")
	}
	if !strings.Contains(url, "calendar.readonly") {
		t.Fatalf("read flow must request readonly scope: %s", url)
	}
	exch := func(ctx context.Context, cfg CalendarOAuthConfig, needWrite bool, code string) (*CalendarToken, error) {
		if code != "auth-code-abc" {
			t.Fatalf("unexpected code %q", code)
		}
		return &CalendarToken{RefreshToken: "refresh-xyz", AccessToken: "access-xyz"}, nil
	}
	valid := func(ctx context.Context, tok *CalendarToken) error { return nil }
	pendingID, err := CalendarOAuthComplete(testOAuthConfig(), state, "auth-code-abc", exch, valid)
	if err != nil {
		t.Fatal(err)
	}
	if pendingID != "pending-123" {
		t.Fatalf("must return pending ID to resume, got %q", pendingID)
	}
	st := CalendarWebStatus()
	if st.Status != CalendarReady || !st.Connected {
		t.Fatalf("must be ready after complete: %+v", st)
	}
	// State is single-use: replay must fail.
	if _, err := CalendarOAuthComplete(testOAuthConfig(), state, "auth-code-abc", exch, valid); err == nil {
		t.Fatal("state replay must fail (CSRF)")
	}
}

func TestBadStateRejected(t *testing.T) {
	t.Setenv("GHOST_CREDENTIALS_DIR", t.TempDir())
	exch := func(ctx context.Context, cfg CalendarOAuthConfig, w bool, code string) (*CalendarToken, error) {
		return &CalendarToken{RefreshToken: "r"}, nil
	}
	valid := func(ctx context.Context, tok *CalendarToken) error { return nil }
	if _, err := CalendarOAuthComplete(testOAuthConfig(), "forged-state", "code", exch, valid); err == nil {
		t.Fatal("forged state must be rejected")
	}
	if _, err := CalendarOAuthComplete(testOAuthConfig(), "", "code", exch, valid); err == nil {
		t.Fatal("empty state must be rejected")
	}
}

func TestFailedValidationNotReady(t *testing.T) {
	t.Setenv("GHOST_CREDENTIALS_DIR", t.TempDir())
	_, state, err := CalendarOAuthBegin(testOAuthConfig(), "sess", "", false)
	if err != nil {
		t.Fatal(err)
	}
	exch := func(ctx context.Context, cfg CalendarOAuthConfig, w bool, code string) (*CalendarToken, error) {
		return &CalendarToken{RefreshToken: "r", AccessToken: "a"}, nil
	}
	valid := func(ctx context.Context, tok *CalendarToken) error {
		return context.DeadlineExceeded // simulate API failure, classified upstream
	}
	_ = valid
	badValid := func(ctx context.Context, tok *CalendarToken) error {
		return &validationTestErr{"calendar_oauth_unauthorized"}
	}
	if _, err := CalendarOAuthComplete(testOAuthConfig(), state, "code", exch, badValid); err == nil {
		t.Fatal("failed validation must not complete")
	}
	if st := CalendarWebStatus(); st.Connected {
		t.Fatal("must not be ready after failed validation")
	}
}

type validationTestErr struct{ s string }

func (e *validationTestErr) Error() string { return e.s }

func TestTokenFilePermsAndRedaction(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHOST_CREDENTIALS_DIR", dir)
	if err := storeCalendarToken(&CalendarToken{RefreshToken: "secret-refresh", AccessToken: "secret-access"}); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(filepath.Join(dir, "calendar-token.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0600 {
		t.Fatalf("token file must be 0600, got %o", fi.Mode().Perm())
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "calendar-token.json"))
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	diag := CalendarRedactedDiagnostics()
	if !diag.Configured {
		t.Fatal("diagnostics must show configured")
	}
	// Diagnostics JSON must never contain credential material.
	dj, _ := json.Marshal(diag)
	if strings.Contains(string(dj), "secret-refresh") || strings.Contains(string(dj), "secret-access") {
		t.Fatal("diagnostics must redact credentials")
	}
	// Disconnect removes the credential.
	if err := CalendarWebDisconnect(); err != nil {
		t.Fatal(err)
	}
	if st := CalendarWebStatus(); st.Connected {
		t.Fatal("must need setup after disconnect")
	}
}

func TestClassifyOAuthErrorNoLeak(t *testing.T) {
	err := classifyOAuthError(context.DeadlineExceeded)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("classified errors must be reason codes")
	}
	if got := classifyOAuthError(errorf("invalid_grant: Token has been revoked")); got.Error() != "calendar_oauth_revoked_or_expired" {
		t.Fatalf("revocation must map to reauth, got %v", got)
	}
}

type errString string

func errorf(s string) error { return errString(s) }

func (e errString) Error() string { return string(e) }
