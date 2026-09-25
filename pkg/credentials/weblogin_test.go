package credentials

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWebLoginRoundTripSealedAndMasked(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("GHOST_CONFIG_DIR", dir)

	if err := SaveWebLogin(WebLogin{
		URL: "https://accounts.example.com/login", Username: "ian@example.com", Password: "hunter2",
	}); err != nil {
		t.Fatalf("SaveWebLogin: %v", err)
	}

	// Lookup by the host derived from the URL.
	l, ok := WebLoginFor("accounts.example.com")
	if !ok {
		t.Fatal("saved login not found by host")
	}
	if l.Username != "ian@example.com" || l.Password != "hunter2" {
		t.Fatalf("roundtrip lost credentials: %+v", l)
	}

	// Owner-facing list: recognises the entry, never reveals the secret.
	metas := ListWebLogins()
	if len(metas) != 1 {
		t.Fatalf("ListWebLogins = %+v, want one entry", metas)
	}
	if metas[0].Host != "accounts.example.com" {
		t.Errorf("host = %q", metas[0].Host)
	}
	if bytes.Contains([]byte(metas[0].Username), []byte("ian@example.com")) {
		t.Errorf("username must be masked, got %q", metas[0].Username)
	}

	// At rest the password is sealed, never plaintext.
	raw, err := os.ReadFile(filepath.Join(dir, ".secrets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("hunter2")) {
		t.Fatal("password must not appear in the secrets file in plaintext")
	}

	if err := DeleteWebLogin("accounts.example.com"); err != nil {
		t.Fatalf("DeleteWebLogin: %v", err)
	}
	if _, ok := WebLoginFor("accounts.example.com"); ok {
		t.Fatal("deleted login must be gone")
	}
}

func TestSaveWebLoginValidates(t *testing.T) {
	t.Setenv("GHOST_CONFIG_DIR", t.TempDir())
	cases := []WebLogin{
		{URL: "ftp://example.com", Username: "u", Password: "p"},
		{URL: "https://example.com/login", Password: "p"},
		{URL: "https://example.com/login", Username: "u"},
	}
	for _, c := range cases {
		if err := SaveWebLogin(c); err == nil {
			t.Errorf("SaveWebLogin(%+v) should be refused", c)
		}
	}
}

func TestNormalizeWebHost(t *testing.T) {
	cases := map[string]string{
		"https://accounts.example.com/login?x=1": "accounts.example.com",
		"HTTPS://WWW.Example.COM":                "example.com",
		"example.com:8443":                       "example.com",
		"www.example.com/path":                   "example.com",
	}
	for in, want := range cases {
		if got := NormalizeWebHost(in); got != want {
			t.Errorf("NormalizeWebHost(%q) = %q, want %q", in, got, want)
		}
	}
}
