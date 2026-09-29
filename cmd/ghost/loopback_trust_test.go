package main

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func loopbackReq(method, host string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(method, "http://"+host+"/v1/chat", nil)
	r.RemoteAddr = "127.0.0.1:50123"
	r.Host = host
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

// Loopback trust is for local programs (TUI, CLI, relay, the console's
// proxy). A web page open in the owner's browser also reaches 127.0.0.1;
// it must not inherit that trust — neither by a cross-site request (CSRF:
// a no-preflight text/plain POST runs a chat turn as the owner) nor by DNS
// rebinding (the page becomes same-origin with the gateway under a foreign
// Host name).
func TestLoopbackTrustRejectsBrowserContexts(t *testing.T) {
	cases := []struct {
		name    string
		host    string
		headers map[string]string
		trusted bool
	}{
		{"native client", "127.0.0.1:18790", nil, true},
		{"localhost name", "localhost:18790", nil, true},
		{"same-machine console origin", "127.0.0.1:18790", map[string]string{"Origin": "http://localhost"}, true},
		{"cross-site page", "127.0.0.1:18790", map[string]string{"Origin": "https://evil.example"}, false},
		{"sandboxed page", "127.0.0.1:18790", map[string]string{"Origin": "null"}, false},
		{"cross-site fetch metadata", "127.0.0.1:18790", map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		{"dns rebinding", "rebind.evil.example:18790", map[string]string{"Origin": "http://rebind.evil.example:18790"}, false},
		{"dns rebinding, no origin", "rebind.evil.example:18790", nil, false},
	}
	for _, tc := range cases {
		if got := isLoopbackRequest(loopbackReq(http.MethodPost, tc.host, tc.headers)); got != tc.trusted {
			t.Errorf("%s: trusted=%v, want %v", tc.name, got, tc.trusted)
		}
	}
	lan := loopbackReq(http.MethodPost, "127.0.0.1:18790", nil)
	lan.RemoteAddr = "192.168.1.50:40000"
	if isLoopbackRequest(lan) {
		t.Error("a LAN peer is never loopback-trusted")
	}
}

// A cross-site page could open a WebSocket to the gateway and receive
// the owner's live conversation: browsers apply no CORS to WebSockets, and
// the upgrader accepted every origin.
func TestWebSocketUpgraderChecksOrigin(t *testing.T) {
	if upgrader.CheckOrigin(loopbackReq(http.MethodGet, "127.0.0.1:18790", map[string]string{"Origin": "https://evil.example"})) {
		t.Fatal("cross-site WebSocket origin accepted")
	}
	if !upgrader.CheckOrigin(loopbackReq(http.MethodGet, "127.0.0.1:18790", nil)) {
		t.Fatal("native client (no Origin) must be accepted")
	}
	if !upgrader.CheckOrigin(loopbackReq(http.MethodGet, "127.0.0.1:18790", map[string]string{"Origin": "http://127.0.0.1:8080"})) {
		t.Fatal("same-machine origin must be accepted")
	}
}

// The chat endpoint must not run for a cross-site page even though its
// TCP peer is loopback.
func TestAuthMiddlewareBlocksCrossSiteLoopback(t *testing.T) {
	ran := false
	h := authMiddleware(func(w http.ResponseWriter, r *http.Request) { ran = true })
	rec := httptest.NewRecorder()
	h(rec, loopbackReq(http.MethodPost, "127.0.0.1:18790", map[string]string{"Origin": "https://evil.example", "Content-Type": "text/plain"}))
	if ran || rec.Code != http.StatusUnauthorized {
		t.Fatalf("cross-site loopback POST reached the handler (code %d)", rec.Code)
	}
}

// A turn that failed because the provider account is out of credit tells
// the owner so — it used to surface as "no available providers".
func TestFriendlyAgentErrorExplainsBilling(t *testing.T) {
	err := fmt.Errorf("LLM call failed: %w", fmt.Errorf("all models cooling down after a recent failure: %w",
		errors.New("API request failed:\n  Status: 402\n  Body: {\"message\":\"Insufficient Balance\"}")))
	if got := friendlyAgentError(err); !strings.Contains(got, "out of credit") {
		t.Fatalf("got %q", got)
	}
	// Non-model errors keep their own text.
	if got := friendlyAgentError(errors.New("tasks store locked")); got != "tasks store locked" {
		t.Fatalf("non-model error rewritten: %q", got)
	}
}
