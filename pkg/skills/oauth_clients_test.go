package skills

import (
	"os"
	"strings"
	"testing"
)

func TestOwnersAppIsStoredSealedAndNeverPlain(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHOST_CREDENTIALS_DIR", dir)
	t.Setenv("GHOST_GOOGLE_CLIENT_ID", "")
	t.Setenv("GHOST_GOOGLE_CLIENT_SECRET", "")
	if OAuthClientConfigured(ProviderGoogle) {
		t.Fatal("nothing is set up yet")
	}
	if err := SaveOAuthClient(ProviderGoogle, OAuthClient{ClientID: " id-123 ", ClientSecret: "s3cret-value"}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(oauthClientsPath())
	if strings.Contains(string(raw), "s3cret-value") || strings.Contains(string(raw), "id-123") {
		t.Fatal("the app's ID and secret must not sit on disk in plain text")
	}
	c := LoadOAuthClient(ProviderGoogle)
	if c.ClientID != "id-123" || c.ClientSecret != "s3cret-value" {
		t.Fatalf("round trip failed: %+v", c)
	}
	if cfg, ok := CalendarClientConfig(); !ok || cfg.RedirectURL != "http://localhost" {
		t.Errorf("Calendar must use the saved app and the loopback address: %+v %v", cfg, ok)
	}
	if cfg, ok := GmailClientConfigFromEnv(); !ok || cfg.ClientID != "id-123" {
		t.Error("Gmail shares the Google app")
	}
	if OAuthClientConfigured(ProviderSpotify) {
		t.Error("saving Google must not configure Spotify")
	}
}

func TestSavingNeedsBothPartsAndAKnownProvider(t *testing.T) {
	t.Setenv("GHOST_CREDENTIALS_DIR", t.TempDir())
	if SaveOAuthClient(ProviderGoogle, OAuthClient{ClientID: "only-id"}) == nil {
		t.Error("a missing secret must be refused")
	}
	if SaveOAuthClient("dropbox", OAuthClient{ClientID: "a", ClientSecret: "b"}) != ErrUnknownProvider {
		t.Error("an unknown provider must be refused")
	}
}

func TestEnvironmentStillOverrides(t *testing.T) {
	t.Setenv("GHOST_CREDENTIALS_DIR", t.TempDir())
	t.Setenv("GHOST_SPOTIFY_CLIENT_ID", "env-id")
	t.Setenv("GHOST_SPOTIFY_CLIENT_SECRET", "env-secret")
	cfg, ok := SpotifyClientConfigFromEnv()
	if !ok || cfg.ClientID != "env-id" || cfg.RedirectURL != "http://127.0.0.1:8888/callback" {
		t.Errorf("%+v %v", cfg, ok)
	}
}
