// Package hass is the Home Assistant device-class capability: the
// FIRST proof that Ghost → Device → Capability → Permission → Action →
// Event → Activity composes end to end.
//
// Provider decision: the Home Assistant REST API itself (single vendor
// by definition — it IS the user's device). No second provider exists
// or is needed; resilience comes from bounded retry + breaker +
// validation + honest states. Reads (states) are low-risk; actuation
// (turn_on/turn_off) is consequential and gates on the broker.
//
// Credentials come from the vault path (hass_url/hass_token in
// secrets, never chat/logs). The device must be registered in the
// trust lattice (paired+) with declared capabilities; the model never
// invents device identifiers.
package hass

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/provider"
	"github.com/ianclemence/ghost/pkg/providers/httpx"
)

// Entity is one validated HA state object.
type Entity struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Name  string `json:"name,omitempty"`
}

// Validate rejects empty ids.
func (e Entity) Validate() error {
	if strings.TrimSpace(e.ID) == "" {
		return provider.Invalid("entity without id")
	}
	return nil
}

// Config wires the device (overridable for tests).
type Config struct {
	HTTPClient *http.Client
	// Base is the HA URL, e.g. http://homeassistant.local:8123.
	Base string
	// Token is the long-lived access token (server-side only).
	Token string
	// BreakerCooldown defaults to 60s.
	BreakerCooldown time.Duration
}

func (c Config) withDefaults() Config {
	out := c
	if out.HTTPClient == nil {
		out.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	out.Base = strings.TrimRight(out.Base, "/")
	if out.BreakerCooldown <= 0 {
		out.BreakerCooldown = 60 * time.Second
	}
	return out
}

// Configured reports whether the device can be reached at all.
func (c Config) Configured() bool {
	return strings.TrimSpace(c.Base) != "" && strings.TrimSpace(c.Token) != ""
}

// Service is the HASS capability.
type Service struct {
	cfg     Config
	breaker *provider.Breaker
}

// New creates the service.
func New(cfg Config) *Service {
	cfg = cfg.withDefaults()
	return &Service{cfg: cfg, breaker: provider.NewBreaker(3, cfg.BreakerCooldown)}
}

func (s *Service) headers() map[string]string {
	return map[string]string{"Authorization": "Bearer " + s.cfg.Token}
}

// Reachable proves the device answers (GET /api/ — 200/401 distinguish
// reachable-unauthorized from unreachable).
func (s *Service) Reachable(ctx context.Context) (provider.FailureClass, error) {
	if !s.cfg.Configured() {
		return provider.FailNotConfigured, fmt.Errorf("home assistant not connected")
	}
	if s.breaker != nil && !s.breaker.Allow() {
		return provider.FailUnavailable, fmt.Errorf("home assistant cooling down")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", s.cfg.Base+"/api/", nil)
	if err != nil {
		return provider.FailInvalid, err
	}
	for k, v := range s.headers() {
		req.Header.Set(k, v)
	}
	resp, err := s.cfg.HTTPClient.Do(req)
	if err != nil {
		return provider.FailNetwork, err
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case 200, 201:
		s.breaker.RecordSuccess()
		return "", nil
	case 401, 403:
		s.breaker.RecordFailure(provider.FailAuth)
		return provider.FailAuth, fmt.Errorf("home assistant rejected credentials")
	default:
		s.breaker.RecordFailure(provider.FailServer)
		return provider.FailServer, fmt.Errorf("home assistant answered %d", resp.StatusCode)
	}
}

// States lists entities (read path).
func (s *Service) States(ctx context.Context) ([]Entity, provider.Result[[]Entity]) {
	if !s.cfg.Configured() {
		return nil, provider.Result[[]Entity]{Failure: provider.FailNotConfigured, Err: fmt.Errorf("not connected")}
	}
	p := provider.Provider[[]Entity]{
		Name: "hass-states",
		Do: func(ctx context.Context) ([]Entity, *provider.CallMeta, error) {
			body, meta, err := httpx.GetJSON(s.cfg.HTTPClient, ctx, s.cfg.Base+"/api/states", s.headers())
			if err != nil {
				return nil, meta, err
			}
			ents, err := parseStates(body)
			if err != nil {
				if meta != nil {
					meta.Failure = provider.FailInvalid
				}
				return nil, meta, err
			}
			return ents, meta, nil
		},
		Validate: func(e []Entity) error {
			if len(e) == 0 {
				return provider.Empty("no entities")
			}
			return nil
		},
		Breaker: s.breaker,
	}
	strat := provider.Strategy[[]Entity]{Providers: []provider.Provider[[]Entity]{p}, Retry: provider.DefaultRetryPolicy(), Timeout: 10 * time.Second}
	r := strat.Execute(ctx)
	return r.Value, r
}

// Actuate calls a domain service (write path: light.turn_on, etc.).
// Domain and service are validated against an allowlist shape
// (domain.service, no shell, no raw paths) — the model cannot invent
// arbitrary endpoints.
func (s *Service) Actuate(ctx context.Context, domain, service, entityID string) provider.Result[bool] {
	if !s.cfg.Configured() {
		return provider.Result[bool]{Failure: provider.FailNotConfigured, Err: fmt.Errorf("not connected")}
	}
	if !validName(domain) || !validName(service) || !validEntity(entityID) {
		return provider.Result[bool]{Failure: provider.FailInvalid, Err: fmt.Errorf("invalid service call")}
	}
	p := provider.Provider[bool]{
		Name: "hass-actuate",
		Do: func(ctx context.Context) (bool, *provider.CallMeta, error) {
			u := fmt.Sprintf("%s/api/services/%s/%s", s.cfg.Base, domain, service)
			payload, _ := json.Marshal(map[string]string{"entity_id": entityID})
			req, err := http.NewRequestWithContext(ctx, "POST", u, bytes.NewReader(payload))
			if err != nil {
				return false, nil, err
			}
			req.Header.Set("Content-Type", "application/json")
			for k, v := range s.headers() {
				req.Header.Set(k, v)
			}
			resp, err := s.cfg.HTTPClient.Do(req)
			if err != nil {
				return false, nil, err
			}
			defer resp.Body.Close()
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
			meta := &provider.CallMeta{StatusCode: resp.StatusCode}
			if resp.StatusCode == 401 || resp.StatusCode == 403 {
				meta.Failure = provider.FailAuth
				return false, meta, fmt.Errorf("rejected credentials")
			}
			if cl := provider.ClassifyHTTP(resp.StatusCode); cl != "" {
				meta.Failure = cl
				return false, meta, fmt.Errorf("answer %d", resp.StatusCode)
			}
			return true, meta, nil
		},
		Breaker: s.breaker,
	}
	strat := provider.Strategy[bool]{Providers: []provider.Provider[bool]{p}, Retry: provider.RetryPolicy{MaxAttempts: 2}, Timeout: 10 * time.Second}
	return strat.Execute(ctx)
}

func validName(s string) bool {
	if s == "" || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

func validEntity(s string) bool {
	if s == "" || len(s) > 128 {
		return false
	}
	parts := strings.Split(s, ".")
	if len(parts) != 2 {
		return false
	}
	return validName(parts[0]) && validName(parts[1])
}

func parseStates(body []byte) ([]Entity, error) {
	if err := httpx.NonEmpty(body, "hass"); err != nil {
		return nil, err
	}
	var raw []struct {
		EntityID   string                 `json:"entity_id"`
		State      interface{}            `json:"state"`
		Attributes map[string]interface{} `json:"attributes"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, provider.Malformed("hass: invalid JSON")
	}
	var out []Entity
	for _, e := range raw {
		state := ""
		switch v := e.State.(type) {
		case string:
			state = v
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) {
				continue
			}
			state = strings.TrimRight(strings.TrimRight(fmt.Sprintf("%f", v), "0"), ".")
		default:
			continue
		}
		name := ""
		if e.Attributes != nil {
			name, _ = e.Attributes["friendly_name"].(string)
		}
		ent := Entity{ID: e.EntityID, State: state, Name: name}
		if err := ent.Validate(); err != nil {
			continue
		}
		out = append(out, ent)
	}
	if len(out) == 0 {
		return nil, provider.Empty("no usable entities")
	}
	return out, nil
}
