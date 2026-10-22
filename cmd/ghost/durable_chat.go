package main

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
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
	// The reply a model streams is written here while it streams, so a
	// process that dies mid-sentence leaves the words on disk instead of
	// losing them. It is a hook because the live hub is built before this
	// store is opened.
	checkpointFlush = func(session, requestID, text string, force bool) {
		_ = st.Checkpoint(session, requestID, text, force)
	}
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

// registerDurableTurns exposes the durable turn lookup and repairs any
// thread a restart cut off: replies are written back into the transcript,
// and turns still worth finishing are finished.
func registerDurableTurns(mux *http.ServeMux, loop *agent.AgentLoop) {
	st := initChatTurns()
	if st == nil {
		return
	}
	recoverInterruptedTurns(st, loop)
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

// A resend is not always a reconnect: the phone can post the same message
// twice with two different request ids (a double tap, a retry while the first
// answer is still coming). Request-id idempotency cannot see that, and the
// result was two pipelines running on one conversation — the duplicate boat
// prompt, two renders of the same film. The fingerprint below closes that gap.
const (
	duplicateWindow   = 3 * time.Minute
	minDuplicateChars = 24 // short repeats ("yes", "try again") are deliberate
)

type recentChatTurn struct {
	requestID string
	at        time.Time
}

var (
	recentChatMu sync.Mutex
	recentChat   = map[string]recentChatTurn{}
)

// chatContentFingerprint identifies "the same message" across request ids.
func chatContentFingerprint(session, channel, chatID, content string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		session, channel, chatID, strings.TrimSpace(content),
	}, "\x00")))
	return hex.EncodeToString(sum[:])
}

// noteChatRequest records a turn that is actually starting.
func noteChatRequest(key, requestID string) {
	if key == "" {
		return
	}
	now := time.Now()
	recentChatMu.Lock()
	defer recentChatMu.Unlock()
	if len(recentChat) > 256 {
		for k, v := range recentChat {
			if now.Sub(v.at) > duplicateWindow {
				delete(recentChat, k)
			}
		}
	}
	recentChat[key] = recentChatTurn{requestID: requestID, at: now}
}

// duplicateChatRequest returns the earlier request id when this content was
// already submitted for the same conversation inside the window.
func duplicateChatRequest(key, requestID string) (string, bool) {
	if key == "" {
		return "", false
	}
	recentChatMu.Lock()
	defer recentChatMu.Unlock()
	v, ok := recentChat[key]
	if !ok || v.requestID == requestID || time.Since(v.at) > duplicateWindow {
		return "", false
	}
	return v.requestID, true
}
