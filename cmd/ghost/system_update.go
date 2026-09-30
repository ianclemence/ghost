package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ianclemence/ghost/pkg/appliance"
	"github.com/ianclemence/ghost/pkg/updaterun"
)

// A paired phone can check for, and start, an update of its own Pod.
//
//	GET  /v1/system/update            installed, available, notes, and the state of a running update
//	POST /v1/system/update {"confirm": true}   start it (detached: it outlives this process)
//
// Starting is refused unless the caller is an authenticated device (or the
// Pod itself), unless the owner explicitly confirms, and unless a newer
// release exists. It installs Ghost's own newer release and nothing else.

var (
	updateCacheMu  sync.Mutex
	updateCacheRel *appliance.Release
	updateCacheAt  time.Time
	updateStarted  time.Time
)

func latestRelease() (*appliance.Release, error) {
	updateCacheMu.Lock()
	defer updateCacheMu.Unlock()
	if updateCacheRel != nil && time.Since(updateCacheAt) < 10*time.Minute {
		return updateCacheRel, nil
	}
	rel, err := resolveRelease(offline())
	if err != nil {
		return nil, err
	}
	updateCacheRel, updateCacheAt = rel, time.Now()
	return rel, nil
}

// plainNotes trims release notes to something a phone can show.
func plainNotes(body string) string {
	body = strings.TrimSpace(strings.NewReplacer("**", "", "`", "", "\r", "").Replace(body))
	if r := []rune(body); len(r) > 900 {
		body = string(r[:900]) + "…"
	}
	return body
}

func registerSystemUpdateRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/system/update", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if principalDevice(r) == "" {
			jsonError(w, http.StatusForbidden, "forbidden", "updating needs an authenticated device")
			return
		}
		switch r.Method {
		case http.MethodGet:
			installed := ghostVersion()
			out := map[string]interface{}{"ok": true, "installed": installed}
			if rel, err := latestRelease(); err == nil {
				out["available"] = rel.Version
				out["newer"] = appliance.IsNewer(rel.Version, installed)
				out["notes"] = plainNotes(rel.Notes)
			} else {
				out["check_failed"] = "Couldn't check for updates right now."
			}
			p := updaterun.DetachedUpdateStatus()
			out["running"], out["success"] = p.Running, p.Success
			if p.Running || p.Success || strings.Contains(p.Log, "Update failed") {
				lines := strings.Split(strings.TrimSpace(p.Log), "\n")
				if len(lines) > 12 {
					lines = lines[len(lines)-12:]
				}
				out["log"] = strings.Join(lines, "\n")
			}
			jsonResponse(w, http.StatusOK, out)
		case http.MethodPost:
			var req struct {
				Confirm bool `json:"confirm"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || !req.Confirm {
				jsonError(w, http.StatusBadRequest, "confirm_required", "Updating restarts Ghost. Confirm to continue.")
				return
			}
			rel, err := latestRelease()
			if err != nil || !appliance.IsNewer(rel.Version, ghostVersion()) {
				jsonError(w, http.StatusConflict, "no_update", "Ghost is already up to date.")
				return
			}
			updateCacheMu.Lock()
			recent := time.Since(updateStarted) < 5*time.Minute
			updateCacheMu.Unlock()
			if recent {
				jsonError(w, http.StatusConflict, "already_started", "An update was just started. Give it a few minutes.")
				return
			}
			if err := updaterun.StartDetachedUpdate(); err != nil {
				jsonError(w, http.StatusConflict, "update_refused", err.Error())
				return
			}
			updateCacheMu.Lock()
			updateStarted = time.Now()
			updateCacheMu.Unlock()
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true, "message": "Update started. Ghost will restart when it finishes."})
		default:
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		}
	}))
}
