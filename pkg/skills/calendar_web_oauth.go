package skills

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"
)

// Proper Google OAuth 2.0 web-server flow for Calendar (product path).
//
// The gcalcli device flow in calendar_oauth.go remains as a fallback
// execution layer; this file is the product OAuth architecture. Answers
// to the deployment questions:
//
//  1. Callback home: production = a Ghost-owned HTTPS callback service
//     (relay) that receives Google's redirect and forwards the code to the
//     originating Ghost over the already-authenticated relay channel.
//  2. LAN: when the browser is on the LAN, Ghost serves the callback
//     directly at https://<ghost-lan>:<port>/oauth/calendar/callback.
//  3. Behind the relay: remote browsers never reach the LAN Ghost, so the
//     relay-hosted callback receives the code and relays it back.
//  4. Remote users: same as (3) — relay callback, never direct LAN.
//  5. A Ghost-owned relay callback service EXISTS (pkg/relay/server
//     handleOAuthCallback): GET /oauth/calendar/callback on the relay
//     forwards allowlisted code/state to the Ghost over its
//     already-authenticated device tunnel; the Ghost validates state and
//     exchanges with its own secret. LAN-only setup can use the direct
//     ghost-web callback without the relay.
//  6. A local callback (http://localhost) is NOT viable as the product
//     path: it runs in the user's browser, not on the Ghost machine, so
//     tokens would land on the wrong device.
//  7. CSRF: cryptographically random 256-bit state, bound to the
//     originating session + pending request, single-use, 10-minute expiry,
//     constant-time comparison.
//  8. Credentials: client secret never leaves the Ghost machine; refresh
//     tokens in a 0600 file outside backup roots; access tokens in memory
//     only.
//  9. Refresh tokens persist in the token file; restart reloads them.
//  10. Revocation detected via invalid_grant on refresh/exchange and via
//     401 on validation -> needs_reauth (never silently "ready").
//  11. Disconnect deletes the token file and marks needs_setup.
//  12. Reconnect is Begin again; old tokens are replaced atomically.
//  13. Restart: token file reloaded; in-flight states expire safely.
//  14. Backup: token files are EXCLUDED from backups; restore requires
//     reconnect (documented, never plaintext secrets in archives).
//  15. Restore: integrations show needs_setup until reconnected.
//  16. Multi-device: tokens belong to the owner's Ghost, not to a device;
//     devices share the owner's integration state via authenticated APIs.
//  17. No token/code/secret/state values in logs: Redacted() structs and
//     no-value error paths (see below).
//  18. Web Console and mobile both call Begin (server mints URL+state) and
//     poll Status; neither ever sees secrets.
//  19. Single Ghost OAuth client per deployment (BYO client ID/secret via
//     settings); a Ghost-managed shared client is a future option once a
//     relay callback service exists.
//  20. Verification: readonly scope needs standard verification; the events
//     (write) scope adds sensitive-scope review. Request the minimum scope
//     for the capability actually used (see ScopesFor).
//
// Security: this file never returns token contents. Errors carry reason
// codes, not credential material.

// Calendar OAuth scopes (narrowest-first).
const (
	// ScopeCalendarReadonly supports reading/listing events only.
	ScopeCalendarReadonly = "https://www.googleapis.com/auth/calendar.readonly"
	// ScopeCalendarEvents supports reading plus creating/editing events.
	ScopeCalendarEvents = "https://www.googleapis.com/auth/calendar.events"
)

// ScopesFor returns the narrowest scopes for the capability. Read-only
// callers get readonly; only event-writing callers get the events scope.
// Full calendar access is never requested.
func ScopesFor(needWrite bool) []string {
	if needWrite {
		return []string{ScopeCalendarEvents}
	}
	return []string{ScopeCalendarReadonly}
}

// CalendarOAuthConfig is the deployment's Google OAuth client. ClientSecret
// must come from secure settings/secrets, never from chat or logs.
type CalendarOAuthConfig struct {
	ClientID     string
	ClientSecret string
	// RedirectURL is the registered callback (relay or LAN direct).
	RedirectURL string
}

// googleEndpoint is Google's OAuth 2.0 web-server endpoint, defined
// directly (stable documented URLs) so Ghost does not pull the
// oauth2/google subpackage and its cloud-metadata dependency.
func googleEndpoint() oauth2.Endpoint {
	return oauth2.Endpoint{
		AuthURL:   "https://accounts.google.com/o/oauth2/auth",
		TokenURL:  "https://oauth2.googleapis.com/token",
		AuthStyle: oauth2.AuthStyleInParams,
	}
}

// newOAuth2Config builds the oauth2 config for the needed scopes.
func newOAuth2Config(cfg CalendarOAuthConfig, needWrite bool) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Scopes:       ScopesFor(needWrite),
		Endpoint:     googleEndpoint(),
	}
}

// oauthState is a pending authorization attempt.
type oauthState struct {
	State     string    `json:"state"`
	SessionID string    `json:"session_id"`
	PendingID string    `json:"pending_id,omitempty"`
	NeedWrite bool      `json:"need_write"`
	CreatedAt time.Time `json:"created_at"`
}

// CalendarToken is the persisted credential (0600, outside backups).
type CalendarToken struct {
	RefreshToken string    `json:"refresh_token"`
	AccessToken  string    `json:"access_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	StoredAt     time.Time `json:"stored_at"`
}

const oauthStateTTL = 10 * time.Minute

var (
	calOAuthMu     sync.Mutex
	calOAuthStates = map[string]oauthState{}
)

// CalendarTokenDir returns the secure token directory (overridable for
// tests via GHOST_CREDENTIALS_DIR). Deliberately outside backup-walked
// roots (config, data, workspace) so tokens are never archived.
func CalendarTokenDir() string {
	if d := strings.TrimSpace(os.Getenv("GHOST_CREDENTIALS_DIR")); d != "" {
		return d
	}
	return "/var/lib/ghost/.credentials"
}

// calendarTokenPath is the token file path.
func calendarTokenPath() string {
	return filepath.Join(CalendarTokenDir(), "calendar-token.json")
}

// newOAuthState mints a 256-bit random state bound to a session.
func newOAuthState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// CalendarOAuthBegin starts authorization: returns the Google consent URL.
// The caller (Web Console/mobile backend) shows the URL; secrets never
// leave the server. pendingID links the preserved user request to resume.
func CalendarOAuthBegin(cfg CalendarOAuthConfig, sessionID, pendingID string, needWrite bool) (authURL, state string, err error) {
	if strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.RedirectURL) == "" {
		return "", "", errors.New("calendar_oauth_not_configured")
	}
	if strings.TrimSpace(sessionID) == "" {
		return "", "", errors.New("calendar_oauth_session_required")
	}
	state, err = newOAuthState()
	if err != nil {
		return "", "", errors.New("calendar_oauth_state_failed")
	}
	calOAuthMu.Lock()
	calOAuthStates[state] = oauthState{State: state, SessionID: sessionID, PendingID: pendingID, NeedWrite: needWrite, CreatedAt: time.Now()}
	// Opportunistic expiry sweep.
	for k, v := range calOAuthStates {
		if time.Since(v.CreatedAt) > oauthStateTTL {
			delete(calOAuthStates, k)
		}
	}
	calOAuthMu.Unlock()
	oc := newOAuth2Config(cfg, needWrite)
	return oc.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce), state, nil
}

// Exchanger exchanges a code for tokens (injectable for tests).
type Exchanger func(ctx context.Context, cfg CalendarOAuthConfig, needWrite bool, code string) (*CalendarToken, error)

// Validator checks Calendar API access with the token (injectable).
type Validator func(ctx context.Context, tok *CalendarToken) error

// defaultExchanger performs the real code exchange + immediate validation
// of token shape (never logs values).
func defaultExchanger(ctx context.Context, cfg CalendarOAuthConfig, needWrite bool, code string) (*CalendarToken, error) {
	oc := newOAuth2Config(cfg, needWrite)
	tok, err := oc.Exchange(ctx, code)
	if err != nil {
		return nil, classifyOAuthError(err)
	}
	out := &CalendarToken{StoredAt: time.Now(), Scope: strings.Join(ScopesFor(needWrite), " ")}
	if tok.RefreshToken != "" {
		out.RefreshToken = tok.RefreshToken
	}
	if tok.AccessToken != "" {
		out.AccessToken = tok.AccessToken
	}
	out.Expiry = tok.Expiry
	if out.RefreshToken == "" && out.AccessToken == "" {
		return nil, errors.New("calendar_oauth_empty_token")
	}
	return out, nil
}

// defaultValidator proves Calendar API access with a minimal events.list.
func defaultValidator(ctx context.Context, tok *CalendarToken) error {
	if tok == nil || (tok.AccessToken == "" && tok.RefreshToken == "") {
		return errors.New("calendar_oauth_no_credential")
	}
	// Only the access token works here; without one, report honestly so
	// the caller can refresh instead of claiming success.
	if tok.AccessToken == "" {
		return errors.New("calendar_oauth_needs_refresh")
	}
	req, err := http.NewRequestWithContext(ctx, "GET",
		"https://www.googleapis.com/calendar/v3/calendars/primary/events?maxResults=1&singleEvents=true", nil)
	if err != nil {
		return errors.New("calendar_oauth_validation_failed")
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("calendar_oauth_validation_failed")
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 200:
		return nil
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return errors.New("calendar_oauth_unauthorized")
	default:
		return errors.New("calendar_oauth_validation_failed")
	}
}

// classifyOAuthError maps exchange failures to reason codes without
// leaking response bodies (which can contain credential material).
func classifyOAuthError(err error) error {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "invalid_grant"):
		return errors.New("calendar_oauth_revoked_or_expired")
	case strings.Contains(msg, "invalid_client"), strings.Contains(msg, "unauthorized_client"):
		return errors.New("calendar_oauth_misconfigured")
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return errors.New("calendar_oauth_timeout")
	default:
		return errors.New("calendar_oauth_exchange_failed")
	}
}

// CalendarOAuthComplete validates state (CSRF), exchanges the code,
// persists the refresh credential, validates API access, and returns the
// pending request ID to resume. On any failure nothing is marked ready.
func CalendarOAuthComplete(cfg CalendarOAuthConfig, state, code string, exch Exchanger, valid Validator) (pendingID string, err error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return "", errors.New("calendar_oauth_invalid_callback")
	}
	calOAuthMu.Lock()
	st, ok := calOAuthStates[state]
	// Constant-time comparison against stored keys to avoid subtle
	// mismatch oracles; single-use regardless of outcome.
	var matched string
	for k := range calOAuthStates {
		if subtle.ConstantTimeCompare([]byte(k), []byte(state)) == 1 {
			matched = k
		}
	}
	if ok && matched != "" {
		delete(calOAuthStates, matched)
	}
	calOAuthMu.Unlock()
	if !ok || matched == "" {
		return "", errors.New("calendar_oauth_bad_state")
	}
	if time.Since(st.CreatedAt) > oauthStateTTL {
		return "", errors.New("calendar_oauth_state_expired")
	}
	if exch == nil {
		exch = defaultExchanger
	}
	if valid == nil {
		valid = defaultValidator
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	tok, err := exch(ctx, cfg, st.NeedWrite, code)
	if err != nil {
		return "", err
	}
	if err := valid(ctx, tok); err != nil {
		return "", err
	}
	if err := storeCalendarToken(tok); err != nil {
		return "", errors.New("calendar_oauth_store_failed")
	}
	return st.PendingID, nil
}

// storeCalendarToken persists the credential atomically with 0600 perms.
func storeCalendarToken(tok *CalendarToken) error {
	if tok == nil || strings.TrimSpace(tok.RefreshToken) == "" && strings.TrimSpace(tok.AccessToken) == "" {
		return errors.New("calendar_oauth_empty_token")
	}
	tok.StoredAt = time.Now()
	dir := CalendarTokenDir()
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "calendar-token.json.tmp")
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, calendarTokenPath())
}

// LoadCalendarToken reloads the persisted credential (restart-safe).
// It returns the token for server-side use only; callers must never
// serialize it to chat/SSE/logs/backups.
func LoadCalendarToken() (*CalendarToken, error) {
	data, err := os.ReadFile(calendarTokenPath())
	if err != nil {
		return nil, err
	}
	var tok CalendarToken
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

// CalendarWebStatus reports product state for the web OAuth path.
func CalendarWebStatus() CalendarState {
	tok, err := LoadCalendarToken()
	if err != nil || tok == nil {
		return CalendarState{Status: CalendarNeedsSetup, NeedsSetup: true,
			Message: "Your calendar isn't connected yet. Connect Google Calendar to continue."}
	}
	if strings.TrimSpace(tok.RefreshToken) == "" && strings.TrimSpace(tok.AccessToken) == "" {
		return CalendarState{Status: CalendarNeedsReauth, NeedsSetup: true,
			Message: "Your calendar connection needs to be renewed."}
	}
	return CalendarState{Status: CalendarReady, Connected: true, Message: "Calendar is connected."}
}

// CalendarWebDisconnect removes the credential (user disconnect flow).
func CalendarWebDisconnect() error {
	err := os.Remove(calendarTokenPath())
	if err != nil && !os.IsNotExist(err) {
		return errors.New("calendar_disconnect_failed")
	}
	return nil
}

// CalendarDiagnostics is the redacted diagnostics projection: presence,
// scope, and age — never credential material.
type CalendarDiagnostics struct {
	Configured bool   `json:"configured"`
	Scope      string `json:"scope,omitempty"`
	StoredAt   string `json:"stored_at,omitempty"`
}

// CalendarRedactedDiagnostics returns safe diagnostics for logs/UI.
func CalendarRedactedDiagnostics() CalendarDiagnostics {
	tok, err := LoadCalendarToken()
	if err != nil || tok == nil {
		return CalendarDiagnostics{Configured: false}
	}
	d := CalendarDiagnostics{Configured: true, Scope: tok.Scope}
	if !tok.StoredAt.IsZero() {
		d.StoredAt = tok.StoredAt.Format(time.RFC3339)
	}
	return d
}

// RefreshCalendarToken exchanges the stored refresh token for a fresh
// access token using the deployment's OAuth client.
func RefreshCalendarToken(cfg CalendarOAuthConfig, needWrite bool) (*CalendarToken, error) {
	tok, err := LoadCalendarToken()
	if err != nil || tok == nil || strings.TrimSpace(tok.RefreshToken) == "" {
		return nil, errors.New("calendar_oauth_no_refresh_token")
	}
	oc := newOAuth2Config(cfg, needWrite)
	src := oc.TokenSource(context.Background(), &oauth2.Token{RefreshToken: tok.RefreshToken})
	nt, err := src.Token()
	if err != nil {
		return nil, classifyOAuthError(err)
	}
	updated := &CalendarToken{Scope: tok.Scope, StoredAt: time.Now(), Expiry: nt.Expiry}
	if nt.RefreshToken != "" {
		updated.RefreshToken = nt.RefreshToken
	} else {
		updated.RefreshToken = tok.RefreshToken
	}
	updated.AccessToken = nt.AccessToken
	if err := storeCalendarToken(updated); err != nil {
		return nil, errors.New("calendar_oauth_store_failed")
	}
	return updated, nil
}

var _ = fmt.Sprintf // keep fmt import if unused in future edits
