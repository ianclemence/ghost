package credentials

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestRefNeverCarriesSecrets(t *testing.T) {
	v := New(t.TempDir())
	t.Setenv("AVIATION_API_KEY", "SECRET-XYZ-123")
	ref := v.Ref("aviationstack")
	if ref.Status != StatusConnected {
		t.Fatalf("must detect configured key, got %s", ref.Status)
	}
	raw, _ := json.Marshal(ref)
	s := string(raw)
	if strings.Contains(s, "SECRET-XYZ-123") {
		t.Fatal("metadata must never carry secret values")
	}
	for _, k := range []string{"api_key", "secret", "token\""} {
		if strings.Contains(s, `"`+k+`":"`) && k != "token\"" {
			t.Fatalf("suspicious key in metadata: %s", k)
		}
	}
}

func TestNotConfigured(t *testing.T) {
	v := New(t.TempDir())
	t.Setenv("AVIATION_API_KEY", "")
	t.Setenv("GHOST_CONFIG_DIR", t.TempDir())
	ref := v.Ref("aviationstack")
	if ref.Status != StatusNotConfigured {
		t.Fatalf("absent key must be not_configured, got %s", ref.Status)
	}
}

func TestStoreValidateLifecycle(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	if err := v.Store("testprov", "key-123"); err != nil {
		t.Fatal(err)
	}
	if got := v.Ref("testprov").Status; got != StatusConfiguring {
		t.Fatalf("after store must be configuring, got %s", got)
	}
	if st := v.Validate("testprov", func(secret string) error {
		if secret != "key-123" {
			t.Fatal("validator must receive the exact secret")
		}
		return nil
	}); st != StatusConnected {
		t.Fatalf("valid must connect, got %s", st)
	}
	if st := v.Validate("testprov", func(secret string) error {
		return errors.New("401 unauthorized")
	}); st != StatusInvalid {
		t.Fatalf("401 must invalidate, got %s", st)
	}
	if st := v.Validate("testprov", func(secret string) error {
		return errors.New("token revoked: invalid_grant")
	}); st != StatusRevoked {
		t.Fatalf("revocation must revoke, got %s", st)
	}
}

func TestTransportErrorKeepsGoodState(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	v.Store("testprov", "k")
	v.Validate("testprov", func(s string) error { return nil })
	if st := v.Validate("testprov", func(s string) error {
		return errors.New("connection reset")
	}); st != StatusConnected {
		t.Fatalf("transport blip must not invalidate good credential, got %s", st)
	}
}

func TestDisconnect(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	v.Store("testprov", "k")
	if err := v.Disconnect("testprov"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TESTPROV_API_KEY", "")
	if got := v.Ref("testprov").Status; got != StatusDisconnected {
		t.Fatalf("must be disconnected, got %s", got)
	}
}

func TestUseBoundary(t *testing.T) {
	v := New(t.TempDir())
	if err := v.Use("missing", func(s string) error { return nil }); err == nil {
		t.Fatal("missing credential must fail")
	}
	v.Store("testprov", "k-sekrit")
	called := false
	if err := v.Use("testprov", func(s string) error {
		called = true
		if s != "k-sekrit" {
			t.Fatal("wrong secret")
		}
		return nil
	}); err != nil || !called {
		t.Fatal("Use must deliver the secret to fn")
	}
}

func TestListConnectionsModel(t *testing.T) {
	v := New(t.TempDir())
	list := v.List()
	if len(list) == 0 {
		t.Fatal("must list known providers")
	}
	raw, _ := json.Marshal(list)
	if strings.Contains(string(raw), "sk-") {
		t.Fatal("list must not contain secrets")
	}
	seen := map[string]bool{}
	for _, c := range list {
		seen[c.ID] = true
		if c.DisplayName == "" || c.Category == "" {
			t.Fatalf("incomplete connection entry: %+v", c)
		}
	}
	for _, id := range []string{"google-calendar", "telegram", "openai"} {
		if !seen[id] {
			t.Fatalf("missing provider %s", id)
		}
	}
}

func TestEmitterLifecycle(t *testing.T) {
	dir := t.TempDir()
	v := New(dir)
	var got []string
	v.SetEmitter(func(t, p string) { got = append(got, t+":"+p) })
	v.Store("testprov", "k")
	v.Validate("testprov", func(s string) error { return nil })
	v.Validate("testprov", func(s string) error { return errors.New("revoked") })
	found := map[string]bool{}
	for _, e := range got {
		found[e] = true
	}
	if !found["credential.validated:testprov"] || !found["credential.revoked:testprov"] {
		t.Fatalf("missing lifecycle events: %v", got)
	}
}
