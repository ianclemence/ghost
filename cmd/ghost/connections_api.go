package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ianclemence/ghost/pkg/config"
	"github.com/ianclemence/ghost/pkg/connectedapp"
	"github.com/ianclemence/ghost/pkg/credentials"
)

// registerConnectionsRoutes wires the device-facing connected-apps API.
//
// Canonical naming:
//   Channels       = message transports (Telegram, WhatsApp, ...).
//                    Managed via /v1/channels/status + /v1/channels/reconnect.
//                    Never appear in connected-apps payloads.
//   Connected apps = external systems Ghost acts on (Gmail, Calendar,
//                    Home Assistant, Spotify, GitHub, ...).
//                    Managed via /v1/connected-apps/* with key "connected_apps".
//   Credentials    = secrets vault internals (.secrets.json, token files).
//                    Never exposed; only status is surfaced via connected-apps.
func registerConnectionsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/v1/connected-apps/", authMiddleware(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/connected-apps/"), "/")
		serveConnectedAppAction(w, r, rest)
	}))
}

func serveConnectedAppAction(w http.ResponseWriter, r *http.Request, rest string) {
	if rest == "" {
		jsonError(w, http.StatusNotFound, "not_found", "unknown connected-app endpoint")
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
		if err := appDisconnect(id); err != nil {
			jsonError(w, http.StatusBadRequest, "disconnect_failed", err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"ok": true,
			"connected_app": map[string]interface{}{
				"id": id, "status": "disconnected",
			},
		})
	default:
		// Connect: accept a secret once (never echo it). OAuth-only apps
		// have no exportable secret and are refused here.
		if len(parts) != 1 || parts[0] == "" || parts[0] == "connect" {
			jsonError(w, http.StatusNotFound, "not_found", "use POST /v1/connected-apps/{id} with {\"value\": \"...\"}")
			return
		}
		if r.Method != http.MethodPost {
			jsonError(w, http.StatusMethodNotAllowed, "invalid_request", "use POST")
			return
		}
		var body struct {
			Value string `json:"value"`
			Extra string `json:"extra,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Value) == "" {
			jsonError(w, http.StatusBadRequest, "invalid_request", "value is required")
			return
		}
		if err := appConnect(id, strings.TrimSpace(body.Value), strings.TrimSpace(body.Extra)); err != nil {
			jsonError(w, http.StatusBadRequest, "connect_failed", err.Error())
			return
		}
		jsonResponse(w, http.StatusOK, map[string]interface{}{
			"ok": true,
			"connected_app": map[string]interface{}{
				"id": id, "status": "connected",
			},
		})
	}
}

// secretsPathFor resolves the .secrets.json used by this gateway.
func secretsPathFor() string {
	return config.SecretsPath(getConfigPath())
}

// connectionVault returns the single credential boundary for this gateway.
// All connected-app writes and disconnects go through it; nothing writes the
// secrets file directly.
func connectionVault() *credentials.Vault {
	return credentials.New(filepath.Dir(secretsPathFor()))
}

// appConnect stores a pasted secret for key/token apps. OAuth-only apps
// (Gmail, Outlook, Spotify, Google Calendar) must use secure browser
// sign-in and are refused here. Home Assistant accepts a URL + token pair.
func appConnect(id, value, extra string) error {
	if connectedapp.IsOAuthOnly(id) {
		return errOAuthOnly(id)
	}
	if id == "home-assistant" || id == "homeassistant" {
		// Accept "url" in value and token in extra; store as hass pair.
		if extra != "" {
			if err := connectionVault().Store("hass_url", value); err != nil {
				return err
			}
			return connectionVault().Store("hass_token", extra)
		}
		return connectionVault().Store(id, value)
	}
	if connectedapp.IsChannelCredential(id) {
		return &connError{"not_a_connected_app: " + id + " is a channel transport, see /v1/channels/status"}
	}
	if connectedapp.IsModelCredential(id) {
		return &connError{"not_a_connected_app: " + id + " is a model provider, see /v1/providers"}
	}
	return connectionVault().Store(id, value)
}

func appDisconnect(id string) error {
	vault := connectionVault()
	// Google Calendar is an OAuth connected app: the vault removes the
	// actual credential (both calendar stacks), not just a config key.
	if id == "google-calendar" {
		return vault.Disconnect(id)
	}
	// Presence check goes through the Vault, never the raw secret store.
	if !vault.Configured(id) {
		// Home Assistant pair counts as configured even though the vault
		// key differs from the app id.
		if id == "home-assistant" && credentials.HassConfigured() {
			_ = vault.Disconnect("hass_url")
			return vault.Disconnect("hass_token")
		}
		return errNotConnected(id)
	}
	return vault.Disconnect(id)
}

// connectionStore / connectionDisconnect remain as deprecated aliases so
// older call sites keep compiling; new code must use appConnect/appDisconnect.
func connectionStore(id, value string) error { return appConnect(id, value, "") }

func connectionDisconnect(id string) error { return appDisconnect(id) }

func errOAuthOnly(id string) error {
	return &connError{"oauth_required: " + id + " uses secure browser sign-in, not a shared secret"}
}

func errNotConnected(id string) error {
	return &connError{"not_connected: " + id}
}

type connError struct{ msg string }

func (e *connError) Error() string { return e.msg }
