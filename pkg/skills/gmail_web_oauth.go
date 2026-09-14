package skills

// Gmail OAuth 2.0 web-server flow (product path).
//
// Cloned from calendar_web_oauth.go. Per head-of-engineering decision the
// deployment uses ONE shared Google Cloud project for Calendar + Gmail:
// same ClientID/Secret, separate redirect URLs (both registered on the
// Cloud client: /oauth/calendar/callback and /oauth/gmail/callback).
//
// Security properties mirror Calendar exactly:
//   - 256-bit single-use state, 10-minute expiry, constant-time compare
//   - refresh tokens sealed (AES-256-GCM) in 0600 files outside backup roots
//   - revocation via invalid_grant / 401 -> needs_reauth, never silent ready
//   - no token/code/secret/state values in logs (reason codes only)

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"github.com/ianclemence/ghost/pkg/config"
)

// Gmail OAuth scopes (narrowest-first).
const (
	// ScopeGmailReadonly supports reading/listing messages only.
	ScopeGmailReadonly = "https://www.googleapis.com/auth/gmail.readonly"
	// ScopeGmailSend supports reading plus sending on the user's behalf.
	ScopeGmailSend = "https://www.googleapis.com/auth/gmail.send"
)

// GmailScopesFor returns the narrowest scopes for the capability. Read-only
// callers get readonly; only senders get the send scope. Full mailbox
// access is never requested.
func GmailScopesFor(needWrite bool) []string {
	if needWrite {
		return []string{ScopeGmailSend}
	}
	return []string{ScopeGmailReadonly}
}

// GmailOAuthConfig is the deployment's shared Google OAuth client. The
// ClientID/Secret are the same Cloud project as Calendar; only the
// RedirectURL differs (/oauth/gmail/callback). Secrets never leave the
// Ghost machine.
type GmailOAuthConfig struct {
	ClientID     string
	ClientSecret string
	// RedirectURL is the registered Gmail callback (relay or LAN direct).
	RedirectURL string
}

// newGmailOAuth2Config builds the oauth2 config for the needed scopes.
// googleEndpoint() is shared with Calendar (same Google auth servers).
func newGmailOAuth2Config(cfg GmailOAuthConfig, needWrite bool) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Scopes:       GmailScopesFor(needWrite),
		Endpoint:     googleEndpoint(),
	}
}

// GmailToken is the persisted credential (0600, outside backups).
type GmailToken struct {
	RefreshToken string    `json:"refresh_token"`
	AccessToken  string    `json:"access_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	StoredAt     time.Time `json:"stored_at"`
}

var (
	gmailOAuthMu     sync.Mutex
	gmailOAuthStates = map[string]oauthState{}
)

// gmailTokenPath is the token file path (same secure dir as Calendar).
func gmailTokenPath() string {
	return filepath.Join(CalendarTokenDir(), "gmail-token.json")
}

// GmailOAuthBegin starts authorization: returns the Google consent URL.
func GmailOAuthBegin(cfg GmailOAuthConfig, sessionID, pendingID string, needWrite bool) (authURL, state string, err error) {
	if strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.RedirectURL) == "" {
		return "", "", errors.New("gmail_oauth_not_configured")
	}
	if strings.TrimSpace(sessionID) == "" {
		return "", "", errors.New("gmail_oauth_session_required")
	}
	state, err = newOAuthState()
	if err != nil {
		return "", "", errors.New("gmail_oauth_state_failed")
	}
	gmailOAuthMu.Lock()
	gmailOAuthStates[state] = oauthState{State: state, SessionID: sessionID, PendingID: pendingID, NeedWrite: needWrite, CreatedAt: time.Now()}
	for k, v := range gmailOAuthStates {
		if time.Since(v.CreatedAt) > oauthStateTTL {
			delete(gmailOAuthStates, k)
		}
	}
	gmailOAuthMu.Unlock()
	oc := newGmailOAuth2Config(cfg, needWrite)
	return oc.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce), state, nil
}

// GmailExchanger exchanges a code for tokens (injectable for tests).
type GmailExchanger func(ctx context.Context, cfg GmailOAuthConfig, needWrite bool, code string) (*GmailToken, error)

// GmailValidator checks Gmail API access with the token (injectable).
type GmailValidator func(ctx context.Context, tok *GmailToken) error

func gmailDefaultExchanger(ctx context.Context, cfg GmailOAuthConfig, needWrite bool, code string) (*GmailToken, error) {
	oc := newGmailOAuth2Config(cfg, needWrite)
	tok, err := oc.Exchange(ctx, code)
	if err != nil {
		return nil, classifyGmailOAuthError(err)
	}
	out := &GmailToken{StoredAt: time.Now(), Scope: strings.Join(GmailScopesFor(needWrite), " ")}
	if tok.RefreshToken != "" {
		out.RefreshToken = tok.RefreshToken
	}
	if tok.AccessToken != "" {
		out.AccessToken = tok.AccessToken
	}
	out.Expiry = tok.Expiry
	if out.RefreshToken == "" && out.AccessToken == "" {
		return nil, errors.New("gmail_oauth_empty_token")
	}
	return out, nil
}

// gmailDefaultValidator proves Gmail API access with a minimal profile call.
func gmailDefaultValidator(ctx context.Context, tok *GmailToken) error {
	if tok == nil || (tok.AccessToken == "" && tok.RefreshToken == "") {
		return errors.New("gmail_oauth_no_credential")
	}
	if tok.AccessToken == "" {
		return errors.New("gmail_oauth_needs_refresh")
	}
	req, err := http.NewRequestWithContext(ctx, "GET",
		"https://gmail.googleapis.com/gmail/v1/users/me/profile", nil)
	if err != nil {
		return errors.New("gmail_oauth_validation_failed")
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("gmail_oauth_validation_failed")
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 200:
		return nil
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return errors.New("gmail_oauth_unauthorized")
	default:
		return errors.New("gmail_oauth_validation_failed")
	}
}

func classifyGmailOAuthError(err error) error {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "invalid_grant"):
		return errors.New("gmail_oauth_revoked_or_expired")
	case strings.Contains(msg, "invalid_client"), strings.Contains(msg, "unauthorized_client"):
		return errors.New("gmail_oauth_misconfigured")
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return errors.New("gmail_oauth_timeout")
	default:
		return errors.New("gmail_oauth_exchange_failed")
	}
}

// GmailOAuthComplete validates state (CSRF), exchanges the code, persists
// the credential, validates API access, and returns the pending request ID.
func GmailOAuthComplete(cfg GmailOAuthConfig, state, code string, exch GmailExchanger, valid GmailValidator) (pendingID string, err error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return "", errors.New("gmail_oauth_invalid_callback")
	}
	gmailOAuthMu.Lock()
	st, ok := gmailOAuthStates[state]
	var matched string
	for k := range gmailOAuthStates {
		if subtle.ConstantTimeCompare([]byte(k), []byte(state)) == 1 {
			matched = k
		}
	}
	if ok && matched != "" {
		delete(gmailOAuthStates, matched)
	}
	gmailOAuthMu.Unlock()
	if !ok || matched == "" {
		return "", errors.New("gmail_oauth_bad_state")
	}
	if time.Since(st.CreatedAt) > oauthStateTTL {
		return "", errors.New("gmail_oauth_state_expired")
	}
	if exch == nil {
		exch = gmailDefaultExchanger
	}
	if valid == nil {
		valid = gmailDefaultValidator
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
	if err := storeGmailToken(tok); err != nil {
		return "", errors.New("gmail_oauth_store_failed")
	}
	return st.PendingID, nil
}

func storeGmailToken(tok *GmailToken) error {
	if tok == nil || strings.TrimSpace(tok.RefreshToken) == "" && strings.TrimSpace(tok.AccessToken) == "" {
		return errors.New("gmail_oauth_empty_token")
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
	key, err := config.MasterKeyFor(gmailTokenPath())
	if err != nil {
		return err
	}
	sealed, err := config.Seal(key, data)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "gmail-token.json.tmp")
	if err := os.WriteFile(tmp, sealed, 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, gmailTokenPath())
}

// LoadGmailToken reloads the persisted credential (restart-safe).
// Server-side use only; never serialize to chat/SSE/logs/backups.
func LoadGmailToken() (*GmailToken, error) {
	data, err := os.ReadFile(gmailTokenPath())
	if err != nil {
		return nil, err
	}
	if config.IsSealed(data) {
		key, err := config.MasterKeyFor(gmailTokenPath())
		if err != nil {
			return nil, err
		}
		plain, err := config.Unseal(key, data)
		if err != nil {
			return nil, err
		}
		data = plain
	}
	var tok GmailToken
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

// GmailWebStatus reports product state for the web OAuth path.
func GmailWebStatus() CalendarState {
	tok, err := LoadGmailToken()
	if err != nil || tok == nil {
		return CalendarState{Status: CalendarNeedsSetup, NeedsSetup: true,
			Message: "Your Gmail isn't connected yet. Connect Gmail to continue."}
	}
	if strings.TrimSpace(tok.RefreshToken) == "" && strings.TrimSpace(tok.AccessToken) == "" {
		return CalendarState{Status: CalendarNeedsReauth, NeedsSetup: true,
			Message: "Your Gmail connection needs to be renewed."}
	}
	return CalendarState{Status: CalendarReady, Connected: true, Message: "Gmail is connected."}
}

// GmailWebDisconnect removes the credential (user disconnect flow).
func GmailWebDisconnect() error {
	err := os.Remove(gmailTokenPath())
	if err != nil && !os.IsNotExist(err) {
		return errors.New("gmail_disconnect_failed")
	}
	return nil
}

// GmailDiagnostics is the redacted diagnostics projection.
type GmailDiagnostics struct {
	Configured bool   `json:"configured"`
	Scope      string `json:"scope,omitempty"`
	StoredAt   string `json:"stored_at,omitempty"`
}

// GmailRedactedDiagnostics returns safe diagnostics for logs/UI.
func GmailRedactedDiagnostics() GmailDiagnostics {
	tok, err := LoadGmailToken()
	if err != nil || tok == nil {
		return GmailDiagnostics{Configured: false}
	}
	d := GmailDiagnostics{Configured: true, Scope: tok.Scope}
	if !tok.StoredAt.IsZero() {
		d.StoredAt = tok.StoredAt.Format(time.RFC3339)
	}
	return d
}

// GmailClientConfigFromEnv reads the deployment's shared Google OAuth
// client (same Cloud project as Calendar) plus the Gmail redirect URL.
// Decision: one shared project, separate registered callbacks.
func GmailClientConfigFromEnv() (GmailOAuthConfig, bool) {
	cfg := GmailOAuthConfig{
		ClientID:     strings.TrimSpace(os.Getenv("GHOST_GOOGLE_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("GHOST_GOOGLE_CLIENT_SECRET")),
		RedirectURL:  strings.TrimSpace(os.Getenv("GHOST_GMAIL_REDIRECT_URL")),
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURL == "" {
		return GmailOAuthConfig{}, false
	}
	return cfg, true
}

// GmailAccessToken returns a usable access token for server-side API calls,
// refreshing via the stored refresh token when expired. Never expose the
// result beyond the provider adapter.
func GmailAccessToken(ctx context.Context, needWrite bool) (string, error) {
	tok, err := LoadGmailToken()
	if err != nil || tok == nil {
		return "", errors.New("gmail_oauth_not_connected")
	}
	if strings.TrimSpace(tok.AccessToken) != "" && time.Now().Add(60*time.Second).Before(tok.Expiry) {
		return tok.AccessToken, nil
	}
	cfg, ok := GmailClientConfigFromEnv()
	if !ok {
		return "", errors.New("gmail_oauth_not_configured")
	}
	if strings.TrimSpace(tok.RefreshToken) == "" {
		return "", errors.New("gmail_oauth_needs_reauth")
	}
	refreshed, err := RefreshGmailToken(cfg, needWrite)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(refreshed.AccessToken) == "" {
		return "", errors.New("gmail_oauth_needs_reauth")
	}
	return refreshed.AccessToken, nil
}

// RefreshGmailToken exchanges the stored refresh token for a fresh
// access token using the deployment's OAuth client.
func RefreshGmailToken(cfg GmailOAuthConfig, needWrite bool) (*GmailToken, error) {
	tok, err := LoadGmailToken()
	if err != nil || tok == nil || strings.TrimSpace(tok.RefreshToken) == "" {
		return nil, errors.New("gmail_oauth_no_refresh_token")
	}
	oc := newGmailOAuth2Config(cfg, needWrite)
	src := oc.TokenSource(context.Background(), &oauth2.Token{RefreshToken: tok.RefreshToken})
	nt, err := src.Token()
	if err != nil {
		return nil, classifyGmailOAuthError(err)
	}
	updated := &GmailToken{Scope: tok.Scope, StoredAt: time.Now(), Expiry: nt.Expiry}
	if nt.RefreshToken != "" {
		updated.RefreshToken = nt.RefreshToken
	} else {
		updated.RefreshToken = tok.RefreshToken
	}
	updated.AccessToken = nt.AccessToken
	if err := storeGmailToken(updated); err != nil {
		return nil, errors.New("gmail_oauth_store_failed")
	}
	return updated, nil
}
