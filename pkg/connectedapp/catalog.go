// Package connectedapp catalog: first-party connected apps for Ghost.
//
// Channels are message transports (Telegram, WhatsApp, ...). They live in
// pkg/channels and are managed via /v1/channels/*. Connected apps are
// external systems Ghost acts on (Gmail, Calendar, Home Assistant, ...).
// They live here and are served via /v1/connected-apps/*.
//
// The model never sees an app or a secret: it sees capability tools
// (email_search, calendar, hass, ...). The runtime resolves
// capability -> usable connected app -> vault credential.
package connectedapp

// AuthKind describes how the user proves ownership of the external system.
type AuthKind string

const (
	AuthOAuth  AuthKind = "oauth"   // browser sign-in via relay/LAN callback, token file 0600
	AuthAPIKey AuthKind = "api_key" // paste-once secret into .secrets.json 0600
	AuthToken  AuthKind = "token"   // paste-once token pair (e.g. hass_url + hass_token)
)

// SetupKind tells clients how to start the connect flow.
type SetupKind string

const (
	// SetupConsoleOAuth requires a browser sign-in; mobile must open the
	// Begin URL and poll status. Never accepts a pasted secret.
	SetupConsoleOAuth SetupKind = "console_oauth"
	// SetupPasteKey accepts POST /v1/connected-apps/{id}/connect {value}.
	SetupPasteKey SetupKind = "paste_key"
	// SetupPastePair accepts two fields (url + token) via the same endpoint
	// with {"value": "<url>", "extra": "<token>"}.
	SetupPastePair SetupKind = "paste_pair"
)

// Connector is the static first-party manifest. Status is live and comes
// from the vault at serve time; everything else here is fixed.
type Connector struct {
	// ID is stable, e.g. "gmail". Never rename: mobile + evidence use it.
	ID string `json:"id"`
	// Provider matches credentials.Vault IDs for .secrets.json lookup.
	Provider string `json:"provider"`
	// DisplayName is user-facing.
	DisplayName string `json:"display_name"`
	// AuthKind + SetupKind drive the mobile connect UI.
	AuthKind AuthKind  `json:"auth_kind"`
	Setup    SetupKind `json:"setup"`
	// Scopes are split so users can grant read-only first.
	ReadScopes  []string `json:"read_scopes"`
	WriteScopes []string `json:"write_scopes,omitempty"`
	// Capabilities are Ghost capability IDs this app can fulfil.
	Capabilities []string `json:"capabilities"`
	// Help is a short product hint shown in clients.
	Help string `json:"help,omitempty"`
}

// FirstParty is the curated set Ghost supports as head-of-engineering.
// Tier 1 ships; Tier 2 is registered so status/connect flows exist even
// before the execution adapter lands.
func FirstParty() []Connector {
	return []Connector{
		{
			ID: "google-calendar", Provider: "google-calendar", DisplayName: "Google Calendar",
			AuthKind: AuthOAuth, Setup: SetupConsoleOAuth,
			ReadScopes:  []string{"calendar.readonly"},
			WriteScopes: []string{"calendar.events"},
			Capabilities: []string{"calendar.read", "calendar.modify"},
			Help:         "Connect in the web console or phone browser. Read-only first; enable writes when needed.",
		},
		{
			ID: "gmail", Provider: "gmail", DisplayName: "Gmail",
			AuthKind: AuthOAuth, Setup: SetupConsoleOAuth,
			ReadScopes:  []string{"gmail.readonly"},
			WriteScopes: []string{"gmail.send"},
			Capabilities: []string{"email.read", "email.send"},
			Help:         "Search and send on your behalf. Sending always asks first. OTPs and reset links are filtered.",
		},
		{
			ID: "outlook", Provider: "outlook", DisplayName: "Outlook",
			AuthKind: AuthOAuth, Setup: SetupConsoleOAuth,
			ReadScopes:  []string{"Mail.Read", "Calendars.Read"},
			WriteScopes: []string{"Mail.Send", "Calendars.ReadWrite"},
			Capabilities: []string{"email.read", "email.send", "calendar.read"},
			Help:         "Microsoft 365 / Outlook.com mail and calendar. Same approval rules as Gmail.",
		},
		{
			ID: "home-assistant", Provider: "homeassistant", DisplayName: "Home Assistant",
			AuthKind: AuthToken, Setup: SetupPastePair,
			Capabilities: []string{"device.read", "device.control"},
			Help:         "Paste your instance URL plus a long-lived token. Stays on your Ghost.",
		},
		{
			ID: "spotify", Provider: "spotify", DisplayName: "Spotify",
			AuthKind: AuthOAuth, Setup: SetupConsoleOAuth,
			ReadScopes:  []string{"user-read-playback-state"},
			WriteScopes: []string{"user-modify-playback-state"},
			Capabilities: []string{"media.playback"},
			Help:         "Playback control. Read-only status first.",
		},
		{
			ID: "github", Provider: "github", DisplayName: "GitHub",
			AuthKind: AuthToken, Setup: SetupPasteKey,
			ReadScopes:  []string{"repo:read"},
			Capabilities: []string{"code.read", "repository.search"},
			Help:         "Paste a read-only personal access token. Never grant write scopes.",
		},
		{
			ID: "notion", Provider: "notion", DisplayName: "Notion",
			AuthKind: AuthToken, Setup: SetupPasteKey,
			Capabilities: []string{"docs"},
			Help:         "Paste an internal integration token for docs access.",
		},
		{
			ID: "openweather", Provider: "openweather", DisplayName: "OpenWeather",
			AuthKind: AuthAPIKey, Setup: SetupPasteKey,
			Capabilities: []string{"weather.get"},
			Help:         "Optional weather provider key. Works without it via built-in fallback.",
		},
		{
			ID: "aviationstack", Provider: "aviationstack", DisplayName: "AviationStack",
			AuthKind: AuthAPIKey, Setup: SetupPasteKey,
			Capabilities: []string{"flight.status"},
			Help:         "Optional flight-status provider key.",
		},
		{
			ID: "aerodatabox", Provider: "aerodatabox", DisplayName: "AeroDataBox",
			AuthKind: AuthAPIKey, Setup: SetupPasteKey,
			Capabilities: []string{"flight.status"},
			Help:         "Optional flight-status fallback key.",
		},
	}
}

// ByID returns the first-party manifest, if any.
func ByID(id string) (Connector, bool) {
	for _, c := range FirstParty() {
		if c.ID == id {
			return c, true
		}
	}
	return Connector{}, false
}

// IsChannelCredential reports vault IDs that are channel transports, not
// connected apps. They must never appear in /v1/connected-apps.
func IsChannelCredential(id string) bool {
	switch id {
	case "telegram", "slack", "discord", "whatsapp", "line", "sms", "wechat", "email-channel":
		return true
	default:
		return false
	}
}

// IsModelCredential reports LLM provider keys, which belong to the AI /
// Intelligence surface (/v1/providers, /v1/model), not connected apps.
func IsModelCredential(id string) bool {
	switch id {
	case "openai", "anthropic", "openrouter", "ollama", "zhipu":
		return true
	default:
		return false
	}
}

// IsOAuthOnly reports apps that must use browser sign-in and must refuse
// pasted secrets on POST .../connect.
func IsOAuthOnly(id string) bool {
	c, ok := ByID(id)
	if !ok {
		return id == "google-calendar"
	}
	return c.AuthKind == AuthOAuth
}
