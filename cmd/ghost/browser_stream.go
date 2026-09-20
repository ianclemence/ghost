package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/browser"
)

// registerBrowserStreamRoutes exposes live browser automation viewing
// (Watch browser automation live).
//
//	POST /v1/browser/screencast {session_id} -> {token, ws_path, expires_at}
//	GET  /v1/browser/screencast?token=... (WS upgrade, single-use ticket)
//
// Frames proxy byte-for-byte to the agent-browser stream server
// (ws://127.0.0.1:<port>, JPEG on repaint, latest-first, ack-paced). Idle
// pages cost zero bandwidth. When streaming is unavailable the mint fails
// with 501 SCREENCAST_UNSUPPORTED and clients fall back to observation
// stills (GET /v1/live/surfaces/browser/{id}/observation).
//
// Security: mint sits behind device auth; the ticket is the WS auth (mobile
// WebSockets cannot send headers). Tickets are 48-char hex, 60s TTL,
// single-use, bound to the ledger row's owner/context/task at mint.
func registerBrowserStreamRoutes(mux *http.ServeMux, al *agent.AgentLoop) {
	mux.HandleFunc("/v1/browser/screencast", func(w http.ResponseWriter, r *http.Request) {
		// WS upgrade path carries ?token= and must NOT require device
		// headers (React Native cannot set them); the ticket is the auth.
		if r.Method == http.MethodGet && strings.TrimSpace(r.URL.Query().Get("token")) != "" {
			serveBrowserStreamWS(w, r, al)
			return
		}
		authMiddleware(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use POST {session_id}")
				return
			}
			var body struct {
				SessionID string `json:"session_id"`
			}
			_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body)
			sessionID := strings.TrimSpace(body.SessionID)
			if sessionID == "" {
				sessionID = strings.TrimSpace(r.URL.Query().Get("session_id"))
			}
			if sessionID == "" {
				jsonError(w, http.StatusBadRequest, "invalid_request", "session_id is required")
				return
			}
			if al == nil {
				jsonError(w, http.StatusServiceUnavailable, "unavailable", "agent loop not ready")
				return
			}
			tk, err := al.MintBrowserScreencast(sessionID)
			if err != nil {
				if strings.Contains(err.Error(), "not found") {
					jsonError(w, http.StatusNotFound, "surface_not_found", "that browser session does not exist")
					return
				}
				jsonError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			// Verify the stream server is reachable now so clients get a
			// fast 501 (fallback to stills) instead of a dead ticket.
			ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
			defer cancel()
			upstream, err := al.BrowserStreamURL(ctx)
			if err != nil {
				var unsup *browser.ScreencastUnsupported
				if errors.As(err, &unsup) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNotImplemented)
					_ = json.NewEncoder(w).Encode(map[string]interface{}{
						"ok": false, "error": map[string]interface{}{
							"code": "SCREENCAST_UNSUPPORTED", "reason": unsup.Reason,
							"fallback": "/v1/live/surfaces/browser/" + sessionID + "/observation",
						},
					})
					return
				}
				jsonError(w, http.StatusBadGateway, "stream_unavailable", err.Error())
				return
			}
			_ = upstream
			jsonResponse(w, http.StatusOK, map[string]interface{}{
				"ok": true, "token": tk.Token, "ws_path": "/v1/browser/screencast?token=" + tk.Token,
				"expires_at": tk.ExpiresAt.UTC().Format(time.RFC3339), "session_id": tk.SessionID,
			})
		})(w, r)
	})
}

func serveBrowserStreamWS(w http.ResponseWriter, r *http.Request, al *agent.AgentLoop) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if al == nil || token == "" {
		http.Error(w, "invalid screencast token", http.StatusBadRequest)
		return
	}
	tk, err := al.ConsumeBrowserScreencast(token)
	if err != nil {
		// Close code 4001 = token invalid/expired/used.
		http.Error(w, "invalid screencast token", http.StatusUnauthorized)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	upstreamBase, err := al.BrowserStreamURL(ctx)
	cancel()
	if err != nil {
		http.Error(w, "screencast unsupported", http.StatusNotImplemented)
		return
	}
	_ = tk
	upstream := upstreamBase + passthroughStreamQuery(r.URL)
	proxyBrowserStream(w, r, upstream)
}

// passthroughStreamQuery forwards only the agent-browser pacing knobs
// (?pacing=ack&maxFps=N); everything else is dropped.
func passthroughStreamQuery(u *url.URL) string {
	q := u.Query()
	var out url.Values = url.Values{}
	if p := strings.ToLower(strings.TrimSpace(q.Get("pacing"))); p == "ack" || p == "push" {
		out.Set("pacing", p)
	}
	if f := strings.TrimSpace(q.Get("maxFps")); f != "" {
		// Clamp 0-120 (0 = uncapped); non-numeric dropped.
		n := 0
		for _, c := range f {
			if c < '0' || c > '9' {
				n = -1
				break
			}
			n = n*10 + int(c-'0')
			if n > 120 {
				break
			}
		}
		if n >= 0 && n <= 120 {
			out.Set("maxFps", strings.TrimSpace(q.Get("maxFps")))
		}
	}
	if len(out) == 0 {
		return ""
	}
	return "?" + out.Encode()
}

// proxyBrowserStream bridges one viewer to the agent-browser stream server.
// Both directions copy raw WS messages; the upstream server already applies
// latest-first + ack pacing so a stalled viewer never builds a backlog here.
func proxyBrowserStream(w http.ResponseWriter, r *http.Request, upstream string) {
	down, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer down.Close()
	dialer := websocket.Dialer{HandshakeTimeout: 8 * time.Second}
	up, _, err := dialer.Dial(upstream, nil)
	if err != nil {
		_ = down.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(4004, "target closed"), time.Now().Add(2*time.Second))
		return
	}
	defer up.Close()
	done := make(chan struct{}, 2)
	go func() { // viewer -> browser (config/ack/input)
		defer func() { done <- struct{}{} }()
		for {
			mt, msg, err := down.ReadMessage()
			if err != nil {
				return
			}
			_ = up.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := up.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}()
	go func() { // browser -> viewer (frames/meta)
		defer func() { done <- struct{}{} }()
		for {
			mt, msg, err := up.ReadMessage()
			if err != nil {
				return
			}
			_ = down.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := down.WriteMessage(mt, msg); err != nil {
				return
			}
		}
	}()
	select {
	case <-done:
	case <-r.Context().Done():
	}
	_ = down.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
}
