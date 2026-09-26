package spotify

// Spotify Web API provider: playback status and control on the user's
// active device. Auth is OAuth via pkg/skills (sealed refresh token);
// secrets never reach the model. Read-only status needs no approval;
// play/pause/next go through the broker like any low-risk action.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/provider"
	"github.com/ianclemence/ghost/pkg/skills"
)

// Config mirrors the other data providers.
type Config struct {
	HTTPClient      *http.Client
	Base            string
	BreakerCooldown time.Duration
}

func (c Config) withDefaults() Config {
	if c.Base == "" {
		c.Base = "https://api.spotify.com/v1"
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if c.BreakerCooldown == 0 {
		c.BreakerCooldown = 60 * time.Second
	}
	return c
}

// Service fronts the Spotify player API.
type Service struct {
	cfg    Config
	break_ *provider.Breaker
}

func New(cfg Config) *Service {
	cfg = cfg.withDefaults()
	return &Service{cfg: cfg, break_: provider.NewBreaker(3, cfg.BreakerCooldown)}
}

// Configured reports whether Spotify OAuth is connected.
func (s *Service) Configured() bool {
	return skills.SpotifyWebStatus().Connected
}

// Status is the redacted now-playing projection.
type Status struct {
	Playing  bool   `json:"playing"`
	Track    string `json:"track,omitempty"`
	Artist   string `json:"artist,omitempty"`
	Device   string `json:"device,omitempty"`
	Progress string `json:"progress,omitempty"`
}

type playerResp struct {
	IsPlaying  bool `json:"is_playing"`
	ProgressMs *int `json:"progress_ms"`
	Device     struct {
		Name string `json:"name"`
	} `json:"device"`
	Item struct {
		Name    string `json:"name"`
		Artists []struct {
			Name string `json:"name"`
		} `json:"artists"`
		DurationMs int `json:"duration_ms"`
	} `json:"item"`
}

func (s *Service) call(ctx context.Context, access, method, path string, body io.Reader) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.cfg.Base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return s.cfg.HTTPClient.Do(req)
}

func checkAuth(resp *http.Response) provider.FailureClass {
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return provider.FailAuth
	}
	if resp.StatusCode == 404 {
		return provider.FailInvalid
	}
	if resp.StatusCode >= 500 {
		return provider.FailServer
	}
	return ""
}

// NowPlaying reports what's on the active device (or empty when idle).
func (s *Service) NowPlaying(ctx context.Context) (Status, provider.Result[Status]) {
	if !s.Configured() {
		return Status{}, provider.Result[Status]{Failure: provider.FailNotConfigured, Err: fmt.Errorf("spotify not connected")}
	}
	access, err := skills.SpotifyAccessToken(ctx, false)
	if err != nil {
		return Status{}, provider.Result[Status]{Failure: provider.FailAuth, Err: err}
	}
	if !s.break_.Allow() {
		return Status{}, provider.Result[Status]{Failure: provider.FailUnavailable, Err: fmt.Errorf("spotify temporarily unavailable")}
	}
	resp, err := s.call(ctx, access, "GET", "/me/player", nil)
	if err != nil {
		s.break_.RecordFailure(provider.FailNetwork)
		return Status{}, provider.Result[Status]{Failure: provider.FailNetwork, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode == 204 {
		s.break_.RecordSuccess()
		return Status{}, provider.Result[Status]{Value: Status{}, Provider: "spotify"}
	}
	if fc := checkAuth(resp); fc != "" {
		s.break_.RecordFailure(fc)
		return Status{}, provider.Result[Status]{Failure: fc, Err: fmt.Errorf("spotify status %d", resp.StatusCode)}
	}
	if resp.StatusCode != 200 {
		s.break_.RecordFailure(provider.FailServer)
		return Status{}, provider.Result[Status]{Failure: provider.FailServer, Err: fmt.Errorf("spotify status %d", resp.StatusCode)}
	}
	var p playerResp
	if err := json.NewDecoder(resp.Body).Decode(&p); err != nil {
		return Status{}, provider.Result[Status]{Failure: provider.FailMalformed, Err: err}
	}
	artists := make([]string, 0, len(p.Item.Artists))
	for _, a := range p.Item.Artists {
		artists = append(artists, a.Name)
	}
	st := Status{Playing: p.IsPlaying, Track: p.Item.Name, Artist: strings.Join(artists, ", "), Device: p.Device.Name}
	s.break_.RecordSuccess()
	return st, provider.Result[Status]{Value: st, Provider: "spotify"}
}

// Control performs play/pause/next/previous on the active device.
// query may carry a Spotify context URI or search-coerced URI payload.
func (s *Service) Control(ctx context.Context, action, contextURI string) provider.Result[string] {
	if !s.Configured() {
		return provider.Result[string]{Failure: provider.FailNotConfigured, Err: fmt.Errorf("spotify not connected")}
	}
	access, err := skills.SpotifyAccessToken(ctx, true)
	if err != nil {
		return provider.Result[string]{Failure: provider.FailAuth, Err: err}
	}
	var method, path string
	var payload io.Reader
	switch action {
	case "play":
		method, path = "PUT", "/me/player/play"
		if contextURI != "" {
			data, _ := json.Marshal(map[string]string{"context_uri": contextURI})
			payload = bytes.NewReader(data)
		}
	case "pause":
		method, path = "PUT", "/me/player/pause"
	case "next":
		method, path = "POST", "/me/player/next"
	case "previous":
		method, path = "POST", "/me/player/previous"
	default:
		return provider.Result[string]{Failure: provider.FailInvalid, Err: fmt.Errorf("unknown playback action %q", action)}
	}
	resp, err := s.call(ctx, access, method, path, payload)
	if err != nil {
		return provider.Result[string]{Failure: provider.FailNetwork, Err: err}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 16*1024))
	if fc := checkAuth(resp); fc != "" {
		return provider.Result[string]{Failure: fc, Err: fmt.Errorf("spotify status %d", resp.StatusCode)}
	}
	if resp.StatusCode != 200 && resp.StatusCode != 204 {
		return provider.Result[string]{Failure: provider.FailServer, Err: fmt.Errorf("spotify status %d", resp.StatusCode)}
	}
	return provider.Result[string]{Value: action, Provider: "spotify"}
}
