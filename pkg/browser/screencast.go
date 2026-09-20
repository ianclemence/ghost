// Package browser live screencast.
//
// The agent-browser CLI (a Node CDP wrapper) already owns frame production:
// `agent-browser stream enable` binds a localhost WebSocket server that emits
// JPEG frames on repaint (latest-first, ack-paced, per-viewer backpressure).
// Ghost never reimplements CDP: this broker only discovers that server and
// mints short-lived, single-use viewer tickets bound to a browser session,
// so the gateway can proxy exactly one viewer stream per ticket.
//
// Transport:
//
//	POST /v1/browser/screencast {session_id} -> {token, ws_path, expires_at}
//	GET  /v1/browser/screencast?token=... (WS upgrade, one-time consume)
//	Frames proxy byte-for-byte to the agent-browser server; idle = zero
//	bandwidth (server only sends on repaint). Fallback when streaming is
//	unavailable: 501 SCREENCAST_UNSUPPORTED, client uses GET observation
//	stills (ScreenshotPath PNGs) instead.
package browser

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// ScreencastUnsupported is returned when live streaming cannot serve: CLI
// missing, daemon down, or node-routed session. Callers must fall back to
// screenshot stills (GET /v1/live/surfaces/.../observation).
type ScreencastUnsupported struct{ Reason string }

func (e *ScreencastUnsupported) Error() string {
	return "screencast unsupported: " + e.Reason
}

// Ticket is one single-use viewer grant. Bound to the browser session that
// was current at mint; consumed on first WS open; expires after TTL.
type Ticket struct {
	Token     string
	SessionID string
	Owner     string
	ContextID string
	TaskID    string
	ExpiresAt time.Time
	consumed  bool
}

// StreamBroker discovers the agent-browser stream server and mints tickets.
// Zero value is usable; all methods are goroutine-safe.
type StreamBroker struct {
	mu      sync.Mutex
	tickets map[string]*Ticket
	stream  string // ws://127.0.0.1:port discovered via `stream status`
	checked time.Time
	run     func(ctx context.Context, args ...string) ([]byte, error)
}

// ticketTTL: 60s from mint to WS open.
const ticketTTL = 60 * time.Second

// streamCacheTTL bounds how long a discovered stream URL is reused.
const streamCacheTTL = 30 * time.Second

// NewStreamBroker returns a broker using the real CLI.
func NewStreamBroker() *StreamBroker {
	return &StreamBroker{
		tickets: map[string]*Ticket{},
		run: func(ctx context.Context, args ...string) ([]byte, error) {
			cmd := exec.CommandContext(ctx, "agent-browser", args...)
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			if err := cmd.Run(); err != nil {
				return nil, fmt.Errorf("agent-browser %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
			}
			return stdout.Bytes(), nil
		},
	}
}

// Mint creates a single-use viewer ticket for sess. Owner/context/task come
// from the caller (gate binding), never from tool args.
func (b *StreamBroker) Mint(sess Session) (Ticket, error) {
	if sess.ID == "" || sess.Owner == "" {
		return Ticket{}, fmt.Errorf("browser: cannot mint screencast without session + owner")
	}
	tok := make([]byte, 24)
	if _, err := rand.Read(tok); err != nil {
		return Ticket{}, fmt.Errorf("browser: ticket entropy unavailable: %v", err)
	}
	t := Ticket{
		Token:     hex.EncodeToString(tok),
		SessionID: sess.ID,
		Owner:     sess.Owner,
		ContextID: sess.ContextID,
		TaskID:    sess.TaskID,
		ExpiresAt: time.Now().UTC().Add(ticketTTL),
	}
	if b == nil {
		return t, nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.tickets == nil {
		b.tickets = map[string]*Ticket{}
	}
	b.tickets[t.Token] = &t
	b.sweepLocked()
	return t, nil
}

// Consume validates and single-use consumes a ticket. Expired or unknown
// tokens fail closed; a consumed token can never open a second viewer.
func (b *StreamBroker) Consume(token string) (Ticket, error) {
	if b == nil {
		return Ticket{}, fmt.Errorf("browser: screencast broker unavailable")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	t, ok := b.tickets[strings.TrimSpace(token)]
	if !ok || t == nil {
		return Ticket{}, fmt.Errorf("browser: invalid screencast token")
	}
	if t.consumed {
		delete(b.tickets, t.Token)
		return Ticket{}, fmt.Errorf("browser: screencast token already used")
	}
	if time.Now().UTC().After(t.ExpiresAt) {
		delete(b.tickets, t.Token)
		return Ticket{}, fmt.Errorf("browser: screencast token expired")
	}
	t.consumed = true
	delete(b.tickets, t.Token)
	return *t, nil
}

// StreamURL discovers (and enables, idempotently) the agent-browser stream
// server. Returns ws://127.0.0.1:port or *ScreencastUnsupported.
func (b *StreamBroker) StreamURL(ctx context.Context) (string, error) {
	if b != nil {
		b.mu.Lock()
		if b.stream != "" && time.Since(b.checked) < streamCacheTTL {
			u := b.stream
			b.mu.Unlock()
			return u, nil
		}
		b.mu.Unlock()
	}
	run := b.run
	if b == nil || run == nil {
		br := NewStreamBroker()
		run = br.run
	}
	// `stream status` reports `Streaming enabled on ws://...` when live.
	if out, err := run(ctx, "stream", "status"); err == nil {
		if u := parseStreamURL(string(out)); u != "" {
			if b != nil {
				b.mu.Lock()
				b.stream = u
				b.checked = time.Now()
				b.mu.Unlock()
			}
			return u, nil
		}
	} else if isCLIMissing(err) {
		return "", &ScreencastUnsupported{Reason: "agent-browser not installed"}
	}
	// Idempotent enable, then re-read status.
	if _, err := run(ctx, "stream", "enable"); err != nil {
		if isCLIMissing(err) {
			return "", &ScreencastUnsupported{Reason: "agent-browser not installed"}
		}
		return "", &ScreencastUnsupported{Reason: "stream enable failed"}
	}
	out, err := run(ctx, "stream", "status")
	if err != nil {
		return "", &ScreencastUnsupported{Reason: "stream unavailable"}
	}
	u := parseStreamURL(string(out))
	if u == "" {
		return "", &ScreencastUnsupported{Reason: "stream unavailable"}
	}
	if b != nil {
		b.mu.Lock()
		b.stream = u
		b.checked = time.Now()
		b.mu.Unlock()
	}
	return u, nil
}

func (b *StreamBroker) sweepLocked() {
	now := time.Now().UTC()
	for k, t := range b.tickets {
		if t == nil || t.consumed || now.After(t.ExpiresAt) {
			delete(b.tickets, k)
		}
	}
	if len(b.tickets) > 256 {
		// Bound memory: drop oldest expiries first (map order is random,
		// so this is approximate — TTL sweep above is the real control).
		for k := range b.tickets {
			delete(b.tickets, k)
			if len(b.tickets) <= 256 {
				break
			}
		}
	}
}

func parseStreamURL(out string) string {
	// Matches `ws://127.0.0.1:33171` anywhere in status output.
	for _, field := range strings.Fields(out) {
		f := strings.Trim(field, "(),")
		if strings.HasPrefix(f, "ws://127.0.0.1:") || strings.HasPrefix(f, "ws://localhost:") {
			return f
		}
	}
	// JSON form: {"url":"ws://..."} or {"port":33171}.
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &m); err == nil {
		if u, _ := m["url"].(string); strings.HasPrefix(u, "ws://") {
			return u
		}
		if p, ok := m["port"].(float64); ok && p > 0 {
			return fmt.Sprintf("ws://127.0.0.1:%d", int(p))
		}
	}
	return ""
}

func isCLIMissing(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "not found") || strings.Contains(s, "executable file not found")
}
