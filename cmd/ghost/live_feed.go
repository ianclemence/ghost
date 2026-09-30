package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// ghostSaysMsg is something Ghost started itself (a reminder that came due, a
// notice, an alert) arriving while the terminal is open.
type ghostSaysMsg struct {
	text string
	kind string // reminder | notice | alert
	at   time.Time
}

// parseLiveFrame reads one frame from the daemon's live channel and returns
// what the terminal should show, if anything. It only surfaces messages Ghost
// initiated: replies to this terminal's own turns already stream over the
// request that made them, and showing them here too would print everything
// twice.
func parseLiveFrame(data []byte) (ghostSaysMsg, bool) {
	var f struct {
		Type     string                 `json:"type"`
		Content  string                 `json:"content"`
		Kind     string                 `json:"kind"`
		Metadata map[string]interface{} `json:"metadata"`
	}
	if json.Unmarshal(data, &f) != nil {
		return ghostSaysMsg{}, false
	}
	if f.Type != "assistant_message" || strings.TrimSpace(f.Content) == "" {
		return ghostSaysMsg{}, false
	}
	origin, _ := f.Metadata["origin"].(string)
	if f.Kind == "" && origin != "ghost" {
		return ghostSaysMsg{}, false
	}
	return ghostSaysMsg{text: strings.TrimSpace(f.Content), kind: f.Kind, at: time.Now()}, true
}

// startLiveFeed keeps a connection to the daemon so a reminder or an alert
// appears in an open terminal the moment it happens. Before this the terminal
// only learned of them at startup, so a reminder that fired while the window
// was open was missing until the next launch. It reconnects quietly with
// backoff and stops with ctx.
func (g *gatewayRuntime) startLiveFeed(ctx context.Context, deliver func(ghostSaysMsg)) {
	u, err := url.Parse(g.baseURL)
	if err != nil {
		return
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = "/v1/ws"
	go func() {
		backoff := time.Second
		for ctx.Err() == nil {
			hdr := http.Header{}
			// "cli" tells the daemon this is a terminal, not a phone, so it does
			// not count as a phone being connected and hold back push
			// notifications.
			hdr.Set("X-Client-Type", "cli")
			conn, _, err := websocket.DefaultDialer.DialContext(ctx, u.String(), hdr)
			if err == nil {
				backoff = time.Second
				conn.SetReadDeadline(time.Now().Add(70 * time.Second))
				conn.SetPongHandler(func(string) error {
					return conn.SetReadDeadline(time.Now().Add(70 * time.Second))
				})
				go func() { <-ctx.Done(); conn.Close() }()
				for {
					_, data, err := conn.ReadMessage()
					if err != nil {
						break
					}
					conn.SetReadDeadline(time.Now().Add(70 * time.Second))
					if msg, ok := parseLiveFrame(data); ok {
						deliver(msg)
					}
				}
				conn.Close()
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
		}
	}()
}
