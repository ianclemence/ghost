package weather

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"github.com/ianclemence/ghost/pkg/provider"
)

// MaxForecastDays is how far ahead Open-Meteo's free forecast reaches.
const MaxForecastDays = 16

// Day is one validated day of a forecast.
type Day struct {
	Date        string   `json:"date"` // YYYY-MM-DD in the place's own time zone
	MaxC        float64  `json:"max_c"`
	MinC        float64  `json:"min_c"`
	RainPct     *float64 `json:"rain_pct,omitempty"`
	Description string   `json:"description,omitempty"`
}

func (d Day) validate() error {
	for _, v := range []float64{d.MaxC, d.MinC} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < -90 || v > 60 {
			return provider.Invalid(fmt.Sprintf("forecast temperature %.1f out of range", v))
		}
	}
	if d.MinC > d.MaxC {
		return provider.Invalid("forecast low is above its high")
	}
	if d.RainPct != nil && (*d.RainPct < 0 || *d.RainPct > 100) {
		return provider.Invalid("rain chance out of range")
	}
	return nil
}

// parseForecast reads Open-Meteo's daily block. A day with a missing
// temperature is dropped rather than filled in.
func parseForecast(body []byte) ([]Day, error) {
	var raw struct {
		Daily struct {
			Time []string   `json:"time"`
			Code []*int     `json:"weather_code"`
			Max  []*float64 `json:"temperature_2m_max"`
			Min  []*float64 `json:"temperature_2m_min"`
			Rain []*float64 `json:"precipitation_probability_max"`
		} `json:"daily"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, provider.Malformed("open-meteo: invalid forecast JSON")
	}
	d := raw.Daily
	if len(d.Time) == 0 {
		return nil, provider.Empty("open-meteo: no forecast days")
	}
	var out []Day
	for i, date := range d.Time {
		if i >= len(d.Max) || i >= len(d.Min) || d.Max[i] == nil || d.Min[i] == nil {
			continue
		}
		day := Day{Date: date, MaxC: *d.Max[i], MinC: *d.Min[i]}
		if i < len(d.Code) && d.Code[i] != nil {
			day.Description = weatherCodeText(*d.Code[i])
		}
		if i < len(d.Rain) && d.Rain[i] != nil {
			r := *d.Rain[i]
			day.RainPct = &r
		}
		if err := day.validate(); err != nil {
			return nil, err
		}
		out = append(out, day)
	}
	if len(out) == 0 {
		return nil, provider.Invalid("open-meteo: forecast had no usable days")
	}
	return out, nil
}

func (s *Service) forecastProvider(lat, lon float64) provider.Provider[[]Day] {
	return provider.Provider[[]Day]{
		Name: "open-meteo",
		Do: func(ctx context.Context) ([]Day, *provider.CallMeta, error) {
			u := fmt.Sprintf("%s/v1/forecast?latitude=%f&longitude=%f&daily=weather_code,temperature_2m_max,temperature_2m_min,precipitation_probability_max&forecast_days=%d&timezone=auto",
				s.cfg.OpenMeteoBase, lat, lon, MaxForecastDays)
			req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
			if err != nil {
				return nil, nil, err
			}
			resp, err := s.cfg.HTTPClient.Do(req)
			if err != nil {
				return nil, nil, err
			}
			defer resp.Body.Close()
			meta := &provider.CallMeta{StatusCode: resp.StatusCode}
			if resp.StatusCode == 429 {
				meta.Failure = provider.FailRateLimited
				return nil, meta, &httpError{Status: 429}
			}
			if cl := provider.ClassifyHTTP(resp.StatusCode); cl != "" {
				meta.Failure = cl
				return nil, meta, &httpError{Status: resp.StatusCode}
			}
			body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			if err != nil {
				return nil, meta, err
			}
			days, err := parseForecast(body)
			if err != nil {
				meta.Failure = failureOf(err)
				return nil, meta, err
			}
			return days, meta, nil
		},
		Breaker: provider.NewBreaker(3, s.cfg.BreakerCooldown),
	}
}

// ForecastByCoords returns up to MaxForecastDays daily forecasts.
func (s *Service) ForecastByCoords(ctx context.Context, lat, lon float64) ([]Day, provider.Result[[]Day]) {
	strat := provider.Strategy[[]Day]{
		Providers: []provider.Provider[[]Day]{s.forecastProvider(lat, lon)},
		Retry:     provider.DefaultRetryPolicy(),
		Timeout:   10 * time.Second,
	}
	r := strat.Execute(ctx)
	if r.Err != nil {
		return nil, r
	}
	return r.Value, r
}

// ForecastByPlace resolves a place, then fetches its forecast. A place that
// cannot be resolved is an honest failure, never a guessed location.
func (s *Service) ForecastByPlace(ctx context.Context, place string) ([]Day, provider.Result[[]Day]) {
	lat, lon, _, err := geocode(ctx, s.cfg.HTTPClient, s.cfg.GeocodeBase, place)
	if err != nil {
		return nil, provider.Result[[]Day]{Failure: geocodeFailure(err), Err: fmt.Errorf("location lookup failed: %w", err)}
	}
	return s.ForecastByCoords(ctx, lat, lon)
}
