// Package currency is the currency-conversion capability on the
// provider strategy.
//
// Provider decision: open.er-api.com (keyless, 160+ currencies) primary,
// Frankfurter (keyless, ECB reference rates) fallback. Both keyless, so
// the capability is READY with zero configuration and degrades honestly
// only when both vendors fail.
package currency

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/provider"
	"github.com/ianclemence/ghost/pkg/providers/httpx"
)

// Conversion is the validated capability result (vendor-neutral).
type Conversion struct {
	From       string    `json:"from"`
	To         string    `json:"to"`
	Rate       float64   `json:"rate"`
	Amount     float64   `json:"amount,omitempty"`
	Converted  float64   `json:"converted,omitempty"`
	AsOf       time.Time `json:"as_of"`
	Provenance string    `json:"provenance"`
}

// Validate rejects non-positive or non-finite rates.
func (c Conversion) Validate() error {
	if math.IsNaN(c.Rate) || math.IsInf(c.Rate, 0) || c.Rate <= 0 {
		return provider.Invalid("exchange rate invalid")
	}
	if c.From == "" || c.To == "" {
		return provider.Invalid("currency pair incomplete")
	}
	return nil
}

// Config wires endpoints (overridable for tests).
type Config struct {
	HTTPClient *http.Client
	// ErAPIBase defaults to https://open.er-api.com.
	ErAPIBase string
	// FrankfurterBase defaults to https://api.frankfurt.app.
	FrankfurterBase string
	// CacheTTL defaults to 1 hour (rates move slowly).
	CacheTTL time.Duration
	// BreakerCooldown defaults to 60s.
	BreakerCooldown time.Duration
}

func (c Config) withDefaults() Config {
	out := c
	if out.HTTPClient == nil {
		out.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if out.ErAPIBase == "" {
		out.ErAPIBase = "https://open.er-api.com"
	}
	if out.FrankfurterBase == "" {
		out.FrankfurterBase = "https://api.frankfurt.app"
	}
	if out.CacheTTL <= 0 {
		out.CacheTTL = time.Hour
	}
	if out.BreakerCooldown <= 0 {
		out.BreakerCooldown = 60 * time.Second
	}
	return out
}

// Service is the currency capability: strategy + selective cache.
type Service struct {
	cfg   Config
	cache *provider.Cache[Conversion]
}

// New creates the capability service.
func New(cfg Config) *Service {
	cfg = cfg.withDefaults()
	return &Service{cfg: cfg, cache: provider.NewCache[Conversion](cfg.CacheTTL)}
}

func parseErAPI(body []byte, from, to string, amount float64) (Conversion, error) {
	if err := httpx.NonEmpty(body, "er-api"); err != nil {
		return Conversion{}, err
	}
	var raw struct {
		Result string             `json:"result"`
		Rates  map[string]float64 `json:"rates"`
		Time   string             `json:"time_last_update_utc"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Conversion{}, provider.Malformed("currency: invalid JSON")
	}
	if raw.Result != "" && raw.Result != "success" {
		return Conversion{}, provider.Invalid("currency: provider reported " + raw.Result)
	}
	rate, ok := raw.Rates[to]
	if !ok {
		return Conversion{}, provider.Invalid("currency: missing rate for " + to)
	}
	out := Conversion{From: from, To: to, Rate: rate, Provenance: "er-api", AsOf: time.Now()}
	if amount > 0 {
		out.Amount = amount
		out.Converted = amount * rate
	}
	if raw.Time != "" {
		if ts, err := time.Parse(time.RFC1123, raw.Time); err == nil {
			out.AsOf = ts
		}
	}
	if err := out.Validate(); err != nil {
		return Conversion{}, err
	}
	return out, nil
}

func parseFrankfurter(body []byte, from, to string, amount float64) (Conversion, error) {
	if err := httpx.NonEmpty(body, "frankfurter"); err != nil {
		return Conversion{}, err
	}
	var raw struct {
		Rates map[string]float64 `json:"rates"`
		Date  string             `json:"date"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Conversion{}, provider.Malformed("currency: invalid JSON")
	}
	rate, ok := raw.Rates[to]
	if !ok {
		return Conversion{}, provider.Invalid("currency: missing rate for " + to)
	}
	out := Conversion{From: from, To: to, Rate: rate, Provenance: "frankfurter", AsOf: time.Now()}
	if amount > 0 {
		out.Amount = amount
		out.Converted = amount * rate
	}
	if raw.Date != "" {
		if ts, err := time.Parse("2006-01-02", raw.Date); err == nil {
			out.AsOf = ts
		}
	}
	if err := out.Validate(); err != nil {
		return Conversion{}, err
	}
	return out, nil
}

// Convert is the capability entry point.
func (s *Service) Convert(ctx context.Context, from, to string, amount float64) (Conversion, provider.Result[Conversion]) {
	from = strings.ToUpper(strings.TrimSpace(from))
	to = strings.ToUpper(strings.TrimSpace(to))
	if from == "" || to == "" {
		return Conversion{}, provider.Result[Conversion]{Failure: provider.FailInvalid, Err: fmt.Errorf("currency pair required")}
	}
	key := from + ">" + to
	if v, ok, stale, _ := s.cache.Get(key); ok && !stale {
		v.Amount = amount
		if amount > 0 {
			v.Converted = amount * v.Rate
		}
		return v, provider.Result[Conversion]{Value: v, Provider: v.Provenance, FromCache: true}
	}
	er := provider.Provider[Conversion]{
		Name: "er-api",
		Do: func(ctx context.Context) (Conversion, *provider.CallMeta, error) {
			body, meta, err := httpx.GetJSON(s.cfg.HTTPClient, ctx,
				fmt.Sprintf("%s/v6/latest/%s", s.cfg.ErAPIBase, from), nil)
			if err != nil {
				return Conversion{}, meta, err
			}
			c, err := parseErAPI(body, from, to, amount)
			if err != nil {
				if meta != nil {
					meta.Failure = provider.FailInvalid
				}
				return Conversion{}, meta, err
			}
			return c, meta, nil
		},
		Validate: func(c Conversion) error { return c.Validate() },
		Breaker:  provider.NewBreaker(3, s.cfg.BreakerCooldown),
	}
	fb := provider.Provider[Conversion]{
		Name: "frankfurter",
		Do: func(ctx context.Context) (Conversion, *provider.CallMeta, error) {
			body, meta, err := httpx.GetJSON(s.cfg.HTTPClient, ctx,
				fmt.Sprintf("%s/latest?from=%s&to=%s", s.cfg.FrankfurterBase, from, to), nil)
			if err != nil {
				return Conversion{}, meta, err
			}
			c, err := parseFrankfurter(body, from, to, amount)
			if err != nil {
				if meta != nil {
					meta.Failure = provider.FailInvalid
				}
				return Conversion{}, meta, err
			}
			return c, meta, nil
		},
		Validate: func(c Conversion) error { return c.Validate() },
		Breaker:  provider.NewBreaker(3, s.cfg.BreakerCooldown),
	}
	strat := provider.Strategy[Conversion]{Providers: []provider.Provider[Conversion]{er, fb}, Retry: provider.DefaultRetryPolicy(), Timeout: 10 * time.Second}
	r := strat.Execute(ctx)
	if r.Err == nil {
		s.cache.Set(key, r.Value, r.Provider)
	}
	return r.Value, r
}
