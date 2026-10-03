package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/cevents"
	"github.com/ianclemence/ghost/pkg/push"
)

// wsClients counts phones currently holding the live connection. A phone that
// is connected already hears everything; push is for when nothing is listening.
var wsClients atomic.Int64

// pushCategoryFor decides whether an outbound message should nudge a phone that
// isn't connected, and how. The conversation itself is untouched: this only
// decides whether to tap the owner on the shoulder.
func pushCategoryFor(msg bus.OutboundMessage) (push.Category, bool) {
	switch t, _ := msg.Metadata["type"].(string); t {
	case "clarify_request":
		return push.Question, true
	case "background_done":
		return push.Update, true
	case "assistant_message", "":
		if reminder, _ := msg.Metadata["reminder"].(bool); reminder && msg.Content != "" {
			return push.Reminder, true
		}
		if _, routine := msg.Metadata["routine"]; routine && msg.Content != "" {
			return push.Update, true
		}
		if _, announced := msg.Metadata["announce"]; announced && msg.Content != "" {
			if urgent, _ := msg.Metadata["urgent"].(bool); urgent {
				return push.Alert, true
			}
			return push.Update, true
		}
		if conversationEvent(msg) && msg.Content != "" {
			return push.Update, true
		}
	case "card_update":
		if msg.Content != "" {
			return push.Update, true
		}
	}
	return "", false
}

// startPushBridge wires Ghost's own events to the phone's push token. It is a
// no-op until a device registers a token.
func startPushBridge(al *agent.AgentLoop) *push.Store {
	store, err := push.NewStore(al.DB())
	if err != nil {
		log.Printf("push: unavailable: %v", err)
		return nil
	}
	notifier := push.NewNotifier(store)
	send := func(cat push.Category, text string) {
		if wsClients.Load() > 0 {
			return // a connected phone already has it
		}
		if cat == push.Update && al.ProactiveQuiet(time.Now()) {
			return // quiet hours hold a plain update; approvals and questions still go
		}
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		if _, err := notifier.NotifyWith(ctx, cat, text); err != nil {
			log.Printf("push: %v", err)
		}
	}

	ch, _ := al.Bus().SubscribeOutbound("push-bridge", false, 300)
	go func() {
		for msg := range ch {
			if cat, ok := pushCategoryFor(msg); ok {
				send(cat, msg.Content)
			}
		}
	}()
	if events := al.CanonicalEvents(); events != nil {
		events.Subscribe(cevents.Filter{Types: []cevents.Type{cevents.PermissionRequested}}, func(*cevents.Event) {
			go send(push.Approval, "")
		})
	}
	return store
}

// registerPushRoutes lets a paired phone hand over (or withdraw) its push token.
//
//	POST   /v1/push/register   {"token": "...", "platform": "ios|android"}
//	DELETE /v1/push/register   forget this device's token
//	GET    /v1/push/status     {"registered": n, "live_connections": n}
func registerPushRoutes(mux *http.ServeMux, store *push.Store) {
	if store == nil {
		return
	}
	mux.HandleFunc("/v1/push/register", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		device := principalDevice(r)
		if device == "" {
			jsonError(w, http.StatusForbidden, "forbidden", "push needs an authenticated device")
			return
		}
		switch r.Method {
		case http.MethodPost:
			var req struct {
				Token    string `json:"token"`
				Platform string `json:"platform"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "invalid request")
				return
			}
			if err := store.Register(device, req.Token, req.Platform); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
		case http.MethodDelete:
			_ = store.Remove(device)
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
		default:
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	}))
	mux.HandleFunc("/v1/push/status", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "registered": len(store.Tokens()), "live_connections": wsClients.Load()})
	}))
}
