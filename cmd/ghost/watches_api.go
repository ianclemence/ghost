package main

import (
	"net/http"
	"strings"

	"github.com/ianclemence/ghost/pkg/watch"
)

// The watch surface: what Ghost is quietly polling in the background, and
// the owner's ability to stop one. Stopping a watch is an owner decision,
// so it is explicit here rather than something a suggestion could do — the
// endpoint only ever removes Ghost's attention, never anything in the
// world.
func registerWatchRoutes(mux *http.ServeMux, apiWorkspaceDir string, agentLoop interface {
	Watches() ([]watch.Watch, error)
	CancelWatch(id, note string) (watch.Watch, error)
}, authMiddleware func(http.HandlerFunc) http.HandlerFunc) {
	mux.HandleFunc("/v1/watches", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		if agentLoop == nil || apiWorkspaceDir == "" {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "watches are unavailable right now")
			return
		}
		list, err := agentLoop.Watches()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "unavailable", "watches are unavailable right now")
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "watches": list})
	}))

	mux.HandleFunc("/v1/watches/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		if agentLoop == nil || apiWorkspaceDir == "" {
			jsonError(w, http.StatusServiceUnavailable, "unavailable", "watches are unavailable right now")
			return
		}
		rest := strings.TrimPrefix(r.URL.Path, "/v1/watches/")
		parts := strings.Split(strings.Trim(rest, "/"), "/")
		if len(parts) != 2 || parts[1] != "disable" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "use /v1/watches/{id}/disable")
			return
		}
		updated, err := agentLoop.CancelWatch(parts[0], "the owner stopped this watch")
		if err != nil {
			jsonError(w, http.StatusBadRequest, "disable_failed", err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "watch": updated})
	}))
}
