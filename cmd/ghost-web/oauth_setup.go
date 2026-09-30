package main

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/ianclemence/ghost/pkg/skills"
)

// The console's side of signing in to Google, Microsoft and Spotify. The steps
// and the sign-in itself are shared with the phone app (pkg/skills/oauth_flow.go).

// handleOAuthSetup describes the setup for a service (GET) or saves the owner's
// app (POST). The secret is never returned.
func handleOAuthSetup(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		svc, ok := skills.OAuthServices[r.URL.Query().Get("service")]
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "unknown service"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true, "name": svc.Name, "provider": svc.Provider,
			"configured": skills.OAuthClientConfigured(svc.Provider),
			"redirect":   skills.DefaultRedirect(svc.Provider),
			"steps":      svc.Steps, "console_url": svc.Console,
			"needs_tenant": svc.Provider == skills.ProviderMicrosoft,
		})
	case http.MethodPost:
		var body struct {
			Service      string `json:"service"`
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
			Tenant       string `json:"tenant"`
		}
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body) != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "that wasn't readable"})
			return
		}
		svc, ok := skills.OAuthServices[body.Service]
		if !ok {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "unknown service"})
			return
		}
		if err := skills.SaveOAuthClient(svc.Provider, skills.OAuthClient{ClientID: body.ClientID, ClientSecret: body.ClientSecret, Tenant: body.Tenant}); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleOAuthPaste finishes a sign-in from the address the owner pasted back.
func handleOAuthPaste(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Service string `json:"service"`
		URL     string `json:"url"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&body)
	msg, err := skills.OAuthFinish(body.Service, body.URL)
	if err != nil {
		var se *skills.SignInError
		if errors.As(err, &se) {
			writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": se.Message})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "unknown service"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": msg})
}
