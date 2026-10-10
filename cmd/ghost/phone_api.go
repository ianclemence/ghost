package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/life"
)

// registerPhoneRoutes is how the owner's phone shares what they switched on
// (notifications from the apps they chose, daily health totals) and watches
// the places Ghost was asked to remind them at.
//
//	POST   /v1/phone/signals              {notes: [...], health: [...]}
//	DELETE /v1/phone/notes                forget every shared notification
//	GET    /v1/phone/places               the places to watch
//	POST   /v1/phone/places/{id}/crossed  {event: enter|exit}
func registerPhoneRoutes(mux *http.ServeMux, al *agent.AgentLoop) {
	store := func() *life.Device { return life.DeviceFor(apiWorkspaceDir) }

	mux.HandleFunc("/v1/phone/signals", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST")
			return
		}
		var req struct {
			Notes   []life.PhoneNote `json:"notes"`
			Health  []life.HealthDay `json:"health"`
			Sharing *struct {
				Notifications bool     `json:"notifications"`
				Apps          []string `json:"apps"`
			} `json:"sharing"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 512*1024)).Decode(&req); err != nil {
			jsonError(w, http.StatusBadRequest, "invalid_request", "invalid json body")
			return
		}
		if len(req.Notes) > 300 || len(req.Health) > 120 {
			jsonError(w, http.StatusBadRequest, "invalid_request", "too much at once")
			return
		}
		if req.Sharing != nil {
			if err := store().SetSharing(req.Sharing.Notifications, req.Sharing.Apps, time.Now()); err != nil {
				jsonError(w, http.StatusInternalServerError, "failed", err.Error())
				return
			}
		}
		notes, err := store().AddNotes(req.Notes, time.Now())
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "failed", err.Error())
			return
		}
		days, err := store().AddHealth(req.Health)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "failed", err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "notes": notes, "health_days": days})
	}))

	mux.HandleFunc("/v1/phone/notes", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use DELETE")
			return
		}
		if err := store().ForgetNotes(); err != nil {
			jsonError(w, http.StatusInternalServerError, "failed", err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})
	}))

	mux.HandleFunc("/v1/phone/places", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET")
			return
		}
		list, err := store().Places(true)
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "failed", err.Error())
			return
		}
		if list == nil {
			list = []life.Place{}
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "places": list})
	}))

	mux.HandleFunc("/v1/phone/places/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/phone/places/"), "/"), "/")
		if r.Method != http.MethodPost || len(parts) != 2 || parts[1] != "crossed" {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use POST /v1/phone/places/{id}/crossed")
			return
		}
		var req struct {
			Event string `json:"event"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1024)).Decode(&req); err != nil || (req.Event != "enter" && req.Event != "exit") {
			jsonError(w, http.StatusBadRequest, "invalid_request", "event is enter or exit")
			return
		}
		p, fire, err := store().Arrived(parts[0], req.Event, time.Now())
		if err != nil {
			jsonError(w, http.StatusNotFound, "not_found", "no such place")
			return
		}
		if fire && al != nil {
			// The phone has already shown it as a notification; this keeps it
			// in the conversation where everything else Ghost says is.
			al.DeliverToOwner("mobile", "default", p.Message, map[string]interface{}{"announce": "place", "reminder": true})
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "fired": fire, "place": p})
	}))
}
