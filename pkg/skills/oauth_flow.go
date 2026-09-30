package skills

import (
	"net/url"
	"strings"
)

// The steps to sign in to Google, Microsoft or Spotify, shared by the web console
// and the phone app so both walk an owner through the same thing.

// OAuthService describes one app the owner can sign in to.
type OAuthService struct {
	Name     string
	Provider string
	Steps    []string
	Console  string
	Note     string
}

// OAuthServices are keyed calendar, gmail, outlook and spotify.
var OAuthServices = map[string]OAuthService{
	"calendar": {Name: "Google Calendar", Provider: ProviderGoogle, Console: "https://console.cloud.google.com/apis/credentials", Steps: googleSteps("Google Calendar API")},
	"gmail":    {Name: "Gmail", Provider: ProviderGoogle, Console: "https://console.cloud.google.com/apis/credentials", Steps: googleSteps("Gmail API")},
	"outlook": {Name: "Outlook", Provider: ProviderMicrosoft, Console: "https://entra.microsoft.com/#view/Microsoft_AAD_RegisteredApps/ApplicationsListBlade", Steps: []string{
		"Open Microsoft Entra, then App registrations, then New registration.",
		"Give it any name. For account type choose the one that includes your account (personal accounts, or any).",
		"Under Redirect URI choose Web and enter the address shown below.",
		"Open API permissions, add Microsoft Graph delegated permissions: Mail.ReadWrite, Mail.Send, Calendars.ReadWrite, offline_access and User.Read.",
		"Open Certificates and secrets, make a new client secret, and copy its Value (not the ID).",
		"Copy the Application (client) ID from Overview. Paste it and the secret below.",
	}},
	"spotify": {Name: "Spotify", Provider: ProviderSpotify, Console: "https://developer.spotify.com/dashboard", Steps: []string{
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

// OAuthServiceForApp maps a connected-app id (google-calendar) to its service key.
func OAuthServiceForApp(appID string) (string, bool) {
	key := appID
	if appID == "google-calendar" {
		key = "calendar"
	}
	_, ok := OAuthServices[key]
	return key, ok
}

// OAuthBegin starts a sign-in and returns the address to open, or needsSetup
// when the owner has not yet given Ghost an app to sign in through.
func OAuthBegin(service, sessionID string) (authURL string, needsSetup bool, err error) {
	svc, ok := OAuthServices[service]
	if !ok {
		return "", false, ErrUnknownProvider
	}
	if !OAuthClientConfigured(svc.Provider) {
		return "", true, nil
	}
	switch service {
	case "calendar":
		cfg, _ := CalendarClientConfig()
		authURL, _, err = CalendarOAuthBegin(cfg, sessionID, "", false)
	case "gmail":
		cfg, _ := GmailClientConfigFromEnv()
		authURL, _, err = GmailOAuthBegin(cfg, sessionID, "", false)
	case "outlook":
		cfg, _ := OutlookClientConfigFromEnv()
		authURL, _, err = OutlookOAuthBegin(cfg, sessionID, "", false)
	case "spotify":
		cfg, _ := SpotifyClientConfigFromEnv()
		authURL, _, err = SpotifyOAuthBegin(cfg, sessionID, "", false)
	}
	return authURL, false, err
}

// OAuthDisconnect forgets the sign-in for a service.
func OAuthDisconnect(service string) error {
	switch service {
	case "calendar":
		return CalendarWebDisconnect()
	case "gmail":
		return GmailWebDisconnect()
	case "outlook":
		return OutlookWebDisconnect()
	case "spotify":
		return SpotifyWebDisconnect()
	}
	return ErrUnknownProvider
}

func oauthCompleters() map[string]func(state, code string) (string, error) {
	return map[string]func(state, code string) (string, error){
		"calendar": func(s, c string) (string, error) {
			cfg, _ := CalendarClientConfig()
			return CalendarOAuthComplete(cfg, s, c, nil, nil)
		},
		"gmail": func(s, c string) (string, error) {
			cfg, _ := GmailClientConfigFromEnv()
			return GmailOAuthComplete(cfg, s, c, nil, nil)
		},
		"outlook": func(s, c string) (string, error) {
			cfg, _ := OutlookClientConfigFromEnv()
			return OutlookOAuthComplete(cfg, s, c, nil, nil)
		},
		"spotify": func(s, c string) (string, error) {
			cfg, _ := SpotifyClientConfigFromEnv()
			return SpotifyOAuthComplete(cfg, s, c, nil, nil)
		},
	}
}

// SignInError is a problem finishing a sign-in, in words to show the owner.
type SignInError struct{ Message string }

func (e *SignInError) Error() string { return e.Message }

// OAuthFinish completes a sign-in from the address the owner pasted back and
// returns the message to show. Any failure is a *SignInError.
func OAuthFinish(service, address string) (string, error) {
	svc, ok := OAuthServices[service]
	complete, hasComplete := oauthCompleters()[service]
	if !ok || !hasComplete {
		return "", &SignInError{"That app can't be connected here."}
	}
	code, state, denied, parsed := ParseSignInAddress(address)
	switch {
	case denied != "":
		return "", &SignInError{"The sign-in was cancelled. You can try again any time."}
	case !parsed:
		return "", &SignInError{signInProblems["invalid_callback"]}
	}
	if _, err := complete(state, code); err != nil {
		return "", &SignInError{FriendlySignInError(err)}
	}
	return svc.Name + " is connected.", nil
}

// ParseSignInAddress pulls the code and state out of what the owner pasted: the
// whole address the browser ended on, or just its query.
func ParseSignInAddress(raw string) (code, state, denied string, ok bool) {
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

// FriendlySignInError turns a sign-in failure into plain words.
func FriendlySignInError(err error) string {
	msg := err.Error()
	for key, text := range signInProblems {
		if strings.HasSuffix(msg, "_"+key) {
			return text
		}
	}
	return "That sign-in didn't complete. Press Connect and try again."
}
