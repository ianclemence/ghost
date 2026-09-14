package skills

// Spotify OAuth 2.0 authorization-code flow.
//
// Read-only status first (user-read-playback-state, user-read-currently-
// playing); playback control adds user-modify-playback-state. Security
// properties mirror Calendar/Gmail/Outlook exactly:
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

// Spotify OAuth scopes (narrowest-first).
const (
	ScopeSpotifyReadPlayback = "user-read-playback-state"
	ScopeSpotifyReadCurrent  = "user-read-currently-playing"
	ScopeSpotifyModify       = "user-modify-playback-state"
)

// SpotifyScopesFor returns the narrowest scopes. Status readers get
// read-only; playback control adds modify.
func SpotifyScopesFor(needWrite bool) []string {
	if needWrite {
		return []string{ScopeSpotifyReadPlayback, ScopeSpotifyReadCurrent, ScopeSpotifyModify}
	}
	return []string{ScopeSpotifyReadPlayback, ScopeSpotifyReadCurrent}
}

// SpotifyOAuthConfig is the deployment's Spotify app registration.
type SpotifyOAuthConfig struct {
	ClientID     string
	ClientSecret string
	// RedirectURL is the registered Spotify callback (relay or LAN direct).
	RedirectURL string
}

func spotifyEndpoint() oauth2.Endpoint {
	return oauth2.Endpoint{
		AuthURL:   "https://accounts.spotify.com/authorize",
		TokenURL:  "https://accounts.spotify.com/api/token",
		AuthStyle: oauth2.AuthStyleInParams,
	}
}

func newSpotifyOAuth2Config(cfg SpotifyOAuthConfig, needWrite bool) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		RedirectURL:  cfg.RedirectURL,
		Scopes:       SpotifyScopesFor(needWrite),
		Endpoint:     spotifyEndpoint(),
	}
}

// SpotifyToken is the persisted credential (0600, outside backups).
type SpotifyToken struct {
	RefreshToken string    `json:"refresh_token"`
	AccessToken  string    `json:"access_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	StoredAt     time.Time `json:"stored_at"`
}

var (
	spotifyOAuthMu     sync.Mutex
	spotifyOAuthStates = map[string]oauthState{}
)

func spotifyTokenPath() string {
	return filepath.Join(CalendarTokenDir(), "spotify-token.json")
}

// SpotifyOAuthBegin starts authorization: returns the Spotify consent URL.
func SpotifyOAuthBegin(cfg SpotifyOAuthConfig, sessionID, pendingID string, needWrite bool) (authURL, state string, err error) {
	if strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.RedirectURL) == "" {
		return "", "", errors.New("spotify_oauth_not_configured")
	}
	if strings.TrimSpace(sessionID) == "" {
		return "", "", errors.New("spotify_oauth_session_required")
	}
	state, err = newOAuthState()
	if err != nil {
		return "", "", errors.New("spotify_oauth_state_failed")
	}
	spotifyOAuthMu.Lock()
	spotifyOAuthStates[state] = oauthState{State: state, SessionID: sessionID, PendingID: pendingID, NeedWrite: needWrite, CreatedAt: time.Now()}
	for k, v := range spotifyOAuthStates {
		if time.Since(v.CreatedAt) > oauthStateTTL {
			delete(spotifyOAuthStates, k)
		}
	}
	spotifyOAuthMu.Unlock()
	oc := newSpotifyOAuth2Config(cfg, needWrite)
	return oc.AuthCodeURL(state), state, nil
}

// SpotifyExchanger exchanges a code for tokens (injectable for tests).
type SpotifyExchanger func(ctx context.Context, cfg SpotifyOAuthConfig, needWrite bool, code string) (*SpotifyToken, error)

// SpotifyValidator checks Spotify API access with the token (injectable).
type SpotifyValidator func(ctx context.Context, tok *SpotifyToken) error

func spotifyDefaultExchanger(ctx context.Context, cfg SpotifyOAuthConfig, needWrite bool, code string) (*SpotifyToken, error) {
	oc := newSpotifyOAuth2Config(cfg, needWrite)
	tok, err := oc.Exchange(ctx, code)
	if err != nil {
		return nil, classifySpotifyOAuthError(err)
	}
	out := &SpotifyToken{StoredAt: time.Now(), Scope: strings.Join(SpotifyScopesFor(needWrite), " ")}
	if tok.RefreshToken != "" {
		out.RefreshToken = tok.RefreshToken
	}
	if tok.AccessToken != "" {
		out.AccessToken = tok.AccessToken
	}
	out.Expiry = tok.Expiry
	if out.RefreshToken == "" && out.AccessToken == "" {
		return nil, errors.New("spotify_oauth_empty_token")
	}
	return out, nil
}

// spotifyDefaultValidator proves Spotify API access with a minimal call.
func spotifyDefaultValidator(ctx context.Context, tok *SpotifyToken) error {
	if tok == nil || (tok.AccessToken == "" && tok.RefreshToken == "") {
		return errors.New("spotify_oauth_no_credential")
	}
	if tok.AccessToken == "" {
		return errors.New("spotify_oauth_needs_refresh")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.spotify.com/v1/me", nil)
	if err != nil {
		return errors.New("spotify_oauth_validation_failed")
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errors.New("spotify_oauth_validation_failed")
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 200:
		return nil
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return errors.New("spotify_oauth_unauthorized")
	default:
		return errors.New("spotify_oauth_validation_failed")
	}
}

func classifySpotifyOAuthError(err error) error {
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "invalid_grant"):
		return errors.New("spotify_oauth_revoked_or_expired")
	case strings.Contains(msg, "invalid_client"), strings.Contains(msg, "unauthorized_client"):
		return errors.New("spotify_oauth_misconfigured")
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline"):
		return errors.New("spotify_oauth_timeout")
	default:
		return errors.New("spotify_oauth_exchange_failed")
	}
}

// SpotifyOAuthComplete validates state (CSRF), exchanges the code, persists
// the credential, validates API access, and returns the pending request ID.
func SpotifyOAuthComplete(cfg SpotifyOAuthConfig, state, code string, exch SpotifyExchanger, valid SpotifyValidator) (pendingID string, err error) {
	if strings.TrimSpace(state) == "" || strings.TrimSpace(code) == "" {
		return "", errors.New("spotify_oauth_invalid_callback")
	}
	spotifyOAuthMu.Lock()
	st, ok := spotifyOAuthStates[state]
	var matched string
	for k := range spotifyOAuthStates {
		if subtle.ConstantTimeCompare([]byte(k), []byte(state)) == 1 {
			matched = k
		}
	}
	if ok && matched != "" {
		delete(spotifyOAuthStates, matched)
	}
	spotifyOAuthMu.Unlock()
	if !ok || matched == "" {
		return "", errors.New("spotify_oauth_bad_state")
	}
	if time.Since(st.CreatedAt) > oauthStateTTL {
		return "", errors.New("spotify_oauth_state_expired")
	}
	if exch == nil {
		exch = spotifyDefaultExchanger
	}
	if valid == nil {
		valid = spotifyDefaultValidator
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
	if err := storeSpotifyToken(tok); err != nil {
		return "", errors.New("spotify_oauth_store_failed")
	}
	return st.PendingID, nil
}

func storeSpotifyToken(tok *SpotifyToken) error {
	if tok == nil || strings.TrimSpace(tok.RefreshToken) == "" && strings.TrimSpace(tok.AccessToken) == "" {
		return errors.New("spotify_oauth_empty_token")
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
	key, err := config.MasterKeyFor(spotifyTokenPath())
	if err != nil {
		return err
	}
	sealed, err := config.Seal(key, data)
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "spotify-token.json.tmp")
	if err := os.WriteFile(tmp, sealed, 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, spotifyTokenPath())
}

// LoadSpotifyToken reloads the persisted credential (restart-safe).
// Server-side use only; never serialize to chat/SSE/logs/backups.
func LoadSpotifyToken() (*SpotifyToken, error) {
	data, err := os.ReadFile(spotifyTokenPath())
	if err != nil {
		return nil, err
	}
	if config.IsSealed(data) {
		key, err := config.MasterKeyFor(spotifyTokenPath())
		if err != nil {
			return nil, err
		}
		plain, err := config.Unseal(key, data)
		if err != nil {
			return nil, err
		}
		data = plain
	}
	var tok SpotifyToken
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

// SpotifyWebStatus reports product state for the web OAuth path.
func SpotifyWebStatus() CalendarState {
	tok, err := LoadSpotifyToken()
	if err != nil || tok == nil {
		return CalendarState{Status: CalendarNeedsSetup, NeedsSetup: true,
			Message: "Your Spotify isn't connected yet. Connect Spotify to continue."}
	}
	if strings.TrimSpace(tok.RefreshToken) == "" && strings.TrimSpace(tok.AccessToken) == "" {
		return CalendarState{Status: CalendarNeedsReauth, NeedsSetup: true,
			Message: "Your Spotify connection needs to be renewed."}
	}
	return CalendarState{Status: CalendarReady, Connected: true, Message: "Spotify is connected."}
}

// SpotifyWebDisconnect removes the credential (user disconnect flow).
func SpotifyWebDisconnect() error {
	err := os.Remove(spotifyTokenPath())
	if err != nil && !os.IsNotExist(err) {
		return errors.New("spotify_disconnect_failed")
	}
	return nil
}

// SpotifyClientConfigFromEnv reads the deployment's Spotify app registration.
func SpotifyClientConfigFromEnv() (SpotifyOAuthConfig, bool) {
	cfg := SpotifyOAuthConfig{
		ClientID:     strings.TrimSpace(os.Getenv("GHOST_SPOTIFY_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("GHOST_SPOTIFY_CLIENT_SECRET")),
		RedirectURL:  strings.TrimSpace(os.Getenv("GHOST_SPOTIFY_REDIRECT_URL")),
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURL == "" {
		return SpotifyOAuthConfig{}, false
	}
	return cfg, true
}

// SpotifyAccessToken returns a usable access token for server-side API
// calls, refreshing when expired. Never expose beyond the adapter.
func SpotifyAccessToken(ctx context.Context, needWrite bool) (string, error) {
	tok, err := LoadSpotifyToken()
	if err != nil || tok == nil {
		return "", errors.New("spotify_oauth_not_connected")
	}
	if strings.TrimSpace(tok.AccessToken) != "" && time.Now().Add(60*time.Second).Before(tok.Expiry) {
		return tok.AccessToken, nil
	}
	cfg, ok := SpotifyClientConfigFromEnv()
	if !ok {
		return "", errors.New("spotify_oauth_not_configured")
	}
	if strings.TrimSpace(tok.RefreshToken) == "" {
		return "", errors.New("spotify_oauth_needs_reauth")
	}
	refreshed, err := RefreshSpotifyToken(cfg, needWrite)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(refreshed.AccessToken) == "" {
		return "", errors.New("spotify_oauth_needs_reauth")
	}
	return refreshed.AccessToken, nil
}

// RefreshSpotifyToken exchanges the stored refresh token for a fresh one.
func RefreshSpotifyToken(cfg SpotifyOAuthConfig, needWrite bool) (*SpotifyToken, error) {
	tok, err := LoadSpotifyToken()
	if err != nil || tok == nil || strings.TrimSpace(tok.RefreshToken) == "" {
		return nil, errors.New("spotify_oauth_no_refresh_token")
	}
	oc := newSpotifyOAuth2Config(cfg, needWrite)
	src := oc.TokenSource(context.Background(), &oauth2.Token{RefreshToken: tok.RefreshToken})
	nt, err := src.Token()
	if err != nil {
		return nil, classifySpotifyOAuthError(err)
	}
	updated := &SpotifyToken{Scope: tok.Scope, StoredAt: time.Now(), Expiry: nt.Expiry}
	if nt.RefreshToken != "" {
		updated.RefreshToken = nt.RefreshToken
	} else {
		updated.RefreshToken = tok.RefreshToken
	}
	updated.AccessToken = nt.AccessToken
	if err := storeSpotifyToken(updated); err != nil {
		return nil, errors.New("spotify_oauth_store_failed")
	}
	return updated, nil
}
