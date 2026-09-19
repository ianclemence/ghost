package connector

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func serveManifest(t *testing.T, m *Manifest) *httptest.Server {
	t.Helper()
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(raw)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchUnsigned(t *testing.T) {
	srv := serveManifest(t, validNative())
	m, err := Fetch(context.Background(), srv.URL, FetchOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "acme-notes" {
		t.Fatalf("unexpected manifest: %+v", m)
	}
}

func TestFetchRequireSignature(t *testing.T) {
	pub, priv, _ := GenerateSigningKey()
	m := validNative()
	if err := Sign(m, priv, "alice"); err != nil {
		t.Fatal(err)
	}
	srv := serveManifest(t, m)

	if _, err := Fetch(context.Background(), srv.URL, FetchOptions{RequireSignature: true, TrustedKeys: []ed25519.PublicKey{pub}}); err != nil {
		t.Fatalf("trusted signature rejected: %v", err)
	}
	other, _, _ := GenerateSigningKey()
	if _, err := Fetch(context.Background(), srv.URL, FetchOptions{RequireSignature: true, TrustedKeys: []ed25519.PublicKey{other}}); err == nil {
		t.Fatal("untrusted signature must fail")
	}
	if _, err := Fetch(context.Background(), srv.URL, FetchOptions{RequireSignature: true}); err == nil {
		t.Fatal("require-signature with no trusted keys must fail")
	}
}

func TestFetchRequireSignatureUnsigned(t *testing.T) {
	srv := serveManifest(t, validNative())
	pub, _, _ := GenerateSigningKey()
	if _, err := Fetch(context.Background(), srv.URL, FetchOptions{RequireSignature: true, TrustedKeys: []ed25519.PublicKey{pub}}); err == nil {
		t.Fatal("unsigned manifest must fail when a signature is required")
	}
}

func TestFetchTampered(t *testing.T) {
	pub, priv, _ := GenerateSigningKey()
	m := validNative()
	if err := Sign(m, priv, "alice"); err != nil {
		t.Fatal(err)
	}
	m.DisplayName = "Tampered"
	srv := serveManifest(t, m)
	if _, err := Fetch(context.Background(), srv.URL, FetchOptions{RequireSignature: true, TrustedKeys: []ed25519.PublicKey{pub}}); err == nil {
		t.Fatal("tampered signed manifest must fail")
	}
}

func TestFetchOversize(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, maxManifestBytes+10))
	}))
	defer srv.Close()
	if _, err := Fetch(context.Background(), srv.URL, FetchOptions{}); err == nil {
		t.Fatal("oversized manifest must fail")
	}
}

func TestFetchBadScheme(t *testing.T) {
	if _, err := Fetch(context.Background(), "ftp://example/x", FetchOptions{}); err == nil {
		t.Fatal("non-http scheme must fail")
	}
}
