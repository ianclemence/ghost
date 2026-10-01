package main

import (
	"net/url"
	"strings"
	"testing"

	"github.com/ianclemence/ghost/pkg/config"
)

// The phone's remote-pairing link is a contract with the app (lib/pairing.ts):
// these are the fields it reads, and the relay address must be one it can call.
func TestRelayPairingURI(t *testing.T) {
	cfg := &config.Config{}
	cfg.Relay.Server = "wss://relay.example.com/"
	uri := relayPairingURI(cfg, "pod-1", strings.Repeat("b", 64), strings.Repeat("c", 64), strings.Repeat("A", 43))

	if !strings.HasPrefix(uri, "ghost://pair?") {
		t.Fatalf("unexpected scheme: %s", uri)
	}
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	want := map[string]string{
		"v": "1", "pod": "pod-1", "ghost": "pod-1", "transport": "relay",
		"relay":  "https://relay.example.com", // the app calls it over https, not wss
		"token":  strings.Repeat("b", 64),
		"client": strings.Repeat("c", 64),
		"pk":     strings.Repeat("A", 43),
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, q.Get(k), v)
		}
	}
}

func TestStatusWithoutASiteIsHonest(t *testing.T) {
	t.Setenv("GHOST_CONNECT_SITE", "")
	cfg := &config.Config{}
	cfg.Agents.Defaults.Workspace = t.TempDir()
	st := (&connectService{}).status(cfg)
	if st.Available || st.Linked {
		t.Fatalf("with no site configured Ghost Connect must say it isn't available: %+v", st)
	}
}
