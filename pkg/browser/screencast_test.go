package browser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMintConsumeRoundTrip(t *testing.T) {
	b := &StreamBroker{}
	sess := Session{ID: "sess-1", Owner: "owner", ContextID: "ctx", TaskID: "task"}
	tk, err := b.Mint(sess)
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if len(tk.Token) != 48 {
		t.Fatalf("token must be 48-char hex, got %q", tk.Token)
	}
	got, err := b.Consume(tk.Token)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if got.SessionID != sess.ID || got.Owner != sess.Owner {
		t.Fatalf("ticket binding lost: %+v", got)
	}
	// Single-use: second consume must fail.
	if _, err := b.Consume(tk.Token); err == nil {
		t.Fatal("reused token must fail")
	}
}

func TestConsumeExpiry(t *testing.T) {
	b := &StreamBroker{tickets: map[string]*Ticket{}}
	tk := &Ticket{Token: "abc", SessionID: "s", Owner: "o", ExpiresAt: time.Now().UTC().Add(-time.Second)}
	b.tickets["abc"] = tk
	if _, err := b.Consume("abc"); err == nil {
		t.Fatal("expired token must fail")
	}
}

func TestMintRequiresBinding(t *testing.T) {
	b := &StreamBroker{}
	if _, err := b.Mint(Session{}); err == nil {
		t.Fatal("mint without session+owner must fail")
	}
}

func TestStreamURLParsesStatus(t *testing.T) {
	b := &StreamBroker{
		run: func(ctx context.Context, args ...string) ([]byte, error) {
			return []byte("Streaming enabled on ws://127.0.0.1:33171\nConnected: true\n"), nil
		},
	}
	u, err := b.StreamURL(context.Background())
	if err != nil {
		t.Fatalf("stream url: %v", err)
	}
	if u != "ws://127.0.0.1:33171" {
		t.Fatalf("unexpected url %q", u)
	}
}

func TestStreamURLMissingCLI(t *testing.T) {
	b := &StreamBroker{
		run: func(ctx context.Context, args ...string) ([]byte, error) {
			return nil, errors.New("exec: \"agent-browser\": executable file not found in $PATH")
		},
	}
	_, err := b.StreamURL(context.Background())
	var unsup *ScreencastUnsupported
	if !errors.As(err, &unsup) {
		t.Fatalf("expected ScreencastUnsupported, got %v", err)
	}
	if !strings.Contains(unsup.Reason, "not installed") {
		t.Fatalf("unexpected reason %q", unsup.Reason)
	}
}
