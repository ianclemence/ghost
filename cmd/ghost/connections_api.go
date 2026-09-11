package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/credentials"
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

// connectionVault returns the single credential boundary for this gateway.
// All connection writes and disconnects go through it; nothing writes the
// secrets file directly.
func connectionVault() *credentials.Vault {
	return credentials.New(filepath.Dir(secretsPathFor()))
}

func connectionStore(id, value string) error {
	if id == "google-calendar" {
		return errOAuthOnly(id)
	}
	return connectionVault().Store(id, value)
}

func connectionDisconnect(id string) error {
	// Google Calendar is an OAuth connected app: the vault removes the
	// actual credential (both calendar stacks), not just a config key.
	if id == "google-calendar" {
		return connectionVault().Disconnect(id)
	}
	s, err := config.LoadSecrets(secretsPathFor())
	if err != nil {
		return err
	}
	if _, ok := s.ProviderAPIKeys[id]; !ok {
		return errNotConnected(id)
	}
	return connectionVault().Disconnect(id)
}

func errOAuthOnly(id string) error {
	return &connError{"oauth_required: " + id + " uses secure browser sign-in, not a shared secret"}
}

func errNotConnected(id string) error {
	return &connError{"not_connected: " + id}
}

type connError struct{ msg string }

func (e *connError) Error() string { return e.msg }
