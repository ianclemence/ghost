package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func isolatedHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("GHOST_MASTER_KEY", "")
	return home
}

func TestAuthStoreSealedAtRest(t *testing.T) {
	isolatedHome(t)
	cred := &AuthCredential{
		AccessToken:  "secret-access-token",
		RefreshToken: "secret-refresh-token",
		Provider:     "openai",
		AuthMethod:   "oauth",
		ExpiresAt:    time.Now().Add(time.Hour),
	}
	if err := SetCredential("openai", cred); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(authFilePath())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "secret-access-token") {
		t.Fatal("auth store must be opaque at rest")
	}
	got, err := GetCredential("openai")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.AccessToken != "secret-access-token" {
		t.Fatalf("credential roundtrip failed: %+v", got)
	}
	info, err := os.Stat(filepath.Dir(authFilePath()))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0700 {
		t.Fatalf("auth dir perms = %o, want 700", info.Mode().Perm())
	}
}

func TestAuthStoreLegacyPlaintextUpgrades(t *testing.T) {
	home := isolatedHome(t)
	path := filepath.Join(home, ".GHOST", "auth.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"credentials":{"openai":{"access_token":"legacy-tok","provider":"openai","auth_method":"token"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := GetCredential("openai")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.AccessToken != "legacy-tok" {
		t.Fatalf("legacy load failed: %+v", got)
	}
	raw, _ := os.ReadFile(path)
	if strings.Contains(string(raw), "legacy-tok") {
		t.Fatal("legacy auth store must be sealed on load")
	}
}
