package credentials

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCollectSecretValuesFindsKeysTokensAndWebLoginPasswords(t *testing.T) {
	p := filepath.Join(t.TempDir(), ".secrets.json")
	os.WriteFile(p, []byte(`{
	  "provider_api_keys": {"deepseek": "sk-abcdef123456", "weblogin:bank.example": "{\"host\":\"bank.example\",\"username\":\"sam\",\"password\":\"correct-horse-7\"}", "google-calendar": "oauth:connected"},
	  "telegram_token": "123456:ABC-def", "brave_api_key": "abc"}`), 0600)
	got := map[string]bool{}
	for _, s := range collectSecretValues(p) {
		got[s] = true
	}
	for _, want := range []string{"sk-abcdef123456", "correct-horse-7", "123456:ABC-def"} {
		if !got[want] {
			t.Errorf("missing secret %q in %v", want, got)
		}
	}
	if got["oauth:connected"] || got["abc"] || got["sam"] {
		t.Errorf("must skip placeholders, too-short strings and usernames: %v", got)
	}
}

// The vault is sealed on disk: what it holds must still be found to scrub.
func TestSecretValuesReadTheSealedVault(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHOST_CONFIG_DIR", dir)
	if err := SaveWebLogin(WebLogin{URL: "https://example.com/login", Username: "ian", Password: "web-pass-123"}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range collectSecretValues(dir + "/.secrets.json") {
		found = found || v == "web-pass-123"
	}
	if !found {
		t.Fatal("a sealed vault's secrets are not scrubbed")
	}
}
