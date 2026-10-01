package connect

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStateRoundTripAndPermissions(t *testing.T) {
	ws := t.TempDir()
	if s, err := LoadState(ws); err != nil || s.Linked() {
		t.Fatalf("a Pod that was never linked has no state: %+v %v", s, err)
	}
	want := &State{Site: "https://x", Relay: "wss://r", Token: "ge1.a.b", ExpiresAt: 99}
	if err := SaveState(ws, want); err != nil {
		t.Fatal(err)
	}
	got, _ := LoadState(ws)
	if *got != *want {
		t.Fatalf("got %+v", got)
	}
	info, _ := os.Stat(filepath.Join(ws, "state", "connect.json"))
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state must be owner-only, got %o", info.Mode().Perm())
	}
	if err := ClearState(ws); err != nil {
		t.Fatal(err)
	}
	if s, _ := LoadState(ws); s.Linked() {
		t.Fatal("cleared state must be empty")
	}
	if err := ClearState(ws); err != nil {
		t.Fatal("clearing twice is fine")
	}
}

func TestNeedsRenewal(t *testing.T) {
	now := time.Now()
	s := &State{Token: "t", ExpiresAt: now.Add(72 * time.Hour).Unix()}
	if s.NeedsRenewal(now) {
		t.Fatal("a fresh pass should not be renewed yet")
	}
	s.ExpiresAt = now.Add(40 * time.Hour).Unix()
	if !s.NeedsRenewal(now) {
		t.Fatal("under 48h left should renew")
	}
	if (&State{}).NeedsRenewal(now) {
		t.Fatal("an unlinked Pod has nothing to renew")
	}
}

func fakeSite(t *testing.T) *httptest.Server {
	polls := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/connect/start", func(w http.ResponseWriter, r *http.Request) {
		var in map[string]string
		json.NewDecoder(r.Body).Decode(&in)
		if in["pod_id"] != "pod-1" {
			http.Error(w, "no", 400)
			return
		}
		json.NewEncoder(w).Encode(StartResult{UserCode: "GHOST-ABCD", VerifyURL: "https://x/connect?code=GHOST-ABCD", PollToken: "poll", ExpiresIn: 600})
	})
	mux.HandleFunc("/api/connect/poll", func(w http.ResponseWriter, r *http.Request) {
		polls++
		switch {
		case polls == 1:
			w.WriteHeader(http.StatusAccepted)
			w.Write([]byte(`{"status":"pending"}`))
		case polls == 2:
			w.WriteHeader(http.StatusPaymentRequired)
			w.Write([]byte(`{"status":"needs_subscription"}`))
		case polls == 3:
			w.WriteHeader(http.StatusGone)
		default:
			json.NewEncoder(w).Encode(map[string]any{"status": "linked", "token": "ge1.t.t", "expires_at": 123, "relay": "wss://relay.x"})
		}
	})
	mux.HandleFunc("/api/connect/renew", func(w http.ResponseWriter, r *http.Request) {
		switch r.Header.Get("Authorization") {
		case "Bearer good":
			json.NewEncoder(w).Encode(Pass{Token: "ge1.new.new", ExpiresAt: 456, Relay: "wss://relay.x"})
		case "Bearer lapsed":
			w.WriteHeader(http.StatusPaymentRequired)
		default:
			w.WriteHeader(http.StatusUnauthorized)
		}
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return ts
}

func TestLinkFlow(t *testing.T) {
	c := NewClient(fakeSite(t).URL + "/")
	ctx := context.Background()
	st, err := c.Start(ctx, "pod-1", "Kitchen")
	if err != nil || st.UserCode != "GHOST-ABCD" || st.Interval != 3 {
		t.Fatalf("start: %+v %v", st, err)
	}
	for i, want := range []error{ErrPending, ErrNoSubscription, ErrExpired} {
		if _, err := c.Poll(ctx, st.PollToken); err != want {
			t.Fatalf("poll %d: got %v, want %v", i, err, want)
		}
	}
	pass, err := c.Poll(ctx, st.PollToken)
	if err != nil || pass.Token != "ge1.t.t" || pass.Relay != "wss://relay.x" || pass.ExpiresAt != 123 {
		t.Fatalf("linked: %+v %v", pass, err)
	}
}

func TestRenew(t *testing.T) {
	c := NewClient(fakeSite(t).URL)
	ctx := context.Background()
	if p, err := c.Renew(ctx, "good"); err != nil || p.Token != "ge1.new.new" {
		t.Fatalf("renew: %+v %v", p, err)
	}
	if _, err := c.Renew(ctx, "lapsed"); err != ErrNoSubscription {
		t.Fatalf("lapsed: %v", err)
	}
	if _, err := c.Renew(ctx, "forged"); err != ErrRejected {
		t.Fatalf("forged: %v", err)
	}
}

func TestUnreachableSiteSaysSo(t *testing.T) {
	c := NewClient("http://127.0.0.1:1")
	if _, err := c.Start(context.Background(), "pod-1", ""); err == nil {
		t.Fatal("expected an error")
	}
}

func TestHTTPBase(t *testing.T) {
	for in, want := range map[string]string{
		"wss://relay.x/": "https://relay.x", "ws://127.0.0.1:8080": "http://127.0.0.1:8080", "https://r.x": "https://r.x",
	} {
		if got := HTTPBase(in); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
}
