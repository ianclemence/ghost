package skills

import (
	"context"
	"strings"
	"testing"
)

func testSpotifyOAuthConfig() SpotifyOAuthConfig {
	return SpotifyOAuthConfig{
		ClientID:     "test-client-id",
		ClientSecret: "test-secret",
		RedirectURL:  "https://relay.example.com/oauth/spotify/callback",
	}
}

func TestSpotifyScopesNarrowest(t *testing.T) {
	ro := SpotifyScopesFor(false)
	if len(ro) != 2 || ro[0] != ScopeSpotifyReadPlayback {
		t.Fatalf("read must be read-only playback scopes: %v", ro)
	}
	w := SpotifyScopesFor(true)
	if len(w) != 3 || w[2] != ScopeSpotifyModify {
		t.Fatalf("write must add modify scope: %v", w)
	}
}

func TestSpotifyBeginRequiresConfig(t *testing.T) {
	if _, _, err := SpotifyOAuthBegin(SpotifyOAuthConfig{}, "sess", "", false); err == nil {
		t.Fatal("begin without client config must fail")
	}
	if _, _, err := SpotifyOAuthBegin(testSpotifyOAuthConfig(), "", "", false); err == nil {
		t.Fatal("begin without session must fail")
	}
}

func TestSpotifyBeginAndCompleteRoundtrip(t *testing.T) {
	t.Setenv("GHOST_CREDENTIALS_DIR", t.TempDir())
	url, state, err := SpotifyOAuthBegin(testSpotifyOAuthConfig(), "sess-1", "pending-123", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(url, "accounts.spotify.com") {
		t.Fatalf("auth URL must point at Spotify: %s", url)
	}
	if strings.Contains(url, "test-secret") {
		t.Fatal("auth URL must never contain client secret")
	}
	exch := func(ctx context.Context, cfg SpotifyOAuthConfig, needWrite bool, code string) (*SpotifyToken, error) {
		if code != "auth-code-abc" {
			t.Fatalf("unexpected code %q", code)
		}
		return &SpotifyToken{RefreshToken: "refresh-xyz", AccessToken: "access-xyz"}, nil
	}
	valid := func(ctx context.Context, tok *SpotifyToken) error { return nil }
	pendingID, err := SpotifyOAuthComplete(testSpotifyOAuthConfig(), state, "auth-code-abc", exch, valid)
	if err != nil {
		t.Fatal(err)
	}
	if pendingID != "pending-123" {
		t.Fatalf("must return pending ID to resume, got %q", pendingID)
	}
	st := SpotifyWebStatus()
	if st.Status != CalendarReady || !st.Connected {
		t.Fatalf("must be ready after complete: %+v", st)
	}
	if _, err := SpotifyOAuthComplete(testSpotifyOAuthConfig(), state, "auth-code-abc", exch, valid); err == nil {
		t.Fatal("state replay must fail (CSRF)")
	}
	if err := SpotifyWebDisconnect(); err != nil {
		t.Fatal(err)
	}
	if st := SpotifyWebStatus(); st.Connected {
		t.Fatal("must need setup after disconnect")
	}
}
