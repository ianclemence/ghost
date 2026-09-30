package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// The URI the terminal QR encodes must be exactly what the app parses: same
// scheme, version and fields as the web console's QR.
func TestPairingURIMatchesApp(t *testing.T) {
	u := pairingURI(pairInvitation{PodID: "pod1", Transport: "lan", Host: "192.168.1.9", Port: "8766", Token: "abc123"})
	if !strings.HasPrefix(u, "ghost://pair?") {
		t.Fatalf("scheme: %s", u)
	}
	parsed, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	q := parsed.Query()
	for k, want := range map[string]string{"v": "1", "pod": "pod1", "transport": "lan", "host": "192.168.1.9", "port": "8766", "token": "abc123"} {
		if q.Get(k) != want {
			t.Errorf("%s = %q, want %q", k, q.Get(k), want)
		}
	}
}

// requestInvitation talks to the daemon's loopback endpoint and refuses
// anything that isn't a usable invitation.
func TestRequestInvitation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/pairing/invitations" || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["display_name"] != "My Phone" {
			t.Errorf("display name not sent: %v", body)
		}
		_ = json.NewEncoder(w).Encode(pairInvitation{PodID: "p", Transport: "lan", Host: "h", Port: "1", Token: "tok", ExpiresIn: 300})
	}))
	defer srv.Close()
	inv, err := requestInvitation(srv.URL, "My Phone")
	if err != nil || inv.Token != "tok" || inv.ExpiresIn != 300 {
		t.Fatalf("got %+v, %v", inv, err)
	}
	if _, err := requestInvitation("http://127.0.0.1:1", "x"); err == nil {
		t.Fatal("an unreachable daemon must be an error")
	}
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{}`)) }))
	defer empty.Close()
	if _, err := requestInvitation(empty.URL, "x"); err == nil {
		t.Fatal("an invitation without a token must be rejected")
	}
}
