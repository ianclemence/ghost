// Package crypto is the crypto-price capability on the provider
// strategy.
//
// Provider decision: CoinGecko simple/price (keyless, broad coin
// coverage, ids like "bitcoin") primary; Coinbase spot price (keyless,
// no id mapping needed for major pairs) fallback. Both keyless: the
// capability is READY with zero configuration.
package crypto

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

// Price is the validated capability result (vendor-neutral).
type Price struct {
	ID         string    `json:"id"`
	VS         string    `json:"vs"`
	Value      float64   `json:"value"`
	Change24h  *float64  `json:"change_24h,omitempty"`
	AsOf       time.Time `json:"as_of"`
	Provenance string    `json:"provenance"`
}

// Validate rejects non-positive or non-finite prices.
func (p Price) Validate() error {
	if math.IsNaN(p.Value) || math.IsInf(p.Value, 0) || p.Value <= 0 {
		return provider.Invalid("crypto price invalid")
	}
	if p.ID == "" || p.VS == "" {
		return provider.Invalid("crypto pair incomplete")
	}
	return nil
}

// Config wires endpoints (overridable for tests).
type Config struct {
	HTTPClient *http.Client
	// GeckoBase defaults to https://api.coingecko.com.
	GeckoBase string
	// CoinbaseBase defaults to https://api.coinbase.com.
	CoinbaseBase string
	// CacheTTL defaults to 2 minutes (prices move fast).
	CacheTTL time.Duration
	// BreakerCooldown defaults to 60s.
	BreakerCooldown time.Duration
}

func (c Config) withDefaults() Config {
	out := c
	if out.HTTPClient == nil {
		out.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if out.GeckoBase == "" {
		out.GeckoBase = "https://api.coingecko.com"
	}
	if out.CoinbaseBase == "" {
		out.CoinbaseBase = "https://api.coinbase.com"
	}
	if out.CacheTTL <= 0 {
		out.CacheTTL = 2 * time.Minute
	}
	if out.BreakerCooldown <= 0 {
		out.BreakerCooldown = 60 * time.Second
	}
	return out
}

// Service is the crypto capability: strategy + short cache.
type Service struct {
	cfg   Config
	cache *provider.Cache[Price]
}

// New creates the capability service.
func New(cfg Config) *Service {
	cfg = cfg.withDefaults()
	return &Service{cfg: cfg, cache: provider.NewCache[Price](cfg.CacheTTL)}
}

func parseGecko(body []byte, id, vs string) (Price, error) {
	if err := httpx.NonEmpty(body, "coingecko"); err != nil {
		return Price{}, err
	}
	var raw map[string]map[string]*float64
	if err := json.Unmarshal(body, &raw); err != nil {
		return Price{}, provider.Malformed("crypto: invalid JSON")
	}
	entry, ok := raw[id]
	if !ok {
		return Price{}, provider.Empty("crypto: unknown coin " + id)
	}
	v, ok := entry[strings.ToLower(vs)]
	if !ok || v == nil {
		return Price{}, provider.Invalid("crypto: missing price for " + vs)
	}
	out := Price{ID: id, VS: strings.ToUpper(vs), Value: *v, AsOf: time.Now(), Provenance: "coingecko"}
	if ch, ok := entry[strings.ToLower(vs)+"_24h_change"]; ok && ch != nil {
		out.Change24h = ch
	}
	if err := out.Validate(); err != nil {
		return Price{}, err
	}
	return out, nil
}

func parseCoinbase(body []byte, symbol, vs string) (Price, error) {
	if err := httpx.NonEmpty(body, "coinbase"); err != nil {
		return Price{}, err
	}
	var raw struct {
		Data *struct {
			Base     string `json:"base"`
			Currency string `json:"currency"`
			Amount   string `json:"amount"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Price{}, provider.Malformed("crypto: invalid JSON")
	}
	if raw.Data == nil || raw.Data.Amount == "" {
		return Price{}, provider.Invalid("crypto: missing spot amount")
	}
	var amt float64
	if _, err := fmt.Sscanf(raw.Data.Amount, "%f", &amt); err != nil {
		return Price{}, provider.Malformed("crypto: spot amount not numeric")
	}
	out := Price{ID: symbol, VS: strings.ToUpper(vs), Value: amt, AsOf: time.Now(), Provenance: "coinbase"}
	if err := out.Validate(); err != nil {
		return Price{}, err
	}
	return out, nil
}

// symbolFor maps common CoinGecko ids to Coinbase pair symbols for the
// fallback path. Unknown ids skip the fallback honestly (no guessing).
func symbolFor(id string) (string, bool) {
	switch strings.ToLower(id) {
	case "bitcoin":
		return "BTC", true
	case "ethereum":
		return "ETH", true
	case "solana":
		return "SOL", true
	default:
		return "", false
	}
}

// GetPrice is the capability entry point. vs is the fiat currency
// ("USD"); the Coinbase fallback supports USD pairs for major coins.
func (s *Service) GetPrice(ctx context.Context, id, vs string) (Price, provider.Result[Price]) {
	id = strings.ToLower(strings.TrimSpace(id))
	vs = strings.ToUpper(strings.TrimSpace(vs))
	if id == "" || vs == "" {
		return Price{}, provider.Result[Price]{Failure: provider.FailInvalid, Err: fmt.Errorf("coin and currency required")}
	}
	key := id + "/" + vs
	if v, ok, stale, _ := s.cache.Get(key); ok && !stale {
		return v, provider.Result[Price]{Value: v, Provider: v.Provenance, FromCache: true}
	}
	gecko := provider.Provider[Price]{
		Name: "coingecko",
		Do: func(ctx context.Context) (Price, *provider.CallMeta, error) {
			u := fmt.Sprintf("%s/api/v3/simple/price?ids=%s&vs_currencies=%s&include_24hr_change=true",
				s.cfg.GeckoBase, id, strings.ToLower(vs))
			body, meta, err := httpx.GetJSON(s.cfg.HTTPClient, ctx, u, nil)
			if err != nil {
				return Price{}, meta, err
			}
			p, err := parseGecko(body, id, vs)
			if err != nil {
				if meta != nil {
					meta.Failure = provider.FailInvalid
				}
				return Price{}, meta, err
			}
			return p, meta, nil
		},
		Validate: func(p Price) error { return p.Validate() },
		Breaker:  provider.NewBreaker(3, s.cfg.BreakerCooldown),
	}
	provs := []provider.Provider[Price]{gecko}
	if sym, ok := symbolFor(id); ok && vs == "USD" {
		provs = append(provs, provider.Provider[Price]{
			Name: "coinbase",
			Do: func(ctx context.Context) (Price, *provider.CallMeta, error) {
				u := fmt.Sprintf("%s/v2/prices/%s-%s/spot", s.cfg.CoinbaseBase, sym, vs)
				body, meta, err := httpx.GetJSON(s.cfg.HTTPClient, ctx, u, nil)
				if err != nil {
					return Price{}, meta, err
				}
				p, err := parseCoinbase(body, id, vs)
				if err != nil {
					if meta != nil {
						meta.Failure = provider.FailInvalid
					}
					return Price{}, meta, err
				}
				return p, meta, nil
			},
			Validate: func(p Price) error { return p.Validate() },
			Breaker:  provider.NewBreaker(3, s.cfg.BreakerCooldown),
		})
	}
	strat := provider.Strategy[Price]{Providers: provs, Retry: provider.DefaultRetryPolicy(), Timeout: 10 * time.Second}
	r := strat.Execute(ctx)
	if r.Err == nil {
		s.cache.Set(key, r.Value, r.Provider)
	}
	return r.Value, r
}
