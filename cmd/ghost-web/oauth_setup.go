package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/ianclemence/ghost/pkg/skills"
)

// Signing in to Google, Microsoft or Spotify needs an app registered with them.
// Ghost does not ship one, so the console walks the owner through registering
// their own, keeps its ID and secret sealed, and finishes the sign-in when the
// owner pastes back the address the browser ended on. Nothing here needs a
// public address for the Pod.

type oauthService struct {
	Name     string
	Provider string
	Steps    []string
	Console  string
	Note     string
}

var oauthServices = map[string]oauthService{
	"calendar": {Name: "Google Calendar", Provider: skills.ProviderGoogle, Console: "https://console.cloud.google.com/apis/credentials", Steps: googleSteps("Google Calendar API")},
	"gmail":    {Name: "Gmail", Provider: skills.ProviderGoogle, Console: "https://console.cloud.google.com/apis/credentials", Steps: googleSteps("Gmail API")},
	"outlook": {Name: "Outlook", Provider: skills.ProviderMicrosoft, Console: "https://entra.microsoft.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade", Steps: []string{
		"Open Microsoft Entra, then App registrations, then New registration.",
		"Give it any name. For account type choose the one that includes your account (personal accounts, or any).",
		"Under Redirect URI choose Web and enter the address shown below.",
		"Open API permissions, add Microsoft Graph delegated permissions: Mail.ReadWrite, Mail.Send, Calendars.ReadWrite, offline_access and User.Read.",
		"Open Certificates and secrets, make a new client secret, and copy its Value (not the ID).",
		"Copy the Application (client) ID from Overview. Paste it and the secret below.",
	}},
	"spotify": {Name: "Spotify", Provider: skills.ProviderSpotify, Console: "https://developer.spotify.com/dashboard", Steps: []string{
		"Open the Spotify developer dashboard and create an app. Any name and description will do.",
		"Under Redirect URIs add the address shown below exactly, and choose the Web API.",
		"Open the app's Settings and copy the Client ID and the Client secret. Paste them below.",
	}},
}

func googleSteps(api string) []string {
	return []string{
		"Open Google Cloud and create a project, or choose one you already have.",
		"Turn on the " + api + " for it (APIs and services, then Library).",
		"Set up the consent screen: choose External, and add your own Google address as a test user.",
		"Then choose Publish app. Without this Google disconnects Ghost every 7 days. Google will warn that the app is unverified; that is fine for an app only you use.",
		"Under Credentials make an OAuth client ID and choose Desktop app as the type.",
		"Copy the Client ID and the Client secret. Paste them below.",
	}
}

func oauthCompleters() map[string]func(state, code string) (string, error) {
	return map[string]func(state, code string) (string, error){
		"calendar": func(s, c string) (string, error) {
			cfg, _ := skills.CalendarClientConfig()
			return skills.CalendarOAuthComplete(cfg, s, c, nil, nil)
		},
		"gmail": func(s, c string) (string, error) {
			cfg, _ := skills.GmailClientConfigFromEnv()
			return skills.GmailOAuthComplete(cfg, s, c, nil, nil)
		},
		"outlook": func(s, c string) (string, error) {
			cfg, _ := skills.OutlookClientConfigFromEnv()
			return skills.OutlookOAuthComplete(cfg, s, c, nil, nil)
		},
		"spotify": func(s, c string) (string, error) {
			cfg, _ := skills.SpotifyClientConfigFromEnv()
			return skills.SpotifyOAuthComplete(cfg, s, c, nil, nil)
		},
	}
}

// handleOAuthSetup describes the setup for a service (GET) or saves the owner's
// app (POST). The secret is never returned.
func handleOAuthSetup(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	switch r.Method {
	case http.MethodGet:
		svc, ok := oauthServices[r.URL.Query().Get("service")]
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
		svc, ok := oauthServices[body.Service]
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

// parseSignInAddress pulls the code and state out of what the owner pasted: the
// whole address the browser ended on, or just its query.
func parseSignInAddress(raw string) (code, state, denied string, ok bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", false
	}
	q := url.Values{}
	if u, err := url.Parse(raw); err == nil && u.RawQuery != "" {
		q = u.Query()
	} else if v, err := url.ParseQuery(strings.TrimPrefix(raw, "?")); err == nil {
		q = v
	}
	if e := q.Get("error"); e != "" {
		return "", "", e, true
	}
	code, state = q.Get("code"), q.Get("state")
	return code, state, "", code != "" && state != ""
}

var signInProblems = map[string]string{
	"bad_state":          "That sign-in has expired, or it wasn't started from here. Press Connect and try again.",
	"state_expired":      "That sign-in took too long. Press Connect and try again.",
	"revoked_or_expired": "That code was already used or has expired. Press Connect and try again.",
	"misconfigured":      "The provider didn't accept the app's ID or secret. Check what you pasted in the setup, and that a Desktop app (Google) or Web redirect (Microsoft) was chosen.",
	"unauthorized":       "You signed in, but the account didn't give Ghost access. Try again and approve everything it asks for.",
	"timeout":            "The provider took too long to answer. Try again.",
	"invalid_callback":   "That address doesn't have a sign-in code in it. Copy the whole address from the page that failed to load.",
	"validation_failed":  "You signed in, but Ghost couldn't reach the service to check it. Try again in a moment.",
	"exchange_failed":    "The provider refused the sign-in. Press Connect and try again.",
	"store_failed":       "Signed in, but Ghost couldn't save the connection. Check the Pod's storage.",
}

func friendlySignInError(err error) string {
	msg := err.Error()
	for key, text := range signInProblems {
		if strings.HasSuffix(msg, "_"+key) {
			return text
		}
	}
	return "That sign-in didn't complete. Press Connect and try again."
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
	complete, ok := oauthCompleters()[body.Service]
	if !ok {
		writeJSON(w, http.StatusBadRequest, map[string]interface{}{"ok": false, "error": "unknown service"})
		return
	}
	code, state, denied, parsed := parseSignInAddress(body.URL)
	switch {
	case denied != "":
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": "The sign-in was cancelled. You can try again any time."})
		return
	case !parsed:
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": signInProblems["invalid_callback"]})
		return
	}
	if _, err := complete(state, code); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": false, "error": friendlySignInError(err)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "message": oauthServices[body.Service].Name + " is connected."})
}
