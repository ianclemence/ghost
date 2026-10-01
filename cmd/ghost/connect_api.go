package main

import (
	"net/http"
	"strings"
)

// registerConnectAPI adds the console's Ghost Connect endpoints. They manage the
// Pod's own door to the outside world, so they are for the owner at home (this
// machine or the home network with device credentials), never for a request that
// arrived through a relay.
func registerConnectAPI(mux *http.ServeMux) {
	home := func(next http.HandlerFunc) http.HandlerFunc {
		return authMiddleware(func(w http.ResponseWriter, r *http.Request) {
			if strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Ghost-Via")), "relay") {
				jsonError(w, http.StatusForbidden, "at_home_only", "Change this from home, not through the relay.")
				return
			}
			next(w, r)
		})
	}

	mux.HandleFunc("/v1/connect/status", home(func(w http.ResponseWriter, r *http.Request) {
		cfg, err := loadConfig()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "config_error", err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, connectSvc.status(cfg))
	}))

	mux.HandleFunc("/v1/connect/link", home(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		cfg, err := loadConfig()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "config_error", err.Error())
			return
		}
		at, err := connectSvc.startLink(cfg)
		if err != nil {
			jsonError(w, http.StatusBadGateway, "link_failed", err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, at)
	}))

	mux.HandleFunc("/v1/connect/unlink", home(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		cfg, err := loadConfig()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "config_error", err.Error())
			return
		}
		if err := connectSvc.unlink(cfg); err != nil {
			jsonError(w, http.StatusInternalServerError, "unlink_failed", err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]bool{"ok": true})
	}))

	mux.HandleFunc("/v1/connect/pair", home(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, `{"error":"method not allowed"}`, http.StatusMethodNotAllowed)
			return
		}
		cfg, err := loadConfig()
		if err != nil {
			jsonError(w, http.StatusInternalServerError, "config_error", err.Error())
			return
		}
		uri, expires, err := connectSvc.pairRemote(cfg, "Phone")
		if err != nil {
			jsonError(w, http.StatusBadGateway, "pair_failed", err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{"uri": uri, "expires_in": expires})
	}))
}
