package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ianclemence/ghost/pkg/localtrust"
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

// The phone sends attachments as objects under "media"; the server once
// decoded that field as strings and rejected every photo. Both shapes decode.
func TestChatRequestAcceptsBothAttachmentShapes(t *testing.T) {
	for name, body := range map[string]string{
		"object": `{"content":"hi","media":[{"base64":"aGk=","mime_type":"image/jpeg"}]}`,
		"string": `{"content":"hi","media":["aGk="]}`,
		"items":  `{"content":"hi","media_items":[{"base64":"aGk=","filename":"a.txt"}]}`,
	} {
		var r internalAPIRequest
		if err := json.Unmarshal([]byte(body), &r); err != nil {
			t.Errorf("%s: %v", name, err)
		}
		if len(r.Media)+len(r.MediaItems) != 1 {
			t.Errorf("%s: want one attachment, got %+v", name, r)
		}
	}
}

// A request the relay replays on the Pod's own loopback came from the internet.
// It must not inherit "this is the owner" trust.
func TestRelayedRequestsAreNotTrustedAsLocal(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/files", nil)
	req.RemoteAddr = "127.0.0.1:41000"
	req.Host = "127.0.0.1:8766"
	if !isLoopbackRequest(req) {
		t.Fatal("a genuine local request is the owner")
	}
	req.Header.Set("X-Ghost-Via", "relay")
	if isLoopbackRequest(req) {
		t.Fatal("a relayed request must not count as loopback")
	}
	h := authMiddleware(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("a relayed request without device credentials must be refused, got %d", rec.Code)
	}
}

// A command the model runs in its shell reaches 127.0.0.1 like any local
// program. For routes that grant authority, mint credentials or change policy,
// loopback alone must not be trust: the local token, which the sandbox cannot
// read, has to come with it.
func TestLoopbackNeedsLocalTokenForSensitiveRoutes(t *testing.T) {
	const tok = "0123456789abcdef0123456789abcdef"
	localtrust.Accept(tok)
	t.Cleanup(func() { localtrust.Accept("") })

	mk := func(method, path, token string) *http.Request {
		r := httptest.NewRequest(method, "http://127.0.0.1:18790"+path, nil)
		r.RemoteAddr = "127.0.0.1:50123"
		if token != "" {
			r.Header.Set(localtrust.Header, token)
		}
		return r
	}
	cases := []struct {
		name   string
		method string
		path   string
		token  string
		want   bool
	}{
		{"resolve without token", http.MethodPost, "/v1/permissions/resolve", "", false},
		{"resolve with wrong token", http.MethodPost, "/v1/permissions/resolve", "nope", false},
		{"resolve with token", http.MethodPost, "/v1/permissions/resolve", tok, true},
		{"grants without token", http.MethodPost, "/v1/permissions/grants", "", false},
		{"mode without token", http.MethodPost, "/v1/mode", "", false},
		{"pairing invitation without token", http.MethodPost, "/v1/pairing/invitations", "", false},
		{"pairing invitation with token", http.MethodPost, "/v1/pairing/invitations", tok, true},
		{"skill install without token", http.MethodPost, "/v1/skills/install", "", false},
		{"live takeover without token", http.MethodPost, "/v1/live/surfaces/x/release", "", false},
		{"password reset read without token", http.MethodGet, "/v1/console/password-reset", "", false},
		{"website logins read without token", http.MethodGet, "/v1/website-logins", "", false},
		{"chat stays loopback-trusted", http.MethodPost, "/v1/chat", "", true},
		{"listing requests stays loopback-trusted", http.MethodGet, "/v1/permissions/requests", "", true},
		{"health stays loopback-trusted", http.MethodGet, "/v1/health", "", true},
	}
	for _, tc := range cases {
		if got := loopbackTrusted(mk(tc.method, tc.path, tc.token)); got != tc.want {
			t.Errorf("%s: trusted=%v, want %v", tc.name, got, tc.want)
		}
	}
	// With no token configured the sensitive routes refuse everyone.
	localtrust.Accept("")
	if loopbackTrusted(mk(http.MethodPost, "/v1/permissions/resolve", tok)) {
		t.Error("with no served token, sensitive routes must fail closed")
	}
	// A relayed request is never loopback-trusted, token or not.
	localtrust.Accept(tok)
	r := mk(http.MethodPost, "/v1/permissions/resolve", tok)
	r.Header.Set("X-Ghost-Via", "relay")
	if loopbackTrusted(r) {
		t.Error("a relayed request must not be trusted")
	}
}
