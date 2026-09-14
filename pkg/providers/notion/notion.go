package notion

// Notion provider: validate an integration token and search pages the
// integration can see. Secrets come from the vault
// (.secrets.json ProviderAPIKeys[notion] or NOTION_TOKEN env fallback);
// they never reach the model. Only pages shared with the integration are
// searchable — sharing is the access control.

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
)

// Config mirrors the other data providers.
type Config struct {
	HTTPClient      *http.Client
	Base            string
	Token           string
	BreakerCooldown time.Duration
}

func (c Config) withDefaults() Config {
	if c.Base == "" {
		c.Base = "https://api.notion.com/v1"
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if c.BreakerCooldown == 0 {
		c.BreakerCooldown = 60 * time.Second
	}
	return c
}

// Service fronts the Notion API.
type Service struct {
	cfg    Config
	break_ *provider.Breaker
}

func New(cfg Config) *Service {
	cfg = cfg.withDefaults()
	return &Service{cfg: cfg, break_: provider.NewBreaker(3, cfg.BreakerCooldown)}
}

// Configured reports whether an integration token is present.
func (s *Service) Configured() bool {
	return strings.TrimSpace(s.cfg.Token) != ""
}

func (s *Service) authed(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.cfg.Base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.Token)
	req.Header.Set("Notion-Version", "2022-06-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}

// Validate proves the token with a cheap bot-user call.
func (s *Service) Validate(ctx context.Context) provider.Result[string] {
	if !s.Configured() {
		return provider.Result[string]{Failure: provider.FailNotConfigured, Err: fmt.Errorf("notion not connected")}
	}
	req, err := s.authed(ctx, "GET", "/users/me", nil)
	if err != nil {
		return provider.Result[string]{Failure: provider.FailInvalid, Err: err}
	}
	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return provider.Result[string]{Failure: provider.FailNetwork, Err: err}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 16*1024))
	switch {
	case resp.StatusCode == 200:
		return provider.Result[string]{Value: "ok", Provider: "notion"}
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return provider.Result[string]{Failure: provider.FailAuth, Err: fmt.Errorf("notion unauthorized")}
	default:
		return provider.Result[string]{Failure: provider.FailServer, Err: fmt.Errorf("notion status %d", resp.StatusCode)}
	}
}

// DocHit is one redacted search hit.
type DocHit struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	URL   string `json:"url"`
}

type searchResp struct {
	Results []struct {
		ID         string `json:"id"`
		URL        string `json:"url"`
		Properties map[string]struct {
			Title []struct {
				PlainText string `json:"plain_text"`
			} `json:"title"`
		} `json:"properties"`
	} `json:"results"`
}

// Search finds pages shared with the integration matching query.
func (s *Service) Search(ctx context.Context, query string) ([]DocHit, provider.Result[[]DocHit]) {
	if !s.Configured() {
		return nil, provider.Result[[]DocHit]{Failure: provider.FailNotConfigured, Err: fmt.Errorf("notion not connected")}
	}
	if !s.break_.Allow() {
		return nil, provider.Result[[]DocHit]{Failure: provider.FailUnavailable, Err: fmt.Errorf("notion temporarily unavailable")}
	}
	payload, _ := json.Marshal(map[string]interface{}{"query": strings.TrimSpace(query), "page_size": 10})
	req, err := s.authed(ctx, "POST", "/search", bytes.NewReader(payload))
	if err != nil {
		return nil, provider.Result[[]DocHit]{Failure: provider.FailInvalid, Err: err}
	}
	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		s.break_.RecordFailure(provider.FailNetwork)
		return nil, provider.Result[[]DocHit]{Failure: provider.FailNetwork, Err: err}
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 || resp.StatusCode == 403 {
		s.break_.RecordFailure(provider.FailAuth)
		return nil, provider.Result[[]DocHit]{Failure: provider.FailAuth, Err: fmt.Errorf("notion unauthorized")}
	}
	if resp.StatusCode != 200 {
		s.break_.RecordFailure(provider.FailServer)
		return nil, provider.Result[[]DocHit]{Failure: provider.FailServer, Err: fmt.Errorf("notion status %d", resp.StatusCode)}
	}
	var out searchResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, provider.Result[[]DocHit]{Failure: provider.FailMalformed, Err: err}
	}
	hits := make([]DocHit, 0, len(out.Results))
	for _, r := range out.Results {
		title := ""
		for _, p := range r.Properties {
			for _, t := range p.Title {
				title += t.PlainText
			}
		}
		if title == "" {
			title = r.ID
		}
		hits = append(hits, DocHit{ID: r.ID, Title: title, URL: r.URL})
	}
	s.break_.RecordSuccess()
	return hits, provider.Result[[]DocHit]{Value: hits, Provider: "notion"}
}
