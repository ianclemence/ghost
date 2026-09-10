package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/agent"
	"github.com/ianclemence/ghost/pkg/bus"
	"github.com/ianclemence/ghost/pkg/live"
)

// registerLiveSurfaceRoutes exposes the Live Surface plane to authenticated
// devices. Ghost decides; the client observes and, when authorized, takes
// over. Observation is read-only; control is an explicit, expiring lease
// that pauses Ghost (the gates enforce the pause).
//
// Routes (all behind device auth):
//
//	GET  /v1/live/surfaces                     list surfaces (?kind=browser|computer)
//	GET  /v1/live/surfaces/{kind}/{id}          surface state
//	GET  /v1/live/surfaces/{kind}/{id}/observation  latest safe observation
//	POST /v1/live/surfaces/{kind}/{id}/takeover  acquire a user control lease
//	POST /v1/live/surfaces/{kind}/{id}/release   release user control (no auto-resume)
//	POST /v1/live/surfaces/{kind}/{id}/resume    return a paused surface to Ghost
//	GET  /v1/live/surfaces/{kind}/{id}/stream    SSE of surface state changes
//
// Resume fails closed unless the surface is paused with no user in
// control; the agent gates revalidate ownership and permission on the
// next real operation.
func registerLiveSurfaceRoutes(mux *http.ServeMux, al *agent.AgentLoop) {
	plane := al.LivePlane()
	if plane == nil {
		plane = live.NewRegistry(ghostID())
		al.SetLivePlane(plane)
	}
	// The appliance computer always exists as a surface for discovery.
	plane.Register("local", live.KindComputer)
	reconcileLivePlaneOnce(plane)

	mux.HandleFunc("/v1/live/surfaces", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use GET")
			return
		}
		kind := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("kind")))
		var out []live.Surface
		for _, s := range plane.List() {
			if kind != "" && s.Kind != live.Kind(kind) {
				continue
			}
			out = append(out, s)
		}
		if out == nil {
			out = []live.Surface{}
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "surfaces": out})
	}))

	mux.HandleFunc("/v1/live/surfaces/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/live/surfaces/"), "/")
		segs := splitPath(rest)
		if len(segs) < 2 {
			jsonError(w, http.StatusBadRequest, "invalid_request", "expected /v1/live/surfaces/{kind}/{id}[/action]")
			return
		}
		kind := normalizeSurfaceKind(segs[0])
		if kind == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "kind must be browser or computer")
			return
		}
		id := segs[1]
		action := ""
		if len(segs) > 2 {
			action = segs[2]
		}
		// Authorize the surface by ensuring it belongs to this Ghost.
		if !surfaceExists(plane, kind, id) && action != "takeover" {
			// Takeover of a known-but-unregistered surface is allowed to
			// register it lazily below; other reads of an unknown surface
			// fail closed.
			jsonError(w, http.StatusNotFound, "surface_not_found", "that surface does not exist")
			return
		}

		switch action {
		case "":
			if r.Method != http.MethodGet {
				jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use GET")
				return
			}
			s, ok := plane.Snapshot(id)
			if !ok {
				jsonError(w, http.StatusNotFound, "surface_not_found", "that surface does not exist")
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "surface": s})
		case "observation":
			if r.Method != http.MethodGet {
				jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use GET")
				return
			}
			s, ok := plane.Snapshot(id)
			if !ok {
				jsonError(w, http.StatusNotFound, "surface_not_found", "that surface does not exist")
				return
			}
			obs := s.Obs
			if obs.Timestamp.IsZero() {
				jsonError(w, http.StatusNotFound, "no_observation", "no observation recorded yet")
				return
			}
			// Serve an internal screenshot when the executor captured one.
			resp := map[string]interface{}{"ok": true, "observation": obs}
			if obs.ScreenshotPath != "" {
				if data, mime, ok := readSurfaceImage(obs.ScreenshotPath); ok {
					resp["image_base64"] = base64.StdEncoding.EncodeToString(data)
					resp["mime_type"] = mime
				}
			}
			jsonResponse(w, http.StatusOK, resp)
		case "takeover":
			if r.Method != http.MethodPost {
				jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use POST")
				return
			}
			device := principalDevice(r)
			if device == "" {
				jsonError(w, http.StatusForbidden, "forbidden", "takeover requires an authenticated device")
				return
			}
			if kind == live.KindComputer {
				plane.Register("local", kind)
			}
			if !surfaceExists(plane, kind, id) {
				plane.Register(id, kind)
			}
			lease, err := plane.Takeover(id, device, 0)
			if err != nil {
				jsonError(w, http.StatusConflict, "control_conflict", err.Error())
				return
			}
			s, _ := plane.Snapshot(id)
			announceLiveSurface(al, kind, id)
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "lease": lease, "surface": s})
		case "release":
			if r.Method != http.MethodPost {
				jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use POST")
				return
			}
			device := principalDevice(r)
			isOwner := device == "owner" || isLoopbackRequest(r)
			if err := plane.Release(id, device, isOwner); err != nil {
				jsonError(w, http.StatusConflict, "control_conflict", err.Error())
				return
			}
			s, _ := plane.Snapshot(id)
			announceLiveSurface(al, kind, id)
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "surface": s})
		case "resume":
			if r.Method != http.MethodPost {
				jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use POST")
				return
			}
			if !surfaceExists(plane, kind, id) {
				jsonError(w, http.StatusNotFound, "surface_not_found", "that surface does not exist")
				return
			}
			if err := plane.Resume(id); err != nil {
				jsonError(w, http.StatusConflict, "resume_refused", err.Error())
				return
			}
			s, _ := plane.Snapshot(id)
			announceLiveSurface(al, kind, id)
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "surface": s})
		case "stream":
			if r.Method != http.MethodGet {
				jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use GET")
				return
			}
			streamSurface(w, r, plane, id)
		default:
			jsonError(w, http.StatusNotFound, "not_found", "unknown surface action")
		}
	}))
}

// announceLiveSurface tells owner devices a surface changed outside the
// turn flow (takeover, release, resume). Identity only; clients fetch
// authoritative state. Session linkage is unavailable here, so no
// session_id is attached — watching clients learn it from the stream.
func announceLiveSurface(al *agent.AgentLoop, kind live.Kind, id string) {
	if al == nil || al.Bus() == nil {
		return
	}
	al.Bus().PublishOutbound(bus.OutboundMessage{
		Channel: "mobile",
		Content: "",
		Metadata: map[string]interface{}{
			"type":       "surface_update",
			"surface_id": id,
			"kind":       string(kind),
		},
	})
}

var liveReconcileOnce sync.Once

// reconcileLivePlaneOnce expires dead user leases on a ticker so a dead
// mobile connection can never leave permanent human control.
func reconcileLivePlaneOnce(plane *live.Registry) {
	liveReconcileOnce.Do(func() {
		ticker := time.NewTicker(time.Minute)
		go func() {
			for range ticker.C {
				plane.Reconcile(time.Now())
			}
		}()
	})
}

func splitPath(s string) []string {
	parts := strings.Split(s, "/")
	out := parts[:0]
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func normalizeSurfaceKind(k string) live.Kind {
	switch strings.ToLower(k) {
	case "browser":
		return live.KindBrowser
	case "computer":
		return live.KindComputer
	}
	return ""
}

func surfaceExists(plane *live.Registry, kind live.Kind, id string) bool {
	if kind == live.KindComputer && id == "local" {
		return true
	}
	_, ok := plane.Snapshot(id)
	return ok
}

// principalDevice resolves the authenticated device identity for a control
// request. Loopback/console callers act as the owner; LAN devices use the
// pairing device id already validated by authMiddleware.
func principalDevice(r *http.Request) string {
	if isLoopbackRequest(r) {
		return "owner"
	}
	return strings.TrimSpace(r.Header.Get("X-Ghost-Device-ID"))
}

// streamSurface emits SSE frames whenever the surface's monotonic sequence
// changes (polled against the plane; no second event bus). Read-only.
func streamSurface(w http.ResponseWriter, r *http.Request, plane *live.Registry, id string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	emit := func(v interface{}) {
		raw, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", string(raw))
		flusher.Flush()
	}
	last := int64(-1)
	for {
		select {
		case <-r.Context().Done():
			return
		default:
		}
		s, ok := plane.Snapshot(id)
		if !ok {
			emit(map[string]interface{}{"type": "surface_closed", "id": id})
			return
		}
		if s.Sequence != last {
			last = s.Sequence
			emit(map[string]interface{}{"type": "surface", "id": id, "surface": s})
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// readSurfaceImage reads a bounded PNG screenshot captured by an executor.
// The path is server-controlled (never client-supplied).
func readSurfaceImage(path string) ([]byte, string, bool) {
	if !strings.HasSuffix(strings.ToLower(path), ".png") {
		return nil, "", false
	}
	fi, err := os.Stat(path)
	if err != nil || fi.Size() <= 0 || fi.Size() > 8<<20 {
		return nil, "", false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", false
	}
	return data, "image/png", true
}
