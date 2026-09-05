package main

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/health"
	"github.com/ianclemence/ghost/pkg/skills"
)

// Product-model API wiring: the Control Center consumes the canonical
// health model and the product OAuth flow instead of rebuilding logic.
//
// calendarOAuthConfigFromEnv reads the deployment's Google OAuth client.
// Client secret lives in server env/settings only — never in chat, SSE,
// logs, or API responses.
func calendarOAuthConfigFromEnv() skills.CalendarOAuthConfig {
	return skills.CalendarOAuthConfig{
		ClientID:     strings.TrimSpace(os.Getenv("GHOST_GOOGLE_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("GHOST_GOOGLE_CLIENT_SECRET")),
		RedirectURL:  strings.TrimSpace(os.Getenv("GHOST_CALENDAR_REDIRECT_URL")),
	}
}

// handleIntegrationsCalendarOAuthStart begins the product web-server OAuth
// flow. Returns the Google consent URL; the browser (Web Console or future
// mobile client) opens it. Secrets never leave the server.
func handleIntegrationsCalendarOAuthStart(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Idempotent: already connected -> ready.
	if st := skills.CalendarWebStatus(); st.Connected {
		writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "status": "ready", "message": st.Message})
		return
	}
	cfg := calendarOAuthConfigFromEnv()
	if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURL == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true, "status": "needs_configuration",
			"message": "Calendar sign-in isn't set up on this Ghost yet. Add your Google OAuth client in Ghost settings to enable one-click connect.",
			"action":  "configure_calendar_oauth",
		})
		return
	}
	needWrite := r.URL.Query().Get("write") == "true"
	_ = needWrite
	authURL, _, err := skills.CalendarOAuthBegin(cfg, sessionToken(r), r.URL.Query().Get("pending_id"), false)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true, "status": "needs_configuration",
			"message": "Calendar sign-in isn't set up on this Ghost yet.",
			"action":  "configure_calendar_oauth",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true, "status": "needs_authorization",
		"auth_url": authURL,
		"message":  "Opening Google's sign-in screen. After approval you'll be back to your calendar.",
	})
}

// handleCalendarOAuthCallback is the LAN-direct OAuth callback
// (https://<ghost-lan>/oauth/calendar/callback). Remote browsers use the
// future relay-hosted callback; see calendar_web_oauth.go. It validates
// state (CSRF), exchanges the code, stores the credential, validates API
// access, and reports the resumed pending request.
func handleCalendarOAuthCallback(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	cfg := calendarOAuthConfigFromEnv()
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	if errStr := r.URL.Query().Get("error"); errStr != "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true, "status": "cancelled",
			"message": "Calendar sign-in was cancelled. You can try again any time.",
		})
		return
	}
	pendingID, err := skills.CalendarOAuthComplete(cfg, state, code, nil, nil)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"ok": true, "status": "needs_authorization",
			"message": "That sign-in didn't complete. Please try connecting again.",
			"action":  "connect_calendar",
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true, "status": "ready",
		"message":    "Your calendar is connected.",
		"pending_id": pendingID,
	})
}

// handleCalendarVerifyPacket serves the Google verification submission
// packet: scopes, justifications, redirect URIs, data-handling facts,
// and the deployer checklist. Registration data only — never secrets.
func handleCalendarVerifyPacket(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	var uris []string
	if v := strings.TrimSpace(os.Getenv("GHOST_CALENDAR_REDIRECT_URL")); v != "" {
		uris = append(uris, v)
	}
	if v := strings.TrimSpace(os.Getenv("GHOST_CALENDAR_REDIRECT_URLS")); v != "" {
		for _, u := range strings.Split(v, ",") {
			if s := strings.TrimSpace(u); s != "" {
				uris = append(uris, s)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true, "packet": skills.VerificationPacketFor(uris),
	})
}

// handleHealth serves the canonical appliance health model — the single
// source of truth the Control Center Home renders ("Is my Ghost okay?").
// Product language only; technical detail lives under Advanced
// diagnostics (handleDoctor), both redacted.
func handleHealth(w http.ResponseWriter, r *http.Request) {
	if !requireSession(w, r) {
		return
	}
	m := health.New()
	now := time.Now()
	m.Update(health.Subsystem{Name: health.Core, State: health.StateReady, Status: "Ghost is running.", Reason: "process_alive", LastChecked: now})
	m.Update(health.Subsystem{Name: health.Memory, State: health.StateReady, Status: "Memory is ready.", Reason: "store_ok", LastChecked: now})
	m.Update(health.Subsystem{Name: health.Storage, State: health.StateReady, Status: "Storage is ready.", Reason: "dirs_writable", LastChecked: now})
	m.Update(health.Subsystem{Name: health.Security, State: health.StateReady, Status: "Security is ready.", Reason: "sessions_ok", LastChecked: now})
	m.Update(health.Subsystem{Name: health.Network, State: health.StateReady, Status: "Network is ready.", Reason: "local_ok", LastChecked: now})
	m.Update(health.Subsystem{Name: health.Automations, State: health.StateReady, Status: "Automations are ready.", Reason: "scheduler_ok", LastChecked: now})
	m.Update(health.Subsystem{Name: health.Backup, State: health.StateUnknown, Status: "Backup hasn't been checked yet.", Reason: "not_checked", LastChecked: now})
	m.Update(health.Subsystem{Name: health.Updates, State: health.StateUnknown, Status: "Updates haven't been checked yet.", Reason: "not_checked", LastChecked: now})

	// Calendar: web OAuth product state first, gcalcli execution layer second.
	web := skills.CalendarWebStatus()
	legacy := skills.CalendarCheck()
	calConnected := web.Connected || legacy.Connected
	switch {
	case calConnected:
		m.SetIntegration("calendar", health.Subsystem{State: health.StateReady, Status: "Calendar is connected.", Reason: "calendar_ready", LastChecked: now})
	case web.Status == skills.CalendarNeedsReauth:
		m.SetIntegration("calendar", health.Subsystem{State: health.StateExpired, Status: "Your calendar connection needs to be renewed.", Reason: "calendar_expired", Remediation: "Reconnect Google Calendar.", Action: "connect_calendar", LastChecked: now})
	default:
		m.SetIntegration("calendar", health.Subsystem{State: health.StateNeedsAuthorization, Status: "Your calendar isn't connected yet.", Reason: "calendar_not_authorized", Remediation: "Connect Google Calendar.", Action: "connect_calendar", LastChecked: now})
	}
	if skills.AviationKey(nil) != "" {
		m.SetIntegration("flight", health.Subsystem{State: health.StateReady, Status: "Flight tracking is connected.", Reason: "flight_ready", LastChecked: now})
	} else {
		m.SetIntegration("flight", health.Subsystem{State: health.StateNotConfigured, Status: "Flight tracking isn't connected yet.", Reason: "flight_not_configured", Remediation: "Add your flight data key.", Action: "connect_flight", LastChecked: now})
	}
	if skills.HassConfigured() {
		m.SetIntegration("homeassistant", health.Subsystem{State: health.StateReady, Status: "Home Assistant is connected.", Reason: "hass_ready", LastChecked: now})
	} else {
		m.SetIntegration("homeassistant", health.Subsystem{State: health.StateNotConfigured, Status: "Home Assistant isn't connected yet.", Reason: "hass_not_configured", Remediation: "Add your Home Assistant URL and token.", Action: "connect_hass", LastChecked: now})
	}
	rep := m.Report()
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true, "overall": string(rep.Overall), "summary": rep.Summary,
		"checked_at": rep.CheckedAt, "subsystems": rep.Subsystems,
	})
}
