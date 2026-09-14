package github

// GitHub provider: validate a personal access token and search code via
// the REST API. Trust-user model per head-of-engineering decision: Ghost
// documents read-only scopes (repo:read) but does not enforce them — the
// token's own scopes are the authority. Secrets come from the vault
// (.secrets.json ProviderAPIKeys[github] or GITHUB_TOKEN env fallback);
// they never reach the model.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/provider"
)

// Config mirrors the other data providers.
type Config struct {
	HTTPClient      *http.Client
	Base            string
	PAT             string
	BreakerCooldown time.Duration
}

func (c Config) withDefaults() Config {
	if c.Base == "" {
		c.Base = "https://api.github.com"
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if c.BreakerCooldown == 0 {
		c.BreakerCooldown = 60 * time.Second
	}
	return c
}

// Service fronts the GitHub REST API.
type Service struct {
	cfg    Config
	break_ *provider.Breaker
}

func New(cfg Config) *Service {
	cfg = cfg.withDefaults()
	return &Service{cfg: cfg, break_: provider.NewBreaker(3, cfg.BreakerCooldown)}
}

// Configured reports whether a PAT is present.
func (s *Service) Configured() bool {
	return strings.TrimSpace(s.cfg.PAT) != ""
}

func (s *Service) authed(ctx context.Context, method, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.cfg.Base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "token "+s.cfg.PAT)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	return req, nil
}

// Validate proves the PAT with a cheap rate_limit call.
func (s *Service) Validate(ctx context.Context) provider.Result[string] {
	if !s.Configured() {
		return provider.Result[string]{Failure: provider.FailNotConfigured, Err: fmt.Errorf("github not connected")}
	}
	req, err := s.authed(ctx, "GET", "/rate_limit")
	if err != nil {
		return provider.Result[string]{Failure: provider.FailInvalid, Err: err}
	}
	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return provider.Result[string]{Failure: provider.FailNetwork, Err: err}
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == 200:
		return provider.Result[string]{Value: "ok", Provider: "github"}
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return provider.Result[string]{Failure: provider.FailAuth, Err: fmt.Errorf("github unauthorized")}
	default:
		return provider.Result[string]{Failure: provider.FailServer, Err: fmt.Errorf("github status %d", resp.StatusCode)}
	}
}

// CodeHit is one redacted search hit.
type CodeHit struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
	URL  string `json:"url"`
}

type codeSearchResp struct {
	Items []struct {
		Name       string `json:"name"`
		Path       string `json:"path"`
		HTMLURL    string `json:"html_url"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	} `json:"items"`
}

// SearchCode searches code across repositories the token can see.
// repo constrains to one repo (owner/name) when non-empty.
func (s *Service) SearchCode(ctx context.Context, query, repo string) ([]CodeHit, provider.Result[[]CodeHit]) {
	if !s.Configured() {
		return nil, provider.Result[[]CodeHit]{Failure: provider.FailNotConfigured, Err: fmt.Errorf("github not connected")}
	}
	q := strings.TrimSpace(query)
	if repo != "" {
		q += " repo:" + strings.TrimSpace(repo)
	}
	if !s.break_.Allow() {
		return nil, provider.Result[[]CodeHit]{Failure: provider.FailUnavailable, Err: fmt.Errorf("github temporarily unavailable")}
	}
	req, err := s.authed(ctx, "GET", "/search/code?q="+url.QueryEscape(q)+"&per_page=10")
	if err != nil {
		return nil, provider.Result[[]CodeHit]{Failure: provider.FailInvalid, Err: err}
	}
	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		s.break_.RecordFailure(provider.FailNetwork)
		return nil, provider.Result[[]CodeHit]{Failure: provider.FailNetwork, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		s.break_.RecordFailure(provider.FailAuth)
		return nil, provider.Result[[]CodeHit]{Failure: provider.FailAuth, Err: fmt.Errorf("github unauthorized")}
	}
	if resp.StatusCode != 200 {
		s.break_.RecordFailure(provider.FailServer)
		return nil, provider.Result[[]CodeHit]{Failure: provider.FailServer, Err: fmt.Errorf("github status %d", resp.StatusCode)}
	}
	var out codeSearchResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, provider.Result[[]CodeHit]{Failure: provider.FailMalformed, Err: err}
	}
	hits := make([]CodeHit, 0, len(out.Items))
	for _, it := range out.Items {
		hits = append(hits, CodeHit{Repo: it.Repository.FullName, Path: it.Path, URL: it.HTMLURL})
	}
	s.break_.RecordSuccess()
	return hits, provider.Result[[]CodeHit]{Value: hits, Provider: "github"}
}

type contentsResp struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

// ReadFile fetches a file's content (base64-decoded, truncated) for
// code_read follow-ups.
func (s *Service) ReadFile(ctx context.Context, repo, path string) (string, provider.Result[string]) {
	if !s.Configured() {
		return "", provider.Result[string]{Failure: provider.FailNotConfigured, Err: fmt.Errorf("github not connected")}
	}
	req, err := s.authed(ctx, "GET", "/repos/"+repo+"/contents/"+strings.TrimPrefix(path, "/"))
	if err != nil {
		return "", provider.Result[string]{Failure: provider.FailInvalid, Err: err}
	}
	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return "", provider.Result[string]{Failure: provider.FailNetwork, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		return "", provider.Result[string]{Failure: provider.FailAuth, Err: fmt.Errorf("github unauthorized")}
	}
	if resp.StatusCode != 200 {
		return "", provider.Result[string]{Failure: provider.FailServer, Err: fmt.Errorf("github status %d", resp.StatusCode)}
	}
	var out contentsResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", provider.Result[string]{Failure: provider.FailMalformed, Err: err}
	}
	if out.Encoding != "base64" {
		return "", provider.Result[string]{Failure: provider.FailInvalid, Err: fmt.Errorf("unexpected github encoding")}
	}
	raw, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(out.Content, "\n", ""))
	if err != nil {
		return "", provider.Result[string]{Failure: provider.FailMalformed, Err: err}
	}
	if len(raw) > 32*1024 {
		raw = raw[:32*1024]
	}
	return string(raw), provider.Result[string]{Value: string(raw), Provider: "github"}
}
