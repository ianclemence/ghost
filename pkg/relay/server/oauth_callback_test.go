package server

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestOAuthCallbackUnknownProvider(t *testing.T) {
	srv, _ := NewServer(Config{RegistryPath: tempRegistry(t)})
	req := httptest.NewRequest("GET", "/oauth/dropbox/callback?ghost=d1&code=c", nil)
	w := httptest.NewRecorder()
	srv.HandleHTTP(w, req)
	if w.Code != 404 {
		t.Fatalf("unknown provider must 404, got %d", w.Code)
	}
}

func TestOAuthCallbackRequiresGhost(t *testing.T) {
	srv, _ := NewServer(Config{RegistryPath: tempRegistry(t)})
	for _, target := range []string{
		"/oauth/calendar/callback?code=c&state=s",
		"/oauth/calendar/callback?ghost=&code=c",
		"/oauth/calendar/callback?ghost=../evil&code=c",
		"/oauth/calendar/callback?ghost=d1%20x&code=c",
	} {
		req := httptest.NewRequest("GET", target, nil)
		w := httptest.NewRecorder()
		srv.HandleHTTP(w, req)
		if w.Code != 400 {
			t.Fatalf("%s: expected 400, got %d", target, w.Code)
		}
	}
}

func TestOAuthCallbackOfflineHonest(t *testing.T) {
	srv, _ := NewServer(Config{RegistryPath: tempRegistry(t)})
	req := httptest.NewRequest("GET", "/oauth/calendar/callback?ghost=ghost-1&code=abc&state=def", nil)
	w := httptest.NewRecorder()
	srv.HandleHTTP(w, req)
	if w.Code != 503 {
		t.Fatalf("offline device must be honest 503, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "device offline") {
		t.Fatalf("must say device offline, got %q", w.Body.String())
	}
}

func TestOAuthCallbackMethodRejected(t *testing.T) {
	srv, _ := NewServer(Config{RegistryPath: tempRegistry(t)})
	req := httptest.NewRequest("POST", "/oauth/calendar/callback?ghost=d1", nil)
	w := httptest.NewRecorder()
	srv.HandleHTTP(w, req)
	if w.Code != 405 {
		t.Fatalf("POST must be rejected, got %d", w.Code)
	}
}

func TestOAuthCallbackRateLimited(t *testing.T) {
	srv, _ := NewServer(Config{RegistryPath: tempRegistry(t)})
	// Exhaust the limiter for this synthetic address.
	old := oauthLimiter
	defer func() { oauthLimiter = old }()
	oauthLimiter = &oauthRateLimit{hits: map[string][]time.Time{}, max: 2, window: 1000000000 * 60}
	_ = old
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/oauth/calendar/callback?ghost=d1&code=c", nil)
		req.RemoteAddr = "10.9.9.9:1234"
		w := httptest.NewRecorder()
		srv.HandleHTTP(w, req)
		if i < 2 && w.Code == 429 {
			t.Fatalf("request %d must pass, got 429", i)
		}
		if i == 2 && w.Code != 429 {
			t.Fatalf("request %d must be rate limited, got %d", i, w.Code)
		}
	}
}
