// Package aqi is the air-quality capability on the provider strategy.
//
// Provider decision: Open-Meteo air-quality (keyless, global, US AQI +
// PM2.5/PM10/O3) as the single provider. No second keyless vendor offers
// comparable normalized AQI (WAQI needs a token); rather than invent a
// weak fallback, the capability runs single-provider with full retry,
// breaker, validation, and cache semantics. A token-backed fallback
// (e.g. WAQI) can be appended to the ordered list without changing the
// contract.
package aqi

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"time"

	"github.com/ianclemence/ghost/pkg/provider"
	"github.com/ianclemence/ghost/pkg/providers/httpx"
)

// Report is the validated capability result (vendor-neutral).
type Report struct {
	AQI        int       `json:"aqi"`
	Category   string    `json:"category"`
	PM25       *float64  `json:"pm25,omitempty"`
	ObservedAt time.Time `json:"observed_at"`
	Provenance string    `json:"provenance"`
}

// Validate enforces a plausible US AQI range and finite particulates.
func (r Report) Validate() error {
	if r.AQI < 0 || r.AQI > 1000 {
		return provider.Invalid(fmt.Sprintf("aqi %d out of range", r.AQI))
	}
	if r.PM25 != nil && (math.IsNaN(*r.PM25) || math.IsInf(*r.PM25, 0) || *r.PM25 < 0) {
		return provider.Invalid("pm2.5 invalid")
	}
	return nil
}

// CategoryFor maps US AQI to the standard human category.
func CategoryFor(aqi int) string {
	switch {
	case aqi <= 50:
		return "Good"
	case aqi <= 100:
		return "Moderate"
	case aqi <= 150:
		return "Unhealthy for Sensitive Groups"
	case aqi <= 200:
		return "Unhealthy"
	case aqi <= 300:
		return "Very Unhealthy"
	default:
		return "Hazardous"
	}
}

// Config wires the endpoint (overridable for tests).
type Config struct {
	HTTPClient *http.Client
	// Base defaults to https://air-quality-api.open-meteo.com.
	Base string
	// CacheTTL defaults to 30 minutes.
	CacheTTL time.Duration
	// BreakerCooldown defaults to 60s.
	BreakerCooldown time.Duration
}

func (c Config) withDefaults() Config {
	out := c
	if out.HTTPClient == nil {
		out.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if out.Base == "" {
		out.Base = "https://air-quality-api.open-meteo.com"
	}
	if out.CacheTTL <= 0 {
		out.CacheTTL = 30 * time.Minute
	}
	if out.BreakerCooldown <= 0 {
		out.BreakerCooldown = 60 * time.Second
	}
	return out
}

// Service is the AQI capability: strategy + selective cache.
type Service struct {
	cfg   Config
	cache *provider.Cache[Report]
}

// New creates the capability service.
func New(cfg Config) *Service {
	cfg = cfg.withDefaults()
	return &Service{cfg: cfg, cache: provider.NewCache[Report](cfg.CacheTTL)}
}

func parse(body []byte) (Report, error) {
	if err := httpx.NonEmpty(body, "open-meteo aqi"); err != nil {
		return Report{}, err
	}
	var raw struct {
		Current *struct {
			USAQI *int     `json:"us_aqi"`
			PM25  *float64 `json:"pm2_5"`
			Time  string   `json:"time"`
		} `json:"current"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Report{}, provider.Malformed("aqi: invalid JSON")
	}
	if raw.Current == nil || raw.Current.USAQI == nil {
		return Report{}, provider.Invalid("aqi: missing us_aqi")
	}
	out := Report{AQI: *raw.Current.USAQI, Category: CategoryFor(*raw.Current.USAQI), ObservedAt: time.Now(), Provenance: "open-meteo"}
	if raw.Current.PM25 != nil {
		v := *raw.Current.PM25
		out.PM25 = &v
	}
	if raw.Current.Time != "" {
		if ts, err := time.Parse("2006-01-02T15:04", raw.Current.Time); err == nil {
			out.ObservedAt = ts
		}
	}
	if err := out.Validate(); err != nil {
		return Report{}, err
	}
	return out, nil
}

// CurrentByCoords is the capability entry point.
func (s *Service) CurrentByCoords(ctx context.Context, lat, lon float64, allowStale bool) (Report, provider.Result[Report]) {
	key := fmt.Sprintf("%.3f,%.3f", lat, lon)
	if v, ok, stale, _ := s.cache.Get(key); ok && (!stale || allowStale) {
		return v, provider.Result[Report]{Value: v, Provider: v.Provenance, FromCache: true, Stale: stale}
	}
	p := provider.Provider[Report]{
		Name: "open-meteo-aqi",
		Do: func(ctx context.Context) (Report, *provider.CallMeta, error) {
			u := fmt.Sprintf("%s/v1/air-quality?latitude=%f&longitude=%f&current=us_aqi,pm2_5",
				s.cfg.Base, lat, lon)
			body, meta, err := httpx.GetJSON(s.cfg.HTTPClient, ctx, u, nil)
			if err != nil {
				return Report{}, meta, err
			}
			r, err := parse(body)
			if err != nil {
				if meta != nil {
					if ve, ok := err.(*provider.ValidationError); ok {
						meta.Failure = ve.Class
					} else {
						meta.Failure = provider.FailInvalid
					}
				}
				return Report{}, meta, err
			}
			return r, meta, nil
		},
		Validate: func(r Report) error { return r.Validate() },
		Breaker:  provider.NewBreaker(3, s.cfg.BreakerCooldown),
	}
	strat := provider.Strategy[Report]{Providers: []provider.Provider[Report]{p}, Retry: provider.DefaultRetryPolicy(), Timeout: 10 * time.Second}
	r := strat.Execute(ctx)
	if r.Err == nil {
		s.cache.Set(key, r.Value, r.Provider)
		return r.Value, r
	}
	if allowStale {
		if v, ok, stale, _ := s.cache.Get(key); ok && stale {
			return v, provider.Result[Report]{Value: v, Provider: v.Provenance, FromCache: true, Stale: true}
		}
	}
	return Report{}, r
}
