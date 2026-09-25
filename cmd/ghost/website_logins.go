package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ianclemence/ghost/pkg/credentials"
)

// registerWebsiteLoginRoutes exposes owner-facing website logins: the Apps
// screen saves a sign-in once, the sealed vault holds it, and the browser
// fills it in. Secrets travel in and never come back out — the list response
// carries only host, URL, and a masked username.
func registerWebsiteLoginRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/website-logins", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			jsonResponse(w, http.StatusOK, map[string]interface{}{
				"ok":     true,
				"logins": credentials.ListWebLogins(),
			})

		case http.MethodPost:
			var req struct {
				Host             string `json:"host"`
				URL              string `json:"url"`
				Username         string `json:"username"`
				Password         string `json:"password"`
				UsernameSelector string `json:"username_selector"`
				PasswordSelector string `json:"password_selector"`
				SubmitSelector   string `json:"submit_selector"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				jsonError(w, http.StatusBadRequest, "invalid_request", "invalid request")
				return
			}
			err := credentials.SaveWebLogin(credentials.WebLogin{
				Host:             strings.TrimSpace(req.Host),
				URL:              strings.TrimSpace(req.URL),
				Username:         strings.TrimSpace(req.Username),
				Password:         req.Password,
				UsernameSelector: strings.TrimSpace(req.UsernameSelector),
				PasswordSelector: strings.TrimSpace(req.PasswordSelector),
				SubmitSelector:   strings.TrimSpace(req.SubmitSelector),
			})
			if err != nil {
				// Validation failures are owner-fixable, not server errors.
				jsonError(w, http.StatusBadRequest, "invalid_request", err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})

		case http.MethodDelete:
			host := strings.TrimSpace(r.URL.Query().Get("host"))
			if host == "" {
				jsonError(w, http.StatusBadRequest, "invalid_request", "host is required")
				return
			}
			if err := credentials.DeleteWebLogin(host); err != nil {
				jsonError(w, http.StatusBadRequest, "delete_failed", "couldn't remove that login")
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{"ok": true})

		default:
			jsonError(w, http.StatusMethodNotAllowed, "method_not_allowed", "use GET, POST, or DELETE")
		}
	}))
}
