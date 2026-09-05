// Package nearby is the nearby-places capability on the provider
// strategy.
//
// Provider decision: Overpass API (OpenStreetMap, keyless) with the two
// production mirrors as primary + fallback — the same mirrors the
// find-nearby skill script already uses (overpass-api.de,
// overpass.kumi.systems). Geocoding via Nominatim (keyless, proper
// User-Agent). No keys anywhere: READY with zero configuration.
package nearby

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/provider"
	"github.com/ianclemence/ghost/pkg/providers/httpx"
)

// Place is one validated result (vendor-neutral).
type Place struct {
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	Distance int     `json:"distance_m,omitempty"`
}

// Validate rejects empty names and impossible coordinates.
func (p Place) Validate() error {
	if strings.TrimSpace(p.Name) == "" {
		return provider.Invalid("place without name")
	}
	if math.IsNaN(p.Lat) || math.IsNaN(p.Lon) || p.Lat < -90 || p.Lat > 90 || p.Lon < -180 || p.Lon > 180 {
		return provider.Invalid("place coordinates invalid")
	}
	return nil
}

// Config wires endpoints (overridable for tests).
type Config struct {
	HTTPClient *http.Client
	// OverpassPrimary defaults to https://overpass-api.de/api/interpreter.
	OverpassPrimary string
	// OverpassFallback defaults to https://overpass.kumi.systems/api/interpreter.
	OverpassFallback string
	// NominatimBase defaults to https://nominatim.openstreetmap.org.
	NominatimBase string
	// CacheTTL defaults to 30 minutes.
	CacheTTL time.Duration
	// BreakerCooldown defaults to 60s.
	BreakerCooldown time.Duration
}

func (c Config) withDefaults() Config {
	out := c
	if out.HTTPClient == nil {
		out.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	if out.OverpassPrimary == "" {
		out.OverpassPrimary = "https://overpass-api.de/api/interpreter"
	}
	if out.OverpassFallback == "" {
		out.OverpassFallback = "https://overpass.kumi.systems/api/interpreter"
	}
	if out.NominatimBase == "" {
		out.NominatimBase = "https://nominatim.openstreetmap.org"
	}
	if out.CacheTTL <= 0 {
		out.CacheTTL = 30 * time.Minute
	}
	if out.BreakerCooldown <= 0 {
		out.BreakerCooldown = 60 * time.Second
	}
	return out
}

// Service is the nearby capability: strategy + selective cache.
type Service struct {
	cfg   Config
	cache *provider.Cache[[]Place]
}

// New creates the capability service.
func New(cfg Config) *Service {
	cfg = cfg.withDefaults()
	return &Service{cfg: cfg, cache: provider.NewCache[[]Place](cfg.CacheTTL)}
}

// Geocode resolves a place name via Nominatim. Failure is honest —
// never a guessed location.
func (s *Service) Geocode(ctx context.Context, place string) (lat, lon float64, err error) {
	u := s.cfg.NominatimBase + "/search?q=" + url.QueryEscape(place) + "&format=json&limit=1"
	body, _, err := httpx.GetJSON(s.cfg.HTTPClient, ctx, u, nil)
	if err != nil {
		return 0, 0, err
	}
	if err := httpx.NonEmpty(body, "nominatim"); err != nil {
		return 0, 0, err
	}
	var arr []struct {
		Lat string `json:"lat"`
		Lon string `json:"lon"`
	}
	if err := json.Unmarshal(body, &arr); err != nil {
		return 0, 0, provider.Malformed("nearby: geocode invalid JSON")
	}
	if len(arr) == 0 {
		return 0, 0, provider.Empty("nearby: location not found")
	}
	if _, err := fmt.Sscanf(arr[0].Lat, "%f", &lat); err != nil {
		return 0, 0, provider.Malformed("nearby: geocode lat not numeric")
	}
	if _, err := fmt.Sscanf(arr[0].Lon, "%f", &lon); err != nil {
		return 0, 0, provider.Malformed("nearby: geocode lon not numeric")
	}
	return lat, lon, nil
}

func parseOverpass(body []byte, amenity string, lat, lon float64, limit int) ([]Place, error) {
	if err := httpx.NonEmpty(body, "overpass"); err != nil {
		return nil, err
	}
	var raw struct {
		Elements []struct {
			Lat  *float64          `json:"lat"`
			Lon  *float64          `json:"lon"`
			Tags map[string]string `json:"tags"`
		} `json:"elements"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, provider.Malformed("nearby: invalid JSON")
	}
	var out []Place
	for _, e := range raw.Elements {
		if e.Lat == nil || e.Lon == nil {
			continue
		}
		name := ""
		if e.Tags != nil {
			name = e.Tags["name"]
		}
		if name == "" {
			continue
		}
		p := Place{Name: name, Type: amenity, Lat: *e.Lat, Lon: *e.Lon,
			Distance: int(haversine(lat, lon, *e.Lat, *e.Lon)),
		}
		if err := p.Validate(); err != nil {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return nil, provider.Empty("nearby: no places found")
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Distance < out[j].Distance })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func haversine(lat1, lon1, lat2, lon2 float64) float64 {
	const R = 6371000
	dLat := (lat2 - lat1) * math.Pi / 180
	dLon := (lon2 - lon1) * math.Pi / 180
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(lat1*math.Pi/180)*math.Cos(lat2*math.Pi/180)*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * R * math.Asin(math.Sqrt(a))
}

func overpassQuery(amenity string, lat, lon float64, radius int) string {
	return fmt.Sprintf("[out:json];node[amenity=%s](around:%d,%f,%f);out %d;",
		url.QueryEscape(amenity), radius, lat, lon, 30)
}

// SearchByCoords is the capability entry point.
func (s *Service) SearchByCoords(ctx context.Context, amenity string, lat, lon float64, radius, limit int) ([]Place, provider.Result[[]Place]) {
	amenity = strings.TrimSpace(amenity)
	if amenity == "" {
		return nil, provider.Result[[]Place]{Failure: provider.FailInvalid, Err: fmt.Errorf("place type required")}
	}
	if radius <= 0 {
		radius = 1500
	}
	if limit <= 0 || limit > 30 {
		limit = 15
	}
	key := fmt.Sprintf("%s@%.3f,%.3f/%d", amenity, lat, lon, radius)
	if v, ok, stale, _ := s.cache.Get(key); ok && !stale {
		return v, provider.Result[[]Place]{Value: v, Provider: "overpass", FromCache: true}
	}
	mkProvider := func(name, base string) provider.Provider[[]Place] {
		return provider.Provider[[]Place]{
			Name: name,
			Do: func(ctx context.Context) ([]Place, *provider.CallMeta, error) {
				body, meta, err := httpx.GetJSON(s.cfg.HTTPClient, ctx,
					base+"?data="+url.QueryEscape(overpassQuery(amenity, lat, lon, radius)), nil)
				if err != nil {
					return nil, meta, err
				}
				places, err := parseOverpass(body, amenity, lat, lon, limit)
				if err != nil {
					if meta != nil {
						if ve, ok := err.(*provider.ValidationError); ok {
							meta.Failure = ve.Class
						} else {
							meta.Failure = provider.FailInvalid
						}
					}
					return nil, meta, err
				}
				return places, meta, nil
			},
			Validate: func(p []Place) error {
				if len(p) == 0 {
					return provider.Empty("nearby: no places")
				}
				return nil
			},
			Breaker: provider.NewBreaker(3, s.cfg.BreakerCooldown),
		}
	}
	strat := provider.Strategy[[]Place]{
		Providers: []provider.Provider[[]Place]{
			mkProvider("overpass-primary", s.cfg.OverpassPrimary),
			mkProvider("overpass-mirror", s.cfg.OverpassFallback),
		},
		Retry: provider.DefaultRetryPolicy(), Timeout: 15 * time.Second,
	}
	r := strat.Execute(ctx)
	if r.Err == nil {
		s.cache.Set(key, r.Value, r.Provider)
	}
	return r.Value, r
}
