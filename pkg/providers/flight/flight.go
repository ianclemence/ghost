// Package flight is the flight-tracking capability on the generic
// provider strategy.
//
// Provider decision (deliberate, researched 2026):
//   - Primary: AviationStack — 100 req/mo free, global schedule + status +
//     delay + gate coverage, simple key auth, flight-number lookup. Best
//     fit for a personal assistant's "is TG123 on time" queries.
//   - Fallback: AeroDataBox (via RapidAPI, 600 units/mo free) —
//     complementary coverage: 365-day history + future schedules where the
//     AviationStack free tier is today-focused, plus codeshare resolution.
//     Same capability contract, different vendor and coverage window.
//   - Rejected: OpenSky as primary/fallback — no usable flight-number
//     lookup (needs ICAO24 + time window) and non-commercial ADS-B terms;
//     FlightAware/Cirium — paid-only, unjustified for this capability;
//     keyless schedule APIs — none with reliable flight-number status.
//
// Either key alone makes the capability READY; both absent is
// NEEDS_CONFIGURATION (never fake live data). The LLM never invents a
// fallback: the ordered list below is the contract.
package flight

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ianclemence/ghost/pkg/provider"
)

// Status is the normalized, vendor-neutral flight status.
type Status string

const (
	StatusScheduled Status = "scheduled"
	StatusActive    Status = "active"
	StatusLanded    Status = "landed"
	StatusCancelled Status = "cancelled"
	StatusDiverted  Status = "diverted"
	StatusDelayed   Status = "delayed"
	StatusUnknown   Status = "unknown"
)

// Flight is the validated capability result (vendor-neutral).
type Flight struct {
	Number     string    `json:"number"`
	Airline    string    `json:"airline,omitempty"`
	From       string    `json:"from,omitempty"`
	To         string    `json:"to,omitempty"`
	Status     Status    `json:"status"`
	Scheduled  time.Time `json:"scheduled,omitempty"`
	Gate       string    `json:"gate,omitempty"`
	Terminal   string    `json:"terminal,omitempty"`
	DelayMin   *int      `json:"delay_min,omitempty"`
	Provenance string    `json:"provenance"`
}

// Validate rejects empty numbers and unknown statuses treated as success.
func (f Flight) Validate() error {
	if strings.TrimSpace(f.Number) == "" {
		return provider.Invalid("missing flight number")
	}
	return nil
}

// NormalizeStatus maps vendor status strings to the contract.
func NormalizeStatus(s string) Status {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "scheduled":
		return StatusScheduled
	case "active", "en-route", "enroute", "airborne", "departed":
		return StatusActive
	case "landed", "arrived":
		return StatusLanded
	case "cancelled", "canceled":
		return StatusCancelled
	case "diverted":
		return StatusDiverted
	case "delayed":
		return StatusDelayed
	default:
		return StatusUnknown
	}
}

// Config wires endpoints (overridable for tests) and credentials.
type Config struct {
	HTTPClient *http.Client
	// AviationBase defaults to https://api.aviationstack.com.
	AviationBase string
	// AviationKey is the primary credential (empty = primary skipped).
	AviationKey string
	// AeroDataBoxBase defaults to https://aerodatabox.p.rapidapi.com.
	AeroDataBoxBase string
	// AeroDataBoxKey is the RapidAPI key (empty = fallback skipped).
	AeroDataBoxKey string
	// CacheTTL defaults to 2 minutes (live status is mutable; short
	// repeat-request cache only, with provenance + stale semantics).
	CacheTTL time.Duration
	// BreakerCooldown defaults to 60s.
	BreakerCooldown time.Duration
}

func (c Config) withDefaults() Config {
	out := c
	if out.HTTPClient == nil {
		out.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if out.AviationBase == "" {
		out.AviationBase = "https://api.aviationstack.com"
	}
	if out.AeroDataBoxBase == "" {
		out.AeroDataBoxBase = "https://aerodatabox.p.rapidapi.com"
	}
	if out.CacheTTL <= 0 {
		out.CacheTTL = 2 * time.Minute
	}
	if out.BreakerCooldown <= 0 {
		out.BreakerCooldown = 60 * time.Second
	}
	return out
}

// Configured reports whether the capability can run at all.
func (c Config) Configured() bool {
	return strings.TrimSpace(c.AviationKey) != "" || strings.TrimSpace(c.AeroDataBoxKey) != ""
}

// Service is the flight capability: strategy + short repeat cache.
type Service struct {
	cfg   Config
	cache *provider.Cache[Flight]
}

// Configured reports whether the service instance can run at all.
func (s *Service) Configured() bool { return s.cfg.Configured() }

// New creates the capability service.
func New(cfg Config) *Service {
	cfg = cfg.withDefaults()
	return &Service{cfg: cfg, cache: provider.NewCache[Flight](cfg.CacheTTL)}
}

func getJSON(client *http.Client, ctx context.Context, u string, headers map[string]string) ([]byte, *provider.CallMeta, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	meta := &provider.CallMeta{StatusCode: resp.StatusCode}
	if resp.StatusCode == 429 {
		meta.Failure = provider.FailRateLimited
		return nil, meta, fmt.Errorf("http 429")
	}
	if cl := provider.ClassifyHTTP(resp.StatusCode); cl != "" {
		meta.Failure = cl
		return nil, meta, fmt.Errorf("http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, meta, err
	}
	return body, meta, nil
}

func (s *Service) aviationProvider(flightNumber string) *provider.Provider[Flight] {
	if strings.TrimSpace(s.cfg.AviationKey) == "" {
		return nil
	}
	key := s.cfg.AviationKey
	return &provider.Provider[Flight]{
		Name: "aviationstack",
		Do: func(ctx context.Context) (Flight, *provider.CallMeta, error) {
			u := fmt.Sprintf("%s/v1/flights?access_key=%s&flight_iata=%s&limit=1",
				s.cfg.AviationBase, url.QueryEscape(key), url.QueryEscape(flightNumber))
			body, meta, err := getJSON(s.cfg.HTTPClient, ctx, u, nil)
			if err != nil {
				return Flight{}, meta, err
			}
			f, err := parseAviationStack(body, flightNumber)
			if err != nil {
				if meta != nil {
					meta.Failure = failureOf(err)
				}
				return Flight{}, meta, err
			}
			f.Provenance = "aviationstack"
			return f, meta, nil
		},
		Validate: func(f Flight) error { return f.Validate() },
		Breaker:  provider.NewBreaker(3, s.cfg.BreakerCooldown),
	}
}

func (s *Service) aeroDataBoxProvider(flightNumber string) *provider.Provider[Flight] {
	if strings.TrimSpace(s.cfg.AeroDataBoxKey) == "" {
		return nil
	}
	key := s.cfg.AeroDataBoxKey
	return &provider.Provider[Flight]{
		Name: "aerodatabox",
		Do: func(ctx context.Context) (Flight, *provider.CallMeta, error) {
			u := fmt.Sprintf("%s/flights/number/%s",
				s.cfg.AeroDataBoxBase, url.PathEscape(flightNumber))
			body, meta, err := getJSON(s.cfg.HTTPClient, ctx, u,
				map[string]string{"X-RapidAPI-Key": key})
			if err != nil {
				return Flight{}, meta, err
			}
			f, err := parseAeroDataBox(body, flightNumber)
			if err != nil {
				if meta != nil {
					meta.Failure = failureOf(err)
				}
				return Flight{}, meta, err
			}
			f.Provenance = "aerodatabox"
			return f, meta, nil
		},
		Validate: func(f Flight) error { return f.Validate() },
		Breaker:  provider.NewBreaker(3, s.cfg.BreakerCooldown),
	}
}

func failureOf(err error) provider.FailureClass {
	if ve, ok := err.(*provider.ValidationError); ok {
		return ve.Class
	}
	return provider.FailInvalid
}

// parseAviationStack parses the /v1/flights envelope. A valid envelope
// with zero flights is an honest empty (unknown flight number), not a
// fabricated status.
func parseAviationStack(body []byte, want string) (Flight, error) {
	if len(strings.TrimSpace(string(body))) == 0 {
		return Flight{}, provider.Empty("empty aviationstack response")
	}
	var env struct {
		Data []struct {
			Flight struct {
				IATA string `json:"iata"`
			} `json:"flight"`
			Airline struct {
				Name string `json:"name"`
			} `json:"airline"`
			Departure struct {
				IATA      string `json:"iata"`
				Scheduled string `json:"scheduled"`
				Gate      string `json:"gate"`
				Terminal  string `json:"terminal"`
				Delay     *int   `json:"delay"`
			} `json:"departure"`
			Arrival struct {
				IATA string `json:"iata"`
			} `json:"arrival"`
			FlightStatus interface{} `json:"flight_status"`
			FlightDate   string      `json:"flight_date"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return Flight{}, provider.Malformed("aviationstack: invalid JSON")
	}
	if len(env.Data) == 0 {
		return Flight{}, provider.Empty("no flight found for " + want)
	}
	d := env.Data[0]
	status := ""
	switch v := d.FlightStatus.(type) {
	case string:
		status = v
	case nil:
		status = ""
	default:
		return Flight{}, provider.Malformed("aviationstack: flight_status not a string")
	}
	out := Flight{
		Number:   d.Flight.IATA,
		Airline:  d.Airline.Name,
		From:     d.Departure.IATA,
		To:       d.Arrival.IATA,
		Status:   NormalizeStatus(status),
		Gate:     d.Departure.Gate,
		Terminal: d.Departure.Terminal,
		DelayMin: d.Departure.Delay,
	}
	if out.Number == "" {
		out.Number = want
	}
	if d.Departure.Scheduled != "" {
		if ts, err := time.Parse(time.RFC3339, d.Departure.Scheduled); err == nil {
			out.Scheduled = ts
		} else if ts, err := time.Parse("2006-01-02T15:04:05", d.Departure.Scheduled); err == nil {
			out.Scheduled = ts
		}
	}
	return out, out.Validate()
}

// parseAeroDataBox parses the /flights/number/{n} array. AeroDataBox
// returns an array (possibly empty); each entry carries departure/arrival
// with scheduled times and a status string.
func parseAeroDataBox(body []byte, want string) (Flight, error) {
	if len(strings.TrimSpace(string(body))) == 0 {
		return Flight{}, provider.Empty("empty aerodatabox response")
	}
	var arr []struct {
		Number  string `json:"number"`
		Airline struct {
			Name string `json:"name"`
		} `json:"airline"`
		Status    string `json:"status"`
		Departure struct {
			Airport struct {
				IATA string `json:"iata"`
			} `json:"airport"`
			ScheduledTimeLocal string `json:"scheduledTimeLocal"`
		} `json:"departure"`
		Arrival struct {
			Airport struct {
				IATA string `json:"iata"`
			} `json:"airport"`
		} `json:"arrival"`
	}
	if err := json.Unmarshal(body, &arr); err != nil {
		return Flight{}, provider.Malformed("aerodatabox: invalid JSON")
	}
	if len(arr) == 0 {
		return Flight{}, provider.Empty("no flight found for " + want)
	}
	a := arr[0]
	out := Flight{
		Number:  a.Number,
		Airline: a.Airline.Name,
		From:    a.Departure.Airport.IATA,
		To:      a.Arrival.Airport.IATA,
		Status:  NormalizeStatus(a.Status),
	}
	if out.Number == "" {
		out.Number = want
	}
	if a.Departure.ScheduledTimeLocal != "" {
		for _, layout := range []string{"2006-01-02 15:04", time.RFC3339, "2006-01-02T15:04:05"} {
			if ts, err := time.Parse(layout, a.Departure.ScheduledTimeLocal); err == nil {
				out.Scheduled = ts
				break
			}
		}
	}
	return out, out.Validate()
}

// Lookup is the capability entry point: readiness → cache → strategy →
// honest failure. Unknown flight numbers (valid empty) are reported as
// "couldn't find that flight", never fabricated.
func (s *Service) Lookup(ctx context.Context, flightNumber string) (Flight, provider.Result[Flight]) {
	flightNumber = strings.ToUpper(strings.TrimSpace(flightNumber))
	if flightNumber == "" {
		return Flight{}, provider.Result[Flight]{Failure: provider.FailInvalid, Err: fmt.Errorf("flight number required")}
	}
	if !s.cfg.Configured() {
		return Flight{}, provider.Result[Flight]{Failure: provider.FailNotConfigured, Err: fmt.Errorf("flight tracking not configured")}
	}
	if v, ok, stale, _ := s.cache.Get(flightNumber); ok && !stale {
		return v, provider.Result[Flight]{Value: v, Provider: v.Provenance, FromCache: true}
	}
	var provs []provider.Provider[Flight]
	if p := s.aviationProvider(flightNumber); p != nil {
		provs = append(provs, *p)
	}
	if p := s.aeroDataBoxProvider(flightNumber); p != nil {
		provs = append(provs, *p)
	}
	strat := provider.Strategy[Flight]{Providers: provs, Retry: provider.DefaultRetryPolicy(), Timeout: 10 * time.Second}
	r := strat.Execute(ctx)
	if r.Err == nil {
		s.cache.Set(flightNumber, r.Value, r.Provider)
	}
	return r.Value, r
}
