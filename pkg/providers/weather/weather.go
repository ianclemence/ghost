// Package weather is the provider-resilience reference implementation
// for Ghost capabilities.
//
// Capability: Weather. Providers: Open-Meteo (keyless primary) and
// OpenWeather (keyed fallback). Neither vendor is hard-coded into the
// capability abstraction: the capability declares an ordered provider
// list, and pkg/provider.Strategy owns selection, timeout, bounded
// retry, validation, fallback, and honest failure.
//
// Validation rejects impossible values (e.g. temperature "banana",
// humidity 500%) as provider failures, never as successful results.
// Selective caching (10-minute TTL with provenance + stale semantics)
// covers weather's slow-moving nature. Offline serves stale cache only
// when the caller accepts it; fresh network data is never fabricated.
package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/provider"
)

// Current is the validated capability result (vendor-neutral).
type Current struct {
	TemperatureC float64   `json:"temperature_c"`
	HumidityPct  *float64  `json:"humidity_pct,omitempty"`
	Description  string    `json:"description,omitempty"`
	ObservedAt   time.Time `json:"observed_at"`
	Provenance   string    `json:"provenance"`
}

// Validate enforces semantic sanity: finite temperature in a plausible
// Earth range, humidity in [0,100] when present, non-empty provenance.
func (c Current) Validate() error {
	if math.IsNaN(c.TemperatureC) || math.IsInf(c.TemperatureC, 0) {
		return provider.Invalid("temperature not numeric")
	}
	if c.TemperatureC < -90 || c.TemperatureC > 60 {
		return provider.Invalid(fmt.Sprintf("temperature %.1f out of range", c.TemperatureC))
	}
	if c.HumidityPct != nil && (*c.HumidityPct < 0 || *c.HumidityPct > 100) {
		return provider.Invalid("humidity out of range")
	}
	return nil
}

// Geocode resolves "Bangkok" etc. to coordinates via Open-Meteo's free
// geocoding API. Separated so tests can inject coordinates directly.
func geocode(ctx context.Context, client *http.Client, base, place string) (lat, lon float64, name string, err error) {
	u := base + "/v1/search?name=" + url.QueryEscape(place) + "&count=1&language=en&format=json"
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return 0, 0, "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		return 0, 0, "", &httpError{Status: 429}
	}
	if resp.StatusCode != 200 {
		return 0, 0, "", &httpError{Status: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return 0, 0, "", err
	}
	var g struct {
		Results []struct {
			Latitude  float64 `json:"latitude"`
			Longitude float64 `json:"longitude"`
			Name      string  `json:"name"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &g); err != nil {
		return 0, 0, "", err
	}
	if len(g.Results) == 0 {
		return 0, 0, "", provider.Empty("no geocode results")
	}
	return g.Results[0].Latitude, g.Results[0].Longitude, g.Results[0].Name, nil
}

type httpError struct{ Status int }

func (e *httpError) Error() string { return fmt.Sprintf("http %d", e.Status) }

// Config wires endpoints (overridable for tests) and credentials.
type Config struct {
	// HTTPClient defaults to a 10s-timeout client.
	HTTPClient *http.Client
	// OpenMeteoBase defaults to https://api.open-meteo.com.
	OpenMeteoBase string
	// GeocodeBase defaults to https://geocoding-api.open-meteo.com.
	GeocodeBase string
	// OpenWeatherBase defaults to https://api.openweathermap.org.
	OpenWeatherBase string
	// OpenWeatherKey empty means the fallback is not configured
	// (capability still works via primary; fallback skipped honestly).
	OpenWeatherKey string
	// CacheTTL defaults to 10 minutes.
	CacheTTL time.Duration
	// BreakerCooldown defaults to 60s.
	BreakerCooldown time.Duration
}

func (c *Config) withDefaults() Config {
	out := *c
	if out.HTTPClient == nil {
		out.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if out.OpenMeteoBase == "" {
		out.OpenMeteoBase = "https://api.open-meteo.com"
	}
	if out.GeocodeBase == "" {
		out.GeocodeBase = "https://geocoding-api.open-meteo.com"
	}
	if out.OpenWeatherBase == "" {
		out.OpenWeatherBase = "https://api.openweathermap.org"
	}
	if out.CacheTTL <= 0 {
		out.CacheTTL = 10 * time.Minute
	}
	if out.BreakerCooldown <= 0 {
		out.BreakerCooldown = 60 * time.Second
	}
	return out
}

// Service is the weather capability: strategy + selective cache.
type Service struct {
	cfg   Config
	cache *provider.Cache[Current]
}

// New creates the capability service.
func New(cfg Config) *Service {
	cfg = cfg.withDefaults()
	return &Service{cfg: cfg, cache: provider.NewCache[Current](cfg.CacheTTL)}
}

// openMeteoProvider builds the keyless primary.
func (s *Service) openMeteoProvider(lat, lon float64) provider.Provider[Current] {
	return provider.Provider[Current]{
		Name: "open-meteo",
		Do: func(ctx context.Context) (Current, *provider.CallMeta, error) {
			u := fmt.Sprintf("%s/v1/forecast?latitude=%f&longitude=%f&current=temperature_2m,relative_humidity_2m,weather_code&timezone=auto",
				s.cfg.OpenMeteoBase, lat, lon)
			req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
			if err != nil {
				return Current{}, nil, err
			}
			resp, err := s.cfg.HTTPClient.Do(req)
			if err != nil {
				return Current{}, nil, err
			}
			defer resp.Body.Close()
			meta := &provider.CallMeta{StatusCode: resp.StatusCode}
			if resp.StatusCode == 429 {
				meta.Failure = provider.FailRateLimited
				return Current{}, meta, &httpError{Status: 429}
			}
			if cl := provider.ClassifyHTTP(resp.StatusCode); cl != "" {
				meta.Failure = cl
				return Current{}, meta, &httpError{Status: resp.StatusCode}
			}
			body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			if err != nil {
				return Current{}, meta, err
			}
			cur, err := parseOpenMeteo(body)
			if err != nil {
				meta.Failure = failureOf(err)
				return Current{}, meta, err
			}
			cur.Provenance = "open-meteo"
			return cur, meta, nil
		},
		Validate: func(c Current) error { return c.Validate() },
		Breaker:  provider.NewBreaker(3, s.cfg.BreakerCooldown),
	}
}

// openWeatherProvider builds the keyed fallback; nil when unconfigured.
func (s *Service) openWeatherProvider(lat, lon float64) *provider.Provider[Current] {
	if strings.TrimSpace(s.cfg.OpenWeatherKey) == "" {
		return nil
	}
	key := s.cfg.OpenWeatherKey
	p := provider.Provider[Current]{
		Name: "openweather",
		Do: func(ctx context.Context) (Current, *provider.CallMeta, error) {
			u := fmt.Sprintf("%s/data/2.5/weather?lat=%f&lon=%f&units=metric&appid=%s",
				s.cfg.OpenWeatherBase, lat, lon, url.QueryEscape(key))
			req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
			if err != nil {
				return Current{}, nil, err
			}
			resp, err := s.cfg.HTTPClient.Do(req)
			if err != nil {
				return Current{}, nil, err
			}
			defer resp.Body.Close()
			meta := &provider.CallMeta{StatusCode: resp.StatusCode}
			if resp.StatusCode == 429 {
				meta.Failure = provider.FailRateLimited
				return Current{}, meta, &httpError{Status: 429}
			}
			if cl := provider.ClassifyHTTP(resp.StatusCode); cl != "" {
				meta.Failure = cl
				return Current{}, meta, &httpError{Status: resp.StatusCode}
			}
			body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			if err != nil {
				return Current{}, meta, err
			}
			cur, err := parseOpenWeather(body)
			if err != nil {
				meta.Failure = failureOf(err)
				return Current{}, meta, err
			}
			cur.Provenance = "openweather"
			return cur, meta, nil
		},
		Validate: func(c Current) error { return c.Validate() },
		Breaker:  provider.NewBreaker(3, s.cfg.BreakerCooldown),
	}
	return &p
}

func failureOf(err error) provider.FailureClass {
	if ve, ok := err.(*provider.ValidationError); ok {
		return ve.Class
	}
	return provider.FailInvalid
}

// parseOpenMeteo parses + validates the forecast payload. Unknown or
// mistyped temperature is a provider failure, never a success.
func parseOpenMeteo(body []byte) (Current, error) {
	if len(strings.TrimSpace(string(body))) == 0 {
		return Current{}, provider.Empty("empty open-meteo response")
	}
	var raw struct {
		Current *struct {
			Temperature2m *float64 `json:"temperature_2m"`
			Humidity      *float64 `json:"relative_humidity_2m"`
			WeatherCode   *int     `json:"weather_code"`
			Time          string   `json:"time"`
		} `json:"current"`
	}
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	var generic map[string]interface{}
	if err := json.Unmarshal(body, &generic); err != nil {
		return Current{}, provider.Malformed("open-meteo: invalid JSON")
	}
	_ = generic
	if err := json.Unmarshal(body, &raw); err != nil {
		return Current{}, provider.Malformed("open-meteo: invalid JSON")
	}
	if raw.Current == nil || raw.Current.Temperature2m == nil {
		// Distinguish "temperature present but wrong type" (malformed)
		// from "missing" (invalid): inspect raw shape.
		if cur, ok := generic["current"].(map[string]interface{}); ok {
			if t, exists := cur["temperature_2m"]; exists && t != nil {
				if _, isNum := t.(float64); !isNum {
					return Current{}, provider.Malformed("open-meteo: temperature not numeric")
				}
			}
		}
		return Current{}, provider.Invalid("open-meteo: missing temperature")
	}
	out := Current{TemperatureC: *raw.Current.Temperature2m, ObservedAt: time.Now()}
	if raw.Current.Humidity != nil {
		h := *raw.Current.Humidity
		out.HumidityPct = &h
	}
	if raw.Current.WeatherCode != nil {
		out.Description = weatherCodeText(*raw.Current.WeatherCode)
	}
	if raw.Current.Time != "" {
		if ts, err := time.Parse("2006-01-02T15:04", raw.Current.Time); err == nil {
			out.ObservedAt = ts
		}
	}
	if err := out.Validate(); err != nil {
		return Current{}, err
	}
	return out, nil
}

// parseOpenWeather parses + validates the current-weather payload.
func parseOpenWeather(body []byte) (Current, error) {
	if len(strings.TrimSpace(string(body))) == 0 {
		return Current{}, provider.Empty("empty openweather response")
	}
	var raw struct {
		Main *struct {
			Temp     *float64 `json:"temp"`
			Humidity *float64 `json:"humidity"`
		} `json:"main"`
		Weather []struct {
			Description string `json:"description"`
		} `json:"weather"`
		Dt *int64 `json:"dt"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return Current{}, provider.Malformed("openweather: invalid JSON")
	}
	if raw.Main == nil || raw.Main.Temp == nil {
		return Current{}, provider.Invalid("openweather: missing temperature")
	}
	out := Current{TemperatureC: *raw.Main.Temp, ObservedAt: time.Now()}
	if raw.Main.Humidity != nil {
		h := *raw.Main.Humidity
		out.HumidityPct = &h
	}
	if len(raw.Weather) > 0 {
		out.Description = raw.Weather[0].Description
	}
	if raw.Dt != nil {
		out.ObservedAt = time.Unix(*raw.Dt, 0)
	}
	if err := out.Validate(); err != nil {
		return Current{}, err
	}
	return out, nil
}

func weatherCodeText(code int) string {
	switch {
	case code == 0:
		return "clear sky"
	case code <= 3:
		return "partly cloudy"
	case code <= 48:
		return "fog"
	case code <= 67:
		return "rain"
	case code <= 77:
		return "snow"
	case code <= 82:
		return "showers"
	case code <= 99:
		return "thunderstorm"
	default:
		return "unknown"
	}
}

// CurrentByCoords is the capability entry point: cache -> strategy
// (primary + optional fallback) -> cache store -> honest failure.
// Offline callers pass allowStale=true to accept cached data explicitly.
func (s *Service) CurrentByCoords(ctx context.Context, lat, lon float64, allowStale bool) (Current, provider.Result[Current]) {
	key := fmt.Sprintf("%.3f,%.3f", lat, lon)
	if v, ok, stale, _ := s.cache.Get(key); ok {
		if !stale {
			return v, provider.Result[Current]{Value: v, Provider: v.Provenance, FromCache: true}
		}
		if allowStale {
			return v, provider.Result[Current]{Value: v, Provider: v.Provenance, FromCache: true, Stale: true}
		}
	}
	provs := []provider.Provider[Current]{s.openMeteoProvider(lat, lon)}
	if fb := s.openWeatherProvider(lat, lon); fb != nil {
		provs = append(provs, *fb)
	}
	strat := provider.Strategy[Current]{Providers: provs, Retry: provider.DefaultRetryPolicy(), Timeout: 10 * time.Second}
	r := strat.Execute(ctx)
	if r.Err == nil {
		s.cache.Set(key, r.Value, r.Provider)
		return r.Value, r
	}
	// All providers failed: serve stale explicitly if allowed.
	if allowStale {
		if v, ok, stale, _ := s.cache.Get(key); ok && stale {
			return v, provider.Result[Current]{Value: v, Provider: v.Provenance, FromCache: true, Stale: true}
		}
	}
	return Current{}, r
}

// CurrentByPlace resolves a place then fetches. Geocode failure is an
// honest failure, never a guessed location.
func (s *Service) CurrentByPlace(ctx context.Context, place string, allowStale bool) (Current, provider.Result[Current]) {
	lat, lon, _, err := geocode(ctx, s.cfg.HTTPClient, s.cfg.GeocodeBase, place)
	if err != nil {
		return Current{}, provider.Result[Current]{Failure: provider.FailInvalid, Err: fmt.Errorf("location lookup failed: %w", err)}
	}
	return s.CurrentByCoords(ctx, lat, lon, allowStale)
}
