package main

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
)

// registerConnectionsRoutes moves safe connect/disconnect onto the
// device-facing API (previously console-only). Secrets are written into the
// existing .secrets.json credential boundary and are NEVER returned to the
// client. OAuth-only providers are refused on this surface (no raw OAuth
// tokens, no credential proxy); their handoff stays console/browser-side.
func registerConnectionsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/connections/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/connections/"), "/")
		if rest == "" {
			jsonError(w, http.StatusNotFound, "not_found", "unknown connection endpoint")
			return
		}
		parts := strings.Split(rest, "/")
		id := parts[0]

		switch {
		case len(parts) == 2 && parts[1] == "disconnect":
			if r.Method != http.MethodPost {
				jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use POST")
				return
			}
			if err := connectionDisconnect(id); err != nil {
				jsonError(w, http.StatusBadRequest, "disconnect_failed", err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{
				"ok": true,
				"connection": map[string]interface{}{
					"id": id, "status": "disconnected",
				},
			})
		default:
			// Connect: accept a secret once (never echo it). OAuth providers
			// have no exportable secret and are refused here.
			if len(parts) != 1 || parts[0] == "" || parts[0] == "connect" {
				jsonError(w, http.StatusNotFound, "not_found", "use POST /v1/connections/{id} with {\"value\": \"...\"}")
				return
			}
			if r.Method != http.MethodPost {
				jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use POST")
				return
			}
			var body struct {
				Value string `json:"value"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Value) == "" {
				jsonError(w, http.StatusBadRequest, "invalid_request", "value is required")
				return
			}
			if err := connectionStore(id, body.Value); err != nil {
				jsonError(w, http.StatusBadRequest, "connect_failed", err.Error())
				return
			}
			jsonResponse(w, http.StatusOK, map[string]interface{}{
				"ok": true,
				"connection": map[string]interface{}{
					"id": id, "status": "connected",
				},
			})
		}
	}))
}

// secretsPathFor resolves the .secrets.json used by this gateway.
func secretsPathFor() string {
	return config.SecretsPath(getConfigPath())
}

func connectionStore(id, value string) error {
	if id == "google-calendar" {
		return errOAuthOnly(id)
	}
	s, err := config.LoadSecrets(secretsPathFor())
	if err != nil {
		return err
	}
	if s.ProviderAPIKeys == nil {
		s.ProviderAPIKeys = map[string]string{}
	}
	s.ProviderAPIKeys[id] = strings.TrimSpace(value)
	return config.SaveSecrets(secretsPathFor(), s)
}

func connectionDisconnect(id string) error {
	s, err := config.LoadSecrets(secretsPathFor())
	if err != nil {
		return err
	}
	if _, ok := s.ProviderAPIKeys[id]; !ok && id != "google-calendar" {
		return errNotConnected(id)
	}
	delete(s.ProviderAPIKeys, id)
	return config.SaveSecrets(secretsPathFor(), s)
}

func errOAuthOnly(id string) error {
	return &connError{"oauth_required: " + id + " uses secure browser sign-in, not a shared secret"}
}

func errNotConnected(id string) error {
	return &connError{"not_connected: " + id}
}

type connError struct{ msg string }

func (e *connError) Error() string { return e.msg }
