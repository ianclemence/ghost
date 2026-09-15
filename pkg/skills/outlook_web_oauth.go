package skills

// Outlook OAuth 2.0 web-server flow (Microsoft identity platform).
//
// Mail + calendar together per head-of-engineering decision: one Microsoft
// app registration covers Mail.Read/Send and Calendars.Read/ReadWrite.
// Security properties mirror Calendar/Gmail exactly:
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

// Outlook OAuth scopes (narrowest-first). Mail + calendar together.
const (
	ScopeOutlookMailRead      = "Mail.Read"
	ScopeOutlookCalendarsRead = "Calendars.Read"
	ScopeOutlookMailSend      = "Mail.Send"
	ScopeOutlookCalendarsRW   = "Calendars.ReadWrite"
)

// OutlookScopesFor returns the narrowest scopes. Readers get Mail.Read +
// Calendars.Read; senders add Mail.Send + Calendars.ReadWrite.
func OutlookScopesFor(needWrite bool) []string {
	if needWrite {
		return []string{ScopeOutlookMailRead, ScopeOutlookMailSend, ScopeOutlookCalendarsRead, ScopeOutlookCalendarsRW}
	}
	return []string{ScopeOutlookMailRead, ScopeOutlookCalendarsRead}
}

// OutlookOAuthConfig is the deployment's Microsoft app registration.
type OutlookOAuthConfig struct {
	ClientID     string
	ClientSecret string
	// RedirectURL is the registered Outlook callback (relay or LAN direct).
	RedirectURL string
	// Tenant selects the Microsoft authority: "common" (default, any
	// account), "consumers" (Outlook.com only), or a tenant GUID.
	Tenant string
}

func outlookEndpoint(tenant string) oauth2.Endpoint {
	tenant = strings.TrimSpace(tenant)
	if tenant == "" {
		tenant = "common"
	}
	return oauth2.Endpoint{
		AuthURL:   "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/authorize",
		TokenURL:  "https://login.microsoftonline.com/" + tenant + "/oauth2/v2.0/token",
		AuthStyle: oauth2.AuthStyleInParams,
	}
}

func newOutlookOAuth2Config(cfg OutlookOAuthConfig, needWrite bool) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Scopes:       OutlookScopesFor(needWrite),
		Endpoint:     outlookEndpoint(cfg.Tenant),
	}
}

// OutlookToken is the persisted credential (0600, outside backups).
type OutlookToken struct {
	RefreshToken string    `json:"refresh_token"`
	AccessToken  string    `json:"access_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	StoredAt     time.Time `json:"stored_at"`
}

var (
	outlookOAuthMu     sync.Mutex
	outlookOAuthStates = map[string]oauthState{}
)

func outlookTokenPath() string {
	return filepath.Join(CalendarTokenDir(), "outlook-token.json")
}

// OutlookOAuthBegin starts authorization: returns the Microsoft consent URL.
func OutlookOAuthBegin(cfg OutlookOAuthConfig, sessionID, pendingID string, needWrite bool) (authURL, state string, err error) {
	if strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.RedirectURL) == "" {
		return "", "", errors.New("outlook_oauth_not_configured")
	}
	if strings.TrimSpace(sessionID) == "" {
		return "", "", errors.New("outlook_oauth_session_required")
	}
	state, err = newOAuthState()
	if err != nil {
		return "", "", errors.New("outlook_oauth_state_failed")
	}
	outlookOAuthMu.Lock()
	outlookOAuthStates[state] = oauthState{State: state, SessionID: sessionID, PendingID: pendingID, NeedWrite: needWrite, CreatedAt: time.Now()}
	for k, v := range outlookOAuthStates {
		if time.Since(v.CreatedAt) > oauthStateTTL {
			delete(outlookOAuthStates, k)
		}
	}
	outlookOAuthMu.Unlock()
	oc := newOutlookOAuth2Config(cfg, needWrite)
	return oc.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.ApprovalForce), state, nil
}

// OutlookExchanger exchanges a code for tokens (injectable for tests).
type OutlookExchanger func(ctx context.Context, cfg OutlookOAuthConfig, needWrite bool, code string) (*OutlookToken, error)

// OutlookValidator checks Graph API access with the token (injectable).
type OutlookValidator func(ctx context.Context, tok *OutlookToken) error

func outlookDefaultExchanger(ctx context.Context, cfg OutlookOAuthConfig, needWrite bool, code string) (*OutlookToken, error) {
	oc := newOutlookOAuth2Config(cfg, needWrite)
	tok, err := oc.Exchange(ctx, code)
	if err != nil {
		return nil, classifyOutlookOAuthError(err)
	}
	out := &OutlookToken{StoredAt: time.Now(), Scope: strings.Join(OutlookScopesFor(needWrite), " ")}
	if tok.RefreshToken != "" {
		out.RefreshToken = tok.RefreshToken
	}
	if tok.AccessToken != "" {
		out.AccessToken = tok.AccessToken
	}
	out.Expiry = tok.Expiry
	if out.RefreshToken == "" && out.AccessToken == "" {
		return nil, errors.New("outlook_oauth_empty_token")
	}
	return out, nil
}

// outlookDefaultValidator proves Graph API access with a minimal call.
func outlookDefaultValidator(ctx context.Context, tok *OutlookToken) error {
	if tok == nil || (tok.AccessToken == "" && tok.RefreshToken == "") {
		return errors.New("outlook_oauth_no_credential")
	}
	if tok.AccessToken == "" {
		return errors.New("outlook_oauth_needs_refresh")
	}
	req, err := http.NewRequestWithContext(ctx, "GET",
		"https://graph.microsoft.com/v1.0/me/messages?$top=1&$select=id,subject", nil)
	if err != nil {
		return errors.New("outlook_oauth_validation_failed")
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("outlook_oauth_validation_failed")
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 200:
		return nil
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return errors.New("outlook_oauth_unauthorized")
	default:
		return errors.New("outlook_oauth_validation_failed")
	}
}

func classifyOutlookOAuthError(err error) error {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "invalid_grant"):
		return errors.New("outlook_oauth_revoked_or_expired")
	case strings.Contains(msg, "invalid_client"), strings.Contains(msg, "unauthorized_client"):
		return errors.New("outlook_oauth_misconfigured")
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return errors.New("outlook_oauth_timeout")
	default:
		return errors.New("outlook_oauth_exchange_failed")
	}
}

// OutlookOAuthComplete validates state (CSRF), exchanges the code, persists
// the credential, validates API access, and returns the pending request ID.
func OutlookOAuthComplete(cfg OutlookOAuthConfig, state, code string, exch OutlookExchanger, valid OutlookValidator) (pendingID string, err error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return "", errors.New("outlook_oauth_invalid_callback")
	}
	outlookOAuthMu.Lock()
	st, ok := outlookOAuthStates[state]
	var matched string
	for k := range outlookOAuthStates {
		if subtle.ConstantTimeCompare([]byte(k), []byte(state)) == 1 {
			matched = k
		}
	}
	if ok && matched != "" {
		delete(outlookOAuthStates, matched)
	}
	outlookOAuthMu.Unlock()
	if !ok || matched == "" {
		return "", errors.New("outlook_oauth_bad_state")
	}
	if time.Since(st.CreatedAt) > oauthStateTTL {
		return "", errors.New("outlook_oauth_state_expired")
	}
	if exch == nil {
		exch = outlookDefaultExchanger
	}
	if valid == nil {
		valid = outlookDefaultValidator
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
	if err := storeOutlookToken(tok); err != nil {
		return "", errors.New("outlook_oauth_store_failed")
	}
	return st.PendingID, nil
}

func storeOutlookToken(tok *OutlookToken) error {
	if tok == nil || strings.TrimSpace(tok.RefreshToken) == "" && strings.TrimSpace(tok.AccessToken) == "" {
		return errors.New("outlook_oauth_empty_token")
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
	key, err := config.MasterKeyFor(outlookTokenPath())
	if err != nil {
		return err
	}
	sealed, err := config.Seal(key, data)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "outlook-token.json.tmp")
	if err := os.WriteFile(tmp, sealed, 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, outlookTokenPath())
}

// LoadOutlookToken reloads the persisted credential (restart-safe).
// Server-side use only; never serialize to chat/SSE/logs/backups.
func LoadOutlookToken() (*OutlookToken, error) {
	data, err := os.ReadFile(outlookTokenPath())
	if err != nil {
		return nil, err
	}
	if config.IsSealed(data) {
		// Read-only: loading must never mint keys.
		key, err := config.ResolveMasterKey(outlookTokenPath())
		if err != nil {
			return nil, err
		}
		plain, err := config.Unseal(key, data)
		if err != nil {
			return nil, err
		}
		data = plain
	}
	var tok OutlookToken
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

// OutlookWebStatus reports product state for the web OAuth path.
func OutlookWebStatus() CalendarState {
	tok, err := LoadOutlookToken()
	if err != nil || tok == nil {
		return CalendarState{Status: CalendarNeedsSetup, NeedsSetup: true,
			Message: "Your Outlook isn't connected yet. Connect Outlook to continue."}
	}
	if strings.TrimSpace(tok.RefreshToken) == "" && strings.TrimSpace(tok.AccessToken) == "" {
		return CalendarState{Status: CalendarNeedsReauth, NeedsSetup: true,
			Message: "Your Outlook connection needs to be renewed."}
	}
	return CalendarState{Status: CalendarReady, Connected: true, Message: "Outlook is connected."}
}

// OutlookWebDisconnect removes the credential (user disconnect flow).
func OutlookWebDisconnect() error {
	err := os.Remove(outlookTokenPath())
	if err != nil && !os.IsNotExist(err) {
		return errors.New("outlook_disconnect_failed")
	}
	return nil
}

// OutlookClientConfigFromEnv reads the deployment's Microsoft app
// registration plus the Outlook redirect URL.
func OutlookClientConfigFromEnv() (OutlookOAuthConfig, bool) {
	cfg := OutlookOAuthConfig{
		ClientID:     strings.TrimSpace(os.Getenv("GHOST_OUTLOOK_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("GHOST_OUTLOOK_CLIENT_SECRET")),
		RedirectURL:  strings.TrimSpace(os.Getenv("GHOST_OUTLOOK_REDIRECT_URL")),
		Tenant:       strings.TrimSpace(os.Getenv("GHOST_OUTLOOK_TENANT")),
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURL == "" {
		return OutlookOAuthConfig{}, false
	}
	return cfg, true
}

// OutlookAccessToken returns a usable access token for server-side API
// calls, refreshing when expired. Never expose beyond the adapter.
func OutlookAccessToken(ctx context.Context, needWrite bool) (string, error) {
	tok, err := LoadOutlookToken()
	if err != nil || tok == nil {
		return "", errors.New("outlook_oauth_not_connected")
	}
	if strings.TrimSpace(tok.AccessToken) != "" && time.Now().Add(60*time.Second).Before(tok.Expiry) {
		return tok.AccessToken, nil
	}
	cfg, ok := OutlookClientConfigFromEnv()
	if !ok {
		return "", errors.New("outlook_oauth_not_configured")
	}
	if strings.TrimSpace(tok.RefreshToken) == "" {
		return "", errors.New("outlook_oauth_needs_reauth")
	}
	refreshed, err := RefreshOutlookToken(cfg, needWrite)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(refreshed.AccessToken) == "" {
		return "", errors.New("outlook_oauth_needs_reauth")
	}
	return refreshed.AccessToken, nil
}

// RefreshOutlookToken exchanges the stored refresh token for a fresh one.
func RefreshOutlookToken(cfg OutlookOAuthConfig, needWrite bool) (*OutlookToken, error) {
	tok, err := LoadOutlookToken()
	if err != nil || tok == nil || strings.TrimSpace(tok.RefreshToken) == "" {
		return nil, errors.New("outlook_oauth_no_refresh_token")
	}
	oc := newOutlookOAuth2Config(cfg, needWrite)
	src := oc.TokenSource(context.Background(), &oauth2.Token{RefreshToken: tok.RefreshToken})
	nt, err := src.Token()
	if err != nil {
		return nil, classifyOutlookOAuthError(err)
	}
	updated := &OutlookToken{Scope: tok.Scope, StoredAt: time.Now(), Expiry: nt.Expiry}
	if nt.RefreshToken != "" {
		updated.RefreshToken = nt.RefreshToken
	} else {
		updated.RefreshToken = tok.RefreshToken
	}
	updated.AccessToken = nt.AccessToken
	if err := storeOutlookToken(updated); err != nil {
		return nil, errors.New("outlook_oauth_store_failed")
	}
	return updated, nil
}
