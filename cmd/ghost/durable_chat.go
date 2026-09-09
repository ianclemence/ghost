package main

import (
	"net/http"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/turnlog"
)

// chatTurns is the durable turn log for /v1/chat idempotency. Reconnect is
// observation/resumption, never re-submission: an identical request_id for
// the same conversation can never start a second execution.
var chatTurns *turnlog.Store

func initChatTurns() *turnlog.Store {
	if chatTurns != nil {
		return chatTurns
	}
	st, err := turnlog.New(joinWorkspace("state", "turns"))
	if err != nil {
		return nil
	}
	_, _ = st.Recover(deviceProcessStart)
	chatTurns = st
	return st
}

// joinWorkspace builds a path under the api workspace dir.
func joinWorkspace(parts ...string) string {
	p := apiWorkspaceDir
	for _, part := range parts {
		p += "/" + part
	}
	return p
}

// registerDurableTurns exposes the durable turn lookup and wires recovery.
func registerDurableTurns(mux *http.ServeMux) {
	st := initChatTurns()
	if st == nil {
		return
	}
	mux.HandleFunc("/v1/chat/turn", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use GET")
			return
		}
		session := strings.TrimSpace(r.URL.Query().Get("session"))
		reqID := strings.TrimSpace(r.URL.Query().Get("request_id"))
		if session == "" || reqID == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "session and request_id required")
			return
		}
		t, err := st.Get(session, reqID)
		if err != nil || t == nil {
			jsonError(w, http.StatusNotFound, "turn_not_found", "turn not found")
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "turn": t})
	}))
}

var _ = time.Now
